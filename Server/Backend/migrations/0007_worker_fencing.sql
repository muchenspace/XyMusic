ALTER TYPE "asset_status" ADD VALUE IF NOT EXISTS 'DELETE_PENDING' BEFORE 'DELETED';
--> statement-breakpoint
CREATE TYPE "object_cleanup_status" AS ENUM ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED');
--> statement-breakpoint
ALTER TABLE "tracks" ADD COLUMN "media_generation" integer DEFAULT 0 NOT NULL;
--> statement-breakpoint
ALTER TABLE "media_jobs" ADD COLUMN "generation" integer DEFAULT 1 NOT NULL;
--> statement-breakpoint
ALTER TABLE "media_jobs" ADD COLUMN "attempt_id" uuid;
--> statement-breakpoint
ALTER TABLE "media_jobs" ADD COLUMN "cancel_requested" boolean DEFAULT false NOT NULL;
--> statement-breakpoint
ALTER TABLE "library_scan_runs" ADD COLUMN "root_version" integer DEFAULT 1 NOT NULL;
--> statement-breakpoint
ALTER TABLE "library_scan_runs" ADD COLUMN "attempt_id" uuid;
--> statement-breakpoint
ALTER TABLE "library_scan_runs" ADD COLUMN "locked_by" varchar(100);
--> statement-breakpoint
ALTER TABLE "library_scan_runs" ADD COLUMN "locked_until" timestamp with time zone;
--> statement-breakpoint
ALTER TABLE "library_scan_runs" ADD COLUMN "heartbeat_at" timestamp with time zone;
--> statement-breakpoint
WITH ranked AS (
  SELECT "id", row_number() OVER (
    PARTITION BY "track_id"
    ORDER BY "created_at" DESC, "id" DESC
  ) AS position
  FROM "media_jobs"
  WHERE "status" IN ('PENDING', 'PROCESSING')
)
UPDATE "media_jobs" AS job
SET "status" = 'CANCELLED',
    "cancel_requested" = true,
    "locked_by" = NULL,
    "locked_until" = NULL,
    "last_error" = 'Superseded during worker fencing migration',
    "last_error_code" = 'SUPERSEDED',
    "updated_at" = now(),
    "version" = "version" + 1
FROM ranked
WHERE job."id" = ranked."id" AND ranked.position > 1;
--> statement-breakpoint
WITH generations AS (
  SELECT "id", row_number() OVER (
    PARTITION BY "track_id"
    ORDER BY "created_at", "id"
  )::integer AS generation
  FROM "media_jobs"
)
UPDATE "media_jobs" AS job
SET "generation" = generations.generation
FROM generations
WHERE job."id" = generations."id";
--> statement-breakpoint
UPDATE "tracks" AS track
SET "media_generation" = generation.maximum
FROM (
  SELECT "track_id", max("generation") AS maximum
  FROM "media_jobs"
  GROUP BY "track_id"
) AS generation
WHERE track."id" = generation."track_id";
--> statement-breakpoint
WITH ranked AS (
  SELECT "id", row_number() OVER (
    PARTITION BY "root_id"
    ORDER BY "created_at" DESC, "id" DESC
  ) AS position
  FROM "library_scan_runs"
  WHERE "status" IN ('PENDING', 'RUNNING')
)
UPDATE "library_scan_runs" AS run
SET "status" = 'CANCELLED',
    "cancel_requested" = true,
    "completed_at" = now(),
    "last_error" = 'Superseded during scan fencing migration',
    "updated_at" = now()
FROM ranked
WHERE run."id" = ranked."id" AND ranked.position > 1;
--> statement-breakpoint
UPDATE "library_scan_runs" AS run
SET "root_version" = root."version"
FROM "library_roots" AS root
WHERE run."root_id" = root."id";
--> statement-breakpoint
CREATE TABLE "object_cleanup_jobs" (
  "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
  "object_key" varchar(500) NOT NULL,
  "reason" varchar(100) NOT NULL,
  "status" "object_cleanup_status" DEFAULT 'PENDING' NOT NULL,
  "attempts" integer DEFAULT 0 NOT NULL,
  "max_attempts" integer DEFAULT 20 NOT NULL,
  "attempt_id" uuid,
  "locked_by" varchar(100),
  "locked_until" timestamp with time zone,
  "next_attempt_at" timestamp with time zone DEFAULT now() NOT NULL,
  "last_error" text,
  "created_at" timestamp with time zone DEFAULT now() NOT NULL,
  "updated_at" timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT "object_cleanup_jobs_attempts_check" CHECK ("attempts" >= 0 AND "max_attempts" > 0)
);
--> statement-breakpoint
ALTER TABLE "media_jobs" ADD CONSTRAINT "media_jobs_generation_check" CHECK ("generation" > 0);
--> statement-breakpoint
ALTER TABLE "media_jobs" DROP CONSTRAINT IF EXISTS "media_jobs_attempts_check";
--> statement-breakpoint
ALTER TABLE "media_jobs" ADD CONSTRAINT "media_jobs_attempts_check" CHECK ("attempts" >= 0 AND "max_attempts" > 0);
--> statement-breakpoint
ALTER TABLE "library_scan_runs" ADD CONSTRAINT "library_scan_runs_root_version_check" CHECK ("root_version" > 0);
--> statement-breakpoint
CREATE UNIQUE INDEX "media_jobs_track_active_unique" ON "media_jobs" USING btree ("track_id") WHERE "status" IN ('PENDING', 'PROCESSING');
--> statement-breakpoint
CREATE UNIQUE INDEX "library_scan_runs_root_active_unique" ON "library_scan_runs" USING btree ("root_id") WHERE "status" IN ('PENDING', 'RUNNING');
--> statement-breakpoint
CREATE UNIQUE INDEX "object_cleanup_jobs_object_key_unique" ON "object_cleanup_jobs" USING btree ("object_key");
--> statement-breakpoint
CREATE INDEX "object_cleanup_jobs_claim_index" ON "object_cleanup_jobs" USING btree ("status", "next_attempt_at", "locked_until");
