# Media Gate

Self-hosted media management app: Go backend and Vue 3/TypeScript frontend, shipped as one binary with embedded frontend assets.

## Working Agreements

- This file applies repository-wide. Read any nested `AGENTS.md` before editing its subtree.
- Inspect the relevant implementation and tests first. Make the smallest correct change; preserve unrelated work and avoid speculative abstractions or compatibility layers.
- Treat source, tests, migrations, and build configuration as evidence of current behavior. Older ADRs, roadmap entries, and recalled memories may be superseded; flag conflicts rather than restoring obsolete behavior.
- Do not commit, push, tag, deploy, or modify production without an explicit request. Report changes, checks actually run, and remaining gaps when finishing.

## Setup And Commands

Use the Go version in [backend/go.mod](backend/go.mod) and Node.js 24 for frontend regression tests (native TypeScript imports/module hooks). Run each command in the directory shown.

For a clean checkout, run these **in order**: `make tools` at the root, `npm ci` in `frontend/`, then `make build` at the root. Ensure Go-installed tools are on `PATH`. Generation runs before the build's npm install; generated API files and embedded frontend assets are gitignored. `make tools` does not install `golangci-lint`.

| Directory | Command | Purpose |
| --- | --- | --- |
| Root | `make tools` | Install Air and the pinned oapi-codegen |
| Root | `make generate` | Generate Go and TypeScript API code |
| Root | `make build` | Generate, build/embed frontend, compile binary |
| Root | `make dev` | Air + Vite using local configuration; prefer the harness for isolated testing |
| Root | `make frontend` | Install/build frontend and copy assets to the backend embed directory |
| Root | `scripts/lint-all.sh` | Run backend and frontend lint; inspect both outputs |
| `backend/` | `go test -count=1 ./...` | Full backend suite; **always use `-count=1`, including focused runs** |
| `backend/` | `go test -count=1 ./internal/crypto/...` | Example focused package test |
| `backend/` | `golangci-lint run ./...` | Backend lint |
| `frontend/` | `npm test` | Frontend regression tests |
| `frontend/` | `npm run type-check` | Vue/TypeScript checks |
| `frontend/` | `npm run lint` | Biome checks; `npm run lint:fix` applies fixes |
| `frontend/` | `npm run build` | Type-check and Vite production build |

Command sources: [Makefile](Makefile), [frontend/package.json](frontend/package.json). Configuration defaults to `.env` in the process working directory; `MEDIAGATE_ENV_FILE` overrides it (empty disables dotenv). See [backend/.env.example](backend/.env.example) and [config.go](backend/internal/config/config.go). A secret key is required: `SECRET_KEY` in dotenv or `MEDIAGATE_SECRET_KEY` in the environment.

## Verification

- Add or update regression tests for changed behavior. Start with focused tests, then run affected backend/frontend suites and lint. Run `make build` for API or embedded-asset integration changes. Never rely on cached Go test results.
- Use the disposable harness for UI/API interaction, auth, SSE, migrations, workers, imports, integration protocols, or multi-service changes. It complements focused regression tests; its smoke flow does not cover every feature or replace browser checks.
- For visible UI changes, check the affected flow at desktop and mobile sizes. Preserve the existing Vue/Tailwind design language.
- For documentation-only changes, verify commands/references and run `git diff --check`; runtime suites are unnecessary unless behavior changed. Report blocked or skipped checks, not assumed passes. Inspect [.github/workflows](.github/workflows) before assuming CI enforces a check.

### Disposable Harness

Read [docs/HARNESS.md](docs/HARNESS.md) before use. It requires Linux. Use a task-specific `HARNESS_ID=<name>` consistently to avoid collisions with other sessions.

| Root Command | Purpose |
| --- | --- |
| `make harness-up HARNESS_ID=<name>` | Start isolated Air/Vite and fake services |
| `make harness-status HARNESS_ID=<name>` | Show instance status and URLs |
| `make harness-logs HARNESS_ID=<name> SERVICE=backend` | Read logs; add `FOLLOW=1` to follow |
| `make harness-smoke HARNESS_ID=<name>` | Search-to-import smoke; run on a fresh instance |
| `make harness-down HARNESS_ID=<name>` | Stop the instance, retaining artifacts |
| `make harness-destroy HARNESS_ID=<name>` | Remove that disposable instance and its data |
| `make harness-ci` | Built-binary smoke with a unique disposable instance |

URLs and disposable credentials are in `tmp/harness/<name>/manifest.json`; logs are in `tmp/harness/<name>/logs/`. Stop instances you start and retain failure artifacts for diagnosis.

**Download tests must use both the fake tracker and fake qBittorrent.** Fetching a real `.torrent` may count as a snatch before qBittorrent is called. Live provider checks require explicit opt-in and remain separate from deterministic gates. Do not use production databases, libraries, or integrations for routine verification.

## Architecture And Contracts

- `backend/cmd/server/main.go` wires services. HTTP adapters live in `backend/internal/api/v1/handlers_*.go`; prefer business logic in the corresponding service package rather than expanding handlers.
- `frontend/src/` contains the Vue SPA (Vite, Tailwind v4, Lucide; `@` aliases `src`). `make frontend` copies its build to `backend/frontend/dist/` for `go:embed`.
- **OpenAPI is authoritative for generated endpoints:** edit [api/openapi.yaml](api/openapi.yaml), then run `make generate`. Never hand-edit `backend/internal/api/v1/gen.go` or `frontend/src/api/schema.d.ts`. Auth cookie handlers and SSE also have manual contracts; inspect both ends when changing them.
- All service database access goes through [store.Store](backend/internal/store/store.go); GORM stays inside `backend/internal/store/sqlite/`. Models and query projections belong in `store/`. Use the transaction-bound Store inside `WithTx`.
- Reuse existing `qbittorrent.Provider`, `plex.Provider`, `matching.Service`, `worker.Loop`, `worker.Registry`, injected HTTP clients, and `telemetry.Manager`; do not create parallel caches, clients, or worker infrastructure without need.
- Keep Biome's `noUnusedImports` disabled for Vue SFCs in [frontend/biome.json](frontend/biome.json): it cannot see template-only imports. Do not re-enable it without a verified template-aware solution.

### Schema Changes

- Use paired `NNNN_name.up.sql` / `NNNN_name.down.sql` files under `backend/internal/store/sqlite/migrations/`. Update model fields, `latestMigrationVersion` in [migrator.go](backend/internal/store/sqlite/migrator.go), and version-specific test expectations. Do not reintroduce GORM AutoMigrate.
- Preserve pure-Go SQLite and the custom migration driver over the existing connection. Neither a CGO driver nor a second driver registering the same `sqlite` name is acceptable.
- Define constraints in SQL migrations, not just GORM tags. Preserve FK enforcement and existing `CASCADE`/`SET NULL` behavior; retain intentional cleanup for legacy FK-less tables in `cleanup.go`.
- Verify fresh install, legacy adoption/data preservation, and relevant upgrade/downgrade tests in [migrator_test.go](backend/internal/store/sqlite/migrator_test.go). Rewinding a fixture's version also requires removing later schema. Schema changes and the clean version marker must commit atomically.

## Persistence Invariants

- `UpdateDownload` is update-only, matching the supplied `UpdatedAt`; stale/deleted snapshots return `ErrNotFound`. Do not resurrect deleted records with an upsert or overwrite newer state from a stale worker.
- Persist before publishing lifecycle events. A committed import/seeding completion also authorizes best-effort torrent cleanup; do not insert a second persistence gate after completion.
- Keep network/filesystem I/O outside DB transactions. Commit domain mutations and their required activity together; eventbus/SSE is a post-commit hint, not authoritative history.
- Whole-item deletion atomically claims `deletion_pending`, disables monitoring, clears overrides, cancels downloads, and records preparation before external cleanup. Preserve final claim checks that block new requests, grabs, files, and other side effects.
- Auto-grab's fresh version/scope checks, URL dedup/blocklist checks, insert, and queued activity share a transaction. Reuse `store.ActiveDownloadStatuses` and `(mediaItemID, downloadURL)` deduplication for manual and automatic downloads.
- Once a series download is linked and seeding/completed, actual files determine monitor coverage; keep release URL deduplication intact. Recheck wanted-episode file presence in the final grab transaction.
- Same-provider re-matches retain episode IDs by natural season/episode key and preserve download CAS versions. A different source/external ID replaces the catalog; do not infer equivalence across providers.

## Security Boundaries

- Preserve separator-aware path containment checks after cleaning paths: library/settings paths against the configured library base, importer paths against the relevant download/release roots. Do not replace them with a naive string-prefix check.
- Sensitive settings use `Sensitive=true` and encryption/decryption in `settings.Service`; indexer secret keys follow `indexer:{id}:{fieldName}`. Never expose secrets, tokens, or raw provider payloads in logs, activity, fixtures, or memory.
- Access tokens are returned as JSON and sent as Bearer tokens; refresh tokens are hashed in storage and transported in HTTP-only cookies. Preserve single-use SSE tickets. Add new privileged operationIDs to `adminOnlyOps` in [middleware_admin.go](backend/internal/api/v1/middleware_admin.go); manual handlers need explicit checks. UI visibility is not authorization.
- Preserve the allowlist sanitizer in [frontend/src/utils/sanitize.ts](frontend/src/utils/sanitize.ts). Activity payloads remain safe, typed, and bounded; actor-only rows must not become public after user deletion. Discord failure messages use safe reasons, disable mentions, and exclude raw `LastError`.

## Task-Specific References

Before changing a domain below, read its current code/tests and the relevant section of [docs/DECISIONS.md](docs/DECISIONS.md). Search by ADR number rather than loading the whole file; later superseding decisions take precedence. [docs/ROADMAP.md](docs/ROADMAP.md) is feature history/planning, not an implementation contract.

| Area | Preserve / Read |
| --- | --- |
| Downloads, imports, notifications | ADR-134, ADR-138, ADR-142. History uses a growing newest-first prefix, filtering before limiting, not mutable offsets. Preserve `DownloadedAt` through retries; do not backfill unknown legacy times. Notify only persisted terminal failures, not retries/cancellation. Episode-ID backfill skips `importing` and retries on later refreshes. |
| Download paths and episode targeting | `qbit_download_path` is the local mount; `qbit_save_path` is the optional remote override. ADR-118/119/144: NULL `episode_id` does not prove a season pack; unnumbered specials are ambiguous. Preserve episode-id > episode-key > season > item status resolution. Explicit import fallback is single-video only; resync retains known episode keys unless parsing supplies conflicting information. |
| Monitoring and metadata | ADR-120, ADR-133, ADR-137, ADR-144. Episode override > season > false, keyed by item/season/episode numbers, not metadata episode IDs. Compare freshness using server `InputUpdatedAt`, not completion/receipt time. `SetMonitorSearchStartedAt` must not bump `UpdatedAt` or overwrite settings. Check current `matching`/`metarefresh` tests for refresh/backfill behavior. |
| Discover and timeline | ADR-135/136. Identity is `(source, mediaType, externalId)`; never guess cross-provider aliases by title. Preserve bounded page/date windows, abort/stale-response guards, and scroll position. Empty timeline windows are not an end marker; initial/Today views center today. |
| Requesters | ADR-140. Request intent is separate from mutable monitoring; repeat requests are additive/idempotent, later attribution uses effective false-to-true transitions. Display cumulative requesters, not historical scopes as current monitoring. |
| Activity and watched state | ADR-141/142/143. Append-only history, cumulative requesters, live state, and latest diagnostics remain independent. Watched identity includes media type; enforce shared/private visibility before pagination. Details is the default tab; fetch each Activity disclosure only when active and expanded, preserving cache while rejecting late responses. |
| Indexers and release selection | ADR-074, ADR-112, ADR-121, ADR-123. Keep `SanitizeYAML` before parsing provider definitions. Untagged releases fall back to English only when no language was detected; `multi` retains priority. Preferred release is a soft preference after profile ranking. |
| Migration design | ADR-125 and ADR-142; retain the custom driver, adoption boundary, and transactional version tracking. |

## Maintaining This File

Keep durable, repository-wide rules and verified commands here. Put feature history and detailed rationale in the relevant docs/tests and link them above. Update affected instructions with behavior changes; avoid duplicating package inventories, migration counts, local-only slash commands, or machine-specific production access details.

<!-- icm:start -->
## Persistent Memory (ICM)

ICM use is required when available. Recall before starting work:

```bash
icm recall "media-gate <task keywords>"
```

Store durable findings when an error is resolved, a design decision is made, a user preference is discovered, or significant work is completed. For long tasks, store a progress summary after roughly 20 tool calls without a store. Do this before the final response.

```bash
icm store -t context-media-gate -c "<verified outcome and relevant context>" -i high
```

Use `errors-resolved` for fixes, `decisions-media-gate` for decisions, and `preferences` with `-i critical` for user preferences. Update an existing memory with `icm update <id> -c "<correction>"` when superseded. Do not store secrets, routine logs/git status, or copies of repository guidance. Memories are leads to verify, not authority over current code or user instructions. If ICM is unavailable, report the limitation and continue without claiming recall/storage succeeded.
<!-- icm:end -->
