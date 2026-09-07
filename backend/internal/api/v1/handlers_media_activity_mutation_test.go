package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/matching"
	mediaservice "github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

type actorValidationStore struct {
	store.Store
	checks *int
}

func (s *actorValidationStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&actorValidationStore{Store: tx, checks: s.checks})
	})
}

func (s *actorValidationStore) GetUser(id uint) (*store.User, error) {
	*s.checks = *s.checks + 1
	return s.Store.GetUser(id)
}

func mediaActivityRows(t *testing.T, st store.Store, itemID uint, ctx context.Context) []store.MediaActivityAttribution {
	t.Helper()
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		t.Fatal("activity fixture context has no actor")
	}
	rows, hasMore, err := st.ListMediaActivityPage(itemID, userID, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Fatal("focused activity fixture unexpectedly exceeded one page")
	}
	return rows
}

func mediaActivityDetails(t *testing.T, row store.MediaActivityAttribution) store.MediaActivityDetails {
	t.Helper()
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details
}

func TestDirectMediaMutationsRequireAndValidateActorForNoOps(t *testing.T) {
	for _, kind := range []string{"media", "season", "episode"} {
		t.Run("missing actor/"+kind, func(t *testing.T) {
			h, st, item, _ := monitorToggleFixture(t)
			var err error
			switch kind {
			case "media":
				preferred := "NEW"
				_, err = h.UpdateMediaItem(context.Background(), UpdateMediaItemRequestObject{
					Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &preferred},
				})
			case "season":
				_, err = h.UpdateSeasonMonitor(context.Background(), UpdateSeasonMonitorRequestObject{
					Id: int64(item.ID), SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: false},
				})
			case "episode":
				_, err = h.UpdateEpisodeMonitor(context.Background(), UpdateEpisodeMonitorRequestObject{
					Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: false},
				})
			}
			if !errors.Is(err, errMediaActivityActorMissing) {
				t.Fatalf("error = %v, want missing actor", err)
			}
			fresh, getErr := st.GetMediaItem(item.ID)
			if getErr != nil || fresh.PreferredRelease != "" || !fresh.Monitored {
				t.Fatalf("missing actor changed media: %+v, %v", fresh, getErr)
			}
		})
	}

	h, st, item, ctx := monitorToggleFixture(t)
	checks := 0
	h.store = &actorValidationStore{Store: st, checks: &checks}
	empty := ""
	if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
		Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &empty},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.UpdateSeasonMonitor(ctx, UpdateSeasonMonitorRequestObject{
		Id: int64(item.ID), SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.UpdateEpisodeMonitor(ctx, UpdateEpisodeMonitorRequestObject{
		Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: true},
	}); err != nil {
		t.Fatal(err)
	}
	if checks != 3 {
		t.Fatalf("transactional actor checks = %d, want 3", checks)
	}
	if rows := mediaActivityRows(t, st, item.ID, ctx); len(rows) != 0 {
		t.Fatalf("accepted no-ops appended activity: %+v", rows)
	}

	userID, _ := auth.UserIDFromContext(ctx)
	if err := st.DeleteUser(userID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
		Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &empty},
	}); !errors.Is(err, store.ErrActivityActorNotFound) {
		t.Fatalf("deleted actor error = %v, want ErrActivityActorNotFound", err)
	}
}

func TestUpdateMediaItemAppendsMonitoringAndSafeSettingsWithSharedOperation(t *testing.T) {
	h, st, item, ctx := monitorToggleFixture(t)
	oldProfile := &store.MediaProfile{Name: "Old profile", Resolutions: `["do-not-log"]`, Languages: `[]`}
	newProfile := &store.MediaProfile{Name: "New profile", Resolutions: `["also-do-not-log"]`, Languages: `[]`}
	if err := st.CreateMediaProfile(oldProfile); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaProfile(newProfile); err != nil {
		t.Fatal(err)
	}
	fresh, err := st.GetMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh.MediaProfileID = &oldProfile.ID
	fresh.PreferredRelease = "OLD-GROUP"
	if err := st.UpdateMediaItem(fresh); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: false}); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New(4)
	events := make(chan eventbus.Event, 2)
	bus.Subscribe(eventbus.MediaActivityAdded, func(event eventbus.Event) { events <- event })
	bus.Start()
	h.syncSvc.SetBus(bus)

	monitored := false
	profileID := int64(newProfile.ID)
	preferred := "NEW-GROUP"
	response, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
		Id: int64(item.ID),
		Body: &UpdateMediaItemJSONRequestBody{
			Monitored: &monitored, MediaProfileId: &profileID, PreferredRelease: &preferred,
		},
	})
	if err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	if _, ok := response.(UpdateMediaItem200JSONResponse); !ok {
		bus.Stop()
		t.Fatalf("response = %+v", response)
	}
	bus.Stop()

	rows := mediaActivityRows(t, st, item.ID, ctx)
	if len(rows) != 2 {
		t.Fatalf("activity rows = %+v, want monitoring and settings", rows)
	}
	if rows[0].Action != store.MediaActivityActionSettingsChanged || rows[1].Action != store.MediaActivityActionMonitoringChanged {
		t.Fatalf("activity actions = %q, %q", rows[0].Action, rows[1].Action)
	}
	if rows[0].OperationID == "" || rows[0].OperationID != rows[1].OperationID {
		t.Fatalf("operation IDs = %q, %q", rows[0].OperationID, rows[1].OperationID)
	}
	userID, _ := auth.UserIDFromContext(ctx)
	for _, row := range rows {
		if row.ActorUserID == nil || *row.ActorUserID != userID {
			t.Fatalf("activity actor = %v, want %d", row.ActorUserID, userID)
		}
	}

	settingsDetails := mediaActivityDetails(t, rows[0])
	if settingsDetails.Total != 2 || len(settingsDetails.FieldChanges) != 2 {
		t.Fatalf("settings details = %+v", settingsDetails)
	}
	wantFields := map[string][2]string{
		"media_profile":     {"Old profile (ID " + profileIDString(oldProfile.ID) + ")", "New profile (ID " + profileIDString(newProfile.ID) + ")"},
		"preferred_release": {"OLD-GROUP", "NEW-GROUP"},
	}
	for _, change := range settingsDetails.FieldChanges {
		want, ok := wantFields[change.Field]
		if !ok || change.Before == nil || change.After == nil || *change.Before != want[0] || *change.After != want[1] {
			t.Errorf("field change = %+v, want %v", change, want)
		}
	}
	if strings.Contains(rows[0].Details, "do-not-log") {
		t.Fatalf("settings activity leaked profile configuration: %s", rows[0].Details)
	}
	monitoringDetails := mediaActivityDetails(t, rows[1])
	if monitoringDetails.Total == 0 {
		t.Fatalf("monitoring details = %+v", monitoringDetails)
	}
	if len(events) != 1 {
		t.Fatalf("activity invalidations = %d, want 1", len(events))
	}
	event := <-events
	payload, ok := event.Payload.(eventbus.MediaActivityPayload)
	if !ok || payload.MediaItemID != item.ID {
		t.Fatalf("activity invalidation payload = %#v", event.Payload)
	}
}

func TestUpdateMediaItemBoundsPreferredReleaseActivitySnapshot(t *testing.T) {
	h, st, item, ctx := monitorToggleFixture(t)
	preferred := strings.Repeat("release-group-", 4000)
	if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
		Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &preferred},
	}); err != nil {
		t.Fatal(err)
	}
	fresh, err := st.GetMediaItem(item.ID)
	if err != nil || fresh.PreferredRelease != preferred {
		t.Fatalf("preferred release was not persisted: length=%d err=%v", len(fresh.PreferredRelease), err)
	}
	rows := mediaActivityRows(t, st, item.ID, ctx)
	if len(rows) != 1 || len(rows[0].Details) > store.MediaActivityMaxDetailsBytes {
		t.Fatalf("bounded activity rows = %+v", rows)
	}
	details := mediaActivityDetails(t, rows[0])
	if len(details.FieldChanges) != 1 || details.FieldChanges[0].After == nil || len(*details.FieldChanges[0].After) > store.MediaActivityMaxTitleBytes {
		t.Fatalf("preferred release activity was not bounded: %+v", details)
	}
}

func profileIDString(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func TestDirectMonitoringActivityCapturesInheritanceDisableAndFinalBulkState(t *testing.T) {
	t.Run("season same value removes override", func(t *testing.T) {
		h, st, item, ctx := monitorToggleFixture(t)
		if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: false}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.UpdateSeasonMonitor(ctx, UpdateSeasonMonitorRequestObject{
			Id: int64(item.ID), SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: true},
		}); err != nil {
			t.Fatal(err)
		}
		details := mediaActivityDetails(t, mediaActivityRows(t, st, item.ID, ctx)[0])
		if details.Total != 1 || len(details.MonitoringChanges) != 1 {
			t.Fatalf("details = %+v", details)
		}
		change := details.MonitoringChanges[0]
		if change.Target.Scope != store.MediaActivityScopeEpisode || change.Before == nil || *change.Before || change.After != nil || change.EffectiveBefore || !change.EffectiveAfter {
			t.Fatalf("override removal = %+v", change)
		}
	})

	t.Run("child configuration while parent off", func(t *testing.T) {
		h, st, item, ctx := monitorToggleFixture(t)
		fresh, _ := st.GetMediaItem(item.ID)
		fresh.Monitored = false
		if err := st.UpdateMediaItem(fresh); err != nil {
			t.Fatal(err)
		}
		if _, err := h.UpdateEpisodeMonitor(ctx, UpdateEpisodeMonitorRequestObject{
			Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: true},
		}); err != nil {
			t.Fatal(err)
		}
		change := mediaActivityDetails(t, mediaActivityRows(t, st, item.ID, ctx)[0]).MonitoringChanges[0]
		if change.Before != nil || change.After == nil || !*change.After || change.EffectiveBefore || change.EffectiveAfter {
			t.Fatalf("parent-off episode change = %+v", change)
		}
	})

	t.Run("episode disable", func(t *testing.T) {
		h, st, item, ctx := monitorToggleFixture(t)
		if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.UpdateEpisodeMonitor(ctx, UpdateEpisodeMonitorRequestObject{
			Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: false},
		}); err != nil {
			t.Fatal(err)
		}
		change := mediaActivityDetails(t, mediaActivityRows(t, st, item.ID, ctx)[0]).MonitoringChanges[0]
		if change.Before == nil || !*change.Before || change.After == nil || *change.After || !change.EffectiveBefore || change.EffectiveAfter {
			t.Fatalf("episode disable = %+v", change)
		}
	})

	t.Run("mixed bulk compares final state", func(t *testing.T) {
		h, st, item, ctx := monitorToggleFixture(t)
		if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: false}); err != nil {
			t.Fatal(err)
		}
		seasons := []struct {
			Monitored    bool `json:"monitored"`
			SeasonNumber int  `json:"seasonNumber"`
		}{{Monitored: true, SeasonNumber: 1}}
		episodes := []struct {
			EpisodeNumber int  `json:"episodeNumber"`
			Monitored     bool `json:"monitored"`
			SeasonNumber  int  `json:"seasonNumber"`
		}{
			{SeasonNumber: 1, EpisodeNumber: 1, Monitored: false},
			{SeasonNumber: 1, EpisodeNumber: 2, Monitored: false},
		}
		if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
			Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{SeasonMonitors: &seasons, EpisodeMonitors: &episodes},
		}); err != nil {
			t.Fatal(err)
		}
		details := mediaActivityDetails(t, mediaActivityRows(t, st, item.ID, ctx)[0])
		if details.Total != 1 || len(details.MonitoringChanges) != 1 {
			t.Fatalf("mixed bulk details = %+v", details)
		}
		change := details.MonitoringChanges[0]
		if change.Target.EpisodeNumber == nil || *change.Target.EpisodeNumber != 2 || change.Before != nil || change.After == nil || *change.After {
			t.Fatalf("mixed bulk retained intermediate change: %+v", change)
		}
	})
}

func TestActivityAppendFailureRollsBackDirectMediaMutations(t *testing.T) {
	for _, kind := range []string{"media", "settings", "season", "episode"} {
		t.Run(kind, func(t *testing.T) {
			h, st, item, ctx := monitorToggleFixture(t)
			if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
				t.Fatal(err)
			}
			beforeItem, _ := st.GetMediaItem(item.ID)
			beforeSeasons, _ := st.ListSeasonMonitorsByMediaItem(item.ID)
			beforeEpisodes, _ := st.ListEpisodeMonitorsByMediaItem(item.ID)
			h.store = &monitorTransactionStore{Store: st, fail: "activity"}
			bus := eventbus.New(2)
			events := make(chan eventbus.Event, 1)
			bus.Subscribe(eventbus.MediaActivityAdded, func(event eventbus.Event) { events <- event })
			bus.Start()
			h.syncSvc.SetBus(bus)

			var err error
			switch kind {
			case "media":
				monitored := false
				_, err = h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
					Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{Monitored: &monitored},
				})
			case "settings":
				preferred := "NEW-GROUP"
				_, err = h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
					Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &preferred},
				})
			case "season":
				_, err = h.UpdateSeasonMonitor(ctx, UpdateSeasonMonitorRequestObject{
					Id: int64(item.ID), SeasonNumber: 1, Body: &UpdateSeasonMonitorJSONRequestBody{Monitored: false},
				})
			case "episode":
				_, err = h.UpdateEpisodeMonitor(ctx, UpdateEpisodeMonitorRequestObject{
					Id: int64(item.ID), SeasonNumber: 1, EpisodeNumber: 1, Body: &UpdateEpisodeMonitorJSONRequestBody{Monitored: false},
				})
			}
			bus.Stop()
			if !errors.Is(err, monitorWriteError) {
				t.Fatalf("error = %v, want activity append failure", err)
			}
			afterItem, _ := st.GetMediaItem(item.ID)
			afterSeasons, _ := st.ListSeasonMonitorsByMediaItem(item.ID)
			afterEpisodes, _ := st.ListEpisodeMonitorsByMediaItem(item.ID)
			if !reflect.DeepEqual(beforeItem, afterItem) || !reflect.DeepEqual(beforeSeasons, afterSeasons) || !reflect.DeepEqual(beforeEpisodes, afterEpisodes) {
				t.Fatalf("activity failure committed domain mutation: item=%+v seasons=%+v episodes=%+v", afterItem, afterSeasons, afterEpisodes)
			}
			if rows := mediaActivityRows(t, st, item.ID, ctx); len(rows) != 0 {
				t.Fatalf("activity failure retained rows: %+v", rows)
			}
			if len(events) != 0 {
				t.Fatalf("rolled-back activity published %d invalidations", len(events))
			}
		})
	}
}

type mediaMatchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f mediaMatchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func mediaMatchClient(calls *int) *http.Client {
	return &http.Client{Transport: mediaMatchRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		*calls++
		body := `{}`
		status := http.StatusOK
		switch {
		case strings.Contains(req.URL.Path, "/season/"):
			body = `{"season_number":1,"episodes":[{"season_number":1,"episode_number":1,"name":"Episode"}]}`
		case strings.Contains(req.URL.Path, "/tv/"):
			body = `{"name":"Matched Show","number_of_seasons":1,"status":"Returning Series"}`
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func TestManualMatchAndUnmatchRequireLiveActorAndRecordIt(t *testing.T) {
	t.Run("missing actor", func(t *testing.T) {
		h, st, item, _ := monitorToggleFixture(t)
		calls := 0
		client := mediaMatchClient(&calls)
		h.matchSvc = matching.NewService(st, settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client), t.TempDir(), client)
		if _, err := h.ManualMatch(context.Background(), ManualMatchRequestObject{Id: int64(item.ID), Body: &ManualMatchJSONRequestBody{Source: "tmdb", ExternalId: 123}}); !errors.Is(err, errMediaActivityActorMissing) {
			t.Fatalf("manual error = %v", err)
		}
		if _, err := h.UnmatchMedia(context.Background(), UnmatchMediaRequestObject{Id: int64(item.ID)}); !errors.Is(err, errMediaActivityActorMissing) {
			t.Fatalf("unmatch error = %v", err)
		}
		if calls != 0 {
			t.Fatalf("unauthenticated manual match made %d provider calls", calls)
		}
	})

	t.Run("deleted actor", func(t *testing.T) {
		h, st, item, ctx := monitorToggleFixture(t)
		userID, _ := auth.UserIDFromContext(ctx)
		if err := st.DeleteUser(userID); err != nil {
			t.Fatal(err)
		}
		calls := 0
		client := mediaMatchClient(&calls)
		h.matchSvc = matching.NewService(st, settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client), t.TempDir(), client)
		if _, err := h.ManualMatch(ctx, ManualMatchRequestObject{Id: int64(item.ID), Body: &ManualMatchJSONRequestBody{Source: "tmdb", ExternalId: 123}}); !errors.Is(err, store.ErrActivityActorNotFound) {
			t.Fatalf("manual error = %v", err)
		}
		if _, err := h.UnmatchMedia(ctx, UnmatchMediaRequestObject{Id: int64(item.ID)}); !errors.Is(err, store.ErrActivityActorNotFound) {
			t.Fatalf("unmatch error = %v", err)
		}
		if calls != 0 {
			t.Fatalf("deleted actor manual match made %d provider calls", calls)
		}
	})

	t.Run("live actor", func(t *testing.T) {
		h, st, item, ctx := monitorToggleFixture(t)
		userID, _ := auth.UserIDFromContext(ctx)
		calls := 0
		client := mediaMatchClient(&calls)
		matchSvc := matching.NewService(st, settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client), t.TempDir(), client)
		bus := eventbus.New(8)
		invalidations := make(chan eventbus.Event, 2)
		bus.Subscribe(eventbus.MediaActivityAdded, func(event eventbus.Event) { invalidations <- event })
		bus.Start()
		matchSvc.SetBus(bus)
		h.matchSvc = matchSvc

		response, err := h.ManualMatch(ctx, ManualMatchRequestObject{Id: int64(item.ID), Body: &ManualMatchJSONRequestBody{Source: "tmdb", ExternalId: 123}})
		if err != nil {
			bus.Stop()
			t.Fatal(err)
		}
		if _, ok := response.(ManualMatch200JSONResponse); !ok {
			bus.Stop()
			t.Fatalf("response = %+v", response)
		}
		if _, err := h.UnmatchMedia(ctx, UnmatchMediaRequestObject{Id: int64(item.ID)}); err != nil {
			bus.Stop()
			t.Fatal(err)
		}
		bus.Stop()
		rows := mediaActivityRows(t, st, item.ID, ctx)
		if len(rows) != 2 || rows[0].Action != store.MediaActivityActionUnmatched || rows[1].Action != store.MediaActivityActionMatchChanged {
			t.Fatalf("match activity = %+v", rows)
		}
		for _, row := range rows {
			if row.ActorUserID == nil || *row.ActorUserID != userID {
				t.Fatalf("activity actor = %+v, want %d", row, userID)
			}
		}
		if calls != 2 || len(invalidations) != 2 {
			t.Fatalf("provider calls=%d invalidations=%d", calls, len(invalidations))
		}
	})
}

func TestResyncAndMediaDeletionHandlersRequireActor(t *testing.T) {
	h, st, item, ctx := monitorToggleFixture(t)
	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := st.DeleteMediaFile(file.ID); err != nil {
			t.Fatal(err)
		}
	}
	library, err := st.GetLibrary(item.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(library.Path, "Show")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "Show.S01E01.1080p.mkv")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	season, episode := 1, 1
	if err := st.CreateMediaFile(&store.MediaFile{
		MediaItemID: item.ID, Path: path, FileName: filepath.Base(path), Size: 5,
		Resolution: "1080p", SeasonNumber: &season, EpisodeNumber: &episode,
	}); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New(8)
	bus.Start()
	h.syncSvc.SetBus(bus)
	h.mediaSvc = mediaservice.NewService(st, h.syncSvc, bus, nil, t.TempDir())
	if _, err := h.ResyncMediaItem(context.Background(), ResyncMediaItemRequestObject{Id: int64(item.ID)}); !errors.Is(err, errMediaActivityActorMissing) {
		bus.Stop()
		t.Fatalf("resync error = %v, want missing actor", err)
	}
	if _, err := h.DeleteMediaItem(context.Background(), DeleteMediaItemRequestObject{Id: int64(item.ID)}); !errors.Is(err, errMediaActivityActorMissing) {
		bus.Stop()
		t.Fatalf("delete error = %v, want missing actor", err)
	}
	if _, err := st.GetMediaItem(item.ID); err != nil {
		bus.Stop()
		t.Fatalf("unauthenticated handlers changed item: %v", err)
	}

	response, err := h.ResyncMediaItem(ctx, ResyncMediaItemRequestObject{Id: int64(item.ID)})
	if err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	if _, ok := response.(ResyncMediaItem200JSONResponse); !ok {
		bus.Stop()
		t.Fatalf("resync response = %+v", response)
	}
	rows := mediaActivityRows(t, st, item.ID, ctx)
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionResyncCompleted || rows[1].Action != store.MediaActivityActionResyncRequested {
		bus.Stop()
		t.Fatalf("resync activity = %+v", rows)
	}
	if _, err := h.DeleteMediaItem(ctx, DeleteMediaItemRequestObject{Id: int64(item.ID)}); err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()
	if _, err := st.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("authenticated deletion did not remove item: %v", err)
	}
}
