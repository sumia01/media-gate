package download

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/worker"
)

const defaultPollInterval = 5 * time.Second

const maxRetries = 5

var retryBackoff = [maxRetries]time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
}

type Service struct {
	store      store.Store
	settings   *settings.Service
	indexerSvc *indexer.Service
	bus        *eventbus.Bus
	qbit       *qbittorrent.Provider
	loop       *worker.Loop
}

func NewService(s store.Store, settingsSvc *settings.Service, indexerSvc *indexer.Service, bus *eventbus.Bus, qbit *qbittorrent.Provider) *Service {
	svc := &Service{
		store:      s,
		settings:   settingsSvc,
		indexerSvc: indexerSvc,
		bus:        bus,
		qbit:       qbit,
	}
	svc.loop = worker.New(worker.Config{
		Name:            "download",
		DefaultInterval: defaultPollInterval,
		IntervalKey:     settings.KeyWorkerDownloadInterval,
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
		slog.Debug("download worker: qBittorrent not configured, skipping", "error", err)
		return
	}

	// Health check: if qBit is unreachable, skip the whole tick. This gates BOTH
	// sendPending (so pending downloads don't burn retry attempts) AND pollActive
	// — during a qBit restart GetTorrent returns "not found" for a still-valid
	// torrent, and polling would wrongly flip it to "failed" and clear its hash.
	if err := client.TestConnection(); err != nil {
		slog.Warn("download worker: qBittorrent unreachable, skipping tick", "error", err)
		return
	}

	justSent := s.sendPending(client)
	s.pollActive(client, justSent)
}

// sendPending picks up downloads in "pending" status and sends them to
// qBittorrent. It returns the IDs of downloads transitioned to "downloading"
// in this call, so the caller can exclude them from the same-tick poll pass
// below (see pollActive).
func (s *Service) sendPending(client *qbittorrent.Client) map[uint]bool {
	status := "pending"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Error("download worker: failed to list pending downloads", "error", err)
		return nil
	}

	if len(downloads) == 0 {
		return nil
	}

	now := time.Now()

	downloadPath := s.settings.GetWithDefault(settings.KeyQBitDownloadPath, "")
	savePath := s.settings.GetWithDefault(settings.KeyQBitSavePath, "")
	if savePath == "" {
		savePath = downloadPath
	}
	category := s.settings.GetWithDefault(settings.KeyQBitCategory, "media-gate-dl")

	if category != "" {
		if err := client.EnsureCategory(category); err != nil {
			slog.Error("download worker: failed to ensure category", "category", category, "error", err)
		}
	}

	justSent := make(map[uint]bool)

	for i := range downloads {
		dl := &downloads[i]

		// Skip downloads in backoff
		if dl.NextRetryAt != nil && dl.NextRetryAt.After(now) {
			continue
		}

		opts := qbittorrent.AddTorrentOptions{
			SavePath: savePath,
			Category: category,
		}

		// Fetch .torrent file via the indexer's authenticated session
		ctx := context.Background()
		torrentData, err := s.indexerSvc.FetchTorrent(ctx, dl.IndexerID, dl.DownloadURL)
		if err != nil {
			slog.Error("download worker: failed to fetch torrent",
				"download_id", dl.ID, "title", dl.Title, "error", err)
			s.handleRetry(dl, err)
			continue
		}

		// Upload .torrent file to qBittorrent (also computes info hash)
		hash, err := client.AddTorrentFile(dl.Title+".torrent", torrentData, opts)
		if err != nil {
			// If qBit rejected it, the torrent may already exist — check by hash
			if checkHash, hashErr := qbittorrent.InfoHash(torrentData); hashErr == nil && checkHash != "" {
				if _, getErr := client.GetTorrent(checkHash); getErr == nil {
					slog.Info("download worker: torrent already in qBittorrent, reusing",
						"download_id", dl.ID, "hash", checkHash)
					hash = checkHash
					err = nil
				}
			}
			if err != nil {
				slog.Error("download worker: failed to add torrent",
					"download_id", dl.ID, "title", dl.Title, "error", err)
				s.handleRetry(dl, err)
				continue
			}
		}

		dl.Status = "downloading"
		dl.ClientTorrentHash = hash
		dl.SavePath = downloadPath
		dl.RetryCount = 0
		dl.NextRetryAt = nil
		dl.LastError = ""
		if err := s.store.UpdateDownload(dl); err != nil {
			slog.Error("download worker: failed to update download status", "download_id", dl.ID, "error", err)
		}

		slog.Info("download worker: torrent added", "download_id", dl.ID, "title", dl.Title, "hash", hash)
		s.bus.Publish(eventbus.DownloadSentToClient, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Hash: hash, Status: "downloading",
		})
		justSent[dl.ID] = true
	}

	return justSent
}

// handleRetry increments retry count and schedules backoff, or fails permanently.
func (s *Service) handleRetry(dl *store.Download, lastErr error) {
	dl.LastError = lastErr.Error()

	if dl.RetryCount < maxRetries {
		dl.RetryCount++
		next := time.Now().Add(retryBackoff[dl.RetryCount-1])
		dl.NextRetryAt = &next
		slog.Warn("download worker: scheduling retry",
			"download_id", dl.ID, "title", dl.Title,
			"retry", dl.RetryCount, "next_retry_at", next)
	} else {
		dl.Status = "failed"
		slog.Error("download worker: max retries exceeded, marking failed",
			"download_id", dl.ID, "title", dl.Title, "retries", dl.RetryCount)
		s.bus.Publish(eventbus.DownloadFailed, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "failed",
		})
	}

	_ = s.store.UpdateDownload(dl)
}

// pollActive checks downloads in "downloading" status against qBittorrent.
// justSent holds IDs that sendPending handed to qBittorrent earlier in this
// same tick — they're skipped here because qBittorrent registers a newly
// added torrent asynchronously (observed lag: single-digit milliseconds to
// a few dozen ms), so GetTorrent can spuriously return "not found" for a
// torrent that in fact was just accepted. Waiting for the next tick (default
// 5s) gives qBittorrent ample time to register it before it's polled.
func (s *Service) pollActive(client *qbittorrent.Client, justSent map[uint]bool) {
	status := "downloading"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Error("download worker: failed to list active downloads", "status", status, "error", err)
		return
	}

	for i := range downloads {
		dl := &downloads[i]
		if dl.ClientTorrentHash == "" || justSent[dl.ID] {
			continue
		}

		info, err := client.GetTorrent(dl.ClientTorrentHash)
		if err != nil {
			if err == qbittorrent.ErrTorrentNotFound {
				// Torrent removed from qBit (e.g. deleted in the UI) while still
				// "downloading". Don't leave the row stuck in an active status
				// forever — transition it to a terminal state (mirrors how
				// cleanupSeeding treats missing seeding torrents) so the monitor
				// can re-grab.
				s.handleMissingTorrent(dl)
			} else {
				slog.Error("download worker: failed to get torrent info",
					"download_id", dl.ID, "error", err)
			}
			continue
		}

		s.updateFromTorrent(dl, info)
	}
}

// handleMissingTorrent transitions a download whose torrent has vanished from
// qBittorrent to a terminal (non-active) status. If the files were already
// imported it is marked "completed"; otherwise "failed" so the monitor can
// re-grab. Mirrors cleanupSeeding's handling of missing seeding torrents.
func (s *Service) handleMissingTorrent(dl *store.Download) {
	if dl.LinkedToLibrary {
		dl.Status = "completed"
		now := time.Now()
		dl.CompletedAt = &now
	} else {
		dl.Status = "failed"
		dl.ClientTorrentHash = ""
	}

	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("download worker: failed to update download for missing torrent",
			"download_id", dl.ID, "error", err)
		return
	}

	slog.Warn("download worker: torrent missing from qBittorrent, marking terminal",
		"download_id", dl.ID, "title", dl.Title, "status", dl.Status)

	if dl.Status == "failed" {
		s.bus.Publish(eventbus.DownloadFailed, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "failed",
		})
	}
}

// updateFromTorrent maps qBittorrent state to download status.
// When qBit reports download is complete (seeding/pausedUP), transitions to "downloaded"
// so the import worker can pick it up.
func (s *Service) updateFromTorrent(dl *store.Download, info *qbittorrent.TorrentInfo) {
	mapped := qbittorrent.MapState(info.State)

	var newStatus string
	switch mapped {
	case "downloading":
		newStatus = "downloading"
	case "seeding", "completed":
		// qBit says files are complete — hand off to import worker
		newStatus = "downloaded"
	case "error":
		newStatus = "failed"
	default:
		// paused, moving, unknown — don't change status
		return
	}

	if newStatus == dl.Status {
		return
	}

	dl.Status = newStatus

	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("download worker: failed to update download",
			"download_id", dl.ID, "error", err)
		return
	}

	slog.Info("download worker: status updated",
		"download_id", dl.ID, "title", dl.Title, "status", newStatus)

	switch newStatus {
	case "downloaded":
		s.bus.Publish(eventbus.DownloadCompleted, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Hash: dl.ClientTorrentHash, Status: newStatus,
		})
	case "failed":
		s.bus.Publish(eventbus.DownloadFailed, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: newStatus,
		})
	}
}

// DownloadWithProgress enriches a download record with real-time torrent data.
type DownloadWithProgress struct {
	store.Download
	Progress      *float32
	DownloadSpeed *int64
	UploadSpeed   *int64
}

// Create validates and persists a new download, then publishes a DownloadCreated event.
func (s *Service) Create(dl *store.Download) error {
	if err := validateURLScheme(dl.DownloadURL); err != nil {
		return err
	}
	exists, err := s.store.HasActiveDownloadByURL(dl.MediaItemID, dl.DownloadURL)
	if err != nil {
		return fmt.Errorf("check duplicate download: %w", err)
	}
	if exists {
		return fmt.Errorf("download already exists for this media item: %w", store.ErrDuplicate)
	}
	s.resolveEpisodeID(dl)
	if err := s.store.CreateDownload(dl); err != nil {
		return err
	}
	slog.Info("download: created", "download_id", dl.ID, "title", dl.Title, "media_item_id", dl.MediaItemID)
	s.bus.Publish(eventbus.DownloadCreated, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: dl.Status,
	})
	return nil
}

// resolveEpisodeID attempts to fill in EpisodeID when not provided by parsing the
// download title. This prevents single-episode downloads from being misclassified
// as season packs (which would block the entire season in the monitor).
func (s *Service) resolveEpisodeID(dl *store.Download) {
	if dl.EpisodeID != nil || dl.MediaItemID == 0 || dl.Title == "" {
		return
	}
	parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
	if parsed.Season == nil || parsed.Episode == nil {
		return // Can't determine episode, or it's actually a season pack
	}
	ep, err := s.store.GetEpisodeByNumber(dl.MediaItemID, *parsed.Season, *parsed.Episode)
	if err != nil {
		return // Episode not found in DB — best-effort, no error
	}
	dl.EpisodeID = &ep.ID
	if dl.SeasonNumber == nil {
		sn := *parsed.Season
		dl.SeasonNumber = &sn
	}
}

// UpdateStatus sets a download's status and resets retry state when going back to "pending".
func (s *Service) UpdateStatus(dlID uint, status string) (*store.Download, error) {
	dl, err := s.store.GetDownload(dlID)
	if err != nil {
		return nil, err
	}
	dl.Status = status
	if status == "pending" {
		dl.RetryCount = 0
		dl.NextRetryAt = nil
		dl.LastError = ""
	}
	if err := s.store.UpdateDownload(dl); err != nil {
		return nil, err
	}
	return dl, nil
}

// ListWithProgress lists downloads and optionally enriches them with real-time
// qBittorrent progress data when filtering by media item.
func (s *Service) ListWithProgress(mediaItemID *uint, status *string) ([]DownloadWithProgress, error) {
	downloads, err := s.store.ListDownloads(mediaItemID, status)
	if err != nil {
		return nil, err
	}

	result := make([]DownloadWithProgress, len(downloads))
	for i := range downloads {
		result[i].Download = downloads[i]
	}

	if mediaItemID != nil {
		if client, err := s.qbit.Client(); err == nil {
			for i := range result {
				hash := result[i].ClientTorrentHash
				if hash == "" {
					continue
				}
				info, err := client.GetTorrent(hash)
				if err != nil {
					continue
				}
				p := float32(info.Progress)
				result[i].Progress = &p
				result[i].DownloadSpeed = &info.DownloadSpeed
				result[i].UploadSpeed = &info.UploadSpeed
			}
		}
	}

	return result, nil
}

// ListTorrentFiles returns the file list for a torrent in qBittorrent.
func (s *Service) ListTorrentFiles(hash string) ([]qbittorrent.TorrentFile, error) {
	if hash == "" {
		return nil, nil
	}
	client, err := s.qbit.Client()
	if err != nil {
		return nil, nil
	}
	files, err := client.GetTorrentFiles(hash)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// Reconcile checks that downloads in "downloading" or "seeding" status still
// have active torrents in qBittorrent. Downloads whose torrents have been
// removed externally are marked as failed. Best-effort — skipped if qBit
// is not configured or unreachable.
func (s *Service) Reconcile() {
	// Recover downloads stuck in the transient "importing" state after a crash or
	// restart mid-import. This is a pure DB fixup independent of qBittorrent, so
	// run it before the qBit reachability check below (which returns early when
	// qBit is unconfigured/unreachable).
	s.recoverStuckImporting()

	client, err := s.qbit.Client()
	if err != nil {
		return // qBit not configured
	}
	if err := client.TestConnection(); err != nil {
		slog.Warn("startup: qBittorrent not reachable, skipping torrent reconciliation", "error", err)
		return
	}

	torrents, err := client.GetTorrents()
	if err != nil {
		slog.Warn("startup: failed to list torrents from qBittorrent", "error", err)
		return
	}
	hashSet := make(map[string]struct{}, len(torrents))
	for _, t := range torrents {
		hashSet[strings.ToLower(t.Hash)] = struct{}{}
	}

	for _, status := range []string{"downloading", "seeding"} {
		downloads, err := s.store.ListDownloads(nil, &status)
		if err != nil {
			continue
		}
		for i := range downloads {
			dl := &downloads[i]
			if dl.ClientTorrentHash == "" {
				continue
			}
			if _, ok := hashSet[strings.ToLower(dl.ClientTorrentHash)]; !ok {
				slog.Warn("startup: torrent missing from client, marking download as failed",
					"download_id", dl.ID, "title", dl.Title, "hash", dl.ClientTorrentHash)
				dl.Status = "failed"
				dl.ClientTorrentHash = ""
				_ = s.store.UpdateDownload(dl)
			}
		}
	}
}

// recoverStuckImporting resets downloads left in the transient "importing" state
// by a crash/restart (or a one-off UpdateDownload failure mid-import) back to
// "downloaded" so the importer re-processes them on its next tick. Without this,
// such rows stay "importing" — an active status — forever and never get re-picked.
// The importer's re-import is idempotent for files it has already tracked, so
// re-processing does not duplicate already-hardlinked files.
func (s *Service) recoverStuckImporting() {
	status := "importing"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Warn("startup: failed to list stuck importing downloads", "error", err)
		return
	}
	for i := range downloads {
		dl := &downloads[i]
		dl.Status = "downloaded"
		if err := s.store.UpdateDownload(dl); err != nil {
			slog.Error("startup: failed to reset stuck importing download",
				"download_id", dl.ID, "error", err)
			continue
		}
		slog.Warn("startup: reset stuck 'importing' download to 'downloaded' for re-import",
			"download_id", dl.ID, "title", dl.Title)
	}
}

// validateURLScheme checks that rawURL parses successfully and has an http or https scheme.
func validateURLScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL must have a host")
	}
	return nil
}
