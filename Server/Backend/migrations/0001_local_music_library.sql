ALTER TABLE "media_jobs" ADD COLUMN "publish_on_ready" boolean DEFAULT false NOT NULL;

CREATE TABLE "local_music_sources" (
  "id" uuid PRIMARY KEY DEFAULT gen_random_uuid() NOT NULL,
  "source_path" varchar(1000) NOT NULL,
  "checksum_sha256" varchar(64) NOT NULL,
  "size_bytes" bigint NOT NULL,
  "modified_at" timestamp with time zone NOT NULL,
  "track_id" uuid NOT NULL,
  "source_asset_id" uuid,
  "media_job_id" uuid,
  "status" varchar(30) DEFAULT 'PENDING' NOT NULL,
  "last_error" text,
  "last_seen_at" timestamp with time zone DEFAULT now() NOT NULL,
  "created_at" timestamp with time zone DEFAULT now() NOT NULL,
  "updated_at" timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE "local_music_sources" ADD CONSTRAINT "local_music_sources_track_id_tracks_id_fk"
  FOREIGN KEY ("track_id") REFERENCES "public"."tracks"("id") ON DELETE cascade ON UPDATE no action;
ALTER TABLE "local_music_sources" ADD CONSTRAINT "local_music_sources_source_asset_id_media_assets_id_fk"
  FOREIGN KEY ("source_asset_id") REFERENCES "public"."media_assets"("id") ON DELETE set null ON UPDATE no action;
ALTER TABLE "local_music_sources" ADD CONSTRAINT "local_music_sources_media_job_id_media_jobs_id_fk"
  FOREIGN KEY ("media_job_id") REFERENCES "public"."media_jobs"("id") ON DELETE set null ON UPDATE no action;
CREATE UNIQUE INDEX "local_music_sources_path_unique" ON "local_music_sources" USING btree ("source_path");
CREATE INDEX "local_music_sources_checksum_index" ON "local_music_sources" USING btree ("checksum_sha256");
CREATE INDEX "local_music_sources_scan_index" ON "local_music_sources" USING btree ("last_seen_at", "status");
