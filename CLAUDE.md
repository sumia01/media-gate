# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Media Gate

Self-hosted, single-binary media management app replacing the Sonarr + Radarr + Overseerr + Prowlarr stack. Go backend + Vue 3 frontend, served as one embedded binary.

## Tech Stack

- **Backend**: Go — stdlib `net/http` (Go 1.22+), GORM (SQLite/Postgres), `log/slog`, koanf (config)
- **Frontend**: Vue 3 + TypeScript (Composition API), Tailwind CSS v4, Vue Router, embedded via `//go:embed`
- **API contract**: OpenAPI spec in `api/` is the single source of truth — Go server code generated with oapi-codegen v2 (strict server mode), TypeScript client generated with openapi-typescript + openapi-fetch

## Project Structure

```
media-gate/
├── cmd/server/          # Go entrypoint (main.go)
├── internal/
│   ├── api/v1/          # Generated oapi-codegen server + handlers (package apiv1)
│   ├── config/          # koanf configuration loading
│   ├── library/         # Library service (CRUD, path validation, folder browsing)
│   ├── sync/            # Sync service (reads library dirs → creates MediaItems)
│   ├── jobqueue/        # Job queue (single worker, history persisted to SQLite)
│   ├── matching/        # Media matching service (TMDB/TVDB auto-match + manual)
│   ├── settings/        # Settings service (CRUD, masking, connection tests)
│   ├── store/           # Store interface + GORM implementations (Library, MediaItem, MediaFile, QualityProfile, SeasonMonitor, Setting, JobRecord)
│   ├── integration/
│   │   ├── tmdb/        # TMDB API v3 client (search, get, test)
│   │   └── tvdb/        # TVDB API v4 client (JWT auth, search, get, test)
│   └── logging/         # slog setup
├── frontend/            # Vue 3 + TypeScript SPA
│   └── src/
│       ├── api/         # Generated TypeScript API client
│       ├── composables/ # Shared reactive state (useJobQueue)
│       ├── components/
│       │   ├── layout/  # App shell: sidebar, topbar, page layout
│       │   └── media/   # Media-related components + shared types
│       ├── views/       # Route-level page components
│       └── router/      # Vue Router config
├── api/                 # OpenAPI spec + oapi-codegen config
├── docs/                # Architecture, decisions, roadmap
├── .air.toml            # Air hot-reload config for Go dev
├── .env.example         # Documented configuration keys
├── Makefile             # Build pipeline
└── go.mod
```

## Build & Run

```bash
# Full build (generate + frontend + Go binary)
make build

# Run the compiled binary
./media-gate

# Development — Air (Go hot-reload) + Vite (frontend HMR) in parallel
make dev

# Install dev tools (air, oapi-codegen)
make tools

# Code generation only (Go + TypeScript from OpenAPI spec)
make generate

# Frontend build only
make frontend

# Clean build artifacts
make clean
```

## Configuration

Configuration loads from `.env` file and/or `MEDIAGATE_`-prefixed environment variables. Config is organized into nested groups (api, db, log, library). Underscore in key names maps to nesting level.

| .env key | Env var | Config field | Default | Description |
|----------|---------|-------------|---------|-------------|
| `API_PORT` | `MEDIAGATE_API_PORT` | `API.Port` | `8080` | HTTP server port |
| `DB_PATH` | `MEDIAGATE_DB_PATH` | `DB.Path` | `media-gate.db` | SQLite database path |
| `LOG_LEVEL` | `MEDIAGATE_LOG_LEVEL` | `Log.Level` | `info` | Log level (debug/info/warn/error) |
| `LOG_FORMAT` | `MEDIAGATE_LOG_FORMAT` | `Log.Format` | `text` | Log format (text/json) |
| `LIBRARY_BASEPATH` | `MEDIAGATE_LIBRARY_BASEPATH` | `Library.BasePath` | `/mnt` | Base path for library directories (path traversal guard) |

## Key Architecture Decisions

- **Store interface pattern**: All data access goes through a Go `Store` interface with GORM implementations. Business logic never touches the database directly.
- **OpenAPI-first**: Change the spec in `api/openapi.yaml`, then run `make generate`. Never hand-edit generated code (`internal/api/v1/gen.go`, `frontend/src/api/schema.d.ts`).
- **Versioned API**: Routes under `/api/v1`, Go code in `internal/api/v1/` (package `apiv1`). Future versions get their own package.
- **Single binary**: The Vue SPA builds into `frontend/dist/`, gets embedded into the Go binary via `frontend/embed.go`. No separate web server needed.
- **go:generate**: `go generate ./...` runs oapi-codegen to regenerate Go server code.
- **Modular frontend**: Components organized by concern — `layout/` for shell pieces, `media/` for domain components, `views/` for route-level pages.

## Development Status

Project has completed **Phase 0** (scaffolding), **Phase 0.5** (frontend layout), **Phase 0.75** (libraries & catalog sync), **Phase 1a** (TMDB/TVDB integration, settings, media matching & job history persistence), and is progressing through **Phase 1b** (core media management: entity model redesign with MediaFile/QualityProfile/SeasonMonitor, add/delete requested media, global search). See `docs/ROADMAP.md` for the full plan and `docs/DECISIONS.md` for ADRs.
