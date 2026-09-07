package subtitle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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

// subtitleLanguagePattern restricts subtitle language codes to a plausible
// ISO 639 style code (optionally with a region/script subtag), e.g. "en",
// "eng", "pt-BR". This value is embedded directly into filenames and paths
// (see determineSavePath) so it must never contain path separators or ".."
// traversal sequences.
var subtitleLanguagePattern = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z]{2,4})?$`)

// allowedSubtitleFormats is the allowlist of subtitle file extensions we will
// ever write to disk. The format is derived from a provider-supplied file
// name (see opensubtitles_adapter.go) and must never be trusted verbatim for
// path construction.
var allowedSubtitleFormats = map[string]bool{
	"srt": true,
	"ass": true,
	"ssa": true,
	"vtt": true,
	"sub": true,
}

// sanitizeLanguageCode validates a subtitle language code against a strict
// allowlist pattern, rejecting anything that isn't a plausible short language
// code (letters and an optional single hyphenated subtag only).
func sanitizeLanguageCode(language string) (string, error) {
	if !subtitleLanguagePattern.MatchString(language) {
		return "", fmt.Errorf("invalid subtitle language code: %q", language)
	}
	return language, nil
}

// sanitizeSubtitleFormat validates a subtitle file extension against an
// allowlist of known subtitle formats.
func sanitizeSubtitleFormat(format string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(format))
	if !allowedSubtitleFormats[f] {
		return "", fmt.Errorf("unsupported subtitle format: %q", format)
	}
	return f, nil
}

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
	ReleaseName      string
	Score            int
	HearingImpaired  bool
	ForeignPartsOnly bool
}

type writtenSubtitleFile struct {
	path string
	info os.FileInfo
}

// Download fetches and atomically records a user-requested subtitle download.
func (s *Service) Download(ctx context.Context, actorUserID, mediaItemID uint, providerName, providerFileID, language string, seasonNumber, episodeNumber *int, opts *DownloadOpts) (*store.Subtitle, error) {
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateSubtitleActor(tx, actorUserID); err != nil {
			return err
		}
		item, err := tx.GetMediaItem(mediaItemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		return nil
	}); err != nil {
		return nil, err
	}

	sub, written, err := s.downloadFile(ctx, mediaItemID, providerName, providerFileID, language, seasonNumber, episodeNumber, "manual", opts)
	if err != nil {
		return nil, err
	}

	err = s.store.WithTx(func(tx store.Store) error {
		if err := validateSubtitleActor(tx, actorUserID); err != nil {
			return err
		}
		item, err := tx.GetMediaItem(mediaItemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		if err := tx.CreateSubtitle(sub); err != nil {
			return fmt.Errorf("saving subtitle record: %w", err)
		}

		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		activity, err := store.NewUserMediaActivity(
			item, actorUserID, store.MediaActivityActionSubtitleDownloaded, operationID,
			store.MediaActivityVisibilityShared, subtitleActivityDetails(sub),
		)
		if err != nil {
			return err
		}
		return tx.AppendMediaActivity(activity)
	})
	if err != nil {
		s.compensateSubtitleFile(mediaItemID, written)
		return nil, err
	}

	s.publishSubtitleEvent(eventbus.SubtitleDownloaded, sub)
	s.publishActivityInvalidation(sub.MediaItemID)
	return sub, nil
}

// downloadAutomatic persists an automatic download without user activity.
func (s *Service) downloadAutomatic(ctx context.Context, mediaItemID uint, providerName, providerFileID, language string, seasonNumber, episodeNumber *int, opts *DownloadOpts) (*store.Subtitle, error) {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return nil, err
	}
	if item.DeletionPending {
		return nil, store.ErrMediaDeletionPending
	}
	sub, written, err := s.downloadFile(ctx, mediaItemID, providerName, providerFileID, language, seasonNumber, episodeNumber, "auto", opts)
	if err != nil {
		return nil, err
	}
	if err := s.store.WithTx(func(tx store.Store) error {
		item, err := tx.GetMediaItem(mediaItemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		return tx.CreateSubtitle(sub)
	}); err != nil {
		s.compensateSubtitleFile(mediaItemID, written)
		return nil, fmt.Errorf("saving subtitle record: %w", err)
	}

	s.publishSubtitleEvent(eventbus.SubtitleDownloaded, sub)
	return sub, nil
}

// downloadFile performs provider and filesystem work without opening a database transaction.
func (s *Service) downloadFile(ctx context.Context, mediaItemID uint, providerName, providerFileID, language string, seasonNumber, episodeNumber *int, source string, opts *DownloadOpts) (*store.Subtitle, *writtenSubtitleFile, error) {
	language, err := sanitizeLanguageCode(language)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid subtitle language: %w", err)
	}

	var provider Provider
	for _, p := range s.providers {
		if p.Name() == providerName {
			provider = p
			break
		}
	}
	if provider == nil {
		return nil, nil, fmt.Errorf("unknown subtitle provider: %s", providerName)
	}

	limiter := s.newLimiter()
	defer limiter.Stop()
	if err := limiter.Wait(ctx); err != nil {
		return nil, nil, err
	}

	dlFile, err := provider.Download(ctx, providerFileID)
	if err != nil {
		return nil, nil, fmt.Errorf("downloading subtitle: %w", err)
	}

	format, err := sanitizeSubtitleFormat(dlFile.Format)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid subtitle format: %w", err)
	}
	dlFile.Format = format

	savePath, matchedFile, err := s.determineSavePath(mediaItemID, seasonNumber, episodeNumber, language, format)
	if err != nil {
		return nil, nil, fmt.Errorf("determining save path: %w", err)
	}

	written, err := writeSubtitleFile(savePath, dlFile.Data)
	if err != nil {
		return nil, nil, err
	}

	sub := &store.Subtitle{
		MediaItemID:    mediaItemID,
		SeasonNumber:   copyInt(seasonNumber),
		EpisodeNumber:  copyInt(episodeNumber),
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

	return sub, written, nil
}

// List returns all subtitles for a media item.
func (s *Service) List(mediaItemID uint) ([]store.Subtitle, error) {
	return s.store.ListSubtitlesByMediaItem(mediaItemID)
}

type subtitleDeleteSnapshot struct {
	id, mediaItemID             uint
	seasonNumber, episodeNumber *int
	language, provider          string
	fileName, filePath          string
	releaseName                 string
}

type stagedSubtitleRemoval struct {
	path             string
	backupPath       string
	backupDir        string
	directoryMode    os.FileMode
	removedDirectory bool
	outcome          string
}

// Delete removes a user-selected subtitle and records the observed file outcome.
func (s *Service) Delete(actorUserID, id uint) error {
	var snapshot subtitleDeleteSnapshot
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateSubtitleActor(tx, actorUserID); err != nil {
			return err
		}
		sub, err := tx.GetSubtitle(id)
		if err != nil {
			return err
		}
		if _, err := tx.GetMediaItem(sub.MediaItemID); err != nil {
			return err
		}
		snapshot = newSubtitleDeleteSnapshot(sub)
		return nil
	}); err != nil {
		return err
	}

	fileRemoval := stageSubtitleFileRemoval(snapshot.filePath)
	var deleted *store.Subtitle
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateSubtitleActor(tx, actorUserID); err != nil {
			return err
		}
		current, err := tx.GetSubtitle(id)
		if err != nil {
			return err
		}
		if !snapshot.matches(current) {
			return store.ErrNotFound
		}
		item, err := tx.GetMediaItem(current.MediaItemID)
		if err != nil {
			return err
		}
		if err := tx.DeleteSubtitle(id); err != nil {
			return err
		}

		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		recordRemoved := true
		details := subtitleActivityDetails(current)
		details.RecordRemoved = &recordRemoved
		details.FileCleanupOutcome = fileRemoval.outcome
		activity, err := store.NewUserMediaActivity(
			item, actorUserID, store.MediaActivityActionSubtitleRemoved, operationID,
			store.MediaActivityVisibilityShared, details,
		)
		if err != nil {
			return err
		}
		if err := tx.AppendMediaActivity(activity); err != nil {
			return err
		}
		deleted = current
		return nil
	}); err != nil {
		fileRemoval.restore()
		return err
	}

	fileRemoval.discardBackup()
	s.publishSubtitleEvent(eventbus.SubtitleDeleted, deleted)
	s.publishActivityInvalidation(deleted.MediaItemID)
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

		_, err := s.downloadAutomatic(ctx, p.MediaItemID, best.ProviderName, best.ProviderFileID, best.Language, seasonNumber, episodeNumber, &DownloadOpts{
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

func validateSubtitleActor(st store.Store, userID uint) error {
	if _, err := st.GetUser(userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
}

func subtitleActivityDetails(sub *store.Subtitle) store.MediaActivityDetails {
	objectID := sub.ID
	target := store.MediaActivityTarget{Scope: store.MediaActivityScopeMedia, ObjectID: &objectID}
	if sub.EpisodeNumber != nil {
		target.Scope = store.MediaActivityScopeEpisode
		target.SeasonNumber = copyInt(sub.SeasonNumber)
		target.EpisodeNumber = copyInt(sub.EpisodeNumber)
	} else if sub.SeasonNumber != nil {
		target.Scope = store.MediaActivityScopeSeason
		target.SeasonNumber = copyInt(sub.SeasonNumber)
	}
	return store.MediaActivityDetails{
		Target:      &target,
		Language:    store.BoundMediaActivityText(sub.Language, store.MediaActivityMaxTitleBytes),
		Provider:    store.BoundMediaActivityText(sub.Provider, store.MediaActivityMaxTitleBytes),
		FileName:    safeSubtitleActivityName(sub.FileName),
		ReleaseName: safeSubtitleActivityName(sub.ReleaseName),
	}
}

func safeSubtitleActivityName(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, `\`, "/")
	return store.BoundMediaActivityText(filepath.Base(value), store.MediaActivityMaxTitleBytes)
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func writeSubtitleFile(path string, data []byte) (*writtenSubtitleFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating subtitle directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, fmt.Errorf("creating subtitle file: %w", err)
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}
	written, err := f.Write(data)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("writing subtitle file: %w", err)
	}
	if written != len(data) {
		cleanup()
		return nil, fmt.Errorf("writing subtitle file: %w", io.ErrShortWrite)
	}
	info, err := f.Stat()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("inspecting subtitle file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("closing subtitle file: %w", err)
	}
	return &writtenSubtitleFile{path: path, info: info}, nil
}

func (s *Service) compensateSubtitleFile(mediaItemID uint, written *writtenSubtitleFile) {
	if written == nil {
		return
	}
	subs, err := s.store.ListSubtitlesByMediaItem(mediaItemID)
	if err != nil {
		slog.Warn("subtitle compensation skipped: database state unavailable", "path", written.path, "error", err)
		return
	}
	for i := range subs {
		if filepath.Clean(subs[i].FilePath) == filepath.Clean(written.path) {
			return
		}
	}
	current, err := os.Lstat(written.path)
	if err != nil {
		return
	}
	if !os.SameFile(written.info, current) {
		slog.Warn("subtitle compensation skipped: file changed", "path", written.path)
		return
	}
	if err := os.Remove(written.path); err != nil && !os.IsNotExist(err) {
		slog.Warn("subtitle compensation failed", "path", written.path, "error", err)
	}
}

func newSubtitleDeleteSnapshot(sub *store.Subtitle) subtitleDeleteSnapshot {
	return subtitleDeleteSnapshot{
		id: sub.ID, mediaItemID: sub.MediaItemID,
		seasonNumber: copyInt(sub.SeasonNumber), episodeNumber: copyInt(sub.EpisodeNumber),
		language: sub.Language, provider: sub.Provider, fileName: sub.FileName,
		filePath: sub.FilePath, releaseName: sub.ReleaseName,
	}
}

func (snapshot subtitleDeleteSnapshot) matches(sub *store.Subtitle) bool {
	return sub != nil && snapshot.id == sub.ID && snapshot.mediaItemID == sub.MediaItemID &&
		equalInts(snapshot.seasonNumber, sub.SeasonNumber) && equalInts(snapshot.episodeNumber, sub.EpisodeNumber) &&
		snapshot.language == sub.Language && snapshot.provider == sub.Provider && snapshot.fileName == sub.FileName &&
		snapshot.filePath == sub.FilePath && snapshot.releaseName == sub.ReleaseName
}

func equalInts(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func stageSubtitleFileRemoval(path string) stagedSubtitleRemoval {
	removal := stagedSubtitleRemoval{path: path, outcome: "already_missing"}
	if path == "" {
		return removal
	}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return removal
		}
		slog.Warn("failed to remove subtitle file", "path", path, "error", err)
		removal.outcome = "failed"
		return removal
	}

	if info.IsDir() {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return removal
			}
			slog.Warn("failed to remove subtitle file", "path", path, "error", err)
			removal.outcome = "failed"
			return removal
		}
		removal.directoryMode = info.Mode()
		removal.removedDirectory = true
		removal.outcome = "removed"
		return removal
	}

	backupDir, err := os.MkdirTemp(filepath.Dir(path), ".mediagate-subtitle-remove-")
	if err != nil {
		slog.Warn("failed to stage subtitle file removal", "path", path, "error", err)
		removal.outcome = "failed"
		return removal
	}
	backupPath := filepath.Join(backupDir, "subtitle")
	if err := os.Rename(path, backupPath); err != nil {
		_ = os.Remove(backupDir)
		if os.IsNotExist(err) {
			return removal
		}
		slog.Warn("failed to stage subtitle file removal", "path", path, "error", err)
		removal.outcome = "failed"
		return removal
	}

	removal.backupDir = backupDir
	removal.backupPath = backupPath
	removal.outcome = "removed"
	return removal
}

func (removal stagedSubtitleRemoval) restore() {
	if removal.backupPath != "" {
		if err := os.Link(removal.backupPath, removal.path); err != nil {
			if os.IsExist(err) {
				slog.Warn("subtitle removal compensation skipped: file changed", "path", removal.path)
				removal.discardBackup()
				return
			}
			slog.Warn("subtitle removal compensation failed", "path", removal.path, "error", err)
			return
		}
		removal.discardBackup()
		return
	}

	if removal.removedDirectory {
		if err := os.Mkdir(removal.path, removal.directoryMode.Perm()); err != nil {
			if os.IsExist(err) {
				slog.Warn("subtitle removal compensation skipped: file changed", "path", removal.path)
				return
			}
			slog.Warn("subtitle removal compensation failed", "path", removal.path, "error", err)
			return
		}
		if err := os.Chmod(removal.path, removal.directoryMode); err != nil {
			slog.Warn("subtitle removal compensation could not restore directory mode", "path", removal.path, "error", err)
		}
	}
}

func (removal stagedSubtitleRemoval) discardBackup() {
	if removal.backupPath == "" {
		return
	}
	if err := os.Remove(removal.backupPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to discard staged subtitle file", "path", removal.backupPath, "error", err)
		return
	}
	if err := os.Remove(removal.backupDir); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to discard staged subtitle directory", "path", removal.backupDir, "error", err)
	}
}

func (s *Service) publishSubtitleEvent(eventType eventbus.EventType, sub *store.Subtitle) {
	s.bus.Publish(eventType, eventbus.SubtitlePayload{
		SubtitleID: sub.ID, MediaItemID: sub.MediaItemID, Language: sub.Language,
		Provider: sub.Provider, FileName: sub.FileName,
	})
}

func (s *Service) publishActivityInvalidation(mediaItemID uint) {
	s.bus.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
}

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
	// Defense-in-depth: language and format are expected to already be
	// validated by the caller (see sanitizeLanguageCode/sanitizeSubtitleFormat
	// in Download), but this function builds filesystem paths directly from
	// them, so re-validate here too in case a future caller forgets to.
	language, err := sanitizeLanguageCode(language)
	if err != nil {
		return "", nil, fmt.Errorf("invalid subtitle language: %w", err)
	}
	format, err = sanitizeSubtitleFormat(format)
	if err != nil {
		return "", nil, fmt.Errorf("invalid subtitle format: %w", err)
	}

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
	var savePath string
	if matchedFile == nil {
		// Fallback: no video file yet — use old-style placement
		fallbackName := fmt.Sprintf("subtitle.%s.%s", language, format)
		savePath = filepath.Join(targetDir, fallbackName)
		downloads, _ := s.store.ListDownloads(&mediaItemID, nil)
		for _, dl := range downloads {
			if dl.LinkedToLibrary && dl.Title != "" {
				if seasonNumber != nil && dl.SeasonNumber != nil && *dl.SeasonNumber != *seasonNumber {
					continue
				}
				releaseDir := filepath.Join(targetDir, importer.BuildReleaseFolderName(dl.Title))
				savePath = filepath.Join(releaseDir, fallbackName)
				break
			}
		}
	} else {
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

		savePath = filepath.Join(videoDir, subtitleFileName)
	}

	// Final containment guard: regardless of how savePath was constructed,
	// it must never resolve outside the configured library base path. This
	// mirrors the filepath.Clean + strings.HasPrefix guard used by the
	// library and settings services.
	if err := s.validateWithinLibraryBase(savePath); err != nil {
		return "", nil, err
	}

	return savePath, matchedFile, nil
}

// validateWithinLibraryBase ensures a computed subtitle save path stays
// within the configured library base path after cleaning, preventing path
// traversal regardless of which input (language, format, title, file name)
// might otherwise have caused it to escape.
func (s *Service) validateWithinLibraryBase(p string) error {
	base := filepath.Clean(s.settings.BasePath())
	clean := filepath.Clean(p)
	if clean != base && !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return fmt.Errorf("subtitle save path %q escapes library base path", p)
	}
	return nil
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
