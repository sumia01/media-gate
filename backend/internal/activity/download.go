package activity

import (
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

// DownloadTargets resolves event-time media scope using the same title parser
// as download deduplication. A missing episode ID alone never implies a pack.
func DownloadTargets(st store.Store, item *store.MediaItem, dl *store.Download) ([]store.MediaActivityTarget, int) {
	objectID := dl.ID
	parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
	episodeTargets := func(seasonNumber, start, end int) ([]store.MediaActivityTarget, int) {
		total := end - start + 1
		targets := make([]store.MediaActivityTarget, 0, min(total, store.MediaActivityMaxDetails))
		for episodeNumber := start; episodeNumber <= end && len(targets) < store.MediaActivityMaxDetails; episodeNumber++ {
			season := seasonNumber
			ep := episodeNumber
			targets = append(targets, store.MediaActivityTarget{
				Scope: store.MediaActivityScopeEpisode, SeasonNumber: &season,
				EpisodeNumber: &ep, ObjectID: &objectID,
			})
		}
		return targets, total
	}
	if dl.EpisodeID != nil {
		if episodes, err := st.ListEpisodesByMediaItem(dl.MediaItemID); err == nil {
			for _, episode := range episodes {
				if episode.ID == *dl.EpisodeID {
					if parsed.Season != nil && parsed.Episode != nil && parsed.EpisodeEnd != nil &&
						*parsed.EpisodeEnd > *parsed.Episode && *parsed.Season == episode.SeasonNumber &&
						episode.EpisodeNumber >= *parsed.Episode && episode.EpisodeNumber <= *parsed.EpisodeEnd {
						return episodeTargets(*parsed.Season, *parsed.Episode, *parsed.EpisodeEnd)
					}
					return episodeTargets(episode.SeasonNumber, episode.EpisodeNumber, episode.EpisodeNumber)
				}
			}
		}
	}
	if parsed.Season != nil && parsed.Episode != nil {
		end := *parsed.Episode
		if parsed.EpisodeEnd != nil && *parsed.EpisodeEnd > end {
			end = *parsed.EpisodeEnd
		}
		return episodeTargets(*parsed.Season, *parsed.Episode, end)
	}
	if parsed.IsSeasonPack() {
		seasonNumber := *parsed.Season
		return []store.MediaActivityTarget{{
			Scope: store.MediaActivityScopeSeason, SeasonNumber: &seasonNumber, ObjectID: &objectID,
		}}, 1
	}
	if item != nil && item.MediaType == "movie" {
		return []store.MediaActivityTarget{{Scope: store.MediaActivityScopeMedia, ObjectID: &objectID}}, 1
	}
	return []store.MediaActivityTarget{{Scope: store.MediaActivityScopeUnknown, ObjectID: &objectID}}, 1
}

func ApplyDownloadTargets(details *store.MediaActivityDetails, targets []store.MediaActivityTarget, total int) {
	details.Total = total
	details.Truncated = total > len(targets)
	if len(targets) == 1 {
		details.Target = &targets[0]
		return
	}
	details.Targets = targets
}
