ALTER TABLE "media_items" ADD COLUMN "deletion_pending" integer NOT NULL DEFAULT 0;

CREATE TABLE "media_activity" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "recorded_at" datetime NOT NULL,
  "actor_kind" text NOT NULL CHECK ("actor_kind" IN ('user', 'system')),
  "actor_user_id" integer REFERENCES "users"("id") ON DELETE SET NULL,
  "actor_component" text NOT NULL DEFAULT '',
  "action" text NOT NULL,
  "operation_id" text NOT NULL,
  "visibility" text NOT NULL CHECK ("visibility" IN ('shared', 'actor_only')),
  "media_title" text NOT NULL CHECK (length("media_title") BETWEEN 1 AND 512),
  "details_version" integer NOT NULL CHECK ("details_version" > 0),
  "details" text NOT NULL CHECK (length("details") BETWEEN 1 AND 32768 AND json_valid("details")),
  CHECK (
    ("actor_kind" = 'user' AND "actor_component" = '') OR
    ("actor_kind" = 'system' AND "actor_user_id" IS NULL AND length("actor_component") BETWEEN 1 AND 64)
  ),
  CHECK ("visibility" = 'shared' OR "actor_kind" = 'user'),
  CHECK (length("operation_id") BETWEEN 1 AND 64)
);

CREATE INDEX "idx_media_activity_media_item_id_id" ON "media_activity"("media_item_id", "id" DESC);
CREATE INDEX "idx_media_activity_actor_user_id" ON "media_activity"("actor_user_id");

DROP TRIGGER IF EXISTS "watched_items_v9_unique_insert";
DROP TRIGGER IF EXISTS "watched_items_v9_unique_update";
DROP INDEX "idx_watched_user_source_ext";
CREATE UNIQUE INDEX "idx_watched_user_source_type_ext"
  ON "watched_items"("user_id", "source", "media_type", "external_id");
