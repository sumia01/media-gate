package matching

import (
	"sort"

	"github.com/sumia01/media-gate/internal/store"
)

type requestScope struct {
	scope         string
	seasonNumber  int
	episodeNumber int
}

func normalizeSeriesRequest(mediaType string, req AddMediaRequest, episodes []episodeData, seasonCount *int) AddMediaRequest {
	if mediaType != "series" || req.Monitored == nil || !*req.Monitored {
		return req
	}
	if req.MonitorNewSeasons == nil {
		monitorNewSeasons := true
		req.MonitorNewSeasons = &monitorNewSeasons
	}
	fillKnownSeasons := len(req.SeasonMonitors) == 0
	if !fillKnownSeasons && *req.MonitorNewSeasons {
		fillKnownSeasons = true
		for _, season := range req.SeasonMonitors {
			fillKnownSeasons = fillKnownSeasons && season.Monitored
		}
		for _, episode := range req.EpisodeMonitors {
			fillKnownSeasons = fillKnownSeasons && episode.Monitored
		}
	}
	if !fillKnownSeasons {
		return req
	}
	seen := make(map[int]struct{})
	for _, season := range req.SeasonMonitors {
		seen[season.SeasonNumber] = struct{}{}
	}
	addSeason := func(seasonNumber int) {
		if _, ok := seen[seasonNumber]; ok {
			return
		}
		seen[seasonNumber] = struct{}{}
		req.SeasonMonitors = append(req.SeasonMonitors, SeasonMonitorReq{
			SeasonNumber: seasonNumber,
			Monitored:    true,
		})
	}
	if seasonCount != nil {
		for season := 1; season <= *seasonCount; season++ {
			addSeason(season)
		}
	}
	for _, episode := range episodes {
		addSeason(episode.seasonNumber)
	}
	sort.Slice(req.SeasonMonitors, func(i, j int) bool {
		return req.SeasonMonitors[i].SeasonNumber < req.SeasonMonitors[j].SeasonNumber
	})
	return req
}

func buildRequestScopes(mediaType string, req AddMediaRequest, episodes []episodeData, seasonCount *int) []requestScope {
	seen := map[requestScope]struct{}{
		requestScope{scope: store.MediaRequestScopeMedia}: {},
	}
	if mediaType != "series" || req.Monitored == nil || !*req.Monitored {
		return sortedRequestScopes(seen)
	}
	if req.MonitorNewSeasons != nil && *req.MonitorNewSeasons {
		seen[requestScope{scope: store.MediaRequestScopeFutureSeasons}] = struct{}{}
	}
	if len(req.SeasonMonitors) == 0 {
		return sortedRequestScopes(seen)
	}

	type episodeKey struct {
		season  int
		episode int
	}
	overrides := make(map[episodeKey]bool, len(req.EpisodeMonitors))
	for _, episode := range req.EpisodeMonitors {
		overrides[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = episode.Monitored
	}

	seasonEpisodes := make(map[int][]int)
	for _, episode := range episodes {
		seasonEpisodes[episode.seasonNumber] = append(seasonEpisodes[episode.seasonNumber], episode.episodeNumber)
	}
	seasonDefaults := make(map[int]bool, len(req.SeasonMonitors))
	allSubmittedSeasons := true
	for _, season := range req.SeasonMonitors {
		seasonDefaults[season.SeasonNumber] = season.Monitored
		allSubmittedSeasons = allSubmittedSeasons && season.Monitored
	}
	allKnownSeasons := true
	if seasonCount != nil {
		for seasonNumber := 1; seasonNumber <= *seasonCount; seasonNumber++ {
			if !seasonDefaults[seasonNumber] {
				allKnownSeasons = false
				break
			}
		}
	}
	allKnownEpisodes := len(episodes) > 0
	for _, episode := range episodes {
		monitored := seasonDefaults[episode.seasonNumber]
		if override, ok := overrides[episodeKey{season: episode.seasonNumber, episode: episode.episodeNumber}]; ok {
			monitored = override
		}
		if !monitored {
			allKnownEpisodes = false
			break
		}
	}
	for _, monitored := range overrides {
		if !monitored {
			allKnownEpisodes = false
			break
		}
	}
	if len(episodes) == 0 {
		allKnownEpisodes = allSubmittedSeasons
		for _, monitored := range overrides {
			allKnownEpisodes = allKnownEpisodes && monitored
		}
	}
	if allSubmittedSeasons && allKnownSeasons && allKnownEpisodes {
		seen[requestScope{scope: store.MediaRequestScopeWholeSeries}] = struct{}{}
		return sortedRequestScopes(seen)
	}

	add := func(scope requestScope) {
		seen[scope] = struct{}{}
	}
	for _, season := range req.SeasonMonitors {
		hasExclusion := false
		for key, monitored := range overrides {
			if key.season == season.SeasonNumber && !monitored {
				hasExclusion = true
				break
			}
		}
		if season.Monitored && !hasExclusion {
			add(requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: season.SeasonNumber})
			continue
		}

		knownEpisodes := seasonEpisodes[season.SeasonNumber]
		for _, episodeNumber := range knownEpisodes {
			monitored := season.Monitored
			if override, ok := overrides[episodeKey{season: season.SeasonNumber, episode: episodeNumber}]; ok {
				monitored = override
			}
			if monitored {
				add(requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: season.SeasonNumber, episodeNumber: episodeNumber})
			}
		}
		if season.Monitored && !hasExclusion && len(knownEpisodes) == 0 {
			add(requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: season.SeasonNumber})
		}
	}
	for key, monitored := range overrides {
		if monitored {
			add(requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: key.season, episodeNumber: key.episode})
		}
	}
	return sortedRequestScopes(seen)
}

func sortedRequestScopes(seen map[requestScope]struct{}) []requestScope {
	scopes := make([]requestScope, 0, len(seen))
	for scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].seasonNumber != scopes[j].seasonNumber {
			return scopes[i].seasonNumber < scopes[j].seasonNumber
		}
		if scopes[i].episodeNumber != scopes[j].episodeNumber {
			return scopes[i].episodeNumber < scopes[j].episodeNumber
		}
		return scopes[i].scope < scopes[j].scope
	})
	return scopes
}

func hasRequestedContent(mediaType string, req AddMediaRequest, scopes []requestScope) bool {
	if mediaType != "series" || req.Monitored == nil || !*req.Monitored {
		return true
	}
	for _, scope := range scopes {
		if scope.scope != store.MediaRequestScopeMedia {
			return true
		}
	}
	return false
}
