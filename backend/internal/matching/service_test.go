package matching

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

// memStore is a minimal in-memory store.Store used to exercise the ordering
// invariants of AddMediaToLibraryFull without depending on the sqlite driver.
// It models transaction isolation: WithTx runs fn against a shadow copy and
// only merges the shadow back into the committed state when fn succeeds, so
// reads on the top-level store do NOT see a tx's writes until it commits.
// Unimplemented methods are promoted from the embedded nil interface and would
// panic if called — none of them are on the AddMediaToLibraryFull path.
type memStore struct {
	store.Store

	ids     *uint
	inTx    *atomic.Bool
	items   map[uint]*store.MediaItem
	metas   map[uint]*store.MediaMetadata
	eps     map[uint][]store.Episode
	seasons map[uint][]store.SeasonMonitor
	epMons  map[uint][]store.EpisodeMonitor
}

func newMemStore() *memStore {
	var id uint
	return &memStore{
		ids:     &id,
		inTx:    &atomic.Bool{},
		items:   map[uint]*store.MediaItem{},
		metas:   map[uint]*store.MediaMetadata{},
		eps:     map[uint][]store.Episode{},
		seasons: map[uint][]store.SeasonMonitor{},
		epMons:  map[uint][]store.EpisodeMonitor{},
	}
}

func (m *memStore) clone() *memStore {
	c := &memStore{
		ids: m.ids, inTx: m.inTx,
		items:   map[uint]*store.MediaItem{},
		metas:   map[uint]*store.MediaMetadata{},
		eps:     map[uint][]store.Episode{},
		seasons: map[uint][]store.SeasonMonitor{},
		epMons:  map[uint][]store.EpisodeMonitor{},
	}
	for k, v := range m.items {
		cp := *v
		c.items[k] = &cp
	}
	for k, v := range m.metas {
		cp := *v
		c.metas[k] = &cp
	}
	for k, v := range m.eps {
		c.eps[k] = append([]store.Episode(nil), v...)
	}
	for k, v := range m.seasons {
		c.seasons[k] = append([]store.SeasonMonitor(nil), v...)
	}
	for k, v := range m.epMons {
		c.epMons[k] = append([]store.EpisodeMonitor(nil), v...)
	}
	return c
}

func (m *memStore) WithTx(fn func(store.Store) error) error {
	m.inTx.Store(true)
	defer m.inTx.Store(false)
	shadow := m.clone()
	if err := fn(shadow); err != nil {
		return err // discard shadow; nothing committed
	}
	// Commit: adopt the shadow's state atomically.
	m.items, m.metas, m.eps, m.seasons, m.epMons = shadow.items, shadow.metas, shadow.eps, shadow.seasons, shadow.epMons
	return nil
}

func (m *memStore) Close() error                             { return nil }
func (m *memStore) GetSetting(string) (*store.Setting, error) { return nil, store.ErrNotFound }

func (m *memStore) MediaItemExistsByExternalID(libraryID uint, source string, externalID int) (bool, error) {
	for _, meta := range m.metas {
		if meta.Source == source && meta.ExternalID == externalID {
			if it, ok := m.items[meta.MediaItemID]; ok && it.LibraryID == libraryID {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *memStore) CreateMediaItem(item *store.MediaItem) error {
	*m.ids++
	item.ID = *m.ids
	cp := *item
	m.items[item.ID] = &cp
	return nil
}

func (m *memStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if v, ok := m.items[id]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, store.ErrNotFound
}

func (m *memStore) UpdateMediaItem(item *store.MediaItem) error {
	if _, ok := m.items[item.ID]; !ok {
		return store.ErrNotFound
	}
	cp := *item
	m.items[item.ID] = &cp
	return nil
}

func (m *memStore) CreateMediaMetadata(meta *store.MediaMetadata) error {
	*m.ids++
	meta.ID = *m.ids
	cp := *meta
	m.metas[meta.MediaItemID] = &cp
	return nil
}

func (m *memStore) GetMediaMetadataByMediaItem(itemID uint) (*store.MediaMetadata, error) {
	if v, ok := m.metas[itemID]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, store.ErrNotFound
}

func (m *memStore) DeleteEpisodesByMediaItem(itemID uint) error { delete(m.eps, itemID); return nil }

func (m *memStore) CreateEpisode(ep *store.Episode) error {
	*m.ids++
	ep.ID = *m.ids
	m.eps[ep.MediaItemID] = append(m.eps[ep.MediaItemID], *ep)
	return nil
}

func (m *memStore) ListEpisodesByMediaItem(itemID uint) ([]store.Episode, error) {
	return append([]store.Episode(nil), m.eps[itemID]...), nil
}

func (m *memStore) CreateSeasonMonitor(sm *store.SeasonMonitor) error {
	*m.ids++
	sm.ID = *m.ids
	m.seasons[sm.MediaItemID] = append(m.seasons[sm.MediaItemID], *sm)
	return nil
}

func (m *memStore) ListSeasonMonitorsByMediaItem(itemID uint) ([]store.SeasonMonitor, error) {
	return append([]store.SeasonMonitor(nil), m.seasons[itemID]...), nil
}

func (m *memStore) UpsertEpisodeMonitor(em *store.EpisodeMonitor) error {
	list := m.epMons[em.MediaItemID]
	for i := range list {
		if list[i].SeasonNumber == em.SeasonNumber && list[i].EpisodeNumber == em.EpisodeNumber {
			list[i].Monitored = em.Monitored
			return nil
		}
	}
	m.epMons[em.MediaItemID] = append(list, *em)
	return nil
}

// fakeTMDBTransport serves canned TMDB responses and records whether any
// request was issued while a transaction was in progress.
type fakeTMDBTransport struct {
	inTx *atomic.Bool

	mu           sync.Mutex
	total        int
	inTxRequests int
}

func (t *fakeTMDBTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.total++
	if t.inTx.Load() {
		t.inTxRequests++
	}
	t.mu.Unlock()

	var body string
	switch {
	case strings.Contains(req.URL.Path, "/season/"):
		// TVSeasonDetails — two episodes per season, both already aired.
		body = `{"season_number":1,"episodes":[
			{"episode_number":1,"season_number":1,"name":"Ep1","air_date":"2020-01-01","runtime":30},
			{"episode_number":2,"season_number":1,"name":"Ep2","air_date":"2020-01-08","runtime":30}
		]}`
	case strings.Contains(req.URL.Path, "/tv/"):
		// TVDetails — two seasons, poster deliberately omitted so no poster
		// download HTTP call is attempted.
		body = `{"name":"Test Show","overview":"An overview","first_air_date":"2020-01-01",
			"number_of_seasons":2,"status":"Returning Series","external_ids":{"imdb_id":"tt123"}}`
	default:
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// recordingRecalc records the state observed when RecalcMediaItemStatus is
// invoked, proving it runs after commit and can see the committed item.
type recordingRecalc struct {
	st   store.Store
	inTx *atomic.Bool

	called       bool
	calledInTx   bool
	itemFound    bool
	episodeCount int
}

func (r *recordingRecalc) RecalcMediaItemStatus(itemID uint) error {
	r.called = true
	r.calledInTx = r.inTx.Load()
	if _, err := r.st.GetMediaItem(itemID); err == nil {
		r.itemFound = true
	}
	eps, _ := r.st.ListEpisodesByMediaItem(itemID)
	r.episodeCount = len(eps)
	return nil
}

// TestAddMediaToLibraryFull_NoNetworkInTxAndRecalcAfterCommit is the regression
// guard for bug #20: external HTTP fetches must NOT run inside store.WithTx
// (holding the single SQLite write lock during network I/O), and the status
// recalc must run AFTER commit so it can see the committed item (previously it
// ran inside the tx against the top-level store and silently no-op'd).
func TestAddMediaToLibraryFull_NoNetworkInTxAndRecalcAfterCommit(t *testing.T) {
	st := newMemStore()

	transport := &fakeTMDBTransport{inTx: st.inTx}
	httpClient := &http.Client{Transport: transport}

	// TMDB key resolved via env fallback so no encryption/secret key is needed.
	set := settings.NewService(st, t.TempDir(), map[string]string{
		settings.KeyTMDBApiKey: "test-key",
	}, "", httpClient)

	svc := NewService(st, set, t.TempDir(), httpClient)
	recalc := &recordingRecalc{st: st, inTx: st.inTx}
	svc.SetStatusRecalculator(recalc)

	lib := &store.Library{ID: 1, Name: "TV", Path: t.TempDir(), MediaType: "series"}

	monitored := true
	item, meta, err := svc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
		Source:     "tmdb",
		ExternalID: 123,
		Monitored:  &monitored,
		SeasonMonitors: []SeasonMonitorReq{
			{SeasonNumber: 1, Monitored: true},
			{SeasonNumber: 2, Monitored: true},
		},
	})
	if err != nil {
		t.Fatalf("AddMediaToLibraryFull: %v", err)
	}

	// No network call may have happened while the transaction was open.
	if transport.inTxRequests != 0 {
		t.Errorf("expected 0 HTTP requests inside WithTx, got %d (of %d total)", transport.inTxRequests, transport.total)
	}
	// Sanity: the fetch actually happened (1 details + 2 season calls).
	if transport.total < 3 {
		t.Errorf("expected at least 3 HTTP requests (details + 2 seasons), got %d", transport.total)
	}

	// Recalc must have run, after commit, and seen the committed item + episodes.
	if !recalc.called {
		t.Fatal("expected RecalcMediaItemStatus to be called after commit")
	}
	if recalc.calledInTx {
		t.Error("RecalcMediaItemStatus ran while the transaction was open; it must run after commit")
	}
	if !recalc.itemFound {
		t.Error("RecalcMediaItemStatus could not see the committed item")
	}
	if recalc.episodeCount != 4 {
		t.Errorf("recalc saw %d episodes, want 4 (2 seasons x 2 episodes)", recalc.episodeCount)
	}

	// Item and metadata were persisted with the fetched values.
	if item == nil || item.Title != "Test Show" {
		t.Fatalf("item = %+v, want Title=Test Show", item)
	}
	if item.Status != "requested" {
		t.Errorf("item.Status = %q, want requested", item.Status)
	}
	if item.Year == nil || *item.Year != 2020 {
		t.Errorf("item.Year = %v, want 2020", item.Year)
	}
	if meta == nil || meta.MediaItemID != item.ID || meta.ImdbID != "tt123" {
		t.Fatalf("meta = %+v, want MediaItemID=%d ImdbID=tt123", meta, item.ID)
	}

	// Episodes and season monitors were committed.
	eps, _ := st.ListEpisodesByMediaItem(item.ID)
	if len(eps) != 4 {
		t.Errorf("persisted %d episodes, want 4", len(eps))
	}
	monitors, _ := st.ListSeasonMonitorsByMediaItem(item.ID)
	if len(monitors) != 2 {
		t.Errorf("persisted %d season monitors, want 2", len(monitors))
	}
}

// TestAddMediaToLibraryFull_FetchFailureAbortsBeforeWrite proves that when the
// external fetch fails, the method returns an error and writes nothing (the
// short transaction is never opened, so nothing is half-created).
func TestAddMediaToLibraryFull_FetchFailureAbortsBeforeWrite(t *testing.T) {
	st := newMemStore()

	// Transport that always fails, simulating a network/API error.
	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}

	set := settings.NewService(st, t.TempDir(), map[string]string{
		settings.KeyTMDBApiKey: "test-key",
	}, "", failing)

	svc := NewService(st, set, t.TempDir(), failing)

	_, _, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source:     "tmdb",
		ExternalID: 999,
	})
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}
	if len(st.items) != 0 || len(st.metas) != 0 {
		t.Errorf("expected no writes on fetch failure, got %d items and %d metas", len(st.items), len(st.metas))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
