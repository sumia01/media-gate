package matching

import (
	"sort"
	"strconv"
	"strings"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
)

func buildMatchActivityDetails(oldMeta, newMeta *store.MediaMetadata, oldEpisodes []store.Episode, newEpisodes []episodeData, automatic bool) store.MediaActivityDetails {
	added, removed := changedEpisodeKeys(oldEpisodes, newEpisodes)
	changedTargets := append(append([]episodeKey(nil), added...), removed...)
	sortEpisodeKeys(changedTargets)
	fieldChanges := matchFieldChanges(oldMeta, newMeta)
	details := store.MediaActivityDetails{
		OldProvider:  providerIdentity(oldMeta),
		NewProvider:  providerIdentity(newMeta),
		Automatic:    &automatic,
		Added:        len(added),
		Removed:      len(removed),
		FieldChanges: fieldChanges,
		Total:        len(added) + len(removed) + len(fieldChanges),
	}
	details.Targets, details.Truncated = boundedEpisodeTargets(changedTargets, false)
	return details
}

func matchFieldChanges(oldMeta, newMeta *store.MediaMetadata) []store.MediaActivityFieldChange {
	changes := make([]store.MediaActivityFieldChange, 0, 5)
	appendChange := func(field string, before, after *string) {
		if sameActivityString(before, after) {
			return
		}
		changes = append(changes, store.MediaActivityFieldChange{Field: field, Before: before, After: after})
	}
	appendChange("title", metadataString(oldMeta, func(meta *store.MediaMetadata) string { return meta.Title }), metadataString(newMeta, func(meta *store.MediaMetadata) string { return meta.Title }))
	appendChange("year", metadataInt(oldMeta, func(meta *store.MediaMetadata) *int { return meta.Year }), metadataInt(newMeta, func(meta *store.MediaMetadata) *int { return meta.Year }))
	appendChange("status", metadataString(oldMeta, func(meta *store.MediaMetadata) string { return meta.Status }), metadataString(newMeta, func(meta *store.MediaMetadata) string { return meta.Status }))
	appendChange("season_count", metadataInt(oldMeta, func(meta *store.MediaMetadata) *int { return meta.Seasons }), metadataInt(newMeta, func(meta *store.MediaMetadata) *int { return meta.Seasons }))
	appendChange("runtime", metadataInt(oldMeta, func(meta *store.MediaMetadata) *int { return meta.Runtime }), metadataInt(newMeta, func(meta *store.MediaMetadata) *int { return meta.Runtime }))
	return changes
}

func metadataString(meta *store.MediaMetadata, value func(*store.MediaMetadata) string) *string {
	if meta == nil {
		return nil
	}
	bounded := store.BoundMediaActivityText(strings.TrimSpace(value(meta)), store.MediaActivityMaxTitleBytes)
	if bounded == "" {
		return nil
	}
	return &bounded
}

func metadataInt(meta *store.MediaMetadata, value func(*store.MediaMetadata) *int) *string {
	if meta == nil || value(meta) == nil {
		return nil
	}
	formatted := strconv.Itoa(*value(meta))
	return &formatted
}

func sameActivityString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func providerIdentity(meta *store.MediaMetadata) *store.MediaActivityProviderIdentity {
	if meta == nil {
		return nil
	}
	return &store.MediaActivityProviderIdentity{
		Source:     meta.Source,
		ExternalID: meta.ExternalID,
		Title:      store.BoundMediaActivityText(meta.Title, store.MediaActivityMaxTitleBytes),
	}
}

func changedEpisodeKeys(oldEpisodes []store.Episode, newEpisodes []episodeData) (added, removed []episodeKey) {
	oldSet := make(map[episodeKey]struct{}, len(oldEpisodes))
	newSet := make(map[episodeKey]struct{}, len(newEpisodes))
	for _, episode := range oldEpisodes {
		oldSet[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = struct{}{}
	}
	for _, episode := range newEpisodes {
		newSet[episodeKey{season: episode.seasonNumber, episode: episode.episodeNumber}] = struct{}{}
	}
	for key := range newSet {
		if _, ok := oldSet[key]; !ok {
			added = append(added, key)
		}
	}
	for key := range oldSet {
		if _, ok := newSet[key]; !ok {
			removed = append(removed, key)
		}
	}
	sortEpisodeKeys(added)
	sortEpisodeKeys(removed)
	return added, removed
}

func episodeKeysFromStored(episodes []store.Episode) []episodeKey {
	keys := make([]episodeKey, 0, len(episodes))
	for _, episode := range episodes {
		keys = append(keys, episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber})
	}
	sortEpisodeKeys(keys)
	return keys
}

func sortEpisodeKeys(keys []episodeKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].season != keys[j].season {
			return keys[i].season < keys[j].season
		}
		return keys[i].episode < keys[j].episode
	})
}

func boundedEpisodeTargets(keys []episodeKey, alreadyTruncated bool) ([]store.MediaActivityTarget, bool) {
	limit := min(len(keys), store.MediaActivityMaxDetails)
	targets := make([]store.MediaActivityTarget, limit)
	for i := 0; i < limit; i++ {
		season, episode := keys[i].season, keys[i].episode
		targets[i] = store.MediaActivityTarget{Scope: store.MediaActivityScopeEpisode, SeasonNumber: &season, EpisodeNumber: &episode}
	}
	return targets, alreadyTruncated || len(keys) > limit
}

func (s *Service) publishActivityAdded(mediaItemID uint) {
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
	}
}
