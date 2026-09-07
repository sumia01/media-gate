CREATE TABLE "media_requests_new" (
  "id" integer PRIMARY KEY AUTOINCREMENT,
  "media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
  "user_id" integer REFERENCES "users"("id") ON DELETE SET NULL,
  "scope" text NOT NULL CHECK ("scope" IN ('media', 'whole_series', 'future_seasons', 'season', 'episode')),
  "season_number" integer,
  "episode_number" integer,
  "requested_at" datetime NOT NULL,
  CHECK (
    ("scope" IN ('media', 'whole_series', 'future_seasons') AND "season_number" IS NULL AND "episode_number" IS NULL) OR
    ("scope" = 'season' AND "season_number" IS NOT NULL AND "episode_number" IS NULL) OR
    ("scope" = 'episode' AND "season_number" IS NOT NULL AND "episode_number" IS NOT NULL)
  )
);

INSERT INTO "media_requests_new" (
  "id", "media_item_id", "user_id", "scope", "season_number", "episode_number", "requested_at"
)
SELECT "id", "media_item_id", "user_id", "scope", "season_number", "episode_number", "requested_at"
FROM "media_requests";

DROP TABLE "media_requests";
ALTER TABLE "media_requests_new" RENAME TO "media_requests";

CREATE INDEX "idx_media_requests_media_item_id" ON "media_requests"("media_item_id");
CREATE INDEX "idx_media_requests_user_id" ON "media_requests"("user_id");
CREATE UNIQUE INDEX "idx_media_requests_unique_attribution"
  ON "media_requests"(
    "media_item_id",
    "user_id",
    "scope",
    IFNULL("season_number", -1),
    IFNULL("episode_number", -1)
  )
  WHERE "user_id" IS NOT NULL;
