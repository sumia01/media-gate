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
| TMDB / TVDB   | Media metadata              | Planned |
| Indexers       | Torrent/NZB search (Prowlarr replacement) | Planned |

More integrations will be defined incrementally as development progresses.

## Project Structure

```
media-gate/
├── cmd/server/          # Go entrypoint (main.go)
├── internal/
│   ├── api/             # Generated oapi-codegen server + handlers
│   ├── config/          # koanf configuration loading
│   ├── store/           # Store interface + GORM implementations
│   ├── integration/     # External service clients (qBittorrent, TMDB, etc.)
│   └── logging/         # slog setup, handler config
├── frontend/            # Vue 3 + TypeScript SPA
│   └── src/api/         # Generated TypeScript API client
├── api/                 # OpenAPI spec files (SSOT) + oapi-codegen config
├── docs/                # Project documentation
├── .env.example         # Documented configuration keys
├── go.mod
├── go.sum
└── Makefile             # Build pipeline: generate → frontend build → go build
```

## Key Design Principles

1. **OpenAPI as Single Source of Truth** — both Go server interfaces and TypeScript client are generated from the same spec
2. **Swappable storage** — database engine can be changed without touching business logic
3. **Single binary deployment** — minimal operational overhead
4. **Pluggable logging** — ready for future observability integrations
5. **Incremental development** — start small, integrate services one by one
