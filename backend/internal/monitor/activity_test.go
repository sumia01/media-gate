package monitor

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/store"
)

type monitorActivityStore struct {
	store.Store
	appendErr   error
	appendCalls *atomic.Int32
}

func (s *monitorActivityStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		wrapped := *s
		wrapped.Store = tx
		return fn(&wrapped)
	})
}

func (s *monitorActivityStore) AppendMediaActivity(activity *store.MediaActivity) error {
	s.appendCalls.Add(1)
	if s.appendErr != nil {
		return s.appendErr
	}
	return s.Store.AppendMediaActivity(activity)
}

func TestAutoDownloadAppendsOneActivityAndPublishesInvalidation(t *testing.T) {
	svc, st, _, item := newDecisionTest(t, "movie")
	fresh, err := st.GetMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Title = "Fresh Parent Title"
	if err := st.UpdateMediaItem(fresh); err != nil {
		t.Fatal(err)
	}

	var createdEvents, grabbedEvents, activityEvents atomic.Int32
	svc.bus.Subscribe(eventbus.DownloadCreated, func(eventbus.Event) { createdEvents.Add(1) })
	svc.bus.Subscribe(eventbus.MonitorGrabbed, func(eventbus.Event) { grabbedEvents.Add(1) })
	svc.bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
	svc.bus.Start()

	result := indexer.TorrentResult{
		Title:       strings.Repeat("r", store.MediaActivityMaxTitleBytes+20),
		IndexerName: strings.Repeat("i", store.MediaActivityMaxTitleBytes+20),
		DownloadURL: "release",
	}
	detail := svc.createAutoDownload(fresh, result, nil, nil)
	svc.bus.Stop()
	if detail.Outcome != "grabbed" {
		t.Fatalf("createAutoDownload() = %+v", detail)
	}
	if createdEvents.Load() != 1 || grabbedEvents.Load() != 1 || activityEvents.Load() != 1 {
		t.Fatalf("events: download.created=%d monitor.grabbed=%d media.activity_added=%d",
			createdEvents.Load(), grabbedEvents.Load(), activityEvents.Load())
	}

	activities, _, err := st.ListMediaActivityPage(item.ID, 1, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 {
		t.Fatalf("activities=%+v, want exactly one", activities)
	}
	got := activities[0]
	if got.ActorKind != store.MediaActivityActorSystem || got.ActorComponent != "monitor" ||
		got.Action != store.MediaActivityActionDownloadQueued || got.MediaTitle != fresh.Title {
		t.Fatalf("activity attribution=%+v", got.MediaActivity)
	}
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(got.Details), &details); err != nil {
		t.Fatal(err)
	}
	if details.Automatic == nil || !*details.Automatic || details.DownloadID == nil || *details.DownloadID != *detail.DownloadID {
		t.Fatalf("activity details=%+v", details)
	}
	if len(details.ReleaseName) != store.MediaActivityMaxTitleBytes || len(details.IndexerName) != store.MediaActivityMaxTitleBytes {
		t.Fatalf("unbounded release/indexer lengths=%d/%d", len(details.ReleaseName), len(details.IndexerName))
	}
	if details.Target == nil || details.Target.Scope != store.MediaActivityScopeMedia || details.Target.ObjectID == nil || *details.Target.ObjectID != *detail.DownloadID {
		t.Fatalf("activity target=%+v", details.Target)
	}
}

func TestAutoDownloadActivityScopeUsesReleaseTitle(t *testing.T) {
	for _, tc := range []struct {
		name         string
		title        string
		seasonNumber *int
		wantScope    string
		wantSeason   int
		wantEpisode  int
	}{
		{name: "single episode with nil episode ID", title: "Show.S02E04.1080p", seasonNumber: ptr(2), wantScope: store.MediaActivityScopeEpisode, wantSeason: 2, wantEpisode: 4},
		{name: "season pack", title: "Show.S02.Complete.1080p", seasonNumber: ptr(2), wantScope: store.MediaActivityScopeSeason, wantSeason: 2},
		{name: "unknown series release", title: "Show.1080p", seasonNumber: ptr(2), wantScope: store.MediaActivityScopeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, st, _, item := newDecisionTest(t, "series")
			detail := svc.createAutoDownload(item, indexer.TorrentResult{Title: tc.title, DownloadURL: tc.name}, nil, tc.seasonNumber)
			if detail.Outcome != "grabbed" {
				t.Fatalf("createAutoDownload() = %+v", detail)
			}
			activities, _, err := st.ListMediaActivityPage(item.ID, 1, nil, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(activities) != 1 {
				t.Fatalf("activities=%+v", activities)
			}
			var details store.MediaActivityDetails
			if err := json.Unmarshal([]byte(activities[0].Details), &details); err != nil {
				t.Fatal(err)
			}
			if details.Target == nil || details.Target.Scope != tc.wantScope {
				t.Fatalf("target=%+v, want scope %s", details.Target, tc.wantScope)
			}
			if tc.wantSeason != 0 && (details.Target.SeasonNumber == nil || *details.Target.SeasonNumber != tc.wantSeason) {
				t.Fatalf("target=%+v, want season %d", details.Target, tc.wantSeason)
			}
			if tc.wantEpisode != 0 && (details.Target.EpisodeNumber == nil || *details.Target.EpisodeNumber != tc.wantEpisode) {
				t.Fatalf("target=%+v, want episode %d", details.Target, tc.wantEpisode)
			}
		})
	}
}

func TestAutoDownloadActivityAppendFailureRollsBackDownload(t *testing.T) {
	svc, st, _, item := newDecisionTest(t, "movie")
	appendCalls := &atomic.Int32{}
	wrapped := &monitorActivityStore{Store: st, appendErr: errors.New("append failed"), appendCalls: appendCalls}
	svc.store = wrapped
	var events atomic.Int32
	svc.bus.SubscribeAll(func(eventbus.Event) { events.Add(1) })
	svc.bus.Start()

	detail := svc.createAutoDownload(item, indexer.TorrentResult{Title: "Movie.1080p", DownloadURL: "release"}, nil, nil)
	svc.bus.Stop()
	downloads, err := st.ListDownloads(&item.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Outcome != "error" || appendCalls.Load() != 1 || len(downloads) != 0 || events.Load() != 0 {
		t.Fatalf("detail=%+v appends=%d downloads=%+v events=%d", detail, appendCalls.Load(), downloads, events.Load())
	}
}

func TestAutoDownloadSuppressedAttemptsDoNotAppendOrPublishActivity(t *testing.T) {
	for _, state := range []string{"duplicate", "blocklisted", "stale disabled parent", "deleted"} {
		t.Run(state, func(t *testing.T) {
			svc, st, _, item := newDecisionTest(t, "movie")
			result := indexer.TorrentResult{Title: "Movie.1080p", DownloadURL: "release"}
			wantOutcome, wantDownloads := "disabled", 0
			switch state {
			case "duplicate":
				if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: result.Title, DownloadURL: result.DownloadURL, Status: "pending"}); err != nil {
					t.Fatal(err)
				}
				wantOutcome, wantDownloads = "active_download", 1
			case "blocklisted":
				if err := st.RecordBlocklistFailure(item.ID, result.DownloadURL, result.Title, "failed", maxBlocklistFailures); err != nil {
					t.Fatal(err)
				}
				wantOutcome = "blocked"
			case "stale disabled parent":
				fresh, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh.Monitored = false
				if err := st.UpdateMediaItem(fresh); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := st.DeleteMediaItem(item.ID); err != nil {
					t.Fatal(err)
				}
			}
			appendCalls := &atomic.Int32{}
			wrapped := &monitorActivityStore{Store: st, appendCalls: appendCalls}
			svc.store = wrapped
			var events atomic.Int32
			svc.bus.SubscribeAll(func(eventbus.Event) { events.Add(1) })
			svc.bus.Start()

			detail := svc.createAutoDownload(item, result, nil, nil)
			svc.bus.Stop()
			downloads, err := st.ListDownloads(&item.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if detail.Outcome != wantOutcome || len(downloads) != wantDownloads || appendCalls.Load() != 0 || events.Load() != 0 {
				t.Fatalf("detail=%+v downloads=%+v appends=%d events=%d", detail, downloads, appendCalls.Load(), events.Load())
			}
		})
	}
}
