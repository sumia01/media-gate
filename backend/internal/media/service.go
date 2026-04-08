package media

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

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
func (s *Service) DeleteMediaItem(itemID uint) error {
	item, err := s.store.GetMediaItem(itemID)
	if err != nil {
		return err
	}

	// Collect file paths before DB cascade deletes the records.
	mediaFiles, _ := s.store.ListMediaFilesByMediaItem(item.ID)

	// Remove torrents from qBittorrent (best-effort).
	id := item.ID
	downloads, _ := s.store.ListDownloads(&id, nil)
	if client, err := s.qbit.Client(); err == nil {
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

	// Detach watched items so they fall back to external poster URL.
	if err := s.store.ClearWatchedMediaItemID(item.ID); err != nil {
		slog.Warn("failed to clear watched media item references", "media_item_id", item.ID, "error", err)
	}

	// Delete DB record (CASCADE removes MediaFile, Download, Episode, etc.).
	if err := s.store.DeleteMediaItem(item.ID); err != nil {
		return err
	}

	s.bus.Publish(eventbus.MediaItemDeleted, eventbus.MediaItemPayload{
		MediaItemID: item.ID, LibraryID: item.LibraryID, Title: item.Title,
	})

	return nil
}

// DeleteDownload removes a download and optionally its associated files:
// torrent from qBittorrent, imported library files from disk.
func (s *Service) DeleteDownload(dlID uint, deleteFiles bool) error {
	dl, err := s.store.GetDownload(dlID)
	if err != nil {
		return err
	}

	if dl.ClientTorrentHash != "" {
		if client, err := s.qbit.Client(); err == nil {
			if err := client.DeleteTorrent(dl.ClientTorrentHash, deleteFiles); err != nil {
				slog.Warn("failed to remove torrent from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
			}
		}
	}

	// Remove imported library files if the download was linked and deleteFiles requested.
	if deleteFiles && dl.LinkedToLibrary {
		s.CleanupImportedFiles(dl)
		if err := s.syncSvc.RecalcMediaItemStatus(dl.MediaItemID); err != nil {
			slog.Warn("delete download: status recalc failed", "media_item_id", dl.MediaItemID, "error", err)
		}
	}

	return s.store.DeleteDownload(dl.ID)
}

// CleanupImportedFiles removes library files that were imported from a specific download.
// It reconstructs the release folder path and removes matching MediaFile records + disk files.
func (s *Service) CleanupImportedFiles(dl *store.Download) {
	item, err := s.store.GetMediaItem(dl.MediaItemID)
	if err != nil {
		slog.Warn("cleanup: media item not found, skipping library file cleanup", "download_id", dl.ID, "error", err)
		return
	}
	lib, err := s.store.GetLibrary(item.LibraryID)
	if err != nil {
		slog.Warn("cleanup: library not found, skipping library file cleanup", "download_id", dl.ID, "error", err)
		return
	}

	meta, _ := s.store.GetMediaMetadataByMediaItem(dl.MediaItemID)
	targetDir := importer.BuildTargetDir(lib, item, meta, dl.SeasonNumber)
	releaseDir := filepath.Join(targetDir, importer.BuildReleaseFolderName(dl.Title))

	// Find MediaFiles belonging to this release folder.
	allFiles, _ := s.store.ListMediaFilesByMediaItem(item.ID)
	var matchedPaths []string
	prefix := releaseDir + string(filepath.Separator)
	for _, mf := range allFiles {
		if strings.HasPrefix(mf.Path, prefix) {
			if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
				slog.Warn("cleanup: failed to remove library file", "path", mf.Path, "error", err)
			}
			matchedPaths = append(matchedPaths, mf.Path)
		}
	}

	// Remove MediaFile DB records.
	if len(matchedPaths) > 0 {
		if err := s.store.DeleteMediaFilesByPaths(matchedPaths); err != nil {
			slog.Warn("cleanup: failed to delete media file records", "download_id", dl.ID, "error", err)
		}
	}

	// Remove the release folder if only companion files remain.
	if importer.OnlyCompanionsLeft(releaseDir) {
		if err := os.RemoveAll(releaseDir); err != nil {
			slog.Warn("cleanup: failed to remove release dir", "path", releaseDir, "error", err)
		}
	}

	// Clean up empty parent directories up to library root.
	importer.RemoveEmptyParents(filepath.Dir(releaseDir), lib.Path)
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
