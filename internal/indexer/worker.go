package indexer

import (
	"log/slog"
	"time"
)

const (
	refreshInterval = 24 * time.Hour
	startupDelay    = 60 * time.Second
)

// RefreshWorker periodically fetches the latest indexer definitions from GitHub.
type RefreshWorker struct {
	indexerSvc *Service
	cacheDir   string
	stopCh     chan struct{}
}

// NewRefreshWorker creates a new background definition refresh worker.
func NewRefreshWorker(svc *Service, cacheDir string) *RefreshWorker {
	return &RefreshWorker{
		indexerSvc: svc,
		cacheDir:   cacheDir,
		stopCh:     make(chan struct{}),
	}
}

// Start launches the background refresh goroutine.
func (w *RefreshWorker) Start() {
	go w.run()
}

// Stop signals the worker to shut down.
func (w *RefreshWorker) Stop() {
	close(w.stopCh)
}

func (w *RefreshWorker) run() {
	// Wait before first fetch to let other services initialize.
	select {
	case <-time.After(startupDelay):
	case <-w.stopCh:
		return
	}

	w.processOnce()

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processOnce()
		}
	}
}

func (w *RefreshWorker) processOnce() {
	if CacheFresh(w.cacheDir) {
		slog.Debug("indexer definition cache is fresh, skipping refresh")
		return
	}

	if err := w.indexerSvc.RefreshDefinitions(w.cacheDir); err != nil {
		slog.Warn("failed to refresh indexer definitions", "error", err)
	}
}
