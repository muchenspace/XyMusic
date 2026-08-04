ALTER TABLE "tag_scraping_job_items"
  ADD COLUMN "attempts" integer NOT NULL DEFAULT 0,
  ADD COLUMN "max_attempts" integer NOT NULL DEFAULT 3,
  ADD COLUMN "next_attempt_at" timestamp with time zone NOT NULL DEFAULT now();
--> statement-breakpoint
ALTER TABLE "tag_scraping_job_items"
  ADD CONSTRAINT "tag_scraping_job_items_attempts_check"
  CHECK (
    "attempts" >= 0
    and "max_attempts" between 1 and 10
    and "attempts" <= "max_attempts"
  );
--> statement-breakpoint
DROP INDEX IF EXISTS "tag_scraping_job_items_pending_position_index";
--> statement-breakpoint
CREATE INDEX "tag_scraping_job_items_pending_ready_index"
  ON "tag_scraping_job_items" ("job_id", "next_attempt_at", "position")
  WHERE "status" = 'PENDING';
