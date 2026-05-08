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
- [x] OpenTelemetry tracing — always-on instrumentation (HTTP, GORM, workers) with noop/real TracerProvider hot-swap
- [x] Runtime-configurable OTel via Settings UI (enable/disable, OTLP endpoint, service name) — changes take effect immediately without restart
- [x] Shared instrumented HTTP client across all integrations (TMDB, TVDB, qBit, OpenSubtitles, Discord, FlareSolverr)
- [x] Structured log export via OpenTelemetry — slog records tee'd to OTLP backend via `otelslog` bridge, configurable minimum log level (debug/info/warn/error), independent of stdout log level
- [x] Log level setting in Settings UI — dropdown in Observability section
- [ ] Dashboard / monitoring integration
- [ ] Postgres driver implementation

## Phase 5.3: Branding ✅
- [x] Custom logo image (`small_logo.png`) replacing text "MG" placeholder on login, setup wizard, and sidebar
- [x] Page title set to "MediaGate"
- [x] Brand text color matched to logo inner line color (`#c4b5fd`) with subtle white glow effect
- [x] Consistent branding across all entry points (login, setup, sidebar header)

## Phase 5.5: Backend directory restructuring ✅
- [x] Move all Go backend files (`cmd/`, `internal/`, `go.mod`, `go.sum`, `.air.toml`, `.env.example`) into `backend/` subdirectory
- [x] Move `frontend/embed.go` to `backend/frontend/embed.go` (must stay within Go module root)
- [x] Keep `api/` at repo root (shared by backend go:generate and frontend openapi-typescript)
- [x] Update Makefile, Dockerfile.build, GitHub Actions, .air.toml, .gitignore for new paths
- [x] Go import paths unchanged (module root = `backend/`, module name unchanged)

## Phase 5.6: Security hardening round 2 ✅
→ See ADR-075

## Phase 5.7: Dead code cleanup ✅
- [x] Remove unused exported methods: `RevokeAllUserTokens`, `BroadcastJSON`, `AddTorrent`/`extractHash`/`btihRegexp`/`postMultipart`, `Caps()`
- [x] Remove unused event constants: `ImportStarted`, `MediaItemSynced`, `MediaItemRemoved`, `MediaItemDeleteReq`, `MediaItemDeletePayload`
- [x] Remove dead struct field: `SearchResult.Description`
- [x] Verified: Cardigann YAML schema fields and template-consumed fields are NOT dead (used at runtime by external definitions)

## Phase 5.8: Code duplication cleanup ✅
→ See ADR-078
- [x] Shared `qbittorrent.Provider` replacing 4 duplicate `getClient()` implementations, with settings invalidation
- [x] Generic `worker.Loop` replacing 3 identical Start/Stop/run patterns in download/importer/monitor
- [x] `indexer.FilterByMediaProfile` replacing 2 duplicate unmarshal+filter callsites
- [x] Indexer definition refresh worker migrated from hand-rolled ticker to `worker.Loop` (now all 6 periodic workers use the same pattern)
- [x] Unified discover handlers (`fetchDiscover` helper + `toDiscoverItem` converter) replacing 3+3 duplicates
- [x] Shared `dateutil.ParseYear` replacing 2 divergent implementations
- [x] Cached TMDB/TVDB clients in matching.Service replacing 6 inline `NewClient` calls
- [x] `applyProfileFields` merging 85%-identical create/update profile functions
- [x] `derefString` helper replacing duplicated optional `*string` deref blocks

## Phase 6.0: Mobile responsive UI ✅
→ See ADR-080
- [x] Sidebar: auto-collapsed on mobile (<768px), overlay with backdrop, hamburger in topbar
- [x] Reduced main content padding on mobile (p-4 vs p-8)
- [x] Library detail view: header wraps to column layout, buttons flex-wrap, path hidden
- [x] Media detail page: stacked hero (poster above info), action bar wraps, Unmatch/Delete hidden, cast/crew/external links/files hidden
- [x] Discover/preview page: same stacked hero treatment, cast/crew/external links hidden
- [x] Torrent result lists: card layout on mobile (title row + size/S/L/indexer/icon-actions row), table preserved on desktop
- [x] Removed freeleech/volume labels (`volumeLabel`) from all torrent result lists (desktop + mobile)

## Phase 6.1: Metadata refresh worker ✅
→ See ADR-081
- [x] `metarefresh.Service` with `worker.Loop` — periodic TMDB/TVDB check for new seasons on monitored series
- [x] `matching.RefreshSeriesMetadata` — compares stored vs external season count, fetches only new seasons (no delete+recreate)
- [x] Skips ended/canceled series, 500ms inter-item rate limiting
- [x] `MetadataRefreshed` SSE event with `MediaItemPayload`
- [x] `KeyWorkerMetadataRefreshInterval` setting (default 6 hours, min 1 hour, configurable in UI)
- [x] Settings UI: metadata refresh interval input in Workers section

## Phase 6.2: Remote download path mapping ✅
→ See ADR-082
- [x] `qbit_save_path` optional setting — override path sent to qBittorrent as `savepath` when NAS mount differs from MediaGate's
- [x] Download service reads `qbit_save_path`; if non-empty uses it for qBit, keeps local `qbit_download_path` on download record for import/sync
- [x] No validation on remote path (it's on the qBit host, not MediaGate's filesystem)
- [x] Frontend: optional text input in Settings view and setup wizard below Download Path folder browser
- [x] Backwards-compatible: empty `qbit_save_path` = same behavior as before

## Phase 6.3: Check Indexers & Add-and-Download from preview ✅
→ See ADR-083
- [x] "Check Indexers" button on media preview page — opens `IndexerSearchModal` in browse-only mode (no `mediaItemId`, Download buttons hidden)
- [x] "Add & Download" button on each torrent result row — inline library picker overlay, two-step flow: add to library + create download, navigates to media detail page
- [x] `IndexerSearchModal` refactored: `mediaItemId` optional, new `source`/`externalId` props, `added` emit, library fetch + picker UI

## Phase 6.4: Library default quality profile ✅
→ See ADR-084
- [x] `LibraryCreate` OpenAPI schema extended with optional `mediaProfileId` field
- [x] `CreateLibrary` and `UpdateLibrary` handlers wire `mediaProfileId` to store model (nullable)
- [x] Library add/edit modal: "Default Quality Profile" dropdown with helper text
- [x] Library list view: profile name badge on library cards
- [x] Library detail page: compact profile select in header action bar, immediate PUT on change
- [x] `AddToLibraryModal`: pre-selects `selectedProfileId` from library's default when library is chosen, user can override

## Phase 7.0: Watched / Seen Tracking ✅
- [x] `WatchedItem` store model — keyed by (UserID, Source, ExternalID) composite unique index, stores title/year/mediaType/posterPath for display
- [x] Store layer — Create, Delete, ListAll, ListByUser, GetBySourceExternal (with optional user filter)
- [x] API endpoints — `POST /watched` (mark as seen), `DELETE /watched/{id}` (unmark), `GET /watched` (list all), `GET /watched/check?source=...&externalId=...` (quick lookup)
- [x] Configurable watched list mode — `watched_list_mode` setting (global/per_user) in Settings API and UI, controls whether watched state is shared or per-user
- [x] "Watched" toggle on media detail page — eye icon button in action bar with watched/unseen state
- [x] "Watched" toggle on media preview page (global search results) — mark without adding to library
- [x] Watched list page (`/watched`) — poster grid with unmark overlay, click navigates to media preview
- [x] Sidebar nav item for Watched page
- [x] "Seen" badge on discover page cards — green tag with eye icon on Recently Added, Trending, Popular Movies/Series
- [x] "Seen" badge on library media grid — green tag next to status pill (available/new/missing/etc.)
- [x] Optional `mediaItemId` on WatchedItem — links to library media item for cached poster resolution
- [x] Watched poster fix — library items use `/api/v1/media/{id}/poster` endpoint, non-library items use TMDB URL
- [x] Versioned schema migration system — replaces ad-hoc `rebuildTablesWithForeignKeys` with ordered `schema_version` migrations in `settings` table
- [x] Graceful RecalcMediaItemStatus — returns nil on `ErrNotFound` instead of failing when media item deleted during match
- [x] Preserved original TMDB poster path in metadata — `savePoster` no longer overwrites `meta.PosterPath` with local filename

## Phase 7.1: Sidebar System Info ✅
- [x] App version — embed build version string at compile time (`-ldflags -X`), expose via `GET /api/v1/health` or dedicated endpoint
- [x] Disk usage API — `GET /api/v1/health` returns total/used/free bytes for the configured `LIBRARY_BASEPATH` mount point
- [x] Sidebar: horizontal divider below user section, version label + disk usage bar/text (e.g. "v1.2.0 · 1.2 TB / 4 TB")
- [x] Collapsed sidebar: show only version number with tooltip showing disk info
- [x] Graceful fallback when disk info unavailable (e.g. permission error, Windows)

## Phase 7.2: Custom Scrollbar Styling ✅
- [x] Themed scrollbar — thin, violet-tinted scrollbar matching dark UI theme via CSS `scrollbar-width`/`scrollbar-color` + WebKit pseudo-elements

## Phase 7.3: Explicit Season Monitoring ✅
- [x] `MonitorNewSeasons` bool on MediaItem — explicit control over auto-monitoring of newly discovered seasons
- [x] Flipped implicit default — no SeasonMonitor row = not monitored (previously meant monitored)
- [x] Migration v3 — backfills explicit SeasonMonitor rows for all monitored series to preserve existing behavior
- [x] Metarefresh auto-creates SeasonMonitor rows for new seasons when MonitorNewSeasons is true
- [x] "Monitor future seasons" toggle in SeasonMonitorModal and AddToLibraryModal
- [x] "New seasons auto-monitored" badge in EpisodeGrid

## Phase 7.4: Episode-level monitoring + UI polish ✅
→ See ADR-089, ADR-090
- [x] `EpisodeMonitor` model — separate table keyed by (MediaItemID, SeasonNumber, EpisodeNumber) surviving episode re-creation on re-match
- [x] Store layer — ListEpisodeMonitorsByMediaItem, UpsertEpisodeMonitor, DeleteEpisodeMonitorsBySeason, DeleteEpisodeMonitorsByMediaItem + migration v4
- [x] Hierarchical resolution — EpisodeMonitor > SeasonMonitor > not monitored (like Sonarr) in AssembleEpisodes and monitor worker auto-grab
- [x] Season cascade — toggling season deletes all episode overrides for that season
- [x] Item disable cleanup — setting monitored=false clears all episode monitors
- [x] API — `PUT /media/{id}/episodes/{seasonNumber}/{episodeNumber}/monitor`, episodeMonitors on MediaItemUpdate and AddMediaRequest, monitored field on Episode schema
- [x] Frontend: per-episode toggle in EpisodeGrid (right-aligned mini toggle pill) and SeasonMonitorModal (with cascade)
- [x] Frontend: "unmonitored" episode status (gray styling for aired+unmonitored episodes)
- [x] Frontend: AddToLibraryModal sends episode monitor overrides (only diffs from season default)
- [x] Renamed "Auto-grab" → "Auto-download" across all UI
- [x] Download settings bar — auto-download, monitor new seasons, quality profile moved to dedicated row between hero and content
- [x] Unified toggle styling — all monitoring controls use consistent toggle pill pattern
- [x] Season header layout — left (chevron+name+count) / right (search icon+toggle) split
- [x] Search icon — replaced "Search" text with magnifying glass in season/episode rows
- [x] Non-flickering refetch — EpisodeGrid only shows loading on initial load, not refetches

## Phase 7.5: YouTube trailer button ✅
→ See ADR-091
- [x] TMDB client — `videos` added to `append_to_response`, `VideoResult`/`VideosResult` types, `Videos` field on `MovieDetails`/`TVDetails`
- [x] `BestTrailerURL` helper — picks best YouTube trailer: official EN > EN > any language, newest first
- [x] `TrailerURL` field on `MediaMetadata` store model (auto-migrated by GORM)
- [x] `trailerUrl` optional field on `MediaMetadata` and `ExternalMediaDetail` OpenAPI schemas
- [x] API layer maps `TrailerURL` in `mediaMetadataToAPI` and `GetExternalMediaDetail`
- [x] Frontend: red-themed "Watch Trailer" card on `MediaDetailView` and `MediaPreviewView` (hidden when no trailer)

## Phase 7.6: Store subpackage split ✅
→ See ADR-093
- [x] Moved 1093-line monolithic `store/sqlite.go` into `store/sqlite/` subpackage
- [x] 16 domain-focused files: `sqlite.go` (struct, constructor, helpers), `migrations.go` (versioned migrations), and one file per entity CRUD
- [x] `store/` retains only the `Store` interface (`store.go`) and models (`models.go`)
- [x] `NewSQLite()` renamed to `sqlite.New()` — only `main.go` import changed
- [x] Clean separation enables future database backends (e.g. `store/postgres/`)

## Phase 7.7: "In library" badge on discover ✅
→ See ADR-094
- [x] `GET /api/v1/media/external-ids` — lightweight endpoint returning `{source, externalId, mediaItemId}` tuples from `MediaMetadata`
- [x] `ListMediaMetadataExternalIDs()` store method selecting only `media_item_id`, `source`, `external_id`
- [x] Frontend `libraryMap` (`Map<string, number>`) built on mount, keyed by `source:externalId` → `mediaItemId`
- [x] Sky-blue "in library" badge with house icon on trending, popular movies, and popular series cards
- [x] Clicking an "in library" discover item navigates to `/media/:id` (library detail) instead of TMDB preview

## Phase 7.8: All Downloads page ✅
→ See ADR-095
- [x] `DownloadsView.vue` — standalone page listing all downloads across all media items
- [x] `mediaItemTitle` optional field added to `Download` OpenAPI schema — populated via LEFT JOIN on `media_items` in `ListDownloads()`
- [x] Sidebar nav item (`Downloads`) in top navigation after Watched
- [x] Status filter dropdown (all/pending/downloading/seeding/completed/failed/etc.)
- [x] Same row structure as per-media `DownloadList.vue`: status/season/indexer badges, title, progress bar, speed, error/retry info
- [x] Media item title shown as clickable link navigating to media detail page
- [x] "Open in library" icon button replaces "Replace" button
- [x] SSE subscription + progress polling for real-time updates
- [x] Retry/delete actions with inline confirmation

## Phase 7.9: Discord webhook notifications ✅
→ See ADR-097
- [x] Discord integration client (`backend/internal/integration/discord/client.go`) — rich embed builder with thumbnail, fields, footer, timestamp
- [x] Notification service (`backend/internal/notification/service.go`) — subscribes to `ImportCompleted` eventbus event, sends Radarr-style rich embed with poster, overview, rating, genres, quality, size, TMDB/IMDb links
- [x] `discord_webhook_url` sensitive setting with URL validation and at-rest encryption
- [x] `POST /settings/test-discord` endpoint for webhook connectivity test
- [x] Settings UI "Notifications" section with Discord card: webhook URL input (show/hide), Test Webhook button, Disconnect button
- [x] Disconnect clears webhook URL from DB (row deletion, not empty string) — no notification sent when URL absent

## Phase 8.0: Discover category pages with infinite scroll ✅
→ See ADR-099
- [x] TMDB client `TrendingAll`, `PopularMovies`, `PopularTV` accept `page` parameter and return `totalPages`
- [x] OpenAPI spec: optional `page` query param + `page`/`totalPages` response fields on trending, popular-movies, popular-series endpoints
- [x] Backend handlers pass `page` through to TMDB client, default to page 1
- [x] Reusable `DiscoverCard.vue` component extracted from HomeView (poster, mediaType/inLibrary/seen badges, rating, title, year)
- [x] `DiscoverCategoryView.vue` — single view for all 3 categories via route prop, `IntersectionObserver`-based infinite scroll with 200px pre-fetch margin
- [x] Routes: `/discover/trending`, `/discover/popular-movies`, `/discover/popular-series`
- [x] "See more →" links on each HomeView discover section header
- [x] Router `afterEach` hook scrolls `<main>` container to top on navigation (inner scroll container, not window)

## Phase 8.1: Subtitle Search & Download (Bazarr replacement) ✅
→ See ADR-100
- [x] OpenSubtitles.com REST API client (`backend/internal/integration/opensubtitles/`) — JWT auth with mutex, login `base_url` capture, 401 retry with fresh request body, file hash computation
- [x] Generic subtitle `Provider` interface (`backend/internal/subtitle/provider.go`) — `Search`, `Download`, `Name` methods for multi-provider extensibility
- [x] OpenSubtitles adapter (`backend/internal/subtitle/opensubtitles_adapter.go`) — wraps API client into generic Provider interface
- [x] Subtitle service (`backend/internal/subtitle/service.go`) — search (multi-provider with rate limiting), download (save to library with release folder placement), list, delete (disk + DB), auto-search on import completion
- [x] Scoring algorithm (`backend/internal/subtitle/score.go`) — hash match +500, release group match +200, language priority, trusted source, download count, HI/foreign penalty
- [x] Release name tokenizer (`backend/internal/subtitle/release.go`) — token overlap matching between subtitle release and download title
- [x] Store model + CRUD (`store/models.go`, `store/sqlite/subtitle.go`) — Subtitle with language, provider, score, HI/foreign flags, source (manual/auto)
- [x] Event bus events — `subtitle.downloaded`, `subtitle.deleted`, `subtitle.auto_search_completed`
- [x] Settings — `opensubtitles_api_key`, `opensubtitles_username`, `opensubtitles_password`, `opensubtitles_rate_limit`, `subtitle_languages`, `subtitle_auto_search`
- [x] OpenAPI spec — `GET /subtitles/search`, `POST /subtitles/download`, `GET /subtitles`, `DELETE /subtitles/{id}`, `POST /settings/test-opensubtitles`
- [x] API handlers (`handlers_subtitle.go`) — thin HTTP adapters calling subtitle service
- [x] `SubtitleSearchModal.vue` — search results sorted by score with language/release/HI/hash/downloads columns, download button per result
- [x] `SubtitleList.vue` — movie subtitle list with SSE-driven refresh, delete button, language/provider/score/HI/auto badges
- [x] EpisodeGrid integration — subtitle search buttons at season and episode level
- [x] MediaDetailView integration — SubtitleList for movies, SubtitleSearchModal overlay, auto-subtitle toggle in download settings bar
- [x] Settings UI — Subtitles section with API key, username, password, test connection, language picker, auto-search toggle, rate limit
- [x] Auto-search on import — `HandleImportCompleted` event handler downloads best subtitle per language after media import
- [x] `TestOpenSubtitles` connection test via settings service

## Phase 8.2: Sample filtering & subtitle improvements ✅
→ See ADR-101
- [x] `fileparse.IsSampleFile` — regex-based sample file/directory detection with word-boundary matching
- [x] Importer sample skip — sample files excluded from import (no hardlink, no MediaFile record)
- [x] Sync sample skip — sample directories and files excluded from library scanning
- [x] Subtitle placement: `findMatchingMediaFile` prefers largest file by size (not first DB match)
- [x] Subtitle placement: `determineSavePath` builds filename from video file basename + language + format (replaces provider filename)
- [x] Subtitle `MediaFileID` association — subtitle DB records linked to matched video file
- [x] Subtitle cleanup on download delete — `CleanupImportedFiles` removes subtitle DB records under release folder + publishes `SubtitleDeleted` SSE events

## Phase 8.3: Settings page tab refactor ✅
- [x] Split monolithic `SettingsView.vue` (1065 lines) into 3 tab components
- [x] Horizontal tab bar (Media DB / Downloads / General) with underline-style active indicator
- [x] `SettingsMediaDb.vue` — TMDB, TVDB integrations + Metadata (primary source, rate limits)
- [x] `SettingsDownloads.vue` — qBittorrent, OpenSubtitles, FlareSolverr
- [x] `SettingsGeneral.vue` — Watched list mode, Discord notifications, Monitor (season pack pref), Workers
- [x] State management stays centralized in parent `SettingsView.vue` — tab components are pure template via props/emits
- [x] Save button persists across tabs, saves all dirty fields from any tab

## Phase 8.4: Self-update ✅
→ See ADR-102
- [x] `updater.Service` — GitHub Releases API client with periodic check via `worker.Loop` (configurable interval, default 6h)
- [x] In-process binary replacement — download asset → temp file → backup `.bak` → `os.Rename` → `syscall.Exec`
- [x] Linux-only gating — disabled on non-Linux, dev builds, or missing GitHub credentials
- [x] SSE notification — `app.update_available` event pushed to all connected frontends
- [x] API endpoints — `GET /update/status`, `POST /update/check`, `POST /update/apply`
- [x] TopBar update indicator — green badge with dropdown showing version + "Go to Settings" navigation (panel teleported to body for z-index correctness)
- [x] Settings General "Updates" section — current version, check now, apply update, check interval setting
- [x] Frontend restart detection — polls `/api/v1/health` after apply, reloads when server returns
- [x] Config — `MEDIAGATE_GITHUB_TOKEN` / `MEDIAGATE_GITHUB_REPO` via systemd EnvironmentFile
- [x] Deploy script — github.conf writes both `GH_*` and `MEDIAGATE_*` prefixed vars, systemd EnvironmentFile added

## Phase 8.5: Database export ✅
→ See ADR-107
- [x] `GET /api/v1/settings/database/export` manual handler — serves SQLite file as attachment with date-stamped filename
- [x] `dbPath` field on Handlers struct, passed from `cfg.DB.Path` at startup
- [x] Frontend: "Database" section in Settings General tab with "Download .db" button using `authFetch`

## Phase 8.6: Plex Library Refresh ✅
→ See ADR-108
- [x] Plex HTTP client (`backend/internal/integration/plex/client.go`) — XML section listing, section refresh trigger, `X-Plex-Token` auth
- [x] Lazy-cached Plex provider (`backend/internal/integration/plex/provider.go`) — settings-invalidated singleton (same pattern as `qbittorrent.Provider`)
- [x] Auto-matcher (`backend/internal/integration/plex/matcher.go`) — scores library↔section by type+basename+fullpath, `FindSection` picks best match
- [x] `plex_url` (plain) + `plex_token` (sensitive) settings keys with `TestPlex()` connection test
- [x] Plex refresh service (`backend/internal/plexrefresh/service.go`) — subscribes to `ImportCompleted` event, resolves matching Plex section, triggers refresh with 3x exponential backoff retry
- [x] OpenAPI spec: `POST /settings/test-plex`, `GET /plex/sections`, `GET/PUT /plex/mappings`, `POST /plex/refresh/{sectionId}`
- [x] Backend handlers (`handlers_plex.go`) — test connection, list sections, CRUD mappings, manual refresh
- [x] Settings stored as `plex:mapping:{libraryID}` → `{plexSectionID}` (no new DB model, consistent with `indexer:*` pattern)
- [x] Frontend: Libraries page split into tabs (Libraries / Media Server)
- [x] `LibrariesMediaServer.vue` — connection settings, test, auto-matched library↔section dropdowns, manual refresh per library
- [ ] Jellyfin/Emby support (architecture ready — generic "Media Server" tab, provider pattern)

## Phase 8.7: Background Workers Panel ✅
→ See ADR-109
- [x] `worker.Loop` status tracking — `lastRunAt`, `nextRunAt`, `running` fields (mutex-protected), `Status()` method, `RunNow()` channel trigger, `OnStateChange` callback
- [x] `worker.Registry` — named worker registration, `All()` for status listing, `RunByName()` for manual trigger, `MakePublisher()` for eventbus bridge
- [x] SSE events — `worker.started` / `worker.finished` with `WorkerPayload{Name, LastRunAt, NextRunAt}` via eventbus
- [x] OpenAPI spec — `GET /workers` (list status), `POST /workers/{name}/run` (manual trigger), `Worker` schema
- [x] API handlers (`handlers_workers.go`) — `ListWorkers` + `RunWorker` (thin adapters over registry)
- [x] 4 workers registered: monitor, metadata-refresh, indexer-def-refresh, update-check
- [x] Frontend `useWorkers.ts` composable — fetches worker list, subscribes to SSE worker events, auto-refetches on state change
- [x] TopBar Workers panel — cycle-arrows icon + "Workers" label, table dropdown with last/next run times, running dot with glow animation, "Run Now" / "Running…" button states
- [x] Panel teleported to `<body>` for z-index correctness, `w-[32rem]` width to prevent button wrapping

## Phase 8.8: Smart Profile Matching ✅
→ See ADR-112
- [x] Language parser (`fileparse/language.go`) — tokenizes release titles, recognizes ~50 language aliases with normalization
- [x] `LanguageMode` field on `MediaProfile` model — "and" (all required) or "or" (any match, order = priority)
- [x] `MatchesLanguages()`, `LanguageScore()`, `PriorityScore()` functions in `fileparse/match.go`
- [x] `FilterByProfile` extended with language filtering; `RankResults()` for multi-dimensional ranking (resolution > language > source)
- [x] `MatchesMediaProfile()` single-result checker with global exclude tags support
- [x] OpenAPI: `languageMode` on MediaProfile, `profileMatch` on TorrentResult, `profileId` query param on search endpoint
- [x] Backend handler: `SearchIndexers` annotates results with `profileMatch` using server-side logic
- [x] Frontend: removed duplicated matching logic from `utils/torrent.ts` — reads `profileMatch` from API response
- [x] Frontend: AND/OR toggle UI on profiles page, priority numbers on resolution/source/language buttons
- [x] DB migration v6: backfills `language_mode='or'` on existing profiles

## Phase 8.9: Icon system (Lucide) ✅
→ See ADR-114
- [x] Replaced all inline SVGs, Unicode characters, and HTML entities with Lucide Vue components (`lucide-vue-next`)
- [x] Consistent monochrome icons that inherit parent `color` — gray inactive, violet active
- [x] Consistent sizing via Tailwind classes (`w-3 h-3`, `w-4 h-4`, `w-5 h-5`)
- [x] Dynamic library-type icons in sidebar (Clapperboard for movies, Tv for series)
- [x] ~20+ frontend files converted to use Lucide components

## Known Bugs ⬜
- [x] Indexer test button tests ALL configured indexers instead of only the one clicked
- [x] BitHU indexer search returns no results despite connection test succeeding
- [x] Cardigann `urlencode` filter missing — text field filters (e.g. API key encoding in download URLs) silently skipped
- [x] Cardigann `search.headers` not applied to download requests — indexers using header-based auth (e.g. Milkie `x-milkie-auth`) fail with 401 on torrent fetch
- [x] Cardigann text field filters not applied after template rendering — filters on `.Text` fields with `.Result` references were never executed
- [x] Monitor `buildDownloadMap` misclassifies single-episode downloads (missing `episode_id`) as season packs — blocks entire season from auto-download
- [x] Cardigann text field rendering order non-deterministic — Go map iteration causes inter-field `.Result` references (e.g. Milkie `_apikey` → `download`) to resolve as raw template literals intermittently

---

*Phases are rough groupings — items may shift between phases as development progresses. Each phase will be broken down into smaller tasks when we get there.*
