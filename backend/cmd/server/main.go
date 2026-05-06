package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"encoding/json"

	"github.com/sumia01/media-gate/frontend"
	apiv1 "github.com/sumia01/media-gate/internal/api/v1"
	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/config"
	"github.com/sumia01/media-gate/internal/download"
	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/integration/opensubtitles"
	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/logging"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/metarefresh"
	"github.com/sumia01/media-gate/internal/monitor"
	"github.com/sumia01/media-gate/internal/notification"
	"github.com/sumia01/media-gate/internal/plexrefresh"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/sse"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	"github.com/sumia01/media-gate/internal/subtitle"
	"github.com/sumia01/media-gate/internal/sync"
	"github.com/sumia01/media-gate/internal/telemetry"
	"github.com/sumia01/media-gate/internal/updater"
	"github.com/sumia01/media-gate/internal/worker"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logging.Setup(cfg.Log.Format, cfg.Log.Level)

	// OpenTelemetry tracing — always wired up; Manager controls whether
	// the global TracerProvider is real or noop.
	otelMgr := telemetry.NewManager(version)
	defer func() {
		if err := otelMgr.Shutdown(context.Background()); err != nil {
			slog.Warn("opentelemetry shutdown error", "error", err)
		}
	}()

	// Shared HTTP client for all external integrations.
	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	db, err := sqlite.New(cfg.DB.Path)
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
	defCacheDir := ".cache/definitions"
	settingsSvc := settings.NewService(db, cfg.Library.BasePath, map[string]string{
		settings.KeyTMDBApiKey:      cfg.TMDB.ApiKey,
		settings.KeyTVDBApiKey:      cfg.TVDB.ApiKey,
		settings.KeyLibraryBasePath: cfg.Library.BasePath,
	}, cfg.Secret.Key, httpClient)
	if err := settingsSvc.MigrateEncryption(); err != nil {
		slog.Error("failed to migrate encryption", "error", err)
		os.Exit(1)
	}

	// Configure OTel from DB settings.
	{
		enabled := settingsSvc.GetWithDefault(settings.KeyOTelEnabled, "false") == "true"
		endpoint := settingsSvc.GetWithDefault(settings.KeyOTelEndpoint, "")
		service := settingsSvc.GetWithDefault(settings.KeyOTelService, "media-gate")
		logLevel := settingsSvc.GetWithDefault(settings.KeyOTelLogLevel, "info")
		if err := otelMgr.Reconfigure(enabled, endpoint, service, logLevel); err != nil {
			slog.Error("failed to configure OTel", "error", err)
		}
	}

	// Auth service.
	if cfg.Secret.Key == "" {
		fmt.Fprintln(os.Stderr, "SECRET_KEY (or MEDIAGATE_SECRET_KEY) is required for JWT signing")
		os.Exit(1)
	}
	authSvc := auth.NewService(db, cfg.Secret.Key)
	if err := authSvc.Bootstrap(cfg.DefaultUser.Email, cfg.DefaultUser.Password); err != nil {
		slog.Error("auth bootstrap failed", "error", err)
		os.Exit(1)
	}
	if err := authSvc.CleanupExpiredTokens(); err != nil {
		slog.Warn("failed to clean up expired tokens", "error", err)
	}

	syncSvc := sync.NewService(db)
	matchSvc := matching.NewService(db, settingsSvc, posterDir, httpClient)
	matchSvc.SetStatusRecalculator(syncSvc)

	// Event bus + SSE broker.
	bus := eventbus.New(1024)
	matchSvc.SetBus(bus)
	syncSvc.SetBus(bus)
	sseBroker := sse.NewBroker()
	bus.SubscribeAll(func(e eventbus.Event) {
		data, err := json.Marshal(e)
		if err != nil {
			return
		}
		sseBroker.Broadcast(string(e.Type), data)
	})

	// Subtitle service.
	osProvider := opensubtitles.NewProvider(settingsSvc, httpClient)
	osAdapter := subtitle.NewOpenSubtitlesProvider(osProvider)
	subtitleSvc := subtitle.NewService(db, settingsSvc, bus, []subtitle.Provider{osAdapter})

	// Plex integration provider.
	plexProvider := plex.NewProvider(settingsSvc, settings.KeyPlexURL, settings.KeyPlexToken, httpClient)

	// Notification service (must subscribe before bus.Start).
	notification.NewService(db, settingsSvc, bus, httpClient)

	// Auto-subtitle search on import (must subscribe before bus.Start).
	bus.Subscribe(eventbus.ImportCompleted, subtitleSvc.HandleImportCompleted)

	// Plex library refresh on import (must subscribe before bus.Start).
	plexRefreshSvc := plexrefresh.NewService(plexProvider, db, slog.Default())
	bus.Subscribe(eventbus.ImportCompleted, plexRefreshSvc.HandleImportCompleted)

	bus.Start()
	defer bus.Stop()

	queue := jobqueue.New(syncSvc, matchSvc, db, 100, bus)
	queue.Start()
	defer queue.Stop()

	indexerSvc, err := indexer.NewService(db, settingsSvc, defCacheDir)
	if err != nil {
		slog.Error("failed to initialize indexer service", "error", err)
		os.Exit(1)
	}
	if err := indexerSvc.MigrateCredentials(); err != nil {
		slog.Error("failed to migrate indexer credentials", "error", err)
		os.Exit(1)
	}

	defRefresher := indexer.NewRefreshWorker(indexerSvc, defCacheDir, settingsSvc)
	defRefresher.Start()
	defer defRefresher.Stop()

	// Worker registry for API exposure (only long-interval workers).
	workerPublisher := worker.MakePublisher(func(evType string, payload any) {
		bus.Publish(eventbus.EventType(evType), payload)
	})
	workerReg := worker.NewRegistry(workerPublisher)
	workerReg.Register(defRefresher.Loop())

	libSvc := library.NewService(db, settingsSvc, settingsSvc)
	qbitProvider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, httpClient)
	mediaSvc := media.NewService(db, syncSvc, bus, qbitProvider, posterDir)

	downloadSvc := download.NewService(db, settingsSvc, indexerSvc, bus, qbitProvider)
	downloadSvc.Reconcile()
	downloadSvc.Start()
	defer downloadSvc.Stop()

	// Self-update service (Linux only, non-dev builds with GitHub credentials).
	var updaterSvc *updater.Service
	if runtime.GOOS == "linux" && version != "dev" && cfg.GitHub.Token != "" && cfg.GitHub.Repo != "" {
		updaterSvc = updater.NewService(version, cfg.GitHub.Token, cfg.GitHub.Repo, settingsSvc, bus)
		if updaterSvc != nil {
			updaterSvc.Start()
			defer updaterSvc.Stop()
			workerReg.Register(updaterSvc.Loop())
			slog.Info("self-update enabled", "repo", cfg.GitHub.Repo)
		}
	}

	handlers := apiv1.NewHandlers(libSvc, db, queue, settingsSvc, matchSvc, syncSvc, indexerSvc, posterDir, cfg.DB.Path, authSvc, cfg.Cookie.Secure, mediaSvc, downloadSvc, subtitleSvc, updaterSvc, plexProvider, workerReg, version)

	// Invalidate cached clients when connection settings change.
	go func() {
		ch := settingsSvc.Subscribe()
		for key := range ch {
			if key == settings.KeyQBitURL || key == settings.KeyQBitUsername || key == settings.KeyQBitPassword {
				qbitProvider.Invalidate()
			}
			if key == settings.KeyOpenSubtitlesApiKey || key == settings.KeyOpenSubtitlesUsername || key == settings.KeyOpenSubtitlesPassword {
				osProvider.Invalidate()
			}
			if key == settings.KeyPlexURL || key == settings.KeyPlexToken {
				plexProvider.Invalidate()
			}
			if key == settings.KeyOTelEnabled || key == settings.KeyOTelEndpoint || key == settings.KeyOTelService || key == settings.KeyOTelLogLevel {
				enabled := settingsSvc.GetWithDefault(settings.KeyOTelEnabled, "false") == "true"
				endpoint := settingsSvc.GetWithDefault(settings.KeyOTelEndpoint, "")
				service := settingsSvc.GetWithDefault(settings.KeyOTelService, "media-gate")
				logLevel := settingsSvc.GetWithDefault(settings.KeyOTelLogLevel, "info")
				if err := otelMgr.Reconfigure(enabled, endpoint, service, logLevel); err != nil {
					slog.Error("failed to reconfigure OTel", "error", err)
				}
			}
		}
	}()

	importerSvc := importer.NewService(db, settingsSvc, syncSvc, bus, qbitProvider)
	importerSvc.Start()
	defer importerSvc.Stop()

	monitorSvc := monitor.NewService(db, indexerSvc, settingsSvc, bus)
	monitorSvc.Start()
	defer monitorSvc.Stop()
	workerReg.Register(monitorSvc.Loop())

	metaRefreshSvc := metarefresh.NewService(db, matchSvc, syncSvc, settingsSvc, bus)
	metaRefreshSvc.Start()
	defer metaRefreshSvc.Stop()
	workerReg.Register(metaRefreshSvc.Loop())

	strictHandler := apiv1.NewStrictHandler(handlers, nil)

	// Build the API mux with all /api/ routes.
	apiMux := http.NewServeMux()

	// SSE endpoint for real-time frontend updates.
	apiMux.Handle("GET /api/v1/events", sseBroker)

	// Poster endpoint — raw binary, not part of generated strict server.
	apiMux.HandleFunc("GET /api/v1/media/{id}/poster", handlers.PosterHandler())

	// Database export — serves the SQLite file as download.
	apiMux.HandleFunc("GET /api/v1/settings/database/export", handlers.DatabaseExportHandler())

	// Rate-limited auth handlers (10 requests per minute per IP).
	authRL := auth.RateLimitMiddleware(10, time.Minute)

	// Manual auth handlers (need cookie access, not in OpenAPI spec).
	apiMux.Handle("POST /api/v1/auth/login", authRL(http.HandlerFunc(handlers.LoginHandler())))
	apiMux.Handle("POST /api/v1/auth/refresh", authRL(http.HandlerFunc(handlers.RefreshHandler())))
	apiMux.HandleFunc("POST /api/v1/auth/logout", handlers.LogoutHandler())
	apiMux.HandleFunc("POST /api/v1/auth/sse-ticket", handlers.SSETicketHandler())

	// Setup handlers (unauthenticated, first-run only).
	apiMux.HandleFunc("GET /api/v1/setup/status", handlers.SetupStatusHandler())
	apiMux.Handle("POST /api/v1/auth/setup", authRL(http.HandlerFunc(handlers.SetupHandler())))

	// Mount generated API routes under /api/v1.
	apiHandler := apiv1.HandlerWithOptions(strictHandler, apiv1.StdHTTPServerOptions{
		BaseURL: "/api/v1",
	})
	apiMux.Handle("/api/", apiHandler)

	// Wrap all API routes with body size limit and auth middleware.
	sized := maxBytesMiddleware(1 << 20)(apiMux) // 1 MB
	authedAPI := auth.AuthMiddleware(authSvc)(sized)

	mux := http.NewServeMux()
	mux.Handle("/api/", authedAPI)

	// Serve the embedded Vue SPA for everything else.
	spa, err := spaHandler()
	if err != nil {
		slog.Error("failed to setup SPA handler", "error", err)
		os.Exit(1)
	}
	mux.Handle("/", spa)

	addr := fmt.Sprintf(":%d", cfg.API.Port)
	slog.Info("starting server", "addr", addr, "version", version)

	// OTel HTTP middleware — always registered; noop provider = zero cost when disabled.
	var handler http.Handler = otelhttp.NewHandler(mux, "media-gate")

	openBrowser(fmt.Sprintf("http://localhost:%d", cfg.API.Port))
	if err := http.ListenAndServe(addr, handler); err != nil {
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

// maxBytesMiddleware limits request body size to prevent memory exhaustion.
func maxBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
