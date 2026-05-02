package apiv1

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/download"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/subtitle"
	"github.com/sumia01/media-gate/internal/updater"
	"github.com/sumia01/media-gate/internal/worker"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

// Ensure Handlers implements the generated StrictServerInterface.
var _ StrictServerInterface = (*Handlers)(nil)

type Handlers struct {
	lib            *library.Service
	store          store.Store
	queue          *jobqueue.Queue
	settings       *settings.Service
	matchSvc       *matching.Service
	syncSvc        *mediasync.Service
	indexerSvc     *indexer.Service
	authSvc        *auth.Service
	mediaSvc       *media.Service
	downloadSvc    *download.Service
	subtitleSvc    *subtitle.Service
	updaterSvc     *updater.Service
	plexProvider   *plex.Provider
	workerRegistry *worker.Registry
	posterDir      string
	dbPath         string
	secureCookies  bool
	setupMu        sync.Mutex
	version        string
}

func NewHandlers(lib *library.Service, s store.Store, q *jobqueue.Queue, set *settings.Service, matchSvc *matching.Service, syncSvc *mediasync.Service, indexerSvc *indexer.Service, posterDir string, dbPath string, authSvc *auth.Service, secureCookies bool, mediaSvc *media.Service, downloadSvc *download.Service, subtitleSvc *subtitle.Service, updaterSvc *updater.Service, plexProvider *plex.Provider, workerReg *worker.Registry, version string) *Handlers {
	return &Handlers{lib: lib, store: s, queue: q, settings: set, matchSvc: matchSvc, syncSvc: syncSvc, indexerSvc: indexerSvc, posterDir: posterDir, dbPath: dbPath, authSvc: authSvc, secureCookies: secureCookies, mediaSvc: mediaSvc, downloadSvc: downloadSvc, subtitleSvc: subtitleSvc, updaterSvc: updaterSvc, plexProvider: plexProvider, workerRegistry: workerReg, version: version}
}

func (h *Handlers) PosterHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		posterPath := filepath.Join(h.posterDir, fmt.Sprintf("%d.jpg", id))
		info, err := os.Stat(posterPath)
		if err != nil {
			http.Error(w, "poster not found", http.StatusNotFound)
			return
		}

		f, err := os.Open(posterPath)
		if err != nil {
			http.Error(w, "poster not found", http.StatusNotFound)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeContent(w, r, posterPath, info.ModTime(), f)
	}
}

func (h *Handlers) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	resp := GetHealth200JSONResponse{Status: "ok", Version: h.version}

	basePath := h.settings.BasePath()
	if usage, err := diskUsage(basePath); err == nil {
		total := int64(usage.Total)
		used := int64(usage.Used)
		free := int64(usage.Free)
		resp.Disk = &struct {
			FreeBytes  *int64 `json:"freeBytes,omitempty"`
			TotalBytes *int64 `json:"totalBytes,omitempty"`
			UsedBytes  *int64 `json:"usedBytes,omitempty"`
		}{
			TotalBytes: &total,
			UsedBytes:  &used,
			FreeBytes:  &free,
		}
	}

	return resp, nil
}
