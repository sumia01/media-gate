# Media Gate

Self-hosted, single-binary media management app (Go backend + Vue 3 frontend). Replaces Sonarr/Radarr/Overseerr/Prowlarr.

## Agent Rules

- **snip is active**: All shell commands are transparently proxied through [snip](https://github.com/edouard-claude/snip), which filters verbose output into compact summaries (e.g. `go test ./...` → `"10 passed, 0 failed"`). The output you receive IS the complete result — it is not truncated or incomplete. NEVER re-run a command because the output looks like a summary. Accept filtered output as-is and act on it.
- **No cached Go tests**: ALWAYS run Go tests with `-count=1` to disable test caching (e.g. `go test -count=1 ./...`). Cached results have caused issues in the past. Never rely on cached test output.

## Commands

```bash
make dev          # Air (Go hot-reload) + Vite (frontend HMR) in parallel
make generate     # Codegen: Go (oapi-codegen) + TypeScript (openapi-typescript)
make build        # Full: generate → frontend → Go binary
make tools        # Install air + oapi-codegen
make frontend     # npm ci + build + copy dist to backend/frontend/dist/
make clean        # Remove build artifacts
```

Go commands run from `backend/`, npm commands from `frontend/`:
```bash
cd backend && go test ./...           # Run all Go tests
cd backend && go test ./internal/crypto/...  # Single package
cd frontend && npm run type-check     # vue-tsc --build
cd frontend && npm run build          # type-check + vite build
```

No linters are configured (no golangci-lint, no eslint/biome).

## Codegen (critical)

OpenAPI spec (`api/openapi.yaml`) is the single source of truth.

1. Edit `api/openapi.yaml`
2. Run `make generate`
3. **Never** hand-edit `backend/internal/api/v1/gen.go` or `frontend/src/api/schema.d.ts`

Go generate directive: `backend/internal/api/v1/generate.go`
TS generate script: `npm run generate:api` in `frontend/`

## Project Layout

Two separate projects under one repo:

- **`backend/`** — Go module (`github.com/sumia01/media-gate`), entrypoint `cmd/server/main.go`
- **`frontend/`** — Vue 3 + TypeScript SPA, Vite, Tailwind v4, `@` alias = `./src`
- **`api/`** — OpenAPI spec + oapi-codegen config (shared by both)

Frontend dist is copied to `backend/frontend/dist/` and embedded via `go:embed` for single-binary output.

Vite dev server proxies `/api` to `http://localhost:8080` (the Go backend).

### Backend `internal/` packages

Handlers in `api/v1/handlers_*.go` (auth, discover, download, indexer, library, media, profile, settings, subtitle, update, watched). Each is a thin HTTP adapter — business logic lives in its own service package.

Key service packages: `auth`, `library`, `sync`, `matching`, `indexer`, `download`, `importer`, `monitor`, `metarefresh`, `media`, `subtitle`, `notification`, `settings`, `updater`.

Supporting packages: `store` (data interface), `eventbus`, `sse`, `jobqueue`, `worker`, `crypto`, `fileparse`, `dateutil`, `telemetry`, `logging`.

Integration clients: `tmdb`, `tvdb`, `qbittorrent`, `discord`, `flaresolverr`, `opensubtitles` (under `integration/`).

## Architecture Rules

### Data access

- All DB access through `Store` interface (`store/store.go`), implemented in `store/sqlite/`. Models in `store/models.go`. Services never touch GORM/DB directly.
- `WithTx(fn func(Store) error)` for multi-step writes.
- All FKs use GORM `constraint:OnDelete:CASCADE` (or `SET NULL`). SQLite FK enforcement via `?_pragma=foreign_keys(1)`. Never write manual cascade deletes.
- **Pure-Go SQLite** (`glebarez/sqlite`). No CGO. Never add CGO-dependent SQLite drivers.

### Migrations

Add new migrations as `func(*sql.DB) error` in `store/sqlite/migrations.go`. Schema version tracked in `settings` table. After AutoMigrate, `runMigrations` runs pending ones. Must set `PRAGMA foreign_keys = ON` explicitly after migrations (AutoMigrate disables FKs during DDL).

### Backend patterns

- **Thin handlers**: `api/v1/handlers_*.go` validate input, call service, map response. `Handlers` struct holds service refs + store (read-only).
- **Event bus + SSE**: `eventbus` dispatches typed events. `sse` streams to frontends. Some event publishers injected via setter methods to avoid circular imports.
- **Shared singletons** — don't duplicate:
  - `qbittorrent.Provider` — lazy-cached qBit client (settings-invalidated)
  - `worker.Loop` — embed for all background workers
  - `matching.Service` — caches TMDB/TVDB clients keyed by API key
  - Shared `*http.Client` with `otelhttp.NewTransport` — created once in `main.go`, injected everywhere
  - `telemetry.Manager` — hot-swaps TracerProvider; noop when disabled

### Security

- **Path traversal**: All filesystem paths validated with `filepath.Clean` + `strings.HasPrefix` against `LIBRARY_BASEPATH`. Three enforcement points: library service, settings service (download path), importer. Always maintain this guard.
- **At-rest encryption**: Sensitive settings use AES-256-GCM, master key from `MEDIAGATE_SECRET_KEY` via SHA-256. `enc:` prefix on ciphertext. Encryption/decryption in `settings.Service` only.
- **Secrets**: Stored with `Sensitive=true` in settings table. Indexer secrets use key pattern `indexer:{id}:{fieldName}`.
- **Auth**: JWT access (15min) + refresh tokens (SHA-256 hashed) in HTTP-only cookies. Login/Refresh/Logout/Setup are manual HTTP handlers (not in OpenAPI — need cookie access). SSE uses single-use 30s tickets.

## Gotchas

- **YAML escape sanitization**: Prowlarr YAML definitions have escapes (`\/`, `\d`) that `yaml.v3` rejects. `SanitizeYAML` preprocesses before parsing. `remote.go` uses regex fallback for ID extraction.
- **Two download paths**: `qbit_download_path` = local mount (import/sync/hardlink). `qbit_save_path` = optional qBittorrent override when its NAS mount differs. When empty, falls back to `qbit_download_path`.
- **Episode monitoring hierarchy**: `EpisodeMonitor` → `SeasonMonitor` → not monitored. Keyed by `(MediaItemID, SeasonNumber, EpisodeNumber)` — NOT by `Episode.ID` — survives re-match. Toggling a season deletes episode overrides. Disabling item monitoring clears all episode monitors.

## Configuration

Config loads from `backend/.env` and/or `MEDIAGATE_`-prefixed env vars (koanf). See `backend/.env.example` for all keys. `SECRET_KEY` is required (encryption + JWT signing).
