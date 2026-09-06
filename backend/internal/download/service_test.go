package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/notification"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

// stubStore embeds store.Store so tests only implement the methods exercised;
// any other call nil-panics and surfaces the gap.
type stubStore struct {
	store.Store
	byStatus  map[string][]store.Download
	download  *store.Download
	updateErr error

	mu      sync.Mutex
	updated []store.Download
}

func (s *stubStore) GetDownload(_ uint) (*store.Download, error) {
	return s.download, nil
}

func (s *stubStore) ListDownloads(_ *uint, status *string) ([]store.Download, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == nil {
		return nil, nil
	}
	return append([]store.Download(nil), s.byStatus[*status]...), nil
}

func (s *stubStore) UpdateDownload(dl *store.Download) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated = append(s.updated, *dl)
	for status, downloads := range s.byStatus {
		for i, existing := range downloads {
			if existing.ID == dl.ID {
				s.byStatus[status] = append(downloads[:i], downloads[i+1:]...)
				s.byStatus[dl.Status] = append(s.byStatus[dl.Status], *dl)
				return nil
			}
		}
	}
	return nil
}

func (s *stubStore) lastUpdate(t *testing.T) store.Download {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.updated) == 0 {
		t.Fatal("no UpdateDownload calls recorded")
	}
	return s.updated[len(s.updated)-1]
}

// TestRecoverStuckImporting covers bug #4: rows stuck in the active "importing"
// status after a crash must be reset to "downloaded" so the importer re-picks
// them, instead of being wedged as active forever.
func TestRecoverStuckImporting(t *testing.T) {
	st := &stubStore{byStatus: map[string][]store.Download{
		"importing": {
			{ID: 1, MediaItemID: 5, Title: "A", Status: "importing"},
			{ID: 2, MediaItemID: 6, Title: "B", Status: "importing"},
		},
	}}
	svc := &Service{store: st, bus: eventbus.New(4)}

	svc.recoverStuckImporting()

	if len(st.updated) != 2 {
		t.Fatalf("updated %d downloads, want 2", len(st.updated))
	}
	for _, u := range st.updated {
		if u.Status != "downloaded" {
			t.Errorf("download %d status = %q, want downloaded", u.ID, u.Status)
		}
	}
}

// TestHandleMissingTorrent_NotLinked covers bug #6: a "downloading" row whose
// torrent vanished from qBit (and was never imported) must go to the terminal,
// non-active "failed" state so the monitor can re-grab.
func TestHandleMissingTorrent_NotLinked(t *testing.T) {
	st := &stubStore{}
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()

	svc := &Service{store: st, bus: bus}
	dl := &store.Download{ID: 9, MediaItemID: 3, Title: "Gone", Status: "downloading", ClientTorrentHash: "abc", LinkedToLibrary: false}

	svc.handleMissingTorrent(dl)
	bus.Stop()

	if dl.Status != "failed" {
		t.Fatalf("status = %q, want failed", dl.Status)
	}
	if dl.ClientTorrentHash != "" {
		t.Error("ClientTorrentHash should be cleared on failure")
	}
	got := st.lastUpdate(t)
	if got.Status != "failed" {
		t.Errorf("persisted status = %q, want failed", got.Status)
	}
	if rec.count(eventbus.DownloadFailed) != 1 {
		t.Errorf("DownloadFailed events = %d, want 1", rec.count(eventbus.DownloadFailed))
	}
}

// TestHandleMissingTorrent_Linked verifies an already-imported download whose
// torrent vanished is marked "completed" (not re-grabbed).
func TestHandleMissingTorrent_Linked(t *testing.T) {
	st := &stubStore{}
	svc := &Service{store: st, bus: eventbus.New(4)}
	dl := &store.Download{ID: 9, MediaItemID: 3, Title: "Done", Status: "downloading", ClientTorrentHash: "abc", LinkedToLibrary: true}

	svc.handleMissingTorrent(dl)

	if dl.Status != "completed" {
		t.Fatalf("status = %q, want completed", dl.Status)
	}
	if dl.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
}

func TestUpdateFromTorrentRecordsDownloadedAt(t *testing.T) {
	st := &stubStore{}
	svc := &Service{store: st, bus: eventbus.New(4)}
	dl := &store.Download{ID: 9, MediaItemID: 3, Title: "Done", Status: "downloading"}
	downloadedAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

	svc.updateFromTorrent(dl, &qbittorrent.TorrentInfo{State: "pausedUP", CompletionOn: downloadedAt.Unix()})

	if dl.Status != "downloaded" {
		t.Fatalf("status = %q, want downloaded", dl.Status)
	}
	if dl.DownloadedAt == nil {
		t.Fatal("DownloadedAt should be set")
	}
	if !dl.DownloadedAt.Equal(downloadedAt) {
		t.Errorf("DownloadedAt = %v, want %v", dl.DownloadedAt, downloadedAt)
	}
	if st.lastUpdate(t).DownloadedAt == nil {
		t.Fatal("persisted DownloadedAt should be set")
	}
}

func TestUpdateStatusPreservesDownloadedAtOnImportRetry(t *testing.T) {
	downloadedAt := time.Now()
	st := &stubStore{download: &store.Download{
		ID: 9, Status: "import_failed", DownloadedAt: &downloadedAt, RetryCount: 3,
	}}
	svc := &Service{store: st}

	dl, err := svc.UpdateStatus(9, "pending")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if dl.DownloadedAt == nil || !dl.DownloadedAt.Equal(downloadedAt) {
		t.Errorf("DownloadedAt = %v, want %v", dl.DownloadedAt, downloadedAt)
	}
	if dl.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0", dl.RetryCount)
	}
}

// TestPollActive_SkipsJustSentThisTick covers the same-tick race: a download
// sendPending just handed to qBittorrent must not be polled again before the
// next tick, since qBittorrent registers a newly added torrent asynchronously
// and GetTorrent can spuriously report it missing. client is nil here — if
// pollActive failed to skip the ID it would panic on the nil dereference,
// making this test self-verifying.
func TestPollActive_SkipsJustSentThisTick(t *testing.T) {
	st := &stubStore{byStatus: map[string][]store.Download{
		"downloading": {
			{ID: 42, MediaItemID: 1, Title: "Fresh", Status: "downloading", ClientTorrentHash: "hash42"},
		},
	}}
	svc := &Service{store: st, bus: eventbus.New(4)}

	svc.pollActive(nil, map[uint]bool{42: true})

	if len(st.updated) != 0 {
		t.Fatalf("pollActive updated %d downloads, want 0 (ID 42 was just sent this tick)", len(st.updated))
	}
}

func TestTerminalFailuresPersistBeforePublishingOnce(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		reason string
		fail   func(*Service, *store.Download)
	}{
		{"retry exhaustion", "pending", "download retries exhausted: fetch failed", func(s *Service, dl *store.Download) {
			s.handleRetry(dl, errors.New("fetch failed"))
		}},
		{"missing torrent", "downloading", "torrent missing from qBittorrent before import", (*Service).handleMissingTorrent},
		{"qbit error", "downloading", "qBittorrent reported a torrent error", func(s *Service, dl *store.Download) {
			s.updateFromTorrent(dl, &qbittorrent.TorrentInfo{State: "error"})
		}},
		{"missing files", "downloading", "qBittorrent reported missing files", func(s *Service, dl *store.Download) {
			s.updateFromTorrent(dl, &qbittorrent.TorrentInfo{State: "missingFiles"})
		}},
	} {
		for _, persistFails := range []bool{false, true} {
			name := tc.name
			if persistFails {
				name += "/persistence failure"
			}
			t.Run(name, func(t *testing.T) {
				st := &stubStore{}
				if persistFails {
					st.updateErr = errors.New("database unavailable")
				}
				bus := eventbus.New(16)
				rec := newRecorder()
				bus.Subscribe(eventbus.DownloadFailed, func(e eventbus.Event) {
					got := st.lastUpdate(t)
					if got.Status != "failed" || got.LastError != tc.reason {
						t.Errorf("event arrived without persisted failure: %+v", got)
					}
					if p, ok := e.Payload.(eventbus.DownloadPayload); !ok || p.DownloadID != 9 || p.Status != "failed" {
						t.Errorf("unexpected failure payload: %+v", e.Payload)
					}
					rec.handle(e)
				})
				bus.Start()
				t.Cleanup(bus.Stop)
				downloadedAt, retryAt := time.Now().Add(-time.Hour), time.Now()
				dl := &store.Download{
					ID: 9, MediaItemID: 3, Status: tc.status, RetryCount: maxRetries,
					ClientTorrentHash: "abc", DownloadedAt: &downloadedAt, NextRetryAt: &retryAt,
				}
				svc := &Service{store: st, bus: bus}
				tc.fail(svc, dl)
				tc.fail(svc, dl)
				bus.Stop()
				if persistFails {
					if rec.count(eventbus.DownloadFailed) != 0 || dl.Status != tc.status {
						t.Fatalf("failed save notified or changed status: events=%d status=%s", rec.count(eventbus.DownloadFailed), dl.Status)
					}
					return
				}
				if rec.count(eventbus.DownloadFailed) != 1 || len(st.updated) != 1 {
					t.Errorf("events=%d writes=%d, want one of each", rec.count(eventbus.DownloadFailed), len(st.updated))
				}
				got := st.lastUpdate(t)
				if got.NextRetryAt != nil || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
					t.Errorf("incorrect terminal timestamps: %+v", got)
				}
			})
		}
	}
}

func TestDownloadNonTerminalChangesDoNotPublishFailure(t *testing.T) {
	st := &stubStore{}
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()
	t.Cleanup(bus.Stop)
	svc := &Service{store: st, bus: bus}
	dl := &store.Download{ID: 1, Status: "pending"}
	for range maxRetries {
		svc.handleRetry(dl, errors.New("temporary fetch error"))
		if dl.Status != "pending" || dl.NextRetryAt == nil || dl.LastError == "" {
			t.Fatalf("expected persisted backoff, got %+v", dl)
		}
	}
	svc.handleRetry(dl, context.Canceled)
	if dl.Status != "pending" || dl.RetryCount != maxRetries {
		t.Fatalf("context cancellation exhausted retries: %+v", dl)
	}
	for _, status := range []string{"cancelled", "completed", "failed", "import_failed"} {
		dl := &store.Download{ID: 2, Status: status, RetryCount: maxRetries}
		svc.handleRetry(dl, errors.New("ignored"))
		svc.handleMissingTorrent(dl)
		svc.updateFromTorrent(dl, &qbittorrent.TorrentInfo{State: "error"})
		if dl.Status != status {
			t.Errorf("changed inactive status %s to %s", status, dl.Status)
		}
	}
	for _, state := range []string{"unknown", "futureState", "pausedDL", "moving"} {
		dl := &store.Download{ID: 3, Status: "downloading"}
		svc.updateFromTorrent(dl, &qbittorrent.TorrentInfo{State: state})
		if dl.Status != "downloading" {
			t.Errorf("client state %s caused terminal transition", state)
		}
	}
	bus.Stop()
	if rec.count(eventbus.DownloadFailed) != 0 {
		t.Error("retry/cancellation/transient state emitted a failure")
	}
}

type qbitSettings map[string]string

func (s qbitSettings) Get(key string) (string, error) { return s[key], nil }

func TestReconcileMissingTorrents(t *testing.T) {
	for _, tc := range []struct {
		name        string
		linked      bool
		unreachable bool
		persistFail bool
		wantStatus  string
		wantEvents  int
	}{
		{"missing download", false, false, false, "failed", 1},
		{"already imported", true, false, false, "completed", 0},
		{"client outage", false, true, false, "downloading", 0},
		{"database outage", false, false, true, "downloading", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/app/version":
					if tc.unreachable {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
				case "/api/v2/torrents/info":
					_, _ = w.Write([]byte("[]"))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			status := "downloading"
			if tc.linked {
				status = "seeding"
			}
			st := &stubStore{byStatus: map[string][]store.Download{
				status: {{ID: 1, Status: status, ClientTorrentHash: "abc", LinkedToLibrary: tc.linked}},
			}}
			if tc.persistFail {
				st.updateErr = errors.New("database unavailable")
			}
			bus := eventbus.New(16)
			rec := newRecorder()
			bus.SubscribeAll(rec.handle)
			bus.Start()
			t.Cleanup(bus.Stop)
			svc := &Service{store: st, bus: bus, qbit: qbittorrent.NewProvider(qbitSettings{"url": srv.URL}, "url", "user", "pass", srv.Client())}
			svc.Reconcile()
			svc.Reconcile()
			bus.Stop()
			if got := rec.count(eventbus.DownloadFailed); got != tc.wantEvents {
				t.Errorf("failure events=%d, want %d", got, tc.wantEvents)
			}
			rows, _ := st.ListDownloads(nil, &tc.wantStatus)
			if len(rows) != 1 {
				t.Fatalf("expected one %s row, got %+v", tc.wantStatus, st.byStatus)
			}
			if tc.wantEvents != 0 && !strings.Contains(rows[0].LastError, "missing") {
				t.Errorf("missing persisted failure reason: %+v", rows[0])
			}
		})
	}
}

func TestPollActiveTransientClientErrorDoesNotFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
			_, _ = w.Write([]byte("Ok."))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	st := &stubStore{byStatus: map[string][]store.Download{
		"downloading": {{ID: 1, Status: "downloading", ClientTorrentHash: "abc"}},
	}}
	bus := eventbus.New(4)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()
	svc := &Service{store: st, bus: bus}
	svc.pollActive(qbittorrent.NewClient(srv.URL, "u", "p", srv.Client()), nil)
	bus.Stop()
	if len(st.updated) != 0 || rec.count(eventbus.DownloadFailed) != 0 {
		t.Error("transient client error persisted or published a failure")
	}
}

type recorder struct {
	mu     sync.Mutex
	events []eventbus.Event
}

type deletionInterleavingStore struct {
	store.Store
	afterList    func()
	beforeDelete func()
}

func (s *deletionInterleavingStore) ListDownloads(id *uint, status *string) ([]store.Download, error) {
	downloads, err := s.Store.ListDownloads(id, status)
	if err == nil && status != nil && *status == "downloading" {
		s.afterList()
	}
	return downloads, err
}

func (s *deletionInterleavingStore) DeleteDownload(id uint) error {
	s.beforeDelete()
	return s.Store.DeleteDownload(id)
}

func (s *deletionInterleavingStore) DeleteMediaItem(id uint) error {
	s.beforeDelete()
	return s.Store.DeleteMediaItem(id)
}

func newPersistedDownload(t *testing.T, status string) (*sqlite.SQLiteStore, *store.Download) {
	t.Helper()
	s, err := sqlite.New(filepath.Join(t.TempDir(), "downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Status: "new", Source: "disk"}
	if err := s.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	downloadedAt, nextRetryAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	dl := &store.Download{
		MediaItemID: item.ID, Title: "Movie.1080p", Status: status, ClientTorrentHash: "abc",
		LastError: "previous failure", DownloadedAt: &downloadedAt, RetryCount: 3, NextRetryAt: &nextRetryAt,
	}
	if err := s.CreateDownload(dl); err != nil {
		t.Fatal(err)
	}
	return s, dl
}

func TestDeleteDownloadDoesNotNotifyStaleInFlightPoll(t *testing.T) {
	for _, wholeItem := range []bool{false, true} {
		name := "single download"
		if wholeItem {
			name = "whole media item"
		}
		t.Run(name, func(t *testing.T) {
			s, dl := newPersistedDownload(t, "downloading")
			downloads := []store.Download{*dl}
			if wholeItem {
				for _, status := range []string{"downloading", "pending", "importing", "seeding"} {
					child := *dl
					child.ID, child.Status = 0, status
					child.ClientTorrentHash = status
					if err := s.CreateDownload(&child); err != nil {
						t.Fatal(err)
					}
					downloads = append(downloads, child)
				}
			}
			snapshotReady, beforeDelete, pollDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			wait := func(ch <-chan struct{}) {
				t.Helper()
				select {
				case <-ch:
				case <-time.After(5 * time.Second):
					t.Error("interleaving barrier timed out")
				}
			}
			var torrentRemoved atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/torrents/delete":
					for _, child := range downloads {
						persisted, err := s.GetDownload(child.ID)
						if err != nil || persisted.Status != "cancelled" || persisted.ClientTorrentHash != child.ClientTorrentHash {
							t.Errorf("torrent removed without all child cancellations: %+v, %v", persisted, err)
						}
					}
					torrentRemoved.Store(true)
				case "/api/v2/torrents/info":
					if !torrentRemoved.Load() {
						t.Error("poll ran before torrent removal")
					}
					_, _ = w.Write([]byte("[]"))
				case "/webhook":
					t.Error("user deletion sent a Discord notification")
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			settingsSvc := settings.NewService(s, "", map[string]string{settings.KeyDiscordWebhookURL: srv.URL + "/webhook"}, "test-secret", srv.Client())
			provider := qbittorrent.NewProvider(qbitSettings{"url": srv.URL}, "url", "user", "pass", srv.Client())
			client, err := provider.Client()
			if err != nil {
				t.Fatal(err)
			}
			bus := eventbus.New(16)
			rec := newRecorder()
			bus.SubscribeAll(rec.handle)
			notification.NewService(s, settingsSvc, bus, srv.Client())
			bus.Start()
			t.Cleanup(bus.Stop)
			interleaving := &deletionInterleavingStore{
				Store: s,
				afterList: func() {
					close(snapshotReady)
					wait(beforeDelete)
				},
				beforeDelete: func() {
					// qBit removal has finished, but the row is deliberately kept alive
					// until the stale poll and all notification handlers have completed.
					close(beforeDelete)
					wait(pollDone)
					bus.Stop()
					for _, child := range downloads {
						persisted, err := s.GetDownload(child.ID)
						if err != nil || persisted.Status != "cancelled" || persisted.LastError != child.LastError || persisted.DownloadedAt == nil || !persisted.DownloadedAt.Equal(*child.DownloadedAt) {
							t.Errorf("stale poll changed cancellation/metadata: %+v, %v", persisted, err)
						}
					}
				},
			}
			svc := &Service{store: interleaving, bus: bus}
			go func() {
				defer close(pollDone)
				svc.pollActive(client, nil)
			}()
			wait(snapshotReady)
			mediaSvc := media.NewService(interleaving, nil, bus, provider, t.TempDir())
			if wholeItem {
				err = mediaSvc.DeleteMediaItem(dl.MediaItemID)
			} else {
				err = mediaSvc.DeleteDownload(dl.ID, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if rec.count(eventbus.DownloadFailed) != 0 {
				t.Fatal("stale poll published a failure after user deletion")
			}
			if _, err := s.GetDownload(dl.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("download was not deleted: %v", err)
			}
			// A worker finishing even later cannot resurrect the removed row.
			svc.handleMissingTorrent(dl)
			if _, err := s.GetDownload(dl.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("stale worker resurrected download: %v", err)
			}
			if wholeItem {
				for _, child := range downloads {
					if _, err := s.GetDownload(child.ID); !errors.Is(err, store.ErrNotFound) {
						t.Fatalf("child download did not cascade: %v", err)
					}
				}
			}
		})
	}
}

func TestManualRetryUsesCurrentSnapshotAndPreservesDownloadedAt(t *testing.T) {
	for _, status := range []string{"failed", "import_failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			s, dl := newPersistedDownload(t, "downloading")
			stale := *dl
			dl.Status = status
			if err := s.UpdateDownload(dl); err != nil {
				t.Fatal(err)
			}
			bus := eventbus.New(8)
			rec := newRecorder()
			bus.SubscribeAll(rec.handle)
			bus.Start()
			t.Cleanup(bus.Stop)
			svc := &Service{store: s, bus: bus}
			retried, err := svc.UpdateStatus(dl.ID, "pending")
			if err != nil {
				t.Fatalf("manual retry: %v", err)
			}
			if retried.Status != "pending" || retried.LastError != "" || retried.RetryCount != 0 || retried.NextRetryAt != nil || retried.DownloadedAt == nil || !retried.DownloadedAt.Equal(*dl.DownloadedAt) {
				t.Fatalf("manual retry did not preserve/reset intended fields: %+v", retried)
			}
			svc.handleMissingTorrent(&stale)
			bus.Stop()
			current, err := s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != "pending" || current.LastError != "" || !current.DownloadedAt.Equal(*dl.DownloadedAt) || rec.count(eventbus.DownloadFailed) != 0 {
				t.Errorf("stale worker overwrote fresh manual retry: %+v", current)
			}
		})
	}
}

func newRecorder() *recorder { return &recorder{} }

func (r *recorder) handle(e eventbus.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) count(t eventbus.EventType) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Type == t {
			n++
		}
	}
	return n
}
