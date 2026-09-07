package sync

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestBuildMonitoringActivityDetailsParentGateIncludesEffectiveEffects(t *testing.T) {
	episodes := []store.Episode{
		{SeasonNumber: 1, EpisodeNumber: 1},
		{SeasonNumber: 1, EpisodeNumber: 2},
	}
	before := monitoringState(false, true, episodes, map[int]bool{1: true, 2: false})
	after := monitoringState(true, true, episodes, map[int]bool{1: true, 2: false})
	before.episodeOverrides[episodeKey{season: 1, episode: 2}] = false
	before.episodeOverrides[episodeKey{season: 3, episode: 7}] = true
	after.episodeOverrides[episodeKey{season: 1, episode: 2}] = false
	after.episodeOverrides[episodeKey{season: 3, episode: 7}] = true

	details := BuildMonitoringActivityDetails(before, after)
	assertMonitoringChanges(t, details,
		wantMonitoringChange(store.MediaActivityScopeMedia, 0, 0, activityBoolPtr(false), activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeFutureSeasons, 0, 0, activityBoolPtr(true), activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeSeason, 1, 0, activityBoolPtr(true), activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 1, nil, nil, false, true),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 3, 7, activityBoolPtr(true), activityBoolPtr(true), false, true),
	)
	for _, change := range details.MonitoringChanges {
		if change.Target.Scope == store.MediaActivityScopeWholeSeries {
			t.Fatal("parent transition must not be reported as a whole-series configuration change")
		}
	}
}

func TestBuildMonitoringActivityDetailsRecordsChildConfigurationWhileParentOff(t *testing.T) {
	before := monitoringState(false, false, nil, map[int]bool{2: false})
	after := monitoringState(false, false, nil, map[int]bool{2: true})
	before.item.MonitorNewSeasons = false
	after.item.MonitorNewSeasons = true
	after.episodeOverrides[episodeKey{season: 2, episode: 4}] = true

	details := BuildMonitoringActivityDetails(before, after)
	assertMonitoringChanges(t, details,
		wantMonitoringChange(store.MediaActivityScopeFutureSeasons, 0, 0, activityBoolPtr(false), activityBoolPtr(true), false, false),
		wantMonitoringChange(store.MediaActivityScopeSeason, 2, 0, activityBoolPtr(false), activityBoolPtr(true), false, false),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 2, 4, nil, activityBoolPtr(true), false, false),
	)
}

func TestBuildMonitoringActivityDetailsRecordsOverrideRemovalFromSameValueSeasonSet(t *testing.T) {
	episodes := []store.Episode{
		{SeasonNumber: 1, EpisodeNumber: 1},
		{SeasonNumber: 1, EpisodeNumber: 2},
	}
	before := monitoringState(true, false, episodes, map[int]bool{1: true})
	after := monitoringState(true, false, episodes, map[int]bool{1: true})
	before.episodeOverrides[episodeKey{season: 1, episode: 1}] = false
	before.episodeOverrides[episodeKey{season: 1, episode: 2}] = true

	details := BuildMonitoringActivityDetails(before, after)
	assertMonitoringChanges(t, details,
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 1, activityBoolPtr(false), nil, false, true),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 2, activityBoolPtr(true), nil, true, true),
	)
}

func TestBuildMonitoringActivityDetailsRecordsParentDisableAndOverrideDeletion(t *testing.T) {
	episodes := []store.Episode{
		{SeasonNumber: 1, EpisodeNumber: 1},
		{SeasonNumber: 1, EpisodeNumber: 2},
		{SeasonNumber: 1, EpisodeNumber: 3},
	}
	before := monitoringState(true, false, episodes, map[int]bool{1: true})
	after := monitoringState(false, false, episodes, map[int]bool{1: true})
	before.episodeOverrides[episodeKey{season: 1, episode: 1}] = false
	before.episodeOverrides[episodeKey{season: 1, episode: 2}] = true

	details := BuildMonitoringActivityDetails(before, after)
	assertMonitoringChanges(t, details,
		wantMonitoringChange(store.MediaActivityScopeMedia, 0, 0, activityBoolPtr(true), activityBoolPtr(false), true, false),
		wantMonitoringChange(store.MediaActivityScopeSeason, 1, 0, activityBoolPtr(true), activityBoolPtr(true), true, false),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 1, activityBoolPtr(false), nil, false, false),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 2, activityBoolPtr(true), nil, true, false),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 3, nil, nil, true, false),
	)
}

func TestBuildMonitoringActivityDetailsUsesUnionOfSettingsEpisodesAndOverrides(t *testing.T) {
	before := monitoringState(true, false,
		[]store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}, {SeasonNumber: 4, EpisodeNumber: 1}},
		map[int]bool{1: true, 2: false},
	)
	after := monitoringState(true, false,
		[]store.Episode{{SeasonNumber: 3, EpisodeNumber: 1}, {SeasonNumber: 4, EpisodeNumber: 1}},
		map[int]bool{2: false, 3: true},
	)
	after.episodeOverrides[episodeKey{season: 5, episode: 2}] = false

	details := BuildMonitoringActivityDetails(before, after)
	assertMonitoringChanges(t, details,
		wantMonitoringChange(store.MediaActivityScopeSeason, 1, 0, activityBoolPtr(true), nil, true, false),
		wantMonitoringChange(store.MediaActivityScopeSeason, 3, 0, nil, activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 1, nil, nil, true, false),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 3, 1, nil, nil, false, true),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 5, 2, nil, activityBoolPtr(false), false, false),
	)
}

func TestBuildMonitoringActivityDetailsComparesOnlyFinalBulkState(t *testing.T) {
	episodes := []store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}}
	before := monitoringState(true, true, episodes, map[int]bool{1: true})
	after := monitoringState(true, true, episodes, map[int]bool{1: true})
	before.episodeOverrides[episodeKey{season: 1, episode: 1}] = false
	after.episodeOverrides[episodeKey{season: 1, episode: 1}] = false

	assertMonitoringChanges(t, BuildMonitoringActivityDetails(before, after))
}

func TestBuildMonitoringActivityDetailsBoundsDetailsAfterExactTotal(t *testing.T) {
	for _, tt := range []struct {
		name      string
		episodes  int
		total     int
		truncated bool
	}{
		{name: "exact limit", episodes: 48, total: 50, truncated: false},
		{name: "over limit", episodes: 60, total: 62, truncated: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			episodes := make([]store.Episode, tt.episodes)
			for i := range episodes {
				episodes[i] = store.Episode{SeasonNumber: 1, EpisodeNumber: i + 1}
			}
			before := monitoringState(false, false, episodes, map[int]bool{1: true})
			after := monitoringState(true, false, episodes, map[int]bool{1: true})

			details := BuildMonitoringActivityDetails(before, after)
			if details.Total != tt.total {
				t.Fatalf("total = %d, want %d", details.Total, tt.total)
			}
			if details.Truncated != tt.truncated {
				t.Fatalf("truncated = %t, want %t", details.Truncated, tt.truncated)
			}
			if len(details.MonitoringChanges) != store.MediaActivityMaxDetails {
				t.Fatalf("changes = %d, want %d", len(details.MonitoringChanges), store.MediaActivityMaxDetails)
			}
			last := details.MonitoringChanges[len(details.MonitoringChanges)-1].Target
			if last.Scope != store.MediaActivityScopeEpisode || last.SeasonNumber == nil || *last.SeasonNumber != 1 || last.EpisodeNumber == nil || *last.EpisodeNumber != 48 {
				t.Fatalf("last retained target = %+v, want episode S01E48", last)
			}
		})
	}
}

func TestNewInitialMonitoringStateProvidesDisabledBaseline(t *testing.T) {
	after := monitoringState(true, true, []store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}}, map[int]bool{1: true})
	before := NewInitialMonitoringState(after.item)

	details := BuildMonitoringActivityDetails(before, after)
	assertMonitoringChanges(t, details,
		wantMonitoringChange(store.MediaActivityScopeMedia, 0, 0, activityBoolPtr(false), activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeFutureSeasons, 0, 0, activityBoolPtr(false), activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeSeason, 1, 0, nil, activityBoolPtr(true), false, true),
		wantMonitoringChange(store.MediaActivityScopeEpisode, 1, 1, nil, nil, false, true),
	)
}

func wantMonitoringChange(scope string, seasonNumber, episodeNumber int, before, after *bool, effectiveBefore, effectiveAfter bool) store.MediaActivityMonitoringChange {
	target := store.MediaActivityTarget{Scope: scope}
	if seasonNumber != 0 {
		target.SeasonNumber = activityIntPtr(seasonNumber)
	}
	if episodeNumber != 0 {
		target.EpisodeNumber = activityIntPtr(episodeNumber)
	}
	return monitoringChange(target, before, after, effectiveBefore, effectiveAfter)
}

func assertMonitoringChanges(t *testing.T, details store.MediaActivityDetails, want ...store.MediaActivityMonitoringChange) {
	t.Helper()
	if details.Total != len(want) {
		t.Fatalf("total = %d, want %d; changes = %+v", details.Total, len(want), details.MonitoringChanges)
	}
	if details.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(details.MonitoringChanges) != len(want) {
		t.Fatalf("changes = %d, want %d; changes = %+v", len(details.MonitoringChanges), len(want), details.MonitoringChanges)
	}
	for i := range want {
		got := details.MonitoringChanges[i]
		if !sameMonitoringChange(got, want[i]) {
			t.Errorf("change %d = %+v, want %+v", i, got, want[i])
		}
	}
}

func sameMonitoringChange(a, b store.MediaActivityMonitoringChange) bool {
	return a.Target.Scope == b.Target.Scope &&
		sameIntPtr(a.Target.SeasonNumber, b.Target.SeasonNumber) &&
		sameIntPtr(a.Target.EpisodeNumber, b.Target.EpisodeNumber) &&
		sameBoolPtr(a.Before, b.Before) &&
		sameBoolPtr(a.After, b.After) &&
		a.EffectiveBefore == b.EffectiveBefore &&
		a.EffectiveAfter == b.EffectiveAfter
}

func sameIntPtr(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func sameBoolPtr(a, b *bool) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
