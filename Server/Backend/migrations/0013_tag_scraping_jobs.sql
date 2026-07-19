CREATE TYPE "public"."tag_scraping_job_status" AS ENUM('PENDING', 'RUNNING', 'COMPLETED', 'CANCELLED', 'FAILED');
CREATE TYPE "public"."tag_scraping_item_status" AS ENUM('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'SKIPPED');

CREATE TABLE "tag_scraping_jobs" (
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
  CONSTRAINT "tag_scraping_jobs_counts_check" CHECK ("tag_scraping_jobs"."total" between 1 and 200 and "tag_scraping_jobs"."processed" >= 0 and "tag_scraping_jobs"."succeeded" >= 0 and "tag_scraping_jobs"."failed" >= 0)
);

CREATE TABLE "tag_scraping_job_items" (
  "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
  "job_id" uuid NOT NULL,
  "track_id" uuid NOT NULL,
  "expected_version" integer NOT NULL,
  "position" integer NOT NULL,
  "status" "tag_scraping_item_status" DEFAULT 'PENDING' NOT NULL,
  "candidate" jsonb,
  "source" varchar(30),
  "message" text,
  "started_at" timestamp with time zone,
  "completed_at" timestamp with time zone,
  "created_at" timestamp with time zone DEFAULT now() NOT NULL,
  "updated_at" timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT "tag_scraping_job_items_version_check" CHECK ("tag_scraping_job_items"."expected_version" > 0 and "tag_scraping_job_items"."position" >= 0)
);

ALTER TABLE "tag_scraping_jobs" ADD CONSTRAINT "tag_scraping_jobs_requested_by_users_id_fk" FOREIGN KEY ("requested_by") REFERENCES "public"."users"("id") ON DELETE set null;
ALTER TABLE "tag_scraping_job_items" ADD CONSTRAINT "tag_scraping_job_items_job_id_tag_scraping_jobs_id_fk" FOREIGN KEY ("job_id") REFERENCES "public"."tag_scraping_jobs"("id") ON DELETE cascade;
ALTER TABLE "tag_scraping_job_items" ADD CONSTRAINT "tag_scraping_job_items_track_id_tracks_id_fk" FOREIGN KEY ("track_id") REFERENCES "public"."tracks"("id") ON DELETE cascade;
CREATE INDEX "tag_scraping_jobs_claim_index" ON "tag_scraping_jobs" USING btree ("status", "created_at");
CREATE INDEX "tag_scraping_jobs_requester_index" ON "tag_scraping_jobs" USING btree ("requested_by", "created_at");
CREATE UNIQUE INDEX "tag_scraping_job_items_position_unique" ON "tag_scraping_job_items" USING btree ("job_id", "position");
CREATE UNIQUE INDEX "tag_scraping_job_items_track_unique" ON "tag_scraping_job_items" USING btree ("job_id", "track_id");
CREATE INDEX "tag_scraping_job_items_work_index" ON "tag_scraping_job_items" USING btree ("job_id", "status", "position");
