ALTER TABLE "metadata_writeback_jobs" ADD COLUMN "root_path_snapshot" varchar(4000);
--> statement-breakpoint
ALTER TABLE "metadata_writeback_jobs" ADD COLUMN "source_path_snapshot" varchar(1000);
--> statement-breakpoint
UPDATE "metadata_writeback_jobs" AS job
SET
  "root_path_snapshot" = root."path",
  "source_path_snapshot" = source."source_path"
FROM "local_music_sources" AS source
JOIN "library_roots" AS root ON root."id" = source."root_id"
WHERE source."id" = job."source_id";
--> statement-breakpoint
ALTER TABLE "metadata_writeback_jobs" ALTER COLUMN "root_path_snapshot" SET NOT NULL;
--> statement-breakpoint
ALTER TABLE "metadata_writeback_jobs" ALTER COLUMN "source_path_snapshot" SET NOT NULL;
