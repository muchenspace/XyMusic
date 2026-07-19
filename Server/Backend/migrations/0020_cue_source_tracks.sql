CREATE TABLE IF NOT EXISTS "local_music_source_tracks" (
  "source_id" uuid NOT NULL REFERENCES "local_music_sources"("id") ON DELETE CASCADE,
  "track_id" uuid NOT NULL REFERENCES "tracks"("id") ON DELETE CASCADE,
  "media_job_id" uuid REFERENCES "media_jobs"("id") ON DELETE SET NULL,
  "segment_index" integer DEFAULT 0 NOT NULL,
  "start_ms" integer DEFAULT 0 NOT NULL,
  "end_ms" integer,
  "cue_path" varchar(1000),
  "cue_checksum_sha256" varchar(64),
  "created_at" timestamp with time zone DEFAULT now() NOT NULL,
  "updated_at" timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT "local_music_source_tracks_source_id_track_id_pk" PRIMARY KEY("source_id", "track_id"),
  CONSTRAINT "local_music_source_tracks_start_check" CHECK ("start_ms" >= 0),
  CONSTRAINT "local_music_source_tracks_end_check" CHECK ("end_ms" IS NULL OR "end_ms" > "start_ms")
  ,CONSTRAINT "local_music_source_tracks_cue_checksum_check" CHECK ("cue_checksum_sha256" IS NULL OR "cue_checksum_sha256" ~ '^[a-f0-9]{64}$')
);
--> statement-breakpoint
INSERT INTO "local_music_source_tracks" ("source_id", "track_id", "media_job_id", "segment_index", "start_ms")
SELECT "id", "track_id", "media_job_id", 0, 0 FROM "local_music_sources"
ON CONFLICT DO NOTHING;
--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "local_music_source_tracks_track_unique" ON "local_music_source_tracks" ("track_id");
CREATE UNIQUE INDEX IF NOT EXISTS "local_music_source_tracks_segment_unique" ON "local_music_source_tracks" ("source_id", "segment_index");
CREATE INDEX IF NOT EXISTS "local_music_source_tracks_job_index" ON "local_music_source_tracks" ("media_job_id");
