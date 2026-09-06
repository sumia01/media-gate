package apiv1

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

func monitorToggleFixture(t *testing.T) (*Handlers, store.Store, *store.MediaItem) {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.New(filepath.Join(dir, "monitor-toggle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	lib := &store.Library{Name: "Series", Path: dir, MediaType: "series"}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-2 * time.Hour)
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series", Monitored: true, Status: "available", CreatedAt: old, UpdatedAt: old}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 1, Title: item.Title}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	sn, en := 1, 1
	if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: sn, EpisodeNumber: en, AirDate: "2000-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "/show/ep1.mkv", FileName: "ep1.mkv", SeasonNumber: &sn, EpisodeNumber: &en}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMonitorDecision(&store.MonitorDecision{MediaItemID: item.ID, CheckedAt: old.Add(time.Hour), Outcome: "already_present", Summary: "Already present."}); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{store: st, syncSvc: mediasync.NewService(st), settings: settings.NewService(st, dir, nil, "test-key", http.DefaultClient)}
	return h, st, item
}

func TestMonitorToggleStaleSnapshotSurvivesRefetch(t *testing.T) {
	for _, kind := range []string{"season update", "season create", "episode update", "episode create"} {
		t.Run(kind, func(t *testing.T) {
			h, st, item := monitorToggleFixture(t)
			if kind == "season update" || kind == "episode update" {
				if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := h.GetMediaItem(context.Background(), GetMediaItemRequestObject{Id: int64(item.ID)})
			if err != nil {
				t.Fatal(err)
			}
			previous, err := st.GetMonitorDecision(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if before.(GetMediaItem200JSONResponse).UpdatedAt.After(previous.CheckedAt) {
				t.Fatal("fixture already stale before toggle")
			}
			if kind == "season update" || kind == "season create" {
				season, enabled := 1, false
				if kind == "season create" {
					season, enabled = 2, true
				}
				response, err := h.UpdateSeasonMonitor(context.Background(), UpdateSeasonMonitorRequestObject{
					Id: int64(item.ID), SeasonNumber: season, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: enabled},
				})
				if err != nil {
					t.Fatal(err)
				}
				if got, ok := response.(UpdateSeasonMonitor200JSONResponse); !ok || got.Monitored != enabled || got.Id == 0 {
					t.Fatalf("response = %+v", response)
				}
				if overrides, err := st.ListEpisodeMonitorsByMediaItem(item.ID); err != nil || len(overrides) != 0 {
					t.Fatalf("season did not clear overrides: %+v, %v", overrides, err)
				}
			} else {
				response, err := h.UpdateEpisodeMonitor(context.Background(), UpdateEpisodeMonitorRequestObject{
					Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: false},
				})
				if err != nil {
					t.Fatal(err)
				}
				if got, ok := response.(UpdateEpisodeMonitor200JSONResponse); !ok || got.Monitored {
					t.Fatalf("response = %+v", response)
				}
				overrides, err := st.ListEpisodeMonitorsByMediaItem(item.ID)
				if err != nil || len(overrides) != 1 || overrides[0].Monitored {
					t.Fatalf("episode not disabled: %+v, %v", overrides, err)
				}
			}

			// Fresh reads use only persisted data, as after a browser reload with
			// settingsChangedAt reset. Recalc is a no-op because status is unchanged.
			reloaded := &Handlers{store: st, settings: h.settings}
			mediaResponse, err := reloaded.GetMediaItem(context.Background(), GetMediaItemRequestObject{Id: int64(item.ID)})
			if err != nil {
				t.Fatal(err)
			}
			decisionResponse, err := reloaded.GetMonitorDecision(context.Background(), GetMonitorDecisionRequestObject{Id: int64(item.ID)})
			if err != nil {
				t.Fatal(err)
			}
			media := mediaResponse.(GetMediaItem200JSONResponse)
			decision := decisionResponse.(GetMonitorDecision200JSONResponse).Decision
			if media.Status != "available" || decision == nil || !decision.CheckedAt.Equal(previous.CheckedAt) {
				t.Fatalf("status/snapshot changed: %+v, %+v", media, decision)
			}
			if media.UpdatedAt.UnixMilli() <= decision.CheckedAt.UnixMilli() {
				t.Fatalf("persisted timestamps do not imply stale: item=%v check=%v", media.UpdatedAt, decision.CheckedAt)
			}
		})
	}
}

var monitorWriteError = errors.New("monitor write failed")

type monitorTransactionStore struct {
	store.Store
	fail     string
	beforeTx func()
}

func (s *monitorTransactionStore) WithTx(fn func(store.Store) error) error {
	if s.beforeTx != nil {
		s.beforeTx()
	}
	return s.Store.WithTx(func(tx store.Store) error { return fn(&monitorTransactionStore{Store: tx, fail: s.fail}) })
}

func (s *monitorTransactionStore) UpdateMediaItem(item *store.MediaItem) error {
	if s.fail == "parent" {
		return monitorWriteError
	}
	return s.Store.UpdateMediaItem(item)
}

func (s *monitorTransactionStore) DeleteEpisodeMonitorsBySeason(id uint, season int) error {
	if s.fail == "clear overrides" {
		return monitorWriteError
	}
	return s.Store.DeleteEpisodeMonitorsBySeason(id, season)
}

func TestMonitorToggleRollsBackWithoutParentTimestamp(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		for _, fault := range []string{"parent", "clear overrides"} {
			if kind == "episode" && fault == "clear overrides" {
				continue
			}
			t.Run(kind+"/"+fault, func(t *testing.T) {
				h, st, item := monitorToggleFixture(t)
				if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
					t.Fatal(err)
				}
				beforeItem, _ := st.GetMediaItem(item.ID)
				beforeSeasons, _ := st.ListSeasonMonitorsByMediaItem(item.ID)
				beforeEpisodes, _ := st.ListEpisodeMonitorsByMediaItem(item.ID)
				h.store = &monitorTransactionStore{Store: st, fail: fault}
				var err error
				if kind == "season" {
					_, err = h.UpdateSeasonMonitor(context.Background(), UpdateSeasonMonitorRequestObject{Id: int64(item.ID), SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: false}})
				} else {
					_, err = h.UpdateEpisodeMonitor(context.Background(), UpdateEpisodeMonitorRequestObject{Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: false}})
				}
				if !errors.Is(err, monitorWriteError) {
					t.Fatalf("error = %v", err)
				}
				afterItem, _ := st.GetMediaItem(item.ID)
				afterSeasons, _ := st.ListSeasonMonitorsByMediaItem(item.ID)
				afterEpisodes, _ := st.ListEpisodeMonitorsByMediaItem(item.ID)
				if !reflect.DeepEqual(beforeItem, afterItem) || !reflect.DeepEqual(beforeSeasons, afterSeasons) || !reflect.DeepEqual(beforeEpisodes, afterEpisodes) {
					t.Fatal("failed transaction changed parent, season or episode state")
				}
			})
		}
	}
}

func TestMonitorToggleUsesFreshParentInsideTransaction(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		t.Run(kind, func(t *testing.T) {
			h, st, item := monitorToggleFixture(t)
			h.store = &monitorTransactionStore{Store: st, beforeTx: func() {
				fresh, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh.Title, fresh.PreferredRelease = "Renamed", "NEW-GROUP"
				if err := st.UpdateMediaItem(fresh); err != nil {
					t.Fatal(err)
				}
			}}
			var err error
			if kind == "season" {
				_, err = h.UpdateSeasonMonitor(context.Background(), UpdateSeasonMonitorRequestObject{Id: int64(item.ID), SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: false}})
			} else {
				_, err = h.UpdateEpisodeMonitor(context.Background(), UpdateEpisodeMonitorRequestObject{Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: false}})
			}
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := st.GetMediaItem(item.ID)
			if err != nil || fresh.Title != "Renamed" || fresh.PreferredRelease != "NEW-GROUP" {
				t.Fatalf("stale parent overwrote unrelated changes: %+v, %v", fresh, err)
			}
		})
	}
}

func TestMonitorToggleMissingItem(t *testing.T) {
	h, _, _ := monitorToggleFixture(t)
	season, err := h.UpdateSeasonMonitor(context.Background(), UpdateSeasonMonitorRequestObject{Id: 999, SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := season.(UpdateSeasonMonitor404JSONResponse); !ok {
		t.Fatalf("season response = %+v", season)
	}
	episode, err := h.UpdateEpisodeMonitor(context.Background(), UpdateEpisodeMonitorRequestObject{Id: 999, SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := episode.(UpdateEpisodeMonitor404JSONResponse); !ok {
		t.Fatalf("episode response = %+v", episode)
	}
}
