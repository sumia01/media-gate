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
│   │   ├── store/       # Store interface + models (store.go, models.go)
│   │   │   └── sqlite/  # SQLite/GORM implementation — one file per domain (library, media_item, download, user, etc.) + migrations
│   │   ├── indexer/     # Indexer service (CRUD, multi-indexer search, engine lifecycle)
│   │   │   ├── cardigann/   # Cardigann YAML engine (definition parser, login, search, HTML scraping, filters)
│   │   │   └── definitions/ # Embedded indexer definitions (go:embed *.yml)
│   │   ├── eventbus/    # Internal event bus (Go channels, typed events, publish/subscribe)
│   │   ├── sse/         # Server-Sent Events broker (real-time frontend push via GET /api/v1/events)
│   │   ├── download/    # Download service + queue worker (create/update/list with progress, sends pending → qBit, polls status, publishes events)
│   │   ├── importer/    # Import worker (hardlink/copy to library, seed cleanup, publishes events) + filesystem cleanup helpers
│   │   ├── monitor/     # Monitor worker (auto-grab: searches indexers for monitored items, creates downloads)
│   │   ├── metarefresh/ # Metadata refresh worker (periodically checks TMDB/TVDB for new seasons on monitored series)
│   │   ├── media/       # Media orchestration service (delete media item/download/library posters with torrent+disk+DB cleanup)
│   │   ├── integration/
│   │   │   ├── tmdb/    # TMDB API v3 client (search, get, test)
│   │   │   ├── tvdb/    # TVDB API v4 client (JWT auth, search, get, test)
│   │   │   ├── qbittorrent/ # qBittorrent Web API v2 client (cookie auth, add/upload/poll/delete torrents, file listing, shared Provider)
│   │   │   ├── flaresolverr/ # FlareSolverr client (connection test)
│   │   │   ├── discord/ # Discord webhook client (rich embeds, connection test)
│   │   │   └── opensubtitles/ # OpenSubtitles.com REST API client (JWT auth, search, download, file hash)
│   │   ├── subtitle/    # Subtitle service (search, download, scoring, auto-search, provider interface)
│   │   ├── notification/    # Notification service (eventbus subscriber, Discord dispatch)
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

## Key Architecture Rules

These are conventions and rules that cannot be derived from reading the code. For full ADR history see `docs/DECISIONS.md`.

### Data access

- **Store interface**: All data access goes through `Store` interface (`store/store.go`), implemented in `store/sqlite/`. Models in `store/models.go`. Business logic never touches the DB directly. `WithTx(fn func(Store) error)` wraps multi-step writes in a transaction.
- **FK CASCADE at DB level**: All child FKs use GORM `constraint:OnDelete:CASCADE` (or `SET NULL`). SQLite FK enforcement via `?_pragma=foreign_keys(1)`. Never write manual cascade delete code.
- **Versioned schema migrations**: `schema_version` in `settings` table. Add new migrations as `func(*sql.DB) error` in `store/sqlite/migrations.go`. After AutoMigrate, `runMigrations` runs pending ones sequentially. Must set `PRAGMA foreign_keys = ON` explicitly after migrations (AutoMigrate disables FKs during DDL, defer restore may hit wrong pooled connection).

### API & code generation

- **OpenAPI-first**: Change `api/openapi.yaml`, then `make generate`. Never hand-edit `gen.go` or `schema.d.ts`.
- **Single binary**: Vue SPA → `frontend/dist/` → copied to `backend/frontend/dist/` → embedded via `go:embed`. `cd backend && go generate ./...` regenerates Go server code.
- **Pure-Go SQLite**: Uses `glebarez/sqlite` (no CGO). Never add CGO-dependent SQLite drivers.

### Backend patterns

- **Thin HTTP handlers**: Handlers in `api/v1/handlers_*.go` are pure HTTP adapters — validate input, call service, map to API types. All business logic lives in service packages. `Handlers` struct holds only service refs + store (read-only) + settings + auth.
- **Event bus + SSE**: `eventbus` dispatches typed events via Go channels. `sse` broker streams to frontends. To avoid circular imports, some event publishers are injected via setter methods on services.
- **Shared singletons — don't duplicate**:
  - `qbittorrent.Provider` — single source for qBit client creation (lazy-cached, settings-invalidated)
  - `worker.Loop` — embed for background workers (download, importer, monitor, metarefresh)
  - `indexer.FilterByMediaProfile` — single source for profile-based torrent filtering
  - `matching.Service` caches TMDB/TVDB clients keyed by API key (re-creates on change)

### Security

- **Path traversal**: All filesystem paths validated with `filepath.Clean` + `strings.HasPrefix` against `LIBRARY_BASEPATH`. Three enforcement points: library service, settings service (download path), importer.
- **At-rest encryption**: Sensitive settings encrypted with AES-256-GCM, master key from `MEDIAGATE_SECRET_KEY` via SHA-256. `enc:` prefix on ciphertext. Encryption/decryption in `settings.Service` only.
- **Secrets in Settings table**: All credentials stored with `Sensitive=true`. Indexer secrets use key pattern `indexer:{id}:{fieldName}`.
- **Auth flow**: JWT access (15min) + refresh tokens (SHA-256 hashed in DB) in HTTP-only cookies. Login/Refresh/Logout/Setup are manual HTTP handlers (not in OpenAPI — need cookie access). SSE uses single-use 30s tickets (not JWT in URL).

### Gotchas

- **YAML escape sanitization**: Prowlarr YAML definitions contain escapes (`\/`, `\d`) that Go's yaml.v3 rejects. `SanitizeYAML` preprocesses before parsing. `remote.go` uses regex fallback for ID extraction.
- **Two download paths**: `qbit_download_path` = local mount (used by import/sync/hardlink). `qbit_save_path` = optional override sent to qBittorrent when its NAS mount differs. When empty, falls back to `qbit_download_path`.
- **Episode monitoring hierarchy**: `EpisodeMonitor` (explicit per-episode) → `SeasonMonitor` (fallback per-season) → not monitored. Keyed by `(MediaItemID, SeasonNumber, EpisodeNumber)` — NOT by `Episode.ID` — survives re-match. Toggling a season deletes all episode overrides. Disabling monitoring on an item clears all episode monitors.