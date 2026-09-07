package sync

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestNewlyEnabledRequestScopesAttributesOnlyTransitions(t *testing.T) {
	episodes := []store.Episode{
		{SeasonNumber: 1, EpisodeNumber: 1},
		{SeasonNumber: 2, EpisodeNumber: 1},
		{SeasonNumber: 3, EpisodeNumber: 1},
		{SeasonNumber: 4, EpisodeNumber: 1},
	}
	attilla := monitoringState(false, false, episodes, map[int]bool{})
	agnes := monitoringState(true, false, episodes, map[int]bool{1: true, 3: true})
	agent := monitoringState(true, false, episodes, map[int]bool{1: true, 2: true, 3: true, 4: true})
	future := monitoringState(true, true, episodes, map[int]bool{1: true, 2: true, 3: true, 4: true})

	assertRequestScopes(t, newlyEnabledRequestScopes(attilla, agnes, []int{1, 2, 3, 4}, nil),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: 1},
		requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: 3},
	)
	assertRequestScopes(t, newlyEnabledRequestScopes(agnes, agent, []int{1, 2, 3, 4}, nil),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: 2},
		requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: 4},
	)
	assertRequestScopes(t, newlyEnabledRequestScopes(agent, future, nil, nil),
		requestScope{scope: store.MediaRequestScopeFutureSeasons},
		requestScope{scope: store.MediaRequestScopeMedia},
	)
}

func TestNewlyEnabledRequestScopesAvoidsIncompleteWholeSeriesAndTracksMissingEpisode(t *testing.T) {
	before := monitoringState(false, false, []store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}}, map[int]bool{})
	after := monitoringState(true, false, []store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}}, map[int]bool{1: true})
	before.seasonCount = 2
	after.seasonCount = 2
	assertRequestScopes(t, newlyEnabledRequestScopes(before, after, []int{1}, nil),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: 1},
	)

	before = monitoringState(true, false, nil, map[int]bool{})
	after = monitoringState(true, false, nil, map[int]bool{})
	after.episodeOverrides[episodeKey{season: 2, episode: 4}] = true
	assertRequestScopes(t, newlyEnabledRequestScopes(before, after, nil, []EpisodeRef{{SeasonNumber: 2, EpisodeNumber: 4}}),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: 2, episodeNumber: 4},
	)
}

func TestNewlyEnabledRequestScopesDoesNotOverstateSeasonOrEpisodeEdits(t *testing.T) {
	episodes := []store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}, {SeasonNumber: 1, EpisodeNumber: 2}}
	before := monitoringState(true, false, episodes, map[int]bool{1: false})
	before.episodeOverrides[episodeKey{season: 1, episode: 1}] = true
	after := monitoringState(true, false, episodes, map[int]bool{1: true})
	after.seasonCount = 1
	assertRequestScopes(t, newlyEnabledRequestScopes(before, after, []int{1}, nil),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: 1, episodeNumber: 2},
	)

	before = monitoringState(true, false, episodes[:1], map[int]bool{1: false})
	after = monitoringState(true, false, episodes[:1], map[int]bool{1: false})
	after.episodeOverrides[episodeKey{season: 1, episode: 1}] = true
	assertRequestScopes(t, newlyEnabledRequestScopes(before, after, nil, []EpisodeRef{{SeasonNumber: 1, EpisodeNumber: 1}}),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: 1, episodeNumber: 1},
	)
}

func TestNewlyEnabledRequestScopesUsesOverridesAndCompactsCompleteSeasonEdits(t *testing.T) {
	episodes := []store.Episode{{SeasonNumber: 1, EpisodeNumber: 1}, {SeasonNumber: 2, EpisodeNumber: 1}}
	before := monitoringState(false, false, episodes[:1], map[int]bool{1: true, 2: true})
	after := monitoringState(true, false, episodes[:1], map[int]bool{1: true, 2: true})
	before.seasonCount = 2
	after.seasonCount = 2
	after.episodeOverrides[episodeKey{season: 2, episode: 1}] = false
	assertRequestScopes(t, newlyEnabledRequestScopes(before, after, nil, nil),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: 1},
	)

	before = monitoringState(true, false, episodes, map[int]bool{1: false, 2: false})
	after = monitoringState(true, false, episodes, map[int]bool{1: true, 2: true})
	before.seasonCount = 2
	after.seasonCount = 2
	assertRequestScopes(t, newlyEnabledRequestScopes(before, after, []int{1, 2}, nil),
		requestScope{scope: store.MediaRequestScopeMedia},
		requestScope{scope: store.MediaRequestScopeWholeSeries},
	)
}

func monitoringState(monitored, monitorFuture bool, episodes []store.Episode, seasons map[int]bool) *MonitoringState {
	return &MonitoringState{
		item: store.MediaItem{
			ID:                1,
			MediaType:         "series",
			Monitored:         monitored,
			MonitorNewSeasons: monitorFuture,
		},
		seasons:          seasons,
		episodeOverrides: map[episodeKey]bool{},
		episodes:         episodes,
	}
}

func assertRequestScopes(t *testing.T, got []requestScope, want ...requestScope) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("scopes = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scope %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
