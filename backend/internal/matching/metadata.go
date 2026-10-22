package matching

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sumia01/media-gate/internal/dateutil"
	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/store"
)

// ExternalDetail holds metadata fetched from an external source without persisting anything.
type ExternalDetail struct {
	Source     string
	ExternalID int
	Title      string
	Overview   string
	PosterURL  string
	Year       *int
	MediaType  string
	Genres     string
	Credits    string
	Status     string
	Runtime    *int
	Seasons    *int
	ImdbID     string
	TrailerURL string
	// ContentRatings is the full provider certification list as JSON, same
	// shape as store.MediaMetadata.ContentRatings. Filtering to the user's
	// selected countries happens at the API layer, as it does for library items.
	ContentRatings string
}

// GetExternalDetail fetches full metadata from TMDB/TVDB for preview without creating DB records.
func (s *Service) GetExternalDetail(source, mediaType string, externalID int) (*ExternalDetail, error) {
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}

	meta := &store.MediaMetadata{Source: source, ExternalID: externalID}

	if source == "tvdb" {
		if err := s.fetchTVDBDetails(apiKey, externalID, meta); err != nil {
			return nil, err
		}
	} else {
		if err := s.fetchTMDBDetails(apiKey, mediaType, externalID, meta); err != nil {
			return nil, err
		}
	}

	return &ExternalDetail{
		Source:     source,
		ExternalID: externalID,
		Title:      meta.Title,
		Overview:   meta.Overview,
		PosterURL:  s.posterURL(source, meta),
		Year:       meta.Year,
		MediaType:  mediaType,
		Genres:     meta.Genres,
		Credits:    meta.Credits,
		Status:     meta.Status,
		Runtime:    meta.Runtime,
		Seasons:    meta.Seasons,
		ImdbID:     meta.ImdbID,
		TrailerURL: meta.TrailerURL,
		// Already populated by the fetch above — the preview path reuses the
		// same provider mapping as a real match, it just never persists it.
		ContentRatings: meta.ContentRatings,
	}, nil
}

// fetchMatchData fetches full metadata and, for series, the episode lists for
// all seasons from the external source into in-memory structures. It performs
// NO database writes and NO other side effects, so it is safe to call BEFORE
// opening a database transaction — this keeps all network I/O out of the
// SQLite write lock. The returned metadata has no MediaItemID set;
// persistMatch assigns it.
func (s *Service) fetchMatchData(source, apiKey, mediaType string, externalID int, confidence float64) (*store.MediaMetadata, []episodeData, error) {
	meta := &store.MediaMetadata{
		Source:     source,
		ExternalID: externalID,
		Confidence: confidence,
		MatchedAt:  time.Now(),
	}

	switch source {
	case "tmdb":
		if err := s.fetchTMDBDetails(apiKey, mediaType, externalID, meta); err != nil {
			return nil, nil, err
		}
	case "tvdb":
		if err := s.fetchTVDBDetails(apiKey, externalID, meta); err != nil {
			return nil, nil, err
		}
	}

	if mediaType == "series" && meta.Seasons != nil && *meta.Seasons > 0 {
		episodes, err := s.fetchEpisodesForSeasons(source, apiKey, externalID, *meta.Seasons)
		if err != nil {
			return nil, nil, err
		}
		return meta, episodes, nil
	}

	return meta, nil, nil
}

func (s *Service) fetchTMDBDetails(apiKey, mediaType string, externalID int, meta *store.MediaMetadata) error {
	client := s.cachedTMDB(apiKey)

	if mediaType == "movie" {
		details, err := client.GetMovie(externalID)
		if err != nil {
			return fmt.Errorf("fetching TMDB movie: %w", err)
		}
		meta.Title = details.Title
		meta.Overview = details.Overview
		meta.PosterPath = details.PosterPath
		meta.Status = details.Status
		meta.ImdbID = details.ImdbID
		if details.Runtime > 0 {
			rt := details.Runtime
			meta.Runtime = &rt
		}
		if y := dateutil.ParseYear(details.ReleaseDate); y != nil {
			meta.Year = y
		}
		meta.ReleaseDate = details.ReleaseDate
		meta.Genres = genresToJSON(details.Genres)
		meta.Credits = tmdbCreditsToJSON(details.Credits)
		meta.TrailerURL = tmdb.BestTrailerURL(details.Videos)
		meta.ContentRatings = contentRatingsToJSON(tmdb.MovieCertifications(details.ReleaseDates))
	} else {
		details, err := client.GetTV(externalID)
		if err != nil {
			return fmt.Errorf("fetching TMDB TV: %w", err)
		}
		meta.Title = details.Name
		meta.Overview = details.Overview
		meta.PosterPath = details.PosterPath
		meta.Status = details.Status
		if details.ExternalIds != nil {
			meta.ImdbID = details.ExternalIds.ImdbID
		}
		if details.NumberOfSeasons > 0 {
			ns := details.NumberOfSeasons
			meta.Seasons = &ns
		}
		if y := dateutil.ParseYear(details.FirstAirDate); y != nil {
			meta.Year = y
		}
		meta.ReleaseDate = details.FirstAirDate
		meta.Genres = genresToJSON(details.Genres)
		meta.Credits = tmdbCreditsToJSON(details.Credits)
		meta.TrailerURL = tmdb.BestTrailerURL(details.Videos)
		meta.ContentRatings = contentRatingsToJSON(tmdb.TVCertifications(details.ContentRatings))
	}
	return nil
}

func (s *Service) fetchTVDBDetails(apiKey string, externalID int, meta *store.MediaMetadata) error {
	client := s.cachedTVDB(apiKey)
	details, err := client.GetSeries(externalID)
	if err != nil {
		return fmt.Errorf("fetching TVDB series: %w", err)
	}
	meta.Title = details.Name
	meta.Overview = details.Overview
	meta.PosterPath = details.Image
	meta.Status = details.Status.Name
	meta.ImdbID = details.ImdbID()
	if len(details.Seasons) > 0 {
		ns := details.MaxSeasonNumber()
		if ns > 0 {
			meta.Seasons = &ns
		}
	}
	if y := dateutil.ParseYear(details.FirstAired); y != nil {
		meta.Year = y
	}
	meta.ReleaseDate = details.FirstAired
	meta.Credits = tvdbCharactersToJSON(details.Characters)
	meta.ContentRatings = tvdbContentRatingsToJSON(details.ContentRatings)
	return nil
}

func genresToJSON(genres []tmdb.Genre) string {
	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}
	b, _ := json.Marshal(names)
	return string(b)
}
