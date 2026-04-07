package matching

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/dateutil"
	"github.com/sumia01/media-gate/internal/eventbus"
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

var ErrAlreadyExists = errors.New("media already exists in this library")

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

// StatusRecalculator recalculates a media item's status based on current files
// and episodes. Implemented by sync.Service; defined here to avoid circular imports.
type StatusRecalculator interface {
	RecalcMediaItemStatus(itemID uint) error
}

type Service struct {
	store        store.Store
	settings     *settings.Service
	posterDir    string
	httpClient   *http.Client
	statusRecalc StatusRecalculator
	bus          *eventbus.Bus

	tmdbMu     sync.Mutex
	tmdbKey    string
	tmdbCached *tmdb.Client

	tvdbMu     sync.Mutex
	tvdbKey    string
	tvdbCached *tvdb.Client
}

func NewService(s store.Store, set *settings.Service, posterDir string) *Service {
	return &Service{
		store:      s,
		settings:   set,
		posterDir:  posterDir,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetStatusRecalculator injects the status recalculator after construction
// to break the init cycle between matching and sync packages.
func (s *Service) SetStatusRecalculator(r StatusRecalculator) {
	s.statusRecalc = r
}

// SetBus injects the event bus for publishing media item events.
func (s *Service) SetBus(b *eventbus.Bus) {
	s.bus = b
}

// WithStore returns a shallow copy of the Service that uses the given store.
// Useful for running operations inside a database transaction.
func (s *Service) WithStore(st store.Store) *Service {
	return &Service{
		store:        st,
		settings:     s.settings,
		posterDir:    s.posterDir,
		httpClient:   s.httpClient,
		statusRecalc: s.statusRecalc,
		bus:          s.bus,
		tmdbKey:      s.tmdbKey,
		tmdbCached:   s.tmdbCached,
		tvdbKey:      s.tvdbKey,
		tvdbCached:   s.tvdbCached,
	}
}

func (s *Service) MatchLibrary(lib *store.Library, fullRematch bool, progressFn func(current, total int)) error {
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

	var items []store.MediaItem
	if fullRematch {
		items, err = s.store.ListMediaItemsByLibrary(lib.ID)
	} else {
		items, err = s.store.ListNewMediaItemsByLibrary(lib.ID)
	}
	if err != nil {
		return fmt.Errorf("listing items: %w", err)
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

		if err := s.matchSingleItem(&item, source, apiKey, lib.MediaType, fullRematch); err != nil {
			slog.Warn("match failed for item", "item_id", item.ID, "title", item.Title, "error", err)
		} else if item.Status == "available" {
			matched++
		}

		if progressFn != nil {
			progressFn(i+1, len(items))
		}
	}

	slog.Info("matching complete", "library", lib.Name, "total", len(items), "matched", matched)
	return nil
}

func (s *Service) matchSingleItem(item *store.MediaItem, source, apiKey, mediaType string, fullRematch bool) error {
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

	// For full rematch, clear existing metadata and episodes first
	if fullRematch {
		_ = s.store.DeleteMediaMetadataByMediaItem(item.ID)
		_ = s.store.DeleteEpisodesByMediaItem(item.ID)
	}

	// Fetch full details and save metadata
	if err := s.applyMatch(item, source, apiKey, mediaType, best.ExternalID, best.Confidence); err != nil {
		return err
	}
	s.DownloadPoster(item.ID)
	return nil
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
	}, nil
}

func (s *Service) ManualMatch(mediaItemID uint, source string, externalID int) (*store.MediaItem, *store.MediaMetadata, error) {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return nil, nil, err
	}

	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, nil, fmt.Errorf("no API key configured for %s", source)
	}

	// Delete existing metadata if any
	_ = s.store.DeleteMediaMetadataByMediaItem(mediaItemID)
	// Delete existing episodes
	_ = s.store.DeleteEpisodesByMediaItem(mediaItemID)

	if err := s.applyMatch(item, source, apiKey, item.MediaType, externalID, 1.0); err != nil {
		return nil, nil, err
	}
	s.DownloadPoster(item.ID)

	// Re-fetch updated item and metadata to return current state.
	item, err = s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return nil, nil, err
	}
	meta, _ := s.store.GetMediaMetadataByMediaItem(mediaItemID)
	return item, meta, nil
}

func (s *Service) Unmatch(mediaItemID uint) error {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return err
	}

	if err := s.store.DeleteMediaMetadataByMediaItem(mediaItemID); err != nil {
		return fmt.Errorf("deleting metadata: %w", err)
	}

	// Delete episode records
	_ = s.store.DeleteEpisodesByMediaItem(mediaItemID)

	// Delete episode monitor overrides (season monitors are kept)
	_ = s.store.DeleteEpisodeMonitorsByMediaItem(mediaItemID)

	// Requested items stay "requested" when unmatched (they have no disk presence to fall back to "new")
	if item.Source == "request" {
		item.Status = "requested"
	} else {
		item.Status = "new"
	}
	return s.store.UpdateMediaItem(item)
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

// AddMediaToLibrary creates a new requested media item with full metadata from an external source.
func (s *Service) AddMediaToLibrary(lib *store.Library, source string, externalID int) (*store.MediaItem, error) {
	// Check for duplicates
	exists, err := s.store.MediaItemExistsByExternalID(lib.ID, source, externalID)
	if err != nil {
		return nil, fmt.Errorf("checking for duplicates: %w", err)
	}
	if exists {
		return nil, ErrAlreadyExists
	}

	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}

	// Create the media item first (we need the ID for poster filename)
	item := &store.MediaItem{
		LibraryID: lib.ID,
		Title:     "pending", // will be updated from metadata
		MediaType: lib.MediaType,
		Status:    "requested",
		Source:    "request",
	}
	if err := s.store.CreateMediaItem(item); err != nil {
		return nil, fmt.Errorf("creating media item: %w", err)
	}

	// Apply match (fetches details, creates metadata — poster downloaded separately after tx commit)
	if err := s.applyMatch(item, source, apiKey, lib.MediaType, externalID, 1.0); err != nil {
		// Clean up on failure
		_ = s.store.DeleteMediaMetadataByMediaItem(item.ID)
		return nil, fmt.Errorf("applying match: %w", err)
	}

	// Update title and year from metadata
	meta, err := s.store.GetMediaMetadataByMediaItem(item.ID)
	if err == nil && meta != nil {
		item.Title = meta.Title
		item.Year = meta.Year
	}
	item.Status = "requested"
	if err := s.store.UpdateMediaItem(item); err != nil {
		return nil, fmt.Errorf("updating media item: %w", err)
	}

	return item, nil
}

// AddMediaRequest holds parameters for adding media to a library with optional
// monitoring configuration.
type AddMediaRequest struct {
	Source            string
	ExternalID        int
	Monitored         *bool
	MonitorNewSeasons *bool
	MediaProfileID    *uint
	SeasonMonitors    []SeasonMonitorReq
	EpisodeMonitors   []EpisodeMonitorReq
}

// SeasonMonitorReq represents a season monitor setting.
type SeasonMonitorReq struct {
	SeasonNumber int
	Monitored    bool
}

// EpisodeMonitorReq represents an episode monitor setting.
type EpisodeMonitorReq struct {
	SeasonNumber  int
	EpisodeNumber int
	Monitored     bool
}

// AddMediaToLibraryFull creates a media item inside a transaction, applies
// optional monitoring/profile settings and season monitors, then downloads
// the poster outside the transaction.
func (s *Service) AddMediaToLibraryFull(topStore store.Store, lib *store.Library, req AddMediaRequest) (*store.MediaItem, *store.MediaMetadata, error) {
	var resultItem *store.MediaItem
	var resultMeta *store.MediaMetadata

	err := topStore.WithTx(func(tx store.Store) error {
		txSvc := s.WithStore(tx)

		item, err := txSvc.AddMediaToLibrary(lib, req.Source, req.ExternalID)
		if err != nil {
			return err
		}

		needsUpdate := false
		if req.Monitored != nil {
			item.Monitored = *req.Monitored
			needsUpdate = true
		}
		if req.MonitorNewSeasons != nil {
			item.MonitorNewSeasons = *req.MonitorNewSeasons
			needsUpdate = true
		}
		if req.MediaProfileID != nil {
			item.MediaProfileID = req.MediaProfileID
			needsUpdate = true
		}
		if needsUpdate {
			if err := tx.UpdateMediaItem(item); err != nil {
				return err
			}
		}

		for _, sm := range req.SeasonMonitors {
			if err := tx.CreateSeasonMonitor(&store.SeasonMonitor{
				MediaItemID:  item.ID,
				SeasonNumber: sm.SeasonNumber,
				Monitored:    sm.Monitored,
			}); err != nil {
				return fmt.Errorf("creating season monitor for S%02d: %w", sm.SeasonNumber, err)
			}
		}

		for _, em := range req.EpisodeMonitors {
			if err := tx.UpsertEpisodeMonitor(&store.EpisodeMonitor{
				MediaItemID:   item.ID,
				SeasonNumber:  em.SeasonNumber,
				EpisodeNumber: em.EpisodeNumber,
				Monitored:     em.Monitored,
			}); err != nil {
				return fmt.Errorf("creating episode monitor for S%02dE%02d: %w", em.SeasonNumber, em.EpisodeNumber, err)
			}
		}

		resultItem = item
		resultMeta, _ = tx.GetMediaMetadataByMediaItem(item.ID)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Download poster outside the transaction to avoid holding a DB write lock during network I/O.
	s.DownloadPoster(resultItem.ID)

	// Re-read metadata to include poster path.
	resultMeta, _ = topStore.GetMediaMetadataByMediaItem(resultItem.ID)

	return resultItem, resultMeta, nil
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

	if item.Source != "request" {
		item.Status = "available"
	}
	if err := s.store.UpdateMediaItem(item); err != nil {
		return err
	}

	// For series with season info, fetch and store episode lists
	if mediaType == "series" && meta.Seasons != nil && *meta.Seasons > 0 {
		s.fetchAndStoreEpisodes(item, source, apiKey, externalID, *meta.Seasons)
	}

	// Recalculate status now that episodes are stored — a series with only
	// some aired episodes on disk should be "partial", not "available".
	if s.statusRecalc != nil {
		if err := s.statusRecalc.RecalcMediaItemStatus(item.ID); err != nil {
			slog.Warn("applyMatch: status recalc failed", "media_item_id", item.ID, "error", err)
		}
	}

	// Notify frontend so it can refresh the media item.
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaItemMatched, eventbus.MediaItemPayload{
			MediaItemID: item.ID,
			LibraryID:   item.LibraryID,
			Title:       item.Title,
		})
	}

	return nil
}

// DownloadPoster fetches the poster image for a media item and updates the metadata.
// This is intentionally separate from applyMatch so it can run outside a DB transaction.
func (s *Service) DownloadPoster(itemID uint) {
	meta, err := s.store.GetMediaMetadataByMediaItem(itemID)
	if err != nil || meta == nil {
		return
	}

	posterURL := s.posterURL(meta.Source, meta)
	if posterURL == "" {
		return
	}

	dest := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", itemID))
	if err := downloadPoster(s.httpClient, posterURL, dest); err != nil {
		slog.Warn("poster download failed", "item_id", itemID, "error", err)
		return
	}
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

func (s *Service) fetchAndStoreEpisodes(item *store.MediaItem, source, apiKey string, externalID, seasonCount int) {
	_ = s.store.DeleteEpisodesByMediaItem(item.ID)

	for season := 1; season <= seasonCount; season++ {
		episodes, err := s.fetchEpisodesFromSource(source, apiKey, externalID, season)
		if err != nil {
			slog.Warn("failed to fetch episodes", "item_id", item.ID, "season", season, "source", source, "error", err)
			continue
		}
		for _, ep := range episodes {
			var runtime *int
			if ep.runtime > 0 {
				r := ep.runtime
				runtime = &r
			}
			_ = s.store.CreateEpisode(&store.Episode{
				MediaItemID:   item.ID,
				SeasonNumber:  ep.seasonNumber,
				EpisodeNumber: ep.episodeNumber,
				Title:         ep.title,
				Overview:      ep.overview,
				AirDate:       ep.airDate,
				Runtime:       runtime,
			})
		}
	}
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
			c := Candidate{
				Source:     "tmdb",
				ExternalID: r.ID,
				Title:      r.Title,
				Overview:   r.Overview,
			}
			if r.PosterPath != "" {
				c.PosterURL = tmdbPosterBase + r.PosterPath
			}
			if y := dateutil.ParseYear(r.ReleaseDate); y != nil {
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
			if y := dateutil.ParseYear(r.FirstAirDate); y != nil {
				c.Year = y
			}
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
		c := Candidate{
			Source:     "tvdb",
			ExternalID: r.ID(),
			Title:      r.Name,
			Overview:   r.Overview,
			PosterURL:  r.ImageURL,
		}
		if y := dateutil.ParseYear(r.FirstAirDate); y != nil {
			c.Year = y
		}
		c.Confidence = Score(query, year, c.Title, c.Year)
		candidates = append(candidates, c)
	}

	return candidates, nil
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
		ns := len(details.Seasons)
		meta.Seasons = &ns
	}
	if y := dateutil.ParseYear(details.FirstAired); y != nil {
		meta.Year = y
	}
	meta.ReleaseDate = details.FirstAired
	meta.Credits = tvdbCharactersToJSON(details.Characters)
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

// TMDBClient returns a cached TMDB client configured with the current API key,
// or nil if no key is configured.
func (s *Service) TMDBClient() *tmdb.Client {
	apiKey, err := s.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return nil
	}
	return s.cachedTMDB(apiKey)
}

// RefreshSeriesMetadata checks TMDB/TVDB for new seasons on a matched series.
// If the season count increased, it fetches episodes for the new seasons only
// and updates the metadata. Returns true if the metadata was updated.
func (s *Service) RefreshSeriesMetadata(item *store.MediaItem, meta *store.MediaMetadata) (bool, error) {
	apiKey, err := s.resolveAPIKey(meta.Source)
	if err != nil {
		return false, err
	}

	var newSeasons int
	var newStatus string

	switch meta.Source {
	case "tmdb":
		details, err := s.cachedTMDB(apiKey).GetTV(meta.ExternalID)
		if err != nil {
			return false, fmt.Errorf("fetching TMDB TV %d: %w", meta.ExternalID, err)
		}
		newSeasons = details.NumberOfSeasons
		newStatus = details.Status
	case "tvdb":
		details, err := s.cachedTVDB(apiKey).GetSeries(meta.ExternalID)
		if err != nil {
			return false, fmt.Errorf("fetching TVDB series %d: %w", meta.ExternalID, err)
		}
		newSeasons = len(details.Seasons)
		newStatus = details.Status.Name
	default:
		return false, fmt.Errorf("unknown source: %s", meta.Source)
	}

	oldSeasons := 0
	if meta.Seasons != nil {
		oldSeasons = *meta.Seasons
	}

	statusChanged := newStatus != "" && newStatus != meta.Status
	seasonsChanged := newSeasons > oldSeasons

	if !seasonsChanged && !statusChanged {
		return false, nil
	}

	if seasonsChanged {
		for season := oldSeasons + 1; season <= newSeasons; season++ {
			episodes, err := s.fetchEpisodesFromSource(meta.Source, apiKey, meta.ExternalID, season)
			if err != nil {
				slog.Warn("metadata refresh: failed to fetch episodes",
					"item_id", item.ID, "season", season, "source", meta.Source, "error", err)
				continue
			}
			for _, ep := range episodes {
				var runtime *int
				if ep.runtime > 0 {
					r := ep.runtime
					runtime = &r
				}
				_ = s.store.CreateEpisode(&store.Episode{
					MediaItemID:   item.ID,
					SeasonNumber:  ep.seasonNumber,
					EpisodeNumber: ep.episodeNumber,
					Title:         ep.title,
					Overview:      ep.overview,
					AirDate:       ep.airDate,
					Runtime:       runtime,
				})
			}
		}
		meta.Seasons = &newSeasons
	}

	if statusChanged {
		meta.Status = newStatus
	}

	if err := s.store.UpdateMediaMetadata(meta); err != nil {
		return false, fmt.Errorf("updating metadata: %w", err)
	}

	slog.Info("metadata refreshed",
		"item_id", item.ID, "title", item.Title,
		"old_seasons", oldSeasons, "new_seasons", newSeasons,
		"status", meta.Status)

	return true, nil
}

// cachedTMDB returns a cached TMDB client for the given key, re-creating if the key changed.
func (s *Service) cachedTMDB(apiKey string) *tmdb.Client {
	s.tmdbMu.Lock()
	defer s.tmdbMu.Unlock()
	if s.tmdbCached == nil || s.tmdbKey != apiKey {
		s.tmdbKey = apiKey
		s.tmdbCached = tmdb.NewClient(apiKey)
	}
	return s.tmdbCached
}

// cachedTVDB returns a cached TVDB client for the given key, re-creating if the key changed.
func (s *Service) cachedTVDB(apiKey string) *tvdb.Client {
	s.tvdbMu.Lock()
	defer s.tvdbMu.Unlock()
	if s.tvdbCached == nil || s.tvdbKey != apiKey {
		s.tvdbKey = apiKey
		s.tvdbCached = tvdb.NewClient(apiKey)
	}
	return s.tvdbCached
}

func (s *Service) rateLimitKey(source string) string {
	if source == "tvdb" {
		return settings.KeyTVDBRateLimit
	}
	return settings.KeyTMDBRateLimit
}

func genresToJSON(genres []tmdb.Genre) string {
	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}
	b, _ := json.Marshal(names)
	return string(b)
}

type CreditPerson struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Type  string `json:"type"`
	Image string `json:"image,omitempty"`
	Order int    `json:"order"`
}

func tmdbCreditsToJSON(credits *tmdb.Credits) string {
	if credits == nil {
		return ""
	}
	var people []CreditPerson

	// Top 10 cast by order
	for i, c := range credits.Cast {
		if i >= 10 {
			break
		}
		people = append(people, CreditPerson{
			Name:  c.Name,
			Role:  c.Character,
			Type:  "cast",
			Image: c.ProfilePath,
			Order: c.Order,
		})
	}

	// Up to 5 key crew (Director, Writer, Screenplay)
	keyJobs := map[string]bool{"Director": true, "Writer": true, "Screenplay": true}
	crewCount := 0
	for _, c := range credits.Crew {
		if crewCount >= 5 {
			break
		}
		if !keyJobs[c.Job] {
			continue
		}
		people = append(people, CreditPerson{
			Name:  c.Name,
			Role:  c.Job,
			Type:  "crew",
			Image: c.ProfilePath,
			Order: crewCount,
		})
		crewCount++
	}

	if len(people) == 0 {
		return ""
	}
	b, _ := json.Marshal(people)
	return string(b)
}

func tvdbCharactersToJSON(characters []tvdb.Character) string {
	if len(characters) == 0 {
		return ""
	}
	var people []CreditPerson

	castTypes := map[string]bool{"Actor": true, "Guest Star": true}
	crewTypes := map[string]bool{"Director": true, "Writer": true}
	castCount, crewCount := 0, 0

	for _, c := range characters {
		if castTypes[c.PeopleType] && castCount < 10 {
			people = append(people, CreditPerson{
				Name:  c.PersonName,
				Role:  c.Name,
				Type:  "cast",
				Image: c.PersonImgURL,
				Order: c.Sort,
			})
			castCount++
		} else if crewTypes[c.PeopleType] && crewCount < 5 {
			people = append(people, CreditPerson{
				Name:  c.PersonName,
				Role:  c.PeopleType,
				Type:  "crew",
				Image: c.PersonImgURL,
				Order: crewCount,
			})
			crewCount++
		}
	}

	if len(people) == 0 {
		return ""
	}
	b, _ := json.Marshal(people)
	return string(b)
}
