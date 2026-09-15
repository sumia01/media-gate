# MediaGate Feature Summary

This document is a planning baseline: it describes functionality implemented in
the current repository, not proposed work. Use it with `docs/ROADMAP.md` for
historical detail and outstanding ideas.

## Product

MediaGate is a self-hosted, single-binary application for managing movie and
TV-series libraries. It combines the primary workflows commonly handled by
separate request, metadata, indexer, download, subtitle, and media-server tools.
The Go API and embedded Vue web application use one SQLite database and are
designed for a homelab or lightweight server.

## Core User Journeys

### Discover, request, and organize media

- Browse recently added local media plus TMDB trending, popular, and similar-title feeds.
- Open cast members from media details and browse their acting credits in counted Movies and Series tabs with independent load-more controls.
- Search TMDB and TVDB globally or from a library, inspect an external preview, and identify titles already in the local library.
- Add movies or series to a selected library as requests, optionally selecting a quality profile and monitoring scope.
- Track cumulative requesters while retaining the original request scope: title, whole series, future seasons, season, or individual episode.
- Manage movie and series libraries rooted in approved filesystem paths, including a directory browser, full scans, single-item resync, and asynchronous sync/match jobs.
- Filter an existing library instantly by a case-insensitive title substring and any selected metadata genres, with responsive controls, live counts, and clearable empty results.
- Parse video filenames and organize physical files into logical media items, including grouped series folders and per-item availability statuses.

### Enrich and manage a media item

- Automatically match local items to TMDB or TVDB, with confidence scoring and manual search, match, unmatch, and re-match controls.
- Display stored metadata including artwork, overview, genres, ratings, runtime, release date, content ratings, IMDb link, trailer, cast, crew, files, and episodes.
- Support per-library defaults and per-item overrides for media profiles, monitoring, future-season monitoring, and preferred release keywords.
- Track movie/series availability from actual files and, for monitored series, the selected aired-episode coverage.
- Maintain a read-only Activity tab with cursor-paginated media history, requester attribution, watched changes, metadata/resync actions, direct user actions, and the latest automatic-search diagnostic.

### Follow series and automate acquisition

- Monitor movies, entire series, seasons, or individual episodes using the hierarchy episode override, then season setting, then unmonitored.
- Optionally monitor newly discovered seasons and choose a season-pack strategy: prefer packs, prefer episodes, or packs only.
- Run an automatic-search worker that finds suitable releases for monitored media, applies profile filtering/ranking and preferred-release ordering, avoids duplicates and repeatedly failed releases, and queues selected downloads.
- Show a followed-series episode timeline with past and future air dates, availability and monitoring state, and download status.
- Persist the latest automatic-search decision, including no-result, blocked, partial-provider-failure, stale-input, and selected-release context.
- Refresh monitored series metadata in the background to add newly available seasons and episodes and reconcile earlier downloads with newly created episode records.

### Search releases, download, import, and seed

- Configure Cardigann-compatible torrent indexers, browse built-in/remote definitions, test connections, and search enabled indexers in parallel.
- Search releases at media, season, or episode scope; inspect profile and preferred-release matches; choose a release manually; or add and download directly from an external preview.
- Define reusable media profiles for resolution, language, source, excludes, priority ordering, and AND/OR language matching; test a profile against live indexer results.
- Send torrents to qBittorrent, show real-time progress/speed/status/file lists, retry or delete downloads, and provide item-level and global download history with filtering and incremental loading.
- Run the complete lifecycle: pending, downloading, downloaded, importing, seeding, completed, or terminal failure states.
- Retry transient download failures with bounded exponential backoff and prevent retries from repeatedly selecting blocklisted releases.
- Import completed releases through hardlinks when possible, copy across filesystems when needed, preserve companion files, update library records, and clean up empty directories.
- Respect per-indexer seed ratio/time obligations before removing completed torrents; support separate local and qBittorrent-side download paths.

### Subtitles, watched state, and notifications

- Search, score, download, list, and delete OpenSubtitles subtitles for movies and episodes; support language priorities, release/hash matching, and hearing-impaired/foreign-parts metadata.
- Optionally search/download subtitles automatically after import.
- Mark titles watched from local details or external previews and show seen badges across discover and library views.
- Configure watched state as shared globally or private to each user.
- Send Discord webhook notifications for successful imports and terminal download/import failures, with connection testing and safe, non-sensitive failure summaries.
- Refresh mapped Plex library sections automatically after imports and relevant deletions, with per-library debouncing, retries, manual refresh, and editable mappings.

## Administration And Operations

- First-run browser onboarding creates the initial account and configures the base path, torrent client, indexer, TMDB, and TVDB.
- Provide multi-user authentication with login, refresh-token rotation, profile/password management, user administration, and administrator/user roles.
- Restrict management functions, including libraries, indexers, profiles, settings, workers, Plex, updates, and user administration, to administrators.
- Configure integrations, worker intervals, paths, profile-wide excludes, content-rating countries, watched mode, subtitles, notifications, Plex, and optional OpenTelemetry from the UI.
- Test configured upstream integrations from the settings interface.
- Encrypt sensitive settings at rest and protect filesystem operations with cleaned, separator-aware path containment checks.
- Publish typed server-sent events for live UI updates across jobs, matching, downloads/imports, monitoring, subtitles, activity, and updates.
- Expose background worker status and manual run controls for monitor, metadata refresh, indexer-definition refresh, and self-update checks.
- Provide a SQLite database export and an optional Linux self-update flow based on GitHub Releases.
- Support optional OpenTelemetry trace/log export, structured logging, health/version/disk-usage reporting, and a disposable fake-integration test harness.

## Integrated Services

| Service | Implemented use |
| --- | --- |
| TMDB | Movie/series/person metadata, artwork, trailers, discovery, similar titles, and acting credits |
| TVDB | Series metadata, episode data, and person identity bridging to TMDB |
| qBittorrent | Torrent submission, status polling, file inspection, and cleanup |
| Cardigann/Prowlarr definitions | Torrent indexer configuration and search |
| FlareSolverr | Optional support for protected indexers |
| OpenSubtitles.com | Subtitle search and download |
| Discord | Import and terminal-failure webhooks |
| Plex | Library-section mapping and refresh triggers |
| GitHub Releases | Optional self-update checks and binary replacement |

## Current Constraints And Known Gaps

- SQLite is the only shipped storage backend; the store abstraction exists, but a PostgreSQL implementation is not present.
- Plex is the supported media-server integration. Jellyfin and Emby are not implemented.
- Discovery feeds use TMDB identities. A TVDB-matched library item is not automatically normalized to its TMDB identity, so it may not receive an in-library badge or hide-filter match in Discover.
- Automatic activity entries for some background outcomes, including import completion, terminal failures, and automatic subtitle results, are intentionally deferred. Direct user actions and automatic queued grabs are recorded.
- The self-update flow is Linux-only and requires a non-development build plus GitHub credentials.
- The app supports torrent indexers and qBittorrent; non-torrent download clients and Usenet workflows are not implemented.
- Operational telemetry export is available, but no built-in observability dashboard or external monitoring integration is provided.
