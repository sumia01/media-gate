-- 0001_baseline: the complete Media Gate schema as of schema_version 9.
--
-- This is the golang-migrate baseline. On a FRESH install it creates the whole
-- schema. On an EXISTING (pre-golang-migrate) database it is NOT executed — the
-- runner detects the legacy schema and stamps this version with Force(), so the
-- on-disk data is never touched (zero data loss).
--
-- Notes on fidelity vs. the old GORM AutoMigrate + V1..V9 system:
--   * media_items includes monitor_new_seasons + preferred_release INLINE (the
--     old system added them via ALTER, which the glebarez AutoMigrate rebuild
--     kept dropping — the whole reason for this switch).
--   * media_metadata includes trailer_url (the old V1 rebuild dropped it; a
--     model column, re-added by AutoMigrate only on the 2nd+ boot).
--   * episode_monitors and subtitles intentionally have NO foreign keys — this
--     matches the current fresh-install schema. Their cascade-on-delete is
--     emulated by cleanupOrphans() at boot, exactly as before.
--   * Indentation is spaces, never TABs.

CREATE TABLE "libraries" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "name" text NOT NULL,
  "path" text NOT NULL,
  "media_type" text NOT NULL,
  "media_profile_id" integer,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "users" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "email" text NOT NULL,
  "password_hash" text NOT NULL,
  "first_name" text,
  "last_name" text,
  "birth_year" integer,
  "is_admin" numeric NOT NULL DEFAULT false,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "media_items" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "library_id" integer NOT NULL REFERENCES "libraries"("id") ON DELETE CASCADE,
  "title" text NOT NULL,
  "media_type" text NOT NULL,
  "status" text NOT NULL DEFAULT 'new',
  "source" text NOT NULL DEFAULT 'disk',
  "year" integer,
  "media_profile_id" integer,
  "monitored" numeric NOT NULL DEFAULT false,
  "monitor_new_seasons" numeric NOT NULL DEFAULT true,
  "monitor_search_started_at" datetime,
  "preferred_release" text NOT NULL DEFAULT '',
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "media_metadata" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "source" text NOT NULL,
  "external_id" integer NOT NULL,
  "imdb_id" text,
  "title" text NOT NULL,
  "overview" text,
  "poster_path" text,
  "genres" text,
  "credits" text,
  "year" integer,
  "rating" real,
  "status" text,
  "runtime" integer,
  "seasons" integer,
  "release_date" text,
  "trailer_url" text,
  "confidence" real,
  "matched_at" datetime,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "media_files" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "path" text NOT NULL,
  "file_name" text NOT NULL,
  "size" integer,
  "resolution" text,
  "source_type" text,
  "season_number" integer,
  "episode_number" integer,
  "added_at" datetime,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "season_monitors" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "season_number" integer NOT NULL,
  "monitored" numeric NOT NULL,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "episode_monitors" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL,
  "season_number" integer NOT NULL,
  "episode_number" integer NOT NULL,
  "monitored" numeric NOT NULL,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "episodes" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "season_number" integer NOT NULL,
  "episode_number" integer NOT NULL,
  "title" text,
  "overview" text,
  "air_date" text,
  "runtime" integer,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "media_profiles" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "name" text NOT NULL,
  "resolutions" text NOT NULL,
  "languages" text NOT NULL,
  "language_mode" text DEFAULT 'or',
  "sources" text,
  "exclude_tags" text,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "settings" (
  "key" text,
  "value" text NOT NULL,
  "sensitive" numeric NOT NULL DEFAULT false,
  "created_at" datetime,
  "updated_at" datetime,
  PRIMARY KEY ("key")
);

CREATE TABLE "job_records" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "type" text NOT NULL,
  "library_id" integer NOT NULL,
  "library_name" text NOT NULL,
  "status" text NOT NULL,
  "result_message" text,
  "error" text,
  "created_at" datetime,
  "started_at" datetime,
  "completed_at" datetime
);

CREATE TABLE "indexers" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "name" text NOT NULL,
  "definition_id" text NOT NULL,
  "enabled" numeric NOT NULL DEFAULT true,
  "settings" text NOT NULL DEFAULT '{}',
  "priority" integer NOT NULL DEFAULT 0,
  "seed_min_ratio" real NOT NULL DEFAULT 0,
  "seed_min_time" integer NOT NULL DEFAULT 0,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "downloads" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "episode_id" integer REFERENCES "episodes"("id") ON DELETE SET NULL,
  "season_number" integer,
  "indexer_id" integer NOT NULL,
  "indexer_name" text NOT NULL,
  "title" text NOT NULL,
  "download_url" text NOT NULL,
  "details_url" text,
  "size" text,
  "imdb_id" text,
  "status" text NOT NULL DEFAULT 'pending',
  "client_torrent_hash" text,
  "save_path" text,
  "seeding_required" numeric NOT NULL DEFAULT false,
  "linked_to_library" numeric NOT NULL DEFAULT false,
  "retry_count" integer NOT NULL DEFAULT 0,
  "next_retry_at" datetime,
  "last_error" text,
  "created_at" datetime,
  "updated_at" datetime,
  "completed_at" datetime
);

CREATE TABLE "download_blocklists" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "download_url" text NOT NULL,
  "title" text,
  "fail_count" integer NOT NULL DEFAULT 0,
  "last_error" text,
  "last_failed_at" datetime,
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "refresh_tokens" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "user_id" integer NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "token" text NOT NULL,
  "expires_at" datetime NOT NULL,
  "created_at" datetime
);

CREATE TABLE "subtitles" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL,
  "media_file_id" integer,
  "season_number" integer,
  "episode_number" integer,
  "language" text NOT NULL,
  "provider" text NOT NULL,
  "provider_file_id" text,
  "release_name" text,
  "file_name" text NOT NULL,
  "file_path" text NOT NULL,
  "format" text,
  "score" integer,
  "hearing_impaired" numeric NOT NULL DEFAULT false,
  "foreign_parts_only" numeric NOT NULL DEFAULT false,
  "source" text NOT NULL DEFAULT 'manual',
  "created_at" datetime,
  "updated_at" datetime
);

CREATE TABLE "watched_items" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "user_id" integer NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "source" text NOT NULL,
  "external_id" integer NOT NULL,
  "imdb_id" text,
  "title" text NOT NULL,
  "media_type" text NOT NULL,
  "year" integer,
  "poster_path" text,
  "media_item_id" integer REFERENCES "media_items"("id") ON DELETE SET NULL,
  "watched_at" datetime,
  "created_at" datetime,
  "updated_at" datetime
);

-- Indexes (mirror the GORM index/uniqueIndex tags).
CREATE INDEX "idx_media_items_library_id" ON "media_items"("library_id");
CREATE UNIQUE INDEX "idx_media_metadata_media_item_id" ON "media_metadata"("media_item_id");
CREATE INDEX "idx_media_files_media_item_id" ON "media_files"("media_item_id");
CREATE UNIQUE INDEX "idx_media_files_path" ON "media_files"("path");
CREATE UNIQUE INDEX "idx_media_season" ON "season_monitors"("media_item_id","season_number");
CREATE UNIQUE INDEX "idx_ep_monitor_unique" ON "episode_monitors"("media_item_id","season_number","episode_number");
CREATE INDEX "idx_episodes_media_item_id" ON "episodes"("media_item_id");
CREATE UNIQUE INDEX "idx_episode_unique" ON "episodes"("media_item_id","season_number","episode_number");
CREATE UNIQUE INDEX "idx_media_profiles_name" ON "media_profiles"("name");
CREATE INDEX "idx_job_records_library_id" ON "job_records"("library_id");
CREATE INDEX "idx_downloads_media_item_id" ON "downloads"("media_item_id");
CREATE INDEX "idx_downloads_episode_id" ON "downloads"("episode_id");
CREATE INDEX "idx_download_blocklists_media_item_id" ON "download_blocklists"("media_item_id");
CREATE UNIQUE INDEX "idx_blocklist_item_url" ON "download_blocklists"("media_item_id","download_url");
CREATE UNIQUE INDEX "idx_libraries_path" ON "libraries"("path");
CREATE UNIQUE INDEX "idx_users_email" ON "users"("email");
CREATE INDEX "idx_refresh_tokens_user_id" ON "refresh_tokens"("user_id");
CREATE UNIQUE INDEX "idx_refresh_tokens_token" ON "refresh_tokens"("token");
CREATE INDEX "idx_subtitles_media_item_id" ON "subtitles"("media_item_id");
CREATE INDEX "idx_subtitles_media_file_id" ON "subtitles"("media_file_id");
CREATE UNIQUE INDEX "idx_subtitles_file_path" ON "subtitles"("file_path");
CREATE INDEX "idx_watched_items_user_id" ON "watched_items"("user_id");
CREATE INDEX "idx_watched_items_media_item_id" ON "watched_items"("media_item_id");
CREATE UNIQUE INDEX "idx_watched_user_source_ext" ON "watched_items"("user_id","source","external_id");
