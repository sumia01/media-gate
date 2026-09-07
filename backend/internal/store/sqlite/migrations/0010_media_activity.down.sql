DROP TABLE IF EXISTS "media_activity";
DROP INDEX IF EXISTS "idx_watched_user_source_type_ext";
CREATE UNIQUE INDEX "idx_watched_user_source_ext"
  ON "watched_items"("user_id", "source", "external_id");
ALTER TABLE "media_items" DROP COLUMN "deletion_pending";
