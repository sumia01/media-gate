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
- **Key views**: Libraries list, Library detail (media grid + add media search), Media detail (poster, metadata, match/delete actions, quality profile selector, IndexerSearchModal with download support at item/season/episode level), Media preview (external media detail from search, Add to Library modal), Indexers (CRUD + Try it out modal), Settings

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
| qBittorrent   | Torrent download client     | Integrated |
| TMDB          | Movie & TV metadata (API v3) | Integrated |
| TVDB          | TV series metadata (API v4)  | Integrated |
| Indexers       | Torrent/NZB search (Cardigann engine) | Integrated |

More integrations will be defined incrementally as development progresses.

## Project Structure

```
media-gate/
├── cmd/server/          # Go entrypoint (main.go)
├── internal/
│   ├── api/v1/          # Generated oapi-codegen server + handlers (versioned)
│   ├── config/          # koanf configuration loading (nested struct groups)
│   ├── library/         # Library service (CRUD, path validation, folder browsing)
│   ├── sync/            # Sync service (per-file scanning, folder grouping, resync)
│   ├── jobqueue/        # Job queue (single worker, history persisted to SQLite)
│   ├── matching/        # Media matching service (TMDB/TVDB auto-match + manual)
│   ├── settings/        # Settings service (CRUD, masking, connection tests)
│   ├── fileparse/       # Filename parser (resolution, source type, season/episode extraction)
│   ├── store/           # Store interface + GORM implementations (Library, MediaItem, MediaFile, Episode, QualityProfile, SeasonMonitor, Setting, JobRecord, Indexer, Download)
│   ├── indexer/         # Indexer service (CRUD, multi-indexer search, engine lifecycle)
│   │   ├── cardigann/   # Cardigann YAML engine (definition parser, login, search, HTML scraping, filters)
│   │   └── definitions/ # Embedded indexer definitions (go:embed *.yml)
│   ├── integration/
│   │   ├── tmdb/        # TMDB API v3 client
│   │   ├── tvdb/        # TVDB API v4 client (JWT auth)
│   │   └── qbittorrent/ # qBittorrent Web API v2 client (cookie auth)
│   ├── download/        # Download queue worker (sends pending → qBit, polls status, seeding rules)
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
| `Episode` | `episodes` | Expected episode from TMDB/TVDB for a series MediaItem. Cross-referenced against MediaFiles to determine present/missing episodes. |
| `MediaMetadata` | `media_metadata` | Matched TMDB/TVDB metadata for a MediaItem (external ID, IMDb ID, poster, overview, credits) |
| `QualityProfile` | `quality_profiles` | Download quality preferences (resolutions, sources, exclude tags). Assignable to Library or MediaItem. |
| `SeasonMonitor` | `season_monitors` | Per-season monitoring toggle for series MediaItems (unique per media item + season number) |
| `Setting` | `settings` | Key-value config stored in DB (API keys, etc.; sensitive flag for masking) |
| `JobRecord` | `job_records` | Persisted completed/failed job history (type, status, timestamps) |
| `Indexer` | `indexers` | Configured indexer instance (name, definition ID, credentials as JSON, priority, enabled, per-indexer seeding rules: seedMinRatio, seedMinTime) |
| `Download` | `downloads` | Tracks download lifecycle (links to MediaItem + optional Episode/Season, indexer info, status: pending/downloading/downloaded/importing/seeding/completed/failed, qBittorrent torrent hash + save path) |

All child foreign keys use GORM `constraint:OnDelete:CASCADE` (or `SET NULL` for nullable FKs like `Download.EpisodeID`), so deleting a Library cascades through MediaItems → MediaFiles, Episodes, SeasonMonitors, Downloads, MediaMetadata. SQLite FK enforcement is enabled via `PRAGMA foreign_keys = ON` at connection time.

## Backend Service Layer

```
HTTP Request
  → api/v1/handlers.go (generated interface, hand-written handlers)
    → library.Service (CRUD, path validation, folder browsing, download path conflict check)
    → settings.Service (settings CRUD, masking, TMDB/TVDB/qBittorrent connection tests)
    → store.Store (GORM → SQLite/Postgres)
    → indexer.Service (indexer CRUD, Cardigann engine orchestration, multi-indexer search)
    → jobqueue.Queue (enqueue background work, persist history to SQLite)
      → sync.Service (read disk, diff DB, create/remove MediaItems)
      → matching.Service (auto-match MediaItems to TMDB/TVDB)
```

- **library.Service** — manages Library CRUD with basePath validation (prevents path traversal); rejects library paths that match the configured download directory
- **sync.Service** — reads a library's directory, walks each media folder for video files (`.mkv`, `.mp4`, etc.), parses filenames for resolution/source/season/episode via the `fileparse` package. Supports three series layouts: season subfolders, flat mixed episodes, and split-season folders (grouped into one MediaItem). Creates MediaItem + MediaFile records per video file; detects removals; supports single-item resync.
- **jobqueue.Queue** — single-worker queue; prevents duplicate jobs per library; completed/failed job history persisted to SQLite `job_records` table (keeps last 200)
- **matching.Service** — auto-matches MediaItems to TMDB (movies) or TVDB (series) using parsed folder names; supports manual match override from UI; handles library-scoped search and adding requested media with full metadata; fetches and stores episode lists for series from TMDB/TVDB; extracts IMDb IDs from TMDB/TVDB responses; supports full re-match (all items) or unmatched-only mode; provides external detail preview (GetExternalDetail) for search results without persisting data
- **settings.Service** — manages DB-backed settings (API keys, download path, etc.); masks sensitive values in list responses; delegates to TMDB/TVDB/qBittorrent clients for connection testing; validates download path against basePath and existing library paths on save
- **tmdb.Client** — TMDB API v3 client; auth via `?api_key=` query param; search movies/TV, get details with credits and external IDs (`append_to_response`), get TV season episodes, test connection
- **tvdb.Client** — TVDB API v4 client; JWT auth via `POST /login`; search series (type-filtered), get extended details with characters and remote IDs (IMDb), get series episodes by season, test connection
- **indexer.Service** — manages indexer CRUD (configurations stored in DB with JSON credentials); loads Cardigann YAML definitions from embedded filesystem; lazy-creates and caches engine instances per indexer; parallel multi-indexer search with semaphore; credential masking in API responses; per-indexer seeding rules (seedMinRatio, seedMinTime). The Cardigann engine (`internal/indexer/cardigann/`) supports POST login with cookie sessions, HTML scraping via goquery, Go template rendering for dynamic inputs, and a filter pipeline (querystring, replace, dateparse, regexp, append, etc.)
- **Download CRUD** — download records managed directly through store (no separate service yet); `ListMediaEpisodes` handler computes per-episode `downloadStatus` from linked Downloads (episode-level → season-level → item-level fallback)
- **qbittorrent.Client** — qBittorrent Web API v2 client; cookie-based SID auth with mutex-guarded session; auto-retry on 403 (session expiry); methods: AddTorrent (magnet/URL), GetTorrent/GetTorrents (status polling), TestConnection; MapState helper maps qBit's 17+ states to simplified categories
- **download.Service** — background worker (30s polling interval); picks up pending downloads from DB and sends to qBittorrent with configured download path; polls active downloads for status changes; enforces per-indexer seeding rules (SeedMinRatio/SeedMinTime); lazily creates qBittorrent client from settings
