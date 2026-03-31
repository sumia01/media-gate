package importer

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

const pollInterval = 10 * time.Second

// Service runs background workers for importing completed downloads into
// library directories and cleaning up torrents after seeding obligations are met.
type Service struct {
	store    store.Store
	settings *settings.Service
	syncSvc  *mediasync.Service
	bus      *eventbus.Bus
	client   *qbittorrent.Client
	mu       sync.Mutex
	stopCh   chan struct{}
}

// NewService creates a new importer service.
func NewService(s store.Store, settingsSvc *settings.Service, syncSvc *mediasync.Service, bus *eventbus.Bus) *Service {
	return &Service{
		store:    s,
		settings: settingsSvc,
		syncSvc:  syncSvc,
		bus:      bus,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background worker goroutine.
func (s *Service) Start() {
	go s.run()
	slog.Info("importer worker started", "interval", pollInterval)
}

// Stop signals the worker to shut down.
func (s *Service) Stop() {
	close(s.stopCh)
}

func (s *Service) run() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.processOnce()
		}
	}
}

func (s *Service) processOnce() {
	client, err := s.getClient()
	if err != nil {
		slog.Debug("importer: qBittorrent not configured, skipping", "error", err)
		return
	}

	s.importDownloaded(client)
	s.cleanupSeeding(client)
}

// getClient returns a cached qBittorrent client, creating one from settings on first call.
func (s *Service) getClient() (*qbittorrent.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		return s.client, nil
	}

	url, err := s.settings.Get(settings.KeyQBitURL)
	if err != nil {
		return nil, err
	}
	username, err := s.settings.Get(settings.KeyQBitUsername)
	if err != nil {
		return nil, err
	}
	password, err := s.settings.Get(settings.KeyQBitPassword)
	if err != nil {
		return nil, err
	}

	s.client = qbittorrent.NewClient(url, username, password)
	return s.client, nil
}

// importDownloaded picks up downloads in "downloaded" status, hardlinks/copies files
// into the library directory, creates MediaFile records, and advances the download status.
func (s *Service) importDownloaded(client *qbittorrent.Client) {
	status := "downloaded"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Error("importer: failed to list downloaded", "error", err)
		return
	}

	for i := range downloads {
		dl := &downloads[i]
		s.importOne(client, dl)
	}
}

func (s *Service) importOne(client *qbittorrent.Client, dl *store.Download) {
	// Mark as importing to prevent double-processing
	dl.Status = "importing"
	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("importer: failed to set importing status", "download_id", dl.ID, "error", err)
		return
	}

	// Look up media item and library
	item, err := s.store.GetMediaItem(dl.MediaItemID)
	if err != nil {
		slog.Error("importer: media item not found", "download_id", dl.ID, "media_item_id", dl.MediaItemID, "error", err)
		s.failImport(dl, "media item not found")
		return
	}

	lib, err := s.store.GetLibrary(item.LibraryID)
	if err != nil {
		slog.Error("importer: library not found", "download_id", dl.ID, "library_id", item.LibraryID, "error", err)
		s.failImport(dl, "library not found")
		return
	}

	// Get metadata for clean title/year (optional — falls back to item)
	meta, _ := s.store.GetMediaMetadataByMediaItem(dl.MediaItemID)

	// Build target directory
	targetDir := BuildTargetDir(lib, item, meta, dl.SeasonNumber)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		slog.Error("importer: failed to create target dir", "download_id", dl.ID, "path", targetDir, "error", err)
		s.failImport(dl, "failed to create target directory")
		return
	}

	// Create release subfolder to isolate this download's files
	releaseName := BuildReleaseFolderName(dl.Title)
	releaseDir := filepath.Join(targetDir, releaseName)
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		slog.Error("importer: failed to create release dir", "download_id", dl.ID, "path", releaseDir, "error", err)
		s.failImport(dl, "failed to create release directory")
		return
	}

	// Get torrent files from qBittorrent
	if dl.ClientTorrentHash == "" {
		slog.Error("importer: no torrent hash", "download_id", dl.ID)
		s.failImport(dl, "no torrent hash")
		return
	}

	files, err := client.GetTorrentFiles(dl.ClientTorrentHash)
	if err != nil {
		slog.Error("importer: failed to get torrent files", "download_id", dl.ID, "error", err)
		s.failImport(dl, "failed to get torrent files from qBittorrent")
		return
	}

	// Detect common root folder in torrent (multi-file torrents wrap files in a root dir)
	rootFolder := torrentRootFolder(files)

	// Import all non-junk files: video files get MediaFile records, companions are just linked
	imported := 0
	for _, f := range files {
		if fileparse.IsJunkFile(f.Name) {
			continue
		}

		// Strip torrent root folder to avoid double-nesting (release dir already isolates)
		relPath := f.Name
		if rootFolder != "" {
			relPath = strings.TrimPrefix(f.Name, rootFolder+"/")
		}

		srcPath := filepath.Join(dl.SavePath, f.Name)
		dstPath := filepath.Join(releaseDir, relPath)

		// Ensure subdirectories exist (e.g., Subs/)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			slog.Warn("importer: failed to create subdir", "download_id", dl.ID, "path", filepath.Dir(dstPath), "error", err)
			continue
		}

		fileName := filepath.Base(relPath)

		if fileparse.IsVideoFile(fileName) {
			dstPath = uniquePath(dstPath)
			if err := hardlinkOrCopy(srcPath, dstPath); err != nil {
				slog.Error("importer: failed to import video file",
					"download_id", dl.ID, "src", srcPath, "dst", dstPath, "error", err)
				s.failImport(dl, "failed to hardlink/copy file: "+fileName)
				return
			}

			// Parse file metadata
			info := fileparse.Parse(fileName)

			// Use download's season number as fallback for series
			seasonNum := info.SeasonNumber
			if seasonNum == nil && dl.SeasonNumber != nil {
				seasonNum = dl.SeasonNumber
			}

			// Create MediaFile record
			mf := &store.MediaFile{
				MediaItemID:   dl.MediaItemID,
				Path:          dstPath,
				FileName:      fileName,
				Size:          f.Size,
				Resolution:    info.Resolution,
				SourceType:    info.SourceType,
				SeasonNumber:  seasonNum,
				EpisodeNumber: info.EpisodeNumber,
				AddedAt:       time.Now(),
			}

			if err := s.store.CreateMediaFile(mf); err != nil {
				slog.Warn("importer: failed to create media file record",
					"download_id", dl.ID, "path", dstPath, "error", err)
				// Continue — file is on disk, record may already exist from a previous attempt
			} else {
				imported++
			}
		} else {
			// Companion file (subtitle, nfo, image, etc.) — link but don't track in DB
			if err := hardlinkOrCopy(srcPath, dstPath); err != nil {
				slog.Warn("importer: failed to import companion file",
					"download_id", dl.ID, "src", srcPath, "dst", dstPath, "error", err)
				// Non-fatal — continue with other files
			}
		}
	}

	if imported == 0 {
		slog.Warn("importer: no video files found in torrent", "download_id", dl.ID, "title", dl.Title)
	}

	// Mark as linked to library
	dl.LinkedToLibrary = true

	// Check if seeding is required
	if s.needsSeeding(dl) {
		dl.Status = "seeding"
	} else {
		dl.Status = "completed"
		now := time.Now()
		dl.CompletedAt = &now
		// No seeding required — remove torrent from qBit
		if dl.ClientTorrentHash != "" {
			if err := client.DeleteTorrent(dl.ClientTorrentHash, true); err != nil {
				slog.Warn("importer: failed to delete torrent from qBit",
					"download_id", dl.ID, "hash", dl.ClientTorrentHash, "error", err)
			}
		}
	}

	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("importer: failed to update download after import", "download_id", dl.ID, "error", err)
	}

	// Resync the media item to pick up fresh file metadata.
	if _, _, _, err := s.syncSvc.ResyncMediaItem(dl.MediaItemID); err != nil {
		slog.Warn("importer: resync after import failed", "download_id", dl.ID, "media_item_id", dl.MediaItemID, "error", err)
	}

	// Recalculate media item status based on current files.
	if err := s.syncSvc.RecalcMediaItemStatus(dl.MediaItemID); err != nil {
		slog.Warn("importer: status recalc failed", "download_id", dl.ID, "media_item_id", dl.MediaItemID, "error", err)
	}

	slog.Info("importer: import complete",
		"download_id", dl.ID, "title", dl.Title, "files", imported, "status", dl.Status)

	s.bus.Publish(eventbus.ImportCompleted, eventbus.ImportPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, FilesCount: imported,
	})
}

// needsSeeding checks if the indexer has any seeding requirements.
func (s *Service) needsSeeding(dl *store.Download) bool {
	idx, err := s.store.GetIndexer(dl.IndexerID)
	if err != nil {
		// Indexer deleted — no seeding needed
		return false
	}
	return idx.SeedMinRatio > 0 || idx.SeedMinTime > 0
}

// failImport sets a download to import_failed status.
func (s *Service) failImport(dl *store.Download, reason string) {
	dl.Status = "import_failed"
	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("importer: failed to set import_failed status", "download_id", dl.ID, "error", err)
	}
	slog.Warn("importer: import failed", "download_id", dl.ID, "title", dl.Title, "reason", reason)
	s.bus.Publish(eventbus.ImportFailed, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "import_failed",
	})
}

// cleanupSeeding checks seeding downloads and removes torrents when obligations are met.
func (s *Service) cleanupSeeding(client *qbittorrent.Client) {
	status := "seeding"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Error("importer: failed to list seeding downloads", "error", err)
		return
	}

	for i := range downloads {
		dl := &downloads[i]
		if !dl.LinkedToLibrary {
			continue
		}

		if dl.ClientTorrentHash == "" {
			s.completeDownload(dl, client)
			continue
		}

		info, err := client.GetTorrent(dl.ClientTorrentHash)
		if err != nil {
			if err == qbittorrent.ErrTorrentNotFound {
				// Torrent removed externally — mark completed
				s.completeDownload(dl, nil)
				continue
			}
			slog.Error("importer: failed to get torrent info for seeding check",
				"download_id", dl.ID, "error", err)
			continue
		}

		if s.seedingComplete(dl, info) {
			s.completeDownload(dl, client)
		}
	}
}

// seedingComplete checks if the indexer's seeding requirements have been met.
func (s *Service) seedingComplete(dl *store.Download, info *qbittorrent.TorrentInfo) bool {
	idx, err := s.store.GetIndexer(dl.IndexerID)
	if err != nil {
		// Indexer deleted — consider seeding complete
		return true
	}

	ratioMet := idx.SeedMinRatio <= 0 || info.Ratio >= idx.SeedMinRatio
	timeMet := idx.SeedMinTime <= 0 || info.SeedingTime >= idx.SeedMinTime*60

	return ratioMet && timeMet
}

// completeDownload marks a download as completed and optionally deletes the torrent.
func (s *Service) completeDownload(dl *store.Download, client *qbittorrent.Client) {
	if client != nil && dl.ClientTorrentHash != "" {
		if err := client.DeleteTorrent(dl.ClientTorrentHash, true); err != nil {
			slog.Warn("importer: failed to delete torrent from qBit",
				"download_id", dl.ID, "hash", dl.ClientTorrentHash, "error", err)
		}
	}

	dl.Status = "completed"
	now := time.Now()
	dl.CompletedAt = &now

	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("importer: failed to mark download completed", "download_id", dl.ID, "error", err)
		return
	}

	slog.Info("importer: seeding complete, torrent removed",
		"download_id", dl.ID, "title", dl.Title)
	s.bus.Publish(eventbus.SeedingCompleted, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "completed",
	})
}

// torrentRootFolder detects the common root folder in a multi-file torrent.
// qBittorrent reports file names relative to the save path. Multi-file torrents
// typically have all files under a single root directory (e.g., "ReleaseName/video.mkv").
// Returns the common first path component, or "" for single-file torrents.
func torrentRootFolder(files []qbittorrent.TorrentFile) string {
	if len(files) < 2 {
		return ""
	}

	var root string
	for _, f := range files {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			// At least one file is at the top level — no common root
			return ""
		}
		if root == "" {
			root = parts[0]
		} else if parts[0] != root {
			return ""
		}
	}
	return root
}
