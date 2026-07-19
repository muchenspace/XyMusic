CREATE TYPE "track_status" AS ENUM ('READY', 'ERROR', 'ARCHIVED');

ALTER TABLE "tracks" ADD COLUMN "status_v2" "track_status" NOT NULL DEFAULT 'READY';
UPDATE "tracks" SET "status_v2" = (
  CASE
    WHEN "status"::text = 'ARCHIVED' THEN 'ARCHIVED'
    WHEN "status"::text = 'FAILED' THEN 'ERROR'
    WHEN "status"::text IN ('DRAFT', 'PROCESSING') AND COALESCE((
      SELECT "media_jobs"."status"::text IN ('FAILED', 'CANCELLED')
      FROM "media_jobs"
      WHERE "media_jobs"."track_id" = "tracks"."id"
      ORDER BY "media_jobs"."created_at" DESC, "media_jobs"."id" DESC
      LIMIT 1
    ), false) THEN 'ERROR'
    ELSE 'READY'
  END
)::"track_status";
DROP INDEX "tracks_status_published_index";
ALTER TABLE "tracks" DROP COLUMN "status";
ALTER TABLE "tracks" RENAME COLUMN "status_v2" TO "status";
CREATE INDEX "tracks_status_published_index" ON "tracks" USING btree ("status", "published_at", "id");

CREATE TEMP TABLE "orphan_album_assets" ON COMMIT DROP AS
SELECT DISTINCT "cover_asset_id" AS "asset_id"
FROM "albums"
WHERE "cover_asset_id" IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM "tracks" WHERE "tracks"."album_id" = "albums"."id");

DELETE FROM "albums"
WHERE NOT EXISTS (SELECT 1 FROM "tracks" WHERE "tracks"."album_id" = "albums"."id");

UPDATE "media_assets"
SET "status" = 'DELETE_PENDING', "updated_at" = now()
WHERE "id" IN (SELECT "asset_id" FROM "orphan_album_assets")
  AND NOT EXISTS (SELECT 1 FROM "artists" WHERE "artists"."artwork_asset_id" = "media_assets"."id")
  AND NOT EXISTS (SELECT 1 FROM "albums" WHERE "albums"."cover_asset_id" = "media_assets"."id")
  AND NOT EXISTS (SELECT 1 FROM "user_profiles" WHERE "user_profiles"."avatar_asset_id" = "media_assets"."id");

INSERT INTO "object_cleanup_jobs" ("object_key", "reason")
SELECT "object_key", 'EMPTY_ALBUM'
FROM "media_assets"
WHERE "id" IN (SELECT "asset_id" FROM "orphan_album_assets")
  AND "status" = 'DELETE_PENDING'
ON CONFLICT ("object_key") DO UPDATE SET
  "reason" = EXCLUDED."reason",
  "status" = 'PENDING',
  "attempts" = 0,
  "attempt_id" = NULL,
  "locked_by" = NULL,
  "locked_until" = NULL,
  "next_attempt_at" = now(),
  "last_error" = NULL,
  "updated_at" = now();
