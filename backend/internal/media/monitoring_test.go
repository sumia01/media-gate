package media_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

func saveMonitor(svc *media.Service, kind string, actor, itemID uint, enabled bool) error {
	if kind == "season" {
		_, err := svc.UpdateSeasonMonitor(actor, itemID, mediasync.SeasonMonitorInput{SeasonNumber: 1, Monitored: enabled})
		return err
	}
	return svc.UpdateEpisodeMonitor(actor, itemID, mediasync.EpisodeMonitorInput{SeasonNumber: 1, EpisodeNumber: 1, Monitored: enabled})
}

func TestMonitorServicesCommitFinalStateAndPublishAfterRecalculation(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		t.Run(kind, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			// With no wanted episodes this stale status recalculates to available;
			// enabling the missing episode must instead leave it missing.
			bus := eventbus.New(16)
			var events []eventbus.Event
			bus.SubscribeAll(func(event eventbus.Event) { events = append(events, event) })
			bus.Start()
			t.Cleanup(bus.Stop)
			syncSvc := mediasync.NewService(st)
			syncSvc.SetBus(bus)
			svc := media.NewService(st, syncSvc, bus, nil, "")
			if err := saveMonitor(svc, kind, actor, item.ID, true); err != nil {
				t.Fatal(err)
			}
			first := readUpdateState(t, st, item.ID, actor)
			if first.item.Status != "missing" || !first.item.UpdatedAt.After(item.UpdatedAt) || len(first.activities) != 1 || len(first.requests) != 2 {
				t.Fatalf("committed state = %+v, parent = %+v", first, first.item)
			}
			if kind == "season" && len(first.episodes) != 0 || kind == "episode" && (len(first.episodes) != 1 || !first.episodes[0].Monitored) {
				t.Fatalf("override state = %+v", first.episodes)
			}
			row := first.activities[0]
			if row.ActorUserID == nil || *row.ActorUserID != actor || row.OperationID == "" || row.Action != store.MediaActivityActionMonitoringChanged {
				t.Fatalf("activity = %+v", row)
			}
			var details store.MediaActivityDetails
			if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
				t.Fatal(err)
			}
			if details.Total != 1 || len(details.MonitoringChanges) != 1 || !details.MonitoringChanges[0].EffectiveAfter {
				t.Fatalf("final monitoring activity = %+v", details)
			}
			if err := saveMonitor(svc, kind, actor, item.ID, true); err != nil {
				t.Fatal(err)
			}
			bus.Stop()
			second := readUpdateState(t, st, item.ID, actor)
			if !reflect.DeepEqual(first.requests, second.requests) || !reflect.DeepEqual(first.activities, second.activities) || !second.item.UpdatedAt.After(first.item.UpdatedAt) {
				t.Fatalf("no-op changed history or failed to touch parent: first=%+v second=%+v", first.item, second.item)
			}
			if len(events) != 2 || events[0].Type != eventbus.MediaRequestAdded || events[1].Type != eventbus.MediaActivityAdded {
				t.Fatalf("events = %+v", events)
			}
		})
	}
}

func TestUpdateSeasonMonitorCreatesReusesAndClearsOnlyItsOverrides(t *testing.T) {
	st, item, actor := updateFixture(t)
	if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 2, EpisodeNumber: 1, Monitored: false}); err != nil {
		t.Fatal(err)
	}
	svc := media.NewService(st, mediasync.NewService(st), nil, nil, "")
	created, err := svc.UpdateSeasonMonitor(actor, item.ID, mediasync.SeasonMonitorInput{SeasonNumber: 2, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.MediaItemID != item.ID || created.SeasonNumber != 2 || !created.Monitored {
		t.Fatalf("created monitor = %+v", created)
	}
	updated, err := svc.UpdateSeasonMonitor(actor, item.ID, mediasync.SeasonMonitorInput{SeasonNumber: 2, Monitored: false})
	if err != nil {
		t.Fatal(err)
	}
	state := readUpdateState(t, st, item.ID, actor)
	if updated.ID != created.ID || updated.Monitored || len(state.seasons) != 2 || len(state.episodes) != 1 || state.episodes[0].SeasonNumber != 1 {
		t.Fatalf("updated=%+v seasons=%+v episodes=%+v", updated, state.seasons, state.episodes)
	}
}

func TestUpdateEpisodeMonitorCreatesWithoutChangingSiblingOverrides(t *testing.T) {
	st, item, actor := updateFixture(t)
	before := readUpdateState(t, st, item.ID, actor)
	svc := media.NewService(st, mediasync.NewService(st), nil, nil, "")
	if err := svc.UpdateEpisodeMonitor(actor, item.ID, mediasync.EpisodeMonitorInput{SeasonNumber: 1, EpisodeNumber: 2, Monitored: false}); err != nil {
		t.Fatal(err)
	}
	after := readUpdateState(t, st, item.ID, actor)
	if !reflect.DeepEqual(before.seasons, after.seasons) || len(after.episodes) != 2 || !reflect.DeepEqual(before.episodes[0], after.episodes[0]) || after.episodes[1].EpisodeNumber != 2 || after.episodes[1].Monitored {
		t.Fatalf("episode mutation changed siblings/seasons: %+v %+v", after.episodes, after.seasons)
	}
}

func TestMonitorServicesRecalculateChangedStatusEvenForNoOpSettings(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		t.Run(kind, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			svc := media.NewService(st, mediasync.NewService(st), nil, nil, "")
			if err := saveMonitor(svc, kind, actor, item.ID, false); err != nil {
				t.Fatal(err)
			}
			state := readUpdateState(t, st, item.ID, actor)
			if state.item.Status != "available" || !state.item.UpdatedAt.After(item.UpdatedAt) {
				t.Fatalf("status/freshness not recalculated: %+v", state.item)
			}
			if kind == "episode" && len(state.activities) != 0 {
				t.Fatalf("no-op episode save fabricated activity: %+v", state.activities)
			}
		})
	}
}

func (s *updateFaultStore) UpdateMediaItem(item *store.MediaItem) error {
	if s.fail == "parent" {
		return errUpdateWrite
	}
	return s.Store.UpdateMediaItem(item)
}

func (s *updateFaultStore) CreateSeasonMonitor(monitor *store.SeasonMonitor) error {
	if s.fail == "child" {
		return errUpdateWrite
	}
	return s.Store.CreateSeasonMonitor(monitor)
}

func (s *updateFaultStore) UpdateSeasonMonitor(monitor *store.SeasonMonitor) error {
	if s.fail == "child" {
		return errUpdateWrite
	}
	return s.Store.UpdateSeasonMonitor(monitor)
}

func (s *updateFaultStore) UpsertEpisodeMonitor(monitor *store.EpisodeMonitor) error {
	if s.fail == "child" {
		return errUpdateWrite
	}
	return s.Store.UpsertEpisodeMonitor(monitor)
}

func (s *updateFaultStore) DeleteEpisodeMonitorsBySeason(itemID uint, season int) error {
	if s.fail == "clear overrides" {
		return errUpdateWrite
	}
	return s.Store.DeleteEpisodeMonitorsBySeason(itemID, season)
}

func TestMonitorServicesRollBackWithoutPublishingOrRecalculating(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		for _, fault := range []string{"child", "parent", "clear overrides", "attribution", store.MediaActivityActionMonitoringChanged, "commit"} {
			if kind == "episode" && fault == "clear overrides" {
				continue
			}
			t.Run(kind+"/"+fault, func(t *testing.T) {
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
				err := saveMonitor(svc, kind, actor, item.ID, true)
				bus.Stop()
				if !errors.Is(err, errUpdateWrite) {
					t.Fatalf("error = %v, want injected write failure", err)
				}
				if after := readUpdateState(t, st, item.ID, actor); !reflect.DeepEqual(before, after) {
					t.Fatalf("failed operation changed persisted state: before=%+v after=%+v", before, after)
				}
				if len(events) != 0 {
					t.Fatalf("failed operation published: %+v", events)
				}
			})
		}
	}
}

func TestMonitorServicesValidateActorAndDeletionOnNoOps(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		for _, invalid := range []string{"zero actor", "deleted actor", "missing item", "deletion pending"} {
			t.Run(kind+"/"+invalid, func(t *testing.T) {
				st, item, actor := updateFixture(t)
				// Make the requested save a true no-op before invalidating its actor/parent.
				if kind == "season" {
					if err := st.DeleteEpisodeMonitorsBySeason(item.ID, 1); err != nil {
						t.Fatal(err)
					}
				}
				itemID := item.ID
				want := store.ErrActivityActorNotFound
				switch invalid {
				case "zero actor":
					actor = 0
				case "deleted actor":
					if err := st.DeleteUser(actor); err != nil {
						t.Fatal(err)
					}
				case "missing item":
					itemID, want = item.ID+100, store.ErrNotFound
				case "deletion pending":
					item.DeletionPending = true
					if err := st.UpdateMediaItem(item); err != nil {
						t.Fatal(err)
					}
					want = store.ErrMediaDeletionPending
				}
				before := readUpdateState(t, st, item.ID, actor)
				svc := media.NewService(st, mediasync.NewService(st), nil, nil, "")
				if err := saveMonitor(svc, kind, actor, itemID, kind == "season"); !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
				if after := readUpdateState(t, st, item.ID, actor); !reflect.DeepEqual(before, after) {
					t.Fatalf("validation changed persisted state: before=%+v after=%+v", before, after)
				}
			})
		}
	}
}

func TestMonitorServicesUseFreshParentAndKeepRecalculationBestEffort(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		t.Run(kind, func(t *testing.T) {
			st, item, actor := updateFixture(t)
			wrapper := &updateFaultStore{Store: st, beforeTx: func() {
				fresh, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh.Title, fresh.PreferredRelease, fresh.Monitored = "Renamed", "group", false
				if err := st.UpdateMediaItem(fresh); err != nil {
					t.Fatal(err)
				}
			}}
			bus := eventbus.New(16)
			var events []eventbus.Event
			bus.SubscribeAll(func(event eventbus.Event) { events = append(events, event) })
			bus.Start()
			t.Cleanup(bus.Stop)
			syncSvc := mediasync.NewService(&postCommitFaultStore{Store: st})
			syncSvc.SetBus(bus)
			svc := media.NewService(wrapper, syncSvc, bus, nil, "")
			if err := saveMonitor(svc, kind, actor, item.ID, true); err != nil {
				t.Fatal(err)
			}
			bus.Stop()
			state := readUpdateState(t, st, item.ID, actor)
			if state.item.Title != "Renamed" || state.item.PreferredRelease != "group" || state.item.Monitored || state.item.Status != "missing" || len(state.requests) != 0 || len(state.activities) != 1 {
				t.Fatalf("fresh parent/best-effort/parent gating violated: state=%+v parent=%+v", state, state.item)
			}
			if len(events) != 1 || events[0].Type != eventbus.MediaActivityAdded {
				t.Fatalf("committed child configuration lost its activity hint: %+v", events)
			}
		})
	}
}
