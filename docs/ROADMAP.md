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
- [x] Update media item endpoint (`PATCH /media/{id}`) — assign quality profile, toggle monitor new seasons
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

## Phase 3: Download Management (Sonarr/Radarr replacement) ⬜
- [x] Download model + CRUD API (persistent Download records, POST/GET/PUT /downloads)
- [x] IndexerSearchModal — search indexers from media detail page with indexer dropdown
- [x] Search buttons at item, season, and episode level (EpisodeGrid)
- [x] Episode download status display (computed from Download records, shown in EpisodeGrid)
- [x] Auto-refresh episode list when closing search modal after adding downloads
- [x] qBittorrent API integration (client adapter, settings UI, connection test)
- [x] Download path setting (FolderBrowser selection, mutual exclusion with library paths)
- [x] Download queue management (server worker: send pending → qBit, poll status, seeding rules)
- [x] Download status monitoring (poll qBittorrent, update Download records)
- [ ] Auto-download based on watchlist

## Phase 4: Request System (Overseerr replacement) ⬜
- [x] Requested media items (source: request, status: requested) — foundation
- [x] Quality profiles model + CRUD API (data model ready, frontend deferred)
- [x] Quality profile assignment on media detail page (dropdown + PATCH endpoint)
- [x] Monitor new seasons toggle on media detail page (series only)
- [x] MediaFile model for multi-copy/multi-quality file tracking
- [x] SeasonMonitor model for per-season monitoring
- [ ] Quality profile UI (list/create/edit)
- [ ] Multi-copy handling (same media in different qualities)
- [x] Series episode tracking (which seasons/episodes are present/missing)
- [ ] Season bundles vs standalone episodes vs complete series downloads
- [ ] Request approval / auto-approve rules
- [ ] User management (if multi-user)

## Phase 5: Observability & Polish ⬜
- [ ] Structured log export (file, Loki, etc.)
- [ ] Dashboard / monitoring integration
- [ ] Postgres driver implementation
- [ ] Notification system (TBD)

---

*Phases are rough groupings — items may shift between phases as development progresses. Each phase will be broken down into smaller tasks when we get there.*
