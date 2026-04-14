package subtitle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/integration/opensubtitles"
	"github.com/sumia01/media-gate/internal/ratelimit"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const defaultRateLimit = 3 // requests per second

// Service coordinates subtitle search, download, and auto-search.
type Service struct {
	store     store.Store
	settings  *settings.Service
	bus       *eventbus.Bus
	providers []Provider
}

// NewService creates a subtitle service.
func NewService(db store.Store, settingsSvc *settings.Service, bus *eventbus.Bus, providers []Provider) *Service {
	return &Service{
		store:     db,
		settings:  settingsSvc,
		bus:       bus,
		providers: providers,
	}
}

// Search queries all providers for subtitles matching the media item.
func (s *Service) Search(ctx context.Context, mediaItemID uint, seasonNumber, episodeNumber *int) ([]SearchResult, error) {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("getting media item: %w", err)
	}

	meta, err := s.store.GetMediaMetadataByMediaItem(mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}

	languages := s.getLanguages()
	if len(languages) == 0 {
		return nil, fmt.Errorf("no subtitle languages configured")
	}

	req := SearchRequest{
		Title:         item.Title,
		Year:          item.Year,
		MediaType:     item.MediaType,
		SeasonNumber:  seasonNumber,
		EpisodeNumber: episodeNumber,
		Languages:     languages,
	}

	if meta != nil {
		if meta.Source == "tmdb" {
			req.TMDbID = meta.ExternalID
		}
		if meta.ImdbID != "" {
			req.IMDbID = meta.ImdbID
		}
		if meta.Title != "" {
			req.Title = meta.Title
		}
		if meta.Year != nil {
			req.Year = meta.Year
		}
	}

	// Compute file hash if a media file exists on disk
	req.FileHash = s.computeFileHash(mediaItemID, seasonNumber, episodeNumber)

	// Find download release name for scoring
	releaseName := s.findReleaseName(mediaItemID, seasonNumber)

	limiter := s.newLimiter()
	defer limiter.Stop()

	var allResults []SearchResult
	for _, p := range s.providers {
		if err := limiter.Wait(ctx); err != nil {
			return allResults, err
		}

		results, err := p.Search(ctx, req)
		if err != nil {
			slog.Warn("subtitle provider search failed", "provider", p.Name(), "error", err)
			continue
		}

		for i := range results {
			results[i].Score = ScoreResult(&results[i], releaseName, languages)
		}
		allResults = append(allResults, results...)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	return allResults, nil
}

// DownloadOpts holds optional metadata for a subtitle download.
type DownloadOpts struct {
	Source           string // "manual" (default) or "auto"
	ReleaseName      string
	Score            int
	HearingImpaired  bool
	ForeignPartsOnly bool
}

// Download fetches a subtitle file from a provider and saves it to the library.
func (s *Service) Download(ctx context.Context, mediaItemID uint, providerName, providerFileID, language string, seasonNumber, episodeNumber *int, opts *DownloadOpts) (*store.Subtitle, error) {
	var provider Provider
	for _, p := range s.providers {
		if p.Name() == providerName {
			provider = p
			break
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("unknown subtitle provider: %s", providerName)
	}

	limiter := s.newLimiter()
	defer limiter.Stop()
	if err := limiter.Wait(ctx); err != nil {
		return nil, err
	}

	dlFile, err := provider.Download(ctx, providerFileID)
	if err != nil {
		return nil, fmt.Errorf("downloading subtitle: %w", err)
	}

	savePath, matchedFile, err := s.determineSavePath(mediaItemID, seasonNumber, episodeNumber, language, dlFile.Format)
	if err != nil {
		return nil, fmt.Errorf("determining save path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return nil, fmt.Errorf("creating subtitle directory: %w", err)
	}
	if err := os.WriteFile(savePath, dlFile.Data, 0644); err != nil {
		return nil, fmt.Errorf("writing subtitle file: %w", err)
	}

	source := "manual"
	if opts != nil && opts.Source != "" {
		source = opts.Source
	}

	sub := &store.Subtitle{
		MediaItemID:    mediaItemID,
		SeasonNumber:   seasonNumber,
		EpisodeNumber:  episodeNumber,
		Language:       language,
		Provider:       providerName,
		ProviderFileID: providerFileID,
		FileName:       filepath.Base(savePath),
		FilePath:       savePath,
		Format:         dlFile.Format,
		Source:         source,
	}

	if matchedFile != nil {
		sub.MediaFileID = &matchedFile.ID
	}

	if opts != nil {
		sub.ReleaseName = opts.ReleaseName
		sub.Score = opts.Score
		sub.HearingImpaired = opts.HearingImpaired
		sub.ForeignPartsOnly = opts.ForeignPartsOnly
	}

	if err := s.store.CreateSubtitle(sub); err != nil {
		return nil, fmt.Errorf("saving subtitle record: %w", err)
	}

	s.bus.Publish(eventbus.SubtitleDownloaded, eventbus.SubtitlePayload{
		SubtitleID:  sub.ID,
		MediaItemID: mediaItemID,
		Language:    sub.Language,
		Provider:    providerName,
		FileName:    sub.FileName,
	})

	return sub, nil
}

// List returns all subtitles for a media item.
func (s *Service) List(mediaItemID uint) ([]store.Subtitle, error) {
	return s.store.ListSubtitlesByMediaItem(mediaItemID)
}

// Delete removes a subtitle file from disk and its DB record.
func (s *Service) Delete(id uint) error {
	sub, err := s.store.GetSubtitle(id)
	if err != nil {
		return err
	}

	if sub.FilePath != "" {
		if err := os.Remove(sub.FilePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove subtitle file", "path", sub.FilePath, "error", err)
		}
	}

	if err := s.store.DeleteSubtitle(id); err != nil {
		return err
	}

	s.bus.Publish(eventbus.SubtitleDeleted, eventbus.SubtitlePayload{
		SubtitleID:  sub.ID,
		MediaItemID: sub.MediaItemID,
		Language:    sub.Language,
		Provider:    sub.Provider,
		FileName:    sub.FileName,
	})

	return nil
}

// HandleImportCompleted is an event handler for auto-searching subtitles after import.
// Subscribe to eventbus.ImportCompleted before bus.Start().
func (s *Service) HandleImportCompleted(e eventbus.Event) {
	p, ok := e.Payload.(eventbus.ImportPayload)
	if !ok {
		return
	}

	autoSearch, _ := s.settings.Get(settings.KeySubtitleAutoSearch)
	if autoSearch != "true" {
		return
	}

	languages := s.getLanguages()
	if len(languages) == 0 {
		return
	}

	// Determine season/episode from the download record
	var seasonNumber, episodeNumber *int
	dl, err := s.store.GetDownload(p.DownloadID)
	if err == nil {
		seasonNumber = dl.SeasonNumber
		if dl.EpisodeID != nil {
			// Get episode number from episodes
			episodes, _ := s.store.ListEpisodesByMediaItem(p.MediaItemID)
			for _, ep := range episodes {
				if ep.ID == *dl.EpisodeID {
					episodeNumber = &ep.EpisodeNumber
					break
				}
			}
		}
	}

	ctx := context.Background()
	results, err := s.Search(ctx, p.MediaItemID, seasonNumber, episodeNumber)
	if err != nil {
		slog.Warn("auto subtitle search failed", "mediaItemId", p.MediaItemID, "error", err)
		return
	}

	if len(results) == 0 {
		slog.Info("no subtitle results for auto-search", "mediaItemId", p.MediaItemID)
		return
	}

	// For each configured language, download the highest-scoring result
	downloaded := 0
	for _, lang := range languages {
		var best *SearchResult
		for i := range results {
			if strings.EqualFold(results[i].Language, lang) {
				best = &results[i]
				break // Already sorted by score desc
			}
		}
		if best == nil {
			continue
		}

		_, err := s.Download(ctx, p.MediaItemID, best.ProviderName, best.ProviderFileID, best.Language, seasonNumber, episodeNumber, &DownloadOpts{
			Source:           "auto",
			ReleaseName:      best.ReleaseName,
			Score:            best.Score,
			HearingImpaired:  best.HearingImpaired,
			ForeignPartsOnly: best.ForeignPartsOnly,
		})
		if err != nil {
			slog.Warn("auto subtitle download failed",
				"mediaItemId", p.MediaItemID, "language", lang, "error", err)
			continue
		}
		downloaded++
	}

	if downloaded > 0 {
		slog.Info("auto subtitle search completed",
			"mediaItemId", p.MediaItemID, "downloaded", downloaded)
		s.bus.Publish(eventbus.SubtitleAutoSearchCompleted, eventbus.SubtitlePayload{
			MediaItemID: p.MediaItemID,
		})
	}
}

// --- helpers ---

func (s *Service) getLanguages() []string {
	raw, err := s.settings.Get(settings.KeySubtitleLanguages)
	if err != nil || raw == "" {
		return nil
	}
	var langs []string
	if err := json.Unmarshal([]byte(raw), &langs); err != nil {
		// Try comma-separated fallback
		for _, l := range strings.Split(raw, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				langs = append(langs, l)
			}
		}
	}
	return langs
}

func (s *Service) findMatchingMediaFile(mediaItemID uint, seasonNumber, episodeNumber *int) *store.MediaFile {
	files, err := s.store.ListMediaFilesByMediaItem(mediaItemID)
	if err != nil {
		return nil
	}
	var best *store.MediaFile
	for i := range files {
		f := &files[i]
		if seasonNumber != nil && (f.SeasonNumber == nil || *f.SeasonNumber != *seasonNumber) {
			continue
		}
		if episodeNumber != nil && (f.EpisodeNumber == nil || *f.EpisodeNumber != *episodeNumber) {
			continue
		}
		if best == nil || f.Size > best.Size {
			best = f
		}
	}
	return best
}

func (s *Service) computeFileHash(mediaItemID uint, seasonNumber, episodeNumber *int) string {
	mf := s.findMatchingMediaFile(mediaItemID, seasonNumber, episodeNumber)
	if mf == nil {
		return ""
	}
	hash, err := opensubtitles.ComputeHash(mf.Path)
	if err != nil {
		slog.Debug("failed to compute file hash", "path", mf.Path, "error", err)
		return ""
	}
	return hash
}

func (s *Service) countExistingSubtitles(mediaItemID uint, mediaFileID uint, language string) int {
	subs, err := s.store.ListSubtitlesByMediaItem(mediaItemID)
	if err != nil {
		return 0
	}
	count := 0
	for _, sub := range subs {
		if sub.MediaFileID != nil && *sub.MediaFileID == mediaFileID && strings.EqualFold(sub.Language, language) {
			count++
		}
	}
	return count
}

func (s *Service) findReleaseName(mediaItemID uint, seasonNumber *int) string {
	downloads, err := s.store.ListDownloads(&mediaItemID, nil)
	if err != nil {
		return ""
	}
	// Find the most recent linked download
	for _, dl := range downloads {
		if dl.LinkedToLibrary && dl.Title != "" {
			if seasonNumber != nil && dl.SeasonNumber != nil && *dl.SeasonNumber != *seasonNumber {
				continue
			}
			return dl.Title
		}
	}
	// Fall back to any download
	for _, dl := range downloads {
		if dl.Title != "" {
			return dl.Title
		}
	}
	return ""
}

func (s *Service) determineSavePath(mediaItemID uint, seasonNumber, episodeNumber *int, language, format string) (string, *store.MediaFile, error) {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return "", nil, err
	}
	lib, err := s.store.GetLibrary(item.LibraryID)
	if err != nil {
		return "", nil, err
	}
	meta, _ := s.store.GetMediaMetadataByMediaItem(mediaItemID)
	targetDir := importer.BuildTargetDir(lib, item, meta, seasonNumber)

	// Try to find the matching video file
	matchedFile := s.findMatchingMediaFile(mediaItemID, seasonNumber, episodeNumber)
	if matchedFile == nil {
		// Fallback: no video file yet — use old-style placement
		fallbackName := fmt.Sprintf("subtitle.%s.%s", language, format)
		downloads, _ := s.store.ListDownloads(&mediaItemID, nil)
		for _, dl := range downloads {
			if dl.LinkedToLibrary && dl.Title != "" {
				if seasonNumber != nil && dl.SeasonNumber != nil && *dl.SeasonNumber != *seasonNumber {
					continue
				}
				releaseDir := filepath.Join(targetDir, importer.BuildReleaseFolderName(dl.Title))
				return filepath.Join(releaseDir, fallbackName), nil, nil
			}
		}
		return filepath.Join(targetDir, fallbackName), nil, nil
	}

	// Build subtitle filename from video file name
	videoDir := filepath.Dir(matchedFile.Path)
	videoBase := strings.TrimSuffix(matchedFile.FileName, filepath.Ext(matchedFile.FileName))

	seq := s.countExistingSubtitles(mediaItemID, matchedFile.ID, language)

	var subtitleFileName string
	if seq == 0 {
		subtitleFileName = fmt.Sprintf("%s.%s.%s", videoBase, language, format)
	} else {
		subtitleFileName = fmt.Sprintf("%s.%s.%02d.%s", videoBase, language, seq, format)
	}

	return filepath.Join(videoDir, subtitleFileName), matchedFile, nil
}

func (s *Service) newLimiter() *ratelimit.Limiter {
	rps := defaultRateLimit
	if raw, err := s.settings.Get(settings.KeyOpenSubtitlesRateLimit); err == nil && raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			rps = v
		}
	}
	return ratelimit.New(rps)
}
