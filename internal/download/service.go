package download

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const pollInterval = 5 * time.Second

type Service struct {
	store      store.Store
	settings   *settings.Service
	indexerSvc *indexer.Service
	client     *qbittorrent.Client
	mu         sync.Mutex
	stopCh     chan struct{}
}

func NewService(s store.Store, settingsSvc *settings.Service, indexerSvc *indexer.Service) *Service {
	return &Service{
		store:      s,
		settings:   settingsSvc,
		indexerSvc: indexerSvc,
		stopCh:     make(chan struct{}),
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
	category := s.settings.GetWithDefault(settings.KeyQBitCategory, "media-gate-dl")

	if category != "" {
		if err := client.EnsureCategory(category); err != nil {
			slog.Error("download worker: failed to ensure category", "category", category, "error", err)
		}
	}

	for i := range downloads {
		dl := &downloads[i]

		opts := qbittorrent.AddTorrentOptions{
			SavePath: downloadPath,
			Category: category,
		}

		// Fetch .torrent file via the indexer's authenticated session
		ctx := context.Background()
		torrentData, err := s.indexerSvc.FetchTorrent(ctx, dl.IndexerID, dl.DownloadURL)
		if err != nil {
			slog.Error("download worker: failed to fetch torrent",
				"download_id", dl.ID, "title", dl.Title, "error", err)
			dl.Status = "failed"
			_ = s.store.UpdateDownload(dl)
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
				dl.Status = "failed"
				_ = s.store.UpdateDownload(dl)
				continue
			}
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

// pollActive checks downloads in "downloading" status against qBittorrent.
func (s *Service) pollActive(client *qbittorrent.Client) {
	status := "downloading"
	downloads, err := s.store.ListDownloads(nil, &status)
	if err != nil {
		slog.Error("download worker: failed to list active downloads", "status", status, "error", err)
		return
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
}
