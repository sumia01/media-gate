# Media Activity Log

Status: Initial scope implemented. Optional automatic lifecycle coverage remains
deferred as described below.

## Implementation status

The initial activity-log scope is implemented in migration `0010` and the media
details UI. The implementation preserves the two-tab layout in this document;
the earlier inline-panel prototype is not part of the product.

- `media_activity` is append-only application history with media deletion
  cascade, deleted-user anonymization, shared/private visibility, bounded typed
  payloads, and stable ID cursor pagination. No synthetic legacy events are
  created.
- Requests, direct monitoring/settings edits, manual and automatic queued grabs,
  manual download status/removal, matching/unmatching, semantic metadata changes,
  future-season policy, explicit and library resync effects, media-removal
  preparation, manual subtitle actions, and global/per-user watched mutations
  record activity at their committed domain boundary.
- A persisted deletion claim prevents new requests, grabs, matches, resync
  applies, imports, posters, or subtitle writes after media cleanup begins,
  without holding a SQLite transaction across external I/O.
- Details remains the default operational tab. Activity contains the lazy saved
  monitor decision and activity feed, both collapsed initially. Cached data and
  disclosure state survive tab switches; hidden requests are aborted and guarded
  against late responses.
- Automatic payload/import completion and failure history and automatic subtitle
  results remain optional follow-up coverage. Metadata refresh diffs intentionally
  remain limited to status, season count, and episode additions.

## Goal and agreed boundaries

Provide a chronological, read-only history on media details: who performed an
action, what happened, which scope it affected, and when it was recorded.

- Keep the cumulative **Requested by** summary unchanged.
- Split media details into **Details** and **Activity** tabs. Details remains
  the default and keeps the operational controls; Activity contains the latest
  auto-download check's toggleable results and the activity log.
- Start both diagnostic/history panels collapsed. Fetch only when Activity is
  selected and the corresponding panel is expanded, never for a hidden tab.
- Include user actions affecting this media item, automatic grabs, and metadata
  refreshes only when they persist a semantic change.
- The user's example of "request canceled" means the existing season/episode
  unmonitor operation. Call it **Monitoring disabled**, not **Request canceled**.
  Do not introduce per-user request withdrawal or an active-request lifecycle.
- History belongs to the media item. Deleting the item deletes its history;
  a global audit log and deleted-media history are out of scope.
- Historical activity must never be used to calculate current monitoring,
  requester membership, download status, or other live state.

## Findings and model decision

**A separate append-only activity model is needed.** Preserve `media_requests`
as cumulative attribution, rather than converting it into an event stream.

| Existing data | Why it cannot serve as activity history |
| --- | --- |
| `media_requests` | Deduplicated by user and scope. Repeated requests and re-enables preserve the first timestamp. Disables are absent. Explicit requests and later enable attribution are indistinguishable. |
| Monitoring rows | Mutable configuration; episode overrides can be removed by broader changes. They do not retain actors or previous values. |
| Downloads | Mutable lifecycle rows that users can delete. They do not retain the initiating user. |
| Latest monitor decision | One overwritten diagnostic snapshot per item, not a history of actions. |
| Event bus/SSE | Best-effort, lossy, without replay or sufficient actor/scope context. Some operations publish multiple events; others publish none. |

Keep three independent sources of truth:

1. Current domain rows determine current state.
2. `media_requests` determines the cumulative requester summary.
3. `media_activity` records committed actions and observed outcomes.

Do not backfill guessed events from requester rows, `UpdatedAt`, or current
monitoring. Existing items initially have no recorded activity. Explain that
older activity was not recorded; do not imply nothing happened before rollout.
The cumulative-only requester display introduced by commit `1e5ab4f` remains
valid and should not be reverted.

## Activity representation

Add `media_activity` in the next migration, currently `0010`, with a Store-only
append/list API. No public edit/delete endpoint and no event replay into domain
state. Append-only describes normal application writes; media cascade and user
anonymization are explicit lifecycle exceptions.

| Field | Purpose |
| --- | --- |
| `id` | SQLite autoincrement sequence; stable identity and canonical recorded order. |
| `media_item_id` | Required FK, `ON DELETE CASCADE`. |
| `recorded_at` | Server UTC time assigned inside the mutation transaction. |
| `actor_kind` | `user` or `system`; never infer it from a nullable user ID. |
| `actor_user_id` | Nullable user FK, `ON DELETE SET NULL`. Required for a live user when appending a user action. |
| `actor_component` | Named executor for system actions, such as `monitor`, `metarefresh`, or `importer`. |
| `action` | Stable typed action, rendered into human wording by the frontend. |
| `operation_id` | Correlates multiple facts from one command or external operation. Not a requester-scope deduplication key. |
| `visibility` | `shared` for media/global watched actions; `actor_only` for per-user watched actions. |
| `media_title` | Safe event-time title snapshot, retained through rename/rematch. |
| `details_version`, `details` | Versioned, typed JSON containing event-time targets, changes, and safe related-object snapshots. |

Index `(media_item_id, id DESC)`. Validate actor and payload combinations before
insertion. Use typed Go payloads and corresponding OpenAPI schemas, not arbitrary
request bodies or serialized domain models. Unknown future action versions must
render a safe generic row rather than break the feed.

Scope is explicit in the payload: `media`, `whole_series`, `future_seasons`,
`season`, `episode`, or a collection of targets. File/download/subtitle actions
also identify the relevant object and its resolved media scope when known.
Use natural season/episode numbers, not only episode IDs, because rematching
can replace those IDs. A null download episode ID does not prove a season pack;
use the existing title-parsing rules and retain unknown scope when ambiguous.

Actor display follows the existing requester policy: resolve the current user
display name, and show **Deleted user** after deletion. Do not retain a name or
email snapshot that bypasses anonymization. System events show their component,
not an arbitrary requester. If known, an asynchronous result may reference its
initiating activity; the system remains the executor. Missing actor plumbing is
an implementation defect, not permission to label manual work as automatic.

Bound payloads, initially 32 KiB and 50 detailed targets/changes per collection.
Store exact totals plus `truncated` for longer collections; show "Showing 50 of
120 changed episodes," not a falsely complete list. Preserve action-level
summaries and counts even when details are truncated. Exclude raw URLs,
credentials, torrent hashes, filesystem paths, provider responses, and raw
errors. Use safe reason codes and bounded plain-text titles/release names.

## Action semantics and coverage

Prefer one entry per meaningful action, not one per database row or emitted
event. Bulk scope changes are one entry with a change list. A mixed settings
command can produce monitoring and profile entries sharing `operation_id`.

| Action | Display and details | Actor |
| --- | --- | --- |
| `request.made` | "Requested Season 2" or other normalized intent. Include whether media was added and which monitoring settings actually changed. | Requesting user |
| `monitoring.changed` | "Monitoring enabled", "Monitoring disabled", or a mixed/configuration-specific summary. Include scopes and before/after configuration and effects. | Acting user |
| `media.settings_changed` | Profile assignment and preferred-release changes, with old/new values and names. | Acting user |
| `download.queued` | "Manual grab queued" or "Automatic grab queued"; release, indexer, download ID, and target scope. Queued does not mean qBittorrent accepted it. | User or monitor |
| `download.status_changed` | Manual retry/cancel/status change, with old/new status. A user-set completed state is not proof of download completion. | Acting user |
| `download.removal_requested`, `download.removed` | Distinguish record removal, requested file removal, and actual cleanup results. Preserve safe download details after row deletion. | Acting user / system result |
| `media.match_changed`, `media.unmatched` | Old/new provider identity and affected metadata/override summary. Do not erase prior activity during rematch. | Acting user or automatic matcher |
| `metadata.changed` | "Media refreshed" with the actual semantic diff, e.g. status changed or three episodes added. No entry for an unchanged automatic check. | Metarefresh or explicit user operation |
| `monitoring.changed` with reason `future_season_policy` | "Enabled Season 3 monitoring under the future-season policy." Separate from metadata changes and historical user intent. | Metarefresh |
| `media.resync_requested`, `media.resync_completed` | Explicit filesystem resync and its actual added/updated/removed record counts. This is not provider metadata refresh. | Acting user / system result |
| `subtitle.downloaded`, `subtitle.removed` | Language, provider, actual target episode/file, and the observed manual action outcome. | Acting user |
| `watched.marked`, `watched.unmarked` | Watched-state mutation for the exact linked item. Shared in global mode; private in per-user mode. | Acting user |

Requests and state transitions are different facts. An accepted repeat request
can produce `request.made` even when all requested monitoring is already on;
its details say no monitoring changed. Re-requesting a previously disabled scope
records the new request and its actual enable effects, while the cumulative
requester row remains deduplicated. Do not add duplicate monitoring entries for
effects already included in the same request activity.

For setters, suppress true no-ops after comparing full configuration. For
explicit commands such as resync, record acceptance and a linked result even
if the result reports no changes. Do not create `metadata.changed` merely to
acknowledge a manual command. Each accepted request is recorded; existing HTTP
retries are not magically distinguishable from repeated user commands. If
transport-level idempotency is introduced, bind it to the whole command, not
just activity insertion.

The "manual actions" boundary is media-affecting commands and mutations, not
every click, page view, search, or GET. Account/settings/indexer/profile-definition
administration and worker/library-wide command history need a separate global
surface and are not expanded into fabricated per-media user actions. Actual
per-item effects of library matching or manually triggered workers are still
recorded as system effects. Only show a user initiator if it was propagated,
never inferred. A Plex scan request is not a confirmed scan completion.

Successful media deletion cascades the feed, as agreed. Its preparatory
transaction must record the removal request and committed monitoring disable /
download cancellation effects, attributed to the deleting user. If final
deletion fails, those changes and their history survive on the item. There is
no promise of a surviving "media deleted" entry. Download/subtitle removal does
not delete their media activity. Routine automatic search checks, unchanged
refreshes, polling, retry scheduling, episode-ID backfill, and derived status
churn are excluded from the default history.

Optional follow-up coverage, not required for the initial feature: automatic
payload completion, import completion, terminal download/import failures, and
automatic subtitle results. These fit the same model but introduce additional
lifecycle producers. A future `download.payload_completed` entry should retain
qBittorrent's completion timestamp and provenance; terminal failures use safe
reason codes and never log every retry. Direct manual retry/cancel/removal and
subtitle actions remain in the initial scope.

## Monitoring correctness

Build a bidirectional configuration diff from fresh before/after snapshots in
the same transaction. Existing requester attribution intentionally records only
effective enables and is not a sufficient activity diff.

- Capture the parent gate, future-season policy, season settings, and episode
  override states. An absent override means inheritance, not explicit false.
- Calculate effective episode changes using the parent gate and the existing
  episode override > season > false hierarchy.
- Enabling the parent is **Enabled media monitoring**, not **Enabled the whole
  series**. Saved season settings may reactivate independently of requested scope.
- Editing a child while the parent is off is a configuration change. Say
  **Set Season 2 monitoring to on; media monitoring is off**, not that episodes
  are now being actively monitored.
- Setting a season to the same value can still remove episode overrides.
  Record that change and its effects; do not suppress it as a boolean no-op.
- Disabling the parent clears episode overrides but retains season settings.
  Describe both the gate change and override removal without claiming every
  stored setting was disabled.
- A bulk PATCH can clear overrides and recreate them. Compare the committed
  final configuration, not intermediate writes as independent actions.
- Compact scopes only when the event-time evidence proves the whole scope was
  affected. Do not expand old whole-series events using today's episode catalog.
- Automatic future-season enablement must use the freshly read parent policy
  and record only season settings actually added/enabled.

These rules apply to request effects, direct monitoring edits, and unmatch or
other operations that remove overrides. Current controls continue to read the
existing domain API, never these deltas.

## Metadata refresh correctness

Today `RefreshSeriesMetadata` updates status/season count and adds missing
episodes. It does not correct every field on existing episodes or refresh all
movie/series metadata. Keep that behavior boundary explicit; broadening refresh
coverage is a separate change, not a prerequisite for truthful activity.

The current `changed` boolean/event is insufficient: episode inserts happen
before the metadata write, errors can be continued, and the pre-fetch snapshot
can become stale. Refactor this boundary before adding authoritative history:

1. Capture the provider identity and metadata ID/version, then fetch candidates
   outside a database transaction. Track successful and failed provider windows.
2. In a short transaction, reload the parent, metadata, episodes, and policy.
   Reject/retry stale candidates if metadata identity or `UpdatedAt` changed
   since fetching began, or if the item was unmatched/deleted.
3. Compare normalized semantic values against the persisted preimage. Exclude
   database IDs, `MatchedAt`, `UpdatedAt`, and equivalent serialization changes.
4. Apply valid fetched changes and append `metadata.changed` atomically only
   when the semantic diff is nonempty. Database write failures roll back both.
5. If some provider windows failed, successful windows may still be applied,
   but the event marks partial coverage. Missing results never imply removals.
6. Apply future-season policy changes from fresh settings and record their own
   monitoring activity in the transaction. Publish invalidation after commit.

Initial diff coverage is status, season count, and episode additions. Manual
matching can record provider-identity and other metadata changes that it
actually persists. Its current destructive pre-fetch deletion and ignored
episode errors also need a fetch-first, atomic replacement boundary. Carry
episode-fetch completeness into that boundary: reject an incomplete replacement
and retain the existing match/catalog, rather than turn failed season fetches
into removals. Never merge episodes across different provider identities. An
unchanged same-identity refresh is not a new metadata-change entry. Do not
couple matching I/O or poster fetching to the database transaction.

## Write path and external outcomes

Append at the domain mutation, through `Store`, not from an eventbus subscriber.
Database-only mutations and their activity succeed or roll back together.
Capture/validate the authenticated actor for every user mutation, including
disables and no-ops, independently of whether requester attribution is inserted.
Handlers remain thin; shared diff/event construction belongs in service code.

First integration points are the existing transactions for new/existing requests,
item PATCH, season PUT, episode PUT, and automatic download insertion. Manual
download insertion needs the same atomic activity boundary. Auto-grab emits both
`DownloadCreated` and `MonitorGrabbed` today; persist just one queued activity.

For manual download state changes, wrap the existing optimistic `UpdateDownload`
and the activity append in one transaction. A stale/deleted snapshot creates no
event. If automatic lifecycle history is added later, its successful commit
must remain the sole authorization for lifecycle publication and import cleanup.
Do not add a later activity write as another cleanup gate.

Resync also needs a scan/apply split: inspect the filesystem outside the
transaction, then reload/validate the item's current identity and file records,
apply the database delta, and append the result in one short transaction. Its
current incremental writes cannot provide an atomic final result simply by
appending an event afterward. Count actual committed record changes, not scanned
paths, and retain the existing catalog if the candidate scan is stale.

Filesystem and external-client operations cannot be atomic with SQLite. Use a
persisted request/cancellation before I/O where appropriate, followed by a
separate observed-result event with the same operation ID. Report successful
database deletion separately from failed/unknown file cleanup. Never claim that
a requested external effect occurred just because the HTTP request returned.
An interrupted operation may have no known result; do not synthesize success
after restart without evidence. This is an activity feature, not a guarantee
of exactly-once external execution.

Before any optional sent-to-client history is added, fix the existing
`sendPending` publication after failed status persistence. It is not currently
an authoritative fact. For initial removal coverage, file cleanup helpers must
return actual outcomes before the log can claim files were removed.

After commit, publish a small `media.activity_added` invalidation containing
only the item ID for shared entries. It is a hint; missing SSE must not lose
history. Do not broadcast private watched activity over the current global
SSE stream. Invalidate it locally or through an audience-aware mechanism.

For watched entries, preserve the existing global versus per-user modes and
their intended authorization semantics. The actor is the authenticated person
making the change, not necessarily the creator of the old global watched row.
Check ownership for per-user deletion; do not impose creator-only deletion on
global watched state. Snapshot the visibility mode at the action: switching to
global later must not expose earlier private history. Resolve the exact linked
item using `(source, mediaType, externalId)` when necessary, without title or
cross-provider guesses. Filter `actor_only` entries in SQL before limiting;
deleted actors never turn private history into shared history. Watched titles
outside the library have no per-media feed.

## Read API and lazy UI

Proposed endpoint: `GET /api/v1/media/{id}/activity?limit=30&before=<cursor>`.
Define `/media/{id}/activity` in `api/openapi.yaml` under the existing server
base, then run `make generate`; do not hand-edit generated Go or TypeScript files.

- Return `items`, `hasMore`, and an optional `nextCursor`.
- Default to 30 entries, enforce a maximum of 100, and query `limit + 1`.
- Order by immutable `id DESC`: newest recorded activity first. Display
  `recorded_at`; an external occurrence time such as qBittorrent completion
  belongs in labeled details and does not reorder previously loaded history.
- Encode a version, media ID, and exclusive last-seen ID in an opaque cursor.
  Validate it and reject malformed/cross-item cursors. New inserts do not shift
  older pages; no mutable offsets or growing-prefix refetches are needed.
- Authenticate and enforce visibility before pagination. Return 404 for a
  missing item and 200 with an empty list for an item with no activity.
- Keep activity out of the existing media-detail payload. Do not add an eager
  count query or load it merely to render the tab or collapsed header.

Use two tabs on media details, for both movies and series, with a compact media
identity/back-navigation header shared across them:

- **Details** is the default tab. Keep the existing media information, poster,
  cumulative **Requested by** summary, monitoring/settings controls, manual
  actions, episodes, downloads, subtitles, and files. Remove only the latest
  auto-download check block from this content.
- **Activity** contains **Latest auto-download check** followed by **Activity
  log**, each as a full-width collapsible section. Keep this tab read-only:
  do not move monitoring toggles, profile/settings controls, manual searches,
  worker triggers, or other operational actions here.

The check block contains only the disclosure and saved diagnostic content,
including its summary, timestamps, freshness warnings, and evaluated list.
Omit its standalone **Refresh saved check** toolbar. Expanding/reopening the
check list reloads the saved snapshot; after a failed load, instruct the user
to close and reopen it to retry. This remains a read-only GET, not an automatic
search trigger. History pagination, refresh, and retry controls remain within
the activity log; they navigate recorded data, not media operations.

Match the existing violet-underlined tab styling. Default to Details on a new
media route; ordinary item refreshes and SSE updates must not change the active
tab. Preserve both panels' expansion and successfully loaded data across tab
switches, but abort outstanding requests when leaving Activity. On returning,
reload an expanded saved-check list and load an expanded history only if it is
missing or dirty. Collapsed sections remain unfetched.

The existing latest-check panel fetches immediately and on monitor completion
even while collapsed. Merely hiding it with a tab does not make it lazy: gate
its request path on both active-tab and disclosure visibility. While hidden,
worker events only mark it dirty; while visible and expanded, they may refresh
its saved result. Preserve server-version freshness comparisons and stale-data
warnings. Before the first successful fetch, use a neutral collapsed label,
not a claim that no check has been recorded. Do not apply this diagnostic
auto-refresh behavior to the append-only feed, which retains explicit refresh
to avoid moving history while the user reads.

Each row shows a distinct action label, actor, target scope, and timestamp, with
the event-time title/release/diff in details. Use past-tense wording and a
visible note: **Historical actions do not necessarily reflect current monitoring.**
For example, displayed newest first:

```text
14:32  Metadata refresh  Media refreshed
       Example Show: added S02E08 and S02E09
14:20  Automatic search  Automatic grab queued
       Example Show S02E07: Example.Show.S02E07.1080p.WEB
14:05  Bob               Monitoring disabled
       Example Show S02E04: on -> off
14:00  Alice             Request made
       Example Show Season 2; monitoring enabled for the affected scope
```

Use a dedicated `MediaActivityPanel` and a focused loading helper/composable
where needed for testing. The activity log's loading states are explicit:

| State/action | Behavior |
| --- | --- |
| Details tab active | No check/history HTTP requests, including during SSE updates. |
| Activity selected, panels collapsed | No check/history HTTP requests just to show the tab. |
| First expansion on Activity | Fetch newest history page; show loading, not an empty result. |
| Successful empty result | "No recorded activity yet. Earlier actions were not recorded." |
| Load older | Explicit button; append stable-ID-deduplicated rows using the cursor. |
| Initial/page error | Retry the failed request; preserve previously loaded rows/cursor. |
| Collapse | Abort pending requests; retain successful pages; no background fetching. |
| Leave Activity tab | Abort pending requests and retain successful pages, cursors, and disclosure state. |
| Return to Activity tab | For expanded history, load if never loaded or refresh if dirty; otherwise reuse cached pages. Collapsed history stays unfetched. |
| Event/local mutation | Mark the feed dirty. Only while Activity is selected and the log expanded, offer "Activity may have changed. Refresh latest." Do not unexpectedly prepend rows. |
| Reopen dirty feed | Refresh the newest page, replacing old pages only after success; clearly mark retained data stale on failure. |
| Explicit refresh | Start a new pagination session; keep old rows until success. |
| SSE reconnect/window focus | Mark cached history dirty without fetching while collapsed or on Details. History refresh remains available because hints can be missed. |
| Route change/unmount | Abort, clear state, invalidate request generation, remove listeners. |

Use both abort signals and request-generation guards so late responses cannot
cross item boundaries, update a hidden panel after tab exit, or overwrite a
newer page/refresh. Serialize load-older and refresh operations. Bound rendered
history, initially 500 entries; at the cap offer **Continue with older activity**
to replace the current window using the oldest cursor, plus **Back to latest**.
Trim each page's requested limit to
the remaining capacity so its next cursor never skips undisplayed entries. Do
not silently drop rows while the user is reading or impose an unreachable-history
limit.

Use accessible tablist/tab/tabpanel relationships, `aria-selected`, associated
panel IDs, and roving keyboard focus with arrow/Home/End navigation. Hidden
tab content must not remain keyboard-focusable. Tab switches must not lose
focus or reset the media page unexpectedly.

Use a heading with a native disclosure button, `aria-expanded`, unique
`aria-controls`, and visible focus. Render timestamps with `<time datetime>`
and an accessible absolute date/time, not hover-only relative labels. Wrap
content on mobile; distinguish icons with text, not color alone. Announce short
loading/result status, preserve focus through pagination, and respect reduced
motion. No nested interactive controls inside the disclosure button.

## Implemented sequence and verification contract

1. Add migration/model, typed activity construction, append/list Store methods,
   visibility and cursor validation, and API generation. Test fresh install and
   upgrades from version 9. No synthetic legacy-event backfill.
2. Instrument requests and all monitoring changes, preserving cumulative
   attribution. Test enables, disables, re-enables, repeats, inherited overrides,
   mixed bulk edits, incomplete metadata, and transaction rollback on append
   failure. Add profile/preferred-release changes at the same boundary.
3. Add manual/automatic grabs and direct manual download status changes. Capture
   actors before background handoff; test one event per grab, duplicate-download
   rejection without a queued event, and stale CAS suppression. Leave extra
   automatic lifecycle outcomes for the optional extension.
4. Refactor metadata fetch/apply and match/unmatch boundaries, then add semantic
   diffs and policy-driven monitoring. Test unchanged refreshes, partial fetches,
   stale rematch responses, failed episode writes, and changed metadata rollback.
5. Refactor resync into scan/transactional apply, then cover resync, media-removal
   preparation, download/subtitle removal, and global/per-user watched mutations.
   Verify safe snapshots survive child deletion, failed final media deletion
   retains preparation history, watched visibility honors the action-time mode,
   and cleanup failures are never described as successful physical deletion.
6. Add the two tabs and panel tests: Details preserves existing operational
   controls/requesters but has no check block; Activity has read-only saved
   check content and history. Verify zero check/history requests while hidden
   or collapsed, tab-switch cancellation/cache reuse, check reopen/retry and
   freshness, activity expansion/reopen, inserts between pages, timestamp ties,
   cursor errors, visibility filtering, stale responses, SSE invalidation /
   reconnect, bounded pagination, retained rows after failures, and accessible
   tab/disclosure keyboard behavior.
7. Run uncached Go tests (`go test -count=1 ./...` from `backend/`), frontend
   regression tests with Node 24, type-check/build and lint. Use the disposable
   harness for authenticated API/SSE and deterministic fake-grab/import checks;
   verify desktop/mobile and keyboard behavior. No real tracker downloads.

The feature is not complete after requester/monitoring instrumentation alone:
the agreed automatic grab, diff-only metadata, and direct manual-action coverage
must also be verified. These are implementation slices, not permission to ship
an unlabeled partial history.

## Code references

- Existing requester decision: [ADR-140](DECISIONS.md#adr-140-persist-requester-attribution-separately-from-media-state).
- Request storage and deduplication: `backend/internal/store/models.go:40` and
  `backend/internal/store/sqlite/media_request.go:9`.
- Request transactions: `backend/internal/matching/service.go:398`.
- Monitoring transactions: `backend/internal/api/v1/handlers_media.go:47`,
  `:421`, and `:503`; enable-only attribution:
  `backend/internal/sync/request_attribution.go:94`.
- Automatic grab transaction: `backend/internal/monitor/service.go:549`.
- Download concurrency and lifecycle: `backend/internal/store/sqlite/download.go:13`,
  `backend/internal/download/service.go:132`, and
  `backend/internal/importer/service.go:487`.
- Metadata refresh and unmatch: `backend/internal/matching/service.go:1329`
  and `:291`; future-season policy: `backend/internal/metarefresh/service.go:86`.
- Lossy delivery: `backend/internal/eventbus/bus.go:107` and `:173`.
- Details layout and requester summary: `frontend/src/views/MediaDetailView.vue:626`
  and `:872`; disclosure precedent: `frontend/src/components/media/MonitorDecisionPanel.vue`.
