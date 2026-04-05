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

---

## ADR-044: Release folder isolation and companion file import
**Date**: 2026-03-30
**Status**: Accepted

**Context**: The importer only imported video files, placing them flat in the target directory. Companion files (subtitles, NFO, images) were ignored and lost when the torrent was deleted from qBittorrent. When two releases of the same movie/episode were downloaded, their files mixed in the same folder — making it impossible to cleanly delete one release without affecting the other.

**Decision**: Each import creates a release subfolder named after the torrent title inside the target directory (e.g., `Title (Year)/ReleaseName/video.mkv`). All non-junk torrent files are hardlinked/copied into the release folder — video files get `MediaFile` DB records as before, companion files (subtitles, NFO, images, subtitle subdirectories) are imported alongside but not tracked in the database. On delete, tracked video files are removed first, then release folders containing only companion files (no remaining video files) are cleaned up with `os.RemoveAll`. Known junk files (`.exe`, `.bat`, `.msi`, torrent spam like `RARBG.txt`, `WWW.*.txt`) are skipped during import.

No DB schema changes are required — companion files are isolated by the filesystem structure. The sync service already descends into non-season subdirectories (release folders), so it continues to discover video files inside release subfolders without modification. The delete handler is backward-compatible with the old flat layout: `onlyCompanionsLeft` returns false when other items' video files are present, preventing accidental `RemoveAll`.

**Rationale**: Filesystem isolation via release subfolders is the simplest approach — no new DB tables, no migration, no companion file lifecycle management. Each release is self-contained: its video + companion files live together and are cleaned up together. This matches how Plex/Jellyfin resolve sidecar subtitles (by filename adjacency within the same folder).

---

## ADR-045: Path traversal protection across all filesystem operations
**Date**: 2026-04-01
**Status**: Accepted

**Context**: The application creates directories and writes files based on user input (library paths, download paths) and external data (torrent file names from qBittorrent API, media titles from TMDB/TVDB). Without validation, a malicious torrent with `../../etc/cron.d/evil` in its file names could cause the importer to read or write files outside the intended directories.

**Decision**: Three layers of path traversal defense:

1. **Library service** (`internal/library/service.go`): `validatePath()` uses `filepath.Clean` + `strings.HasPrefix` to ensure all library paths (Create, Update, Browse) stay within `LIBRARY_BASEPATH`. Rejects prefix tricks (e.g., `basePath + "evil"`), relative paths, and empty/whitespace input.

2. **Settings service** (`internal/settings/service.go`): `validateDownloadPath()` applies the same `filepath.Clean` + `HasPrefix` check to the `qbit_download_path` setting, plus mutual exclusion with existing library paths.

3. **Importer** (`internal/importer/path.go`): `safePath()` validates that both source (download dir) and destination (library dir) paths stay within their respective base directories during the import loop. Torrent file names from the qBittorrent API are validated before any filesystem operation (hardlink, copy, mkdir). Files that fail validation are skipped with a warning log.

Additionally, `sanitizePath()` in the importer strips `/`, `\`, and other illegal characters from media titles (TMDB/TVDB) and release folder names before using them in `filepath.Join`, preventing title-based traversal.

**Known limitation**: Symlinks inside allowed directories that point outside are not detected by the string-prefix check. The OS-level path stays within the base, but the resolved target may not. This is documented in test suite (`TestSymlinkTraversal_DocumentedLimitation`). In practice, symlink creation requires existing filesystem access within the base path, and Docker bind-mounts provide an additional boundary.

**Rationale**: Defense in depth — user-facing inputs (library paths, download paths) are validated at the API boundary, and external data (torrent file names) is validated at the import boundary. The `filepath.Clean` + `HasPrefix` pattern is simple, stdlib-only, and proven effective against all standard traversal vectors on Linux. Comprehensive test coverage (69 test cases across 3 packages) exercises adversarial paths including `../`, prefix tricks, empty strings, unicode look-alike slashes, null bytes, and basePath parent escapes.

---

## ADR-046: Monitor worker with auto-grab and season pack preference
**Date**: 2026-04-01
**Status**: Accepted

**Context**: Users want to subscribe to movies and series so that new releases are automatically downloaded when they become available — the core Sonarr/Radarr auto-grab feature.

**Decision**: A background monitor worker (`internal/monitor/`) polls every 15 minutes:

1. **Movies**: Checks release date (from TMDB/TVDB metadata), searches indexers by IMDb ID, filters by quality profile, and creates a download for the best match.
2. **Series**: Builds a list of "wanted" episodes (aired, in a monitored season, no file, no active download), groups by season, searches indexers per-season, and matches results to episodes.

Season pack vs. individual episode preference is controlled by a global setting (`monitor_season_pack_preference`) with three modes:
- `prefer_packs` (default): Season pack preferred when >= 70% of aired episodes in a season are missing; individual episodes for smaller gaps.
- `prefer_episodes`: Always prefers individual episode torrents; season packs only as fallback.
- `packs_only`: Only downloads season packs, never individual episodes.

Per-season monitoring via `SeasonMonitor` model (default: all seasons monitored). `MonitorSearchStartedAt` tracks first unsuccessful search attempt for UI indicators.

**Removed**: `MonitorNewSeasons` field — superseded by planned periodic metadata refresh worker that will detect new seasons automatically.

**Deduplication**: Three-layer protection — wanted list filters out items with active downloads, fresh DB re-check before each `createAutoDownload` call (race condition guard), and active status map covering the full download lifecycle (pending → completed).

**Rationale**: Polling-based approach (vs. webhook/push) is simpler and sufficient for the use case — release dates don't change frequently. The configurable season pack preference addresses the tension between download efficiency (fewer torrents, consistent quality) and bandwidth efficiency (don't re-download episodes you already have).

---

## ADR-047: Atomic Add-to-Library with external episode prefetch
**Date**: 2026-04-01
**Status**: Accepted

**Context**: The Add to Library flow for series required choosing a library, monitor settings, quality profile, and per-season monitoring. The original implementation created the media item in the DB immediately, then applied settings in subsequent requests. This left partial records on failure and allowed the monitor worker to pick up items before the user finalized season choices.

**Decision**: Three changes to make the flow atomic:

1. **External episodes endpoint** (`GET /search/{source}/{externalId}/episodes`): Returns episode data from TMDB/TVDB without touching the DB. The frontend prefetches this on the media preview page so it's ready when the modal opens. Reuses `matching.Service.FetchExternalEpisodes` (new public wrapper around existing `fetchEpisodesFromSource`).

2. **Extended `AddMediaRequest`**: The `POST /libraries/{id}/media` body now accepts optional `monitored`, `mediaProfileId`, and `seasonMonitors[]` fields. Everything is sent in a single request.

3. **DB transaction**: The handler wraps the entire create flow in `Store.WithTx` — a new method on the Store interface that runs a callback inside a GORM transaction. The matching service is given a transactional store via `matching.Service.WithStore(txStore)`. If any step fails (item creation, metadata fetch, season monitor creation), the entire transaction rolls back — no partial records. Poster download runs after the transaction commits to minimize lock duration (network I/O outside the transaction).

The `WithTx` implementation creates a `SQLiteStore` backed by the transactional `*gorm.DB`, so all existing store methods automatically participate in the transaction.

**Frontend flow**: The modal is fully client-side until the final "Add" button. Step 1: library + monitor + quality profile. Step 2 (series): season/episode toggles using prefetched external data. Final click sends one POST with all choices.

**Rationale**: Atomic create prevents partial records and race conditions with the monitor worker. External episode prefetch eliminates wait time in the modal. `WithTx` on the Store interface is a general-purpose pattern reusable for any future multi-step write operations.

---

## ADR-048: Store.WithTx for transactional operations
**Date**: 2026-04-01
**Status**: Accepted

**Context**: The Store interface had no transaction support. Multi-step operations (e.g., create item + metadata + season monitors) were individual DB calls — if one failed mid-way, partial records remained.

**Decision**: Add `WithTx(fn func(Store) error) error` to the Store interface. The SQLite implementation uses GORM's `db.Transaction()` which handles begin/commit/rollback automatically. The callback receives a new `SQLiteStore` instance backed by the transactional `*gorm.DB` — all existing methods work without modification because they use `s.db` internally.

For services that need to participate in a caller's transaction, a `WithStore(store.Store) *Service` pattern creates a shallow clone of the service using the transactional store. Currently implemented on `matching.Service`.

**Rationale**: Adding transaction support at the Store interface level means any handler or service can wrap multi-step writes atomically. The `SQLiteStore{db: tx}` approach requires zero changes to existing CRUD methods. The `WithStore` pattern keeps services unaware of transaction management — the caller controls the transaction scope. Poster download is extracted from `applyMatch` into a separate `DownloadPoster` method called after transaction commit — all callers (`matchSingleItem`, `ManualMatch`, `AddMediaToLibrary` handler) follow this pattern to avoid holding DB write locks during network I/O.

---

## ADR-049: Configurable worker poll intervals via DB settings
**Date**: 2026-04-01
**Status**: Accepted

**Context**: The three background workers (monitor, download, importer) had hardcoded poll intervals. Tuning required code changes and redeployment.

**Decision**: Store worker poll intervals as DB settings (`worker_monitor_interval`, `worker_download_interval`, `worker_importer_interval`), values in seconds. Each worker reads its interval on startup via `settings.GetDurationWithDefault()`, falling back to the previous hardcoded default (monitor: 900s, download: 5s, importer: 10s). Workers subscribe to a settings change notification channel (`settings.Subscribe()`) and dynamically reset their ticker when their interval key changes — no restart required. The settings service gains a pub/sub mechanism: `Subscribe()` returns a `<-chan string` that receives the key name on each `Update()` call. Frontend Settings page exposes the three intervals in a "Workers" section with number inputs.

**Rationale**: DB-backed settings with live notification avoids the need for config file changes or process restarts. The channel-based notification pattern is lightweight and reusable for any future setting that needs to trigger runtime behavior changes. Storing values as seconds (string) is consistent with existing rate limit settings.

---

## ADR-050: Typed Settings API replacing generic key-value array
**Date**: 2026-04-01
**Status**: Accepted

**Context**: The settings API used a generic `{key: string, value: string}[]` array for both GET and PUT. All values were strings — numeric fields like worker intervals and rate limits required string conversion on the frontend. HTML `<input type="number">` naturally produces JavaScript numbers, causing JSON unmarshal errors (`cannot unmarshal number into string`) when the backend received them. Workarounds (frontend `String()` wrappers, backend `interface{}` coercion) were fragile.

**Decision**: Replace the generic `Setting` and `SettingsUpdate` schemas with a single typed `Settings` object where each setting is a named field with its correct type (string, integer, or string enum). The same schema is used for both GET response and PUT request body — all fields are optional so PUT sends only changed fields. The DB layer remains a string key-value store; type conversion (string ↔ int) lives in the API handler layer via `settingsToAPI` and `settingsFromAPI` converter functions. The `sensitive` boolean field is removed from the API — the backend still masks sensitive values in GET responses, and the frontend knows which fields are sensitive by convention.

**Rationale**: An explicit typed schema eliminates stringly-typed bugs, provides IDE autocompletion on both frontend and backend, and lets the OpenAPI code generators produce correct types (`*int` in Go, `number` in TypeScript). The settings key set is closed and known — there's no reason for a generic array. Keeping the DB layer as key-value strings avoids a migration and keeps the settings service (used by workers via `Get`/`GetWithDefault`) unchanged.

---

## ADR-051: Library Copies — completed downloads as file management UI
**Date**: 2026-04-02
**Status**: Accepted

**Context**: When torrents finish seeding, the download worker marks them `completed` and removes them from qBittorrent, but the `Download` DB records persist. Meanwhile, imported files live in release-isolated folders in the library. Users had no way to manage individual release copies (e.g., delete a 480p copy while keeping a 720p one) without deleting the entire media item or using the filesystem directly.

**Decision**: Split the DownloadList component into two visual sections: "Downloads" (active torrents: pending through seeding) and "Library Copies" (completed downloads with `linkedToLibrary=true`). Library copies show simplified cards (no progress/speed/torrent actions) with an inline delete confirmation. Deleting a library copy calls the existing `DELETE /downloads/{id}?deleteFiles=true` endpoint, which already runs `cleanupImportedFiles` — removing the release folder from disk, deleting matching `MediaFile` DB records, cleaning up empty parent directories, and recalculating the media item status.

**Rationale**: No backend changes needed — the `DeleteDownload` handler with `deleteFiles=true` already performs full release folder cleanup via `cleanupImportedFiles` (reconstructs release path from `BuildTargetDir` + `BuildReleaseFolderName`, removes disk files, deletes `MediaFile` records by path prefix, companion file check, empty parent removal, status recalc). The release folder isolation pattern (ADR established in Phase 3) ensures each torrent's files are self-contained in their own subfolder, making per-release deletion safe and clean. Frontend-only change keeps the scope minimal.

---

## ADR-052: Indexer settings merge and password masking safety
**Date**: 2026-04-02
**Status**: Accepted

**Context**: Editing an indexer and saving without changing the password caused the password to be lost. The backend masked password fields (replacing the value with `****...`) before sending to the frontend. On save, the frontend omitted masked values, but still sent a `settings: {}` object — the backend treated any non-nil settings as a full replacement, overwriting the stored JSON with an empty map. Additionally, `<input type="password">` hid the masked `****` prefix from users, making it impossible to tell whether the field contained a masked value or a real password.

**Decision**: Three-layer fix: (1) Backend `Update` now uses `mergeSettings` instead of replacing the entire settings JSON — incoming keys overwrite, missing keys are preserved. (2) `mergeSettings` skips values starting with `****` to prevent masked placeholders from overwriting real credentials. (3) Frontend clears masked fields to empty on edit open, with a placeholder "Unchanged — leave empty to keep current"; empty password fields are excluded from the update request.

**Rationale**: The merge approach matches how other optional fields (name, enabled, priority) already work in `Update` — only provided values change. The three layers provide defense in depth: frontend omits unchanged passwords, backend merge preserves missing keys, and the `****` prefix check catches any masked values that slip through.

---

## ADR-053: Login pre-GET for session cookie establishment
**Date**: 2026-04-02
**Status**: Accepted

**Context**: The Cardigann login engine sent a direct POST to the tracker login page. Some trackers (e.g. nCore) require a session cookie set by a GET request to the login page before accepting the POST — without it, the login fails even with correct credentials.

**Decision**: Add a GET request to the login URL before the POST in `Engine.Login`. The HTTP client's cookie jar captures the session cookie from the GET response, which is then automatically included in the subsequent POST.

**Rationale**: This matches the behavior of Prowlarr/Jackett's Cardigann implementation. The pre-GET is lightweight (single extra request) and harmless for trackers that don't require it. The cookie jar handles propagation automatically.

---

## ADR-054: Environment variable fallback for TMDB/TVDB API keys
**Date**: 2026-04-02
**Status**: Accepted

**Context**: API keys were only configurable via the UI (DB-backed settings, ADR-014). Container and headless deployments benefit from injecting API keys via environment variables without requiring UI setup.

**Decision**: Add `TMDB_APIKEY` and `TVDB_APIKEY` to the koanf config (also available as `MEDIAGATE_TMDB_APIKEY` / `MEDIAGATE_TVDB_APIKEY`). The settings service accepts an `envFallbacks` map at construction. `Get()` checks the DB first — if no DB entry exists, it falls back to the env value. DB always wins when both are set. The Settings API response includes `tmdbApiKeyFromEnv` / `tvdbApiKeyFromEnv` booleans so the frontend can show a subtle hint ("Configured via environment variable") when an env fallback is present.

**Rationale**: DB-first priority ensures UI-saved keys are never overridden unexpectedly. Env fallback supports infrastructure-as-code workflows (Docker Compose, Kubernetes secrets) where API keys are injected at deploy time. The frontend hint is intentionally subtle — it informs without being intrusive, and the user can still test the connection to verify the env key works.

---

## ADR-055: Season monitor modal when enabling monitoring on detail page
**Date**: 2026-04-02
**Status**: Accepted

**Context**: When a series was added without monitoring and the user later enabled the monitor toggle on the detail page, all seasons defaulted to monitored (because missing SeasonMonitor rows default to `monitored=true`). The user had no opportunity to choose which seasons to monitor. Additionally, the Add-to-Library modal showed the season selection step even when monitor was toggled off, which was redundant.

**Decision**: Three changes: (1) When toggling monitor ON for a series on the detail page, intercept the toggle and show a SeasonMonitorModal with per-season toggles (all on by default). On confirm, send `PATCH /media/{id}` with both `monitored: true` and a new `seasonMonitors` array field for atomic upsert. On cancel, leave the item unmonitored. (2) The `MediaItemUpdate` OpenAPI schema gains a `seasonMonitors` field, and the backend handler upserts season monitor records (create-or-update). (3) The Add-to-Library modal skips the season step when the monitor toggle is off, and omits `seasonMonitors` from the request body. (4) Season monitored/unmonitored badges in the EpisodeGrid are hidden when the media item itself is not monitored.

**Rationale**: Monitoring is a deliberate choice — users should always explicitly pick which seasons to track before the auto-grab worker acts on them. Skipping the season step when unmonitored avoids a confusing extra click. Atomic `seasonMonitors` on PATCH keeps the API consistent with the existing create flow (POST) which already supports the same field.

---

## ADR-056: At-rest encryption for sensitive settings
**Date**: 2026-04-02
**Status**: Accepted

**Context**: Sensitive credentials (API keys, passwords) were stored as plaintext in the SQLite database. While the API layer masks values before returning them to clients, direct database access (backups, DB browsers) exposes secrets in cleartext.

**Decision**: Add AES-256-GCM encryption in the `settings.Service` layer. A master key is derived from `MEDIAGATE_SECRET_KEY` env var via SHA-256. Encrypted values are stored with an `enc:` prefix (`enc:<base64(nonce+ciphertext)>`) so the system can distinguish encrypted from plaintext values. If no key is configured, values are stored in plaintext with a startup warning (dev-friendly). An idempotent `MigrateEncryption()` runs at startup to encrypt any existing plaintext sensitive values. The `internal/crypto` package is stdlib-only (no external dependencies).

**Rationale**: Service-layer encryption keeps the store simple and encryption transparent to all callers (Get/Update/List). The `enc:` prefix enables gradual migration without downtime. SHA-256 key derivation is appropriate because the input is a secret key (not a password), so slow KDF is unnecessary. Plaintext fallback avoids breaking dev workflows.

---

## ADR-057: Unified secrets management — indexer credentials in Settings table
**Date**: 2026-04-02
**Status**: Accepted

**Context**: Indexer credentials were stored as plaintext JSON in `Indexer.Settings` column with separate masking logic (`maskSettings` based on definition metadata). This duplicated the settings service's masking pattern and meant indexer secrets weren't covered by at-rest encryption (ADR-056).

**Decision**: Move password-type indexer fields (identified by `field.Type == "password"` in Cardigann definitions) from `Indexer.Settings` JSON to the shared `Settings` table. Key pattern: `indexer:{id}:{fieldName}` (e.g., `indexer:5:password`). Non-sensitive fields (username, text) remain in the JSON column. The `settings.Service` provides `SetIndexerSecret`, `GetIndexerSecrets`, and `DeleteIndexerSecrets` helpers. On indexer delete, cascade-delete removes related Setting rows via `DeleteSettingsByPrefix`. An idempotent `MigrateCredentials()` runs at startup to move existing credentials. Indexer secret keys are automatically excluded from the settings API (`List` filters out `indexer:*` keys).

**Rationale**: Single canonical location for all secrets enables consistent encryption, masking, and auditing. The `indexer:{id}:{field}` key pattern provides natural namespacing. `DeleteSettingsByPrefix` enables clean cascade without FK constraints between Settings and Indexers. The API contract and frontend behavior are unchanged — indexer settings still appear as a `map[string]string` with masked password fields.

---

## ADR-058: JWT + refresh token authentication architecture
**Date**: 2026-04-02
**Status**: Accepted

**Context**: All API endpoints and frontend routes were publicly accessible. Need email/password auth with session persistence (remember-me) without being overly complex. All users have equal access (no roles).

**Decision**: Short-lived JWT access tokens (15 min, HS256, `Authorization: Bearer` header) + long-lived refresh tokens (24h default, 30d with remember-me, stored in DB + HTTP-only cookie). JWT signing key derived from existing `MEDIAGATE_SECRET_KEY` via `crypto.DeriveKey` (SHA-256). Passwords hashed with bcrypt. Refresh tokens are rotated on each use (old revoked, new issued). Login/Refresh/Logout are manual HTTP handlers (not in OpenAPI spec) because oapi-codegen strict server doesn't expose `http.ResponseWriter` for cookie access. SSE uses `?token=` query param fallback since EventSource can't set custom headers. Auth middleware skips public paths (login, refresh, health, poster images). Default user bootstrapped from `DEFAULTUSER_EMAIL`/`DEFAULTUSER_PASSWORD` env vars if no users exist; app exits if neither env vars nor DB users are found.

**Rationale**: JWT + refresh token is a well-understood pattern that balances security (short access token TTL) with UX (transparent refresh). HTTP-only cookies for refresh tokens prevent XSS token theft. Manual handlers for cookie-dependent endpoints keep the OpenAPI-first approach clean for all other endpoints. Deriving the JWT key from the existing secret key avoids adding another config value. Poster images are public because `<img>` tags cannot set `Authorization` headers — these are non-sensitive cached assets anyway.

---

## ADR-059: Initial setup wizard with dynamic library base path
**Date**: 2026-04-02
**Status**: Accepted

**Context**: A fresh installation with an empty database required manually setting env vars (`DEFAULTUSER_EMAIL`/`DEFAULTUSER_PASSWORD`, `LIBRARY_BASEPATH`, API keys) before the app was usable. No browser-based first-run experience existed.

**Decision**: Add a 6-step setup wizard (`/setup`) that runs on first launch: (1) Create admin account, (2) Set library base path, (3) Configure torrent client, (4) Add indexer (skippable), (5) TMDB API key (required), (6) TVDB API key (optional). Onboarding state tracked in DB via `onboarding_step` and `onboarding_completed` settings. Two new unauthenticated endpoints: `POST /api/v1/auth/setup` (first-user creation, guarded by `CountUsers() == 0`) and `GET /api/v1/setup/status`. `LIBRARY_BASEPATH` moved from env-only config to a DB-backed setting with env fallback (same pattern as TMDB/TVDB keys, ADR-054). Library service refactored to use a `BasePathProvider` interface instead of a static string, with `settings.Service.BasePath()` as the implementation. Server starts successfully with 0 users (`Bootstrap()` returns nil with a log message). Frontend router guard checks setup status on every navigation and redirects to `/setup` when incomplete. Existing installations (users exist, no `onboarding_completed` key) are auto-detected as completed — the wizard never appears.

**Rationale**: Browser-based setup eliminates the need for env var configuration on first run, making the app accessible to non-technical users. The `BasePathProvider` interface allows library path to be changed at runtime without restart. The two-endpoint approach (status + setup) keeps the auth flow clean — setup returns the same `LoginResponse` as login, and the wizard steps use the normal authenticated settings API after auto-login. Skippable steps (indexer, TVDB) allow users to get started quickly and configure optional features later.

---

## ADR-060: Status recalculation after match and resync
**Date**: 2026-04-02
**Status**: Accepted

**Context**: When a series was matched (auto or manual), `applyMatch` set the status to `"available"` before episode lists were fetched and stored. `RecalcMediaItemStatus` — which correctly compares aired episodes against files on disk — was never called after matching. Similarly, `ResyncMediaItem` re-scanned files but did not recalculate status. This caused series with partial episode coverage to show as "available" instead of "partial".

**Decision**: (1) Add a `StatusRecalculator` interface in the `matching` package (implemented by `sync.Service`) and inject it via `SetStatusRecalculator()` to avoid circular imports. Call `RecalcMediaItemStatus` at the end of `applyMatch`, after episodes are stored. (2) Call `RecalcMediaItemStatus` in the `ResyncMediaItem` handler after re-scanning files. (3) Inject `*eventbus.Bus` into the matching service via `SetBus()`. Publish `media.item_matched` from `applyMatch` and `media.resync_completed` from the resync handler so the frontend receives real-time status updates via SSE.

**Rationale**: The existing `RecalcMediaItemStatus` function already had the correct partial/available logic (comparing aired episodes against media files). The bug was that it was never invoked after matching or resync. The `StatusRecalculator` interface avoids a `matching → sync` import cycle. Publishing SSE events ensures the frontend reflects the corrected status without requiring a manual page refresh — the frontend already listened for these event types but the backend never emitted them.

---

## ADR-061: Discover page with TMDB trending/popular and recently added
**Date**: 2026-04-02
**Status**: Accepted

**Context**: The home page (`/`) showed static demo data with hard-coded TMDB poster URLs. Needed real content: recently added items from the user's libraries and trending/popular content from TMDB.

**Decision**: Four separate `GET /discover/*` endpoints instead of a single aggregated endpoint. `recently-added` queries the local DB (cross-library, sorted by createdAt DESC, limit 20, metadata-enriched items only). `trending`, `popular-movies`, `popular-series` call TMDB's `/trending/all/week`, `/movie/popular`, `/tv/popular` respectively. New `DiscoverItem` schema (distinct from `MatchCandidate`) with `mediaType` and `rating` fields. TMDB endpoints return empty arrays (not errors) when API key is not configured. Frontend fetches all 4 sections in parallel on mount with independent loading skeletons.

**Rationale**: Separate endpoints allow each section to load and fail independently — recently-added always works even without TMDB configuration. `DiscoverItem` avoids polluting `MatchCandidate` with fields irrelevant to matching (rating, mediaType) and vice versa (confidence, existingMediaId). `VoteAverage` was added to existing `MovieResult`/`TVResult` structs (zero-value ignored by existing consumers) to avoid duplicating types for popular endpoints.

---

## ADR-062: Remote indexer definitions from Prowlarr/Indexers GitHub repo
**Date**: 2026-04-03
**Status**: Accepted

**Context**: Only the embedded `ncore.yml` definition was available. Users needed access to the full Prowlarr indexer catalog (~550 definitions) when adding indexers, without requiring manual YAML management.

**Decision**: On startup, load definitions from a disk cache (`.cache/definitions/`) if fresh (< 24h), otherwise fall back to the embedded `ncore.yml`. A background `RefreshWorker` (60s startup delay, 24h ticker) downloads the Prowlarr/Indexers GitHub tarball (`GET /repos/Prowlarr/Indexers/tarball/master`), extracts `definitions/v11/*.yml` files, parses them with `cardigann.ParseDefinition`, writes to disk cache, and hot-swaps the in-memory definition map under a `sync.RWMutex`. All cached engines are invalidated on refresh. Unparseable definitions are logged and skipped (never fail startup). The `ErrorBlock.Message` field was changed from `string` to a `StringOrText` type with custom `UnmarshalYAML` to handle both `message: "text"` and `message: {text: "text"}` forms used in v11 definitions. Three definitions with invalid YAML (unescaped regex backslashes) in the upstream repo are skipped.

**Rationale**: The tarball approach downloads all ~550 definitions in a single HTTP call (~2-5MB compressed) instead of 550 individual file fetches. Disk caching ensures fast startup and offline operation. The embedded `ncore.yml` fallback guarantees at least one definition is always available. The `RWMutex` on the definitions map allows concurrent reads during search while still supporting hot-swap on refresh. The 24h refresh interval matches GitHub's unauthenticated rate limit (60 req/hour) with ample margin.

---

## ADR-063: Cardigann engine — cookie login, encoding, and URL fixes
**Date**: 2026-04-03
**Status**: Accepted

**Context**: BitHU indexer returned no search results despite passing connection tests. Investigation revealed three issues: (1) cookie-based login method not implemented, (2) ISO-8859-2 encoded responses parsed as raw bytes producing garbled text, (3) URL `$raw` suffix concatenated without `&` separator.

**Decision**: Added `loginCookie()` method that parses `name=value; name2=value2` cookie strings from definition inputs and injects them into the HTTP client's cookie jar. Added `readBody()` helper that detects the definition's `encoding` field and converts non-UTF-8 responses using `golang.org/x/text/encoding/ianaindex`. Fixed URL building to insert `&` between `params.Encode()` and `rawSuffix` when both are non-empty. Also added `FieldDef.Default` support with template rendering for definitions that use default values referencing `.Result`, and search path template rendering via `RenderTemplate`.

**Rationale**: These are all standard Cardigann features used by many Prowlarr definitions. Cookie login is used by trackers like BitHU that authenticate via browser cookies. Encoding support is needed for Hungarian (ISO-8859-2), Russian (windows-1251), and other non-UTF-8 sites. The `$raw` separator fix prevents malformed query strings.

---

## ADR-064: FlareSolverr integration for Cloudflare-protected indexers
**Date**: 2026-04-03
**Status**: Accepted

**Context**: Public indexers like 1337x are behind Cloudflare protection, returning 403 to direct HTTP requests. FlareSolverr is a widely-used Docker sidecar that solves Cloudflare challenges via headless browser. Prowlarr definitions mark these indexers with `info_flaresolverr` setting type.

**Decision**: Added a global `flaresolverr_url` setting (Settings UI with test connection). The engine's `doRequest()` wrapper checks if the definition has an `info_flaresolverr` setting and a URL is configured, then routes GET requests through FlareSolverr's `POST /v1` API. POST requests (login) always go direct. Cookies from FlareSolverr's solved response are injected into the engine's cookie jar for subsequent requests. The indexer add/edit form shows an amber warning banner when a definition needs FlareSolverr but it's not configured, with a link to Settings.

**Rationale**: Transparent proxy approach — no changes to individual definitions. The `doRequest()` wrapper replaces `httpClient.Do` at all GET call sites (pre-login, verifyLogin, search, fetchURL) while login POST stays direct. FlareSolverr URL is injected into engine config at both `getOrCreateEngine` and `TestConnection` paths.

---

## ADR-065: JSON response parsing for Cardigann engine
**Date**: 2026-04-03
**Status**: Accepted

**Context**: ~97 cached indexer definitions use `response: type: json` in their search paths (Milkie, YTS, HHD/UNIT3D, ABNormal, etc.). The engine only supported HTML parsing via goquery, causing these definitions to silently fail.

**Decision**: Added JSON response detection via `SearchPath.Response.Type` field. When `"json"`, the engine unmarshals the response body and uses `resolveJSONPath()` for dot-path traversal (simple keys, nested paths, `key[N]` array indexing, `$` for root, `..key` for parent traversal). Four JSON patterns supported: flat array behind a key (`rows.selector: torrents`), root array (`$`), attribute sub-object (`rows.attribute: attributes`), and nested arrays with parent access (`rows.attribute: torrents`, `rows.multiple: true`). `jsonValueToString()` handles float64→int formatting (no scientific notation) and bool→`"True"`/`"False"` (matching UNIT3D case map key convention). The existing `parseRow` was refactored: field extraction stays HTML/JSON-specific, but defaults/text-rendering/SearchResult-construction moved to shared `buildSearchResult()`.

**Rationale**: Inline JSON path traversal (~40 lines) instead of external library — the selector syntax used in definitions is simple (dot-separated keys + array indexing + parent reference). The `buildSearchResult` refactor ensures defaults, text templates, filters, and result construction are identical for both HTML and JSON paths, avoiding logic duplication. Case mapping in JSON mode compares extracted string values (not CSS selector presence), with `"*"` as wildcard — matching Prowlarr's behavior.

---

## ADR-066: Search headers support for API-key authenticated indexers
**Date**: 2026-04-03
**Status**: Accepted

**Context**: Milkie and other API-based indexers authenticate via custom HTTP headers (e.g. `x-milkie-auth: {{ .Config.apikey }}`) defined in `search.headers`. The engine ignored this field, causing 401 responses.

**Decision**: Added `Headers map[string][]string` to the `Search` struct. In the `Search()` method, after creating the HTTP request, each header value is rendered as a Go template (with the same `TemplateContext` used for inputs) and set on the request.

**Rationale**: Simple addition — reuses existing `RenderTemplate` infrastructure. Connection tests for these indexers often succeed because they test login (which may have no auth requirement) rather than search, so the 401 only manifested during actual searches.

---

## ADR-067: Media detail search sends title as text query fallback
**Date**: 2026-04-03
**Status**: Accepted

**Context**: The IndexerSearchModal (opened from media detail page via "Search Indexers") only sent the IMDB ID to the search API, without the media title. Indexers that don't support IMDB-based search (e.g. Milkie) received an empty text query and returned no results. The IndexerTryModal (on the indexers page) already sent both title and IMDB ID correctly.

**Decision**: Added `query: props.title` to the IndexerSearchModal's GET `/indexers/search` request, matching IndexerTryModal's behavior. Also fixed the title prop passed from MediaDetailView to use `item.title` (plain title) instead of `item.title (year)` — the year suffix caused poor search results on indexers that match by title text.

**Rationale**: The Cardigann engine renders search URL templates using either `.Keywords` (text query) or `.Query.IMDBID`. When IMDB is not supported by a definition, `.Keywords` is the only fallback — it must be populated. Stripping the year avoids false negatives from indexers that don't include the year in their title format.

---

## ADR-068: Download worker retry with exponential backoff
**Date**: 2026-04-03
**Status**: Accepted

**Context**: When qBittorrent is temporarily unreachable (restart, network blip) or an indexer's torrent download fails transiently (rate limit, Cloudflare), `sendPending` immediately marks downloads as `"failed"`. Users must manually click "Retry" in the UI. The monitor worker's auto-grabs are effectively wasted on the first transient error.

**Decision**: Added automatic retry with exponential backoff to the download worker. Three new fields on `Download`: `RetryCount` (int), `NextRetryAt` (*time.Time), `LastError` (string). Backoff schedule: 30s, 2m, 10m, 30m, 1h (5 retries max). A qBit health check (`TestConnection()`) runs before `sendPending` — if qBit is down, the entire send pass is skipped without consuming retry attempts. Manual retry via UI resets all retry state. Frontend shows last error for failed downloads and retry count/next attempt time for pending downloads in backoff.

**Rationale**: Transient failures (qBit restart, indexer rate limit) are common in homelab environments. Burning downloads to "failed" on the first error forces unnecessary manual intervention. The health check prevents retry exhaustion when qBit is completely offline. The 5-retry/1h-max backoff caps total retry window at ~1.5h before giving up — long enough for typical restarts but short enough to surface real problems.

---

## ADR-069: Pure-Go SQLite and cross-platform prod builds
**Date**: 2026-04-03
**Status**: Accepted

**Context**: The project needed cross-platform release binaries (linux/amd64, darwin/arm64, windows/amd64). The original SQLite driver (`gorm.io/driver/sqlite` wrapping `mattn/go-sqlite3`) requires CGO, which makes cross-compilation significantly harder — each target needs a matching C cross-compiler. macOS targets are especially problematic as they require the macOS SDK, which cannot legally be distributed in Docker images. Zig CC was attempted as a universal C cross-compiler but failed on macOS targets due to missing system libraries (`libresolv`, `CoreFoundation`).

**Decision**: Replaced `gorm.io/driver/sqlite` with `github.com/glebarez/sqlite` (wraps `modernc.org/sqlite`, a pure-Go SQLite implementation). Added `Dockerfile.build` (multi-stage: Node frontend build → Go cross-compile) and Makefile targets (`build-linux-amd64`, `build-darwin-arm64`, `build-windows-amd64`, `build-all`). Binaries output to `dist/` via Docker `--output`. All builds use `CGO_ENABLED=0`.

**Rationale**: The pure-Go SQLite driver eliminates all CGO/C-toolchain complexity. Cross-compilation becomes trivial (`GOOS`/`GOARCH` flags only). The multi-stage Dockerfile handles frontend build + API code generation + Go compilation in one reproducible pipeline. The `glebarez/sqlite` driver is a drop-in replacement — only the import path changes, all GORM code remains identical. The ~10-15% performance difference vs C SQLite is negligible for a self-hosted media manager. Also fixed pre-existing TypeScript errors in setup wizard components (optional `message` field on `ConnectionTestResult`) and layout components (`noUncheckedIndexedAccess` compatibility).

---

## ADR-070: Windows path support in setup wizard
**Date**: 2026-04-04
**Status**: Accepted

**Context**: The setup wizard's Library Base Path step validated absolute paths by checking `startsWith('/')`, which only works for Unix paths. On Windows, absolute paths use drive letters (e.g. `F:\mediagate\dist\media\`), causing the wizard to reject valid paths with "Base path must be an absolute path".

**Decision**: Extended the frontend validation in `SetupBasePath.vue` to accept both Unix (`/...`) and Windows (`X:\...` or `X:/...`) absolute paths using the regex `/^[a-zA-Z]:[/\\]/`. No backend changes needed — Go's `filepath.Clean` and `filepath.Separator` are already platform-aware.

**Rationale**: With cross-platform binaries now available (Phase 5.0), Windows is a supported target. The frontend validation must match what the backend accepts on each platform.

---

## ADR-071: Open browser on startup on Windows
**Date**: 2026-04-04
**Status**: Accepted

**Context**: When users launch the Windows binary, they need to manually open a browser and navigate to `http://localhost:<port>`. This is a friction point for non-technical users who expect a desktop-app-like experience.

**Decision**: On Windows, the server opens the default browser at `http://localhost:<port>` immediately before `ListenAndServe`. Implemented via build-tag-separated files: `browser_windows.go` (calls `rundll32 url.dll,FileProtocolHandler`) and `browser_other.go` (no-op). The command is fire-and-forget (`exec.Command.Start()`).

**Rationale**: Minimal code, zero dependencies. Build tags keep the Windows-specific import (`os/exec`) out of other platforms. `rundll32 url.dll` is the standard Windows API for opening URLs in the default browser.

---

## ADR-072: Profile test-search with shared filter logic
**Date**: 2026-04-04
**Status**: Accepted

**Context**: Users had no way to verify that a media profile (quality profile) would produce the expected filtering results before enabling auto-grab on a library. The only option was to enable monitoring and wait — no dry-run capability existed.

**Decision**: Added a `GET /media-profiles/{id}/test-search` endpoint that searches all enabled indexers for a given title and filters results using the same profile logic as the monitor auto-grab worker. The core filtering logic (`indexer.FilterByProfile` in `internal/indexer/filter.go`) is a single shared function called by both the monitor worker and the test-search handler. Frontend adds a "Test" button to each profile row, opening a 3-step wizard modal (search media → [pick season for series] → view filtered results with auto-grab pick highlighted).

**Rationale**: Extracting `FilterByProfile` into the `indexer` package ensures the test endpoint and the monitor always use identical filtering logic — if the filter changes, both paths update automatically. The `indexer` package is the natural home because it owns `TorrentResult` and already depends on `fileparse` for title parsing. The alternative (duplicating filter code in monitor and handler) would risk silent divergence, defeating the purpose of a test feature.

---

## ADR-073: GitHub Actions release pipeline and Proxmox LXC deployment
**Date**: 2026-04-04
**Status**: Accepted

**Context**: The project had cross-platform build targets (`make build-linux-amd64`, etc.) using Docker, but no CI/CD pipeline for automated releases. Deploying to a Proxmox homelab required manual binary copying and service setup. The repository is private, so release assets need authenticated access.

**Decision**: Added two components:

1. **GitHub Actions release workflow** (`.github/workflows/release.yml`): Triggers on `v*` tag push. Builds frontend (Node 22), runs Go code generation (oapi-codegen), cross-compiles 3 binaries (linux/amd64, darwin/arm64, windows/amd64) with `CGO_ENABLED=0 go build -trimpath`, creates a GitHub Release with auto-generated notes and binary assets. Also syncs `deploy/proxmox-lxc.sh` to a public Gist via `popsiclestick/gist-sync-action` (requires `GIST_TOKEN` secret with `gist` scope).

2. **Proxmox LXC deploy script** (`deploy/proxmox-lxc.sh`): Interactive bash script run on a Proxmox host. Creates an unprivileged Debian 12 LXC container via `pct create`, downloads the binary from GitHub Release API using a fine-grained PAT, sets up a `mediagate` system user, systemd service with security hardening (`ProtectSystem=strict`, `NoNewPrivileges`), and installs an update script (`/usr/local/bin/media-gate-update`). Optional features: CIFS NAS mount (credentials in `/etc/cifs-credentials`, fstab entry), DB migration from existing install (`pct push` + matching secret key). The deploy script is distributed via public Gist to solve the bootstrap problem (private repo, no PAT yet).

**Rationale**: The Actions workflow replicates `Dockerfile.build` logic natively (no Docker-in-Docker), runs in GitHub's free tier (2000 min/month for private repos). Semver tags give explicit control over what becomes a release. The Gist sync solves the chicken-and-egg problem of accessing a private repo's deploy script without a PAT. The LXC script supports DB migration (existing secret key + `pct push`) for moving between Proxmox hosts or rebuilding containers. CIFS mount inside the LXC (rather than host bind mount) keeps containers self-contained and portable across multiple Proxmox hosts.

---

## ADR-074: YAML escape sanitization for Prowlarr indexer definitions
**Date**: 2026-04-05
**Status**: Accepted

**Context**: When syncing indexer definitions from the Prowlarr/Indexers GitHub repo, several YAML files fail to parse with Go's `yaml.v3` because they use escape sequences (`\/`, `\d`) that were valid in YAML 1.1 or tolerated by C# YamlDotNet but are rejected by `yaml.v3` (YAML 1.2 strict). This caused affected definitions (e.g. arabtorrents, btarg, swarmazon-api) to be silently dropped — they never loaded at all.

**Decision**: Two-pronged fix:

1. **Regex fallback for ID extraction** (`internal/indexer/definitions/remote.go`): When `yaml.Unmarshal` fails during header-only parsing (to extract the `id` field), a regex `^id:\s*(\S+)` extracts the ID instead. This ensures the raw YAML bytes still enter the definition map even if the file has invalid escapes.

2. **YAML escape sanitizer** (`internal/indexer/cardigann/sanitize.go`): `SanitizeYAML()` scans through YAML content byte-by-byte, tracks double-quoted string boundaries, and doubles backslashes for any escape sequence that `yaml.v3` would reject. Called in `ParseDefinition()` before `yaml.Unmarshal`. Valid escape set verified from `yaml.v3` source (`scannerc.go`): `0 a b t \t n v f r e ' " \ N _ L P x u U (space)`.

**Rationale**: The regex fallback is a minimal, safe change — `id:` always appears at the top level of Cardigann definitions, so a simple regex is reliable. The sanitizer handles the full parse by converting unknown escapes (e.g. `\d` → `\\d`, `\/` → `\\/`) to their literal equivalents, matching what YamlDotNet does implicitly. This is forward-compatible: any future upstream definitions with non-standard escapes will work automatically.

---

## ADR-075: Backend directory restructuring
**Date**: 2026-04-05
**Status**: Accepted

**Context**: The project root mixed Go backend files (`cmd/`, `internal/`, `go.mod`, `go.sum`, `.air.toml`, `.env.example`) with the Vue frontend (`frontend/`), shared API spec (`api/`), CI/CD configs, and docs. As the project grew, the flat structure made it harder to navigate and reason about boundaries between the Go backend and the rest of the repo.

**Decision**: Moved all Go backend files into a `backend/` subdirectory:

- `cmd/server/` → `backend/cmd/server/`
- `internal/` → `backend/internal/`
- `go.mod`, `go.sum` → `backend/go.mod`, `backend/go.sum`
- `.air.toml` → `backend/.air.toml`
- `.env.example` → `backend/.env.example`
- `frontend/embed.go` → `backend/frontend/embed.go` (must be inside Go module root for the embed directive to work)

The `api/` directory stays at the repo root since both the Go backend (`go:generate` with relative paths) and the Vue frontend (`openapi-typescript ../api/openapi.yaml`) use it. The `frontend/` SPA source also stays at the root. Build artifacts flow: `frontend/dist/` → copied to `backend/frontend/dist/` → embedded into Go binary.

The Go module name (`github.com/sumia01/media-gate`) is unchanged. Since `go.mod` moved into `backend/`, that directory is now the Go module root. All internal package import paths (e.g. `github.com/sumia01/media-gate/internal/store`) continue to work without modification — Go resolves them relative to the `go.mod` location.

Updated files: `Makefile` (all Go commands prefixed with `cd backend &&`), `Dockerfile.build` (builder stage `WORKDIR /app/backend`), `.github/workflows/release.yml` (`go-version-file: backend/go.mod`, working-directory for Go steps), `.air.toml` (removed irrelevant exclude dirs), `.gitignore` (updated paths), `backend/internal/api/v1/generate.go` (one extra `../` level for `api/` spec path).

**Rationale**: Clean separation of concerns at the filesystem level. The Go module root trick (keeping the module name unchanged) avoids touching ~25 Go files with import path updates. The `api/` stays shared to avoid duplication or complex symlink setups. The build pipeline (`make build`, `make dev`, Docker, CI) all verified working after the move.

---

## ADR-076: Dead code removal
**Date**: 2026-04-05
**Status**: Accepted

**Context**: A comprehensive backend review identified 36 dead code items across exported methods, event constants, and struct fields. Cleaning these up reduces cognitive load and prevents accidental use of non-functional code paths.

**Decision**: Removed confirmed dead code:

1. **Unused exported methods** (4): `RevokeAllUserTokens` (auth), `BroadcastJSON` (SSE), `AddTorrent` + `extractHash` + `btihRegexp` + `postMultipart` (qBittorrent), `Caps()` (Cardigann engine).
2. **Unused event constants** (5): `ImportStarted`, `MediaItemSynced`, `MediaItemRemoved`, `MediaItemDeleteReq`, `MediaItemDeletePayload` type.
3. **Dead internal struct field** (1): `SearchResult.Description` — set by Cardigann parser but never propagated to `TorrentResult`.

Intentionally kept items after deeper analysis:
- `TemplateContext.False` — used by 69+ Prowlarr YAML definitions as `{{ .False }}` in Go templates at runtime.
- `SearchQuery.Year` / `SearchQuery.Genre` — referenced by 15+ YAML definitions; unimplemented features, not dead code.
- YAML schema fields (`LegacyLinks`, `Caps.Modes`, `AllowRawSearch`, `CountBlock`, `SearchPath.Categories`) — part of the Prowlarr definition format; removing them would silently drop parsed data. Go's `yaml.v3` ignores unknown fields, so they don't break parsing, but they document the upstream schema.
- External API response fields (16 TMDB/TVDB fields) — standard practice for modeling full API responses; low value removing them.

**Rationale**: Only removed items with zero runtime callers/consumers. Kept YAML-template-consumed fields that the initial review incorrectly flagged — external YAML definitions form part of the runtime codepath even though no Go code references the fields directly.

---

## ADR-077: SSE connect race condition fix
**Date**: 2026-04-05
**Status**: Accepted

**Context**: A single browser tab opening a page (e.g. media detail) created 3 SSE connections instead of 1. Three components call `useEventStream()` during initial render (TopBarA via useJobQueue, MediaDetailView, DownloadList). The `connect()` function in `useEventStream.ts` guards against duplicate connections by checking `eventSource.value`, but `eventSource.value` is only assigned after an async ticket fetch resolves. All three components see `null` and each starts its own fetch → openSSE → EventSource.

**Decision**: Added a synchronous `connecting` flag (module-scoped `let connecting = false`). Set to `true` at the top of `connect()` before the async ticket fetch, reset to `false` in `openSSE` (success), `onerror` (failure), and `disconnect()`. Both `connect()` and `useEventStream()` check the flag alongside `eventSource.value`.

**Rationale**: Minimal fix — one boolean flag, no refactoring needed. The flag is synchronous so it's set before any component yields to the event loop, preventing concurrent `connect()` calls. Reset on all exit paths (success, error, explicit disconnect) ensures reconnect logic still works.

---

## ADR-078: Code duplication cleanup — shared providers, worker loop, helpers
**Date**: 2026-04-05
**Status**: Accepted

**Context**: Backend deep review identified 11 code duplication patterns. qBittorrent client construction was copy-pasted across 4 locations with no settings invalidation. The Start/Stop/run worker loop was identical in 3 services. Profile filter unpacking, discover handlers, TMDB/TVDB client creation, year parsing, and several conversion helpers were all duplicated.

**Decision**: Introduced three new packages and several helper functions:

1. **`qbittorrent.Provider`** (`backend/internal/integration/qbittorrent/provider.go`) — lazy-cached qBit client with `Client()` and `Invalidate()`. Uses `SettingsGetter` interface to avoid circular import with settings package. Created once in `main.go`, shared by download/importer/handlers. A goroutine subscribes to settings changes and invalidates the cache when qBit URL/username/password change.
2. **`worker.Loop`** (`backend/internal/worker/loop.go`) — generic ticker-based worker with settings-driven interval, configurable startup delay, and settings-change subscription. Download, importer, and monitor services now embed `*worker.Loop` and only define their `processOnce()`.
3. **`indexer.FilterByMediaProfile`** — accepts `*store.MediaProfile` directly, unmarshals JSON fields internally. Replaces identical unmarshal+filter code in handlers and monitor.
4. **`dateutil.ParseYear`** (`backend/internal/dateutil/dateutil.go`) — shared year parser with 1900–2099 validation, replacing two divergent implementations.
5. **`cachedTMDB`/`cachedTVDB`** on `matching.Service` — mutex-protected client cache keyed by API key, replaces 6 inline `NewClient` calls. Settings tests and discover handlers intentionally left uncached (test clients need fresh keys, discover is infrequent).
6. **`fetchDiscover` + `toDiscoverItem`** — helper on Handlers struct and unified converter, replacing 3+3 near-identical discover functions.
7. **`applyProfileFields`** — replaces `mediaProfileFromAPI` + `updateMediaProfileFromAPI` (85% identical).
8. **`derefString`** — replaces 4-line optional `*string` deref block duplicated in auth handlers.

Intentionally skipped: fetch-by-ID + 404 pattern (Go generics can't express typed response variants cleanly), external detail vs metadata conversion (different purposes, not true duplication).

**Rationale**: Each abstraction was introduced only where 3+ identical copies existed or where the duplication masked a bug (qBit client was never invalidated on settings change despite a comment claiming it was). New packages are minimal — no frameworks, just functions and small structs. The `SettingsGetter` and `SettingsSubscriber` interfaces avoid circular imports without adding complexity.
