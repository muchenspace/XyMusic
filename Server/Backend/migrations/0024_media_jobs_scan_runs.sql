ALTER TABLE "media_jobs"
  ADD COLUMN "scan_run_id" uuid REFERENCES "library_scan_runs"("id") ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS "media_jobs_scan_run_status_updated_index"
  ON "media_jobs" ("scan_run_id", "status", "updated_at" DESC, "id" DESC);
