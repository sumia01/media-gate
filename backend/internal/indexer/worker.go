package indexer

import (
	"log/slog"
	"time"

	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/worker"
)

const (
	defaultRefreshInterval = 24 * time.Hour
	refreshStartupDelay    = 60 * time.Second
)

// RefreshWorker periodically fetches the latest indexer definitions from GitHub.
type RefreshWorker struct {
	loop *worker.Loop
}

// NewRefreshWorker creates a new background definition refresh worker.
func NewRefreshWorker(svc *Service, cacheDir string, settingsSvc *settings.Service) *RefreshWorker {
	w := &RefreshWorker{}
	w.loop = worker.New(worker.Config{
		Name:            "indexer-def-refresh",
		DefaultInterval: defaultRefreshInterval,
		IntervalKey:     settings.KeyWorkerIndexerDefRefreshInterval,
		Settings:        settingsSvc,
		StartupDelay:    refreshStartupDelay,
		Process: func() {
			if CacheFresh(cacheDir) {
				slog.Debug("indexer definition cache is fresh, skipping refresh")
				return
			}
			if err := svc.RefreshDefinitions(cacheDir); err != nil {
				slog.Warn("failed to refresh indexer definitions", "error", err)
			}
		},
	})
	return w
}

// Start launches the background refresh goroutine.
func (w *RefreshWorker) Start() {
	w.loop.Start()
}

// Stop signals the worker to shut down.
func (w *RefreshWorker) Stop() {
	w.loop.Stop()
}
