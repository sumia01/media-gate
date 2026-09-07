# MediaGate

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/R6R11XD6DX)

A self-hosted, single-binary media management app I built for my own homelab to consolidate what Sonarr, Radarr, Overseerr, Prowlarr, and Bazarr do into one place.

Go backend, Vue 3 frontend, one executable. No containers required, no runtime dependencies.

**~70 MB RAM | near-zero CPU** — runs comfortably on a Raspberry Pi or a minimal LXC container.

![MediaGate — Discover trending and popular media](docs/assets/mediagate.discover.jpg)

<details>
<summary><strong>More screenshots & demos</strong></summary>
<br>

<details>
<summary>Movie download — search, pick a release, send to qBittorrent</summary>
<img width="1306" height="1037" alt="movie-download" src="https://github.com/user-attachments/assets/2e8a6324-55ef-4c4a-87a0-8764590e68c2" />


</details>

<details>
<summary>Profile & indexer search — test quality profiles against live results</summary>
<img width="1306" height="1037" alt="profile-indexer" src="https://github.com/user-attachments/assets/40389b70-babe-44f9-b269-6fb4dd269f91" />

</details>

<details>
<summary>TV show monitoring — episode-level auto-grab setup</summary>
<img width="1306" height="1037" alt="tv-show-monitor" src="https://github.com/user-attachments/assets/e136d55d-c18c-4caf-a34c-0d46de34ad15" />

</details>

<details>
<summary>Plex library mapping — auto-refresh after import</summary>

![Plex library mapping](docs/assets/library-plex-maping.jpg)
</details>

</details>

---

## Why

I was running Sonarr, Radarr, Overseerr, Prowlarr, and Bazarr side by side in my homelab — five services, each with its own database, update cycle, and failure modes. It worked, but felt like more moving parts than I needed. MediaGate is my attempt to fold all of that into a single process with a unified UI and one SQLite database.

## Features

### Library Management

- Create movie and series libraries mapped to filesystem directories
- Automatic directory scanning with video file detection (resolution, source type, season/episode parsing)
- Series folder grouping ("Show Name Season N" folders merged under one title)
- Media item status tracking: `new`, `requested`, `partial`, `available`, `missing`
- Re-sync individual items or entire libraries as async jobs with progress tracking
- Path traversal protection enforced at all filesystem access points

### Metadata Matching

- Automatic matching against TMDB and TVDB with confidence scoring
- Manual search and override when auto-match is wrong
- Full metadata: title, overview, genres, year, rating, runtime, release date, IMDB ID, trailer URL, cast/crew
- Complete episode data: season/episode structure, air dates, runtimes
- Poster downloading and local caching
- Library-wide batch matching with progress tracking
- Configurable primary metadata source (TMDB or TVDB)

### Discover & Search

- Global search across TMDB/TVDB by query and media type
- Discovery feeds: trending, popular movies, popular series
- Recently added feed from local libraries
- Full external media preview before adding to a library
- Duplicate detection: results indicate if media already exists locally
- Persistent "Hide in library" filter on Discover, category, and similar-title pages; Recently Added stays visible
- Library identities include media type, so a movie and series with the same provider ID are not confused. TVDB-to-TMDB identity normalization remains a known limitation of badges and filtering

### Request Tracking

- Records who requested each movie or series, including multiple requesters for media already in the library
- Series requests preserve whole-series, future-season, season, and directly selected episode intent instead of deriving history from current monitoring state
- Media details show a compact, deduplicated requester summary; scoped intent stays stored as history rather than implying current monitoring state
- Enabling monitoring later attributes only newly enabled scopes to that user; unchanged or disabled scopes do not rewrite request history
- Repeat requests are idempotent and additively enable the requested monitoring scope without disabling another user's selections
- Deleted accounts retain anonymous request history as `Deleted user`; legacy requested items remain unattributed

### Media Activity

- Media details are split into a default operational **Details** tab and a read-only **Activity** tab
- Per-media chronological history records the actor, action, event-time scope/title, timestamp, and safe typed details independently from current monitoring and cumulative requester attribution
- Covers requests, monitoring/settings changes, manual and automatic grabs, manual download actions, match/metadata/resync changes, deletion preparation, manual subtitles, and global/per-user watched changes
- Latest automatic-search diagnostics and activity history start collapsed and load only while their Activity disclosure is open
- Stable cursor pagination, bounded rendering, abort/stale-response guards, and explicit SSE dirty hints keep long histories responsive without moving rows while they are being read
- Deleted users remain anonymous, private watched history stays private, media deletion cascades its history, and no legacy events are fabricated

### Episode Timeline

- Drag, swipe, or use keyboard/arrow controls to explore past and upcoming episodes of followed series
- Loads 14-day windows on demand, with bounded rendered days and nearby cache rather than downloading the whole calendar
- The initial view and Today shortcut center the subtly highlighted current-day column, with availability badges and explicit unmonitored-episode labels
- Download/import and metadata events refresh the visible dates without moving the timeline

### Indexer Engine

- Cardigann-compatible YAML indexer definitions (700+ torrent sites via Prowlarr/Indexers)
- Background worker refreshes definitions from GitHub every 24 hours
- YAML sanitization for escape sequences that parsers reject
- Multi-indexer parallel search with category and result limit filtering
- Search modes: general, TV search (season/episode), movie search (IMDB ID)
- Per-indexer priority, seed ratio/time requirements
- FlareSolverr integration for Cloudflare-protected sites
- Connection testing per indexer

### Media Profiles (Quality Filtering)

- Define preferred resolutions, languages, source types (BluRay, WEB-DL, etc.)
- Language filtering with AND/OR mode: OR = any language matches (order = priority), AND = all languages required
- English fallback: untagged releases (no language token in title) are treated as English when `eng` is in the profile — most indexers omit the language tag for English-only releases
- Priority-based ranking: resolution > language > source, with user-defined preference order
- Exclude tags to filter unwanted releases (global + per-profile)
- Season pack preference: `prefer_packs`, `prefer_episodes`, `packs_only`
- Assignable per-library or per-media-item (item overrides library)
- Profile test search: run live queries against indexers to preview filtering results
- Backend-driven profile matching: search results annotated with match status server-side (single source of truth)

### Download Management

- Full torrent lifecycle: `pending` → `downloading` → `downloaded` → `importing` → `seeding` → `completed`
- Background worker polls qBittorrent every 5 seconds (configurable)
- Automatic retry with exponential backoff (30s → 2m → 10m → 30m → 1h, up to 5 retries)
- qBittorrent health check: skips sending if unreachable to avoid burning retries
- Real-time progress, speed, and ETA tracking
- Inspect torrent file list per download
- Paginated all-downloads history: newest 30 initially, then 100 more per click, with status filters applied before pagination
- Download completion date on every finished row: exact qBittorrent time for new downloads, closest recorded `Downloaded by` time for legacy history
- Dual path support: local mount path vs qBittorrent NAS mount override
- qBittorrent category support

### Auto-Import

- Hardlinks completed downloads into organized library directories (preserves seeding)
- Season pack and single-episode detection via title parsing
- Post-import status recalculation
- Seeding obligation tracking: per-indexer seed ratio and seed time requirements
- Automatic torrent cleanup after seeding obligations are met
- Companion file handling (NFO, SRT) and empty directory cleanup

### Monitor (Automated Search & Grab)

- Background worker searches indexers every 15 minutes (configurable) for monitored media
- Episode-level monitoring hierarchy: episode monitor → season monitor → not monitored
- Monitors keyed by `(MediaItemID, SeasonNumber, EpisodeNumber)` — survives re-matching
- Toggle entire seasons (clears episode-level overrides)
- Auto-monitor new seasons when they appear
- Profile-based filtering of search results before grabbing
- Latest auto-download decision on the read-only Activity tab: release/profile counts, blocklisted selections, existing downloads, missing metadata, and actual grabs
- Distinguishes no enabled indexers, genuine empty searches, and partial/complete indexer failures
- Persists one bounded snapshot per item (up to 50 details), keeping the evaluated input version separate from completion time so settings changes during a search remain visibly stale

### Metadata Refresh

- Background worker checks every 6 hours (configurable) for new seasons/episodes
- Targets monitored series that are not "Ended" or "Canceled"
- Creates episode records and season monitors when new content is discovered
- Recalculates media item status after updates
- Resolves orphan downloads: backfills episode IDs on downloads created before the episode existed in the database

### Subtitles

- Search and download subtitles from OpenSubtitles.com
- Multi-language support with priority ordering
- Automatic subtitle search after media import (configurable)
- Metadata: language, hearing impaired flag, foreign parts only, trust score, hash match
- Per-item subtitle management (list, download, delete)

### Watched Tracking

- Mark/unmark media as watched with seen badges across the UI
- Mode: global (shared) or per-user
- Stores exact provider, media type, and external ID identity so movie/series numeric IDs cannot collide
- Watched activity is shared in global mode and actor-only in per-user mode

### Notifications

- Discord webhook notifications on import events
- Terminal download/import failure notifications through the same webhook, without retry spam or alerts for intentional deletion
- Failure messages use safe summaries rather than raw errors, credentials, or private paths; mention parsing is disabled
- Successful imports use rich embeds with media title, year, type, and poster image; failure messages use a compact title, safe reason, and download ID
- Connection testing from the UI

### Plex Integration

- Automatic library refresh after download import
- Auto-matches MediaGate libraries to Plex sections by type and path
- Manual section mapping override per library
- Retry with exponential backoff on transient Plex failures
- Manual per-library refresh trigger from the UI

### Self-Update

- Background worker checks GitHub Releases every 6 hours (configurable)
- In-process binary replacement (Linux)
- UI shows current version, update availability, release notes, published date
- Manual check and apply from settings

### Real-Time UI (Event Bus + SSE)

- Internal typed event bus with Server-Sent Events push to frontends
- Covers full lifecycle: downloads, imports, library sync, matching, monitoring, media activity, subtitles, updates
- SSE authentication via single-use 30-second tickets

### Multi-User Auth & Security

- Multi-user support with registration, login, profile management
- **Admin/user role system** — first user is admin; admin-only access to settings, libraries, indexers, profiles, workers, updates, Plex, and user management
- JWT access tokens (15min) + refresh tokens (24h / 30d with "remember me")
- HTTP-only cookies, optional secure flag for TLS reverse proxies
- Password hashing (bcrypt), refresh token hashing (SHA-256)
- AES-256-GCM at-rest encryption for sensitive settings
- Path traversal protection at three enforcement points
- XSS sanitization on user-facing HTML content (indexer descriptions)
- 6-step browser-based onboarding wizard on first launch

### Administration

- Admin role required for all management operations (enforced via centralized middleware)
- Database export endpoint for backup and debugging (full SQLite dump via API, admin-only)
- Self-delete prevention (users cannot delete themselves)

### Observability

- Optional OpenTelemetry distributed tracing and log export (OTLP HTTP)
- slog records tee'd to both stdout and OTLP backend with independent log level control
- Hot-swappable TracerProvider and LoggerProvider (noop when disabled)
- Automatic HTTP span propagation via shared instrumented client
- Configurable from the UI: enable/disable, endpoint, service name, log level

## Integrations

| Service | Purpose |
|---------|---------|
| **TMDB** | Movie/series metadata, posters, trending/popular feeds |
| **TVDB** | Series metadata, episode data |
| **qBittorrent** | Torrent downloading, progress tracking, cleanup |
| **Prowlarr/Indexers** | 700+ torrent indexer definitions (Cardigann YAML) |
| **FlareSolverr** | Cloudflare challenge bypass for protected indexer sites |
| **Plex** | Automatic library refresh after import (section-level scan trigger) |
| **Discord** | Webhook notifications for successful imports and terminal download/import failures |
| **OpenSubtitles.com** | Subtitle search and download |
| **GitHub Releases** | Self-update checking and binary replacement |

## Background Workers

| Worker | Default Interval | Purpose |
|--------|-----------------|---------|
| Monitor | 15 min | Auto-search and grab for monitored media |
| Download | 5 sec | Send pending torrents, poll active downloads |
| Importer | 10 sec | Hardlink completed downloads, cleanup after seeding |
| Metadata Refresh | 6 hours | Check for new seasons/episodes |
| Update Check | 6 hours | Check GitHub for new releases |
| Indexer Definitions | 24 hours | Refresh Prowlarr definitions from GitHub |

The UI includes a dedicated **Workers panel** with real-time SSE-driven status for each worker (last run, next run, current state) and manual trigger buttons to run any worker on demand.

## Tech stack

| Layer | Tech |
|-------|------|
| Backend | Go 1.22+, stdlib `net/http`, GORM, `log/slog`, koanf |
| Frontend | Vue 3 + TypeScript (Composition API), Tailwind CSS v4, Vue Router, Lucide icons |
| Database | SQLite via pure-Go driver (`glebarez/sqlite`) — no CGO needed |
| API contract | OpenAPI spec &rarr; `oapi-codegen` (Go) + `openapi-typescript` (TS) |
| Build | Makefile + Docker multi-stage for cross-compilation |

## Quick start

### Development

```bash
make tools      # install air + oapi-codegen
make dev        # Air (Go hot-reload) + Vite (frontend HMR) in parallel
make harness-up # isolated Air + Vite instance with safe fake integrations
```

Disposable instances keep their own DB, cache, filesystem, ports, credentials,
and backend/frontend logs under `tmp/harness/`. Run the full fake
tracker-to-import smoke flow with `make harness-smoke`, then remove the instance
with `make harness-destroy`. `make harness-ci` runs the same deterministic flow
against the built single binary for release pipelines. See
[`docs/HARNESS.md`](docs/HARNESS.md).

For non-harness Vite sessions, `VITE_API_PROXY_TARGET` overrides the default
`http://localhost:8080` backend proxy target.

Run backend tests without cached results and use Node.js 24 for the frontend regression tests (native TypeScript imports and module hooks):

```bash
(cd backend && go test -count=1 ./...)
(cd frontend && npm test)
scripts/lint-all.sh   # run from the repository root; check both linter outputs
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
| `API_HOST` | empty | HTTP bind host; empty listens on all interfaces |
| `DATA_DIR` | `.` | Runtime cache root |
| `DB_PATH` | `media-gate.db` | SQLite database path |
| `LIBRARY_BASEPATH` | `/mnt` | Root path for library directories |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `BROWSER_OPEN` | `true` | Open the UI automatically where supported |
| `MEDIAGATE_ENV_FILE` | `.env` | Alternate dotenv path; set empty to disable dotenv loading |
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
├── harness/             # Disposable local/CI instance runner and smoke flow
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
- **Independent activity history** — append-only per-media facts are committed with domain mutations; current state and cumulative requester attribution remain separate sources of truth
- **External-effect boundaries** — persisted claims/requests authorize network and filesystem work outside SQLite transactions, with observed outcomes recorded separately
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
