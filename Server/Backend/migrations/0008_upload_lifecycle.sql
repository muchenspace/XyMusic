ALTER TABLE "media_uploads" ADD COLUMN "completion_token" uuid;
--> statement-breakpoint
ALTER TABLE "media_uploads" ADD COLUMN "completion_started_at" timestamp with time zone;
--> statement-breakpoint
CREATE INDEX "media_uploads_uploader_active_index" ON "media_uploads" USING btree ("uploader_id", "status", "created_at");
