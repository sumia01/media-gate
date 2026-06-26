package plexrefresh

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/store"
)

const testDebounce = 20 * time.Millisecond

// stubStore embeds store.Store so the test only needs to implement the two
// methods the service actually calls; any other call nil-panics and surfaces
// the gap rather than hiding it behind a no-op.
type stubStore struct {
	store.Store
	items    map[uint]*store.MediaItem
	settings map[string]*store.Setting
}

func (s *stubStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if item, ok := s.items[id]; ok {
		return item, nil
	}
	return nil, store.ErrNotFound
}

func (s *stubStore) GetSetting(key string) (*store.Setting, error) {
	if setting, ok := s.settings[key]; ok {
		return setting, nil
	}
	return nil, store.ErrNotFound
}

// stubSettings feeds a fixed Plex URL + token to the plex.Provider.
type stubSettings map[string]string

func (s stubSettings) Get(key string) (string, error) { return s[key], nil }

// recorder captures the section IDs the fake Plex server is asked to refresh.
type recorder struct {
	mu       sync.Mutex
	sections []string
}

func (r *recorder) add(section string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sections = append(r.sections, section)
}

func (r *recorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sections...)
}

// newHarness wires a service to a fake Plex server that records refresh calls.
func newHarness(t *testing.T, st *stubStore) (*Service, *recorder) {
	t.Helper()

	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Plex refresh path: /library/sections/{id}/refresh
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == "library" && parts[1] == "sections" && parts[3] == "refresh" {
			rec.add(parts[2])
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	provider := plex.NewProvider(
		stubSettings{"plex_url": srv.URL, "plex_token": "tok"},
		"plex_url", "plex_token", srv.Client(),
	)

	svc := NewService(provider, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.debounce = testDebounce
	return svc, rec
}

func mapping(libraryID, sectionID string) map[string]*store.Setting {
	return map[string]*store.Setting{"plex:mapping:" + libraryID: {Value: sectionID}}
}

// waitForCalls polls until at least n refresh calls are recorded or the timeout
// elapses, returning the recorded calls either way.
func waitForCalls(rec *recorder, n int, timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if calls := rec.calls(); len(calls) >= n {
			return calls
		}
		time.Sleep(2 * time.Millisecond)
	}
	return rec.calls()
}

func TestHandleImportCompleted_RefreshesMappedSection(t *testing.T) {
	st := &stubStore{
		items:    map[uint]*store.MediaItem{42: {ID: 42, LibraryID: 7}},
		settings: mapping("7", "3"),
	}
	svc, rec := newHarness(t, st)

	svc.HandleImportCompleted(eventbus.Event{Payload: eventbus.ImportPayload{MediaItemID: 42}})

	calls := waitForCalls(rec, 1, time.Second)
	if len(calls) != 1 || calls[0] != "3" {
		t.Fatalf("expected one refresh of section 3, got %v", calls)
	}
}

func TestHandleMediaItemDeleted_UsesPayloadLibraryID(t *testing.T) {
	// No media item in the store on purpose: the handler must rely on the
	// LibraryID carried by the payload, since the item is already deleted.
	st := &stubStore{settings: mapping("7", "3")}
	svc, rec := newHarness(t, st)

	svc.HandleMediaItemDeleted(eventbus.Event{Payload: eventbus.MediaItemPayload{MediaItemID: 42, LibraryID: 7}})

	calls := waitForCalls(rec, 1, time.Second)
	if len(calls) != 1 || calls[0] != "3" {
		t.Fatalf("expected one refresh of section 3, got %v", calls)
	}
}

func TestHandleSubtitleDeleted_RefreshesViaMediaItem(t *testing.T) {
	st := &stubStore{
		items:    map[uint]*store.MediaItem{42: {ID: 42, LibraryID: 7}},
		settings: mapping("7", "3"),
	}
	svc, rec := newHarness(t, st)

	svc.HandleSubtitleDeleted(eventbus.Event{Payload: eventbus.SubtitlePayload{MediaItemID: 42}})

	calls := waitForCalls(rec, 1, time.Second)
	if len(calls) != 1 || calls[0] != "3" {
		t.Fatalf("expected one refresh of section 3, got %v", calls)
	}
}

func TestHandleDownloadDeleted_RefreshesViaMediaItem(t *testing.T) {
	st := &stubStore{
		items:    map[uint]*store.MediaItem{42: {ID: 42, LibraryID: 7}},
		settings: mapping("7", "3"),
	}
	svc, rec := newHarness(t, st)

	svc.HandleDownloadDeleted(eventbus.Event{Payload: eventbus.DownloadPayload{MediaItemID: 42}})

	calls := waitForCalls(rec, 1, time.Second)
	if len(calls) != 1 || calls[0] != "3" {
		t.Fatalf("expected one refresh of section 3, got %v", calls)
	}
}

func TestNoMappingConfigured_NoRefresh(t *testing.T) {
	st := &stubStore{
		items:    map[uint]*store.MediaItem{42: {ID: 42, LibraryID: 7}},
		settings: map[string]*store.Setting{}, // no plex:mapping:7
	}
	svc, rec := newHarness(t, st)

	svc.HandleImportCompleted(eventbus.Event{Payload: eventbus.ImportPayload{MediaItemID: 42}})

	// Wait well past the debounce window; nothing should be refreshed.
	time.Sleep(testDebounce + 150*time.Millisecond)
	if calls := rec.calls(); len(calls) != 0 {
		t.Fatalf("expected no refresh without a mapping, got %v", calls)
	}
}

func TestDebounceCoalescesBurst(t *testing.T) {
	st := &stubStore{
		items:    map[uint]*store.MediaItem{42: {ID: 42, LibraryID: 7}},
		settings: mapping("7", "3"),
	}
	svc, rec := newHarness(t, st)

	// A download deletion that also drops several subtitle files fires a burst
	// of events for the same library; they must collapse into one scan.
	for range 5 {
		svc.HandleSubtitleDeleted(eventbus.Event{Payload: eventbus.SubtitlePayload{MediaItemID: 42}})
	}

	waitForCalls(rec, 1, time.Second)
	// Give any erroneously-scheduled extra timers time to fire before asserting.
	time.Sleep(testDebounce + 100*time.Millisecond)
	calls := rec.calls()
	if len(calls) != 1 || calls[0] != "3" {
		t.Fatalf("expected the burst to coalesce into one refresh of section 3, got %v", calls)
	}
}

func TestDebounceSeparatesDistinctLibraries(t *testing.T) {
	st := &stubStore{
		items: map[uint]*store.MediaItem{
			1: {ID: 1, LibraryID: 7},
			2: {ID: 2, LibraryID: 8},
		},
		settings: map[string]*store.Setting{
			"plex:mapping:7": {Value: "3"},
			"plex:mapping:8": {Value: "4"},
		},
	}
	svc, rec := newHarness(t, st)

	svc.HandleSubtitleDeleted(eventbus.Event{Payload: eventbus.SubtitlePayload{MediaItemID: 1}})
	svc.HandleSubtitleDeleted(eventbus.Event{Payload: eventbus.SubtitlePayload{MediaItemID: 2}})

	waitForCalls(rec, 2, time.Second)
	time.Sleep(testDebounce + 100*time.Millisecond)
	calls := rec.calls()
	if len(calls) != 2 {
		t.Fatalf("expected two refreshes for two libraries, got %v", calls)
	}
	got := map[string]bool{calls[0]: true, calls[1]: true}
	if !got["3"] || !got["4"] {
		t.Fatalf("expected refreshes of sections 3 and 4, got %v", calls)
	}
}
