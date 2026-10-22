package media_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

func updateFixture(t *testing.T) (store.Store, *store.MediaItem, uint) {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.New(filepath.Join(dir, "update.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	library := &store.Library{Name: "Series", Path: dir, MediaType: "series"}
	if err := st.CreateLibrary(library); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-2 * time.Hour)
	item := &store.MediaItem{
		LibraryID: library.ID, Title: "Show", MediaType: "series", Status: "missing",
		Monitored: true, CreatedAt: old, UpdatedAt: old,
	}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	// Creation defaults future-season monitoring on; this fixture needs an
	// explicit disabled baseline to exercise false-to-true attribution.
	item.MonitorNewSeasons = false
	if err := st.UpdateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	seasons := 1
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 1, Seasons: &seasons}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2000-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: false}); err != nil {
		t.Fatal(err)
	}
	actor := &store.User{Email: "actor@example.test", PasswordHash: "hash"}
	if err := st.CreateUser(actor); err != nil {
		t.Fatal(err)
	}
	return st, item, actor.ID
}

type updateState struct {
	item       *store.MediaItem
	seasons    []store.SeasonMonitor
	episodes   []store.EpisodeMonitor
	requests   []store.MediaRequestAttribution
	keywords   []string
	activities []store.MediaActivityAttribution
}

func readUpdateState(t *testing.T, st store.Store, itemID, userID uint) updateState {
	t.Helper()
	var state updateState
	var err error
	if state.item, err = st.GetMediaItem(itemID); err != nil {
		t.Fatal(err)
	}
	if state.seasons, err = st.ListSeasonMonitorsByMediaItem(itemID); err != nil {
		t.Fatal(err)
	}
	if state.episodes, err = st.ListEpisodeMonitorsByMediaItem(itemID); err != nil {
		t.Fatal(err)
	}
	if state.requests, err = st.ListMediaRequestsByMediaItem(itemID); err != nil {
		t.Fatal(err)
	}
	if state.keywords, err = st.ListReleaseKeywords(userID, "group"); err != nil {
		t.Fatal(err)
	}
	var more bool
	if state.activities, more, err = st.ListMediaActivityPage(itemID, userID, nil, 100); err != nil || more {
		t.Fatalf("activity page: more=%v err=%v", more, err)
	}
	return state
}

func TestUpdateMediaItemCommitsCombinedMutationAndPublishesInOrder(t *testing.T) {
	st, item, actor := updateFixture(t)
	profile := &store.MediaProfile{Name: "HD", Resolutions: `[]`, Languages: `[]`}
	if err := st.CreateMediaProfile(profile); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New(16)
	var events []eventbus.Event
	bus.SubscribeAll(func(event eventbus.Event) { events = append(events, event) })
	bus.Start()
	t.Cleanup(bus.Stop)
	syncSvc := mediasync.NewService(st)
	syncSvc.SetBus(bus)
	svc := media.NewService(st, syncSvc, bus, nil, "")
	preferred, force := " Group, GROUP, Other ", false
	input := media.UpdateInput{
		MediaProfileID: &profile.ID, PreferredRelease: &preferred, ForceRelease: &force,
		SeasonMonitors:  []mediasync.SeasonMonitorInput{{SeasonNumber: 1, Monitored: true}},
		EpisodeMonitors: []mediasync.EpisodeMonitorInput{{SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}},
	}
	updated, err := svc.UpdateMediaItem(actor, item.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	bus.Stop()
	state := readUpdateState(t, st, item.ID, actor)
	if !reflect.DeepEqual(updated, state.item) || updated.MediaProfileID == nil || *updated.MediaProfileID != profile.ID || updated.PreferredRelease != "group, other" || updated.ForceRelease || !updated.UpdatedAt.After(item.UpdatedAt) {
		t.Fatalf("updated item = %+v, persisted = %+v", updated, state.item)
	}
	if len(state.episodes) != 1 || !state.episodes[0].Monitored {
		t.Fatalf("episode override must be applied after season reset: %+v", state.episodes)
	}
	if !reflect.DeepEqual(state.keywords, []string{"group"}) || len(state.requests) != 2 {
		t.Fatalf("keywords=%v requests=%+v", state.keywords, state.requests)
	}
	for _, request := range state.requests {
		if request.Scope != store.MediaRequestScopeMedia && request.Scope != store.MediaRequestScopeEpisode {
			t.Fatalf("request attributed intermediate season state: %+v", request)
		}
	}
	if len(state.activities) != 2 || state.activities[0].Action != store.MediaActivityActionSettingsChanged || state.activities[1].Action != store.MediaActivityActionMonitoringChanged || state.activities[0].OperationID == "" || state.activities[0].OperationID != state.activities[1].OperationID {
		t.Fatalf("activities = %+v", state.activities)
	}
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(state.activities[0].Details), &details); err != nil {
		t.Fatal(err)
	}
	if details.Total != 3 || len(details.FieldChanges) != 3 {
		t.Fatalf("settings activity = %+v", details)
	}
	if len(events) != 2 || events[0].Type != eventbus.MediaRequestAdded || events[1].Type != eventbus.MediaActivityAdded {
		t.Fatalf("events = %+v", events)
	}
}

var errUpdateWrite = errors.New("update write failed")

type updateFaultStore struct {
	store.Store
	fail     string
	beforeTx func()
}

func (s *updateFaultStore) WithTx(fn func(store.Store) error) error {
	if s.beforeTx != nil {
		s.beforeTx()
	}
	return s.Store.WithTx(func(tx store.Store) error {
		if err := fn(&updateFaultStore{Store: tx, fail: s.fail}); err != nil {
			return err
		}
		if s.fail == "commit" {
			return errUpdateWrite
		}
		return nil
	})
}

func (s *updateFaultStore) RememberReleaseKeywords(userID uint, keywords []string) error {
	if s.fail == "keywords" {
		return errUpdateWrite
	}
	return s.Store.RememberReleaseKeywords(userID, keywords)
}

func (s *updateFaultStore) CreateMediaRequest(request *store.MediaRequest) error {
	if s.fail == "attribution" {
		return errUpdateWrite
	}
	return s.Store.CreateMediaRequest(request)
}

func (s *updateFaultStore) AppendMediaActivity(entry *store.MediaActivity) error {
	if s.fail == entry.Action {
		return errUpdateWrite
	}
	return s.Store.AppendMediaActivity(entry)
}

func TestUpdateMediaItemRollsBackAllWritesAndSkipsPostCommitEffects(t *testing.T) {
	for _, fault := range []string{"keywords", "attribution", store.MediaActivityActionMonitoringChanged, store.MediaActivityActionSettingsChanged, "commit"} {
		t.Run(fault, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			before := readUpdateState(t, st, item.ID, actor)
			bus := eventbus.New(16)
			var events []eventbus.Event
			bus.SubscribeAll(func(event eventbus.Event) { events = append(events, event) })
			bus.Start()
			t.Cleanup(bus.Stop)
			syncSvc := mediasync.NewService(st)
			syncSvc.SetBus(bus)
			svc := media.NewService(&updateFaultStore{Store: st, fail: fault}, syncSvc, bus, nil, "")
			preferred := "GROUP"
			updated, err := svc.UpdateMediaItem(actor, item.ID, media.UpdateInput{
				PreferredRelease: &preferred,
				SeasonMonitors:   []mediasync.SeasonMonitorInput{{SeasonNumber: 1, Monitored: true}},
			})
			bus.Stop()
			if !errors.Is(err, errUpdateWrite) || updated != nil {
				t.Fatalf("updated=%+v err=%v", updated, err)
			}
			if after := readUpdateState(t, st, item.ID, actor); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed operation changed persisted state: before=%+v after=%+v", before, after)
			}
			if len(events) != 0 {
				t.Fatalf("failed operation published events: %+v", events)
			}
		})
	}
}

func TestUpdateMediaItemValidatesActorProfileAndDeletionForNoOps(t *testing.T) {
	for _, kind := range []string{"zero actor", "deleted actor", "missing item", "missing profile", "deletion pending", "no-op"} {
		t.Run(kind, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			itemID := item.ID
			input := media.UpdateInput{}
			var want error
			switch kind {
			case "zero actor":
				actor, want = 0, store.ErrActivityActorNotFound
			case "deleted actor":
				if err := st.DeleteUser(actor); err != nil {
					t.Fatal(err)
				}
				want = store.ErrActivityActorNotFound
			case "missing item":
				itemID, want = item.ID+100, store.ErrNotFound
			case "missing profile":
				profileID := uint(999)
				input.MediaProfileID, want = &profileID, media.ErrMediaProfileNotFound
			case "deletion pending":
				item.DeletionPending = true
				if err := st.UpdateMediaItem(item); err != nil {
					t.Fatal(err)
				}
				want = store.ErrMediaDeletionPending
			}
			svc := media.NewService(st, mediasync.NewService(st), nil, nil, "")
			_, err := svc.UpdateMediaItem(actor, itemID, input)
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			state := readUpdateState(t, st, item.ID, actor)
			if len(state.activities) != 0 || len(state.requests) != 0 || len(state.episodes) != 1 || state.episodes[0].Monitored {
				t.Fatalf("validation/no-op changed state: %+v", state)
			}
		})
	}
}

func TestUpdateMediaItemOmittedAndEmptyMonitorLists(t *testing.T) {
	for _, kind := range []string{"omitted", "empty seasons", "empty episodes"} {
		t.Run(kind, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			input := media.UpdateInput{}
			wantStatus := "missing"
			if kind == "empty seasons" {
				input.SeasonMonitors = []mediasync.SeasonMonitorInput{}
				wantStatus = "available"
			}
			if kind == "empty episodes" {
				input.EpisodeMonitors = []mediasync.EpisodeMonitorInput{}
				wantStatus = "available"
			}
			svc := media.NewService(st, mediasync.NewService(st), nil, nil, "")
			updated, err := svc.UpdateMediaItem(actor, item.ID, input)
			if err != nil || updated.Status != wantStatus {
				t.Fatalf("updated=%+v err=%v, want status %s", updated, err, wantStatus)
			}
			state := readUpdateState(t, st, item.ID, actor)
			if len(state.activities) != 0 || len(state.requests) != 0 || len(state.seasons) != 1 || !state.seasons[0].Monitored || len(state.episodes) != 1 || state.episodes[0].Monitored {
				t.Fatalf("empty/omitted inputs changed monitoring: %+v", state)
			}
		})
	}
}

func TestUpdateMediaItemUsesFreshParentAndDisableClearsOverrides(t *testing.T) {
	st, item, actor := updateFixture(t)
	wrapper := &updateFaultStore{Store: st, beforeTx: func() {
		fresh, err := st.GetMediaItem(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		fresh.Title, fresh.PreferredRelease = "Renamed", "group"
		if err := st.UpdateMediaItem(fresh); err != nil {
			t.Fatal(err)
		}
	}}
	svc := media.NewService(wrapper, mediasync.NewService(st), nil, nil, "")
	monitored := false
	updated, err := svc.UpdateMediaItem(actor, item.ID, media.UpdateInput{Monitored: &monitored})
	if err != nil {
		t.Fatal(err)
	}
	state := readUpdateState(t, st, item.ID, actor)
	if updated.Title != "Renamed" || updated.PreferredRelease != "group" || updated.Monitored || len(state.episodes) != 0 || len(state.activities) != 1 {
		t.Fatalf("updated=%+v state=%+v", updated, state)
	}
}

type postCommitFaultStore struct {
	store.Store
	failRead bool
}

func (s *postCommitFaultStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if s.failRead {
		return nil, errUpdateWrite
	}
	return s.Store.GetMediaItem(id)
}

func (s *postCommitFaultStore) ListMediaFilesByMediaItem(uint) ([]store.MediaFile, error) {
	return nil, errUpdateWrite
}

func TestUpdateMediaItemPostCommitFailuresRemainBestEffort(t *testing.T) {
	for _, kind := range []string{"status recalculation", "response reload"} {
		t.Run(kind, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			bus := eventbus.New(16)
			var events []eventbus.Event
			bus.SubscribeAll(func(event eventbus.Event) { events = append(events, event) })
			bus.Start()
			t.Cleanup(bus.Stop)
			mutationStore, recalcStore := st, st
			if kind == "status recalculation" {
				recalcStore = &postCommitFaultStore{Store: st}
			} else {
				mutationStore = &postCommitFaultStore{Store: st, failRead: true}
			}
			syncSvc := mediasync.NewService(recalcStore)
			syncSvc.SetBus(bus)
			svc := media.NewService(mutationStore, syncSvc, bus, nil, "")
			future := true
			updated, err := svc.UpdateMediaItem(actor, item.ID, media.UpdateInput{MonitorNewSeasons: &future})
			bus.Stop()
			if err != nil || !updated.MonitorNewSeasons || updated.Status != "missing" {
				t.Fatalf("committed mutation reported as failed: updated=%+v err=%v", updated, err)
			}
			state := readUpdateState(t, st, item.ID, actor)
			wantStatus := "missing"
			if kind == "response reload" {
				wantStatus = "available"
			}
			if !state.item.MonitorNewSeasons || state.item.Status != wantStatus || len(state.activities) != 1 || len(state.requests) != 2 || len(events) != 2 {
				t.Fatalf("post-commit failure suppressed persisted effects: state=%+v events=%+v", state, events)
			}
		})
	}
}
