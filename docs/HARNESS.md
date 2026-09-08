# Disposable Test Harness

The harness starts an isolated Media Gate instance for manual development checks
or deterministic release-gate tests. Each instance owns its database, runtime
cache, library, download directory, ports, processes, credentials, and logs.

## Safety Model

The default harness never inherits `backend/.env`. It configures only a local
stateful tracker and a local qBittorrent-compatible HTTP server. The fake server
accepts the production qBittorrent client protocol but never joins a torrent
network, and it refuses to create or remove payloads outside the instance's
download root.

TMDB and TVDB remain opt-in live, read-only dependencies. Pass their keys
explicitly as `HARNESS_TMDB_APIKEY` and `HARNESS_TVDB_APIKEY`; they are not
written to the manifest. Plex, Discord, OpenSubtitles, FlareSolverr, and real
trackers are not configured.

Do not configure a real tracker in an instance used for download tests. Media
Gate fetches the `.torrent` from the tracker before submitting it to
qBittorrent, so a fake qBittorrent alone does not prevent a private-tracker
snatch.

## Local Usage

Local mode runs Air and Vite with hot reload. It currently targets Linux and
requires Go, Node.js, `curl`, util-linux `setsid`/`flock`, coreutils `timeout`,
Linux pidfds, and Air; install project dependencies first with `make tools` and
`npm ci` in `frontend/`.

Backend, Vite, and fake-integration ports are assigned by the kernel on every
startup. A host-global, per-user startup lock serializes allocation and service
binding across harness roots. Fixed `HARNESS_API_PORT`, `HARNESS_UI_PORT`,
and `HARNESS_FAKE_PORT` overrides remain available for debugging and fail rather
than falling through to a different port when occupied.

```bash
make harness-up
make harness-status
make harness-logs SERVICE=backend
make harness-smoke
make harness-down
make harness-destroy
```

`harness-down` preserves the database, filesystem, manifest, and logs for
inspection. `harness-destroy` removes the instance completely.

Use `HARNESS_ID` for concurrent instances:

```bash
make harness-up HARNESS_ID=download-fix
make harness-logs HARNESS_ID=download-fix SERVICE=all FOLLOW=1
make harness-destroy HARNESS_ID=download-fix
```

The generated `tmp/harness/<id>/manifest.json` contains the assigned ports and
URLs, disposable login credentials, paths, PIDs, and seeded entity IDs. Vite is
started with that frontend port and proxies `/api` to the assigned backend URL;
the seeded qBittorrent settings use the assigned fake-server URL. Logs are
available directly in `tmp/harness/<id>/logs/`:

- `backend.log`: Air output and JSON backend logs
- `frontend.log`: Vite output
- `fakes.log`: fake tracker/qBittorrent process output

Startup runs the backend's embedded schema migrations as a foreground,
migration-only step against the instance database before launching any service.
A migration error aborts startup immediately and leaves the instance logs and
filesystem available for inspection.

Each long-running service has its own session and a Linux child-subreaper that
stays alive until every process in that session has exited and been reaped.
Startup and CI commands trap interruption and failure and stop every started
session. `harness-down` and `harness-destroy` validate the recorded PID identity
and session before signalling through pidfds, including Air child process
groups, Vite, Node.js, and esbuild. Pidfds prevent a concurrently reused PID
from redirecting a teardown signal to another instance or host process.

The initial library sync queues normal metadata matching. Without an explicit
TMDB/TVDB key, that background match job reports that no API key is configured;
the seeded disk item and deterministic harness flow remain available.

## Fake Controls

New torrents start in qBittorrent's downloading state. They can be advanced or
failed without any external I/O:

```bash
make harness-complete
make harness-error
make harness-reset-fakes
```

The fake control state is also available at the `fakeUrl` from the manifest:

```text
GET  /_harness/health
GET  /_harness/state
POST /_harness/complete
POST /_harness/error
POST /_harness/reset
```

`make harness-smoke` verifies the frontend endpoint, login, fake tracker search,
torrent fetch and submission, completion, import, and resulting library file.
Run it on a fresh instance because it intentionally creates a download.

## CI And Release Gates

CI mode builds and runs the production-style single binary instead of Air and
Vite:

```bash
make harness-ci
```

The command creates a unique instance, runs the deterministic smoke flow, and
destroys it on success. On failure it stops all processes but retains the
instance directory so logs and filesystem output can be uploaded as CI
artifacts. Set `HARNESS_BINARY=/path/to/media-gate` when the pipeline already
built the binary.

A release pipeline should keep deterministic and live checks separate:

```text
backend tests with -count=1
frontend type-check, tests, and lint
production build
make harness-ci (or HARNESS_BINARY=... harness/harness.sh ci)
release packaging
```

Only deterministic fake-backed tests should block a release. Checks against
TMDB, TVDB, or real trackers should run manually or on a schedule because rate
limits and provider outages are not Media Gate regressions.

Use the harness when a change affects UI/API interaction, authentication, SSE,
database migrations, worker lifecycle, filesystem imports, integration
protocols, or multi-service behavior. Unit tests remain the faster and more
useful signal for isolated parsers, helpers, and pure service logic.
