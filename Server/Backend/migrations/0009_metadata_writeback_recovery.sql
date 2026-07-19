ALTER TABLE "metadata_writeback_jobs" ADD COLUMN "attempt_id" uuid;
--> statement-breakpoint
ALTER TABLE "metadata_writeback_jobs" ADD COLUMN "stage" varchar(30) DEFAULT 'QUEUED' NOT NULL;
--> statement-breakpoint
ALTER TABLE "metadata_writeback_jobs" ADD COLUMN "backup_expires_at" timestamp with time zone;
--> statement-breakpoint
UPDATE "metadata_writeback_jobs"
SET
  "stage" = CASE
    WHEN "status" = 'READY' THEN 'COMMITTED'
    WHEN "status" = 'PROCESSING' AND "backup_path" IS NOT NULL THEN 'PREPARED'
    WHEN "status" = 'PROCESSING' THEN 'PREPARING'
    ELSE 'QUEUED'
  END,
  "backup_expires_at" = CASE
    WHEN "status" = 'READY' AND "backup_path" IS NOT NULL
      THEN now() + interval '7 days'
    ELSE NULL
  END;
--> statement-breakpoint
ALTER TABLE "metadata_writeback_jobs" ADD CONSTRAINT "metadata_writeback_jobs_stage_check"
  CHECK ("stage" IN ('QUEUED', 'PREPARING', 'PREPARED', 'FILE_REPLACED', 'COMMITTED'));
--> statement-breakpoint
CREATE INDEX "metadata_writeback_jobs_backup_expiry_index"
  ON "metadata_writeback_jobs" ("status", "backup_expires_at");
