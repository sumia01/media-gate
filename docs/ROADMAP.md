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

## Phase 1: Core Media Management ⬜
- [ ] TMDB/TVDB integration — search and fetch media metadata
- [ ] Media library data model (movies, TV shows, episodes)
- [ ] Library browsing UI
- [ ] Add/remove media to watchlist

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
- [ ] Media request workflow
- [ ] Request approval / auto-approve rules
- [ ] User management (if multi-user)

## Phase 5: Observability & Polish ⬜
- [ ] Structured log export (file, Loki, etc.)
- [ ] Dashboard / monitoring integration
- [ ] Postgres driver implementation
- [ ] Notification system (TBD)

---

*Phases are rough groupings — items may shift between phases as development progresses. Each phase will be broken down into smaller tasks when we get there.*
