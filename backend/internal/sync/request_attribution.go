package sync

import (
	"errors"
	"sort"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
)

type episodeKey struct {
	season  int
	episode int
}

// MonitoringState is an immutable snapshot used to attribute only monitoring
// scopes that changed from disabled to enabled during one user action.
type MonitoringState struct {
	item             store.MediaItem
	seasonCount      int
	seasons          map[int]bool
	episodeOverrides map[episodeKey]bool
	episodes         []store.Episode
}

// NewInitialMonitoringState returns the disabled baseline used when media did
// not exist before an add operation.
func NewInitialMonitoringState(item store.MediaItem) *MonitoringState {
	item.Monitored = false
	item.MonitorNewSeasons = false
	return &MonitoringState{
		item:             item,
		seasons:          make(map[int]bool),
		episodeOverrides: make(map[episodeKey]bool),
	}
}

func SnapshotMonitoring(st store.Store, itemID uint) (*MonitoringState, error) {
	item, err := st.GetMediaItem(itemID)
	if err != nil {
		return nil, err
	}
	seasonMonitors, err := st.ListSeasonMonitorsByMediaItem(itemID)
	if err != nil {
		return nil, err
	}
	episodeMonitors, err := st.ListEpisodeMonitorsByMediaItem(itemID)
	if err != nil {
		return nil, err
	}
	episodes, err := st.ListEpisodesByMediaItem(itemID)
	if err != nil {
		return nil, err
	}
	state := &MonitoringState{
		item:             *item,
		seasons:          make(map[int]bool, len(seasonMonitors)),
		episodeOverrides: make(map[episodeKey]bool, len(episodeMonitors)),
		episodes:         episodes,
	}
	metadata, err := st.GetMediaMetadataByMediaItem(itemID)
	if err == nil && metadata.Seasons != nil {
		state.seasonCount = *metadata.Seasons
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	for _, monitor := range seasonMonitors {
		state.seasons[monitor.SeasonNumber] = monitor.Monitored
	}
	for _, monitor := range episodeMonitors {
		state.episodeOverrides[episodeKey{season: monitor.SeasonNumber, episode: monitor.EpisodeNumber}] = monitor.Monitored
	}
	return state, nil
}

func (s *MonitoringState) episodeMonitored(episode store.Episode) bool {
	return s.episodeNumberMonitored(episode.SeasonNumber, episode.EpisodeNumber)
}

func (s *MonitoringState) episodeNumberMonitored(seasonNumber, episodeNumber int) bool {
	if !s.item.Monitored {
		return false
	}
	if monitored, ok := s.episodeOverrides[episodeKey{season: seasonNumber, episode: episodeNumber}]; ok {
		return monitored
	}
	return s.seasons[seasonNumber]
}

func (s *MonitoringState) seasonMonitored(seasonNumber int) bool {
	return s.item.Monitored && s.seasons[seasonNumber]
}

type requestScope struct {
	scope         string
	seasonNumber  int
	episodeNumber int
}

type EpisodeRef struct {
	SeasonNumber  int
	EpisodeNumber int
}

// recordMonitoringTransitions persists attribution in the caller's monitoring
// transaction and reports whether an event should be published after commit.
func RecordMonitoringTransitions(st store.Store, userID uint, before, after *MonitoringState, changedSeasons []int, changedEpisodes []EpisodeRef) (bool, error) {
	scopes := newlyEnabledRequestScopes(before, after, changedSeasons, changedEpisodes)
	if len(scopes) == 0 {
		return false, nil
	}

	requestedAt := time.Now().UTC()
	for _, scope := range scopes {
		request := &store.MediaRequest{
			MediaItemID: after.item.ID,
			UserID:      &userID,
			Scope:       scope.scope,
			RequestedAt: requestedAt,
		}
		if scope.scope == store.MediaRequestScopeSeason || scope.scope == store.MediaRequestScopeEpisode {
			seasonNumber := scope.seasonNumber
			request.SeasonNumber = &seasonNumber
		}
		if scope.scope == store.MediaRequestScopeEpisode {
			episodeNumber := scope.episodeNumber
			request.EpisodeNumber = &episodeNumber
		}
		if err := st.CreateMediaRequest(request); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) PublishRequestAttribution(item *store.MediaItem) {
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaRequestAdded, eventbus.MediaItemPayload{
			MediaItemID: item.ID,
			LibraryID:   item.LibraryID,
			Title:       item.Title,
		})
	}
}

func newlyEnabledRequestScopes(before, after *MonitoringState, changedSeasons []int, changedEpisodes []EpisodeRef) []requestScope {
	seen := make(map[requestScope]struct{})
	add := func(scope requestScope) { seen[scope] = struct{}{} }

	if before.item.MediaType == "movie" {
		if !before.item.Monitored && after.item.Monitored {
			add(requestScope{scope: store.MediaRequestScopeMedia})
		}
		return sortedRequestScopes(seen)
	}

	if !(before.item.Monitored && before.item.MonitorNewSeasons) && after.item.Monitored && after.item.MonitorNewSeasons {
		add(requestScope{scope: store.MediaRequestScopeFutureSeasons})
	}

	transitioned := make(map[episodeKey]struct{})
	seasonEpisodes := make(map[int][]store.Episode)
	seasonNumbers := make(map[int]struct{})
	catalogEpisodes := make(map[episodeKey]struct{})
	for _, episode := range after.episodes {
		key := episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}
		seasonEpisodes[episode.SeasonNumber] = append(seasonEpisodes[episode.SeasonNumber], episode)
		seasonNumbers[episode.SeasonNumber] = struct{}{}
		catalogEpisodes[key] = struct{}{}
		if !before.episodeMonitored(episode) && after.episodeMonitored(episode) {
			transitioned[key] = struct{}{}
		}
	}
	for seasonNumber := range before.seasons {
		seasonNumbers[seasonNumber] = struct{}{}
	}
	for seasonNumber := range after.seasons {
		seasonNumbers[seasonNumber] = struct{}{}
	}
	extraEpisodes := make(map[int][]episodeKey)
	overrideKeys := make(map[episodeKey]struct{}, len(before.episodeOverrides)+len(after.episodeOverrides))
	for key := range before.episodeOverrides {
		overrideKeys[key] = struct{}{}
	}
	for key := range after.episodeOverrides {
		overrideKeys[key] = struct{}{}
	}
	for key := range overrideKeys {
		seasonNumbers[key.season] = struct{}{}
		if _, inCatalog := catalogEpisodes[key]; inCatalog {
			continue
		}
		extraEpisodes[key.season] = append(extraEpisodes[key.season], key)
		if !before.episodeNumberMonitored(key.season, key.episode) && after.episodeNumberMonitored(key.season, key.episode) {
			transitioned[key] = struct{}{}
		}
	}
	changedSeasonSet := make(map[int]struct{}, len(changedSeasons))
	for _, seasonNumber := range changedSeasons {
		changedSeasonSet[seasonNumber] = struct{}{}
		seasonNumbers[seasonNumber] = struct{}{}
	}

	seasonTransitions := make(map[int]struct{})
	episodeTransitions := make(map[episodeKey]struct{})
	for seasonNumber := range seasonNumbers {
		episodes := seasonEpisodes[seasonNumber]
		extra := extraEpisodes[seasonNumber]
		defaultTransition := !before.seasonMonitored(seasonNumber) && after.seasonMonitored(seasonNumber)
		if len(episodes) == 0 && len(extra) == 0 {
			if defaultTransition {
				seasonTransitions[seasonNumber] = struct{}{}
			}
			continue
		}
		allEpisodesTransitioned := defaultTransition
		for _, episode := range episodes {
			if _, ok := transitioned[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}]; !ok {
				allEpisodesTransitioned = false
				break
			}
		}
		for _, key := range extra {
			if _, ok := transitioned[key]; !ok {
				allEpisodesTransitioned = false
				break
			}
		}
		if allEpisodesTransitioned {
			seasonTransitions[seasonNumber] = struct{}{}
			continue
		}
		for _, episode := range episodes {
			key := episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}
			if _, ok := transitioned[key]; ok {
				episodeTransitions[key] = struct{}{}
			}
		}
		for _, key := range extra {
			if _, ok := transitioned[key]; ok {
				episodeTransitions[key] = struct{}{}
			}
		}
	}

	for _, episode := range changedEpisodes {
		key := episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}
		if !before.episodeNumberMonitored(key.season, key.episode) && after.episodeNumberMonitored(key.season, key.episode) {
			episodeTransitions[key] = struct{}{}
		}
	}

	parentTransition := !before.item.Monitored && after.item.Monitored
	allSeasonsChanged := after.seasonCount > 0
	for seasonNumber := 1; seasonNumber <= after.seasonCount; seasonNumber++ {
		if _, ok := changedSeasonSet[seasonNumber]; !ok {
			allSeasonsChanged = false
			break
		}
	}
	wholeSeries := after.seasonCount > 0 && (parentTransition || allSeasonsChanged)
	if wholeSeries {
		for seasonNumber := 1; seasonNumber <= after.seasonCount; seasonNumber++ {
			if _, ok := seasonTransitions[seasonNumber]; !ok {
				wholeSeries = false
				break
			}
		}
	}
	if wholeSeries {
		add(requestScope{scope: store.MediaRequestScopeWholeSeries})
	} else {
		for seasonNumber := range seasonTransitions {
			add(requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: seasonNumber})
		}
		for key := range episodeTransitions {
			if _, seasonAttributed := seasonTransitions[key.season]; !seasonAttributed {
				add(requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: key.season, episodeNumber: key.episode})
			}
		}
	}
	if len(seen) > 0 {
		add(requestScope{scope: store.MediaRequestScopeMedia})
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
