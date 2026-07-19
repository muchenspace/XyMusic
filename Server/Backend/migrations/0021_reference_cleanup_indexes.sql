CREATE INDEX IF NOT EXISTS "user_profiles_avatar_asset_index" ON "user_profiles" ("avatar_asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "artists_artwork_asset_index" ON "artists" ("artwork_asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "albums_cover_asset_index" ON "albums" ("cover_asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "lyrics_asset_index" ON "lyrics" ("asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "track_variants_asset_index" ON "track_variants" ("asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "media_uploads_track_index" ON "media_uploads" ("track_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "media_uploads_asset_index" ON "media_uploads" ("asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "media_jobs_source_asset_index" ON "media_jobs" ("source_asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "media_jobs_track_index" ON "media_jobs" ("track_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "local_music_sources_track_index" ON "local_music_sources" ("track_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "local_music_sources_source_asset_index" ON "local_music_sources" ("source_asset_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "local_music_sources_media_job_index" ON "local_music_sources" ("media_job_id");
