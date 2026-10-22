package matching

import (
	"fmt"

	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

type Candidate struct {
	Source          string
	ExternalID      int
	Title           string
	Overview        string
	Year            *int
	PosterURL       string
	Confidence      float64
	ExistingMediaID *uint
}

func (s *Service) SearchCandidates(query, mediaType string, year *int, source string) ([]Candidate, error) {
	if source == "" {
		source = s.settings.GetWithDefault(settings.KeyMetadataPrimarySource, "tmdb")
	}
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}
	return s.searchSource(query, mediaType, year, source, apiKey)
}

// SearchForLibrary searches external sources based on library media type.
// It also annotates candidates with existing media item IDs if they already exist in the library.
func (s *Service) SearchForLibrary(lib *store.Library, query string) ([]Candidate, error) {
	source := s.settings.GetWithDefault(settings.KeyMetadataPrimarySource, "tmdb")
	candidates, err := s.SearchCandidates(query, lib.MediaType, nil, source)
	if err != nil {
		return nil, err
	}

	// Check which candidates already exist in the library
	items, err := s.store.ListMediaItemsByLibrary(lib.ID)
	if err != nil {
		return nil, fmt.Errorf("listing library items: %w", err)
	}

	// Build a lookup: source+externalID → media item ID
	ids := make([]uint, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	metas, err := s.store.ListMediaMetadataByMediaItemIDs(ids)
	if err != nil {
		return nil, fmt.Errorf("listing metadata: %w", err)
	}

	type metaKey struct {
		source     string
		externalID int
	}
	existingMap := make(map[metaKey]uint, len(metas))
	for _, m := range metas {
		existingMap[metaKey{source: m.Source, externalID: m.ExternalID}] = m.MediaItemID
	}

	for i, c := range candidates {
		if mediaItemID, ok := existingMap[metaKey{source: c.Source, externalID: c.ExternalID}]; ok {
			candidates[i].ExistingMediaID = &mediaItemID
		}
	}

	return candidates, nil
}

func (s *Service) searchSource(query, mediaType string, year *int, source, apiKey string) ([]Candidate, error) {
	switch source {
	case "tmdb":
		return s.searchTMDB(apiKey, query, mediaType, year)
	case "tvdb":
		return s.searchTVDB(apiKey, query, year)
	default:
		return nil, fmt.Errorf("unknown source: %s", source)
	}
}

func (s *Service) searchTMDB(apiKey, query, mediaType string, year *int) ([]Candidate, error) {
	client := s.cachedTMDB(apiKey)

	var candidates []Candidate

	if mediaType == "movie" {
		results, err := client.SearchMovie(query, year)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			c := tmdbMovieCandidate(r)
			c.Confidence = Score(query, year, c.Title, c.Year)
			candidates = append(candidates, c)
		}
	} else {
		results, err := client.SearchTV(query, year)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			c := tmdbTVCandidate(r)
			c.Confidence = Score(query, year, c.Title, c.Year)
			candidates = append(candidates, c)
		}
	}

	return candidates, nil
}

func (s *Service) searchTVDB(apiKey, query string, year *int) ([]Candidate, error) {
	client := s.cachedTVDB(apiKey)
	results, err := client.SearchSeries(query, year)
	if err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, r := range results {
		c := tvdbSeriesCandidate(r)
		c.Confidence = Score(query, year, c.Title, c.Year)
		candidates = append(candidates, c)
	}

	return candidates, nil
}
