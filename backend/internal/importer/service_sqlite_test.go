package importer

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/metarefresh"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	mediasync "github.com/sumia01/media-gate/internal/sync"
	"github.com/sumia01/media-gate/internal/worker"
)

type importCASStore struct {
	store.Store
	beforeUpdate func(*store.Download) error
	afterUpdate  func(*store.Download)
}

func (s *importCASStore) UpdateDownload(dl *store.Download) error {
	if s.beforeUpdate != nil {
		if err := s.beforeUpdate(dl); err != nil {
			return err
		}
	}
	if err := s.Store.UpdateDownload(dl); err != nil {
		return err
	}
	if s.afterUpdate != nil {
		s.afterUpdate(dl)
	}
	return nil
}

func newImportFixture(t *testing.T, seed bool) (*sqlite.SQLiteStore, *store.Download) {
	t.Helper()
	s, err := sqlite.New(filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	lib := &store.Library{Name: "Shows", Path: t.TempDir(), MediaType: "series"}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series", Source: "disk", Monitored: true}
	if err := s.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 1, Status: "Returning Series"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1}); err != nil {
		t.Fatal(err)
	}
	idx := &store.Indexer{Name: "Indexer", DefinitionID: "test"}
	if seed {
		idx.SeedMinRatio, idx.SeedMinTime = 2, 10
	}
	if err := s.CreateIndexer(idx); err != nil {
		t.Fatal(err)
	}
	downloadedAt := time.Now().Add(-time.Hour)
	dl := &store.Download{
		MediaItemID: item.ID, IndexerID: idx.ID, Title: "Show.S01E01.1080p", Status: "downloaded",
		ClientTorrentHash: "abc", SavePath: t.TempDir(), LastError: "previous error", DownloadedAt: &downloadedAt,
	}
	if err := os.WriteFile(filepath.Join(dl.SavePath, "Show.S01E01.mkv"), []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDownload(dl); err != nil {
		t.Fatal(err)
	}
	return s, dl
}

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the real metadata worker without provider changes or external I/O.
func runUnchangedMetadataRefresh(t *testing.T, s store.Store) {
	t.Helper()
	httpClient := &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/3/tv/1" {
			t.Errorf("unexpected metadata request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"number_of_seasons":0,"status":"Returning Series"}`))}, nil
	})}
	settingsSvc := settings.NewService(s, "", map[string]string{settings.KeyTMDBApiKey: "test-key"}, "test-secret", httpClient)
	matchSvc := matching.NewService(s, settingsSvc, t.TempDir(), httpClient)
	svc := metarefresh.NewService(s, matchSvc, mediasync.NewService(s), settingsSvc, eventbus.New(8))
	done := make(chan struct{})
	worker.NewRegistry(func(_ string, running bool, _, _ time.Time) {
		if !running {
			close(done)
		}
	}).Register(svc.Loop())
	svc.Start()
	defer svc.Stop()
	svc.Loop().RunNow()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata refresh did not finish")
	}
}

func TestImportSQLiteLifecycleWithMetadataBackfill(t *testing.T) {
	for _, outcome := range []string{"backfill before claim", "retry", "exhausted", "no videos", "completed", "seeding", "cleanup error"} {
		t.Run(outcome, func(t *testing.T) {
			s, dl := newImportFixture(t, outcome == "seeding")
			if outcome == "exhausted" {
				dl.RetryCount = maxImportRetries
				if err := s.UpdateDownload(dl); err != nil {
					t.Fatal(err)
				}
			}
			downloadedAt := *dl.DownloadedAt
			var filesFail, seedMet atomic.Bool
			filesFail.Store(outcome == "retry" || outcome == "exhausted")
			var deletes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/torrents/files":
					if filesFail.Load() {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					files := []qbittorrent.TorrentFile{{Name: "Show.S01E01.mkv", Size: 5}}
					if outcome == "no videos" {
						files = nil
					}
					_ = json.NewEncoder(w).Encode(files)
				case "/api/v2/torrents/info":
					info := qbittorrent.TorrentInfo{Hash: "abc", State: "uploading"}
					if seedMet.Load() {
						info.Ratio, info.SeedingTime = 2, 600
					}
					_ = json.NewEncoder(w).Encode([]qbittorrent.TorrentInfo{info})
				case "/api/v2/torrents/delete":
					deletes.Add(1)
					persisted, err := s.GetDownload(dl.ID)
					if err != nil || persisted.Status != "completed" || !persisted.LinkedToLibrary || persisted.DownloadedAt == nil || !persisted.DownloadedAt.Equal(downloadedAt) {
						t.Errorf("cleanup preceded persisted import completion: %+v, %v", persisted, err)
					}
					if outcome == "cleanup error" {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
				default:
					t.Errorf("unexpected qBit request: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			client := qbittorrent.NewClient(srv.URL, "u", "p", srv.Client())
			bus := eventbus.New(16)
			rec := newRecorder()
			bus.SubscribeAll(rec.handle)
			bus.Start()
			t.Cleanup(bus.Stop)
			st := &importCASStore{Store: s}
			refreshed := false
			refresh := func(update *store.Download) {
				if update.Status != "importing" || refreshed {
					return
				}
				refreshed = true
				runUnchangedMetadataRefresh(t, s)
				current, err := s.GetDownload(dl.ID)
				if err != nil {
					t.Fatal(err)
				}
				if outcome != "backfill before claim" && (current.EpisodeID != nil || !current.UpdatedAt.Equal(update.UpdatedAt)) {
					t.Fatalf("metadata invalidated owned import: %+v", current)
				}
			}
			if outcome == "backfill before claim" {
				st.beforeUpdate = func(update *store.Download) error { refresh(update); return nil }
			} else {
				st.afterUpdate = refresh
			}
			svc := &Service{store: st, syncSvc: mediasync.NewService(s), bus: bus}
			svc.importOne(client, dl)
			current, err := s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantFailed := outcome == "exhausted" || outcome == "no videos"
			switch outcome {
			case "backfill before claim", "retry":
				if current.Status != "downloaded" || deletes.Load() != 0 {
					t.Fatalf("expected retryable row with untouched torrent: %+v", current)
				}
				if outcome == "retry" && (current.RetryCount != 1 || current.NextRetryAt == nil || !strings.Contains(current.LastError, "failed to get torrent files")) {
					t.Fatalf("retry reason/backoff not persisted: %+v", current)
				}
				runUnchangedMetadataRefresh(t, s)
				current, err = s.GetDownload(dl.ID)
				if err != nil || current.EpisodeID == nil {
					t.Fatalf("unchanged later cycle did not backfill: %+v, %v", current, err)
				}
				filesFail.Store(false)
				svc.importOne(client, current)
			case "exhausted", "no videos":
				if current.Status != "import_failed" || current.LastError == "" || current.NextRetryAt != nil || deletes.Load() != 0 {
					t.Fatalf("terminal outcome not safely persisted: %+v", current)
				}
			case "seeding":
				if current.Status != "seeding" || !current.LinkedToLibrary || deletes.Load() != 0 {
					t.Fatalf("seeding obligations were bypassed: %+v", current)
				}
				svc.cleanupSeeding(client)
				if deletes.Load() != 0 {
					t.Fatal("torrent removed before seeding requirements were met")
				}
				seedMet.Store(true)
				svc.cleanupSeeding(client)
			}
			bus.Stop()
			current, err = s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.DownloadedAt == nil || !current.DownloadedAt.Equal(downloadedAt) {
				t.Error("DownloadedAt changed during lifecycle")
			}
			if wantFailed {
				if rec.count(eventbus.ImportFailed) != 1 || rec.count(eventbus.ImportCompleted) != 0 {
					t.Error("unexpected failure lifecycle events")
				}
			} else if current.Status != "completed" || rec.count(eventbus.ImportCompleted) != 1 || rec.count(eventbus.ImportFailed) != 0 || deletes.Load() != 1 {
				t.Errorf("success lifecycle mismatch: status=%s success=%d failure=%d deletes=%d", current.Status, rec.count(eventbus.ImportCompleted), rec.count(eventbus.ImportFailed), deletes.Load())
			}
			if outcome == "seeding" && rec.count(eventbus.SeedingCompleted) != 1 {
				t.Error("seeding completion event missing")
			}
		})
	}
}

func TestImportSQLiteRejectedOutcomesNeverNotifyOrRemoveTorrent(t *testing.T) {
	for _, outcome := range []string{"claim", "retry", "failure", "success", "seeding", "seeding completion"} {
		for _, databaseFailure := range []bool{false, true} {
			name := outcome + "/cancelled"
			if databaseFailure {
				name = outcome + "/database failure"
			}
			t.Run(name, func(t *testing.T) {
				s, dl := newImportFixture(t, outcome == "seeding" || outcome == "seeding completion")
				if outcome == "seeding completion" {
					dl.Status, dl.LinkedToLibrary = "seeding", true
					if err := s.UpdateDownload(dl); err != nil {
						t.Fatal(err)
					}
				}
				var deletes atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/auth/login":
						http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
						_, _ = w.Write([]byte("Ok."))
					case "/api/v2/torrents/files":
						if outcome == "retry" {
							w.WriteHeader(http.StatusServiceUnavailable)
						} else if outcome == "failure" {
							_, _ = w.Write([]byte("[]"))
						} else {
							_ = json.NewEncoder(w).Encode([]qbittorrent.TorrentFile{{Name: "Show.S01E01.mkv", Size: 5}})
						}
					case "/api/v2/torrents/delete":
						deletes.Add(1)
					default:
						t.Errorf("unexpected request: %s", r.URL.Path)
					}
				}))
				defer srv.Close()
				client := qbittorrent.NewClient(srv.URL, "u", "p", srv.Client())
				bus := eventbus.New(16)
				rec := newRecorder()
				bus.SubscribeAll(rec.handle)
				bus.Start()
				t.Cleanup(bus.Stop)
				targetStatus := "completed"
				switch outcome {
				case "claim":
					targetStatus = "importing"
				case "retry":
					targetStatus = "downloaded"
				case "failure":
					targetStatus = "import_failed"
				case "seeding":
					targetStatus = "seeding"
				}
				st := &importCASStore{Store: s}
				cancel := func() {
					current, err := s.GetDownload(dl.ID)
					if err != nil {
						t.Fatal(err)
					}
					current.Status = "cancelled"
					if err := s.UpdateDownload(current); err != nil {
						t.Fatal(err)
					}
				}
				st.beforeUpdate = func(update *store.Download) error {
					if update.Status == targetStatus {
						if databaseFailure {
							return errors.New("database unavailable")
						}
						cancel()
					}
					return nil
				}
				svc := &Service{store: st, syncSvc: mediasync.NewService(s), bus: bus}
				if outcome == "seeding completion" {
					svc.completeDownload(dl, client)
				} else {
					svc.importOne(client, dl)
				}
				bus.Stop()
				current, err := s.GetDownload(dl.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := "cancelled"
				if databaseFailure {
					wantStatus = "importing"
					if outcome == "claim" {
						wantStatus = "downloaded"
					} else if outcome == "seeding completion" {
						wantStatus = "seeding"
					}
				}
				if current.Status != wantStatus || current.LastError != "previous error" || current.DownloadedAt == nil || !current.DownloadedAt.Equal(*dl.DownloadedAt) {
					t.Errorf("rejected outcome overwrote current state: %+v", current)
				}
				if deletes.Load() != 0 || rec.count(eventbus.ImportCompleted) != 0 || rec.count(eventbus.ImportFailed) != 0 || rec.count(eventbus.SeedingCompleted) != 0 {
					t.Error("lost ownership or failed persistence still caused cleanup/events")
				}
				if _, err := os.Stat(filepath.Join(dl.SavePath, "Show.S01E01.mkv")); err != nil {
					t.Errorf("source payload was not preserved: %v", err)
				}
			})
		}
	}
}

func TestCompletedTorrentCleanupCommitsClaimBeforeNetworkIO(t *testing.T) {
	s, dl := newImportFixture(t, false)
	dl.Status, dl.LinkedToLibrary = "seeding", true
	if err := s.UpdateDownload(dl); err != nil {
		t.Fatal(err)
	}
	original := *dl
	unrelated := &store.Download{MediaItemID: dl.MediaItemID, Status: "pending", Title: "Another download"}
	if err := s.CreateDownload(unrelated); err != nil {
		t.Fatal(err)
	}
	removing, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRemoval := func() { releaseOnce.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
			_, _ = w.Write([]byte("Ok."))
			return
		}
		if r.URL.Path != "/api/v2/torrents/delete" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		current, err := s.GetDownload(original.ID)
		if err != nil || current.Status != "completed" || !current.LinkedToLibrary {
			t.Errorf("cleanup did not have a committed import: %+v, %v", current, err)
		}
		close(removing)
		<-release
	}))
	defer srv.Close()
	defer releaseRemoval()
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()
	defer bus.Stop()
	svc := &Service{store: s, bus: bus}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		svc.completeDownload(dl, qbittorrent.NewClient(srv.URL, "u", "p", srv.Client()))
	}()
	t.Cleanup(func() { releaseRemoval(); <-finished })
	select {
	case <-removing:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not reach qBit")
	}
	// The server cannot finish removal until we release it, but unrelated writes
	// must succeed now rather than waiting for qBit or failing with SQLITE_BUSY.
	written := make(chan error, 1)
	go func() {
		unrelated.LastError = "unrelated update"
		written <- s.UpdateDownload(unrelated)
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("unrelated write failed while qBit was paused: %v", err)
		}
	case <-time.After(5 * time.Second):
		releaseRemoval()
		<-written
		t.Fatal("unrelated write blocked on qBit cleanup")
	}
	updated, err := s.GetDownload(unrelated.ID)
	if err != nil || updated.LastError != "unrelated update" {
		t.Fatalf("unrelated update did not persist: %+v, %v", updated, err)
	}
	current, err := s.GetDownload(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimVersion := current.UpdatedAt
	if current.Status != "completed" || current.CompletedAt == nil || claimVersion.Equal(original.UpdatedAt) {
		t.Fatalf("cleanup claim did not commit before network I/O: %+v", current)
	}
	staleCancellation := original
	staleCancellation.Status = "cancelled"
	if err := s.UpdateDownload(&staleCancellation); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale cancellation error = %v, want rejected snapshot", err)
	}
	// Fresh cancellation can commit while qBit is paused. It does not revoke
	// this already-authorized removal, and cleanup must not overwrite it later.
	current.Status = "cancelled"
	if err := s.UpdateDownload(current); err != nil {
		t.Fatalf("fresh cancellation after cleanup claim could not proceed: %v", err)
	}
	releaseRemoval()
	<-finished
	bus.Stop()
	if rec.count(eventbus.SeedingCompleted) != 1 || !dl.UpdatedAt.Equal(claimVersion) {
		t.Fatal("authorized finalization failed or caller did not receive committed claim version")
	}
	current, err = s.GetDownload(original.ID)
	if err != nil || current.Status != "cancelled" || current.LastError != original.LastError || current.DownloadedAt == nil || !current.DownloadedAt.Equal(*original.DownloadedAt) {
		t.Fatalf("cleanup overwrote later cancellation or metadata: %+v, %v", current, err)
	}
}

func TestCompletedTorrentCleanupCancelledBeforeClaim(t *testing.T) {
	s, dl := newImportFixture(t, false)
	dl.Status, dl.LinkedToLibrary = "seeding", true
	if err := s.UpdateDownload(dl); err != nil {
		t.Fatal(err)
	}
	cancelled := *dl
	cancelled.Status = "cancelled"
	if err := s.UpdateDownload(&cancelled); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("qBit request despite cancellation committed before cleanup claim")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()
	defer bus.Stop()
	svc := &Service{store: s, bus: bus}
	version := dl.UpdatedAt
	svc.completeDownload(dl, qbittorrent.NewClient(srv.URL, "u", "p", srv.Client()))
	bus.Stop()
	if rec.count(eventbus.SeedingCompleted) != 0 {
		t.Fatal("cancelled download authorized finalization")
	}
	if !dl.UpdatedAt.Equal(version) {
		t.Error("rejected claim changed caller snapshot")
	}
	current, err := s.GetDownload(dl.ID)
	if err != nil || current.Status != "cancelled" || !current.UpdatedAt.Equal(cancelled.UpdatedAt) || current.LastError != dl.LastError || current.DownloadedAt == nil || !current.DownloadedAt.Equal(*dl.DownloadedAt) {
		t.Fatalf("rejected cleanup changed cancellation or metadata: %+v, %v", current, err)
	}
}
