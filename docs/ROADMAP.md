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
- [x] Entity model redesign — split logical media (MediaItem) from physical files (MediaFile), add QualityProfile and SeasonMonitor models, quality profile CRUD API
- [ ] Cast & crew display on media detail page

## Phase 2: Indexer Integration (Prowlarr replacement) ⬜
- [ ] Indexer configuration and management
- [ ] Search across configured indexers
- [ ] Result ranking and filtering

## Phase 3: Download Management (Sonarr/Radarr replacement) ⬜
- [ ] qBittorrent API integration
- [ ] Download queue management
- [ ] Auto-download based on watchlist
- [ ] Download status monitoring

## Phase 4: Request System (Overseerr replacement) ⬜
- [x] Requested media items (source: request, status: requested) — foundation
- [x] Quality profiles model + CRUD API (data model ready, frontend deferred)
- [x] MediaFile model for multi-copy/multi-quality file tracking
- [x] SeasonMonitor model for per-season monitoring
- [ ] Quality profile UI (list/create/edit)
- [ ] Request details page (quality profile, monitoring settings)
- [ ] Multi-copy handling (same media in different qualities)
- [ ] Series episode tracking (which seasons/episodes are present/missing)
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
