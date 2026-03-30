# Media Gate — Decision Log

Architecture Decision Records: documenting key choices and their reasoning.

---

## ADR-001: Monorepo with single binary deployment
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need a simple deployment model for a homelab environment.

**Decision**: Frontend (Vue) and backend (Go) live in one repo. The built Vue SPA is embedded into the Go binary via `//go:embed`. One binary to deploy.

**Rationale**: Minimal operational overhead. No need for separate web server, no CORS headaches, simple upgrades (replace one file).

---

## ADR-002: OpenAPI as single source of truth for API contract
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need to keep the Go backend API and Vue frontend client in sync.

**Decision**: Write OpenAPI specs in `api/`, generate Go server code with oapi-codegen (strict server mode), and generate TypeScript client for the frontend.

**Rationale**: Eliminates contract drift between frontend and backend. Strict server mode enforces type-safe handler signatures in Go.

---

## ADR-003: GORM with Store interface pattern
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Want to support both SQLite (simple/dev) and Postgres (production) without coupling business logic to a specific database.

**Decision**: Define a Go `Store` interface for data access. Implement it using GORM, which supports both SQLite and Postgres drivers. The active driver is selected via configuration.

**Rationale**: GORM abstracts SQL dialect differences. The Store interface keeps business logic testable and database-agnostic.

---

## ADR-004: Vue 3 + TypeScript for frontend
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need a modern SPA framework. User has deep React experience but wants to explore Vue.

**Decision**: Vue 3 with TypeScript and Composition API.

**Rationale**: Learning opportunity. TypeScript ensures type safety and pairs well with generated API client.

---

## ADR-005: slog with pluggable handlers for logging
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need structured logging that can eventually feed into an observability stack (dashboards, alerting).

**Decision**: Use Go's `log/slog` with swappable handlers. Start with text/JSON handler to stdout/file, plan for future integration (Loki, etc.).

**Rationale**: slog is stdlib, zero dependency. Handler interface makes it easy to add new log targets without changing call sites.

---

## ADR-007: koanf for configuration management
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need a way to load application configuration. Primary source is a `.env` file in development; in deployed environments, configuration comes from environment variables.

**Decision**: Use [koanf](https://github.com/knadh/koanf) for configuration loading. Load from `.env` file (dev) and environment variables (production).

**Rationale**: koanf is lightweight, composable, and handles multiple config sources cleanly — unlike viper, which is overcomplicated for this use case. koanf's provider model makes it trivial to layer `.env` files with env vars.

---

## ADR-006: qBittorrent as download client
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need a torrent client for downloading media.

**Decision**: Integrate with qBittorrent via its Web API.

**Rationale**: Already running in the homelab, well-known API, stable.

---

## ADR-008: openapi-typescript + openapi-fetch for frontend API client
**Date**: 2026-03-27
**Status**: Accepted

**Context**: Need a type-safe way to call the backend API from the Vue frontend, generated from the same OpenAPI spec used for Go code generation.

**Decision**: Use [openapi-typescript](https://github.com/openapi-ts/openapi-typescript) to generate TypeScript types from the spec, and [openapi-fetch](https://github.com/openapi-ts/openapi-typescript/tree/main/packages/openapi-fetch) as the runtime HTTP client.

**Rationale**: Lightweight, type-safe at compile time (no runtime bloat), actively maintained, and pairs naturally with the OpenAPI-first workflow already established with oapi-codegen on the backend.

---

## ADR-009: Versioned API routes and packages
**Date**: 2026-03-27
**Status**: Accepted

**Context**: As the API evolves, breaking changes may be needed. Need a strategy that allows introducing new API versions without disrupting existing clients.

**Decision**: API routes are versioned under `/api/v1`. On the Go side, generated code and handlers live in `internal/api/v1/` (package `apiv1`). Future versions get their own package (`internal/api/v2/`, package `apiv2`) and are mounted side-by-side on the mux.

**Rationale**: Clean separation at both the URL and code level. Multiple versions can coexist during migration periods.

---

## ADR-010: Modular frontend component architecture
**Date**: 2026-03-27
**Status**: Accepted

**Context**: The frontend will grow significantly as features are added. Early decisions about component organization pay off later.

**Decision**: Frontend components are organized by concern:
- `components/layout/` — app shell pieces (sidebar, topbar, page layout)
- `components/media/` — domain-specific components (media cards, shared types/data)
- `views/` — route-level page components that compose layout + domain components

**Rationale**: Keeps components small and focused. Layout components are reusable across pages. Domain components can be composed freely without coupling to a specific layout.

---

## ADR-011: In-memory job queue for background tasks
**Date**: 2026-03-28
**Status**: Amended by ADR-017

**Context**: Library sync can take time (reading disk, diffing DB). It shouldn't block the API request. Need a way to enqueue background work and track progress.

**Decision**: In-memory job queue (`internal/jobqueue/`) with a single worker goroutine, buffered channel, and mutex-guarded state. The frontend polls `GET /jobs` to track status.

**Rationale**: Simple and sufficient for a single-instance homelab app. No need for Redis or a message broker. Active/pending jobs live in memory; completed job history is persisted to SQLite (see ADR-017).

---

## ADR-012: Folder-name parsing for media item title and year
**Date**: 2026-03-28
**Status**: Accepted

**Context**: Library directories follow common naming conventions like `Movie Title (2024)` or `Movie.Title.2024`. Need to extract a clean title and optional year from folder names.

**Decision**: The sync service (`internal/sync/`) uses a regex-based `parseFolderName()` helper that extracts a trailing 4-digit year (with optional parens/brackets) and replaces dots with spaces.

**Rationale**: Covers the two most common naming conventions in media libraries. Intentionally kept simple — TMDB matching (Phase 1) will handle fuzzy/ambiguous cases. Avoids pulling in a full media filename parser library for what is a best-effort extraction.

---

## ADR-013: Composable-based job polling with per-library callbacks
**Date**: 2026-03-28
**Status**: Accepted

**Context**: The frontend needs to know when a library sync finishes so it can reload the media list. Multiple components (topbar, library detail view) need access to job state.

**Decision**: A shared `useJobQueue` composable manages global job state with adaptive polling (2s when active, 30s idle). It tracks which library IDs have active jobs and fires `onSyncDone(libraryId)` callbacks when a library's job transitions from active to completed/failed.

**Rationale**: Avoids WebSocket complexity for a single-user app. The per-library callback pattern is more precise than watching a global `hasActiveJob` flag — it correctly handles fast syncs (job completes between polls) and multiple libraries syncing concurrently.

---

## ADR-014: DB-backed settings for API keys
**Date**: 2026-03-28
**Status**: Accepted

**Context**: TMDB and TVDB integrations require API keys. These need to be configurable at runtime from the UI, not baked into `.env` or environment variables.

**Decision**: Store settings as key-value pairs in a `settings` table (GORM model with `Key` as primary key). Sensitive values (API keys) are masked in API responses (`****` + last 4 chars). The settings service auto-detects which keys are sensitive based on a hardcoded set.

**Rationale**: DB storage allows runtime changes from the UI without server restart. Masking prevents API key leakage while still showing enough of the value for the user to identify which key is stored. The `sensitive` flag is server-determined (not user-controlled) to prevent accidental exposure.

---

## ADR-015: Connection test accepts optional API key in request body
**Date**: 2026-03-28
**Status**: Accepted

**Context**: Users need to test TMDB/TVDB API keys before saving them. Initially, the test endpoint read the key from DB, which meant users had to save first — unintuitive UX. But after saving, the stored key (returned masked) also needs to be testable without re-entering it.

**Decision**: The `POST /settings/test-tmdb` and `POST /settings/test-tvdb` endpoints accept an optional `apiKey` in the request body. If provided, the supplied key is tested directly. If omitted, the backend falls back to the saved key from the database.

**Rationale**: Covers both use cases with a single endpoint: testing a freshly pasted key (before save) and re-testing an already saved key (without the frontend sending the masked value).

---

## ADR-016: TMDB/TVDB auto-match with manual override
**Date**: 2026-03-28
**Status**: Accepted

**Context**: MediaItems created by sync have a parsed title and year but no metadata. Need to link them to TMDB/TVDB entries for posters, descriptions, and ratings.

**Decision**: A matching service (`internal/matching/`) auto-matches MediaItems during sync by searching TMDB (movies) or TVDB (series) using the parsed folder name. The best result is stored as `MediaMetadata` linked to the MediaItem. Users can also manually search and pick a match from the UI if auto-match is wrong or missing.

**Rationale**: Auto-match handles the common case (well-named folders). Manual match covers edge cases (ambiguous names, wrong matches). The two-step approach keeps the sync fast while giving users full control.

---

## ADR-017: Persist completed job history to SQLite
**Date**: 2026-03-28
**Status**: Accepted

**Context**: Previously, completed/failed jobs were kept in an in-memory slice (max 20). Job history was lost on server restart, making it impossible to see past sync results.

**Decision**: Completed and failed jobs are now persisted to a `job_records` table in SQLite. The queue reads the max existing ID on startup to continue the sequence. `ListJobs` returns active in-memory jobs plus recent records from DB. Old records are trimmed to keep the last 200.

**Rationale**: Minimal complexity increase for significant UX improvement. Users can see job history across restarts. The 200-record cap prevents unbounded growth.

---

## ADR-018: Dedicated media detail page instead of side panel
**Date**: 2026-03-28
**Status**: Accepted

**Context**: Clicking a media card in the library grid opened a `MatchPanel` side panel — useful for matching, but no room for rich metadata display (poster, overview, genres, ratings, runtime/seasons).

**Decision**: Added a `GET /media/{id}` API endpoint and a dedicated `/media/:id` route (`MediaDetailView.vue`). Library grid cards now navigate to this page. The MatchPanel is rendered on the detail page (triggered by a "Re-match" button) instead of on the library grid.

**Rationale**: A full page provides space for a hero poster, metadata stats grid, genre pills, match source info, and action buttons. The side panel was too constrained for a rich detail view. Moving match/unmatch actions to the detail page keeps the library grid simple (browse-only).

---

## ADR-019: Requested media via MediaItem extension (not separate entity)
**Date**: 2026-03-28
**Status**: Accepted

**Context**: Users want to add media to a library before it physically exists on disk (e.g., movies they want to download later). This is the foundation for the future request/download workflow.

**Decision**: Extend the existing `MediaItem` model with a `Source` field (`disk` | `request`) and a `requested` status, rather than creating a separate `Request` entity. Requested items are invisible to the sync service. They get full TMDB/TVDB metadata and posters on creation.

**Rationale**:
- Reuses existing matching, metadata, and poster infrastructure without duplication
- Library views show both existing and requested media in a unified list
- The sync service naturally ignores requested items (filters by `source = "disk"`)
- A future import process (torrent integration) will transition the status — the sync service doesn't need to handle this
- If request-specific features are needed later (priority, approval), they can be added as fields rather than requiring a new entity and migration

---

## ADR-020: Library-scoped media search and requests
**Date**: 2026-03-28
**Status**: Accepted

**Context**: When adding requested media, the system needs to know which library the media belongs to. Multiple libraries of the same type may exist (e.g., "Kids Movies", "Adult Movies").

**Decision**: Media search (`GET /libraries/{id}/search`) and add (`POST /libraries/{id}/media`) are scoped to a specific library. The search automatically uses the library's `mediaType` (movie/series) to filter results. The topbar global search bar triggers the same add-media panel when on a library detail page.

**Rationale**: Library-scoping ensures requested items are tied to the correct library path for future download/import. The search endpoint also annotates results with `existingMediaId` if the candidate already exists in that library, preventing duplicates at the UI level.

---

## ADR-022: PATCH endpoint for partial media item updates
**Date**: 2026-03-28
**Status**: Accepted

**Context**: After adding requested media or syncing from disk, users need to assign quality profiles and configure monitoring preferences per media item. The existing `GET /media/{id}` and `DELETE /media/{id}` endpoints don't support updates.

**Decision**: Add `PATCH /media/{id}` with a `MediaItemUpdate` body containing optional fields (`mediaProfileId`, `monitorNewSeasons`). Only provided fields are updated — omitted fields are left unchanged. The handler validates that the referenced profile exists before assignment.

**Rationale**: PATCH (not PUT) because updates are partial — users typically change one setting at a time from the UI. This avoids requiring the client to send the entire MediaItem back. The frontend wires this to a dropdown and toggle on the media detail page for immediate, per-field saves.

---

## ADR-021: Entity model redesign — MediaFile, QualityProfile, SeasonMonitor
**Date**: 2026-03-28
**Status**: Accepted

**Context**: The original MediaItem model conflated logical media (metadata, requests) with physical files (path, folder name). This prevented supporting multiple file copies per item (different qualities), per-season monitoring for series, and quality profiles for download preferences.

**Decision**: Split the model into separate concerns:
- **MediaItem** — logical media entry (title, year, status, source). No longer holds path/folder info. Gains `QualityProfileID` and `MonitorNewSeasons` fields.
- **MediaFile** — physical file on disk, linked to a MediaItem. Holds path, fileName, size, resolution, sourceType, and optional season/episode numbers. One MediaItem can have many MediaFiles.
- **QualityProfile** — defines download quality preferences (resolutions, sources, exclude tags). Can be assigned to a Library (default) or individual MediaItem (override).
- **SeasonMonitor** — per-season monitoring toggle for series, unique per (MediaItemID, SeasonNumber).

The sync service now creates a MediaFile alongside each MediaItem when scanning disk directories. Removal detection compares MediaFile paths instead of MediaItem paths. Orphaned MediaItems (zero remaining files) are cleaned up.

**Rationale**: Separating logical media from physical files enables: multi-copy support (same movie in 1080p and 4K), episode-level tracking for series, quality-based download decisions, and a clean path for the future download pipeline. The QualityProfile model is CRUD-ready via API; frontend UI is deferred to a later step.

---

## ADR-023: Cast & crew as JSON field on MediaMetadata
**Date**: 2026-03-28
**Status**: Accepted

**Context**: Media detail pages need to show cast and crew. Both TMDB and TVDB provide credits data but in different structures (TMDB: `append_to_response=credits`, TVDB: extended endpoint with `characters` array).

**Decision**: Store credits as a JSON string field (`Credits`) on `MediaMetadata`, same pattern as `Genres`. A unified `CreditPerson` struct (name, role, type, image, order) normalizes both TMDB and TVDB data. TMDB credits are fetched via `append_to_response` (no extra API call). TVDB credits come from the `/series/{id}/extended` endpoint. Credits are capped at 10 cast + 5 key crew per item.

**Rationale**: JSON-in-column avoids a separate credits table and join queries for what is display-only data. The unified struct hides source differences from the API and frontend. The cap keeps storage and UI reasonable — full filmographies aren't useful for a media manager. Using `append_to_response` (TMDB) and switching to the extended endpoint (TVDB) avoids additional API calls per item.

---

## ADR-024: Per-file scanning with filename parsing
**Date**: 2026-03-29
**Status**: Accepted

**Context**: The sync service previously created one MediaFile per top-level folder with no quality/resolution/source parsing and no season/episode extraction. Users need to see individual video files with their quality metadata, and series need episode-level tracking.

**Decision**: A standalone `fileparse` package extracts resolution (2160p/1080p/720p/480p), source type (bluray/webdl/webrip/hdtv/dvdrip), and season/episode numbers from filenames using regex. The sync service now walks media folders for actual video files (`.mkv`, `.mp4`, `.avi`, `.ts`, `.wmv`, `.flv`), creating one MediaFile per video file instead of per folder.

**Rationale**: Regex-based parsing is fast, dependency-free, and covers the most common naming conventions in media libraries (`S01E03`, `1x03`, `1080p`, `BluRay`, etc.). The `fileparse` package has no internal dependencies, making it easy to test and reuse.

---

## ADR-025: Three series folder layouts with split-season grouping
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Series libraries can have three different folder structures: (1) show folder with season subfolders, (2) show folder with flat mixed episodes from multiple seasons, and (3) multiple top-level folders per season (e.g., "Breaking Bad Season 1", "Breaking Bad Season 2"). All three need to produce correct MediaItems with properly tagged MediaFiles.

**Decision**: The sync service detects and handles all three layouts:
- **Standard**: Season subfolders (`Season 01/`, `S1/`) are detected via regex; files inside inherit the subfolder's season number as fallback if the filename lacks S##E## patterns.
- **Flat**: Video files directly in the show folder; season/episode extracted purely from filenames.
- **Split-season**: Top-level folders with season suffixes (e.g., "ShowName Season 2") are grouped by stripping the suffix to produce a canonical name. All folders in a group share one MediaItem.

The grouping logic (`groupFolders`) only activates for series libraries. Movie libraries treat each folder independently.

**Rationale**: All three layouts are common in real media libraries. Grouping split-season folders avoids duplicate MediaItems for the same series, which would break matching, episode tracking, and the user experience.

---

## ADR-026: Episode model as proper table (not JSON)
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Series need episode-level tracking to show which episodes are present on disk and which are missing. Episodes come from TMDB/TVDB and are cross-referenced against MediaFiles.

**Decision**: Episodes are stored in a dedicated `episodes` table with a composite unique index on `(media_item_id, season_number, episode_number)`. The matching service fetches episode lists from TMDB (`GET /tv/{id}/season/{n}`) or TVDB (`GET /series/{id}/episodes/default?season={n}`) and creates Episode records. The `GET /media/{id}/episodes` endpoint groups episodes by season and annotates each with a `hasFile` flag computed by cross-referencing against MediaFiles.

**Rationale**: A proper table (vs JSON-in-column like credits/genres) is necessary because episodes are cross-referenced against MediaFiles, queried per-season, potentially hundreds per series, and updated independently. The `hasFile` flag is computed at query time rather than stored, keeping it always in sync with the actual files on disk.

---

## ADR-027: IMDb ID from existing TMDB/TVDB integrations
**Date**: 2026-03-29
**Status**: Accepted

**Context**: The target tracker for downloads searches most effectively by IMDb ID. IMDb has no free API, but TMDB and TVDB — already integrated — both return IMDb IDs in their responses.

**Decision**: Extract IMDb IDs from existing integrations rather than adding a new service:
- **TMDB movies**: `imdb_id` field already present in `GET /movie/{id}` response — just added to the `MovieDetails` struct.
- **TMDB TV**: `append_to_response=external_ids` added to the existing `GetTV()` call — returns `imdb_id` with zero extra API calls.
- **TVDB series**: `remoteIds` array already present in the `/series/{id}/extended` response — added `RemoteID` struct and helper method to filter by `sourceName == "IMDB"`.

The IMDb ID is stored as a string field on `MediaMetadata`, exposed in the API, and displayed on the media detail page alongside a "View on IMDb" link.

**Rationale**: No new integration, no new API key, no rate limit impact. All three sources were already returning this data — it was being silently discarded during JSON unmarshaling. Adding struct fields and one `append_to_response` parameter was sufficient.

---

## ADR-028: Match mode selection — unmatched only vs full re-match
**Date**: 2026-03-29
**Status**: Accepted

**Context**: The library-level "Match" button only matched items with `status = "new"` (no metadata). Users needed a way to re-match all items in a library — e.g., after switching metadata source, or to pick up IMDb IDs for items matched before that feature existed.

**Decision**: The Match button now opens a modal with two options: "Unmatched only" (default behavior) and "Full re-match" (re-matches all items, replacing existing metadata and episodes). Implemented as a `fullRematch` query parameter on `POST /libraries/{id}/match`, propagated through the job queue to `matching.MatchLibrary()`. Full rematch uses `ListMediaItemsByLibrary` (all items) instead of `ListNewMediaItemsByLibrary` (unmatched only), and clears existing metadata/episodes before re-matching each item.

**Rationale**: A modal is less error-prone than two separate buttons — the destructive option (replacing all metadata) requires an explicit choice. The query parameter approach avoids a new endpoint while keeping the default behavior unchanged for auto-match after sync.

---

## ADR-029: TVDB search type filtering and string ID handling
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Two bugs in the TVDB integration: (1) The TVDB v4 `/search` endpoint returns all entity types (series, movies, people) by default. Selecting a result that was actually a movie and calling `/series/{id}/extended` returned 404. (2) The search response returns `tvdb_id` as a string, but the Go struct had it typed as `int`, causing JSON unmarshal errors.

**Decision**:
- Add `type=series` parameter to `SearchSeries()` so only series results are returned.
- Change `SeriesResult.ID` from `int` field to a `TVDBID string` field with an `ID()` method that converts via `strconv.Atoi`.

**Rationale**: The type filter prevents cross-type ID collisions (a movie and a series can share the same numeric ID). Parsing the string ID with a method keeps the rest of the codebase working with `int` IDs while correctly handling the API's string response.

---

## ADR-030: Poster cache-busting via updatedAt timestamp
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Poster files are stored as `{mediaItemId}.jpg`. After re-matching, the file changes on disk but the browser serves the old image from cache because the URL hasn't changed.

**Decision**: Append `?t={updatedAt timestamp}` to poster URLs on the frontend. When a re-match updates the MediaItem's `updatedAt`, the URL changes and the browser fetches the new image. The backend poster handler ignores the query parameter — `http.ServeContent` uses the file's `ModTime` for ETag/caching.

**Rationale**: Zero backend changes needed. The `updatedAt` field is already present on every MediaItem and naturally changes on re-match. No need for random strings or version counters.

---

## ADR-031: Global search with preview page and Add to Library modal
**Date**: 2026-03-29
**Status**: Accepted (supersedes ADR-020 for search UX)

**Context**: The original search-and-add flow (ADR-020) was tightly coupled to a library: the user could only search from a library detail page, clicking a result immediately added it, and there was no way to preview metadata before committing. Users wanted to: (1) search from any page, (2) toggle between movie/series, (3) preview full metadata before adding, (4) choose which library to add to.

**Decision**: Reworked the search flow into three stages:
- **Global search overlay** (`GlobalSearchOverlay.vue`) — mounted in the app layout, accessible from any page via topbar. Includes a movie/series toggle and calls a new `GET /search` endpoint (not library-scoped). Clicking a result navigates to a preview page instead of immediately adding.
- **Media preview page** (`MediaPreviewView.vue`) — new route `/search/:source/:externalId` that calls `GET /search/{source}/{externalId}` to fetch full external metadata (poster, overview, genres, cast, crew, IMDb ID) without creating any DB records. Mirrors the existing `MediaDetailView` layout.
- **Add to Library modal** (`AddToLibraryModal.vue`) — lists compatible libraries (filtered by mediaType), pre-selects the library if search was initiated from a library page. Calls the existing `POST /libraries/{id}/media` endpoint.

Backend additions:
- `GET /search` — global search endpoint with `query` + `mediaType` params, delegates to existing `matching.SearchCandidates()`
- `GET /search/{source}/{externalId}` — preview endpoint, new `matching.GetExternalDetail()` method reuses existing `fetchTMDBDetails`/`fetchTVDBDetails` without persisting anything
- `ExternalMediaDetail` schema in OpenAPI spec

The `useGlobalSearch` composable was expanded with `activeLibraryId` and `searchMediaType` state. When search is opened from a library page, the library is pre-selected in the Add modal and the media type toggle is pre-set. The old `AddMediaSearch.vue` component was removed.

**Rationale**: Decoupling search from a specific library enables browsing/discovery. The preview page gives users confidence in what they're adding (especially useful when multiple results share similar names). The three-stage flow (search → preview → add) matches the mental model of Sonarr/Radarr. Reusing existing backend methods (`SearchCandidates`, `fetchTMDBDetails`, `fetchTVDBDetails`, `posterURL`) meant the backend changes were minimal — two new thin endpoints and one new struct.

---

## ADR-032: Frontend shared utilities, types, and base components
**Date**: 2026-03-29
**Status**: Accepted

**Context**: The frontend had accumulated duplicated code across views and components: `parseGenres()` and `profileImageUrl()` were copy-pasted between `MediaDetailView` and `MediaPreviewView`, `posterUrl()` between `MediaDetailView` and `LibraryDetailView`, error banners (identical Tailwind markup) in 6 files, modal Teleport+backdrop structures in 4 files, and every component repeated `type X = components['schemas']['X']` aliases.

**Decision**: Introduced four new shared modules:
- **`src/types/api.ts`** — centralized re-exports of all commonly used OpenAPI schema types. Components import from `@/types/api` instead of aliasing `components['schemas']` inline.
- **`src/utils/media.ts`** — pure utility functions (`parseGenres`, `profileImageUrl`, `posterUrl`) extracted from views.
- **`src/components/BaseModal.vue`** — reusable modal wrapper (Teleport to body, backdrop click-to-close, configurable max-width via prop). Replaces inline Teleport+backdrop patterns.
- **`src/components/ErrorBanner.vue`** — takes a `message` prop, renders the standard error banner when non-empty. Replaces 6 identical `v-if="error"` div blocks.

Also removed dead code: the unused `NavItem` interface and `navItems` constant from `dummyData.ts`.

**Rationale**: Reduces duplication, makes the codebase easier to read and maintain. Changes to error styling or modal behavior now happen in one place. The type re-export file eliminates the most common boilerplate line in every component.

---

## ADR-033: Cardigann-compatible indexer engine
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Phase 2 of the roadmap is indexer integration (Prowlarr replacement). The choice is between integrating with Prowlarr as an external dependency or building a native indexer engine. The primary target tracker is ncore (Hungarian private tracker).

**Decision**: Build a native Cardigann engine that can parse and execute Prowlarr's YAML indexer definitions directly. The engine lives in `internal/indexer/cardigann/` and supports:
- YAML definition parsing (same format as Prowlarr's `definitions/v11/*.yml`)
- POST-based login with cookie session management
- HTML scraping with CSS selectors (via `goquery`)
- Go template rendering for dynamic inputs/queries
- Filter pipeline (querystring, replace, dateparse, regexp, append, etc.)
- Category mapping (site categories → Newznab standard)

Built-in definitions are embedded via `go:embed` in `internal/indexer/definitions/`. Dropping a `.yml` file into this directory makes it available. The indexer service (`internal/indexer/service.go`) handles CRUD, engine lifecycle caching, credential masking, and parallel multi-indexer search.

**Rationale**: Building a native engine keeps the "single binary" deployment model — no external Prowlarr instance needed. The Cardigann YAML format is a well-established standard with 700+ definitions maintained by the Prowlarr community. For ncore specifically, the format is simple: POST login + HTML scraping. The engine is ~600 lines of Go and uses only two new dependencies (`yaml.v3`, `goquery`). Future indexers can be added by dropping YAML files — no code changes needed.

---

## ADR-034: Download model and CRUD API
**Date**: 2026-03-29
**Status**: Accepted

**Context**: To track torrent downloads across their lifecycle (pending → downloading → seeding → completed), the system needs a persistent record linking a download to a media item, with optional episode/season scoping. qBittorrent integration is a later phase; this first step establishes the data model and API.

**Decision**: A `Download` table tracks each download request with fields: MediaItemID (required), EpisodeID (optional, for episode-level), SeasonNumber (optional, for season packs), IndexerID/IndexerName (denormalized — indexer may be deleted), Title, DownloadURL, Status (7-state enum: pending/downloading/downloaded/importing/seeding/completed/failed), and future-proofing fields (ClientTorrentHash, SavePath, SeedingRequired, LinkedToLibrary). CRUD endpoints: `POST /downloads`, `GET /downloads` (with mediaItemId/status filters), `GET /downloads/{id}`, `PUT /downloads/{id}` (status update).

**Rationale**: Persisting downloads server-side decouples the UI from the torrent client's state, enables retry/audit, and allows computing episode download status. The denormalized IndexerName survives indexer deletion. Future qBittorrent integration will update status via the existing PUT endpoint.

---

## ADR-035: Per-indexer seeding rules
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Different trackers have different seeding requirements. Some require a minimum ratio, others a minimum seed time. These need to be configurable per indexer rather than globally.

**Decision**: Add `SeedMinRatio` (float64) and `SeedMinTime` (int, minutes) as dedicated fields on the Indexer model. Defaults are 0 (no requirement). These are exposed in the API and the indexer management UI. When a download is created from an indexer, these rules will be enforced during the seeding phase (qBittorrent integration — future).

**Rationale**: Dedicated DB fields (not JSON settings) enable querying and make the rules visible in the UI. Per-indexer granularity matches how trackers actually work — a ratio of 1.0 on one tracker and 0.5 on another.

---

## ADR-036: IndexerSearchModal on media detail page
**Date**: 2026-03-29
**Status**: Accepted

**Context**: Users need to search for torrents from the media detail page at three levels: entire item (movie or series), a specific season, or a specific episode. The existing IndexerTryModal was designed for the indexer settings page (single indexer, two-step meta search flow) and doesn't fit this use case.

**Decision**: A new `IndexerSearchModal` component with a single-step flow (no meta search needed — IMDb ID is already known from metadata). Features: indexer dropdown (all enabled indexers or a specific one), editable season/episode filter inputs (pre-filled from the search level), results table with Indexer column (multi-indexer results), and a Download button that calls `POST /downloads`. Search buttons are added at three levels: action bar on the media detail page (item-level), season headers in EpisodeGrid (season-level, `@click.stop`), and episode rows in EpisodeGrid (episode-level).

**Rationale**: Separate component from IndexerTryModal avoids conditional complexity — the two modals serve fundamentally different flows (known IMDb ID vs. discovery). The three-level search hierarchy matches how users think about missing content: "I need this whole series", "I need season 3", "I need S03E07".

---

## ADR-037: Computed episode download status
**Date**: 2026-03-29
**Status**: Accepted

**Context**: After adding a download, the episode list should reflect that a download is in progress — not just show "missing". The Episode model has no status field in the DB; status is computed from `hasFile` and `airDate`.

**Decision**: The `ListMediaEpisodes` handler now also fetches Download records for the media item and computes a per-episode `downloadStatus` field (added to the Episode API schema as an optional enum). The resolution order: episode-level download (by EpisodeID) takes precedence, then season-level download (by SeasonNumber, applied to all episodes in that season), then item-level download (applied to all episodes). When multiple downloads target the same episode, the highest-priority active status wins (downloading > pending > downloaded > importing > seeding). Completed and failed downloads are excluded from the computation (they don't override the "missing" state).

**Rationale**: Computing status at query time rather than storing it keeps the Episode table clean and avoids sync issues between Download and Episode records. The cascade logic (episode → season → item) correctly handles the three download scopes. The frontend EpisodeGrid shows download statuses with a distinct sky-blue color scheme to differentiate from file-based states.

---

## ADR-038: qBittorrent client adapter with cookie-based auth
**Date**: 2026-03-30
**Status**: Accepted

**Context**: Phase 3 requires sending torrents to a download client and tracking their progress. qBittorrent is already running in the homelab (ADR-006). The Web API v2 uses cookie-based authentication (SID cookie from POST `/api/v2/auth/login`).

**Decision**: Build a native Go client in `internal/integration/qbittorrent/` following the TVDB client pattern — mutex-guarded session, lazy authentication, auto-retry on 403 (session expiry). The SID cookie is managed as a plain string field (no `http.CookieJar`). Methods: `TestConnection`, `AddTorrent` (magnet/URL via multipart POST), `GetTorrent`/`GetTorrents` (status polling), and `MapState` (maps qBit's 17+ states to simplified categories). Settings stored as `qbit_url`, `qbit_username`, `qbit_password` with connection test endpoint `POST /settings/test-qbittorrent`.

**Rationale**: Manual SID management keeps the pattern identical to TVDB's bearer token handling — one string field, one mutex, clear invalidation. A separate `QbittorrentTestRequest` schema (url + username + password) avoids overloading the existing `ConnectionTestRequest` (which only has apiKey). The `MapState` helper is included in the client package for immediate use by the download worker.

---

## ADR-039: Download path setting with mutual exclusion from library paths
**Date**: 2026-03-30
**Status**: Accepted

**Context**: qBittorrent needs a directory to save downloaded files. This directory must be within `LIBRARY_BASEPATH` (same filesystem boundary as libraries) but cannot overlap with any library path — otherwise the sync service would pick up incomplete downloads.

**Decision**: A `qbit_download_path` setting selectable via the existing FolderBrowser component on the Settings page. Two-way validation enforces mutual exclusion:
1. **Settings side**: When saving `qbit_download_path`, the settings service validates the path is within `basePath` and does not match any existing library path (`store.ListLibraries()`).
2. **Library side**: When creating or updating a library, the library service reads `qbit_download_path` from settings and rejects if the library path matches.

The library service accepts a `SettingsGetter` interface (just `Get(key) (string, error)`) to avoid circular imports with the settings package.

**Rationale**: Two-way validation is bombproof — neither side can create a conflict regardless of operation order. The interface-based dependency keeps the packages decoupled. Using the existing FolderBrowser component means zero new frontend components.

---

## ADR-040: Download queue worker with ticker-based polling
**Date**: 2026-03-30
**Status**: Accepted

**Context**: Download records are created with status "pending" when users click Download in the IndexerSearchModal. Something needs to pick these up, send them to qBittorrent, and track their progress through the download lifecycle.

**Decision**: A `download.Service` runs a background goroutine with a 30-second ticker. On each tick it:
1. **Sends pending**: Queries downloads with status "pending", calls `qbittorrent.AddTorrent` for each, updates status to "downloading" and stores the torrent hash and save path.
2. **Polls active**: Queries downloads with status "downloading" or "seeding", calls `qbittorrent.GetTorrent` for each by hash, maps qBit state via `MapState`, and updates status accordingly.
3. **Enforces seeding rules**: When a torrent enters the "seeding" state, looks up the indexer's `SeedMinRatio` and `SeedMinTime`. Transitions to "completed" only when both thresholds are met (or if both are 0). If the indexer has been deleted, seeding is considered complete.

The qBittorrent client is lazily created from settings on first use — if settings aren't configured yet, the tick is silently skipped.

**Rationale**: Ticker-based polling is simple and sufficient for a single-user homelab app. 30 seconds balances responsiveness with qBittorrent API load. Lazy client creation means the server starts without errors even if qBittorrent isn't configured yet. Failed torrents are marked immediately so users see the error in the UI.

---

## ADR-041: GORM FK CASCADE constraints replace manual cascade deletes
**Date**: 2026-03-30
**Status**: Accepted

**Context**: Deleting a Library or MediaItem required explicit handler code to cascade-delete all child records (MediaFiles, MediaMetadata, Episodes, SeasonMonitors, Downloads). This was error-prone — the Downloads table was missed initially, leaving orphaned records.

**Decision**: Use GORM's `constraint:OnDelete:CASCADE` tag on all foreign key fields that reference a parent entity:
- `MediaItem.LibraryID` → CASCADE (Library deletion cascades to all its MediaItems)
- `MediaMetadata.MediaItemID` → CASCADE
- `MediaFile.MediaItemID` → CASCADE
- `SeasonMonitor.MediaItemID` → CASCADE
- `Episode.MediaItemID` → CASCADE
- `Download.MediaItemID` → CASCADE
- `Download.EpisodeID` → SET NULL (nullable FK — preserve download record when episode is deleted)

SQLite requires `PRAGMA foreign_keys = ON` to enforce FK constraints, enabled at connection time. Manual cascade delete code removed from handlers; unused `DeleteXxxByMediaItem` methods removed from the Store interface (except those still used by matching/sync services).

**Rationale**: Database-level constraints are more reliable than application-level cascade logic — they can't be forgotten when new child tables are added. Simplifies handler code and reduces the Store interface surface. The DB can be safely deleted and recreated since AutoMigrate rebuilds the schema.

---

## ADR-042: Authenticated torrent fetch via indexer engine + two-step download resolution
**Date**: 2026-03-30
**Status**: Accepted

**Context**: Private trackers require authentication (cookies) to download .torrent files. Passing the download URL directly to qBittorrent's `urls` field fails silently because qBit has no session cookies. Additionally, some trackers (e.g., nCore) return an HTML page at the download URL containing the real .torrent link — a standard Cardigann `download.selectors` pattern.

**Decision**: Fetch .torrent files through the Cardigann engine's authenticated HTTP client (`FetchDownload` on Engine, `FetchTorrent` on indexer.Service), then upload the raw bytes to qBittorrent via multipart file upload (`AddTorrentFile`). If the response is HTML instead of bencode, parse it with the definition's `download.selectors` to extract the real download link and fetch again. Compute info hash locally from the .torrent bytes using a minimal bencode parser + SHA1.

**Rationale**: This is the standard Cardigann flow — `download.selectors` exist exactly for two-step resolution. Fetching through the engine reuses the existing authenticated session. Local hash computation avoids the unreliable `extractHash` (magnet-only) approach.

---

## ADR-043: Downloads section on media detail page with server-side qBit enrichment
**Date**: 2026-03-30
**Status**: Accepted

**Context**: Users had no visibility into download status from the media detail page, and no way to retry, delete, or replace downloads.

**Decision**: Add a `DownloadList` component on the media detail page showing all downloads for the media item. The backend enriches `GET /downloads?mediaItemId=X` with real-time progress/speed from qBittorrent. Added `DELETE /downloads/{id}` (with optional `deleteFiles` query param) and `GET /downloads/{id}/files` (proxies qBit's torrent file listing). The frontend polls every 5s while active downloads exist, supports retry (set status back to pending), delete (removes from DB + qBit), and replace (delete old after new download created via IndexerSearchModal).

**Rationale**: Server-side enrichment avoids exposing qBit credentials to the frontend and reduces round-trips. The replace flow deletes the old download only after the user picks a new one (not immediately), preventing accidental data loss. Torrent file listing is on-demand (user clicks a button) rather than auto-polled.
