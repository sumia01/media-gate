# Media Gate — Architecture

## Overview

Media Gate is a self-hosted, all-in-one media management application that replaces the Sonarr + Radarr + Overseerr + Prowlarr stack with a single, custom-built solution.

## Tech Stack

### Backend — Go
- **API layer**: OpenAPI spec (SSOT) → generated with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) in **strict server** mode
- **Database ORM**: GORM with swappable driver (Postgres / SQLite)
- **Logging**: `log/slog` with pluggable handlers (stdout, file, future: Loki or similar)
- **Configuration**: [koanf](https://github.com/knadh/koanf) — loads from `.env` file (dev) and environment variables (production)
- **HTTP server**: stdlib `net/http` (Go 1.22+)

### Frontend — Vue + TypeScript
- **Framework**: Vue 3 with TypeScript
- **Styling**: Tailwind CSS
- **Routing**: Vue Router
- **API client**: Generated from the same OpenAPI spec via [openapi-typescript](https://github.com/openapi-ts/openapi-typescript) + [openapi-fetch](https://github.com/openapi-ts/openapi-typescript/tree/main/packages/openapi-fetch)
- **SPA**: Served by the Go backend via `embed.FS`
- **Key views**: Libraries list, Library detail (media grid + add media search), Media detail (poster, metadata, match/delete actions), Settings

### Data Layer
- **Interface-based**: A `Store` interface in Go, with concrete implementations for:
  - `sqlite` — lightweight, single-file, great for dev and small deployments
  - `postgres` — production-grade, for heavier usage
- **ORM**: GORM handles both drivers behind the Store interface

## Deployment

- **Single binary**: The Vue SPA is built, embedded into the Go binary via `//go:embed`, and served alongside the API
- **Target**: Self-hosted homelab server

## External Integrations

| Service       | Role                        | Status  |
|---------------|-----------------------------|---------|
| qBittorrent   | Torrent download client     | Planned |
| TMDB          | Movie & TV metadata (API v3) | Integrated |
| TVDB          | TV series metadata (API v4)  | Integrated |
| Indexers       | Torrent/NZB search (Prowlarr replacement) | Planned |

More integrations will be defined incrementally as development progresses.

## Project Structure

```
media-gate/
├── cmd/server/          # Go entrypoint (main.go)
├── internal/
│   ├── api/v1/          # Generated oapi-codegen server + handlers (versioned)
│   ├── config/          # koanf configuration loading (nested struct groups)
│   ├── library/         # Library service (CRUD, path validation, folder browsing)
│   ├── sync/            # Sync service (reads library dirs → creates MediaItems)
│   ├── jobqueue/        # Job queue (single worker, history persisted to SQLite)
│   ├── matching/        # Media matching service (TMDB/TVDB auto-match + manual)
│   ├── settings/        # Settings service (CRUD, masking, connection tests)
│   ├── store/           # Store interface + GORM implementations (Library, MediaItem, MediaFile, QualityProfile, SeasonMonitor, Setting, JobRecord)
│   ├── integration/
│   │   ├── tmdb/        # TMDB API v3 client
│   │   └── tvdb/        # TVDB API v4 client (JWT auth)
│   └── logging/         # slog setup, handler config
├── frontend/            # Vue 3 + TypeScript SPA
│   └── src/
│       ├── api/         # Generated TypeScript API client
│       ├── composables/ # Shared reactive state (useJobQueue, useGlobalSearch)
│       ├── components/
│       │   ├── layout/  # App shell: sidebar, topbar, page layout
│       │   └── media/   # Media-related components + shared types
│       ├── views/       # Route-level page components
│       └── router/      # Vue Router config
├── api/                 # OpenAPI spec files (SSOT) + oapi-codegen config
├── docs/                # Project documentation
├── .air.toml            # Air hot-reload config for Go dev
├── .env.example         # Documented configuration keys
├── go.mod
├── go.sum
└── Makefile             # Build pipeline: tools, generate, frontend, build, dev, clean
```

## Key Design Principles

1. **OpenAPI as Single Source of Truth** — both Go server interfaces and TypeScript client are generated from the same spec
2. **Versioned API** — API routes live under `/api/v1`, Go code in `internal/api/v1/` (package `apiv1`), ready for future versions side-by-side
3. **Swappable storage** — database engine can be changed without touching business logic
4. **Single binary deployment** — minimal operational overhead
5. **Modular frontend** — components split into layout (shell, sidebar, topbar) and domain (media cards, etc.) for reusability
6. **Pluggable logging** — ready for future observability integrations
7. **Incremental development** — start small, integrate services one by one

## Data Models

| Model | Table | Description |
|-------|-------|-------------|
| `Library` | `libraries` | A media library (name, path, mediaType: movie/series, optional quality profile) |
| `MediaItem` | `media_items` | A logical media entry — either synced from disk (source: disk) or manually requested (source: request). Links to quality profile and tracks monitor-new-seasons preference. |
| `MediaFile` | `media_files` | A physical file/folder on disk, linked to a MediaItem. Tracks path, fileName, size, resolution, sourceType, and optional season/episode numbers. |
| `MediaMetadata` | `media_metadata` | Matched TMDB/TVDB metadata for a MediaItem (external ID, poster, overview) |
| `QualityProfile` | `quality_profiles` | Download quality preferences (resolutions, sources, exclude tags). Assignable to Library or MediaItem. |
| `SeasonMonitor` | `season_monitors` | Per-season monitoring toggle for series MediaItems (unique per media item + season number) |
| `Setting` | `settings` | Key-value config stored in DB (API keys, etc.; sensitive flag for masking) |
| `JobRecord` | `job_records` | Persisted completed/failed job history (type, status, timestamps) |

## Backend Service Layer

```
HTTP Request
  → api/v1/handlers.go (generated interface, hand-written handlers)
    → library.Service (CRUD, path validation, folder browsing)
    → settings.Service (settings CRUD, masking, TMDB/TVDB connection tests)
    → store.Store (GORM → SQLite/Postgres)
    → jobqueue.Queue (enqueue background work, persist history to SQLite)
      → sync.Service (read disk, diff DB, create/remove MediaItems)
      → matching.Service (auto-match MediaItems to TMDB/TVDB)
```

- **library.Service** — manages Library CRUD with basePath validation (prevents path traversal)
- **sync.Service** — reads a library's directory, parses folder names for title/year, diffs against DB MediaFiles to add/remove entries. Creates a MediaItem + MediaFile per folder; cleans up orphaned MediaItems with zero files.
- **jobqueue.Queue** — single-worker queue; prevents duplicate jobs per library; completed/failed job history persisted to SQLite `job_records` table (keeps last 200)
- **matching.Service** — auto-matches MediaItems to TMDB (movies) or TVDB (series) using parsed folder names; supports manual match override from UI; handles library-scoped search and adding requested media with full metadata
- **settings.Service** — manages DB-backed settings (API keys etc.); masks sensitive values in list responses; delegates to TMDB/TVDB clients for connection testing
- **tmdb.Client** — TMDB API v3 client; auth via `?api_key=` query param; search movies/TV, get details, test connection
- **tvdb.Client** — TVDB API v4 client; JWT auth via `POST /login`; search series, get details, test connection
