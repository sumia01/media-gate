# MediaGate

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/R6R11XD6DX)

Self-hosted, single-binary media management app built to replace the Sonarr + Radarr + Overseerr + Prowlarr stack.

Go backend, Vue 3 frontend, one executable. No containers required, no runtime dependencies — just drop the binary on your homelab box and go.

---

## Why

The *arr stack works, but running four separate services with their own databases, update cycles, and failure modes felt like overkill for a homelab. MediaGate consolidates all of that into a single process with a unified UI and one SQLite database.

## What it does

- **Library management** — scan directories, organize media, browse folders with path-traversal protection
- **Metadata matching** — TMDB & TVDB integration with auto-match and manual override
- **Indexer engine** — Cardigann-compatible YAML definitions, remote Prowlarr/Indexers sync, multi-indexer parallel search
- **Download orchestration** — qBittorrent integration, persistent download records, retry with exponential backoff, seeding rules
- **Auto-import** — hardlink/copy to library, release folder isolation, companion file handling, post-import resync
- **Monitor / auto-grab** — background worker watches for releases matching quality profiles, season pack preference
- **Real-time UI** — event bus + Server-Sent Events replace polling for downloads, imports, job status
- **Discover page** — trending/popular from TMDB, recently added from your libraries
- **Watched/seen tracking** — mark media as watched, seen badges across the UI
- **Setup wizard** — 6-step browser-based onboarding on first launch
- **Security** — AES-256-GCM at-rest encryption, JWT + refresh tokens, rate limiting, SSE ticket auth

## Tech stack

| Layer | Tech |
|-------|------|
| Backend | Go 1.22+, stdlib `net/http`, GORM, `log/slog`, koanf |
| Frontend | Vue 3 + TypeScript (Composition API), Tailwind CSS v4, Vue Router |
| Database | SQLite via pure-Go driver (`glebarez/sqlite`) — no CGO needed |
| API contract | OpenAPI spec &rarr; `oapi-codegen` (Go) + `openapi-typescript` (TS) |
| Build | Makefile + Docker multi-stage for cross-compilation |

## Quick start

### Development

```bash
make tools      # install air + oapi-codegen
make dev        # Air (Go hot-reload) + Vite (frontend HMR) in parallel
```

### Production build

```bash
make build      # generate code, build frontend, compile single binary
./media-gate    # serves UI + API on :8080
```

### Cross-platform release builds

```bash
make build-linux-amd64
make build-darwin-arm64
make build-windows-amd64
make build-all          # all three
```

Uses `Dockerfile.build` — Docker required, CGO is not.

## Configuration

Copy `backend/.env.example` to `backend/.env`, or use `MEDIAGATE_`-prefixed environment variables.

| Key | Default | Description |
|-----|---------|-------------|
| `SECRET_KEY` | — | **Required.** Master key for encryption + JWT signing |
| `API_PORT` | `8080` | HTTP server port |
| `DB_PATH` | `media-gate.db` | SQLite database path |
| `LIBRARY_BASEPATH` | `/mnt` | Root path for library directories |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `TMDB_APIKEY` | — | Fallback TMDB key (can also set in UI) |
| `TVDB_APIKEY` | — | Fallback TVDB key (can also set in UI) |
| `COOKIE_SECURE` | `false` | Set `true` behind a TLS-terminating reverse proxy |

Most settings are configurable through the web UI after initial setup.

## Deployment

**Simplest path:** copy the binary, create a systemd service, point a reverse proxy at it.

**Proxmox LXC:** `deploy/proxmox-lxc.sh` is an interactive script that creates a Debian 12 LXC container, downloads the binary from GitHub Releases, sets up a systemd unit, and optionally configures a CIFS NAS mount. Includes an in-place update script.

**Releases:** GitHub Actions builds cross-platform binaries on `v*` tag push.

## Project structure

```
media-gate/
├── backend/             # Go backend
│   ├── cmd/server/      #   entrypoint
│   ├── internal/        #   domain packages (api, auth, library, sync,
│   │                    #   matching, download, importer, indexer, ...)
│   └── frontend/        #   embed.go (compiled SPA embedded here)
├── frontend/            # Vue 3 + TypeScript SPA
│   └── src/
│       ├── api/         #   generated API client
│       ├── composables/ #   shared reactive state
│       ├── components/  #   UI components (layout, media)
│       └── views/       #   route-level pages
├── api/                 # OpenAPI spec (single source of truth)
├── docs/                # architecture decisions, roadmap
├── deploy/              # Proxmox LXC deployment script
├── Dockerfile.build     # multi-stage cross-platform builder
└── Makefile             # build pipeline
```

## Architecture highlights

- **OpenAPI-first** — change the spec in `api/openapi.yaml`, run `make generate`, never hand-edit generated code
- **Store interface pattern** — all data access through a Go interface with GORM implementations; `WithTx` for transactional writes
- **Single binary** — Vue SPA builds into `frontend/dist/`, embedded into Go via `go:embed`
- **Pure-Go SQLite** — no CGO, trivial cross-compilation
- **Event-driven** — internal event bus with typed events, SSE broker pushes to frontends
- **Thin HTTP handlers** — handlers are pure adapters; all business logic lives in service packages

Design decisions are documented as ADRs in `docs/DECISIONS.md`. Full roadmap in `docs/ROADMAP.md`.

## Status

This is an actively developed personal project. The core loop — discover, match, search indexers, download, import, monitor — is functional. See `docs/ROADMAP.md` for completed phases and what's planned.

## Disclaimer

MediaGate is a media library management tool. Users are solely responsible for ensuring their use of this software complies with all applicable laws in their jurisdiction. The authors do not endorse or encourage copyright infringement or any other illegal activity.

## License

This project is licensed under the [GNU General Public License v2.0](LICENSE).

## Support

If you find this project useful, consider [buying me a coffee](https://ko-fi.com/sumia01).
