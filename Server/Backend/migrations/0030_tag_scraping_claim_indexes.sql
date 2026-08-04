CREATE INDEX IF NOT EXISTS "tag_scraping_job_items_pending_position_index"
  ON "tag_scraping_job_items" ("job_id", "position")
  WHERE "status" = 'PENDING';
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "tag_scraping_job_items_running_lease_index"
  ON "tag_scraping_job_items" ("job_id", "locked_until", "position")
  WHERE "status" = 'RUNNING';
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "tag_scraping_job_items_recovery_index"
  ON "tag_scraping_job_items" ("status", "locked_until")
  WHERE "status" = 'RUNNING';
