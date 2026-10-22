package matching

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type requestFixture struct {
	db      *sqlite.SQLiteStore
	library *store.Library
	user    *store.User
	profile *store.MediaProfile
	item    *store.MediaItem
}

func newRequestFixture(t *testing.T, existing bool) requestFixture {
	t.Helper()
	db, err := sqlite.New(filepath.Join(t.TempDir(), "request.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f := requestFixture{
		db: db, library: &store.Library{Name: "Series", Path: t.TempDir(), MediaType: "series"},
		user:    &store.User{Email: "request@example.test", PasswordHash: "fixture"},
		profile: &store.MediaProfile{Name: "Requested profile"},
	}
	for _, create := range []func() error{
		func() error { return db.CreateLibrary(f.library) },
		func() error { return db.CreateUser(f.user) },
		func() error { return db.CreateMediaProfile(f.profile) },
	} {
		if err := create(); err != nil {
			t.Fatal(err)
		}
	}
	if existing {
		f.item = &store.MediaItem{LibraryID: f.library.ID, Title: "Show", MediaType: "series", Source: "request", Status: "requested"}
		if err := db.CreateMediaItem(f.item); err != nil {
			t.Fatal(err)
		}
		f.item.Monitored, f.item.MonitorNewSeasons = false, false
		if err := db.UpdateMediaItem(f.item); err != nil {
			t.Fatal(err)
		}
		seasons := 1
		for _, create := range []func() error{
			func() error {
				return db.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: f.item.ID, Source: "tmdb", ExternalID: 42, Title: "Show", Seasons: &seasons})
			},
			func() error {
				return db.CreateEpisode(&store.Episode{MediaItemID: f.item.ID, SeasonNumber: 1, EpisodeNumber: 1})
			},
			func() error {
				return db.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: f.item.ID, SeasonNumber: 1, Monitored: false})
			},
			func() error {
				return db.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: f.item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: false})
			},
		} {
			if err := create(); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f
}

func (f requestFixture) service(t *testing.T, st store.Store) *Service {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"name":"Show","number_of_seasons":1}`
		if strings.Contains(req.URL.Path, "/season/") {
			body = `{"episodes":[{"season_number":1,"episode_number":1,"name":"Episode"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "fixture"}, "", client)
	return NewService(st, set, t.TempDir(), client)
}

func (f requestFixture) input(withActor bool) AddMediaRequest {
	monitored, future := true, false
	req := AddMediaRequest{
		Source: "tmdb", ExternalID: 42, Monitored: &monitored, MonitorNewSeasons: &future,
		MediaProfileID: &f.profile.ID, SeasonMonitors: []SeasonMonitorReq{{SeasonNumber: 1, Monitored: true}},
	}
	if withActor {
		req.RequesterID = &f.user.ID
	}
	return req
}

func TestRequestAttributionCallersCommitRepeatedIntentAndCorrelatedActivity(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, withActor := range []bool{false, true} {
			name := "new/anonymous"
			if existing {
				name = "existing/anonymous"
			}
			if withActor {
				name = strings.ReplaceAll(name, "anonymous", "actor")
			}
			t.Run(name, func(t *testing.T) {
				f := newRequestFixture(t, existing)
				svc := f.service(t, f.db)
				recalc := &requestRecalcCounter{}
				svc.SetStatusRecalculator(recalc)
				bus := eventbus.New(16)
				var activityEvents atomic.Int32
				bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
				bus.Start()
				t.Cleanup(bus.Stop)
				svc.SetBus(bus)
				req := f.input(withActor)
				item, _, created, err := svc.AddMediaToLibraryFull(f.db, f.library, req)
				if err != nil || created == existing {
					t.Fatalf("first request: created=%v error=%v", created, err)
				}
				rows, _, err := f.db.ListMediaActivityPage(item.ID, f.user.ID, nil, 100)
				if err != nil {
					t.Fatal(err)
				}
				if withActor {
					if len(rows) != 2 || rows[0].Action != store.MediaActivityActionSettingsChanged || rows[1].Action != store.MediaActivityActionRequestMade || rows[0].OperationID != rows[1].OperationID {
						t.Fatalf("first activity = %+v", rows)
					}
					details := decodeStoredActivityDetails(t, rows[1].MediaActivity)
					if details.MediaAdded == nil || *details.MediaAdded == existing || len(details.MonitoringChanges) == 0 {
						t.Fatalf("request details = %+v", details)
					}
				} else if len(rows) != 0 {
					t.Fatalf("anonymous request fabricated activity: %+v", rows)
				}
				_, _, created, err = svc.AddMediaToLibraryFull(f.db, f.library, req)
				if err != nil || created {
					t.Fatalf("repeat request: created=%v error=%v", created, err)
				}
				bus.Stop()
				if recalc.calls != 2 {
					t.Fatalf("status recalculations = %d, want 2", recalc.calls)
				}
				requests, err := f.db.ListMediaRequestsByMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				rows, _, err = f.db.ListMediaActivityPage(item.ID, f.user.ID, nil, 100)
				if err != nil {
					t.Fatal(err)
				}
				if withActor {
					if len(requests) != 2 || len(rows) != 3 || rows[0].Action != store.MediaActivityActionRequestMade || activityEvents.Load() != 2 {
						t.Fatalf("repeat intent/history: requests=%+v rows=%+v events=%d", requests, rows, activityEvents.Load())
					}
					details := decodeStoredActivityDetails(t, rows[0].MediaActivity)
					if details.MediaAdded == nil || *details.MediaAdded || len(details.MonitoringChanges) != 0 {
						t.Fatalf("repeat details = %+v", details)
					}
				} else if len(requests) != 0 || len(rows) != 0 || activityEvents.Load() != 0 {
					t.Fatal("anonymous request recorded attribution/activity")
				}
				fresh, err := f.db.GetMediaItem(item.ID)
				if err != nil || !fresh.Monitored || fresh.MediaProfileID == nil || *fresh.MediaProfileID != f.profile.ID {
					t.Fatalf("saved settings = %+v, error=%v", fresh, err)
				}
				overrides, err := f.db.ListEpisodeMonitorsByMediaItem(item.ID)
				if err != nil || len(overrides) != 0 {
					t.Fatalf("requested season overrides = %+v, error=%v", overrides, err)
				}
			})
		}
	}
}

type requestAttributionFaultStore struct {
	store.Store
	point        string
	err          error
	requestCount int
}

type requestRecalcCounter struct{ calls int }

func (r *requestRecalcCounter) RecalcMediaItemStatus(uint) error {
	r.calls++
	return nil
}

func (s *requestAttributionFaultStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		if err := fn(&requestAttributionFaultStore{Store: tx, point: s.point, err: s.err}); err != nil {
			return err
		}
		if s.point == "commit" {
			return s.err
		}
		return nil
	})
}

func (s *requestAttributionFaultStore) CreateMediaRequest(request *store.MediaRequest) error {
	s.requestCount++
	if s.point == "second intent" && s.requestCount == 2 {
		return s.err
	}
	return s.Store.CreateMediaRequest(request)
}

func (s *requestAttributionFaultStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if s.point == "after snapshot" && s.requestCount > 0 {
		return nil, s.err
	}
	return s.Store.GetMediaItem(id)
}

func (s *requestAttributionFaultStore) AppendMediaActivity(entry *store.MediaActivity) error {
	if (s.point == "request activity" && entry.Action == store.MediaActivityActionRequestMade) ||
		(s.point == "profile activity" && entry.Action == store.MediaActivityActionSettingsChanged) {
		return s.err
	}
	return s.Store.AppendMediaActivity(entry)
}

func TestRequestAttributionCallersRollBackWithoutPublishing(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, point := range []string{"second intent", "after snapshot", "request activity", "profile activity", "commit"} {
			name := "new/" + point
			if existing {
				name = "existing/" + point
			}
			t.Run(name, func(t *testing.T) {
				f := newRequestFixture(t, existing)
				failure := errors.New("injected attribution failure")
				st := &requestAttributionFaultStore{Store: f.db, point: point, err: failure}
				svc := f.service(t, st)
				recalc := &requestRecalcCounter{}
				svc.SetStatusRecalculator(recalc)
				bus := eventbus.New(16)
				var events atomic.Int32
				for _, typ := range []eventbus.EventType{eventbus.MediaActivityAdded, eventbus.MediaRequestAdded, eventbus.MediaItemMatched} {
					bus.Subscribe(typ, func(eventbus.Event) { events.Add(1) })
				}
				bus.Start()
				t.Cleanup(bus.Stop)
				svc.SetBus(bus)
				_, _, _, err := svc.AddMediaToLibraryFull(st, f.library, f.input(true))
				if !errors.Is(err, failure) {
					t.Fatalf("request error = %v, want injected failure", err)
				}
				bus.Stop()
				if events.Load() != 0 || recalc.calls != 0 {
					t.Fatalf("rolled-back request published %d events and recalculated %d times", events.Load(), recalc.calls)
				}
				items, err := f.db.ListMediaItemsByLibrary(f.library.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !existing {
					if len(items) != 0 {
						t.Fatalf("new request survived rollback: %+v", items)
					}
					return
				}
				if len(items) != 1 || items[0].Monitored || items[0].MediaProfileID != nil || !items[0].UpdatedAt.Equal(f.item.UpdatedAt) {
					t.Fatalf("existing settings survived rollback: %+v", items)
				}
				requests, err := f.db.ListMediaRequestsByMediaItem(f.item.ID)
				if err != nil || len(requests) != 0 {
					t.Fatalf("intent survived rollback: %+v, error=%v", requests, err)
				}
				rows, _, err := f.db.ListMediaActivityPage(f.item.ID, f.user.ID, nil, 100)
				if err != nil || len(rows) != 0 {
					t.Fatalf("activity survived rollback: %+v, error=%v", rows, err)
				}
				overrides, err := f.db.ListEpisodeMonitorsByMediaItem(f.item.ID)
				if err != nil || len(overrides) != 1 || overrides[0].Monitored {
					t.Fatalf("rollback lost prior override: %+v, error=%v", overrides, err)
				}
			})
		}
	}
}
