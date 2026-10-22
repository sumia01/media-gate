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
- **Key views**: Libraries list, Library detail (media grid + add media search), Media detail (poster, metadata, match/delete actions, quality profile selector, downloads section with progress/retry/delete/replace, IndexerSearchModal with download support at item/season/episode level), Media preview (external media detail from search, Add to Library modal), Indexers (CRUD + Try it out modal), Settings

### Data Layer
- **Interface-based**: A `Store` interface in Go, with concrete implementations for:
  - `sqlite` — lightweight, single-file, great for dev and small deployments
  - `postgres` — production-grade, for heavier usage
- **ORM**: GORM handles both drivers behind the Store interface
- **Transactions**: `Store.WithTx(fn func(Store) error)` runs a callback inside a DB transaction; the callback receives a transactional Store instance so all methods automatically participate in the transaction

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
│   ├── download/        # Download queue worker (sends pending → qBit, polls status)
│   ├── importer/        # Import worker (hardlink/copy to library, seed cleanup)
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
| `Download` | `downloads` | Tracks download lifecycle (links to MediaItem + optional Episode/Season, indexer info, status: pending/downloading/downloaded/importing/seeding/completed/failed/import_failed, qBittorrent torrent hash + save path, linkedToLibrary flag) |

All child foreign keys use GORM `constraint:OnDelete:CASCADE` (or `SET NULL` for nullable FKs like `Download.EpisodeID`), so deleting a Library cascades through MediaItems → MediaFiles, Episodes, SeasonMonitors, Downloads, MediaMetadata. SQLite FK enforcement is enabled via `PRAGMA foreign_keys = ON` at connection time.

## Matching Package Organization

`backend/internal/matching/` retains one `Service`, its existing dependencies,
and its public API. Responsibility-based files make the workflows easier to
locate without introducing new collaborators:

| Files | Responsibility |
| --- | --- |
| [service.go](../backend/internal/matching/service.go), [clients.go](../backend/internal/matching/clients.go) | Service construction, transaction-bound cloning, settings-based provider clients and caching |
| [match.go](../backend/internal/matching/match.go), [match_activity.go](../backend/internal/matching/match_activity.go) | Automatic/manual matching, unmatching, stale-version checks, persistence and bounded activity details |
| [requests.go](../backend/internal/matching/requests.go), [request_scopes.go](../backend/internal/matching/request_scopes.go) | New/repeat requests, additive monitoring, requester attribution/activity and scope normalization |
| [search.go](../backend/internal/matching/search.go), [confidence.go](../backend/internal/matching/confidence.go) | Provider search, library membership annotation and confidence scoring |
| [metadata.go](../backend/internal/matching/metadata.go), [episodes.go](../backend/internal/matching/episodes.go) | Provider detail/episode conversion, external previews, and identity-preserving episode replacement |
| [refresh.go](../backend/internal/matching/refresh.go) | Metadata refresh candidates, semantic diffs and future-season policy |
| [poster.go](../backend/internal/matching/poster.go) | Poster coordination, current-candidate checks and atomic installation |
| [credits.go](../backend/internal/matching/credits.go), [contentratings.go](../backend/internal/matching/contentratings.go) | Provider-person identities and normalized regional certifications |

This is file organization, not a new service boundary. Provider I/O remains
outside write transactions; mutations/activity stay atomic, and existing
post-commit status, poster and event ordering is unchanged. `WithStore`,
`topStore`, client caches and mutex ownership retain their existing semantics.
See ADR-048, ADR-079, ADR-140, ADR-142 and ADR-144 in [the decision log](DECISIONS.md).

## Backend Service Layer

```
HTTP Request
  → api/v1/handlers.go (generated interface, hand-written handlers)
    → library.Service (CRUD, path validation, folder browsing, download path conflict check)
    → settings.Service (settings CRUD, masking, TMDB/TVDB/qBittorrent connection tests)
    → media.Service (media-settings mutation workflow and resource removal)
    → store.Store (GORM → SQLite/Postgres)
    → indexer.Service (indexer CRUD, Cardigann engine orchestration, multi-indexer search)
    → jobqueue.Queue (enqueue background work, persist history to SQLite)
      → sync.Service (read disk, diff DB, create/remove MediaItems)
      → matching.Service (auto-match MediaItems to TMDB/TVDB)
```

- **library.Service** — manages Library CRUD with basePath validation (prevents path traversal via `filepath.Clean` + prefix check); rejects library paths that match the configured download directory
- **sync.Service** — reads a library's directory, walks each media folder for video files (`.mkv`, `.mp4`, etc.), parses filenames for resolution/source/season/episode via the `fileparse` package. Supports three series layouts: season subfolders, flat mixed episodes, and split-season folders (grouped into one MediaItem). Creates MediaItem + MediaFile records per video file; detects removals; supports single-item resync.
- **jobqueue.Queue** — single-worker buffered queue; prevents duplicate type/library jobs and returns caller-owned job/progress/timestamp snapshots. `Enqueue` rejects full/stopping queues rather than blocking on capacity; sync/match HTTP adapters map these temporary rejections to 503 and retain duplicate 409 responses. `BeginShutdown` closes admission and discards waiting jobs before HTTP drain; job claiming shares its mutex. `Stop` then waits only for the active job, its history attempt, and final publications before bus/database teardown. Pending work is not durable/restarted; completed/failed history remains in SQLite `job_records` (last 200). See ADR-152.
- **matching.Service** — auto-matches MediaItems to TMDB (movies) or TVDB (series) using parsed folder names; supports manual match override from UI; handles library-scoped search and adding requested media with full metadata; fetches and stores episode lists for series from TMDB/TVDB; extracts IMDb IDs from TMDB/TVDB responses; supports full re-match (all items) or unmatched-only mode; provides external detail preview (GetExternalDetail) and external episode listing (FetchExternalEpisodes) for search results without persisting data; `WithStore(store.Store)` creates a shallow clone for transactional use
- **settings.Service** — manages DB-backed settings (API keys, download path, worker intervals, etc.); masks sensitive values in list responses; delegates to TMDB/TVDB/qBittorrent clients for connection testing; validates download path against basePath and existing library paths on save; pub/sub notification channel (`Subscribe`/`notify`) for live setting change propagation to workers
- **media.Service** — owns `UpdateMediaItem` through an API-independent `UpdateInput`: validate the actor/profile and fresh parent, apply settings and bulk monitoring, remember private keywords, and append requester attribution/activity in one transaction. Separate `UpdateSeasonMonitor` and `UpdateEpisodeMonitor` operations reuse the existing sync input types, preserve season-wide override clearing versus a single episode override, and transactionally touch the fresh parent with attribution/activity. Status recalculation and request/activity hints remain post-commit; only the whole-item operation reloads the item for its response. Recalculation/reload failures remain best-effort. HTTP adapters retain authentication-context extraction, input conversion, existing error mapping, and response enrichment. Resource-removal operations retain their existing preparation/external-cleanup boundaries. See ADR-153.
- **tmdb.Client** — TMDB API v3 client; auth via `?api_key=` query param; search movies/TV, get details with credits and external IDs (`append_to_response`), get TV season episodes, test connection
- **tvdb.Client** — TVDB API v4 client; JWT auth via `POST /login`; search series (type-filtered), get extended details with characters and remote IDs (IMDb), get series episodes by season, test connection
- **indexer.Service** — manages indexer CRUD (configurations stored in DB with JSON credentials); loads Cardigann YAML definitions from embedded filesystem; lazy-creates and caches engine instances per indexer; parallel multi-indexer search with semaphore; credential masking in API responses; per-indexer seeding rules (seedMinRatio, seedMinTime). The Cardigann engine (`internal/indexer/cardigann/`) supports POST login with cookie sessions, HTML scraping via goquery, Go template rendering for dynamic inputs, and a filter pipeline (querystring, replace, dateparse, regexp, append, etc.)
- **Download CRUD** — download records managed directly through store (no separate service yet); `ListMediaEpisodes` handler computes per-episode `downloadStatus` from linked Downloads (episode-level → season-level → item-level fallback)
- **qbittorrent.Client** — qBittorrent Web API v2 client; cookie-based SID auth with mutex-guarded session; auto-retry on 403 (session expiry); methods: AddTorrent (magnet/URL), AddTorrentFile (multipart .torrent upload with savepath/downloadPath/useAutoTMM/category), GetTorrent/GetTorrents (status polling), GetTorrentFiles (per-torrent file listing), DeleteTorrent (remove torrent + optional file deletion), EnsureCategory (auto-create), TestConnection; InfoHash helper computes SHA1 from .torrent bencode; MapState helper maps qBit's 17+ states to simplified categories
- **download.Service** — background worker (configurable polling interval, default 5s); picks up pending downloads from DB, fetches .torrent via indexer engine's authenticated session (two-step resolution for HTML intermediaries), uploads to qBittorrent with configured download path and category; handles duplicate torrents by reusing existing qBit entries; polls active downloads for status changes; transitions to "downloaded" when qBit reports completion; lazily creates qBittorrent client from settings; subscribes to settings changes for dynamic interval updates
- **importer.Service** — background worker (configurable polling interval, default 10s); picks up "downloaded" downloads, hardlinks (or copies on cross-device) torrent video files into library folder structure (`Title (Year)/` for movies, `Title (Year)/Season XX/` for series), creates MediaFile records; validates source and destination paths against their respective base directories before any filesystem operation (prevents path traversal via malicious torrent file names); checks per-indexer seeding rules — if no seeding needed, removes torrent and completes immediately; otherwise transitions to "seeding" and a cleanup worker monitors obligations, removing torrents from qBit when met; subscribes to settings changes for dynamic interval updates
