package download

import (
	"sync"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
)

// stubStore embeds store.Store so tests only implement the methods exercised;
// any other call nil-panics and surfaces the gap.
type stubStore struct {
	store.Store
	byStatus map[string][]store.Download

	mu      sync.Mutex
	updated []store.Download
}

func (s *stubStore) ListDownloads(_ *uint, status *string) ([]store.Download, error) {
	if status == nil {
		return nil, nil
	}
	return s.byStatus[*status], nil
}

func (s *stubStore) UpdateDownload(dl *store.Download) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, *dl)
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

type recorder struct {
	mu     sync.Mutex
	events []eventbus.Event
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
