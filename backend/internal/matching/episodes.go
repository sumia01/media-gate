package matching

import (
	"fmt"
	"log/slog"

	"github.com/sumia01/media-gate/internal/store"
)

type episodeKey struct {
	season  int
	episode int
}

type episodeData struct {
	seasonNumber  int
	episodeNumber int
	title         string
	overview      string
	airDate       string
	runtime       int
}

// SeasonEpisodeData holds a season's episode list fetched from an external source (no DB).
type SeasonEpisodeData struct {
	SeasonNumber  int
	TotalEpisodes int
	Episodes      []EpisodeInfo
}

// EpisodeInfo holds episode data from an external source.
type EpisodeInfo struct {
	SeasonNumber  int
	EpisodeNumber int
	Title         string
	AirDate       string
	Runtime       int
}

// FetchExternalEpisodes retrieves episode data from TMDB/TVDB without persisting anything.
func (s *Service) FetchExternalEpisodes(source string, externalID, seasonCount int) ([]SeasonEpisodeData, error) {
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}

	var result []SeasonEpisodeData
	for season := 1; season <= seasonCount; season++ {
		episodes, err := s.fetchEpisodesFromSource(source, apiKey, externalID, season)
		if err != nil {
			slog.Warn("failed to fetch external episodes", "season", season, "source", source, "error", err)
			continue
		}
		infos := make([]EpisodeInfo, len(episodes))
		for i, ep := range episodes {
			infos[i] = EpisodeInfo{
				SeasonNumber:  ep.seasonNumber,
				EpisodeNumber: ep.episodeNumber,
				Title:         ep.title,
				AirDate:       ep.airDate,
				Runtime:       ep.runtime,
			}
		}
		result = append(result, SeasonEpisodeData{
			SeasonNumber:  season,
			TotalEpisodes: len(episodes),
			Episodes:      infos,
		})
	}
	return result, nil
}

// fetchEpisodesForSeasons requires every provider window so a replacement can
// never turn a transient season failure into catalog deletion.
func (s *Service) fetchEpisodesForSeasons(source, apiKey string, externalID, seasonCount int) ([]episodeData, error) {
	var all []episodeData
	for season := 1; season <= seasonCount; season++ {
		episodes, err := s.fetchEpisodesFromSource(source, apiKey, externalID, season)
		if err != nil {
			return nil, fmt.Errorf("fetching %s season %d episodes: %w", source, season, err)
		}
		all = append(all, episodes...)
	}
	return all, nil
}

func (s *Service) fetchEpisodesFromSource(source, apiKey string, externalID, season int) ([]episodeData, error) {
	switch source {
	case "tmdb":
		client := s.cachedTMDB(apiKey)
		details, err := client.GetTVSeason(externalID, season)
		if err != nil {
			return nil, err
		}
		episodes := make([]episodeData, len(details.Episodes))
		for i, ep := range details.Episodes {
			episodes[i] = episodeData{
				seasonNumber:  ep.SeasonNumber,
				episodeNumber: ep.EpisodeNumber,
				title:         ep.Name,
				overview:      ep.Overview,
				airDate:       ep.AirDate,
				runtime:       ep.Runtime,
			}
		}
		return episodes, nil
	case "tvdb":
		client := s.cachedTVDB(apiKey)
		entries, err := client.GetSeriesEpisodes(externalID, season)
		if err != nil {
			return nil, err
		}
		episodes := make([]episodeData, len(entries))
		for i, ep := range entries {
			episodes[i] = episodeData{
				seasonNumber:  ep.SeasonNumber,
				episodeNumber: ep.Number,
				title:         ep.Name,
				overview:      ep.Overview,
				airDate:       ep.Aired,
				runtime:       ep.Runtime,
			}
		}
		return episodes, nil
	default:
		return nil, fmt.Errorf("unknown source: %s", source)
	}
}

// newEpisodeFromData converts an episodeData (provider response) into a
// store.Episode ready for persistence.
func newEpisodeFromData(mediaItemID uint, ep episodeData) *store.Episode {
	var runtime *int
	if ep.runtime > 0 {
		r := ep.runtime
		runtime = &r
	}
	return &store.Episode{
		MediaItemID:   mediaItemID,
		SeasonNumber:  ep.seasonNumber,
		EpisodeNumber: ep.episodeNumber,
		Title:         ep.title,
		Overview:      ep.overview,
		AirDate:       ep.airDate,
		Runtime:       runtime,
	}
}

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
