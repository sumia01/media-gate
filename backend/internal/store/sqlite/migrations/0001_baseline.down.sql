-- Reverse of 0001_baseline. Drops every table (children before parents so FK
-- enforcement never blocks). Provided for completeness — the app never runs
-- Down() automatically.

DROP TABLE IF EXISTS "watched_items";
DROP TABLE IF EXISTS "subtitles";
DROP TABLE IF EXISTS "refresh_tokens";
DROP TABLE IF EXISTS "download_blocklists";
DROP TABLE IF EXISTS "downloads";
DROP TABLE IF EXISTS "indexers";
DROP TABLE IF EXISTS "job_records";
DROP TABLE IF EXISTS "settings";
DROP TABLE IF EXISTS "media_profiles";
DROP TABLE IF EXISTS "episodes";
DROP TABLE IF EXISTS "episode_monitors";
DROP TABLE IF EXISTS "season_monitors";
DROP TABLE IF EXISTS "media_files";
DROP TABLE IF EXISTS "media_metadata";
DROP TABLE IF EXISTS "media_items";
DROP TABLE IF EXISTS "users";
DROP TABLE IF EXISTS "libraries";
