CREATE INDEX IF NOT EXISTS "tracks_updated_id_index"
  ON "tracks" ("updated_at" DESC, "id" DESC);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "albums_updated_id_index"
  ON "albums" ("updated_at" DESC, "id" DESC);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "local_music_sources_source_path_trgm_index"
  ON "local_music_sources" USING gin ("source_path" gin_trgm_ops);
--> statement-breakpoint
DROP INDEX IF EXISTS "metadata_writeback_jobs_track_time_index";
--> statement-breakpoint
CREATE INDEX "metadata_writeback_jobs_track_time_index"
  ON "metadata_writeback_jobs" ("track_id", "created_at" DESC, "id" DESC);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "metadata_writeback_jobs_created_id_index"
  ON "metadata_writeback_jobs" ("created_at" DESC, "id" DESC);
--> statement-breakpoint
ALTER TABLE "tag_scraping_job_items" ADD COLUMN "attempt_id" uuid;
--> statement-breakpoint
ALTER TABLE "tag_scraping_job_items" ADD COLUMN "locked_by" varchar(100);
--> statement-breakpoint
ALTER TABLE "tag_scraping_job_items" ADD COLUMN "locked_until" timestamp with time zone;
--> statement-breakpoint
DROP INDEX IF EXISTS "tag_scraping_job_items_work_index";
--> statement-breakpoint
CREATE INDEX "tag_scraping_job_items_work_index"
  ON "tag_scraping_job_items" ("job_id", "status", "locked_until", "position");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "playlists_owner_name_index"
  ON "playlists" ("owner_id", "name", "id");
