# Media Gate

Self-hosted, single-binary media management app (Go backend + Vue 3 frontend). Replaces Sonarr/Radarr/Overseerr/Prowlarr/Bazarr.

## Development Status

- Core discover, library, matching, indexer, download/import, monitoring, subtitle, Plex, notification, auth, and self-update flows are implemented.
- The All Downloads page uses bounded newest-first history loading (30 initially, then 100 more), with server-side status filtering, exact qBittorrent completion timestamps for new downloads, and a labelled fallback for legacy finished rows.
- Discover supports a persistent "Hide in library" filter and a bidirectional, lazy-loaded episode timeline for followed series; its initial and Today views center a subtly highlighted current-day column. Direct membership includes media type; TVDB-to-TMDB identity normalization remains deferred.
- Media details show the latest bounded automatic-search decision, with evaluated input freshness separate from completion time. Discord alerts cover persisted terminal download/import failures without retry spam.
- Media details show deduplicated requester attribution, with exact season/episode labels for partial series requests. Repeat requests reuse the media item and additively enable requested monitoring scopes.
- A disposable local/CI harness provides isolated named instances, readable Air/Vite/fake-service logs, and a deterministic fake tracker-to-qBittorrent-to-import smoke flow without real torrent side effects.

## Agent Rules

- **No cached Go tests**: ALWAYS run Go tests with `-count=1` to disable test caching (e.g. `go test -count=1 ./...`). Cached results have caused issues in the past. Never rely on cached test output.

## Commands

```bash
make dev          # Air (Go hot-reload) + Vite (frontend HMR) in parallel
make generate     # Codegen: Go (oapi-codegen) + TypeScript (openapi-typescript)
make build        # Full: generate → frontend → Go binary
make tools        # Install air + oapi-codegen
make frontend     # npm ci + build + copy dist to backend/frontend/dist/
make clean        # Remove build artifacts
make harness-up   # Isolated Air + Vite instance with safe fake integrations
make harness-ci   # Built-binary deterministic release-gate smoke test
```

Go commands run from `backend/`, npm commands from `frontend/`:
```bash
cd backend && go test -count=1 ./...           # Run all Go tests
cd backend && go test -count=1 ./internal/crypto/...  # Single package
cd frontend && npm run type-check     # vue-tsc --build
cd frontend && npm test               # regression tests; use Node.js 24 (native TS imports/module hooks)
cd frontend && npm run build          # type-check + vite build
```

## Disposable Harness

Use `make harness-up` for an isolated Air + Vite instance backed by a local fake
tracker and stateful fake qBittorrent API. Read `tmp/harness/default/manifest.json`
for URLs/credentials and `tmp/harness/default/logs/` for backend, frontend, and
fake-service output. Run `make harness-smoke` for the deterministic
search-to-import flow, `make harness-down` to preserve artifacts, and
`make harness-destroy` to discard everything. Named parallel instances use
`HARNESS_ID=<name>`. Full details are in `docs/HARNESS.md`.

Use the harness when changes affect frontend/API interaction, auth, SSE,
migrations, workers, imports, integration protocols, or multi-service behavior.
Skip it for isolated pure helpers when focused unit tests provide better signal.
Never configure a real tracker for download testing: fetching a `.torrent` may
already count as a snatch. Release-gate harness tests must use fake integrations;
live provider checks are explicit and non-blocking.

Backend uses `golangci-lint` (default linters, no config file). Frontend uses Biome (`biome.json`) for linting and formatting — run `npm run lint` to check, `npm run lint:fix` to auto-fix.

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
- **`frontend/`** — Vue 3 + TypeScript SPA, Vite, Tailwind v4, Lucide icons, `@` alias = `./src`
- **`api/`** — OpenAPI spec + oapi-codegen config (shared by both)

Frontend dist is copied to `backend/frontend/dist/` and embedded via `go:embed` for single-binary output.

Vite dev server proxies `/api` to `VITE_API_PROXY_TARGET`, defaulting to `http://localhost:8080` (the Go backend).

### Backend `internal/` packages

Handlers in `api/v1/handlers_*.go` (auth, database, discover, download, indexer, library, media, plex, profile, settings, subtitle, update, watched, workers). Each is a thin HTTP adapter — business logic lives in its own service package.

Key service packages: `auth`, `library`, `sync`, `matching`, `indexer`, `download`, `importer`, `monitor`, `metarefresh`, `media`, `subtitle`, `notification`, `plexrefresh`, `settings`, `updater`.

Supporting packages: `store` (data interface), `eventbus`, `sse`, `jobqueue`, `worker`, `crypto`, `fileparse` (title parsing: resolution, source, season/episode, language extraction, profile matching), `dateutil`, `telemetry`, `logging`, `devharness` (loopback fake tracker/qBittorrent for disposable tests only).

Integration clients: `tmdb`, `tvdb`, `qbittorrent`, `plex`, `discord`, `flaresolverr`, `opensubtitles` (under `integration/`).

## Architecture Rules

### Data access

- All DB access through `Store` interface (`store/store.go`), implemented in `store/sqlite/`. Models and query projections live in `store/` (`models.go`, `monitor_decision.go`, `timeline.go`). Services never touch GORM/DB directly.
- `WithTx(fn func(Store) error)` for multi-step writes.
- All FKs use GORM `constraint:OnDelete:CASCADE` (or `SET NULL`). SQLite FK enforcement via `?_pragma=foreign_keys(1)`. Never write manual cascade deletes.
- **Pure-Go SQLite** (`glebarez/sqlite`). No CGO. Never add CGO-dependent SQLite drivers.

### Migrations

Schema is managed entirely by `golang-migrate`; GORM AutoMigrate is not used. Add paired embedded SQL files under `store/sqlite/migrations/` as `NNNN_name.up.sql` and `NNNN_name.down.sql`, then bump `latestMigrationVersion` in `store/sqlite/migrator.go`. Version state lives in `schema_migrations`. Preserve the pure-Go SQLite driver and verify fresh install plus legacy adoption tests.

Current migrations reach version 8: `0005` adds latest monitor snapshots, `0006` indexes timeline dates, `0007` adds nullable evaluated-input timestamps, and `0008` adds scoped requester attribution. Legacy snapshots keep unknown input freshness. Tests that rewind a migration version must also remove schema added after that version, not merely change the version row.

### Backend patterns

- **Thin handlers**: `api/v1/handlers_*.go` validate input, call service, map response. `Handlers` struct holds service refs + store (read-only).
- **Event bus + SSE**: `eventbus` dispatches typed events. `sse` streams to frontends. Some event publishers injected via setter methods to avoid circular imports.
- **Persist before lifecycle events**: `UpdateDownload` uses the supplied `UpdatedAt` as an optimistic concurrency check and returns `ErrNotFound` for stale/deleted snapshots. Only successful writes authorize lifecycle publication. Import completion's committed update also authorizes best-effort torrent cleanup; never insert another persistence gate afterward or hold a DB transaction across qBittorrent I/O.
- **Shared singletons** — don't duplicate:
  - `qbittorrent.Provider` — lazy-cached qBit client (settings-invalidated)
  - `plex.Provider` — lazy-cached Plex client (settings-invalidated)
  - `worker.Loop` — embed for all background workers
  - `worker.Registry` — named worker registration, status listing, manual trigger, eventbus bridge via `MakePublisher`
  - `matching.Service` — caches TMDB/TVDB clients keyed by API key
  - Shared `*http.Client` with `otelhttp.NewTransport` — created once in `main.go`, injected everywhere
  - `telemetry.Manager` — hot-swaps TracerProvider + LoggerProvider; noop when disabled. slog tee'd to OTLP via `otelslog` bridge with independent log level.

### Security

- **Path traversal**: All filesystem paths validated with `filepath.Clean` + `strings.HasPrefix` against `LIBRARY_BASEPATH`. Three enforcement points: library service, settings service (download path), importer. Always maintain this guard.
- **At-rest encryption**: Sensitive settings use AES-256-GCM, master key from `MEDIAGATE_SECRET_KEY` via SHA-256. `enc:` prefix on ciphertext. Encryption/decryption in `settings.Service` only.
- **Secrets**: Stored with `Sensitive=true` in settings table. Indexer secrets use key pattern `indexer:{id}:{fieldName}`.
- **Auth**: JWT access (15min) + refresh tokens (SHA-256 hashed) in HTTP-only cookies. Login/Refresh/Logout/Setup are manual HTTP handlers (not in OpenAPI — need cookie access). SSE uses single-use 30s tickets.
- **Admin role**: `IsAdmin` bool on User model. First user promoted via migration V7 + Setup/Bootstrap. Centralized `AdminMiddleware` (operationID-based `StrictMiddlewareFunc`) guards ~40 operations. Manual handlers (DB export) have inline admin checks. Frontend: router guards (`meta.admin`), sidebar filtering, UI element hiding.
- **XSS**: Indexer definition HTML content sanitized via DOMParser-based allowlist sanitizer (`frontend/src/utils/sanitize.ts`).

## Gotchas

- **Discover filtering**: Membership and card identity are keyed by `(source, mediaType, externalId)`, not numeric ID alone. Keep Recently Added unfiltered. Retain raw provider pages, continue past fully hidden pages with a five-page automatic scan budget, and preserve abort/stale-response guards. Cross-provider TVDB/TMDB membership remains unresolved; do not guess by title.
- **Episode timeline**: Followed means a monitored series item. Include dated past/future episodes even when individually unmonitored, resolving episode override > season > false. Fetch half-open `[from, to)` windows (31-day API maximum; frontend uses 14 days), keep nearby cache/DOM bounded, and preserve the visible date during panning and SSE invalidation. Initial load and the Today action must center the current-day column using the measured rail width. Empty windows are not the end of the timeline.
- **Monitor decision freshness**: One latest snapshot per item, at most 50 prioritized details. `CheckedAt` is completion time; nullable `InputUpdatedAt` copies the evaluated item's `UpdatedAt`. Compare server versions only, never a browser receipt timestamp. Season/episode edits touch the parent transactionally; `SetMonitorSearchStartedAt` updates only the marker without bumping `UpdatedAt` or rewriting monitoring settings. Old snapshots remain unknown, not backfilled.
- **Requester attribution**: Persist request intent independently from mutable monitor settings. Scope rows are media, season, or episode; whole-series compaction must account for metadata season count when provider episode fetches are incomplete. Repeat requests are idempotent and may enable scopes, but must never disable monitoring selected by another requester. User deletion keeps anonymous history; media deletion cascades it.
- **Deletion and auto-grab**: Disable parent monitoring and cancel its downloads in one transaction before external media cleanup. The monitor's final fresh-parent check, URL dedup/blocklist check, and insert share a transaction, so an in-flight search cannot re-grab after deletion begins. Never restore a stale whole-item snapshot merely to update a search marker.
- **Import ownership and notifications**: Metadata episode-ID backfill skips `importing` snapshots and revisits deferred work on later successful refreshes, even unchanged ones. Publish terminal failure events only after persistence, not on retries/cancellation. Discord uses fixed safe reasons instead of raw `LastError`, disables mentions, and retains best-effort delivery rather than a durable notification queue.
- **YAML escape sanitization**: Prowlarr YAML definitions have escapes (`\/`, `\d`) that `yaml.v3` rejects. `SanitizeYAML` preprocesses before parsing. `remote.go` uses regex fallback for ID extraction.
- **Two download paths**: `qbit_download_path` = local mount (import/sync/hardlink). `qbit_save_path` = optional qBittorrent override when its NAS mount differs. When empty, falls back to `qbit_download_path`.
- **Episode monitoring hierarchy**: `EpisodeMonitor` → `SeasonMonitor` → not monitored. Keyed by `(MediaItemID, SeasonNumber, EpisodeNumber)` — NOT by `Episode.ID` — survives re-match. Toggling a season deletes episode overrides. Disabling item monitoring clears all episode monitors.
- **Download season pack detection**: `buildDownloadMap` in the monitor uses title parsing (not just `episode_id == nil`) to determine if a download is a season pack. UI-created downloads may have `season_number` set without `episode_id` even for single episodes — relying solely on NULL `episode_id` would block the entire season. Single-episode downloads without `episode_id` are now keyed by `(seasonNumber, episodeNumber)` from title parsing to prevent re-downloads.
- **Episode download status resolution**: `AssembleEpisodes` uses a four-tier `resolveDownloadStatuses()` function: episode-id > episode-key (parsed from title) > season > item. Downloads with `episode_id = NULL` but a parseable single-episode title (e.g. `S01E07`) are scoped to that episode only, not the entire season. True season packs (season-only or episode range titles) still apply season-wide. `metarefresh` backfills `episode_id` on orphan downloads after metadata refresh discovers the episode.
- **Download deduplication**: Three-layer guard prevents duplicate downloads: (1) `store.HasActiveDownloadByURL` checks `(mediaItemID, downloadURL)` before insert, (2) `download.Service.Create()` and `monitor.createAutoDownload()` both call it, (3) frontend tracks download state by URL string (not array index). `store.ActiveDownloadStatuses` is the single source of truth for what "active" means — used by both store and monitor.
- **Downloads history pagination**: The All Downloads page requests a growing newest-first prefix: 30 records initially, then +100. Status filtering must happen before ordering/limiting, filter changes reset the window, and polling/SSE refreshes must retain the current limit. `hasMore` is derived with a `limit + 1` query; do not replace this with mutable offset pages without handling inserts and stale responses.
- **Downloaded timestamp**: `Download.DownloadedAt` records qBittorrent's `completion_on` time when the payload becomes complete. Preserve it through import retries and later lifecycle transitions. Legacy terminal rows stay NULL because `updated_at` and `completed_at` represent different events and are not valid backfills; the UI may show them only as a clearly labelled `Downloaded by` upper-bound fallback.
- **Metadata refresh episode backfill**: `RefreshSeriesMetadata` handles two cases: (1) new season added → backfill old last season + fetch episodes for new seasons, (2) season count unchanged → re-fetch ALL known seasons' episodes and insert any missing ones (`backfillSeasonEpisodes`). Also corrects stale season counts when provider reports fewer than stored. Ended/canceled series are skipped entirely at the `metarefresh` level. TVDB season counting uses `MaxSeasonNumber()` (highest season number) instead of `len(Seasons)` to exclude specials and duplicate orderings.
- **English language fallback**: In `fileparse.MatchesLanguages` and `LanguageScore`, untagged release titles (no detectable language token) are treated as English when `"eng"` is in the profile — applies in BOTH "or" and "and" modes. The `len(detected) == 0` guard is critical: genuinely-detected non-English releases (e.g. `[ger]`) must NOT be morphed into English. The fallback runs after the `multi` short-circuit, so `multi` keeps top priority. Side effect in OR-mode ranking: untagged releases out-rank explicitly-tagged non-English releases when `eng` is first in profile — intentional.
- **Biome + Vue SFCs**: Biome's `noUnusedImports` cannot see `<template>` usage — it WILL remove component imports that are only used in templates. The rule is disabled for `*.vue` files in `biome.json` overrides. NEVER re-enable it without adding `unplugin-vue-components`.

## Production Debugging

Production runs on an LXC accessible via `ssh root@media-gate`. Service: `media-gate.service`.

**Log script**: `scripts/prod-logs.sh` fetches journalctl logs over SSH. Supports level filtering (`-l`), grep (`-g`), follow mode (`-f`), time range (`-s`/`-u`), and save to file (`-o` → `tmp/logs/`). Use the `/prod-logs` command to invoke it interactively.

## Configuration

Config loads from `backend/.env` and/or `MEDIAGATE_`-prefixed env vars (koanf). See `backend/.env.example` for all keys. `SECRET_KEY` is required (encryption + JWT signing).

<!-- icm:start -->
## Persistent memory (ICM) — MANDATORY

This project uses [ICM](https://github.com/rtk-ai/icm) for persistent memory across sessions.
You MUST use it actively. Not optional.

### Recall (before starting work)
```bash
icm recall "query"                        # search memories
icm recall "query" -t "topic-name"        # filter by topic
icm recall-context "query" --limit 5      # formatted for prompt injection
```

### Store — MANDATORY triggers
You MUST call `icm store` when ANY of the following happens:
1. **Error resolved** → `icm store -t errors-resolved -c "description" -i high -k "keyword1,keyword2"`
2. **Architecture/design decision** → `icm store -t decisions-{project} -c "description" -i high`
3. **User preference discovered** → `icm store -t preferences -c "description" -i critical`
4. **Significant task completed** → `icm store -t context-{project} -c "summary of work done" -i high`
5. **Conversation exceeds ~20 tool calls without a store** → store a progress summary

Do this BEFORE responding to the user. Not after. Not later. Immediately.

Do NOT store: trivial details, info already in CLAUDE.md, ephemeral state (build logs, git status).

### Other commands
```bash
icm update <id> -c "updated content"     # edit memory in-place
icm health                                # topic hygiene audit
icm topics                                # list all topics
```
<!-- icm:end -->
