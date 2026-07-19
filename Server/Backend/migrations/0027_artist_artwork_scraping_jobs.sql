CREATE TABLE "artist_artwork_scraping_jobs" (
  "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
  "requested_by" uuid,
  "options" jsonb NOT NULL,
  "status" "tag_scraping_job_status" DEFAULT 'PENDING' NOT NULL,
  "total" integer NOT NULL,
  "processed" integer DEFAULT 0 NOT NULL,
  "succeeded" integer DEFAULT 0 NOT NULL,
  "failed" integer DEFAULT 0 NOT NULL,
  "cancel_requested" boolean DEFAULT false NOT NULL,
  "started_at" timestamp with time zone,
  "completed_at" timestamp with time zone,
  "created_at" timestamp with time zone DEFAULT now() NOT NULL,
  "updated_at" timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT "artist_artwork_scraping_jobs_counts_check" CHECK (
    "total" between 1 and 200
    and "processed" between 0 and "total"
    and "succeeded" between 0 and "processed"
    and "failed" between 0 and "processed"
    and "succeeded" + "failed" <= "processed"
  )
);

CREATE TABLE "artist_artwork_scraping_job_items" (
  "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
  "job_id" uuid NOT NULL,
  "artist_id" uuid NOT NULL,
  "expected_version" integer NOT NULL,
  "position" integer NOT NULL,
  "status" "tag_scraping_item_status" DEFAULT 'PENDING' NOT NULL,
  "attempts" integer DEFAULT 0 NOT NULL,
  "max_attempts" integer DEFAULT 3 NOT NULL,
  "next_attempt_at" timestamp with time zone DEFAULT now() NOT NULL,
  "attempt_id" uuid,
  "locked_by" varchar(100),
  "locked_until" timestamp with time zone,
  "candidate" jsonb,
  "source" varchar(30),
  "message" text,
  "started_at" timestamp with time zone,
  "completed_at" timestamp with time zone,
  "created_at" timestamp with time zone DEFAULT now() NOT NULL,
  "updated_at" timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT "artist_artwork_scraping_job_items_values_check" CHECK (
    "expected_version" > 0
    and "position" >= 0
    and "attempts" >= 0
    and "max_attempts" between 1 and 10
    and "attempts" <= "max_attempts"
  )
);

ALTER TABLE "artist_artwork_scraping_jobs"
  ADD CONSTRAINT "artist_artwork_scraping_jobs_requested_by_users_id_fk"
  FOREIGN KEY ("requested_by") REFERENCES "public"."users"("id") ON DELETE set null;

ALTER TABLE "artist_artwork_scraping_job_items"
  ADD CONSTRAINT "artist_artwork_scraping_job_items_job_id_jobs_id_fk"
  FOREIGN KEY ("job_id") REFERENCES "public"."artist_artwork_scraping_jobs"("id") ON DELETE cascade;

ALTER TABLE "artist_artwork_scraping_job_items"
  ADD CONSTRAINT "artist_artwork_scraping_job_items_artist_id_artists_id_fk"
  FOREIGN KEY ("artist_id") REFERENCES "public"."artists"("id") ON DELETE cascade;

CREATE INDEX "artist_artwork_scraping_jobs_claim_index"
  ON "artist_artwork_scraping_jobs" ("status", "created_at", "id");

CREATE INDEX "artist_artwork_scraping_jobs_requester_index"
  ON "artist_artwork_scraping_jobs" ("requested_by", "created_at");

CREATE UNIQUE INDEX "artist_artwork_scraping_job_items_position_unique"
  ON "artist_artwork_scraping_job_items" ("job_id", "position");

CREATE UNIQUE INDEX "artist_artwork_scraping_job_items_artist_unique"
  ON "artist_artwork_scraping_job_items" ("job_id", "artist_id");

CREATE INDEX "artist_artwork_scraping_job_items_work_index"
  ON "artist_artwork_scraping_job_items" (
    "job_id", "status", "next_attempt_at", "locked_until", "position"
  );

CREATE INDEX "artist_artwork_scraping_job_items_recovery_index"
  ON "artist_artwork_scraping_job_items" ("status", "locked_until")
  WHERE "status" = 'RUNNING';

CREATE INDEX "artist_artwork_scraping_job_items_artist_index"
  ON "artist_artwork_scraping_job_items" ("artist_id", "status", "created_at");
