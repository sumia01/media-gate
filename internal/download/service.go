package download

import (
	"log/slog"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const pollInterval = 30 * time.Second

type Service struct {
	store    store.Store
	settings *settings.Service
	client   *qbittorrent.Client
	mu       sync.Mutex
	stopCh   chan struct{}
}

func NewService(s store.Store, settingsSvc *settings.Service) *Service {
	return &Service{
		store:    s,
		settings: settingsSvc,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background worker goroutine.
func (s *Service) Start() {
	go s.run()
	slog.Info("download worker started", "interval", pollInterval)
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
		slog.Debug("download worker: qBittorrent not configured, skipping", "error", err)
		return
	}

	s.sendPending(client)
	s.pollActive(client)
}

// getClient returns a cached qBittorrent client, creating one from settings on first call.
// If settings change, the cached client is invalidated by clearing it.
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

// sendPending picks up downloads in "pending" status and sends them to qBittorrent.
func (s *Service) sendPending(client *qbittorrent.Client) {
	status := "pending"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Error("download worker: failed to list pending downloads", "error", err)
		return
	}

	if len(downloads) == 0 {
		return
	}

	downloadPath := s.settings.GetWithDefault(settings.KeyQBitDownloadPath, "")

	for i := range downloads {
		dl := &downloads[i]

		opts := qbittorrent.AddTorrentOptions{
			SavePath: downloadPath,
		}

		hash, err := client.AddTorrent(dl.DownloadURL, opts)
		if err != nil {
			slog.Error("download worker: failed to add torrent",
				"download_id", dl.ID, "title", dl.Title, "error", err)
			dl.Status = "failed"
			if err := s.store.UpdateDownload(dl); err != nil {
				slog.Error("download worker: failed to update download status", "download_id", dl.ID, "error", err)
			}
			continue
		}

		dl.Status = "downloading"
		dl.ClientTorrentHash = hash
		dl.SavePath = downloadPath
		if err := s.store.UpdateDownload(dl); err != nil {
			slog.Error("download worker: failed to update download status", "download_id", dl.ID, "error", err)
		}

		slog.Info("download worker: torrent added", "download_id", dl.ID, "title", dl.Title, "hash", hash)
	}
}

// pollActive checks downloads in "downloading" or "seeding" status against qBittorrent.
func (s *Service) pollActive(client *qbittorrent.Client) {
	for _, status := range []string{"downloading", "seeding"} {
		st := status
		downloads, err := s.store.ListDownloads(nil, &st)
		if err != nil {
			slog.Error("download worker: failed to list active downloads", "status", status, "error", err)
			continue
		}

		for i := range downloads {
			dl := &downloads[i]
			if dl.ClientTorrentHash == "" {
				continue
			}

			info, err := client.GetTorrent(dl.ClientTorrentHash)
			if err != nil {
				if err == qbittorrent.ErrTorrentNotFound {
					slog.Warn("download worker: torrent not found in qBittorrent",
						"download_id", dl.ID, "hash", dl.ClientTorrentHash)
				} else {
					slog.Error("download worker: failed to get torrent info",
						"download_id", dl.ID, "error", err)
				}
				continue
			}

			s.updateFromTorrent(dl, info)
		}
	}
}

// updateFromTorrent maps qBittorrent state to download status and enforces seeding rules.
func (s *Service) updateFromTorrent(dl *store.Download, info *qbittorrent.TorrentInfo) {
	mapped := qbittorrent.MapState(info.State)

	var newStatus string
	switch mapped {
	case "downloading":
		newStatus = "downloading"
	case "seeding":
		if s.seedingComplete(dl, info) {
			newStatus = "completed"
		} else {
			newStatus = "seeding"
		}
	case "completed":
		// qBit reports pausedUP — torrent is done seeding
		if s.seedingComplete(dl, info) {
			newStatus = "completed"
		} else {
			newStatus = "seeding"
		}
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
	if newStatus == "completed" {
		now := time.Now()
		dl.CompletedAt = &now
	}

	if err := s.store.UpdateDownload(dl); err != nil {
		slog.Error("download worker: failed to update download",
			"download_id", dl.ID, "error", err)
		return
	}

	slog.Info("download worker: status updated",
		"download_id", dl.ID, "title", dl.Title, "status", newStatus)
}

// seedingComplete checks if the indexer's seeding requirements have been met.
func (s *Service) seedingComplete(dl *store.Download, info *qbittorrent.TorrentInfo) bool {
	indexer, err := s.store.GetIndexer(dl.IndexerID)
	if err != nil {
		// Indexer may have been deleted — consider seeding complete
		return true
	}

	ratioMet := indexer.SeedMinRatio <= 0 || info.Ratio >= indexer.SeedMinRatio
	timeMet := indexer.SeedMinTime <= 0 || info.SeedingTime >= indexer.SeedMinTime*60 // SeedMinTime is minutes, SeedingTime is seconds

	return ratioMet && timeMet
}
