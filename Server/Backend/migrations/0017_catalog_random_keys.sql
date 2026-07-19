ALTER TABLE "albums" ADD COLUMN "random_key" double precision;
UPDATE "albums" SET "random_key" = random();
ALTER TABLE "albums" ALTER COLUMN "random_key" SET DEFAULT random();
ALTER TABLE "albums" ALTER COLUMN "random_key" SET NOT NULL;

ALTER TABLE "tracks" ADD COLUMN "random_key" double precision;
UPDATE "tracks" SET "random_key" = random();
ALTER TABLE "tracks" ALTER COLUMN "random_key" SET DEFAULT random();
ALTER TABLE "tracks" ALTER COLUMN "random_key" SET NOT NULL;

CREATE INDEX "albums_random_key_index" ON "albums" ("random_key", "id");
CREATE INDEX "tracks_ready_random_key_index" ON "tracks" ("random_key", "id")
  WHERE "status" = 'READY' AND "published_at" IS NOT NULL;
