package importer_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/notification"
	"github.com/sumia01/media-gate/internal/plexrefresh"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	"github.com/sumia01/media-gate/internal/subtitle"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

type unavailableCleanupStore struct {
	store.Store
	claims    atomic.Int32
	idlePolls atomic.Int32
}

func (s *unavailableCleanupStore) WithTx(func(store.Store) error) error {
	s.claims.Add(1)
	return errors.New("SQLITE_BUSY: cleanup claim unavailable")
}

func (s *unavailableCleanupStore) ListDownloads(itemID *uint, status *string) ([]store.Download, error) {
	rows, err := s.Store.ListDownloads(itemID, status)
	if err == nil && status != nil && *status == "downloaded" && len(rows) == 0 {
		s.idlePolls.Add(1)
	}
	return rows, err
}

type finalizationSubtitleProvider struct{ searches atomic.Int32 }

func (p *finalizationSubtitleProvider) Name() string { return "finalization-test" }

func (p *finalizationSubtitleProvider) Search(context.Context, subtitle.SearchRequest) ([]subtitle.SearchResult, error) {
	p.searches.Add(1)
	return []subtitle.SearchResult{{ProviderName: p.Name(), ProviderFileID: "1", Language: "en"}}, nil
}

func (p *finalizationSubtitleProvider) Download(context.Context, string) (*subtitle.DownloadedFile, error) {
	return &subtitle.DownloadedFile{FileName: "Movie.en.srt", Format: "srt", Data: []byte("1\n00:00:00,000 --> 00:00:01,000\nTest\n")}, nil
}

func TestImportSQLiteFinalizationReachesRealConsumers(t *testing.T) {
	s, err := sqlite.New(filepath.Join(t.TempDir(), "finalization.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Status: "requested", Source: "request"}
	if err := s.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 1}); err != nil {
		t.Fatal(err)
	}
	downloadedAt := time.Now().Add(-time.Hour)
	dl := &store.Download{MediaItemID: item.ID, Title: "Movie.2024", Status: "downloaded", ClientTorrentHash: "abc", SavePath: t.TempDir(), DownloadedAt: &downloadedAt}
	if err := os.WriteFile(filepath.Join(dl.SavePath, "Movie.2024.mkv"), []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDownload(dl); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(&store.Setting{Key: fmt.Sprintf("plex:mapping:%d", lib.ID), Value: "1"}); err != nil {
		t.Fatal(err)
	}
	var deletes, notifications, refreshes atomic.Int32
	finished := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v5.0.0"))
		case "/api/v2/torrents/files":
			_, _ = w.Write([]byte(`[{"name":"Movie.2024.mkv","size":99}]`))
		case "/api/v2/torrents/delete":
			current, err := s.GetDownload(dl.ID)
			if err != nil || current.Status != "completed" || !current.LinkedToLibrary || current.CompletedAt == nil {
				t.Errorf("uncommitted cleanup: %+v, %v", current, err)
			}
			deletes.Add(1)
			// Even an actual qBit cleanup failure must not skip other finalization.
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/webhook":
			notifications.Add(1)
			w.WriteHeader(http.StatusNoContent)
			finished <- "notification"
		case "/library/sections/1/refresh":
			refreshes.Add(1)
			finished <- "plex"
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	settingsSvc := settings.NewService(s, lib.Path, map[string]string{
		settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
		settings.KeyPlexURL: srv.URL, settings.KeyPlexToken: "test-token", settings.KeyDiscordWebhookURL: srv.URL + "/webhook",
		settings.KeySubtitleAutoSearch: "true", settings.KeySubtitleLanguages: `["en"]`, settings.KeyWorkerImporterInterval: "1",
	}, "test-secret", srv.Client())
	bus := eventbus.New(32)
	var imports, resyncs, subtitles atomic.Int32
	bus.SubscribeAll(func(e eventbus.Event) {
		switch e.Type {
		case eventbus.ImportCompleted:
			imports.Add(1)
		case eventbus.ResyncCompleted:
			resyncs.Add(1)
		case eventbus.SubtitleAutoSearchCompleted:
			subtitles.Add(1)
			finished <- "subtitle"
		}
	})
	plexSvc := plexrefresh.NewService(plex.NewProvider(settingsSvc, settings.KeyPlexURL, settings.KeyPlexToken, srv.Client()), s, slog.Default())
	bus.Subscribe(eventbus.ImportCompleted, plexSvc.HandleImportCompleted)
	subProvider := &finalizationSubtitleProvider{}
	subSvc := subtitle.NewService(s, settingsSvc, bus, []subtitle.Provider{subProvider})
	bus.Subscribe(eventbus.ImportCompleted, subSvc.HandleImportCompleted)
	notification.NewService(s, settingsSvc, bus, srv.Client())
	bus.Start()
	defer bus.Stop()
	syncSvc := mediasync.NewService(s)
	syncSvc.SetBus(bus)
	st := &unavailableCleanupStore{Store: s}
	provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
	svc := importer.NewService(st, settingsSvc, syncSvc, bus, provider)
	svc.Start()
	defer svc.Stop()
	seen := make(map[string]bool)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for len(seen) != 3 || st.idlePolls.Load() < 2 {
		select {
		case name := <-finished:
			seen[name] = true
		case <-poll.C:
		case <-deadline.C:
			t.Fatalf("finalization did not reach real consumers or later ticks: consumers=%v idle=%d", seen, st.idlePolls.Load())
		}
	}
	bus.Stop()
	if st.claims.Load() != 0 || st.idlePolls.Load() < 2 || deletes.Load() != 1 || imports.Load() != 1 || resyncs.Load() != 1 || subtitles.Load() != 1 || subProvider.searches.Load() != 1 || notifications.Load() != 1 || refreshes.Load() != 1 {
		t.Fatalf("lost or duplicate finalization: claims=%d idle=%d deletes=%d imports=%d resyncs=%d subtitles=%d searches=%d notifications=%d plex=%d", st.claims.Load(), st.idlePolls.Load(), deletes.Load(), imports.Load(), resyncs.Load(), subtitles.Load(), subProvider.searches.Load(), notifications.Load(), refreshes.Load())
	}
	current, err := s.GetDownload(dl.ID)
	if err != nil || current.Status != "completed" || current.DownloadedAt == nil || !current.DownloadedAt.Equal(downloadedAt) {
		t.Fatalf("completion state lost: %+v, %v", current, err)
	}
	item, err = s.GetMediaItem(item.ID)
	if err != nil || item.Status != "available" {
		t.Fatalf("status finalization missing: %+v, %v", item, err)
	}
	files, err := s.ListMediaFilesByMediaItem(item.ID)
	if err != nil || len(files) != 1 || files[0].Size != 5 {
		t.Fatalf("resync missing: %+v, %v", files, err)
	}
	subs, err := s.ListSubtitlesByMediaItem(item.ID)
	if err != nil || len(subs) != 1 || subs[0].Source != "auto" {
		t.Fatalf("subtitle auto-download missing: %+v, %v", subs, err)
	}
}
