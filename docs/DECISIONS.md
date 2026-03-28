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
