package matching

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
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

	ids      *uint
	inTx     *atomic.Bool
	items    map[uint]*store.MediaItem
	metas    map[uint]*store.MediaMetadata
	eps      map[uint][]store.Episode
	seasons  map[uint][]store.SeasonMonitor
	epMons   map[uint][]store.EpisodeMonitor
	requests map[uint][]store.MediaRequest
	users    map[uint]*store.User
	profiles map[uint]*store.MediaProfile
	activity map[uint][]store.MediaActivity
	beforeTx func(*memStore)

	appendErr         error
	appendFailAt      int
	appendCalls       int
	deleteEpisodesErr error
	createEpisodeErr  error
}

func newMemStore() *memStore {
	var id uint
	return &memStore{
		ids:      &id,
		inTx:     &atomic.Bool{},
		items:    map[uint]*store.MediaItem{},
		metas:    map[uint]*store.MediaMetadata{},
		eps:      map[uint][]store.Episode{},
		seasons:  map[uint][]store.SeasonMonitor{},
		epMons:   map[uint][]store.EpisodeMonitor{},
		requests: map[uint][]store.MediaRequest{},
		users:    map[uint]*store.User{},
		profiles: map[uint]*store.MediaProfile{},
		activity: map[uint][]store.MediaActivity{},
	}
}

func (m *memStore) clone() *memStore {
	c := &memStore{
		ids: m.ids, inTx: m.inTx,
		appendErr: m.appendErr, appendFailAt: m.appendFailAt, appendCalls: m.appendCalls,
		deleteEpisodesErr: m.deleteEpisodesErr, createEpisodeErr: m.createEpisodeErr,
		items:    map[uint]*store.MediaItem{},
		metas:    map[uint]*store.MediaMetadata{},
		eps:      map[uint][]store.Episode{},
		seasons:  map[uint][]store.SeasonMonitor{},
		epMons:   map[uint][]store.EpisodeMonitor{},
		requests: map[uint][]store.MediaRequest{},
		users:    map[uint]*store.User{},
		profiles: map[uint]*store.MediaProfile{},
		activity: map[uint][]store.MediaActivity{},
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
	for k, v := range m.requests {
		c.requests[k] = append([]store.MediaRequest(nil), v...)
	}
	for k, v := range m.users {
		cp := *v
		c.users[k] = &cp
	}
	for k, v := range m.profiles {
		cp := *v
		c.profiles[k] = &cp
	}
	for k, v := range m.activity {
		c.activity[k] = append([]store.MediaActivity(nil), v...)
	}
	return c
}

func (m *memStore) WithTx(fn func(store.Store) error) error {
	if m.beforeTx != nil {
		hook := m.beforeTx
		m.beforeTx = nil
		hook(m)
	}
	m.inTx.Store(true)
	defer m.inTx.Store(false)
	shadow := m.clone()
	if err := fn(shadow); err != nil {
		return err // discard shadow; nothing committed
	}
	// Commit: adopt the shadow's state atomically.
	m.items, m.metas, m.eps, m.seasons, m.epMons, m.requests = shadow.items, shadow.metas, shadow.eps, shadow.seasons, shadow.epMons, shadow.requests
	m.users, m.profiles, m.activity = shadow.users, shadow.profiles, shadow.activity
	m.appendCalls = shadow.appendCalls
	return nil
}

func (m *memStore) Close() error                              { return nil }
func (m *memStore) GetSetting(string) (*store.Setting, error) { return nil, store.ErrNotFound }

func (m *memStore) GetMediaItemByExternalID(libraryID uint, source string, externalID int) (*store.MediaItem, error) {
	for _, meta := range m.metas {
		if meta.Source == source && meta.ExternalID == externalID {
			if item, ok := m.items[meta.MediaItemID]; ok && item.LibraryID == libraryID {
				copy := *item
				return &copy, nil
			}
		}
	}
	return nil, store.ErrNotFound
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

func (m *memStore) UpdateMediaMetadata(meta *store.MediaMetadata) error {
	if _, ok := m.metas[meta.MediaItemID]; !ok {
		return store.ErrNotFound
	}
	cp := *meta
	m.metas[meta.MediaItemID] = &cp
	return nil
}

func (m *memStore) DeleteMediaMetadataByMediaItem(itemID uint) error {
	delete(m.metas, itemID)
	return nil
}

func (m *memStore) DeleteEpisodesByMediaItem(itemID uint) error {
	if m.deleteEpisodesErr != nil {
		return m.deleteEpisodesErr
	}
	delete(m.eps, itemID)
	return nil
}

func (m *memStore) CreateEpisode(ep *store.Episode) error {
	if m.createEpisodeErr != nil {
		return m.createEpisodeErr
	}
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

func (m *memStore) ListEpisodeMonitorsByMediaItem(itemID uint) ([]store.EpisodeMonitor, error) {
	return append([]store.EpisodeMonitor(nil), m.epMons[itemID]...), nil
}

func (m *memStore) UpdateSeasonMonitor(monitor *store.SeasonMonitor) error {
	list := m.seasons[monitor.MediaItemID]
	for i := range list {
		if list[i].SeasonNumber == monitor.SeasonNumber {
			list[i] = *monitor
			m.seasons[monitor.MediaItemID] = list
			return nil
		}
	}
	return store.ErrNotFound
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

func (m *memStore) DeleteEpisodeMonitorsBySeason(mediaItemID uint, seasonNumber int) error {
	monitors := m.epMons[mediaItemID]
	kept := monitors[:0]
	for _, monitor := range monitors {
		if monitor.SeasonNumber != seasonNumber {
			kept = append(kept, monitor)
		}
	}
	m.epMons[mediaItemID] = kept
	return nil
}

func (m *memStore) DeleteEpisodeMonitorsByMediaItem(mediaItemID uint) error {
	delete(m.epMons, mediaItemID)
	return nil
}

func (m *memStore) CreateMediaRequest(request *store.MediaRequest) error {
	if request.UserID == nil {
		return store.ErrRequesterNotFound
	}
	if _, ok := m.users[*request.UserID]; !ok {
		return store.ErrRequesterNotFound
	}
	for _, existing := range m.requests[request.MediaItemID] {
		if existing.UserID != nil && request.UserID != nil && *existing.UserID == *request.UserID &&
			existing.Scope == request.Scope && equalIntPtr(existing.SeasonNumber, request.SeasonNumber) &&
			equalIntPtr(existing.EpisodeNumber, request.EpisodeNumber) {
			return nil
		}
	}
	*m.ids++
	request.ID = *m.ids
	m.requests[request.MediaItemID] = append(m.requests[request.MediaItemID], *request)
	return nil
}

func (m *memStore) GetUser(id uint) (*store.User, error) {
	if user, ok := m.users[id]; ok {
		cp := *user
		return &cp, nil
	}
	return nil, store.ErrNotFound
}

func (m *memStore) CreateMediaProfile(profile *store.MediaProfile) error {
	*m.ids++
	profile.ID = *m.ids
	cp := *profile
	m.profiles[profile.ID] = &cp
	return nil
}

func (m *memStore) GetMediaProfile(id uint) (*store.MediaProfile, error) {
	if profile, ok := m.profiles[id]; ok {
		cp := *profile
		return &cp, nil
	}
	return nil, store.ErrNotFound
}

func (m *memStore) AppendMediaActivity(activity *store.MediaActivity) error {
	m.appendCalls++
	if m.appendErr != nil && (m.appendFailAt == 0 || m.appendCalls == m.appendFailAt) {
		return m.appendErr
	}
	if err := store.ValidateMediaActivity(activity); err != nil {
		return err
	}
	if _, err := m.GetMediaItem(activity.MediaItemID); err != nil {
		return err
	}
	if activity.ActorKind == store.MediaActivityActorUser {
		if _, err := m.GetUser(*activity.ActorUserID); err != nil {
			return store.ErrActivityActorNotFound
		}
	}
	*m.ids++
	activity.ID = *m.ids
	m.activity[activity.MediaItemID] = append(m.activity[activity.MediaItemID], *activity)
	return nil
}

func (m *memStore) addUser(id uint) {
	m.users[id] = &store.User{ID: id, Email: "user@example.com"}
}

func equalIntPtr(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
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
	requesterID := uint(7)
	st.addUser(requesterID)
	item, meta, created, err := svc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
		Source:      "tmdb",
		ExternalID:  123,
		RequesterID: &requesterID,
		Monitored:   &monitored,
		SeasonMonitors: []SeasonMonitorReq{
			{SeasonNumber: 1, Monitored: true},
			{SeasonNumber: 2, Monitored: true},
		},
	})
	if err != nil {
		t.Fatalf("AddMediaToLibraryFull: %v", err)
	}
	if !created {
		t.Fatal("new media reported as existing")
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
	requests := st.requests[item.ID]
	if len(requests) != 3 || !hasRequestScope(requests, requesterID, store.MediaRequestScopeMedia, 0, 0) ||
		!hasRequestScope(requests, requesterID, store.MediaRequestScopeWholeSeries, 0, 0) ||
		!hasRequestScope(requests, requesterID, store.MediaRequestScopeFutureSeasons, 0, 0) {
		t.Errorf("persisted requests = %+v, want title, whole-series, and future-season attribution", requests)
	}

	secondRequesterID := uint(8)
	st.addUser(secondRequesterID)
	monitorFuture := false
	requestsBefore := transport.total
	st.seasons[item.ID][1].Monitored = false
	existing, _, created, err := svc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
		Source:            "tmdb",
		ExternalID:        123,
		RequesterID:       &secondRequesterID,
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors: []SeasonMonitorReq{
			{SeasonNumber: 1, Monitored: false},
			{SeasonNumber: 2, Monitored: true},
		},
	})
	if err != nil {
		t.Fatalf("second AddMediaToLibraryFull: %v", err)
	}
	if created {
		t.Fatal("existing media reported as newly created")
	}
	if existing.ID != item.ID {
		t.Errorf("existing item ID = %d, want %d", existing.ID, item.ID)
	}
	if transport.total != requestsBefore {
		t.Errorf("duplicate request made %d metadata HTTP calls, want 0", transport.total-requestsBefore)
	}
	requests = st.requests[item.ID]
	if len(requests) != 5 || !hasRequestScope(requests, secondRequesterID, store.MediaRequestScopeMedia, 0, 0) ||
		!hasRequestScope(requests, secondRequesterID, store.MediaRequestScopeSeason, 2, 0) {
		t.Errorf("requests after second requester = %+v, want title plus season 2", requests)
	}
	if !st.seasons[item.ID][1].Monitored {
		t.Error("requesting season 2 did not enable its existing season monitor")
	}

	st.seasons[item.ID][0].Monitored = false
	st.seasons[item.ID][1].Monitored = false
	thirdRequesterID := uint(9)
	st.addUser(thirdRequesterID)
	_, _, created, err = svc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
		Source:      "tmdb",
		ExternalID:  123,
		RequesterID: &thirdRequesterID,
		Monitored:   &monitored,
	})
	if err != nil {
		t.Fatalf("request with omitted season monitors: %v", err)
	}
	if created {
		t.Fatal("existing media with defaulted seasons reported as newly created")
	}
	for _, season := range st.seasons[item.ID] {
		if !season.Monitored {
			t.Errorf("defaulted whole-series request did not enable season %d", season.SeasonNumber)
		}
	}
}

func hasRequestScope(requests []store.MediaRequest, userID uint, scope string, seasonNumber, episodeNumber int) bool {
	for _, request := range requests {
		if request.UserID == nil || *request.UserID != userID || request.Scope != scope {
			continue
		}
		if seasonNumber != 0 && (request.SeasonNumber == nil || *request.SeasonNumber != seasonNumber) {
			continue
		}
		if episodeNumber != 0 && (request.EpisodeNumber == nil || *request.EpisodeNumber != episodeNumber) {
			continue
		}
		return true
	}
	return false
}

func TestAddMediaToLibraryFullRecordsNewRepeatAndReenabledRequestActivity(t *testing.T) {
	st := newMemStore()
	requesterID := uint(21)
	st.addUser(requesterID)
	transport := &fakeTMDBTransport{inTx: st.inTx}
	httpClient := &http.Client{Transport: transport}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", httpClient)
	svc := NewService(st, set, t.TempDir(), httpClient)
	bus := eventbus.New(16)
	var activityEvents atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
	bus.Start()
	defer bus.Stop()
	svc.SetBus(bus)
	lib := &store.Library{ID: 1, Name: "TV", Path: t.TempDir(), MediaType: "series"}
	monitored := true
	req := AddMediaRequest{
		Source:      "tmdb",
		ExternalID:  321,
		RequesterID: &requesterID,
		Monitored:   &monitored,
		SeasonMonitors: []SeasonMonitorReq{
			{SeasonNumber: 1, Monitored: true},
			{SeasonNumber: 2, Monitored: true},
		},
	}

	item, _, created, err := svc.AddMediaToLibraryFull(st, lib, req)
	if err != nil || !created {
		t.Fatalf("new request: created=%v err=%v", created, err)
	}
	activities := st.activity[item.ID]
	if len(activities) != 1 {
		t.Fatalf("new request activities = %d, want 1", len(activities))
	}
	if activities[0].ActorUserID == nil || *activities[0].ActorUserID != requesterID {
		t.Fatalf("new request actor = %v, want user %d", activities[0].ActorUserID, requesterID)
	}
	newDetails := decodeActivityDetails(t, activities[0])
	if newDetails.MediaAdded == nil || !*newDetails.MediaAdded {
		t.Fatalf("new request MediaAdded = %v, want true", newDetails.MediaAdded)
	}
	if !hasActivityTarget(newDetails, store.MediaActivityScopeMedia, 0, 0) ||
		!hasActivityTarget(newDetails, store.MediaActivityScopeWholeSeries, 0, 0) ||
		!hasActivityTarget(newDetails, store.MediaActivityScopeFutureSeasons, 0, 0) {
		t.Fatalf("new request targets = %+v / %+v", newDetails.Target, newDetails.Targets)
	}
	if len(newDetails.MonitoringChanges) == 0 {
		t.Fatal("new request did not record its monitoring effects")
	}
	requestCount := len(st.requests[item.ID])

	_, _, created, err = svc.AddMediaToLibraryFull(st, lib, req)
	if err != nil || created {
		t.Fatalf("repeat request: created=%v err=%v", created, err)
	}
	activities = st.activity[item.ID]
	if len(activities) != 2 {
		t.Fatalf("activities after repeat = %d, want 2", len(activities))
	}
	repeatDetails := decodeActivityDetails(t, activities[1])
	if repeatDetails.MediaAdded == nil || *repeatDetails.MediaAdded {
		t.Fatalf("repeat request MediaAdded = %v, want false", repeatDetails.MediaAdded)
	}
	if len(repeatDetails.MonitoringChanges) != 0 {
		t.Fatalf("repeat monitoring changes = %+v, want none", repeatDetails.MonitoringChanges)
	}
	if len(st.requests[item.ID]) != requestCount {
		t.Fatalf("repeat request rows = %d, want deduplicated count %d", len(st.requests[item.ID]), requestCount)
	}

	for i := range st.seasons[item.ID] {
		if st.seasons[item.ID][i].SeasonNumber == 2 {
			st.seasons[item.ID][i].Monitored = false
		}
	}
	_, _, _, err = svc.AddMediaToLibraryFull(st, lib, req)
	if err != nil {
		t.Fatalf("re-enable request: %v", err)
	}
	activities = st.activity[item.ID]
	if len(activities) != 3 {
		t.Fatalf("activities after re-enable = %d, want 3", len(activities))
	}
	reenabledDetails := decodeActivityDetails(t, activities[2])
	if !hasEffectiveMonitoringChange(reenabledDetails, store.MediaActivityScopeSeason, 2, 0, false, true) {
		t.Fatalf("re-enable monitoring changes = %+v, want season 2 false-to-true", reenabledDetails.MonitoringChanges)
	}
	if len(st.requests[item.ID]) != requestCount {
		t.Fatalf("re-enable request rows = %d, want deduplicated count %d", len(st.requests[item.ID]), requestCount)
	}
	bus.Stop()
	if activityEvents.Load() != 3 {
		t.Fatalf("media.activity_added events = %d, want one per committed request", activityEvents.Load())
	}
}

func TestAddMediaToLibraryFullRecordsProfileAssignmentWithRequestOperation(t *testing.T) {
	st := newMemStore()
	requesterID := uint(31)
	st.addUser(requesterID)
	profile := &store.MediaProfile{Name: strings.Repeat("profile", 100)}
	if err := st.CreateMediaProfile(profile); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTMDBTransport{inTx: st.inTx}
	client := &http.Client{Transport: transport}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
	svc := NewService(st, set, t.TempDir(), client)
	lib := &store.Library{ID: 1, MediaType: "series"}

	item, _, created, err := svc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
		Source: "tmdb", ExternalID: 901, RequesterID: &requesterID, MediaProfileID: &profile.ID,
	})
	if err != nil || !created {
		t.Fatalf("new profiled request: created=%v err=%v", created, err)
	}
	rows := st.activity[item.ID]
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionRequestMade || rows[1].Action != store.MediaActivityActionSettingsChanged || rows[0].OperationID != rows[1].OperationID {
		t.Fatalf("profile request activities = %+v", rows)
	}
	details := decodeStoredActivityDetails(t, rows[1])
	if len(details.FieldChanges) != 1 || details.FieldChanges[0].Field != "media_profile" || details.FieldChanges[0].Before != nil || details.FieldChanges[0].After == nil {
		t.Fatalf("profile details = %+v", details)
	}
	label := *details.FieldChanges[0].After
	if len(label) > store.MediaActivityMaxTitleBytes || !strings.HasSuffix(label, fmt.Sprintf(" (ID %d)", profile.ID)) {
		t.Fatalf("profile label = %q (%d bytes)", label, len(label))
	}

	secondProfile := &store.MediaProfile{Name: "Ignored profile"}
	if err := st.CreateMediaProfile(secondProfile); err != nil {
		t.Fatal(err)
	}
	_, _, created, err = svc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
		Source: "tmdb", ExternalID: 901, RequesterID: &requesterID, MediaProfileID: &secondProfile.ID,
	})
	if err != nil || created {
		t.Fatalf("repeat profiled request: created=%v err=%v", created, err)
	}
	rows = st.activity[item.ID]
	if len(rows) != 3 || rows[2].Action != store.MediaActivityActionRequestMade {
		t.Fatalf("already-profiled request activities = %+v", rows)
	}
	fresh, _ := st.GetMediaItem(item.ID)
	if fresh.MediaProfileID == nil || *fresh.MediaProfileID != profile.ID {
		t.Fatalf("existing profile was replaced: %+v", fresh.MediaProfileID)
	}
}

func TestAddMediaToLibraryFullExistingRequestAssignsEmptyProfile(t *testing.T) {
	st := newMemStore()
	requesterID := uint(32)
	st.addUser(requesterID)
	profile := &store.MediaProfile{Name: "Existing profile"}
	if err := st.CreateMediaProfile(profile); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: 1, Title: "Existing", MediaType: "movie", Status: "requested", Source: "request"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 902, Title: item.Title}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, nil, t.TempDir(), http.DefaultClient)
	result, _, created, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "movie"}, AddMediaRequest{
		Source: "tmdb", ExternalID: 902, RequesterID: &requesterID, MediaProfileID: &profile.ID,
	})
	if err != nil || created {
		t.Fatalf("existing profiled request: created=%v err=%v", created, err)
	}
	if result.MediaProfileID == nil || *result.MediaProfileID != profile.ID {
		t.Fatalf("assigned profile = %v", result.MediaProfileID)
	}
	rows := st.activity[item.ID]
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionRequestMade || rows[1].Action != store.MediaActivityActionSettingsChanged || rows[0].OperationID != rows[1].OperationID {
		t.Fatalf("existing profile activities = %+v", rows)
	}
}

func TestAddMediaToLibraryFullRollsBackWhenProfileActivityAppendFails(t *testing.T) {
	st := newMemStore()
	requesterID := uint(33)
	st.addUser(requesterID)
	profile := &store.MediaProfile{Name: "Rollback profile"}
	if err := st.CreateMediaProfile(profile); err != nil {
		t.Fatal(err)
	}
	st.appendErr = errors.New("profile activity append failed")
	st.appendFailAt = 2
	transport := &fakeTMDBTransport{inTx: st.inTx}
	client := &http.Client{Transport: transport}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)

	_, _, _, err := NewService(st, set, t.TempDir(), client).AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source: "tmdb", ExternalID: 903, RequesterID: &requesterID, MediaProfileID: &profile.ID,
	})
	if !errors.Is(err, st.appendErr) {
		t.Fatalf("error = %v, want profile append failure", err)
	}
	if len(st.items) != 0 || len(st.metas) != 0 || len(st.requests) != 0 || len(st.activity) != 0 {
		t.Fatalf("profile append failure committed state: items=%d metas=%d requests=%d activities=%d", len(st.items), len(st.metas), len(st.requests), len(st.activity))
	}
}

func TestAddMediaToLibraryFullRechecksIdentityAfterFetch(t *testing.T) {
	st := newMemStore()
	firstRequester, secondRequester := uint(34), uint(35)
	st.addUser(firstRequester)
	st.addUser(secondRequester)
	transport := &fakeTMDBTransport{inTx: st.inTx}
	client := &http.Client{Transport: transport}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
	firstSvc := NewService(st, set, t.TempDir(), client)
	secondSvc := NewService(st, set, t.TempDir(), client)
	lib := &store.Library{ID: 1, MediaType: "series"}
	req := AddMediaRequest{Source: "tmdb", ExternalID: 904, RequesterID: &firstRequester}

	var winner *store.MediaItem
	var winnerCreated bool
	var winnerErr error
	st.beforeTx = func(*memStore) {
		winner, _, winnerCreated, winnerErr = secondSvc.AddMediaToLibraryFull(st, lib, AddMediaRequest{
			Source: "tmdb", ExternalID: 904, RequesterID: &secondRequester,
		})
	}
	result, _, created, err := firstSvc.AddMediaToLibraryFull(st, lib, req)
	if winnerErr != nil || !winnerCreated {
		t.Fatalf("winning request: created=%v err=%v", winnerCreated, winnerErr)
	}
	if err != nil || created {
		t.Fatalf("racing request: created=%v err=%v", created, err)
	}
	if len(st.items) != 1 || result.ID != winner.ID {
		t.Fatalf("race created duplicate items: result=%+v winner=%+v items=%d", result, winner, len(st.items))
	}
	requests := st.requests[winner.ID]
	if !hasRequestScope(requests, firstRequester, store.MediaRequestScopeMedia, 0, 0) || !hasRequestScope(requests, secondRequester, store.MediaRequestScopeMedia, 0, 0) {
		t.Fatalf("race requests = %+v", requests)
	}
	rows := st.activity[winner.ID]
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionRequestMade || rows[1].Action != store.MediaActivityActionRequestMade {
		t.Fatalf("race activities = %+v", rows)
	}
}

func TestAddMediaToLibraryFullRecordsNaturalEpisodeActivityTarget(t *testing.T) {
	st := newMemStore()
	requesterID := uint(26)
	st.addUser(requesterID)
	item := &store.MediaItem{LibraryID: 1, Title: "Episode", MediaType: "series", Status: "requested", Source: "request"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	seasonCount := 1
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 432, Seasons: &seasonCount}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 2}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: 1, Monitored: false}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, nil, t.TempDir(), http.DefaultClient)
	monitored, monitorFuture := true, false
	_, _, _, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source:            "tmdb",
		ExternalID:        432,
		RequesterID:       &requesterID,
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: false}},
		EpisodeMonitors:   []EpisodeMonitorReq{{SeasonNumber: 1, EpisodeNumber: 2, Monitored: true}},
	})
	if err != nil {
		t.Fatalf("episode request: %v", err)
	}
	details := decodeActivityDetails(t, st.activity[item.ID][0])
	if !hasActivityTarget(details, store.MediaActivityScopeEpisode, 1, 2) {
		t.Fatalf("episode request targets = %+v, want natural S01E02", details.Targets)
	}
	if !hasEffectiveMonitoringChange(details, store.MediaActivityScopeEpisode, 1, 2, false, true) {
		t.Fatalf("episode monitoring changes = %+v, want S01E02 false-to-true", details.MonitoringChanges)
	}
}

func TestAddMediaToLibraryFullUsesTransactionalIncompleteCatalogForActivity(t *testing.T) {
	st := newMemStore()
	requesterID := uint(22)
	st.addUser(requesterID)
	item := &store.MediaItem{LibraryID: 1, Title: "Incomplete", MediaType: "series", Status: "requested", Source: "request"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	seasonCount := 2
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 654, Seasons: &seasonCount}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1}); err != nil {
		t.Fatal(err)
	}
	for season := 1; season <= seasonCount; season++ {
		if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: season, Monitored: false}); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(st, nil, t.TempDir(), http.DefaultClient)
	monitored, monitorFuture := true, true
	_, _, created, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source:            "tmdb",
		ExternalID:        654,
		RequesterID:       &requesterID,
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: true}},
	})
	if err != nil || created {
		t.Fatalf("existing incomplete-catalog request: created=%v err=%v", created, err)
	}
	details := decodeActivityDetails(t, st.activity[item.ID][0])
	if !hasActivityTarget(details, store.MediaActivityScopeWholeSeries, 0, 0) ||
		hasActivityTarget(details, store.MediaActivityScopeSeason, 1, 0) {
		t.Fatalf("normalized targets = %+v, want whole-series truth from metadata season count", details.Targets)
	}
	if !hasEffectiveMonitoringChange(details, store.MediaActivityScopeSeason, 2, 0, false, true) {
		t.Fatalf("monitoring changes = %+v, want missing-catalog season 2 effect", details.MonitoringChanges)
	}
}

func TestAddMediaToLibraryFullRollsBackWhenRequestActivityAppendFails(t *testing.T) {
	st := newMemStore()
	requesterID := uint(23)
	st.addUser(requesterID)
	appendErr := errors.New("append failed")
	st.appendErr = appendErr
	transport := &fakeTMDBTransport{inTx: st.inTx}
	httpClient := &http.Client{Transport: transport}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", httpClient)
	svc := NewService(st, set, t.TempDir(), httpClient)
	monitored := true

	_, _, _, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source:      "tmdb",
		ExternalID:  777,
		RequesterID: &requesterID,
		Monitored:   &monitored,
	})
	if !errors.Is(err, appendErr) {
		t.Fatalf("AddMediaToLibraryFull error = %v, want append failure", err)
	}
	if len(st.items) != 0 || len(st.metas) != 0 || len(st.requests) != 0 || len(st.activity) != 0 {
		t.Fatalf("append failure committed state: items=%d metas=%d requests=%d activities=%d", len(st.items), len(st.metas), len(st.requests), len(st.activity))
	}
}

func TestAddMediaToLibraryFullRejectedScopeDoesNotRecordActivity(t *testing.T) {
	st := newMemStore()
	requesterID := uint(24)
	st.addUser(requesterID)
	item := &store.MediaItem{LibraryID: 1, Title: "Rejected", MediaType: "series", Status: "requested", Source: "request"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	seasonCount := 1
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 888, Seasons: &seasonCount}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, nil, t.TempDir(), http.DefaultClient)
	monitored, monitorFuture := true, false

	_, _, _, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source:            "tmdb",
		ExternalID:        888,
		RequesterID:       &requesterID,
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: false}},
	})
	if !errors.Is(err, ErrNoRequestedScope) {
		t.Fatalf("error = %v, want ErrNoRequestedScope", err)
	}
	if len(st.activity[item.ID]) != 0 || len(st.requests[item.ID]) != 0 {
		t.Fatalf("rejected request wrote requests=%d activities=%d", len(st.requests[item.ID]), len(st.activity[item.ID]))
	}
}

func decodeActivityDetails(t *testing.T, activity store.MediaActivity) store.MediaActivityDetails {
	t.Helper()
	if activity.Action != store.MediaActivityActionRequestMade {
		t.Fatalf("activity action = %q, want request.made", activity.Action)
	}
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(activity.Details), &details); err != nil {
		t.Fatalf("decoding activity details: %v", err)
	}
	return details
}

func hasActivityTarget(details store.MediaActivityDetails, scope string, seasonNumber, episodeNumber int) bool {
	targets := details.Targets
	if details.Target != nil {
		targets = append(targets, *details.Target)
	}
	for _, target := range targets {
		if target.Scope != scope || (seasonNumber != 0 && (target.SeasonNumber == nil || *target.SeasonNumber != seasonNumber)) ||
			(episodeNumber != 0 && (target.EpisodeNumber == nil || *target.EpisodeNumber != episodeNumber)) {
			continue
		}
		return true
	}
	return false
}

func hasEffectiveMonitoringChange(details store.MediaActivityDetails, scope string, seasonNumber, episodeNumber int, before, after bool) bool {
	for _, change := range details.MonitoringChanges {
		if change.Target.Scope != scope || change.EffectiveBefore != before || change.EffectiveAfter != after ||
			(seasonNumber != 0 && (change.Target.SeasonNumber == nil || *change.Target.SeasonNumber != seasonNumber)) ||
			(episodeNumber != 0 && (change.Target.EpisodeNumber == nil || *change.Target.EpisodeNumber != episodeNumber)) {
			continue
		}
		return true
	}
	return false
}

func TestBuildRequestScopesRejectsEmptyAndDoesNotPromotePartialInput(t *testing.T) {
	monitored := true
	monitorFuture := false
	episodes := []episodeData{
		{seasonNumber: 1, episodeNumber: 1},
		{seasonNumber: 2, episodeNumber: 1},
	}

	empty := buildRequestScopes("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors: []SeasonMonitorReq{
			{SeasonNumber: 1, Monitored: false},
			{SeasonNumber: 2, Monitored: false},
		},
	}, episodes, nil)
	if hasRequestedContent("series", AddMediaRequest{Monitored: &monitored}, empty) {
		t.Errorf("all-disabled scopes = %+v, want title attribution without monitored content", empty)
	}

	partial := buildRequestScopes("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 2, Monitored: true}},
	}, episodes, nil)
	if len(partial) != 2 || partial[0].scope != store.MediaRequestScopeMedia || partial[1].scope != store.MediaRequestScopeSeason || partial[1].seasonNumber != 2 {
		t.Errorf("partial scopes = %+v, want title plus season 2", partial)
	}
}

func TestNormalizeSeriesRequestDefaultsKnownSeasonsAndFutureMonitoring(t *testing.T) {
	monitored := true
	seasonCount := 2
	req := normalizeSeriesRequest("series", AddMediaRequest{Monitored: &monitored}, []episodeData{
		{seasonNumber: 2, episodeNumber: 1},
		{seasonNumber: 1, episodeNumber: 1},
		{seasonNumber: 2, episodeNumber: 2},
	}, &seasonCount)

	if req.MonitorNewSeasons == nil || !*req.MonitorNewSeasons {
		t.Error("MonitorNewSeasons was not defaulted to true")
	}
	if len(req.SeasonMonitors) != 2 || req.SeasonMonitors[0].SeasonNumber != 1 || req.SeasonMonitors[1].SeasonNumber != 2 {
		t.Errorf("SeasonMonitors = %+v, want known seasons 1 and 2", req.SeasonMonitors)
	}
}

func TestNormalizeSeriesRequestFillsSeasonSkippedByProviderFetch(t *testing.T) {
	monitored := true
	monitorFuture := true
	seasonCount := 2
	req := normalizeSeriesRequest("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: true}},
	}, []episodeData{{seasonNumber: 1, episodeNumber: 1}}, &seasonCount)

	if len(req.SeasonMonitors) != 2 || req.SeasonMonitors[0].SeasonNumber != 1 || req.SeasonMonitors[1].SeasonNumber != 2 {
		t.Fatalf("SeasonMonitors = %+v, want metadata seasons 1 and 2", req.SeasonMonitors)
	}
	for _, season := range req.SeasonMonitors {
		if !season.Monitored {
			t.Errorf("season %d was not enabled for whole-series request", season.SeasonNumber)
		}
	}
}

func TestBuildRequestScopesDoesNotCompactIncompleteMetadata(t *testing.T) {
	monitored := true
	monitorFuture := false
	seasonCount := 2
	scopes := buildRequestScopes("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: true}},
	}, []episodeData{{seasonNumber: 1, episodeNumber: 1}}, &seasonCount)

	if len(scopes) != 2 || scopes[0].scope != store.MediaRequestScopeMedia ||
		scopes[1].scope != store.MediaRequestScopeSeason || scopes[1].seasonNumber != 1 {
		t.Fatalf("scopes = %+v, want title plus season 1", scopes)
	}
}

func TestBuildRequestScopesDoesNotCompactUnfetchedEpisodeExclusion(t *testing.T) {
	monitored := true
	monitorFuture := false
	seasonCount := 1
	scopes := buildRequestScopes("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: true}},
		EpisodeMonitors:   []EpisodeMonitorReq{{SeasonNumber: 1, EpisodeNumber: 2, Monitored: false}},
	}, []episodeData{{seasonNumber: 1, episodeNumber: 1}}, &seasonCount)

	if len(scopes) != 2 || scopes[0].scope != store.MediaRequestScopeMedia ||
		scopes[1].scope != store.MediaRequestScopeEpisode || scopes[1].episodeNumber != 1 {
		t.Fatalf("scopes = %+v, want title plus episode 1", scopes)
	}
}

func TestBuildRequestScopesDoesNotEnableEmptySeasonWithExclusion(t *testing.T) {
	monitored := true
	monitorFuture := false
	seasonCount := 1
	scopes := buildRequestScopes("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors:    []SeasonMonitorReq{{SeasonNumber: 1, Monitored: true}},
		EpisodeMonitors:   []EpisodeMonitorReq{{SeasonNumber: 1, EpisodeNumber: 1, Monitored: false}},
	}, nil, &seasonCount)

	if hasRequestedContent("series", AddMediaRequest{Monitored: &monitored}, scopes) {
		t.Fatalf("scopes = %+v, want no monitored content attribution", scopes)
	}
}

func TestBuildRequestScopesUsesEpisodeScopeForPartialSeasons(t *testing.T) {
	monitored := true
	monitorFuture := false
	scopes := buildRequestScopes("series", AddMediaRequest{
		Monitored:         &monitored,
		MonitorNewSeasons: &monitorFuture,
		SeasonMonitors: []SeasonMonitorReq{
			{SeasonNumber: 1, Monitored: true},
			{SeasonNumber: 2, Monitored: false},
		},
		EpisodeMonitors: []EpisodeMonitorReq{
			{SeasonNumber: 1, EpisodeNumber: 2, Monitored: false},
			{SeasonNumber: 2, EpisodeNumber: 1, Monitored: true},
		},
	}, []episodeData{
		{seasonNumber: 1, episodeNumber: 1},
		{seasonNumber: 1, episodeNumber: 2},
		{seasonNumber: 2, episodeNumber: 1},
	}, nil)

	want := []requestScope{
		{scope: store.MediaRequestScopeMedia},
		{scope: store.MediaRequestScopeEpisode, seasonNumber: 1, episodeNumber: 1},
		{scope: store.MediaRequestScopeEpisode, seasonNumber: 2, episodeNumber: 1},
	}
	if len(scopes) != len(want) {
		t.Fatalf("scopes = %+v, want %+v", scopes, want)
	}
	for i := range want {
		if scopes[i] != want[i] {
			t.Errorf("scope %d = %+v, want %+v", i, scopes[i], want[i])
		}
	}
}

// TestAddMediaToLibraryFull_FetchFailureAbortsBeforeWrite proves that when the
// external fetch fails, the method returns an error and writes nothing (the
// short transaction is never opened, so nothing is half-created).
func TestAddMediaToLibraryFull_FetchFailureAbortsBeforeWrite(t *testing.T) {
	st := newMemStore()
	requesterID := uint(25)
	st.addUser(requesterID)

	// Transport that always fails, simulating a network/API error.
	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}

	set := settings.NewService(st, t.TempDir(), map[string]string{
		settings.KeyTMDBApiKey: "test-key",
	}, "", failing)

	svc := NewService(st, set, t.TempDir(), failing)

	_, _, _, err := svc.AddMediaToLibraryFull(st, &store.Library{ID: 1, MediaType: "series"}, AddMediaRequest{
		Source:      "tmdb",
		ExternalID:  999,
		RequesterID: &requesterID,
	})
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}
	if len(st.items) != 0 || len(st.metas) != 0 || len(st.activity) != 0 {
		t.Errorf("expected no writes on fetch failure, got %d items, %d metas, and %d activity feeds", len(st.items), len(st.metas), len(st.activity))
	}
}

func TestPersistMatchPropagatesEpisodeCatalogWriteErrors(t *testing.T) {
	deleteErr := errors.New("delete episodes")
	createErr := errors.New("create episode")
	for _, tt := range []struct {
		name      string
		configure func(*memStore)
		want      error
	}{
		{name: "delete", configure: func(st *memStore) { st.deleteEpisodesErr = deleteErr }, want: deleteErr},
		{name: "create", configure: func(st *memStore) { st.createEpisodeErr = createErr }, want: createErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMemStore()
			item := &store.MediaItem{LibraryID: 1, Title: "Catalog", MediaType: "series", Source: "request"}
			if err := st.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			tt.configure(st)
			seasonCount := 1
			err := NewService(st, nil, t.TempDir(), http.DefaultClient).persistMatch(item, &store.MediaMetadata{Seasons: &seasonCount}, []episodeData{{seasonNumber: 1, episodeNumber: 1}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("persistMatch error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestMatchActivityUsesManualAndAutomaticActors(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "manual"
		if automatic {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			st := newMemStore()
			item := &store.MediaItem{LibraryID: 1, Title: "Test Show", MediaType: "series", Status: "new", Source: "disk"}
			if err := st.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			client := matchingTMDBClient(1, 0)
			set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
			svc := NewService(st, set, t.TempDir(), client)
			if automatic {
				if err := svc.matchSingleItem(item.ID, "tmdb", "test-key", "series", false); err != nil {
					t.Fatal(err)
				}
			} else {
				st.addUser(41)
				if _, _, err := svc.ManualMatch(item.ID, "tmdb", 123, 41); err != nil {
					t.Fatal(err)
				}
			}

			rows := st.activity[item.ID]
			if len(rows) != 1 || rows[0].Action != store.MediaActivityActionMatchChanged {
				t.Fatalf("match activity = %+v", rows)
			}
			if automatic {
				if rows[0].ActorKind != store.MediaActivityActorSystem || rows[0].ActorComponent != "matching" || rows[0].ActorUserID != nil {
					t.Fatalf("automatic actor = %+v", rows[0])
				}
			} else if rows[0].ActorKind != store.MediaActivityActorUser || rows[0].ActorUserID == nil || *rows[0].ActorUserID != 41 {
				t.Fatalf("manual actor = %+v", rows[0])
			}
			details := decodeStoredActivityDetails(t, rows[0])
			if details.OldProvider != nil || details.NewProvider == nil || details.NewProvider.ExternalID != 123 || details.Added != 1 {
				t.Fatalf("match details = %+v", details)
			}
		})
	}
}

func TestAutomaticFullRematchSuppressesSemanticNoop(t *testing.T) {
	st := newMemStore()
	year, seasons := 2020, 1
	item := &store.MediaItem{LibraryID: 1, Title: "Test Show", MediaType: "series", Status: "available", Source: "disk", UpdatedAt: time.Unix(10, 0)}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	meta := &store.MediaMetadata{
		MediaItemID: item.ID, Source: "tmdb", ExternalID: 123, Title: "Test Show", Year: &year,
		Status: "Returning Series", Seasons: &seasons, UpdatedAt: time.Unix(20, 0),
	}
	if err := st.CreateMediaMetadata(meta); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Title: "existing"}); err != nil {
		t.Fatal(err)
	}
	oldMetaID := meta.ID
	oldEpisodeID := st.eps[item.ID][0].ID
	client := matchingTMDBClient(1, 0)
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
	svc := NewService(st, set, t.TempDir(), client)
	bus := eventbus.New(4)
	var activityEvents atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
	bus.Start()
	svc.SetBus(bus)

	if err := svc.matchSingleItem(item.ID, "tmdb", "test-key", "series", true); err != nil {
		t.Fatal(err)
	}
	bus.Stop()
	freshMeta, _ := st.GetMediaMetadataByMediaItem(item.ID)
	freshEpisodes, _ := st.ListEpisodesByMediaItem(item.ID)
	if freshMeta.ID != oldMetaID || len(freshEpisodes) != 1 || freshEpisodes[0].ID != oldEpisodeID {
		t.Fatalf("semantic no-op replaced catalog: meta=%+v episodes=%+v", freshMeta, freshEpisodes)
	}
	if len(st.activity[item.ID]) != 0 || activityEvents.Load() != 0 {
		t.Fatalf("semantic no-op emitted activity: rows=%+v events=%d", st.activity[item.ID], activityEvents.Load())
	}
}

func TestMatchPublishesActivityBeforePosterIOCompletes(t *testing.T) {
	st, item := matchedMemFixture(t, 43)
	posterStarted := make(chan struct{})
	releasePoster := make(chan struct{})
	var startOnce sync.Once
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case req.URL.Host == "image.tmdb.org":
			startOnce.Do(func() { close(posterStarted) })
			<-releasePoster
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(jpeg(2048)))), Header: make(http.Header)}, nil
		case strings.Contains(req.URL.Path, "/season/"):
			body = `{"season_number":1,"episodes":[{"episode_number":1,"season_number":1,"name":"Episode","air_date":"2020-01-01","runtime":30}]}`
		case strings.Contains(req.URL.Path, "/tv/"):
			body = `{"name":"Test Show","first_air_date":"2020-01-01","number_of_seasons":1,"status":"Returning Series","poster_path":"/poster.jpg"}`
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
	svc := NewService(st, set, t.TempDir(), client)
	bus := eventbus.New(4)
	activityPublished := make(chan struct{}, 1)
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityPublished <- struct{}{} })
	bus.Start()
	defer bus.Stop()
	svc.SetBus(bus)
	done := make(chan error, 1)
	go func() {
		_, _, err := svc.ManualMatch(item.ID, "tmdb", 123, 43)
		done <- err
	}()

	select {
	case <-posterStarted:
	case <-time.After(time.Second):
		t.Fatal("poster download did not start")
	}
	select {
	case <-activityPublished:
	case <-time.After(time.Second):
		close(releasePoster)
		t.Fatal("activity publication was blocked behind poster I/O")
	}
	close(releasePoster)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMatchRejectsIncompleteAndStaleCandidatesWithoutReplacingCatalog(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "incomplete season window"
		if stale {
			name = "stale metadata version"
		}
		t.Run(name, func(t *testing.T) {
			st := newMemStore()
			st.addUser(42)
			item := &store.MediaItem{LibraryID: 1, Title: "Old Show", MediaType: "series", Status: "available", Source: "disk", UpdatedAt: time.Unix(10, 0)}
			if err := st.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			seasonCount := 1
			oldMeta := &store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 99, Title: "Old Provider", Seasons: &seasonCount, UpdatedAt: time.Unix(20, 0)}
			if err := st.CreateMediaMetadata(oldMeta); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 9, Title: "Old episode"}); err != nil {
				t.Fatal(err)
			}
			prior, err := store.NewSystemMediaActivity(item, "fixture", store.MediaActivityActionMetadataChanged, "prior", store.MediaActivityDetails{Added: 1, Total: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.AppendMediaActivity(prior); err != nil {
				t.Fatal(err)
			}

			failSeason := 2
			if stale {
				failSeason = 0
				st.beforeTx = func(store *memStore) {
					store.metas[item.ID].UpdatedAt = store.metas[item.ID].UpdatedAt.Add(time.Second)
				}
			}
			client := matchingTMDBClient(2, failSeason)
			set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
			svc := NewService(st, set, t.TempDir(), client)
			_, _, err = svc.ManualMatch(item.ID, "tmdb", 123, 42)
			if stale && !errors.Is(err, ErrStaleMatchCandidate) {
				t.Fatalf("error = %v, want stale candidate", err)
			}
			if !stale && err == nil {
				t.Fatal("incomplete candidate was accepted")
			}
			meta, _ := st.GetMediaMetadataByMediaItem(item.ID)
			episodes, _ := st.ListEpisodesByMediaItem(item.ID)
			if meta.ExternalID != 99 || len(episodes) != 1 || episodes[0].EpisodeNumber != 9 || len(st.activity[item.ID]) != 1 {
				t.Fatalf("old match was not retained: meta=%+v episodes=%+v activity=%+v", meta, episodes, st.activity[item.ID])
			}
		})
	}
}

func TestRequestAndMatchRejectDeletionPending(t *testing.T) {
	t.Run("existing request", func(t *testing.T) {
		st, item := matchedMemFixture(t, 61)
		item.DeletionPending = true
		if err := st.UpdateMediaItem(item); err != nil {
			t.Fatal(err)
		}
		monitored := true
		requesterID := uint(61)
		_, _, _, err := NewService(st, nil, t.TempDir(), http.DefaultClient).AddMediaToLibraryFull(
			st,
			&store.Library{ID: 1, MediaType: "series"},
			AddMediaRequest{Source: "tmdb", ExternalID: 99, RequesterID: &requesterID, Monitored: &monitored},
		)
		if !errors.Is(err, store.ErrMediaDeletionPending) {
			t.Fatalf("request error = %v, want ErrMediaDeletionPending", err)
		}
		current, _ := st.GetMediaItem(item.ID)
		if current.Monitored || len(st.requests[item.ID]) != 0 || len(st.activity[item.ID]) != 0 {
			t.Fatalf("rejected request changed item: current=%+v requests=%+v activity=%+v", current, st.requests[item.ID], st.activity[item.ID])
		}
	})

	t.Run("match final apply", func(t *testing.T) {
		st, item := matchedMemFixture(t, 62)
		st.beforeTx = func(current *memStore) {
			claimed := *current.items[item.ID]
			claimed.DeletionPending = true
			claimed.UpdatedAt = claimed.UpdatedAt.Add(time.Second)
			current.items[item.ID] = &claimed
		}
		client := matchingTMDBClient(1, 0)
		set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
		_, _, err := NewService(st, set, t.TempDir(), client).ManualMatch(item.ID, "tmdb", 123, 62)
		if !errors.Is(err, store.ErrMediaDeletionPending) {
			t.Fatalf("match error = %v, want ErrMediaDeletionPending", err)
		}
		assertOldMatchIntact(t, st, item.ID)
	})
}

func TestMatchAndUnmatchRollbackWithActivityAndEpisodeFailures(t *testing.T) {
	t.Run("match episode write", func(t *testing.T) {
		st, item := matchedMemFixture(t, 51)
		st.createEpisodeErr = errors.New("episode write failed")
		client := matchingTMDBClient(1, 0)
		set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
		_, _, err := NewService(st, set, t.TempDir(), client).ManualMatch(item.ID, "tmdb", 123, 51)
		if !errors.Is(err, st.createEpisodeErr) {
			t.Fatalf("error = %v", err)
		}
		assertOldMatchIntact(t, st, item.ID)
	})

	t.Run("match activity append", func(t *testing.T) {
		st, item := matchedMemFixture(t, 52)
		st.appendErr = errors.New("activity write failed")
		client := matchingTMDBClient(1, 0)
		set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
		_, _, err := NewService(st, set, t.TempDir(), client).ManualMatch(item.ID, "tmdb", 123, 52)
		if !errors.Is(err, st.appendErr) {
			t.Fatalf("error = %v", err)
		}
		assertOldMatchIntact(t, st, item.ID)
	})

	t.Run("unmatch activity append", func(t *testing.T) {
		st, item := matchedMemFixture(t, 53)
		st.appendErr = errors.New("activity write failed")
		err := NewService(st, nil, t.TempDir(), http.DefaultClient).Unmatch(item.ID, 53)
		if !errors.Is(err, st.appendErr) {
			t.Fatalf("error = %v", err)
		}
		assertOldMatchIntact(t, st, item.ID)
	})

	t.Run("unmatch episode delete", func(t *testing.T) {
		st, item := matchedMemFixture(t, 55)
		st.deleteEpisodesErr = errors.New("episode delete failed")
		err := NewService(st, nil, t.TempDir(), http.DefaultClient).Unmatch(item.ID, 55)
		if !errors.Is(err, st.deleteEpisodesErr) {
			t.Fatalf("error = %v", err)
		}
		assertOldMatchIntact(t, st, item.ID)
	})
}

func TestSuccessfulRematchPreservesPriorActivity(t *testing.T) {
	st, item := matchedMemFixture(t, 56)
	prior, err := store.NewSystemMediaActivity(item, "fixture", store.MediaActivityActionMetadataChanged, "prior", store.MediaActivityDetails{Added: 1, Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMediaActivity(prior); err != nil {
		t.Fatal(err)
	}
	client := matchingTMDBClient(1, 0)
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
	if _, _, err := NewService(st, set, t.TempDir(), client).ManualMatch(item.ID, "tmdb", 123, 56); err != nil {
		t.Fatal(err)
	}
	rows := st.activity[item.ID]
	if len(rows) != 2 || rows[0].OperationID != "prior" || rows[1].Action != store.MediaActivityActionMatchChanged {
		t.Fatalf("rematch activity history = %+v", rows)
	}
}

func TestUnmatchRecordsEffectsKeepsSeasonMonitorsAndPriorActivity(t *testing.T) {
	st, item := matchedMemFixture(t, 54)
	prior, err := store.NewSystemMediaActivity(item, "fixture", store.MediaActivityActionMetadataChanged, "prior", store.MediaActivityDetails{Added: 1, Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMediaActivity(prior); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 9, Monitored: false}); err != nil {
		t.Fatal(err)
	}

	if err := NewService(st, nil, t.TempDir(), http.DefaultClient).Unmatch(item.ID, 54); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMediaMetadataByMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("metadata error = %v", err)
	}
	episodes, _ := st.ListEpisodesByMediaItem(item.ID)
	overrides, _ := st.ListEpisodeMonitorsByMediaItem(item.ID)
	seasons, _ := st.ListSeasonMonitorsByMediaItem(item.ID)
	if len(episodes) != 0 || len(overrides) != 0 || len(seasons) != 1 {
		t.Fatalf("unmatch effects: episodes=%+v overrides=%+v seasons=%+v", episodes, overrides, seasons)
	}
	rows := st.activity[item.ID]
	if len(rows) != 2 || rows[0].OperationID != "prior" || rows[1].Action != store.MediaActivityActionUnmatched {
		t.Fatalf("activity history = %+v", rows)
	}
	details := decodeStoredActivityDetails(t, rows[1])
	if details.OldProvider == nil || details.OldProvider.ExternalID != 99 || details.Removed != 1 || details.DatabaseRecordsRemoved != 3 || len(details.MonitoringChanges) == 0 {
		t.Fatalf("unmatch details = %+v", details)
	}
}

func matchedMemFixture(t *testing.T, userID uint) (*memStore, *store.MediaItem) {
	t.Helper()
	st := newMemStore()
	st.addUser(userID)
	item := &store.MediaItem{LibraryID: 1, Title: "Old Show", MediaType: "series", Status: "available", Source: "disk"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	seasons := 1
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 99, Title: "Old Provider", Seasons: &seasons}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 9, Title: "Old episode"}); err != nil {
		t.Fatal(err)
	}
	return st, item
}

func assertOldMatchIntact(t *testing.T, st *memStore, itemID uint) {
	t.Helper()
	meta, err := st.GetMediaMetadataByMediaItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	episodes, err := st.ListEpisodesByMediaItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ExternalID != 99 || len(episodes) != 1 || episodes[0].EpisodeNumber != 9 || len(st.activity[itemID]) != 0 {
		t.Fatalf("rolled-back match = meta %+v episodes %+v activity %+v", meta, episodes, st.activity[itemID])
	}
}

func decodeStoredActivityDetails(t *testing.T, activity store.MediaActivity) store.MediaActivityDetails {
	t.Helper()
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(activity.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details
}

func matchingTMDBClient(seasonCount, failSeason int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := "{}"
		switch {
		case strings.Contains(req.URL.Path, "/search/tv"):
			body = `{"results":[{"id":123,"name":"Test Show","first_air_date":"2020-01-01"}]}`
		case strings.Contains(req.URL.Path, "/season/"):
			seasonText := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
			season, _ := strconv.Atoi(seasonText)
			if season == failSeason {
				status = http.StatusBadGateway
			} else {
				body = fmt.Sprintf(`{"season_number":%d,"episodes":[{"episode_number":1,"season_number":%d,"name":"S%d episode","air_date":"2020-01-01","runtime":30}]}`, season, season, season)
			}
		case strings.Contains(req.URL.Path, "/tv/"):
			body = fmt.Sprintf(`{"name":"Test Show","first_air_date":"2020-01-01","number_of_seasons":%d,"status":"Returning Series"}`, seasonCount)
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
