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
- [x] Remote indexer definitions — fetch ~550 Prowlarr/Indexers v11 YAML definitions from GitHub tarball, disk cache with 24h TTL, background refresh worker, embedded fallback when offline
- [x] Cardigann `ErrorBlock.Message` flexible parsing (string or `{text: "..."}` map form for v11 compatibility)
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

## Phase 4: Request System (Overseerr replacement) ✅
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
- [x] User management — email/password auth with JWT access tokens + refresh token rotation, bcrypt hashing, remember-me, HTTP-only cookie refresh tokens, auth middleware on all API routes, SSE token query param fallback, default user bootstrap from env vars, login/profile/change-password/user-list/register/delete views

## Phase 4.5: Security Hardening ✅
- [x] AES-256-GCM encryption for sensitive settings (API keys, passwords) at rest in the database
- [x] Master key via environment variable (`MEDIAGATE_SECRET_KEY`) — SHA-256 key derivation, plaintext fallback for dev mode
- [x] Unified secrets management — indexer credentials (password-type fields) migrated from `Indexer.Settings` JSON to shared `Settings` table with `indexer:{id}:{field}` key pattern
- [x] `internal/crypto` package — stdlib-only AES-256-GCM with `enc:` prefix format for migration detection
- [x] Idempotent startup migrations — `MigrateEncryption` (plaintext → encrypted) and `MigrateCredentials` (Indexer JSON → Settings table)

## Phase 4.6: Initial Setup Wizard ✅
- [x] Setup wizard / onboarding flow — 6-step guided setup on first launch (account, base path, torrent client, indexer, TMDB, TVDB)
- [x] `POST /api/v1/auth/setup` — unauthenticated first-user creation with auto-login
- [x] `GET /api/v1/setup/status` — onboarding state endpoint (needsSetup, step, completed)
- [x] Dynamic library base path — `LIBRARY_BASEPATH` moved from env-only to DB-backed setting with env fallback (`BasePathProvider` interface)
- [x] Server starts without users — lenient `Bootstrap()` logs info instead of erroring when no users and no env vars
- [x] Frontend router guard redirects all routes to `/setup` when onboarding incomplete
- [x] Existing installations auto-detected as completed (users exist → wizard never shown)
- [x] Progress persistence — wizard resumes at correct step on page refresh via `onboarding_step` setting

## Phase 4.7: Discover Page ✅
- [x] TMDB client extensions — `TrendingAll(timeWindow)`, `PopularMovies()`, `PopularTV()` methods with `VoteAverage` field on result types
- [x] Store method — `ListRecentMediaItems(limit)` for cross-library recently added query
- [x] 4 new API endpoints — `GET /discover/recently-added`, `/discover/trending`, `/discover/popular-movies`, `/discover/popular-series`
- [x] `DiscoverItem` schema in OpenAPI — source, externalId, title, overview, year, posterUrl, mediaType, rating
- [x] Discover handlers — graceful degradation (empty array when TMDB key not configured)
- [x] HomeView rewrite — 4 sections with independent loading, skeleton placeholders, click navigation to media detail / external preview
- [x] Removed static demo data (`dummyData.ts`, `MediaCardA.vue`)

## Phase 4.8: Cardigann Engine Hardening ✅
- [x] Cookie-based login (`login.method: cookie`) — cookie string injection into HTTP client jar + session verification
- [x] Character encoding support — `readBody()` converts non-UTF-8 responses (e.g. ISO-8859-2) via `golang.org/x/text/encoding/ianaindex`
- [x] URL building fix — proper `&` separator between encoded params and `$raw` suffix
- [x] Search path template rendering — `RenderTemplate` on `search.paths[].path` for dynamic URLs
- [x] Browser User-Agent header — `uaTransport` RoundTripper injects Chrome UA on all requests
- [x] Field `default` support — `FieldDef.Default` with template rendering referencing `.Result`
- [x] Search headers support — `search.headers` with template rendering (e.g. `x-milkie-auth: {{ .Config.apikey }}`)
- [x] JSON response parsing — `response: type: json` detection, `resolveJSONPath` for dot-path/array-index/parent traversal, `jsonValueToString` with proper number/bool formatting, `parseRowsJSON` handling flat arrays, root arrays, attribute sub-objects, and nested arrays with `multiple: true`
- [x] FlareSolverr integration — global URL setting, `doRequest()` wrapper routing GET requests through FlareSolverr `/v1` API for Cloudflare-protected indexers, cookie injection from solved responses, Settings UI with test connection, indexer form warning when FlareSolverr needed but not configured
- [x] Fuzzy-searchable dropdown for indexer definition picker
- [x] Info settings display — read-only `info_*` type settings shown in indexer add/edit form
- [x] Indexer test button scoping — test result shown only next to the tested indexer
- [x] Try-it-out query fix — candidate title sent alongside IMDB ID for indexers that don't support IMDB search
- [x] Try-it-out modal overflow fix — scrollable results within fixed-height modal
- [x] Media detail search query fix — IndexerSearchModal now sends title (without year suffix) as text query fallback for indexers that don't support IMDB search
- [x] Download retry with exponential backoff — automatic retry (5 attempts, 30s→1h backoff) for transient failures (qBit unreachable, indexer fetch errors), qBit health check skips send pass when offline, manual retry resets state, UI shows last error and retry countdown

## Phase 4.9: Download Retry Resilience ✅
- [x] Download retry with exponential backoff (30s, 2m, 10m, 30m, 1h — max 5 retries)
- [x] qBit health check before send pass — skip entirely when unreachable, don't consume retries
- [x] `RetryCount`, `NextRetryAt`, `LastError` fields on Download model + SQLite DDL
- [x] Manual retry resets retry state (count, backoff, error)
- [x] Frontend shows last error for failed downloads, retry count and countdown for pending downloads in backoff
- [x] OpenAPI schema updated with retry fields

## Phase 5.0: Cross-Platform Prod Builds ✅
- [x] Pure-Go SQLite driver (`glebarez/sqlite` replacing CGO-dependent `mattn/go-sqlite3`)
- [x] `Dockerfile.build` — multi-stage builder (Node frontend + Go cross-compile, `--output` extraction)
- [x] Makefile targets: `build-linux-amd64`, `build-darwin-arm64`, `build-windows-amd64`, `build-all`
- [x] All builds use `CGO_ENABLED=0` — no C compiler or SDK required
- [x] TypeScript strict mode fixes (setup wizard `ConnectionTestResult`, layout `noUncheckedIndexedAccess`)

## Phase 5.1: Profile Test Search ⬜
- [x] `GET /media-profiles/{id}/test-search` API endpoint — searches indexers and applies profile filter server-side
- [x] Shared `indexer.FilterByProfile` function (`internal/indexer/filter.go`) — single source of truth for profile-based result filtering, used by both monitor auto-grab and test-search endpoint
- [x] `ProfileTestSearchResult` OpenAPI schema — returns profile name, total/filtered counts, filtered torrent results
- [x] `TestProfileModal.vue` — 3-step wizard modal (TMDB/TVDB media search → season selection for series → filtered indexer results with auto-grab pick highlighted)
- [x] "Test" button on each profile row in the Profiles page
- [ ] Optional: IMDb ID passthrough for more precise indexer search
- [ ] Optional: Season dropdown from external media detail

## Phase 5.2: CI/CD & Proxmox LXC Deployment ⬜
- [x] GitHub Actions release workflow — `v*` tag push triggers frontend build + Go cross-compile for 3 platforms, creates GitHub Release with binary assets
- [x] Public Gist sync — deploy script auto-published to Gist on each release for easy access from Proxmox hosts
- [x] Proxmox LXC deploy script — interactive `pct create` with Debian 12, binary download via GitHub PAT, systemd service, security hardening
- [x] Optional CIFS NAS mount — credentials file, fstab entry, mount verification
- [x] DB migration support — `pct push` existing DB + matching secret key for container rebuilds/host moves
- [x] In-place update script — `media-gate-update` downloads latest (or specific) release, swaps binary, restarts service
- [ ] GitHub repo: add `GIST_TOKEN` secret (PAT with `gist` scope)
- [ ] GitHub repo: first `git tag v1.0.0 && git push --tags` to test pipeline

## Phase 5.4: Observability & Polish ⬜
- [ ] Structured log export (file, Loki, etc.)
- [ ] Dashboard / monitoring integration
- [ ] Postgres driver implementation
- [ ] Notification system (TBD)

## Phase 5.3: Branding ✅
- [x] Custom logo image (`small_logo.png`) replacing text "MG" placeholder on login, setup wizard, and sidebar
- [x] Page title set to "MediaGate"
- [x] Brand text color matched to logo inner line color (`#c4b5fd`) with subtle white glow effect
- [x] Consistent branding across all entry points (login, setup, sidebar header)

## Known Bugs ⬜
- [x] Indexer test button tests ALL configured indexers instead of only the one clicked
- [x] BitHU indexer search returns no results despite connection test succeeding

---

*Phases are rough groupings — items may shift between phases as development progresses. Each phase will be broken down into smaller tasks when we get there.*
