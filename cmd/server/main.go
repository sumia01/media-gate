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
	"github.com/sumia01/media-gate/internal/indexer"
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
	settingsSvc := settings.NewService(db)
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

	handlers := apiv1.NewHandlers(library.NewService(db, cfg.Library.BasePath), db, queue, settingsSvc, matchSvc, syncSvc, indexerSvc, posterDir)
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
