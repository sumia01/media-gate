package matching

import (
	"fmt"

	"github.com/sumia01/media-gate/internal/store"
)

// replaceMatchEpisodes runs inside the match transaction. For the same provider
// identity, retain rows with the same season/episode key: deleting and recreating
// them would clear download.EpisodeID through ON DELETE SET NULL, including an
// importer's currently owned snapshot. A different provider identity starts a
// new catalog rather than assuming its episode numbering means the same thing.
func replaceMatchEpisodes(tx store.Store, itemID uint, previous []store.Episode, candidate []episodeData, sameProvider bool) error {
	existing := make(map[episodeKey]store.Episode, len(previous))
	if sameProvider {
		for _, episode := range previous {
			existing[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = episode
		}
	} else if err := tx.DeleteEpisodesByMediaItem(itemID); err != nil {
		return fmt.Errorf("deleting previous episodes: %w", err)
	}
	for _, data := range candidate {
		episode := newEpisodeFromData(itemID, data)
		key := episodeKey{season: data.seasonNumber, episode: data.episodeNumber}
		if old, ok := existing[key]; ok {
			episode.ID, episode.CreatedAt = old.ID, old.CreatedAt
			if err := tx.UpdateEpisode(episode); err != nil {
				return fmt.Errorf("updating episode S%02dE%02d: %w", data.seasonNumber, data.episodeNumber, err)
			}
			delete(existing, key)
		} else if err := tx.CreateEpisode(episode); err != nil {
			return fmt.Errorf("creating episode S%02dE%02d: %w", data.seasonNumber, data.episodeNumber, err)
		}
	}
	// Keep deletion order deterministic and only remove keys absent from the
	// complete fetched catalog. The outer transaction rolls back on any failure.
	for _, old := range previous {
		if _, removed := existing[episodeKey{season: old.SeasonNumber, episode: old.EpisodeNumber}]; removed {
			if err := tx.DeleteEpisode(old.ID); err != nil {
				return fmt.Errorf("deleting episode S%02dE%02d: %w", old.SeasonNumber, old.EpisodeNumber, err)
			}
		}
	}
	return nil
}
