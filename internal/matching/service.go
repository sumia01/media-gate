package matching

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/ratelimit"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	autoMatchThreshold = 0.8
	tmdbPosterBase     = "https://image.tmdb.org/t/p/w500"
)

type Candidate struct {
	Source     string
	ExternalID int
	Title      string
	Overview   string
	Year       *int
	PosterURL  string
	Confidence float64
}

type Service struct {
	store      store.Store
	settings   *settings.Service
	posterDir  string
	httpClient *http.Client
}

func NewService(s store.Store, set *settings.Service, posterDir string) *Service {
	return &Service{
		store:      s,
		settings:   set,
		posterDir:  posterDir,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Service) MatchLibrary(lib *store.Library, progressFn func(current, total int)) error {
	source := s.settings.GetWithDefault(settings.KeyMetadataPrimarySource, "tmdb")
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return fmt.Errorf("no API key configured for %s", source)
	}

	rpsStr := s.settings.GetWithDefault(s.rateLimitKey(source), "4")
	rps, _ := strconv.Atoi(rpsStr)
	if rps <= 0 {
		rps = 4
	}

	items, err := s.store.ListNewMediaItemsByLibrary(lib.ID)
	if err != nil {
		return fmt.Errorf("listing new items: %w", err)
	}

	if len(items) == 0 {
		if progressFn != nil {
			progressFn(0, 0)
		}
		return nil
	}

	limiter := ratelimit.New(rps)
	defer limiter.Stop()

	ctx := context.Background()
	matched := 0

	for i, item := range items {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}

		if err := s.matchSingleItem(&item, source, apiKey, lib.MediaType); err != nil {
			slog.Warn("match failed for item", "item_id", item.ID, "title", item.Title, "error", err)
		} else if item.Status == "matched" {
			matched++
		}

		if progressFn != nil {
			progressFn(i+1, len(items))
		}
	}

	slog.Info("matching complete", "library", lib.Name, "total", len(items), "matched", matched)
	return nil
}

func (s *Service) matchSingleItem(item *store.MediaItem, source, apiKey, mediaType string) error {
	candidates, err := s.searchSource(item.Title, mediaType, item.Year, source, apiKey)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	if best.Confidence < autoMatchThreshold {
		return nil
	}

	// Fetch full details and save metadata
	return s.applyMatch(item, source, apiKey, mediaType, best.ExternalID, best.Confidence)
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

func (s *Service) ManualMatch(mediaItemID uint, source string, externalID int) error {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return err
	}

	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return fmt.Errorf("no API key configured for %s", source)
	}

	// Delete existing metadata if any
	_ = s.store.DeleteMediaMetadataByMediaItem(mediaItemID)

	return s.applyMatch(item, source, apiKey, item.MediaType, externalID, 1.0)
}

func (s *Service) Unmatch(mediaItemID uint) error {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return err
	}

	if err := s.store.DeleteMediaMetadataByMediaItem(mediaItemID); err != nil {
		return fmt.Errorf("deleting metadata: %w", err)
	}

	item.Status = "new"
	return s.store.UpdateMediaItem(item)
}

func (s *Service) applyMatch(item *store.MediaItem, source, apiKey, mediaType string, externalID int, confidence float64) error {
	meta := &store.MediaMetadata{
		MediaItemID: item.ID,
		Source:      source,
		ExternalID:  externalID,
		Confidence:  confidence,
		MatchedAt:   time.Now(),
	}

	switch source {
	case "tmdb":
		if err := s.fetchTMDBDetails(apiKey, mediaType, externalID, meta); err != nil {
			return err
		}
	case "tvdb":
		if err := s.fetchTVDBDetails(apiKey, externalID, meta); err != nil {
			return err
		}
	}

	if err := s.store.CreateMediaMetadata(meta); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	// Download poster
	posterURL := s.posterURL(source, meta)
	if posterURL != "" {
		dest := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", item.ID))
		if err := downloadPoster(s.httpClient, posterURL, dest); err != nil {
			slog.Warn("poster download failed", "item_id", item.ID, "error", err)
		} else {
			meta.PosterPath = fmt.Sprintf("%d.jpg", item.ID)
			_ = s.store.UpdateMediaMetadata(meta)
		}
	}

	item.Status = "matched"
	return s.store.UpdateMediaItem(item)
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
	client := tmdb.NewClient(apiKey)

	var candidates []Candidate

	if mediaType == "movie" {
		results, err := client.SearchMovie(query, year)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			c := Candidate{
				Source:     "tmdb",
				ExternalID: r.ID,
				Title:      r.Title,
				Overview:   r.Overview,
			}
			if r.PosterPath != "" {
				c.PosterURL = tmdbPosterBase + r.PosterPath
			}
			if y := parseYear(r.ReleaseDate); y != nil {
				c.Year = y
			}
			c.Confidence = Score(query, year, c.Title, c.Year)
			candidates = append(candidates, c)
		}
	} else {
		results, err := client.SearchTV(query, year)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			c := Candidate{
				Source:     "tmdb",
				ExternalID: r.ID,
				Title:      r.Name,
				Overview:   r.Overview,
			}
			if r.PosterPath != "" {
				c.PosterURL = tmdbPosterBase + r.PosterPath
			}
			if y := parseYear(r.FirstAirDate); y != nil {
				c.Year = y
			}
			c.Confidence = Score(query, year, c.Title, c.Year)
			candidates = append(candidates, c)
		}
	}

	return candidates, nil
}

func (s *Service) searchTVDB(apiKey, query string, year *int) ([]Candidate, error) {
	client := tvdb.NewClient(apiKey)
	results, err := client.SearchSeries(query, year)
	if err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, r := range results {
		c := Candidate{
			Source:     "tvdb",
			ExternalID: r.ID,
			Title:      r.Name,
			Overview:   r.Overview,
			PosterURL:  r.ImageURL,
		}
		if y := parseYear(r.FirstAirDate); y != nil {
			c.Year = y
		}
		c.Confidence = Score(query, year, c.Title, c.Year)
		candidates = append(candidates, c)
	}

	return candidates, nil
}

func (s *Service) fetchTMDBDetails(apiKey, mediaType string, externalID int, meta *store.MediaMetadata) error {
	client := tmdb.NewClient(apiKey)

	if mediaType == "movie" {
		details, err := client.GetMovie(externalID)
		if err != nil {
			return fmt.Errorf("fetching TMDB movie: %w", err)
		}
		meta.Title = details.Title
		meta.Overview = details.Overview
		meta.PosterPath = details.PosterPath
		meta.Status = details.Status
		if details.Runtime > 0 {
			rt := details.Runtime
			meta.Runtime = &rt
		}
		if y := parseYear(details.ReleaseDate); y != nil {
			meta.Year = y
		}
		meta.Genres = genresToJSON(details.Genres)
	} else {
		details, err := client.GetTV(externalID)
		if err != nil {
			return fmt.Errorf("fetching TMDB TV: %w", err)
		}
		meta.Title = details.Name
		meta.Overview = details.Overview
		meta.PosterPath = details.PosterPath
		meta.Status = details.Status
		if details.NumberOfSeasons > 0 {
			ns := details.NumberOfSeasons
			meta.Seasons = &ns
		}
		if y := parseYear(details.FirstAirDate); y != nil {
			meta.Year = y
		}
		meta.Genres = genresToJSON(details.Genres)
	}
	return nil
}

func (s *Service) fetchTVDBDetails(apiKey string, externalID int, meta *store.MediaMetadata) error {
	client := tvdb.NewClient(apiKey)
	details, err := client.GetSeries(externalID)
	if err != nil {
		return fmt.Errorf("fetching TVDB series: %w", err)
	}
	meta.Title = details.Name
	meta.Overview = details.Overview
	meta.PosterPath = details.Image
	meta.Status = details.Status.Name
	if details.NumberOfSeasons > 0 {
		ns := details.NumberOfSeasons
		meta.Seasons = &ns
	}
	if y := parseYear(details.FirstAired); y != nil {
		meta.Year = y
	}
	return nil
}

func (s *Service) posterURL(source string, meta *store.MediaMetadata) string {
	if meta.PosterPath == "" {
		return ""
	}
	if source == "tmdb" {
		if strings.HasPrefix(meta.PosterPath, "/") {
			return tmdbPosterBase + meta.PosterPath
		}
		return ""
	}
	// TVDB: PosterPath is already a full URL
	return meta.PosterPath
}

func (s *Service) resolveAPIKey(source string) (string, error) {
	switch source {
	case "tmdb":
		return s.settings.Get(settings.KeyTMDBApiKey)
	case "tvdb":
		return s.settings.Get(settings.KeyTVDBApiKey)
	default:
		return "", fmt.Errorf("unknown source: %s", source)
	}
}

func (s *Service) rateLimitKey(source string) string {
	if source == "tvdb" {
		return settings.KeyTVDBRateLimit
	}
	return settings.KeyTMDBRateLimit
}

func parseYear(dateStr string) *int {
	if len(dateStr) < 4 {
		return nil
	}
	y, err := strconv.Atoi(dateStr[:4])
	if err != nil || y < 1900 || y > 2099 {
		return nil
	}
	return &y
}

func genresToJSON(genres []tmdb.Genre) string {
	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}
	b, _ := json.Marshal(names)
	return string(b)
}
