package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/sumia01/media-gate/internal/api/v1"
	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

func TestDecisionInputTimestampIsCopiedNotAdvanced(t *testing.T) {
	svc, st, _, item := newDecisionTest(t, "movie")
	input := item.UpdatedAt
	check := newDecisionCheck(item)
	if check.row.InputUpdatedAt == &item.UpdatedAt {
		t.Fatal("snapshot aliases the mutable item timestamp")
	}
	// Later reads or writes of the same object cannot roll the captured input forward.
	for range 2 {
		item.UpdatedAt = item.UpdatedAt.Add(time.Minute)
		if check.row.InputUpdatedAt == nil || !check.row.InputUpdatedAt.Equal(input) {
			t.Fatalf("input version advanced: %+v", check.row)
		}
	}
	check.add(decisionDetail("no_results", "No results."))
	beforeSave := time.Now()
	svc.saveDecision(check)
	got := latestDecision(t, st, item.ID)
	if got.InputUpdatedAt == nil || !got.InputUpdatedAt.Equal(input) || got.CheckedAt.Before(beforeSave) {
		t.Fatalf("input/completion timestamps = %+v", got)
	}
	next := newDecisionCheck(item)
	if next.row.InputUpdatedAt == nil || !next.row.InputUpdatedAt.Equal(item.UpdatedAt) {
		t.Fatalf("next check did not capture its own input: %+v", next.row)
	}
}

type decisionInputListStore struct {
	store.Store
	inputs []store.MediaItem
	reads  int
}

func (s *decisionInputListStore) ListMonitoredMediaItems() ([]store.MediaItem, error) {
	return append([]store.MediaItem(nil), s.inputs...), nil
}

func (s *decisionInputListStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	s.reads++
	if s.reads == 1 {
		// Model an input read captured before an edit; subsequent config reads are fresh.
		item := s.inputs[0]
		return &item, nil
	}
	return s.Store.GetMediaItem(id)
}

func decisionHandlers(svc *Service, st store.Store) *apiv1.Handlers {
	return apiv1.NewHandlers(nil, st, nil, svc.settings, nil, mediasync.NewService(st), nil,
		"", "", nil, false, nil, nil, nil, nil, nil, nil, "test")
}

func decisionAPIState(t *testing.T, svc *Service, st store.Store, id uint) (apiv1.MediaItem, apiv1.MonitorDecision) {
	t.Helper()
	// Recreate the reader and round-trip JSON, as when remounting without local edit state.
	handler := apiv1.Handler(apiv1.NewStrictHandler(decisionHandlers(svc, st), nil))
	var item apiv1.MediaItem
	var snapshot struct {
		Decision *apiv1.MonitorDecision `json:"decision"`
	}
	for _, request := range []struct {
		path string
		out  any
	}{
		{fmt.Sprintf("/media/%d", id), &item},
		{fmt.Sprintf("/media/%d/monitor-decision", id), &snapshot},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, request.path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", request.path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), request.out); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot.Decision == nil {
		t.Fatal("missing saved check")
	}
	return item, *snapshot.Decision
}

func TestDecisionInputFreshnessSurvivesSettingsChangesAndReload(t *testing.T) {
	for _, kind := range []string{"season", "episode"} {
		for _, when := range []string{"before_cycle_with_old_inputs", "during_search", "after_check"} {
			t.Run(kind+"/"+when, func(t *testing.T) {
				svc, st, search, item := newDecisionTest(t, "series")
				actor := &store.User{Email: kind + "-" + when + "@example.test", PasswordHash: "hash"}
				if err := st.CreateUser(actor); err != nil {
					t.Fatal(err)
				}
				actorCtx := auth.ContextWithUserID(context.Background(), actor.ID)
				input := item.UpdatedAt
				// A cycle can hold an item version older than processItem's entry time.
				svc.store = &decisionInputListStore{Store: st, inputs: []store.MediaItem{*item}}
				toggle := func() {
					h := decisionHandlers(svc, st)
					var err error
					if kind == "season" {
						_, err = h.UpdateSeasonMonitor(actorCtx, apiv1.UpdateSeasonMonitorRequestObject{
							Id: int64(item.ID), SeasonNumber: 1, Body: &apiv1.UpdateSeasonMonitorJSONRequestBody{Monitored: false},
						})
					} else {
						_, err = h.UpdateEpisodeMonitor(actorCtx, apiv1.UpdateEpisodeMonitorRequestObject{
							Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &apiv1.UpdateEpisodeMonitorJSONRequestBody{Monitored: false},
						})
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if when == "before_cycle_with_old_inputs" {
					toggle()
				}
				if when == "during_search" {
					started, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
					search.after = func() {
						close(started)
						<-resume
					}
					go func() {
						svc.processOnce()
						close(done)
					}()
					t.Cleanup(func() {
						select {
						case <-resume:
						default:
							close(resume)
						}
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Error("check did not finish")
						}
					})
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						t.Fatal("search did not start")
					}
					toggle()
					close(resume)
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("check did not finish")
					}
				} else {
					svc.processOnce()
				}
				if when == "after_check" {
					toggle()
				}
				for range 2 {
					current, decision := decisionAPIState(t, svc, st, item.ID)
					if decision.InputUpdatedAt == nil || !decision.InputUpdatedAt.Equal(input) {
						t.Fatalf("check input rolled forward: %+v; want %v", decision, input)
					}
					if current.UpdatedAt.UnixMilli() <= decision.InputUpdatedAt.UnixMilli() {
						t.Fatalf("reloaded API cannot show stale: item=%v input=%v", current.UpdatedAt, decision.InputUpdatedAt)
					}
					if when != "after_check" && !decision.CheckedAt.After(current.UpdatedAt) {
						t.Fatalf("fixture did not reproduce a change before completion: item=%v completed=%v", current.UpdatedAt, decision.CheckedAt)
					}
				}
			})
		}
	}
}

func TestDecisionSearchMarkersDoNotMakeUnchangedInputsStale(t *testing.T) {
	for _, mediaType := range []string{"movie", "series"} {
		t.Run(mediaType, func(t *testing.T) {
			svc, st, search, item := newDecisionTest(t, mediaType)
			input := item.UpdatedAt
			for _, grab := range []bool{false, false, true} {
				if grab {
					search.results = []indexer.TorrentResult{{Title: "Show.S01.Complete.1080p", DownloadURL: "release"}}
				}
				svc.processOnce()
				current, decision := decisionAPIState(t, svc, st, item.ID)
				if !current.UpdatedAt.Equal(input) || decision.InputUpdatedAt == nil || !decision.InputUpdatedAt.Equal(input) {
					t.Fatalf("marker produced false stale input: item=%v snapshot=%+v", current.UpdatedAt, decision)
				}
				persisted, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				if (persisted.MonitorSearchStartedAt == nil) != grab {
					t.Fatalf("search marker not set/cleared: %+v", persisted)
				}
			}
		})
	}
}

func TestAutoDownloadRejectsSelectionWhenParentChangesDuringSearch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mediaType string
		title     string
		touch     func(*testing.T, *Service, *decisionTestStore, *store.MediaItem)
	}{
		{
			name:      "episode monitor change",
			mediaType: "series",
			title:     "Show.S01.Complete.1080p",
			touch: func(t *testing.T, svc *Service, st *decisionTestStore, item *store.MediaItem) {
				actor := &store.User{Email: "episode-race@example.test", PasswordHash: "hash"}
				if err := st.CreateUser(actor); err != nil {
					t.Fatal(err)
				}
				_, err := decisionHandlers(svc, st).UpdateEpisodeMonitor(
					auth.ContextWithUserID(context.Background(), actor.ID),
					apiv1.UpdateEpisodeMonitorRequestObject{
						Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1,
						Body: &apiv1.UpdateEpisodeMonitorJSONRequestBody{Monitored: false},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:      "preferred release parent touch",
			mediaType: "movie",
			title:     "Show.1080p-OLD",
			touch: func(t *testing.T, _ *Service, st *decisionTestStore, item *store.MediaItem) {
				fresh, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh.PreferredRelease = "NEW"
				if err := st.UpdateMediaItem(fresh); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, st, search, item := newDecisionTest(t, tc.mediaType)
			inputVersion := item.UpdatedAt
			search.results = []indexer.TorrentResult{{Title: tc.title, DownloadURL: "release"}}
			search.after = func() { tc.touch(t, svc, st, item) }

			svc.processItem(item)

			downloads, err := st.ListDownloads(&item.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			activities, _, err := st.ListMediaActivityPage(item.ID, 1, nil, 10)
			if err != nil {
				t.Fatal(err)
			}
			for _, activity := range activities {
				if activity.Action == store.MediaActivityActionDownloadQueued {
					t.Fatalf("stale selection appended download activity: %+v", activity)
				}
			}
			decision := latestDecision(t, st, item.ID)
			if len(downloads) != 0 || decision.Outcome != "no_eligible_targets" {
				t.Fatalf("stale selection was not suppressed: downloads=%+v activities=%+v decision=%+v", downloads, activities, decision)
			}
			if decision.InputUpdatedAt == nil || !decision.InputUpdatedAt.Equal(inputVersion) {
				t.Fatalf("decision input=%v, want %v", decision.InputUpdatedAt, inputVersion)
			}
			found := false
			for _, detail := range decision.Details {
				if detail.Outcome == "no_eligible_targets" && detail.SelectedTitle == tc.title &&
					strings.Contains(detail.Explanation, "changed during the search") {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing truthful stale-selection detail: %+v", decision.Details)
			}
		})
	}
}

func TestAutoDownloadQueuesWhenOnlySearchMarkerChanges(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "movie")
	inputVersion := item.UpdatedAt
	search.results = []indexer.TorrentResult{{Title: "Show.1080p", DownloadURL: "release"}}
	search.after = func() {
		startedAt := time.Now().UTC()
		if err := st.SetMonitorSearchStartedAt(item.ID, &startedAt); err != nil {
			t.Fatal(err)
		}
	}

	svc.processItem(item)

	downloads, err := st.ListDownloads(&item.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := st.GetMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	decision := latestDecision(t, st, item.ID)
	if len(downloads) != 1 || decision.Outcome != "grabbed" || !current.UpdatedAt.Equal(inputVersion) {
		t.Fatalf("unchanged input did not queue: downloads=%+v item=%+v decision=%+v", downloads, current, decision)
	}
}
