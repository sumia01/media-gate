package importer

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
	"github.com/sumia01/media-gate/internal/worker"
)

const defaultPollInterval = 10 * time.Second

// maxImportRetries bounds how many times a transient import error (e.g. a
// failed GetTorrentFiles call) is retried before the download is marked
// permanently import_failed.
const maxImportRetries = 5

// importRetryBackoff is the delay before re-attempting import after each
// transient failure, indexed by (RetryCount-1).
var importRetryBackoff = [maxImportRetries]time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
}

// Service runs background workers for importing completed downloads into
// library directories and cleaning up torrents after seeding obligations are met.
type Service struct {
	store    store.Store
	settings *settings.Service
	syncSvc  *mediasync.Service
	bus      *eventbus.Bus
	qbit     *qbittorrent.Provider
	loop     *worker.Loop
}

// NewService creates a new importer service.
func NewService(s store.Store, settingsSvc *settings.Service, syncSvc *mediasync.Service, bus *eventbus.Bus, qbit *qbittorrent.Provider) *Service {
	svc := &Service{
		store:    s,
		settings: settingsSvc,
		syncSvc:  syncSvc,
		bus:      bus,
		qbit:     qbit,
	}
	svc.loop = worker.New(worker.Config{
		Name:            "importer",
		DefaultInterval: defaultPollInterval,
		IntervalKey:     settings.KeyWorkerImporterInterval,
		Settings:        settingsSvc,
		Process:         svc.processOnce,
	})
	return svc
}

// Start launches the background worker goroutine.
func (s *Service) Start() { s.loop.Start() }

// Stop signals the worker to shut down.
func (s *Service) Stop() { s.loop.Stop() }

func (s *Service) processOnce() {
	client, err := s.qbit.Client()
	if err != nil {
		slog.Debug("importer: qBittorrent not configured, skipping", "error", err)
		return
	}

	// Skip the qBit probe entirely when there's nothing to do this tick — avoids
	// an authenticated round-trip to qBit on every idle poll (the common case).
	downloadedStatus, seedingStatus := "downloaded", "seeding"
	downloaded, errD := s.store.ListDownloads(nil, &downloadedStatus)
	seeding, errS := s.store.ListDownloads(nil, &seedingStatus)
	if errD == nil && errS == nil && len(downloaded) == 0 && len(seeding) == 0 {
		return
	}

	// Health-gate: if qBit is unreachable, skip this tick entirely so a transient
	// outage doesn't burn import retries or flip downloads to import_failed.
	// Downloads stay in "downloaded"/"seeding" and are retried next tick.
	if err := client.TestConnection(); err != nil {
		slog.Warn("importer: qBittorrent unreachable, skipping import tick", "error", err)
		return
	}

	s.importDownloaded(client)
	s.cleanupSeeding(client)
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

	now := time.Now()
	for i := range downloads {
		dl := &downloads[i]
		// Respect the backoff scheduled by a previous transient import failure.
		if dl.NextRetryAt != nil && dl.NextRetryAt.After(now) {
			continue
		}
		s.importOne(client, dl)
	}
}

func (s *Service) importOne(client *qbittorrent.Client, dl *store.Download) {
	if dl.Status != "downloaded" {
		return
	}
	// Mark as importing to prevent double-processing
	claimed := *dl
	claimed.Status = "importing"
	if err := s.store.UpdateDownload(&claimed); err != nil {
		slog.Error("importer: failed to set importing status", "download_id", dl.ID, "error", err)
		return
	}
	*dl = claimed
	slog.Info("importer: starting import", "download_id", dl.ID, "title", dl.Title, "media_item_id", dl.MediaItemID)

	// Look up media item and library
	item, err := s.store.GetMediaItem(dl.MediaItemID)
	if err != nil {
		slog.Error("importer: media item not found", "download_id", dl.ID, "media_item_id", dl.MediaItemID, "error", err)
		if errors.Is(err, store.ErrNotFound) {
			s.failImport(dl, "media item not found")
		} else {
			s.retryImport(dl, "failed to load media item")
		}
		return
	}
	if item.DeletionPending {
		slog.Info("importer: media deletion pending, abandoning import", "download_id", dl.ID, "media_item_id", dl.MediaItemID)
		return
	}

	lib, err := s.store.GetLibrary(item.LibraryID)
	if err != nil {
		slog.Error("importer: library not found", "download_id", dl.ID, "library_id", item.LibraryID, "error", err)
		if errors.Is(err, store.ErrNotFound) {
			s.failImport(dl, "library not found")
		} else {
			s.retryImport(dl, "failed to load library")
		}
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
		// Transient (qBit hiccup / metadata not ready yet): don't fail permanently.
		// Revert to "downloaded" with backoff so a later tick retries.
		slog.Error("importer: failed to get torrent files", "download_id", dl.ID, "error", err)
		s.retryImport(dl, "failed to get torrent files from qBittorrent: "+err.Error())
		return
	}
	targetEpisode, err := s.singleEpisodeTarget(item, dl, files)
	if err != nil {
		slog.Warn("importer: failed to resolve episode target", "download_id", dl.ID, "error", err)
		s.retryImport(dl, "failed to load the selected episode")
		return
	}

	// Load paths already imported for this item so a re-import (e.g. a download
	// reset from a crashed "importing" state) doesn't hardlink duplicates.
	importedPaths := map[string]bool{}
	if existing, lerr := s.store.ListMediaFilesByMediaItem(dl.MediaItemID); lerr == nil {
		for _, mf := range existing {
			importedPaths[mf.Path] = true
		}
	}

	// Detect common root folder in torrent (multi-file torrents wrap files in a root dir)
	rootFolder := torrentRootFolder(files)

	// Import all non-junk files: video files get MediaFile records, companions are just linked
	imported := 0
	for _, f := range files {
		if fileparse.IsJunkFile(f.Name) {
			continue
		}
		if fileparse.IsSampleFile(f.Name) {
			slog.Debug("importer: skipping sample file", "download_id", dl.ID, "file", f.Name)
			continue
		}

		// Strip torrent root folder to avoid double-nesting (release dir already isolates)
		relPath := f.Name
		if rootFolder != "" {
			relPath = strings.TrimPrefix(f.Name, rootFolder+"/")
		}

		srcPath := filepath.Join(dl.SavePath, f.Name)
		dstPath := filepath.Join(releaseDir, relPath)

		// Validate paths stay within allowed boundaries
		if err := safePath(dl.SavePath, srcPath); err != nil {
			slog.Warn("importer: skipping file with path traversal in source", "download_id", dl.ID, "file", f.Name)
			continue
		}
		if err := safePath(releaseDir, dstPath); err != nil {
			slog.Warn("importer: skipping file with path traversal in destination", "download_id", dl.ID, "file", f.Name)
			continue
		}

		// Ensure subdirectories exist (e.g., Subs/)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			slog.Warn("importer: failed to create subdir", "download_id", dl.ID, "path", filepath.Dir(dstPath), "error", err)
			continue
		}

		fileName := filepath.Base(relPath)

		if fileparse.IsVideoFile(fileName) {
			// Idempotent re-import: if this file's deterministic destination is
			// already a tracked MediaFile, skip it. Re-linking would pick a
			// numbered unique path and create a duplicate file + record.
			if importedPaths[dstPath] {
				slog.Debug("importer: file already imported, skipping", "download_id", dl.ID, "path", dstPath)
				imported++
				continue
			}
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
			episodeNum := info.EpisodeNumber
			if episodeNum == nil && targetEpisode != nil {
				// A season embedded in an otherwise unnumbered filename still
				// prevents relabelling a file from a different season.
				fileSeason := fileparse.ParseTorrentSeasonEpisode(fileName).Season
				if fileSeason == nil || *fileSeason == targetEpisode.SeasonNumber {
					seasonNum, episodeNum = &targetEpisode.SeasonNumber, &targetEpisode.EpisodeNumber
				}
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
				EpisodeNumber: episodeNum,
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
		// No video files were imported (archive/RAR-only release, all samples, or
		// every file rejected by path validation). Do NOT mark completed and do NOT
		// delete the torrent: "completed" is an active status that would permanently
		// block the monitor from re-grabbing, and deleting the torrent would destroy
		// the data. Fail non-destructively — import_failed is non-active, so the
		// monitor can re-grab, and the payload stays on disk for inspection/retry.
		s.failImport(dl, "no video files imported (archive-only release, all samples, or files rejected)")
		return
	}

	nextDL := *dl
	nextDL.LinkedToLibrary = true

	// Check if seeding is required
	if s.needsSeeding(dl) {
		nextDL.Status = "seeding"
	} else {
		nextDL.Status = "completed"
		now := time.Now()
		nextDL.CompletedAt = &now
	}

	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("importer: failed to update download after import", "download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL
	if dl.Status == "completed" {
		s.removeCompletedTorrent(dl, client)
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
	if dl.Status != "importing" && dl.Status != "downloaded" {
		return
	}
	nextDL := *dl
	nextDL.Status = "import_failed"
	nextDL.LastError = reason
	nextDL.NextRetryAt = nil
	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("importer: failed to set import_failed status", "download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL
	slog.Warn("importer: import failed", "download_id", dl.ID, "title", dl.Title, "reason", reason)
	s.bus.Publish(eventbus.ImportFailed, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "import_failed",
	})
}

// retryImport handles a transient import failure (e.g. qBittorrent briefly
// unreachable while fetching torrent files). It reverts the download to
// "downloaded" — still an active status, so the monitor won't re-grab — and
// schedules a backoff so a later importer tick retries. After maxImportRetries
// it gives up and marks the download import_failed so the monitor can re-grab.
func (s *Service) retryImport(dl *store.Download, reason string) {
	if dl.Status != "importing" && dl.Status != "downloaded" {
		return
	}
	if dl.RetryCount >= maxImportRetries {
		s.failImport(dl, reason+" (max import retries exceeded)")
		return
	}
	nextDL := *dl
	nextDL.LastError = reason
	nextDL.RetryCount++
	next := time.Now().Add(importRetryBackoff[nextDL.RetryCount-1])
	nextDL.NextRetryAt = &next
	nextDL.Status = "downloaded"
	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("importer: failed to schedule import retry", "download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL
	slog.Warn("importer: transient import error, scheduling retry",
		"download_id", dl.ID, "title", dl.Title,
		"retry", dl.RetryCount, "next_retry_at", next, "reason", reason)
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
	if dl.Status != "seeding" {
		return
	}

	nextDL := *dl
	nextDL.Status = "completed"
	now := time.Now()
	nextDL.CompletedAt = &now

	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("importer: failed to mark download completed", "download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL
	s.removeCompletedTorrent(dl, client)

	slog.Info("importer: seeding complete, torrent removed",
		"download_id", dl.ID, "title", dl.Title)
	s.bus.Publish(eventbus.SeedingCompleted, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "completed",
	})
}

// Call only after the completion CAS commits: that write authorizes cleanup and
// finalization. Earlier cancellation rejects the CAS; later cancellation cannot
// revoke authorization. Do not add a second persistence gate here: completed rows
// are not polled again, so a transient error would strand finalization. qBit I/O
// stays outside transactions and deletion remains best-effort.
func (s *Service) removeCompletedTorrent(dl *store.Download, client *qbittorrent.Client) {
	if client == nil || dl.ClientTorrentHash == "" {
		return
	}
	if err := client.DeleteTorrent(dl.ClientTorrentHash, true); err != nil {
		slog.Warn("importer: failed to delete torrent from qBit",
			"download_id", dl.ID, "hash", dl.ClientTorrentHash, "error", err)
	}
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
