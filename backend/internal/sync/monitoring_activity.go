package sync

import (
	"sort"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
)

// PublishActivityInvalidation announces committed activity without making the
// lossy event bus part of the persistence boundary.
func (s *Service) PublishActivityInvalidation(mediaItemID uint) {
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
	}
}

// BuildMonitoringActivityDetails compares committed monitoring snapshots. It
// keeps stored settings separate from their effects through the parent gate.
func BuildMonitoringActivityDetails(before, after *MonitoringState) store.MediaActivityDetails {
	changes := make([]store.MediaActivityMonitoringChange, 0)

	if before.item.Monitored != after.item.Monitored {
		changes = append(changes, monitoringChange(
			store.MediaActivityTarget{Scope: store.MediaActivityScopeMedia},
			activityBoolPtr(before.item.Monitored), activityBoolPtr(after.item.Monitored),
			before.item.Monitored, after.item.Monitored,
		))
	}

	if after.item.MediaType == "series" {
		beforeEffective := before.item.Monitored && before.item.MonitorNewSeasons
		afterEffective := after.item.Monitored && after.item.MonitorNewSeasons
		if before.item.MonitorNewSeasons != after.item.MonitorNewSeasons || beforeEffective != afterEffective {
			changes = append(changes, monitoringChange(
				store.MediaActivityTarget{Scope: store.MediaActivityScopeFutureSeasons},
				activityBoolPtr(before.item.MonitorNewSeasons), activityBoolPtr(after.item.MonitorNewSeasons),
				beforeEffective, afterEffective,
			))
		}

		seasonNumbers := make(map[int]struct{}, len(before.seasons)+len(after.seasons))
		for seasonNumber := range before.seasons {
			seasonNumbers[seasonNumber] = struct{}{}
		}
		for seasonNumber := range after.seasons {
			seasonNumbers[seasonNumber] = struct{}{}
		}
		for _, seasonNumber := range sortedSeasonNumbers(seasonNumbers) {
			beforeValue, beforeOK := before.seasons[seasonNumber]
			afterValue, afterOK := after.seasons[seasonNumber]
			beforeEffective := before.item.Monitored && beforeValue
			afterEffective := after.item.Monitored && afterValue
			if sameOptionalBool(beforeValue, beforeOK, afterValue, afterOK) && beforeEffective == afterEffective {
				continue
			}
			changes = append(changes, monitoringChange(
				store.MediaActivityTarget{Scope: store.MediaActivityScopeSeason, SeasonNumber: activityIntPtr(seasonNumber)},
				optionalBoolPtr(beforeValue, beforeOK), optionalBoolPtr(afterValue, afterOK),
				beforeEffective, afterEffective,
			))
		}

		episodeKeys := make(map[episodeKey]struct{}, len(before.episodes)+len(after.episodes)+len(before.episodeOverrides)+len(after.episodeOverrides))
		addEpisodes := func(episodes []store.Episode) {
			for _, episode := range episodes {
				episodeKeys[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = struct{}{}
			}
		}
		addEpisodes(before.episodes)
		addEpisodes(after.episodes)
		for key := range before.episodeOverrides {
			episodeKeys[key] = struct{}{}
		}
		for key := range after.episodeOverrides {
			episodeKeys[key] = struct{}{}
		}

		for _, key := range sortedEpisodeKeys(episodeKeys) {
			beforeValue, beforeOK := before.episodeOverrides[key]
			afterValue, afterOK := after.episodeOverrides[key]
			beforeEffective := before.episodeNumberMonitored(key.season, key.episode)
			afterEffective := after.episodeNumberMonitored(key.season, key.episode)
			if sameOptionalBool(beforeValue, beforeOK, afterValue, afterOK) && beforeEffective == afterEffective {
				continue
			}
			changes = append(changes, monitoringChange(
				store.MediaActivityTarget{
					Scope:         store.MediaActivityScopeEpisode,
					SeasonNumber:  activityIntPtr(key.season),
					EpisodeNumber: activityIntPtr(key.episode),
				},
				optionalBoolPtr(beforeValue, beforeOK), optionalBoolPtr(afterValue, afterOK),
				beforeEffective, afterEffective,
			))
		}
	}

	total := len(changes)
	if total > store.MediaActivityMaxDetails {
		changes = changes[:store.MediaActivityMaxDetails]
	}
	return store.MediaActivityDetails{
		MonitoringChanges: changes,
		Total:             total,
		Truncated:         total > len(changes),
	}
}

func monitoringChange(target store.MediaActivityTarget, before, after *bool, effectiveBefore, effectiveAfter bool) store.MediaActivityMonitoringChange {
	return store.MediaActivityMonitoringChange{
		Target:          target,
		Before:          before,
		After:           after,
		EffectiveBefore: effectiveBefore,
		EffectiveAfter:  effectiveAfter,
	}
}

func sortedSeasonNumbers(seasons map[int]struct{}) []int {
	numbers := make([]int, 0, len(seasons))
	for seasonNumber := range seasons {
		numbers = append(numbers, seasonNumber)
	}
	sort.Ints(numbers)
	return numbers
}

func sortedEpisodeKeys(keys map[episodeKey]struct{}) []episodeKey {
	sorted := make([]episodeKey, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].season != sorted[j].season {
			return sorted[i].season < sorted[j].season
		}
		return sorted[i].episode < sorted[j].episode
	})
	return sorted
}

func sameOptionalBool(beforeValue bool, beforeOK bool, afterValue bool, afterOK bool) bool {
	return beforeOK == afterOK && (!beforeOK || beforeValue == afterValue)
}

func optionalBoolPtr(value bool, ok bool) *bool {
	if !ok {
		return nil
	}
	return activityBoolPtr(value)
}

func activityBoolPtr(value bool) *bool { return &value }

func activityIntPtr(value int) *int { return &value }
