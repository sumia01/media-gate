package media

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sumia01/media-gate/internal/activity"
	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

const mediaRemovalTargetTitleMaxBytes = 128

// Service handles cross-cutting media operations that span multiple stores
// and external systems (qBittorrent, filesystem, event bus).
type Service struct {
	store     store.Store
	syncSvc   *mediasync.Service
	bus       *eventbus.Bus
	qbit      *qbittorrent.Provider
	posterDir string
}

// NewService creates a media service.
func NewService(s store.Store, syncSvc *mediasync.Service, bus *eventbus.Bus, qbit *qbittorrent.Provider, posterDir string) *Service {
	return &Service{store: s, syncSvc: syncSvc, bus: bus, qbit: qbit, posterDir: posterDir}
}

// DeleteMediaItem removes a media item and all associated resources:
// torrents from qBittorrent, imported files from disk, poster, and DB record.
func (s *Service) DeleteMediaItem(userID, itemID uint) error {
	var item *store.MediaItem
	var downloads []store.Download
	prepared := false
	// Disable new monitor grabs and cancel every child atomically before external
	// cleanup. Preserve hashes and paths until the parent deletion cascades rows.
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateMediaActivityActor(tx, userID); err != nil {
			return err
		}
		var err error
		item, err = tx.GetMediaItem(itemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			downloads, err = tx.ListDownloads(&itemID, nil)
			return err
		}
		beforeMonitoring, err := mediasync.SnapshotMonitoring(tx, item.ID)
		if err != nil {
			return err
		}
		item.Monitored = false
		item.MonitorSearchStartedAt = nil
		item.DeletionPending = true
		if err := tx.DeleteEpisodeMonitorsByMediaItem(item.ID); err != nil {
			return err
		}
		if err := tx.UpdateMediaItem(item); err != nil {
			return err
		}
		downloads, err = tx.ListDownloads(&itemID, nil)
		if err != nil {
			return err
		}
		downloadChanges := make([]store.MediaActivityFieldChange, 0, min(len(downloads), store.MediaActivityMaxDetails))
		downloadChangeTotal := 0
		for i := range downloads {
			oldStatus := downloads[i].Status
			downloads[i].Status = "cancelled"
			if err := tx.UpdateDownload(&downloads[i]); err != nil {
				return err
			}
			if oldStatus == "cancelled" {
				continue
			}
			downloadChangeTotal++
			if len(downloadChanges) < store.MediaActivityMaxDetails {
				target := mediaRemovalDownloadTarget(tx, item, &downloads[i])
				before, after := store.BoundMediaActivityText(oldStatus, 64), "cancelled"
				downloadChanges = append(downloadChanges, store.MediaActivityFieldChange{
					Field: "download.status", Before: &before, After: &after, Target: &target,
				})
			}
		}
		afterMonitoring, err := mediasync.SnapshotMonitoring(tx, item.ID)
		if err != nil {
			return err
		}
		details := mediasync.BuildMonitoringActivityDetails(beforeMonitoring, afterMonitoring)
		details.FieldChanges = downloadChanges
		details.Total += downloadChangeTotal
		details.Truncated = details.Truncated || downloadChangeTotal > len(downloadChanges)
		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		entry, err := store.NewUserMediaActivity(
			item, userID, store.MediaActivityActionRemovalRequested, operationID,
			store.MediaActivityVisibilityShared, details,
		)
		if err != nil {
			return err
		}
		if err := tx.AppendMediaActivity(entry); err != nil {
			return err
		}
		prepared = true
		return nil
	}); err != nil {
		return fmt.Errorf("disable monitoring and cancel downloads before media deletion: %w", err)
	}
	if prepared {
		s.publishActivityInvalidation(item.ID)
	}
	slog.Info("media: deleting media item", "media_item_id", item.ID, "title", item.Title, "library_id", item.LibraryID)

	// Collect file paths before DB cascade deletes the records.
	mediaFiles, _ := s.store.ListMediaFilesByMediaItem(item.ID)

	// Remove torrents from qBittorrent (best-effort).
	if client, err := s.qbitClient(); err == nil {
		for _, dl := range downloads {
			if dl.ClientTorrentHash == "" {
				continue
			}
			if err := client.DeleteTorrent(dl.ClientTorrentHash, true); err != nil {
				slog.Warn("failed to remove torrent from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
			}
		}
	} else if len(downloads) > 0 {
		slog.Warn("qBittorrent not available, skipping torrent cleanup", "error", err)
	}

	// Determine library root to stop empty-dir cleanup.
	var libraryRoot string
	if lib, err := s.store.GetLibrary(item.LibraryID); err == nil {
		libraryRoot = lib.Path
	}

	// Remove tracked video files and collect their parent directories.
	releaseDirs := map[string]bool{}
	for _, mf := range mediaFiles {
		if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove library file", "path", mf.Path, "error", err)
		}
		releaseDirs[filepath.Dir(mf.Path)] = true
	}

	// Clean up release folders that now contain only companion files.
	for dir := range releaseDirs {
		if importer.OnlyCompanionsLeft(dir) {
			if err := os.RemoveAll(dir); err != nil {
				slog.Warn("failed to remove release dir", "path", dir, "error", err)
			}
		}
		if libraryRoot != "" {
			importer.RemoveEmptyParents(filepath.Dir(dir), libraryRoot)
		}
	}

	// Delete poster file.
	posterPath := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", item.ID))
	_ = os.Remove(posterPath)

	// Delete DB record (CASCADE removes MediaFile, Download, Episode, etc.).
	if err := s.store.DeleteMediaItem(item.ID); err != nil {
		return err
	}

	slog.Info("media: media item deleted", "media_item_id", item.ID, "title", item.Title, "downloads_removed", len(downloads), "files_removed", len(mediaFiles))
	s.bus.Publish(eventbus.MediaItemDeleted, eventbus.MediaItemPayload{
		MediaItemID: item.ID, LibraryID: item.LibraryID, Title: item.Title,
	})

	return nil
}

// DeleteDownload records cancellation before external cleanup, then atomically
// removes successfully cleaned records and records the observed result.
func (s *Service) DeleteDownload(userID, dlID uint, deleteFiles bool) error {
	var (
		dl          *store.Download
		item        *store.MediaItem
		operationID string
		targets     []store.MediaActivityTarget
		targetTotal int
	)
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateDownloadActivityActor(tx, userID); err != nil {
			return err
		}
		var err error
		dl, err = tx.GetDownload(dlID)
		if err != nil {
			return err
		}
		item, err = tx.GetMediaItem(dl.MediaItemID)
		if err != nil {
			return err
		}
		operationID, err = store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		targets, targetTotal = activity.DownloadTargets(tx, item, dl)
		oldStatus := dl.Status
		if dl.Status != "cancelled" {
			dl.Status = "cancelled"
			if err := tx.UpdateDownload(dl); err != nil {
				return fmt.Errorf("cancel download before deletion: %w", err)
			}
		}

		details := downloadRemovalActivityDetails(dl, targets, targetTotal)
		details.RequestedDeleteFiles = &deleteFiles
		details.OldStatus = oldStatus
		details.NewStatus = "cancelled"
		entry, err := store.NewUserMediaActivity(
			item, userID, store.MediaActivityActionDownloadRemovalRequest, operationID,
			store.MediaActivityVisibilityShared, details,
		)
		if err != nil {
			return err
		}
		return tx.AppendMediaActivity(entry)
	}); err != nil {
		return err
	}
	s.publishActivityInvalidation(dl.MediaItemID)

	torrentOutcome := "not_applicable"
	if dl.ClientTorrentHash != "" {
		torrentOutcome = "unavailable"
		if client, err := s.qbit.Client(); err == nil {
			torrentOutcome = "requested"
			if err := client.DeleteTorrent(dl.ClientTorrentHash, deleteFiles); err != nil {
				torrentOutcome = "failed"
				slog.Warn("failed to remove torrent from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
			}
		}
	}

	cleanup := ImportedFileCleanupResult{Outcome: "not_applicable"}
	if deleteFiles && dl.LinkedToLibrary {
		cleanup = s.CleanupImportedFiles(dl)
	}

	recordRemoved := true
	removedSubtitles := make([]store.Subtitle, 0, len(cleanup.subtitles))
	if err := s.store.WithTx(func(tx store.Store) error {
		currentDownload, err := tx.GetDownload(dl.ID)
		if err != nil {
			return err
		}
		if !sameDownloadDeletionPreimage(dl, currentDownload) {
			return store.ErrNotFound
		}
		currentItem, err := tx.GetMediaItem(dl.MediaItemID)
		if err != nil {
			return err
		}
		cleanedPaths := make(map[string]struct{}, len(cleanup.mediaFilePaths))
		for _, path := range cleanup.mediaFilePaths {
			cleanedPaths[path] = struct{}{}
		}
		removedPaths := make([]string, 0, len(cleanedPaths))
		if len(cleanedPaths) > 0 {
			currentFiles, err := tx.ListMediaFilesByMediaItem(dl.MediaItemID)
			if err != nil {
				return err
			}
			for _, file := range currentFiles {
				if _, ok := cleanedPaths[file.Path]; ok {
					removedPaths = append(removedPaths, file.Path)
				}
			}
		}
		if len(removedPaths) > 0 {
			if err := tx.DeleteMediaFilesByPaths(removedPaths); err != nil {
				return err
			}
		}
		cleanedSubtitleIDs := make(map[uint]struct{}, len(cleanup.subtitles))
		for _, subtitle := range cleanup.subtitles {
			cleanedSubtitleIDs[subtitle.ID] = struct{}{}
		}
		removedSubtitles = removedSubtitles[:0]
		if len(cleanedSubtitleIDs) > 0 {
			currentSubtitles, err := tx.ListSubtitlesByMediaItem(dl.MediaItemID)
			if err != nil {
				return err
			}
			for _, subtitle := range currentSubtitles {
				if _, ok := cleanedSubtitleIDs[subtitle.ID]; !ok {
					continue
				}
				if err := tx.DeleteSubtitle(subtitle.ID); err != nil {
					return err
				}
				removedSubtitles = append(removedSubtitles, subtitle)
			}
		}
		if err := tx.DeleteDownload(dl.ID); err != nil {
			return err
		}

		details := downloadRemovalActivityDetails(dl, targets, targetTotal)
		details.RequestedDeleteFiles = &deleteFiles
		details.RecordRemoved = &recordRemoved
		details.TorrentCleanupOutcome = torrentOutcome
		details.FileCleanupOutcome = cleanup.Outcome
		details.PhysicalFilesRemoved = cleanup.PhysicalFilesRemoved
		details.PhysicalFilesFailed = cleanup.PhysicalFilesFailed
		details.DatabaseRecordsRemoved = len(removedPaths) + len(removedSubtitles)
		entry, err := store.NewSystemMediaActivity(
			currentItem, "media", store.MediaActivityActionDownloadRemoved, operationID, details,
		)
		if err != nil {
			return err
		}
		return tx.AppendMediaActivity(entry)
	}); err != nil {
		return err
	}
	s.publishActivityInvalidation(dl.MediaItemID)

	if deleteFiles && dl.LinkedToLibrary {
		if err := s.syncSvc.RecalcMediaItemStatus(dl.MediaItemID); err != nil {
			slog.Warn("delete download: status recalc failed", "media_item_id", dl.MediaItemID, "error", err)
		}
	}

	slog.Info("media: download deleted", "download_id", dl.ID, "title", dl.Title, "media_item_id", dl.MediaItemID, "delete_files", deleteFiles)
	for i := range removedSubtitles {
		sub := removedSubtitles[i]
		s.bus.Publish(eventbus.SubtitleDeleted, eventbus.SubtitlePayload{
			SubtitleID: sub.ID, MediaItemID: sub.MediaItemID, Language: sub.Language,
			Provider: sub.Provider, FileName: sub.FileName,
		})
	}
	if deleteFiles && dl.LinkedToLibrary {
		s.bus.Publish(eventbus.DownloadDeleted, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title,
		})
	}
	return nil
}

// ImportedFileCleanupResult describes observed filesystem work. The successful
// record snapshots remain private until DeleteDownload's final transaction.
type ImportedFileCleanupResult struct {
	Outcome              string
	PhysicalFilesRemoved int
	PhysicalFilesFailed  int
	mediaFilePaths       []string
	subtitles            []store.Subtitle
}

// CleanupImportedFiles removes files imported from a download and returns the
// child records that are now safe to remove. It performs no database writes.
func (s *Service) CleanupImportedFiles(dl *store.Download) ImportedFileCleanupResult {
	result := ImportedFileCleanupResult{Outcome: "unavailable"}
	item, err := s.store.GetMediaItem(dl.MediaItemID)
	if err != nil {
		slog.Warn("cleanup: media item not found, skipping library file cleanup", "download_id", dl.ID, "error", err)
		return result
	}
	lib, err := s.store.GetLibrary(item.LibraryID)
	if err != nil {
		slog.Warn("cleanup: library not found, skipping library file cleanup", "download_id", dl.ID, "error", err)
		return result
	}

	allFiles, err := s.store.ListMediaFilesByMediaItem(item.ID)
	if err != nil {
		slog.Warn("cleanup: failed to list library files", "download_id", dl.ID, "error", err)
		return result
	}
	subtitles, err := s.store.ListSubtitlesByMediaItem(item.ID)
	if err != nil {
		slog.Warn("cleanup: failed to list subtitles", "download_id", dl.ID, "error", err)
		return result
	}

	releaseName := importer.BuildReleaseFolderName(dl.Title)
	releaseDirs := make(map[string]struct{})
	candidateFiles := make([]store.MediaFile, 0)
	for _, mf := range allFiles {
		if releaseDir, ok := trackedReleaseDir(lib.Path, mf.Path, releaseName); ok {
			candidateFiles = append(candidateFiles, mf)
			releaseDirs[releaseDir] = struct{}{}
		}
	}

	candidateSubtitles := make([]store.Subtitle, 0)
	for _, sub := range subtitles {
		if releaseDir, ok := trackedReleaseDir(lib.Path, sub.FilePath, releaseName); ok {
			candidateSubtitles = append(candidateSubtitles, sub)
			releaseDirs[releaseDir] = struct{}{}
		}
	}
	if len(candidateFiles)+len(candidateSubtitles) == 0 {
		return result
	}

	for _, mf := range candidateFiles {
		if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
			result.PhysicalFilesFailed++
			slog.Warn("cleanup: failed to remove library file", "path", mf.Path, "error", err)
			continue
		} else if err == nil {
			result.PhysicalFilesRemoved++
		}
		result.mediaFilePaths = append(result.mediaFilePaths, mf.Path)
	}

	for _, sub := range candidateSubtitles {
		if err := os.Remove(sub.FilePath); err != nil && !os.IsNotExist(err) {
			result.PhysicalFilesFailed++
			slog.Warn("cleanup: failed to remove subtitle file", "subtitle_id", sub.ID, "error", err)
			continue
		} else if err == nil {
			result.PhysicalFilesRemoved++
		}
		result.subtitles = append(result.subtitles, sub)
	}

	// A failed tracked removal must not be hidden by a recursive directory delete.
	if result.PhysicalFilesFailed == 0 {
		for releaseDir := range releaseDirs {
			if importer.OnlyCompanionsLeft(releaseDir) {
				companionCount := countFiles(releaseDir)
				if err := os.RemoveAll(releaseDir); err != nil {
					result.PhysicalFilesFailed++
					slog.Warn("cleanup: failed to remove release dir", "path", releaseDir, "error", err)
				} else {
					result.PhysicalFilesRemoved += companionCount
				}
			}
			importer.RemoveEmptyParents(filepath.Dir(releaseDir), lib.Path)
		}
	}

	result.Outcome = "completed"
	if result.PhysicalFilesFailed > 0 {
		result.Outcome = "failed"
		if len(result.mediaFilePaths)+len(result.subtitles) > 0 {
			result.Outcome = "partial"
		}
	}
	return result
}

func trackedReleaseDir(libraryRoot, storedPath, releaseName string) (string, bool) {
	root := filepath.Clean(libraryRoot)
	path := filepath.Clean(storedPath)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}

	for dir := filepath.Dir(path); dir != root; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == releaseName {
			return dir, true
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", false
}

func sameDownloadDeletionPreimage(expected, current *store.Download) bool {
	return expected != nil && current != nil && expected.ID == current.ID &&
		expected.MediaItemID == current.MediaItemID && expected.Status == current.Status &&
		expected.RetryCount == current.RetryCount && expected.UpdatedAt.Equal(current.UpdatedAt)
}

func validateDownloadActivityActor(st store.Store, userID uint) error {
	return validateMediaActivityActor(st, userID)
}

func validateMediaActivityActor(st store.Store, userID uint) error {
	if _, err := st.GetUser(userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
}

func mediaRemovalDownloadTarget(st store.Store, item *store.MediaItem, download *store.Download) store.MediaActivityTarget {
	targets, _ := activity.DownloadTargets(st, item, download)
	target := store.MediaActivityTarget{Scope: store.MediaActivityScopeUnknown, ObjectID: &download.ID}
	if item.MediaType == "movie" {
		target.Scope = store.MediaActivityScopeMedia
	}
	if len(targets) == 1 {
		target = targets[0]
	} else if len(targets) > 1 && targets[0].SeasonNumber != nil {
		target.Scope = store.MediaActivityScopeSeason
		target.SeasonNumber = targets[0].SeasonNumber
	}
	target.Title = store.BoundMediaActivityText(download.Title, mediaRemovalTargetTitleMaxBytes)
	return target
}

func (s *Service) qbitClient() (*qbittorrent.Client, error) {
	if s.qbit == nil {
		return nil, errors.New("qBittorrent provider is not configured")
	}
	return s.qbit.Client()
}

func downloadRemovalActivityDetails(dl *store.Download, targets []store.MediaActivityTarget, total int) store.MediaActivityDetails {
	details := store.MediaActivityDetails{
		ReleaseName: store.BoundMediaActivityText(dl.Title, store.MediaActivityMaxTitleBytes),
		IndexerName: store.BoundMediaActivityText(dl.IndexerName, store.MediaActivityMaxTitleBytes),
		DownloadID:  &dl.ID,
	}
	activity.ApplyDownloadTargets(&details, targets, total)
	return details
}

func countFiles(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func (s *Service) publishActivityInvalidation(mediaItemID uint) {
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
	}
}

// CleanupPostersForLibrary removes poster files for all media items in a library.
func (s *Service) CleanupPostersForLibrary(libraryID uint) error {
	items, err := s.store.ListMediaItemsByLibrary(libraryID)
	if err != nil {
		return err
	}
	for _, item := range items {
		posterPath := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", item.ID))
		_ = os.Remove(posterPath)
	}
	return nil
}
