# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Media Gate

Self-hosted, single-binary media management app replacing the Sonarr + Radarr + Overseerr + Prowlarr stack. Go backend + Vue 3 frontend, served as one embedded binary.

## Tech Stack

- **Backend**: Go — stdlib `net/http` (Go 1.22+), GORM (SQLite/Postgres), `log/slog`, koanf (config)
- **Frontend**: Vue 3 + TypeScript (Composition API), Tailwind CSS v4, Vue Router, embedded via `//go:embed`
- **API contract**: OpenAPI spec in `api/` is the single source of truth — Go server code generated with oapi-codegen v2 (strict server mode), TypeScript client generated with openapi-typescript + openapi-fetch

## Project Structure

```
media-gate/
├── backend/
│   ├── cmd/server/      # Go entrypoint (main.go)
│   ├── internal/
│   │   ├── api/v1/      # Generated server + handlers split by domain (library, media, download, indexer, auth, settings, convert)
│   │   ├── auth/        # Auth service (JWT, bcrypt, refresh tokens, middleware, user CRUD, bootstrap)
│   │   ├── config/      # koanf configuration loading
│   │   ├── library/     # Library service (CRUD, path validation, folder browsing, download path conflict check)
│   │   ├── sync/        # Sync service (reads library dirs → creates MediaItems, episode assembly, season monitor upsert, resync with event publish)
│   │   ├── jobqueue/    # Job queue (single worker, history persisted to SQLite)
│   │   ├── matching/    # Media matching service (TMDB/TVDB auto-match + manual, cached clients, add-to-library transaction, external detail)
│   │   ├── crypto/      # AES-256-GCM at-rest encryption for sensitive settings
│   │   ├── dateutil/    # Shared date utilities (ParseYear)
│   │   ├── worker/      # Generic ticker-based worker loop with settings-driven interval
│   │   ├── settings/    # Settings service (CRUD, masking, encryption, connection tests, indexer secret helpers)
│   │   ├── store/       # Store interface + GORM implementations (Library, MediaItem, MediaFile, QualityProfile, SeasonMonitor, Episode, Setting, JobRecord, Indexer, Download, User, RefreshToken)
│   │   ├── indexer/     # Indexer service (CRUD, multi-indexer search, engine lifecycle)
│   │   │   ├── cardigann/   # Cardigann YAML engine (definition parser, login, search, HTML scraping, filters)
│   │   │   └── definitions/ # Embedded indexer definitions (go:embed *.yml)
│   │   ├── eventbus/    # Internal event bus (Go channels, typed events, publish/subscribe)
│   │   ├── sse/         # Server-Sent Events broker (real-time frontend push via GET /api/v1/events)
│   │   ├── download/    # Download service + queue worker (create/update/list with progress, sends pending → qBit, polls status, publishes events)
│   │   ├── importer/    # Import worker (hardlink/copy to library, seed cleanup, publishes events) + filesystem cleanup helpers
│   │   ├── monitor/     # Monitor worker (auto-grab: searches indexers for monitored items, creates downloads)
│   │   ├── media/       # Media orchestration service (delete media item/download/library posters with torrent+disk+DB cleanup)
│   │   ├── integration/
│   │   │   ├── tmdb/    # TMDB API v3 client (search, get, test)
│   │   │   ├── tvdb/    # TVDB API v4 client (JWT auth, search, get, test)
│   │   │   ├── qbittorrent/ # qBittorrent Web API v2 client (cookie auth, add/upload/poll/delete torrents, file listing, shared Provider)
│   │   │   └── flaresolverr/ # FlareSolverr client (connection test)
│   │   └── logging/     # slog setup
│   ├── frontend/        # embed.go — embeds compiled SPA into Go binary
│   ├── go.mod
│   ├── go.sum
│   ├── .air.toml        # Air hot-reload config for Go dev
│   └── .env.example     # Documented configuration keys
├── frontend/            # Vue 3 + TypeScript SPA
│   └── src/
│       ├── api/         # Generated TypeScript API client
│       ├── types/       # Shared API type re-exports from schema
│       ├── utils/       # Shared utility functions (parseGenres, posterUrl, formatSize, formatBytes)
│       ├── composables/ # Shared reactive state (useJobQueue, useEventStream, useGlobalSearch, useSidebarLibraries, useAuth)
│       ├── components/
│       │   ├── layout/  # App shell: sidebar, topbar, page layout
│       │   └── media/   # Media-related components + shared types
│       ├── views/       # Route-level page components
│       │   └── setup/   # Setup wizard step components
│       └── router/      # Vue Router config
├── api/                 # OpenAPI spec + oapi-codegen config (shared by backend + frontend)
├── docs/                # Architecture, decisions, roadmap
├── deploy/              # Proxmox LXC deploy script
├── .github/workflows/   # GitHub Actions (release build pipeline)
├── Dockerfile.build     # Multi-stage cross-platform builder
└── Makefile             # Build pipeline
```

## Build & Run

```bash
# Full build (generate + frontend + Go binary)
make build

# Run the compiled binary
./media-gate

# Development — Air (Go hot-reload) + Vite (frontend HMR) in parallel
make dev

# Install dev tools (air, oapi-codegen)
make tools

# Code generation only (Go + TypeScript from OpenAPI spec)
make generate

# Frontend build only
make frontend

# Cross-platform prod builds (Docker required)
make build-linux-amd64    # → dist/media-gate-linux-amd64
make build-darwin-arm64   # → dist/media-gate-darwin-arm64
make build-windows-amd64  # → dist/media-gate-windows-amd64.exe
make build-all            # all three platforms

# Clean build artifacts
make clean
```

## Configuration

Configuration loads from `backend/.env` file and/or `MEDIAGATE_`-prefixed environment variables. Config is organized into nested groups (api, db, log, library). Underscore in key names maps to nesting level.

| .env key | Env var | Config field | Default | Description |
|----------|---------|-------------|---------|-------------|
| `API_PORT` | `MEDIAGATE_API_PORT` | `API.Port` | `8080` | HTTP server port |
| `DB_PATH` | `MEDIAGATE_DB_PATH` | `DB.Path` | `media-gate.db` | SQLite database path |
| `LOG_LEVEL` | `MEDIAGATE_LOG_LEVEL` | `Log.Level` | `info` | Log level (debug/info/warn/error) |
| `LOG_FORMAT` | `MEDIAGATE_LOG_FORMAT` | `Log.Format` | `text` | Log format (text/json) |
| `LIBRARY_BASEPATH` | `MEDIAGATE_LIBRARY_BASEPATH` | `Library.BasePath` | `/mnt` | Base path for library directories (path traversal guard) |
| `TMDB_APIKEY` | `MEDIAGATE_TMDB_APIKEY` | `TMDB.ApiKey` | _(empty)_ | TMDB API key fallback (used when not set in DB/UI) |
| `TVDB_APIKEY` | `MEDIAGATE_TVDB_APIKEY` | `TVDB.ApiKey` | _(empty)_ | TVDB API key fallback (used when not set in DB/UI) |
| `SECRET_KEY` | `MEDIAGATE_SECRET_KEY` | `Secret.Key` | _(empty)_ | Master key for encrypting secrets at rest and JWT signing (**required**) |
| `DEFAULTUSER_EMAIL` | `MEDIAGATE_DEFAULTUSER_EMAIL` | `DefaultUser.Email` | _(empty)_ | Bootstrap user email (optional — setup wizard is primary; used only when no users in DB) |
| `DEFAULTUSER_PASSWORD` | `MEDIAGATE_DEFAULTUSER_PASSWORD` | `DefaultUser.Password` | _(empty)_ | Bootstrap user password (optional — setup wizard is primary; used only when no users in DB) |
| `COOKIE_SECURE` | `MEDIAGATE_COOKIE_SECURE` | `Cookie.Secure` | `false` | Force Secure flag on cookies (set `true` behind TLS-terminating reverse proxy) |

## Key Architecture Decisions

- **Store interface pattern**: All data access goes through a Go `Store` interface with GORM implementations. Business logic never touches the database directly. `WithTx(fn func(Store) error)` wraps multi-step writes in a DB transaction — the callback receives a transactional Store instance.
- **FK CASCADE at DB level**: All child foreign keys use GORM `constraint:OnDelete:CASCADE` (or `SET NULL` for nullable FKs). SQLite FK enforcement via `?_foreign_keys=ON` DSN parameter. Startup migration rebuilds tables missing FK constraints (AutoMigrate can't add FKs to existing SQLite tables). Orphan cleanup runs on every startup. No manual cascade delete code in handlers.
- **OpenAPI-first**: Change the spec in `api/openapi.yaml`, then run `make generate`. Never hand-edit generated code (`backend/internal/api/v1/gen.go`, `frontend/src/api/schema.d.ts`).
- **Versioned API**: Routes under `/api/v1`, Go code in `backend/internal/api/v1/` (package `apiv1`). Future versions get their own package.
- **Single binary**: The Vue SPA builds into `frontend/dist/`, gets copied to `backend/frontend/dist/`, and embedded into the Go binary via `backend/frontend/embed.go`. No separate web server needed.
- **go:generate**: `cd backend && go generate ./...` runs oapi-codegen to regenerate Go server code.
- **Modular frontend**: Components organized by concern — `layout/` for shell pieces, `media/` for domain components, `views/` for route-level pages.
- **Event bus + SSE**: Internal event bus (`backend/internal/eventbus/`) using Go channels dispatches typed events (download lifecycle, library sync/match, media item changes, monitor grabs). Workers and handlers publish events on state transitions. SSE broker (`backend/internal/sse/`) subscribes to all events and streams them to connected frontends via `GET /api/v1/events`. Frontend `useEventStream` composable provides reactive SSE subscription — replaces polling for job status, download progress, and media item updates. The matching service publishes `media.item_matched` after each successful match (including status recalculation), and the resync handler publishes `media.resync_completed` after re-scanning files — both injected via setter methods to avoid circular imports.
- **Path traversal protection**: All filesystem paths are validated with `filepath.Clean` + `strings.HasPrefix` against `LIBRARY_BASEPATH`. Three enforcement points: library service (Create/Update/Browse), settings service (download path), and importer (torrent file names from qBittorrent API). See ADR-045.
- **At-rest encryption**: Sensitive settings (API keys, passwords, indexer credentials) are encrypted with AES-256-GCM before storing in the DB. Master key derived from `MEDIAGATE_SECRET_KEY` env var via SHA-256. Encrypted values use `enc:` prefix for migration detection. Without a key, values are stored in plaintext (dev mode). Encryption/decryption happens in `settings.Service`; the store layer always sees ciphertext. See ADR-056.
- **Unified secrets management**: All credentials (TMDB/TVDB keys, qBit password, indexer passwords/2FA) are stored in the `Settings` table with `Sensitive=true`. Indexer password-type fields are stored with key pattern `indexer:{id}:{fieldName}` and excluded from the settings API. Non-sensitive indexer fields remain in the `Indexer.Settings` JSON column. See ADR-057.
- **JWT + refresh token auth**: Short-lived JWT access tokens (15 min, HS256) + long-lived refresh tokens (24h/30d with remember-me, stored as SHA-256 hashes) in HTTP-only cookies. Auth middleware on all `/api/` routes, skips login/refresh/health/setup-status. Login/Refresh/Logout/Setup are manual HTTP handlers (need cookie access, not in OpenAPI spec). SSE uses single-use ticket system: frontend exchanges JWT for a 30s ticket via `POST /api/v1/auth/sse-ticket`, then opens EventSource with `?ticket=` — avoids exposing JWT in URLs. Default user bootstrapped from env vars. Passwords require minimum 8 characters. Cookie `Secure` flag respects `COOKIE_SECURE` config and `X-Forwarded-Proto` header for reverse-proxy deployments. See ADR-058.
- **Setup wizard / onboarding**: 6-step browser-based wizard on first launch (`/setup`). `POST /api/v1/auth/setup` creates first user (guarded by 0-user check), `GET /api/v1/setup/status` returns onboarding state. Progress tracked via `onboarding_step`/`onboarding_completed` settings. Frontend router guard redirects all routes to wizard when incomplete. Existing installations auto-detected as completed. `LIBRARY_BASEPATH` is a DB-backed setting with env fallback; library service uses `BasePathProvider` interface for dynamic resolution. See ADR-059.
- **Pure-Go SQLite**: Uses `glebarez/sqlite` (wraps `modernc.org/sqlite`) instead of `mattn/go-sqlite3`. No CGO required — enables trivial cross-compilation with `GOOS`/`GOARCH` without C toolchains. See ADR-069.
- **Cross-platform prod builds**: `Dockerfile.build` multi-stage builder (Node frontend → Go binary) with `--output` extraction. Three Makefile targets (`build-linux-amd64`, `build-darwin-arm64`, `build-windows-amd64`) produce standalone binaries in `dist/`. CGO_ENABLED=0, no C compiler needed. See ADR-069.
- **Shared profile filter**: `indexer.FilterByProfile` (`backend/internal/indexer/filter.go`) is the single source of truth for profile-based torrent result filtering (resolution, source, exclude tags). Used by both the monitor auto-grab worker and the `GET /media-profiles/{id}/test-search` endpoint. Never duplicate this logic. See ADR-072.
- **CI/CD release pipeline**: GitHub Actions workflow (`.github/workflows/release.yml`) triggers on `v*` tag push, builds frontend + runs Go code generation + cross-compiles 3 platform binaries, creates GitHub Release with assets, and syncs the deploy script to a public Gist. See ADR-073.
- **Proxmox LXC deployment**: `deploy/proxmox-lxc.sh` is an interactive script that creates a Debian 12 LXC container on Proxmox, downloads the binary from GitHub Release (via PAT), sets up systemd service, optionally configures CIFS NAS mount, supports DB migration from existing installs, and installs an in-place update script. See ADR-073.
- **YAML escape sanitization**: Prowlarr upstream YAML definitions use escape sequences (`\/`, `\d`) that Go's `yaml.v3` (YAML 1.2) rejects. `SanitizeYAML` (`backend/internal/indexer/cardigann/sanitize.go`) preprocesses double-quoted strings before parsing, and `remote.go` uses regex fallback for ID extraction when header parse fails. See ADR-074.
- **Thin HTTP handlers**: API handlers (`backend/internal/api/v1/handlers_*.go`) are pure HTTP adapters — validate input, call a service method, map result to API types. All business logic, side effects (event publishing, status recalculation), and multi-step orchestration live in service packages (`media.Service`, `download.Service`, `sync.Service`, `matching.Service`). The `Handlers` struct holds only service references, `store.Store` (for read-only queries/404 checks), `settings`, and auth — no `eventbus.Bus` or `qbittorrent.Provider`. See ADR-079.
- **SSE ticket authentication**: SSE endpoint no longer accepts JWT in query string (`?token=`). Frontend exchanges JWT for a single-use, 30-second ticket via `POST /api/v1/auth/sse-ticket`, then opens EventSource with `?ticket=`. Auth middleware redeems and deletes the ticket on first use. `TicketStore` is in-memory with lazy cleanup. See ADR-075.
- **Auth rate limiting**: Per-IP fixed-window rate limiter (`backend/internal/auth/ratelimit.go`) limits login, setup, and refresh endpoints to 10 requests per minute per IP. Extracts client IP from `X-Forwarded-For` → `X-Real-IP` → `RemoteAddr`. Returns 429 with `Retry-After` header. See ADR-075.
- **Refresh token hashing**: Refresh tokens are stored as SHA-256 hashes in the database. Plaintext is returned to the client (cookie) but never persisted. Protects active sessions if the database file is compromised. See ADR-075.
- **Shared qBittorrent Provider**: `qbittorrent.Provider` (`backend/internal/integration/qbittorrent/provider.go`) is the single source of truth for qBit client creation. Lazy-cached with mutex, invalidated via goroutine watching settings changes. Uses `SettingsGetter` interface to avoid circular import with settings package. See ADR-078.
- **Generic worker loop**: `worker.Loop` (`backend/internal/worker/loop.go`) provides ticker-based background processing with settings-driven interval, configurable startup delay, and automatic interval updates on settings change. Download, importer, and monitor services embed `*worker.Loop`. See ADR-078.
- **Shared profile filter**: `indexer.FilterByMediaProfile` (`backend/internal/indexer/filter.go`) accepts `*store.MediaProfile` directly and unmarshals JSON criteria internally. Single source of truth for profile-based torrent filtering. See ADR-078.
- **Cached TMDB/TVDB clients**: `matching.Service` caches TMDB and TVDB clients keyed by API key (`cachedTMDB`/`cachedTVDB`). Re-creates on key change. Avoids wasteful re-authentication especially for TVDB's JWT handshake. See ADR-078.

## Development Status

Project has completed **Phase 0** (scaffolding), **Phase 0.5** (frontend layout), **Phase 0.75** (libraries & catalog sync), **Phase 1a** (TMDB/TVDB integration, settings, media matching & job history persistence), **Phase 2** (indexer integration — Cardigann engine, remote indexer definitions from Prowlarr/Indexers GitHub tarball with disk cache and background refresh, indexer management UI, search results UI, per-indexer seeding rules), and is progressing through **Phase 1b** (core media management), **Phase 3** (download management — Download model + CRUD API, IndexerSearchModal with item/season/episode search, search result season/episode title parsing with match highlighting, episode download status, qBittorrent client adapter with authenticated torrent fetch and file upload, download path + category settings, download queue worker with seeding rules and duplicate handling, downloads section on media detail page with progress/retry/delete/replace and torrent file listing, import worker with hardlink/copy to library and seed cleanup, release folder isolation with companion file import, full delete with torrent/file/empty-dir cleanup, post-import resync and frontend auto-refresh, FK constraint rebuild migration and startup orphan/torrent reconciliation, event bus + SSE refactor replacing polling with real-time push, media item status recalculation based on file presence, path traversal protection with comprehensive test suite, monitor worker with auto-grab for movies and series based on quality profiles and season pack preference setting, atomic Add-to-Library with external episode prefetch and DB transaction via Store.WithTx, season monitor modal when enabling monitoring on detail page with atomic seasonMonitors upsert on PATCH, configurable worker poll intervals with live settings notification and dynamic ticker reset, typed settings API replacing generic key-value array with explicit fields per setting), **Phase 4** (multi-copy handling, Library Copies UI for per-release file management via completed download records, user management with JWT + refresh token auth, login/profile/users/registration views, auth middleware on all routes), and **Phase 4.5** (security hardening — AES-256-GCM at-rest encryption for sensitive settings, unified secrets management moving indexer credentials to Settings table, master key via `MEDIAGATE_SECRET_KEY`, idempotent startup migrations), **Phase 4.6** (initial setup wizard — 6-step onboarding flow, unauthenticated first-user creation, dynamic library base path with `BasePathProvider` interface, DB-backed `LIBRARY_BASEPATH` with env fallback, lenient server bootstrap without users), **Phase 4.7** (discover page — dynamic home page with recently added from libraries, TMDB trending/popular movies/series, 4 independent API endpoints with graceful degradation, `DiscoverItem` schema, skeleton loading states), and **Phase 4.8** (Cardigann engine hardening — cookie login, charset encoding, URL fixes, search headers, JSON response parsing with 4 pattern support, FlareSolverr proxy integration, fuzzy definition picker, info settings display, indexer test scoping, try-it-out query + overflow fixes, media detail search query fallback), and **Phase 4.9** (download retry with exponential backoff — 5-attempt retry with 30s→1h backoff for transient qBit/indexer failures, qBit health check before send pass, retry state on Download model, UI error display and countdown), and **Phase 5.0** (cross-platform prod builds — pure-Go SQLite driver replacing CGO-dependent mattn/go-sqlite3, multi-stage Dockerfile.build with Node frontend + Go cross-compile, Makefile targets for linux/amd64 + darwin/arm64 + windows/amd64, CGO_ENABLED=0), and **Phase 5.1** (profile test search — `GET /media-profiles/{id}/test-search` endpoint with shared `indexer.FilterByProfile` function as single source of truth for profile-based filtering, 3-step wizard modal with TMDB/TVDB media search and filtered indexer results showing auto-grab pick), and **Phase 5.2** (CI/CD + Proxmox LXC deployment — GitHub Actions release pipeline triggered by semver tags building 3 platform binaries as GitHub Releases, public Gist sync for deploy script, interactive Proxmox LXC creation with systemd service and optional CIFS NAS mount and DB migration, in-place update script), and **Phase 5.3** (branding — custom logo replacing text "MG" placeholder on login, setup, and sidebar, page title set to "MediaGate", levendula text color with white glow matching logo inner lines), and **Phase 5.4** (YAML escape sanitization — `SanitizeYAML` preprocessor for Prowlarr upstream definitions with invalid YAML 1.2 escapes, regex fallback for ID extraction in remote/cached definition loading), and **Phase 5.5** (backend directory restructuring — moved all Go files into `backend/` subdirectory, `api/` stays at repo root shared by both sides, Go module root = `backend/` so zero import path changes needed, updated Makefile/Dockerfile/CI/air config), and **Phase 5.6** (security hardening round 2 — SSE ticket system replacing JWT-in-URL with single-use 30s tickets, per-IP rate limiting on auth endpoints 10 req/min, 1MB request body size limit middleware, refresh tokens stored as SHA-256 hashes, password minimum 8 characters, setup endpoint race condition mutex, `COOKIE_SECURE` config for reverse-proxy deployments with `X-Forwarded-Proto` detection, URL validation on FlareSolverr/qBit settings save, debug log URL query-param redaction, wildcard CORS removal from SSE), and **Phase 5.7** (dead code cleanup — removed unused exported methods `RevokeAllUserTokens`/`BroadcastJSON`/`AddTorrent`+helpers/`Caps()`, dead event constants `ImportStarted`/`MediaItemSynced`/`MediaItemRemoved`/`MediaItemDeleteReq`+payload type, dead struct field `SearchResult.Description`; verified Cardigann YAML schema and template-consumed fields are NOT dead), and **Phase 5.8** (code duplication cleanup — shared `qbittorrent.Provider` with lazy caching and settings-change invalidation replacing 4 duplicate client constructors, generic `worker.Loop` replacing 3 identical Start/Stop/run patterns in download/importer/monitor, `indexer.FilterByMediaProfile` accepting `*store.MediaProfile` directly, unified `fetchDiscover` helper and `toDiscoverItem` converter replacing 3 near-identical discover handlers, shared `dateutil.ParseYear` replacing 2 implementations, `applyProfileFields` merging profile create/update, `derefString` helper, cached TMDB/TVDB clients in matching service keyed by API key), and **Phase 5.9** (domain boundary enforcement — moved business logic out of HTTP handlers into services: `media.Service` for delete orchestration of media items/downloads/library posters with torrent+disk+DB cleanup, `download.Service` expanded with Create/UpdateStatus/ListWithProgress/ListTorrentFiles/Reconcile, `sync.Service` expanded with AssembleEpisodes and UpsertSeasonMonitors and internal ResyncMediaItem event publish+recalc, `matching.Service` expanded with ManualMatch returning item+meta, AddMediaToLibraryFull wrapping WithTx, TMDBClient for discover, `importer` package gained exported RemoveEmptyParents/OnlyCompanionsLeft filesystem helpers, `flaresolverr` client package extracted from settings, removed `safenet` SSRF package as counterproductive for self-hosted app with local services, removed `bus` and `qbit` fields from Handlers struct). See `docs/ROADMAP.md` for the full plan and `docs/DECISIONS.md` for ADRs.