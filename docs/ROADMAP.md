# Media Gate — Roadmap

## Phase 0: Project Skeleton ✅
- [x] Go module init (`go mod init`)
- [x] Vue 3 + TypeScript project init (frontend/)
- [x] Minimal OpenAPI spec (health endpoint)
- [x] oapi-codegen setup (strict server generation)
- [x] Frontend API client generation from OpenAPI spec
- [x] Go server serving the embedded Vue SPA
- [x] Makefile for full build pipeline (`build`, `generate`, `dev`, `tools`, `clean`)
- [x] GORM setup with Store interface + SQLite driver
- [x] Basic slog configuration
- [x] API versioning (`/api/v1` routes, `internal/api/v1/` package)
- [x] Nested config structure (api/db/log groups)
- [x] Dev workflow: Air (Go hot-reload) + Vite (frontend HMR)

## Phase 0.5: Frontend Layout ✅
- [x] App shell layout — sidebar + topbar + content area
- [x] Modular component architecture (layout/ + media/ separation)
- [x] Collapsible sidebar navigation
- [x] Search bar in topbar
- [x] Responsive media card grid with poster images
- [x] Dummy data with real TMDB posters for visual prototyping

## Phase 0.75: Libraries & Catalog Sync ✅
- [x] Library CRUD — API endpoints, store layer, service with basePath guard
- [x] Folder browser component for visual path selection
- [x] Libraries admin page (add/edit/delete/scan)
- [x] MediaItem model — tracks folders within a library (title, year, status)
- [x] Sync service — reads library directory, creates/removes MediaItem records
- [x] In-memory job queue with single worker goroutine and duplicate prevention
- [x] API: `GET /libraries/{id}/media`, `POST /libraries/{id}/sync`, `GET /jobs`
- [x] Library detail view with media item grid and sync button
- [x] Dynamic sidebar — fetches libraries from API, shows per-library nav items
- [x] Topbar sync status icon with jobs dropdown panel
- [x] Auto-reload media items when library sync completes (per-library callback)
- [x] Cascade delete media items when library is deleted

## Phase 1a: TMDB/TVDB Integration & Settings ✅
- [x] TMDB client — API v3 integration (search movie/TV, get details, test connection)
- [x] TVDB client — API v4 integration (JWT auth, search series, get details, test connection)
- [x] Settings system — DB-backed settings with sensitive value masking, Settings UI
- [x] Connection test — test API keys on-the-fly (unsaved) or from saved settings
- [x] Media matching — auto-match MediaItems to TMDB/TVDB entries, manual match UI
- [x] Job history persistence — completed/failed jobs stored in SQLite (survives restarts)

## Phase 1b: Core Media Management ⬜
- [x] Rich media detail page (poster, overview, ratings, genres, match info)
- [x] Add media to library — search TMDB/TVDB from library detail page, add as requested item with full metadata
- [x] Delete requested media items (with metadata + poster cleanup)
- [x] Global search bar in topbar triggers add-media panel on library pages
- [x] Global search rework — search overlay with movie/series toggle, media preview page, "Add to Library" modal with library picker, Esc to close
- [x] Entity model redesign — split logical media (MediaItem) from physical files (MediaFile), add QualityProfile and SeasonMonitor models, quality profile CRUD API
- [x] Update media item endpoint (`PATCH /media/{id}`) — assign quality profile, toggle monitored (auto-grab)
- [x] Quality profile selector on media detail page
- [x] Cast & crew display on media detail page
- [x] Collapsible files list on media detail page (collapsed by default)
- [x] Season lists collapsed by default on media detail page
- [x] Media detail top bar — actions, settings & quality profile moved to header row alongside back nav
- [x] IMDb ID tracking — extracted from TMDB/TVDB during matching, stored on MediaMetadata, displayed on media detail page with link
- [x] Match mode modal — library match button offers "unmatched only" or "full re-match all" via modal

## Phase 2: Indexer Integration (Prowlarr replacement) ✅
- [x] Cardigann YAML engine (parse + execute Prowlarr indexer definitions)
- [x] Built-in indexer definitions (ncore.yml embedded via go:embed)
- [x] Indexer CRUD (store model, service, API endpoints)
- [x] Multi-indexer search with parallel execution
- [x] Indexer connection testing
- [x] Indexer management UI (frontend)
- [x] Search results UI (frontend)
- [x] Per-indexer seeding rules (seedMinRatio, seedMinTime)
- [x] "Try it out" modal — meta search + indexer search from indexer management page

## Phase 3: Download Management (Sonarr/Radarr replacement) ✅
- [x] Download model + CRUD API (persistent Download records, POST/GET/PUT /downloads)
- [x] IndexerSearchModal — search indexers from media detail page with indexer dropdown
- [x] Search result season/episode matching — parse SxxExx from torrent titles, sort and highlight full (season+episode) and partial (season) matches in IndexerSearchModal
- [x] Quality profile match indicator — yellow star on search results matching media item's profile (resolution + source)
- [x] Search buttons at item, season, and episode level (EpisodeGrid)
- [x] Episode download status display (computed from Download records, shown in EpisodeGrid)
- [x] Auto-refresh episode list when closing search modal after adding downloads
- [x] qBittorrent API integration (client adapter, settings UI, connection test)
- [x] Download path setting (FolderBrowser selection, mutual exclusion with library paths)
- [x] Download queue management (server worker: send pending → qBit, poll status, seeding rules)
- [x] Download status monitoring (poll qBittorrent, update Download records)
- [x] Authenticated torrent fetch (indexer session cookies, two-step download.selectors resolution)
- [x] qBittorrent category setting (auto-create, defaults to media-gate-dl)
- [x] Download path fix (savepath + downloadPath + useAutoTMM=false for qBit 4.4+)
- [x] Duplicate torrent handling (reuse existing torrent in qBit on retry)
- [x] Downloads section on media detail page (list, progress, retry/delete/replace, torrent file tree)
- [x] DELETE /downloads/{id} endpoint (removes from DB + qBit, optional deleteFiles param)
- [x] Real-time progress/speed enrichment on download list (server-side qBit polling)
- [x] Import worker — hardlink/copy completed downloads into library, create MediaFile records
- [x] Seed cleanup worker — monitor seeding obligations, remove torrents from qBit when met
- [x] Full delete — remove torrents from qBit, imported library files, empty dirs, poster, and cascade DB records
- [x] Post-import resync — trigger ResyncMediaItem after import, frontend auto-refreshes file list on status transition
- [x] FK constraint rebuild migration — startup detects missing FK constraints and rebuilds tables
- [x] Startup integrity checks — orphan record cleanup, torrent hash reconciliation with qBit client
- [x] Download status lifecycle: pending → downloading → downloaded → importing → seeding → completed
- [x] Release folder isolation — each import creates a release subfolder, companion files (subtitles, NFO, images) imported alongside video
- [x] Event bus + SSE — internal event bus (Go channels) with typed events for download lifecycle, library sync/match, media item changes; SSE endpoint for real-time frontend push; replaced polling in useJobQueue, DownloadList, LibraryDetailView, MediaDetailView
- [x] Media item status recalculation — auto-recalc after import, download delete, and re-match based on file presence (available/partial/requested/missing); poster shown for all matched statuses; episode list auto-refreshes on download changes
- [x] Path traversal protection — basePath validation on library/settings/importer; torrent file name validation during import; comprehensive test suite (69 cases)
- [x] Auto-grab monitor worker — background worker polls every 15min for monitored items, searches indexers by IMDb ID with quality profile filtering, auto-creates downloads for released movies and aired episodes
- [x] Season pack preference setting — global setting (prefer_packs/prefer_episodes/packs_only) controls season pack vs individual episode download strategy with 70% threshold
- [x] Season monitor API — per-season monitored toggle (GET/PUT /media/{id}/season-monitors/{seasonNumber})
- [x] Auto-grab UI — monitored toggle on media detail page, "Searching for Xd" indicator, per-season monitored badges in episode grid, release date in stats grid
- [x] Atomic Add-to-Library — external episode prefetch endpoint, extended AddMediaRequest with seasonMonitors/monitored/mediaProfileId, DB transaction wrapping entire create flow (Store.WithTx), fully client-side modal until final submit
- [x] Season monitor modal on enable — toggling monitor ON for a series shows season selector modal (reuses season data from episodes endpoint), PATCH /media/{id} extended with seasonMonitors for atomic upsert, skip season step in Add-to-Library when unmonitored
- [x] Configurable worker poll intervals — DB settings for monitor/download/importer intervals with live notification and dynamic ticker reset, frontend Settings UI
- [x] Typed settings API — replaced generic key-value array with explicit typed fields per setting (string/integer/enum), eliminating stringly-typed bugs
- [x] Auto-download based on watchlist (implemented as auto-grab monitor worker)

## Phase 4: Request System (Overseerr replacement) ⬜
- [x] Requested media items (source: request, status: requested) — foundation
- [x] Quality profiles model + CRUD API (data model ready, frontend deferred)
- [x] Quality profile assignment on media detail page (dropdown + PATCH endpoint)
- [x] MediaFile model for multi-copy/multi-quality file tracking
- [x] SeasonMonitor model for per-season monitoring
- [x] Quality profile UI (list/create/edit)
- [x] Multi-copy handling (same media in different qualities)
- [x] Series episode tracking (which seasons/episodes are present/missing)
- [x] Season bundles vs standalone episodes vs complete series downloads (implemented as season pack preference setting with prefer_packs/prefer_episodes/packs_only modes)
- [x] Library Copies UI — completed downloads split into dedicated section on media detail page, per-release delete with inline confirmation (reuses existing cleanupImportedFiles backend logic)
- [ ] User management (if multi-user)

## Phase 4.5: Security Hardening ✅
- [x] AES-256-GCM encryption for sensitive settings (API keys, passwords) at rest in the database
- [x] Master key via environment variable (`MEDIAGATE_SECRET_KEY`) — SHA-256 key derivation, plaintext fallback for dev mode
- [x] Unified secrets management — indexer credentials (password-type fields) migrated from `Indexer.Settings` JSON to shared `Settings` table with `indexer:{id}:{field}` key pattern
- [x] `internal/crypto` package — stdlib-only AES-256-GCM with `enc:` prefix format for migration detection
- [x] Idempotent startup migrations — `MigrateEncryption` (plaintext → encrypted) and `MigrateCredentials` (Indexer JSON → Settings table)

## Phase 5: Observability & Polish ⬜
- [ ] Initial setup wizard / onboarding flow (guide through API keys, download client, library creation on first launch)
- [ ] Structured log export (file, Loki, etc.)
- [ ] Dashboard / monitoring integration
- [ ] Postgres driver implementation
- [ ] Notification system (TBD)

---

*Phases are rough groupings — items may shift between phases as development progresses. Each phase will be broken down into smaller tasks when we get there.*
