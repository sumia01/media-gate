package metarefresh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type claimDuringBackfillStore struct {
	store.Store
	claim func()
}

func (s *claimDuringBackfillStore) ListDownloads(itemID *uint, status *string) ([]store.Download, error) {
	downloads, err := s.Store.ListDownloads(itemID, status)
	if err == nil {
		s.claim()
	}
	return downloads, err
}

func TestOrphanBackfillDefersImportingAndLosesToConcurrentClaim(t *testing.T) {
	for _, concurrentClaim := range []bool{false, true} {
		name := "already importing"
		if concurrentClaim {
			name = "claim after metadata snapshot"
		}
		t.Run(name, func(t *testing.T) {
			s, err := sqlite.New(filepath.Join(t.TempDir(), "metadata.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lib := &store.Library{Name: "Shows", Path: t.TempDir(), MediaType: "series"}
			if err := s.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series", Source: "disk"}
			if err := s.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			ep := &store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1}
			if err := s.CreateEpisode(ep); err != nil {
				t.Fatal(err)
			}
			downloadedAt := time.Now().Add(-time.Hour)
			dl := &store.Download{MediaItemID: item.ID, Title: "Show.S01E01", Status: "downloaded", LastError: "previous error", DownloadedAt: &downloadedAt}
			if err := s.CreateDownload(dl); err != nil {
				t.Fatal(err)
			}
			claim := func() {
				dl.Status = "importing"
				if err := s.UpdateDownload(dl); err != nil {
					t.Fatal(err)
				}
			}
			var st store.Store = s
			if concurrentClaim {
				st = &claimDuringBackfillStore{Store: s, claim: claim}
			} else {
				claim()
			}
			(&Service{store: st}).resolveOrphanDownloads(item.ID)
			got, err := s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "importing" || got.EpisodeID != nil || !got.UpdatedAt.Equal(dl.UpdatedAt) {
				t.Fatalf("backfill invalidated claimed import: %+v", got)
			}
			// The owning import can still persist a retry using its original claim.
			dl.Status = "downloaded"
			if err := s.UpdateDownload(dl); err != nil {
				t.Fatalf("import retry rejected after metadata pass: %v", err)
			}
			stale := *dl
			(&Service{store: s}).resolveOrphanDownloads(item.ID)
			got, err = s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.EpisodeID == nil || *got.EpisodeID != ep.ID || got.LastError != "previous error" || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
				t.Fatalf("later metadata pass did not safely backfill: %+v", got)
			}
			stale.Status = "importing"
			if err := s.UpdateDownload(&stale); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("stale claim overwrote metadata: %v", err)
			}
		})
	}
}

type providerEpisode struct {
	season  int
	episode int
	title   string
}

type refreshProvider struct {
	seasons  int
	status   string
	episodes map[int][]providerEpisode
	fail     map[int]bool
}

func (p refreshProvider) client() *http.Client {
	return &http.Client{Transport: refreshRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := "{}"
		if strings.Contains(req.URL.Path, "/season/") {
			season, _ := strconv.Atoi(req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:])
			if p.fail[season] {
				status = http.StatusBadGateway
			} else {
				entries := make([]string, 0, len(p.episodes[season]))
				for _, episode := range p.episodes[season] {
					entries = append(entries, fmt.Sprintf(`{"season_number":%d,"episode_number":%d,"name":%q,"air_date":"2020-01-01","runtime":30}`, episode.season, episode.episode, episode.title))
				}
				body = fmt.Sprintf(`{"season_number":%d,"episodes":[%s]}`, season, strings.Join(entries, ","))
			}
		} else if strings.Contains(req.URL.Path, "/tv/") {
			body = fmt.Sprintf(`{"name":"Refresh Show","number_of_seasons":%d,"status":%q}`, p.seasons, p.status)
		} else {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

type refreshRoundTripFunc func(*http.Request) (*http.Response, error)

func (f refreshRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type refreshFixture struct {
	store *sqlite.SQLiteStore
	item  *store.MediaItem
	meta  *store.MediaMetadata
}

func newRefreshFixture(t *testing.T, seasons int, monitored, monitorNewSeasons bool) *refreshFixture {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.New(filepath.Join(dir, "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	lib := &store.Library{Name: "Shows", Path: dir, MediaType: "series"}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Refresh Show", MediaType: "series", Source: "disk", Status: "partial", Monitored: monitored, MonitorNewSeasons: monitorNewSeasons}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	item.Monitored = monitored
	item.MonitorNewSeasons = monitorNewSeasons
	if err := st.UpdateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	item, err = st.GetMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := &store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 123, Title: item.Title, Status: "Returning Series", Seasons: &seasons, MatchedAt: time.Now().Add(-time.Hour)}
	if err := st.CreateMediaMetadata(meta); err != nil {
		t.Fatal(err)
	}
	return &refreshFixture{store: st, item: item, meta: meta}
}

func (f *refreshFixture) matchingService(t *testing.T, st store.Store, provider refreshProvider) *matching.Service {
	t.Helper()
	client := provider.client()
	set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "test-key"}, "", client)
	return matching.NewService(st, set, t.TempDir(), client)
}

func (f *refreshFixture) addEpisode(t *testing.T, season, episode int) {
	t.Helper()
	if err := f.store.CreateEpisode(&store.Episode{MediaItemID: f.item.ID, SeasonNumber: season, EpisodeNumber: episode, Title: fmt.Sprintf("S%dE%d", season, episode)}); err != nil {
		t.Fatal(err)
	}
}

func refreshActivityRows(t *testing.T, st store.Store, itemID uint) []store.MediaActivityAttribution {
	t.Helper()
	rows, more, err := st.ListMediaActivityPage(itemID, 0, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if more {
		t.Fatal("unexpected extra activity page")
	}
	return rows
}

func refreshDetails(t *testing.T, row store.MediaActivityAttribution) store.MediaActivityDetails {
	t.Helper()
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details
}

func TestRefreshSeriesMetadataSuppressesUnchangedSemanticResult(t *testing.T) {
	fixture := newRefreshFixture(t, 1, true, false)
	fixture.addEpisode(t, 1, 1)
	beforeMeta := fixture.meta.UpdatedAt
	beforeItem := fixture.item.UpdatedAt
	provider := refreshProvider{seasons: 1, status: " Returning Series ", episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1, title: "same"}}}}

	result, err := fixture.matchingService(t, fixture.store, provider).RefreshSeriesMetadata(fixture.item, fixture.meta)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.ActivityAdded || len(refreshActivityRows(t, fixture.store, fixture.item.ID)) != 0 {
		t.Fatalf("unchanged result = %+v", result)
	}
	meta, _ := fixture.store.GetMediaMetadataByMediaItem(fixture.item.ID)
	item, _ := fixture.store.GetMediaItem(fixture.item.ID)
	if !meta.UpdatedAt.Equal(beforeMeta) || !item.UpdatedAt.Equal(beforeItem) {
		t.Fatalf("unchanged refresh rewrote versions: metadata=%v/%v item=%v/%v", beforeMeta, meta.UpdatedAt, beforeItem, item.UpdatedAt)
	}
}

func TestRefreshSeriesMetadataRecordsStatusSeasonAndEpisodeSemanticDiff(t *testing.T) {
	fixture := newRefreshFixture(t, 1, true, false)
	fixture.addEpisode(t, 1, 1)
	inputUpdatedAt := fixture.item.UpdatedAt
	if err := fixture.store.UpsertMonitorDecision(&store.MonitorDecision{
		MediaItemID: fixture.item.ID, InputUpdatedAt: &inputUpdatedAt, CheckedAt: time.Now(), Outcome: "no_release", Summary: "saved decision",
	}); err != nil {
		t.Fatal(err)
	}
	provider := refreshProvider{
		seasons: 2,
		status:  "Ended",
		episodes: map[int][]providerEpisode{
			1: {{season: 1, episode: 1}, {season: 1, episode: 2}},
			2: {{season: 2, episode: 1}},
		},
	}

	result, err := fixture.matchingService(t, fixture.store, provider).RefreshSeriesMetadata(fixture.item, fixture.meta)
	if err != nil || !result.Changed || !result.ActivityAdded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	rows := refreshActivityRows(t, fixture.store, fixture.item.ID)
	if len(rows) != 1 || rows[0].Action != store.MediaActivityActionMetadataChanged || rows[0].ActorComponent != "metarefresh" {
		t.Fatalf("activity rows = %+v", rows)
	}
	details := refreshDetails(t, rows[0])
	if details.Added != 2 || details.Total != 4 || len(details.Targets) != 2 || len(details.FieldChanges) != 2 || details.Partial || details.SuccessfulProviderWindows != 2 || details.FailedProviderWindows != 0 {
		t.Fatalf("metadata details = %+v", details)
	}
	meta, _ := fixture.store.GetMediaMetadataByMediaItem(fixture.item.ID)
	item, _ := fixture.store.GetMediaItem(fixture.item.ID)
	episodes, _ := fixture.store.ListEpisodesByMediaItem(fixture.item.ID)
	if meta.Status != "Ended" || meta.Seasons == nil || *meta.Seasons != 2 || len(episodes) != 3 {
		t.Fatalf("refreshed state: meta=%+v episodes=%+v", meta, episodes)
	}
	decision, err := fixture.store.GetMonitorDecision(fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !item.UpdatedAt.After(inputUpdatedAt) || decision.InputUpdatedAt == nil || !decision.InputUpdatedAt.Equal(inputUpdatedAt) {
		t.Fatalf("refresh did not stale saved decision: item=%v decision=%+v", item.UpdatedAt, decision)
	}
}

func TestRefreshSeriesMetadataMarksPartialAndNeverRemovesFromFailedWindows(t *testing.T) {
	fixture := newRefreshFixture(t, 2, true, false)
	fixture.addEpisode(t, 1, 1)
	fixture.addEpisode(t, 2, 1)
	provider := refreshProvider{
		seasons:  2,
		status:   "Returning Series",
		episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1}, {season: 1, episode: 2}}},
		fail:     map[int]bool{2: true},
	}

	result, err := fixture.matchingService(t, fixture.store, provider).RefreshSeriesMetadata(fixture.item, fixture.meta)
	if err != nil || !result.Changed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	episodes, _ := fixture.store.ListEpisodesByMediaItem(fixture.item.ID)
	if len(episodes) != 3 {
		t.Fatalf("partial refresh removed or missed episodes: %+v", episodes)
	}
	details := refreshDetails(t, refreshActivityRows(t, fixture.store, fixture.item.ID)[0])
	if !details.Partial || details.FailedProviderWindows != 1 || details.SuccessfulProviderWindows != 1 || details.Added != 1 || details.Removed != 0 {
		t.Fatalf("partial details = %+v", details)
	}
}

var refreshWriteError = errors.New("refresh write failed")

type refreshFaultStore struct {
	store.Store
	fail     string
	beforeTx func()
}

func (s *refreshFaultStore) WithTx(fn func(store.Store) error) error {
	if s.beforeTx != nil {
		s.beforeTx()
	}
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&refreshFaultStore{Store: tx, fail: s.fail})
	})
}

func (s *refreshFaultStore) CreateEpisode(episode *store.Episode) error {
	if s.fail == "episode" {
		return refreshWriteError
	}
	return s.Store.CreateEpisode(episode)
}

func (s *refreshFaultStore) AppendMediaActivity(activity *store.MediaActivity) error {
	if s.fail == "activity" {
		return refreshWriteError
	}
	return s.Store.AppendMediaActivity(activity)
}

func TestRefreshSeriesMetadataSuppressesStaleCandidate(t *testing.T) {
	fixture := newRefreshFixture(t, 1, true, false)
	fixture.addEpisode(t, 1, 1)
	wrapper := &refreshFaultStore{Store: fixture.store, beforeTx: func() {
		current, err := fixture.store.GetMediaMetadataByMediaItem(fixture.item.ID)
		if err != nil {
			t.Fatal(err)
		}
		current.Status = "Concurrent change"
		if err := fixture.store.UpdateMediaMetadata(current); err != nil {
			t.Fatal(err)
		}
	}}
	provider := refreshProvider{seasons: 2, status: "Ended", episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1}}, 2: {{season: 2, episode: 1}}}}

	result, err := fixture.matchingService(t, wrapper, provider).RefreshSeriesMetadata(fixture.item, fixture.meta)
	if !errors.Is(err, matching.ErrStaleRefreshCandidate) || result.Changed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	meta, _ := fixture.store.GetMediaMetadataByMediaItem(fixture.item.ID)
	episodes, _ := fixture.store.ListEpisodesByMediaItem(fixture.item.ID)
	if meta.Status != "Concurrent change" || meta.Seasons == nil || *meta.Seasons != 1 || len(episodes) != 1 || len(refreshActivityRows(t, fixture.store, fixture.item.ID)) != 0 {
		t.Fatalf("stale candidate changed state: meta=%+v episodes=%+v", meta, episodes)
	}
}

func TestRefreshSeriesMetadataRollsBackEpisodeAndActivityFailures(t *testing.T) {
	for _, failure := range []string{"episode", "activity"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newRefreshFixture(t, 1, true, false)
			fixture.addEpisode(t, 1, 1)
			wrapper := &refreshFaultStore{Store: fixture.store, fail: failure}
			provider := refreshProvider{seasons: 2, status: "Ended", episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1}}, 2: {{season: 2, episode: 1}}}}
			result, err := fixture.matchingService(t, wrapper, provider).RefreshSeriesMetadata(fixture.item, fixture.meta)
			if !errors.Is(err, refreshWriteError) || result.Changed {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			meta, _ := fixture.store.GetMediaMetadataByMediaItem(fixture.item.ID)
			episodes, _ := fixture.store.ListEpisodesByMediaItem(fixture.item.ID)
			if meta.Status != "Returning Series" || meta.Seasons == nil || *meta.Seasons != 1 || len(episodes) != 1 || len(refreshActivityRows(t, fixture.store, fixture.item.ID)) != 0 {
				t.Fatalf("failed refresh committed state: meta=%+v episodes=%+v", meta, episodes)
			}
		})
	}
}

func TestRefreshSeriesMetadataFutureSeasonPolicyAndSharedOperation(t *testing.T) {
	for _, existingFalse := range []bool{false, true} {
		name := "new monitor"
		if existingFalse {
			name = "false to true"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newRefreshFixture(t, 1, true, true)
			fixture.addEpisode(t, 1, 1)
			if existingFalse {
				if err := fixture.store.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: fixture.item.ID, SeasonNumber: 2, Monitored: false}); err != nil {
					t.Fatal(err)
				}
			}
			provider := refreshProvider{seasons: 2, status: "Returning Series", episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1}}, 2: {{season: 2, episode: 1}}}}
			result, err := fixture.matchingService(t, fixture.store, provider).RefreshSeriesMetadata(fixture.item, fixture.meta)
			if err != nil || !result.Changed {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			monitors, _ := fixture.store.ListSeasonMonitorsByMediaItem(fixture.item.ID)
			if len(monitors) != 1 || monitors[0].SeasonNumber != 2 || !monitors[0].Monitored {
				t.Fatalf("future monitor = %+v", monitors)
			}
			rows := refreshActivityRows(t, fixture.store, fixture.item.ID)
			if len(rows) != 2 || rows[0].Action != store.MediaActivityActionMonitoringChanged || rows[1].Action != store.MediaActivityActionMetadataChanged || rows[0].OperationID == "" || rows[0].OperationID != rows[1].OperationID {
				t.Fatalf("shared operation rows = %+v", rows)
			}
			policy := refreshDetails(t, rows[0])
			if policy.Reason != "future_season_policy" || policy.Total != 1 || len(policy.MonitoringChanges) != 1 {
				t.Fatalf("policy details = %+v", policy)
			}
			change := policy.MonitoringChanges[0]
			if change.After == nil || !*change.After || !change.EffectiveAfter || existingFalse && (change.Before == nil || *change.Before) || !existingFalse && change.Before != nil {
				t.Fatalf("policy change = %+v", change)
			}
		})
	}
}

func TestRefreshSeriesMetadataSuppressesFuturePolicyWhenDisabledOrParentOff(t *testing.T) {
	for _, parentOn := range []bool{false, true} {
		name := "parent off"
		if parentOn {
			name = "policy off"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newRefreshFixture(t, 1, parentOn, false)
			fixture.addEpisode(t, 1, 1)
			provider := refreshProvider{seasons: 2, status: "Returning Series", episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1}}, 2: {{season: 2, episode: 1}}}}
			if _, err := fixture.matchingService(t, fixture.store, provider).RefreshSeriesMetadata(fixture.item, fixture.meta); err != nil {
				t.Fatal(err)
			}
			monitors, _ := fixture.store.ListSeasonMonitorsByMediaItem(fixture.item.ID)
			rows := refreshActivityRows(t, fixture.store, fixture.item.ID)
			if len(monitors) != 0 || len(rows) != 1 || rows[0].Action != store.MediaActivityActionMetadataChanged {
				t.Fatalf("suppressed policy: monitors=%+v rows=%+v", monitors, rows)
			}
		})
	}
}

type refreshRecalc struct{ calls atomic.Int32 }

func (r *refreshRecalc) RecalcMediaItemStatus(uint) error {
	r.calls.Add(1)
	return nil
}

func TestProcessOncePublishesRefreshAndSingleActivityInvalidation(t *testing.T) {
	fixture := newRefreshFixture(t, 1, true, false)
	fixture.addEpisode(t, 1, 1)
	provider := refreshProvider{seasons: 1, status: "Ended", episodes: map[int][]providerEpisode{1: {{season: 1, episode: 1}}}}
	matchSvc := fixture.matchingService(t, fixture.store, provider)
	bus := eventbus.New(8)
	var refreshed, invalidated atomic.Int32
	bus.Subscribe(eventbus.MetadataRefreshed, func(eventbus.Event) { refreshed.Add(1) })
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { invalidated.Add(1) })
	bus.Start()
	recalc := &refreshRecalc{}
	service := &Service{store: fixture.store, matchSvc: matchSvc, syncSvc: recalc, bus: bus}
	service.processOnce()
	bus.Stop()
	if recalc.calls.Load() != 1 || refreshed.Load() != 1 || invalidated.Load() != 1 {
		t.Fatalf("post-commit effects: recalc=%d refreshed=%d activity=%d", recalc.calls.Load(), refreshed.Load(), invalidated.Load())
	}
}
