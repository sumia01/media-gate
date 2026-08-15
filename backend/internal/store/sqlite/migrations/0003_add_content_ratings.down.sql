-- Drops the certification column. Requires SQLite >= 3.35 for DROP COLUMN,
-- which the bundled pure-Go driver (glebarez/go-sqlite) satisfies.
ALTER TABLE "media_metadata" DROP COLUMN "content_ratings";
