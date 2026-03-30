package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/sumia01/media-gate/frontend"
	apiv1 "github.com/sumia01/media-gate/internal/api/v1"
	"github.com/sumia01/media-gate/internal/config"
	"github.com/sumia01/media-gate/internal/download"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/logging"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/sync"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logging.Setup(cfg.Log.Format, cfg.Log.Level)

	db, err := store.NewSQLite(cfg.DB.Path)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		slog.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	posterDir := ".cache/posters"
	settingsSvc := settings.NewService(db, cfg.Library.BasePath)
	syncSvc := sync.NewService(db)
	matchSvc := matching.NewService(db, settingsSvc, posterDir)

	queue := jobqueue.New(syncSvc, matchSvc, db, 100)
	queue.Start()
	defer queue.Stop()

	indexerSvc, err := indexer.NewService(db)
	if err != nil {
		slog.Error("failed to initialize indexer service", "error", err)
		os.Exit(1)
	}

	libSvc := library.NewService(db, cfg.Library.BasePath, settingsSvc)
	handlers := apiv1.NewHandlers(libSvc, db, queue, settingsSvc, matchSvc, syncSvc, indexerSvc, posterDir)

	// Startup check: reconcile download hashes with torrent client.
	reconcileDownloadsWithTorrentClient(db, settingsSvc)

	downloadSvc := download.NewService(db, settingsSvc, indexerSvc)
	downloadSvc.Start()
	defer downloadSvc.Stop()

	importerSvc := importer.NewService(db, settingsSvc, syncSvc)
	importerSvc.Start()
	defer importerSvc.Stop()

	strictHandler := apiv1.NewStrictHandler(handlers, nil)

	mux := http.NewServeMux()

	// Poster endpoint — raw binary, not part of generated strict server
	mux.HandleFunc("GET /api/v1/media/{id}/poster", handlers.PosterHandler())

	// Mount generated API routes under /api/v1.
	apiHandler := apiv1.HandlerWithOptions(strictHandler, apiv1.StdHTTPServerOptions{
		BaseURL: "/api/v1",
	})

	mux.Handle("/api/", apiHandler)

	// Serve the embedded Vue SPA for everything else.
	spa, err := spaHandler()
	if err != nil {
		slog.Error("failed to setup SPA handler", "error", err)
		os.Exit(1)
	}
	mux.Handle("/", spa)

	addr := fmt.Sprintf(":%d", cfg.API.Port)
	slog.Info("starting server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// reconcileDownloadsWithTorrentClient checks that active download hashes
// still exist in the torrent client. Downloads whose torrents have been
// removed externally are marked as failed. Best-effort — skipped if qBit
// is not configured or unreachable.
func reconcileDownloadsWithTorrentClient(db *store.SQLiteStore, settingsSvc *settings.Service) {
	url, err := settingsSvc.Get(settings.KeyQBitURL)
	if err != nil || url == "" {
		return // qBit not configured
	}
	username, _ := settingsSvc.Get(settings.KeyQBitUsername)
	password, _ := settingsSvc.Get(settings.KeyQBitPassword)

	client := qbittorrent.NewClient(url, username, password)
	if err := client.TestConnection(); err != nil {
		slog.Warn("startup: qBittorrent not reachable, skipping torrent reconciliation", "error", err)
		return
	}

	// Get all torrents from qBit in one call for efficiency.
	torrents, err := client.GetTorrents()
	if err != nil {
		slog.Warn("startup: failed to list torrents from qBittorrent", "error", err)
		return
	}
	hashSet := make(map[string]struct{}, len(torrents))
	for _, t := range torrents {
		hashSet[strings.ToLower(t.Hash)] = struct{}{}
	}

	// Check downloads that should have an active torrent.
	for _, status := range []string{"downloading", "seeding"} {
		downloads, err := db.ListDownloads(nil, &status)
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
				_ = db.UpdateDownload(dl)
			}
		}
	}
}

// spaHandler serves the embedded frontend files. For unknown paths it falls
// back to index.html so that Vue Router's history mode works.
func spaHandler() (http.Handler, error) {
	distFS, err := frontend.DistFS()
	if err != nil {
		return nil, fmt.Errorf("creating frontend fs: %w", err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(distFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}), nil
}
