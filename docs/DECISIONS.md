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
