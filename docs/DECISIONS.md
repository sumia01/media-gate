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
