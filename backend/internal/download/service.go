package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/activity"
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
		item, err := s.store.GetMediaItem(dl.MediaItemID)
		if err != nil || item.DeletionPending {
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
			s.handleRetry(dl, fmt.Errorf("failed to fetch torrent: %w", err))
			continue
		}

		// Upload .torrent file to qBittorrent (also computes info hash)
		hash, err := client.AddTorrentFile(dl.Title+".torrent", torrentData, opts)
		addedTorrent := err == nil
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
				s.handleRetry(dl, fmt.Errorf("failed to add torrent to qBittorrent: %w", err))
				continue
			}
		}

		dl.Status = "downloading"
		dl.ClientTorrentHash = hash
		dl.SavePath = downloadPath
		dl.RetryCount = 0
		dl.NextRetryAt = nil
		dl.LastError = ""
		if err := s.store.WithTx(func(tx store.Store) error {
			item, err := tx.GetMediaItem(dl.MediaItemID)
			if err != nil {
				return err
			}
			if item.DeletionPending {
				return store.ErrMediaDeletionPending
			}
			return tx.UpdateDownload(dl)
		}); err != nil {
			slog.Error("download worker: failed to update download status", "download_id", dl.ID, "error", err)
			if addedTorrent && hash != "" {
				if cleanupErr := client.DeleteTorrent(hash, true); cleanupErr != nil {
					slog.Warn("download worker: failed to remove unclaimed torrent", "download_id", dl.ID, "hash", hash, "error", cleanupErr)
				}
			}
			continue
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
	if dl.Status != "pending" || errors.Is(lastErr, context.Canceled) {
		return
	}
	nextDL := *dl
	nextDL.LastError = lastErr.Error()

	if dl.RetryCount < maxRetries {
		nextDL.RetryCount++
		next := time.Now().Add(retryBackoff[nextDL.RetryCount-1])
		nextDL.NextRetryAt = &next
		slog.Warn("download worker: scheduling retry",
			"download_id", dl.ID, "title", dl.Title,
			"retry", nextDL.RetryCount, "next_retry_at", next)
	} else {
		nextDL.Status = "failed"
		nextDL.NextRetryAt = nil
		nextDL.LastError = "download retries exhausted: " + lastErr.Error()
		slog.Error("download worker: max retries exceeded, marking failed",
			"download_id", dl.ID, "title", dl.Title, "retries", dl.RetryCount)
	}

	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("download worker: failed to persist retry outcome", "download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL
	if dl.Status == "failed" {
		s.bus.Publish(eventbus.DownloadFailed, eventbus.DownloadPayload{
			DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "failed",
		})
	}
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
	if dl.Status != "downloading" && dl.Status != "seeding" {
		return
	}
	nextDL := *dl
	if dl.LinkedToLibrary {
		nextDL.Status = "completed"
		now := time.Now()
		nextDL.CompletedAt = &now
	} else {
		nextDL.Status = "failed"
		nextDL.ClientTorrentHash = ""
		nextDL.LastError = "torrent missing from qBittorrent before import"
		nextDL.NextRetryAt = nil
	}

	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("download worker: failed to update download for missing torrent",
			"download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL

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
	if dl.Status != "downloading" {
		return
	}
	mapped := qbittorrent.MapState(info.State)

	var newStatus string
	switch mapped {
	case "downloading":
		newStatus = "downloading"
	case "seeding", "completed":
		// qBit says files are complete — hand off to import worker
		newStatus = "downloaded"
	case "error":
		if info.State != "error" && info.State != "missingFiles" {
			return // An unknown client state is not a confirmed failure.
		}
		newStatus = "failed"
	default:
		// paused, moving, unknown — don't change status
		return
	}

	if newStatus == dl.Status {
		return
	}

	nextDL := *dl
	nextDL.Status = newStatus
	if newStatus == "failed" {
		nextDL.LastError = "qBittorrent reported a torrent error"
		if info.State == "missingFiles" {
			nextDL.LastError = "qBittorrent reported missing files"
		}
		nextDL.NextRetryAt = nil
	}
	if newStatus == "downloaded" && dl.DownloadedAt == nil {
		downloadedAt := time.Now()
		if info.CompletionOn > 0 {
			downloadedAt = time.Unix(info.CompletionOn, 0)
		}
		nextDL.DownloadedAt = &downloadedAt
	}

	if err := s.store.UpdateDownload(&nextDL); err != nil {
		slog.Error("download worker: failed to update download",
			"download_id", dl.ID, "error", err)
		return
	}
	*dl = nextDL

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

// Create validates and atomically persists a manual download and its activity.
func (s *Service) Create(userID uint, dl *store.Download) error {
	if err := validateURLScheme(dl.DownloadURL); err != nil {
		return err
	}

	created := *dl
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateActivityActor(tx, userID); err != nil {
			return err
		}
		item, err := tx.GetMediaItem(created.MediaItemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		exists, err := tx.HasActiveDownloadByURL(created.MediaItemID, created.DownloadURL)
		if err != nil {
			return fmt.Errorf("check duplicate download: %w", err)
		}
		if exists {
			return fmt.Errorf("download already exists for this media item: %w", store.ErrDuplicate)
		}
		resolveEpisodeID(tx, &created)
		if err := tx.CreateDownload(&created); err != nil {
			return err
		}

		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		automatic := false
		details := manualDownloadActivityDetails(tx, item, &created)
		details.Automatic = &automatic
		entry, err := store.NewUserMediaActivity(
			item, userID, store.MediaActivityActionDownloadQueued, operationID,
			store.MediaActivityVisibilityShared, details,
		)
		if err != nil {
			return err
		}
		return tx.AppendMediaActivity(entry)
	}); err != nil {
		return err
	}
	*dl = created
	slog.Info("download: created", "download_id", dl.ID, "title", dl.Title, "media_item_id", dl.MediaItemID)
	s.bus.Publish(eventbus.DownloadCreated, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: dl.Status,
	})
	s.publishActivityInvalidation(dl.MediaItemID)
	return nil
}

// resolveEpisodeID attempts to fill in EpisodeID when not provided by parsing the
// download title. This prevents single-episode downloads from being misclassified
// as season packs (which would block the entire season in the monitor).
func resolveEpisodeID(st store.Store, dl *store.Download) {
	if dl.EpisodeID != nil || dl.MediaItemID == 0 || dl.Title == "" {
		return
	}
	parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
	if parsed.Season == nil || parsed.Episode == nil {
		return // Can't determine episode, or it's actually a season pack
	}
	ep, err := st.GetEpisodeByNumber(dl.MediaItemID, *parsed.Season, *parsed.Episode)
	if err != nil {
		return // Episode not found in DB — best-effort, no error
	}
	dl.EpisodeID = &ep.ID
	if dl.SeasonNumber == nil {
		sn := *parsed.Season
		dl.SeasonNumber = &sn
	}
}

// UpdateStatus atomically persists a manual status mutation and its activity.
func (s *Service) UpdateStatus(userID, dlID uint, status string) (*store.Download, error) {
	var (
		dl            *store.Download
		activityAdded bool
	)
	err := s.store.WithTx(func(tx store.Store) error {
		if err := validateActivityActor(tx, userID); err != nil {
			return err
		}
		current, err := tx.GetDownload(dlID)
		if err != nil {
			return err
		}
		item, err := tx.GetMediaItem(current.MediaItemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}

		oldStatus := current.Status
		changed := oldStatus != status
		if status == "pending" && (current.RetryCount != 0 || current.NextRetryAt != nil || current.LastError != "") {
			changed = true
		}
		if !changed {
			dl = current
			return nil
		}

		current.Status = status
		if status == "pending" {
			current.RetryCount = 0
			current.NextRetryAt = nil
			current.LastError = ""
		}
		if err := tx.UpdateDownload(current); err != nil {
			return err
		}

		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		details := manualDownloadActivityDetails(tx, item, current)
		details.OldStatus = oldStatus
		details.NewStatus = current.Status
		if status == "pending" {
			details.Reason = "manual_retry"
		}
		entry, err := store.NewUserMediaActivity(
			item, userID, store.MediaActivityActionDownloadStatusChanged, operationID,
			store.MediaActivityVisibilityShared, details,
		)
		if err != nil {
			return err
		}
		if err := tx.AppendMediaActivity(entry); err != nil {
			return err
		}
		dl = current
		activityAdded = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if activityAdded {
		s.publishActivityInvalidation(dl.MediaItemID)
	}
	return dl, nil
}

func validateActivityActor(st store.Store, userID uint) error {
	if _, err := st.GetUser(userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
}

func manualDownloadActivityDetails(st store.Store, item *store.MediaItem, dl *store.Download) store.MediaActivityDetails {
	details := store.MediaActivityDetails{
		ReleaseName: store.BoundMediaActivityText(dl.Title, store.MediaActivityMaxTitleBytes),
		IndexerName: store.BoundMediaActivityText(dl.IndexerName, store.MediaActivityMaxTitleBytes),
		DownloadID:  &dl.ID,
	}
	targets, total := activity.DownloadTargets(st, item, dl)
	activity.ApplyDownloadTargets(&details, targets, total)
	return details
}

func (s *Service) publishActivityInvalidation(mediaItemID uint) {
	s.bus.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
}

// ListWithProgress lists downloads and optionally enriches them with real-time
// qBittorrent progress data when filtering by media item.
func (s *Service) ListWithProgress(mediaItemID *uint, status *string, limit *int) ([]DownloadWithProgress, bool, error) {
	var (
		downloads []store.Download
		hasMore   bool
		err       error
	)
	if limit == nil {
		downloads, err = s.store.ListDownloads(mediaItemID, status)
	} else {
		downloads, hasMore, err = s.store.ListDownloadsPage(mediaItemID, status, *limit)
	}
	if err != nil {
		return nil, false, err
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

	return result, hasMore, nil
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
// removed externally are marked as failed, or completed if already imported.
// Best-effort: skipped if qBit is not configured or unreachable.
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
				s.handleMissingTorrent(dl)
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
