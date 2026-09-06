package media

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

type deletionStore struct {
	store.Store
	updateErr error
	deleteErr error
}

func (s *deletionStore) UpdateDownload(dl *store.Download) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.Store.UpdateDownload(dl)
}

func (s *deletionStore) DeleteDownload(id uint) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.Store.DeleteDownload(id)
}

func TestDeleteDownloadCancellationAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deleteFiles  bool
		cancelFails  bool
		deleteFails  bool
		qbitFails    bool
		unconfigured bool
	}{
		{name: "keep imported files"},
		{name: "remove imported files", deleteFiles: true},
		{name: "cancellation save fails", deleteFiles: true, cancelFails: true},
		{name: "database delete fails", deleteFails: true},
		{name: "qbit failure remains best effort", deleteFiles: true, qbitFails: true},
		{name: "qbit unconfigured remains best effort", deleteFiles: true, unconfigured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := sqlite.New(filepath.Join(t.TempDir(), "media.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
			if err := s.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Status: "ready", Source: "disk"}
			if err := s.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			downloadedAt := time.Now().Add(-time.Hour)
			dl := &store.Download{
				MediaItemID: item.ID, Title: "Movie.1080p", Status: "seeding", ClientTorrentHash: "abc",
				LinkedToLibrary: true, LastError: "previous failure", DownloadedAt: &downloadedAt,
			}
			if err := s.CreateDownload(dl); err != nil {
				t.Fatal(err)
			}
			releaseDir := filepath.Join(importer.BuildTargetDir(lib, item, nil, nil), importer.BuildReleaseFolderName(dl.Title))
			if err := os.MkdirAll(releaseDir, 0755); err != nil {
				t.Fatal(err)
			}
			video := filepath.Join(releaseDir, "Movie.mkv")
			if err := os.WriteFile(video, []byte("video"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := s.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: video, FileName: "Movie.mkv", Size: 5}); err != nil {
				t.Fatal(err)
			}
			var deleteCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/torrents/delete":
					deleteCalls.Add(1)
					got, err := s.GetDownload(dl.ID)
					if err != nil || got.Status != "cancelled" || got.LastError != dl.LastError || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
						t.Errorf("cleanup started without preserved cancellation: %+v, %v", got, err)
					}
					wantDeleteFiles := "false"
					if tc.deleteFiles {
						wantDeleteFiles = "true"
					}
					if r.FormValue("hashes") != "abc" || r.FormValue("deleteFiles") != wantDeleteFiles {
						t.Errorf("wrong torrent cleanup options: %v", r.Form)
					}
					if tc.qbitFails {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			config := map[string]string{settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p"}
			if tc.unconfigured {
				config = nil
			}
			settingsSvc := settings.NewService(s, "", config, "test-secret", srv.Client())
			provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
			bus := eventbus.New(16)
			var deletedEvents, failureEvents atomic.Int32
			bus.SubscribeAll(func(e eventbus.Event) {
				if e.Type == eventbus.DownloadDeleted {
					deletedEvents.Add(1)
				}
				if e.Type == eventbus.DownloadFailed || e.Type == eventbus.ImportFailed {
					failureEvents.Add(1)
				}
			})
			bus.Start()
			t.Cleanup(bus.Stop)
			st := &deletionStore{Store: s}
			writeErr := errors.New("database unavailable")
			if tc.cancelFails {
				st.updateErr = writeErr
			}
			if tc.deleteFails {
				st.deleteErr = writeErr
			}
			svc := NewService(st, mediasync.NewService(s), bus, provider, "")
			err = svc.DeleteDownload(dl.ID, tc.deleteFiles)
			if tc.cancelFails || tc.deleteFails {
				if !errors.Is(err, writeErr) {
					t.Fatalf("expected DB error, got %v", err)
				}
				got, err := s.GetDownload(dl.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := "cancelled"
				if tc.cancelFails {
					wantStatus = "seeding"
				}
				if got.Status != wantStatus || got.LastError != dl.LastError || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) || got.ClientTorrentHash != "abc" || !got.LinkedToLibrary {
					t.Errorf("failed deletion lost recoverable cleanup state: %+v", got)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.GetDownload(dl.ID); !errors.Is(err, store.ErrNotFound) {
					t.Errorf("download not deleted: %v", err)
				}
			}
			wantCalls := int32(1)
			if tc.cancelFails || tc.unconfigured {
				wantCalls = 0
			}
			if deleteCalls.Load() != wantCalls {
				t.Errorf("qBit cleanup calls=%d, want %d", deleteCalls.Load(), wantCalls)
			}
			removed := tc.deleteFiles && !tc.cancelFails
			_, statErr := os.Stat(video)
			files, err := s.ListMediaFilesByMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if removed {
				if !os.IsNotExist(statErr) || len(files) != 0 {
					t.Errorf("imported file cleanup changed: stat=%v, rows=%d", statErr, len(files))
				}
			} else if statErr != nil || len(files) != 1 {
				t.Errorf("imported files unexpectedly removed: stat=%v, rows=%d", statErr, len(files))
			}
			if tc.deleteFails {
				// Retrying deletion of the retained cancelled row must still work.
				st.deleteErr = nil
				if err := svc.DeleteDownload(dl.ID, false); err != nil {
					t.Fatalf("retry deletion: %v", err)
				}
			}
			bus.Stop()
			wantDeletedEvents := int32(0)
			if removed && !tc.deleteFails {
				wantDeletedEvents = 1
			}
			if deletedEvents.Load() != wantDeletedEvents || failureEvents.Load() != 0 {
				t.Errorf("unexpected events: deleted=%d failures=%d", deletedEvents.Load(), failureEvents.Load())
			}
		})
	}
}

type cancellationFailureStore struct {
	store.Store
	writes    *int
	parentErr bool
}

func (s *cancellationFailureStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&cancellationFailureStore{Store: tx, writes: s.writes, parentErr: s.parentErr})
	})
}

func (s *cancellationFailureStore) UpdateMediaItem(item *store.MediaItem) error {
	if s.parentErr {
		return errors.New("disable monitoring failed")
	}
	return s.Store.UpdateMediaItem(item)
}

func (s *cancellationFailureStore) UpdateDownload(dl *store.Download) error {
	(*s.writes)++
	if *s.writes == 2 {
		return errors.New("second cancellation failed")
	}
	return s.Store.UpdateDownload(dl)
}

func TestDeleteMediaItemCancellationRollbackHasNoSideEffects(t *testing.T) {
	for _, parentErr := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_failure=%t", parentErr), func(t *testing.T) {
			s, err := sqlite.New(filepath.Join(t.TempDir(), "media.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
			if err := s.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			startedAt := time.Now().Add(-time.Hour)
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Source: "disk", Monitored: true, MonitorSearchStartedAt: &startedAt}
			if err := s.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			downloadedAt := time.Now().Add(-time.Hour)
			children := []store.Download{
				{MediaItemID: item.ID, Status: "downloading", ClientTorrentHash: "one", LastError: "one", SavePath: "/downloads/one", DownloadedAt: &downloadedAt},
				{MediaItemID: item.ID, Status: "importing", ClientTorrentHash: "two", LastError: "two", SavePath: "/downloads/two", DownloadedAt: &downloadedAt},
			}
			for i := range children {
				if err := s.CreateDownload(&children[i]); err != nil {
					t.Fatal(err)
				}
			}
			video := filepath.Join(lib.Path, "Movie.mkv")
			posterDir := t.TempDir()
			poster := filepath.Join(posterDir, fmt.Sprintf("%d.jpg", item.ID))
			for _, path := range []string{video, poster} {
				if err := os.WriteFile(path, []byte("preserve"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: video, FileName: "Movie.mkv"}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("external request before all cancellations committed")
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer srv.Close()
			settingsSvc := settings.NewService(s, "", map[string]string{
				settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
			}, "test-secret", srv.Client())
			provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
			writes := 0
			st := &cancellationFailureStore{Store: s, writes: &writes, parentErr: parentErr}
			bus := eventbus.New(8)
			bus.SubscribeAll(func(e eventbus.Event) { t.Errorf("event on aborted deletion: %s", e.Type) })
			bus.Start()
			defer bus.Stop()
			if err := NewService(st, nil, bus, provider, posterDir).DeleteMediaItem(item.ID); err == nil {
				t.Fatal("expected cancellation error")
			}
			wantWrites := 2
			if parentErr {
				wantWrites = 0
			}
			if writes != wantWrites {
				t.Fatalf("cancellation writes=%d, want %d", writes, wantWrites)
			}
			for _, child := range children {
				got, err := s.GetDownload(child.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != child.Status || !got.UpdatedAt.Equal(child.UpdatedAt) || got.ClientTorrentHash != child.ClientTorrentHash || got.SavePath != child.SavePath || got.LastError != child.LastError || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
					t.Errorf("child cancellation did not roll back: %+v", got)
				}
			}
			parent, err := s.GetMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !parent.Monitored || parent.MonitorSearchStartedAt == nil || !parent.MonitorSearchStartedAt.Equal(startedAt) || !parent.UpdatedAt.Equal(item.UpdatedAt) {
				t.Errorf("parent disable did not roll back: %+v", parent)
			}
			for _, path := range []string{video, poster} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("file removed on cancellation failure: %s: %v", path, err)
				}
			}
		})
	}
}
