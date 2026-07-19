CREATE EXTENSION IF NOT EXISTS "pg_trgm";
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "album_artists_artist_album_index"
  ON "album_artists" ("artist_id", "album_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "track_artists_artist_track_index"
  ON "track_artists" ("artist_id", "track_id");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "tracks_normalized_title_trgm_index"
  ON "tracks" USING gin ("normalized_title" gin_trgm_ops);
--> statement-breakpoint
DROP INDEX IF EXISTS "tracks_title_trgm_index";
--> statement-breakpoint
CREATE INDEX "tracks_title_trgm_index"
  ON "tracks" USING gin ("title" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "albums_normalized_title_trgm_index"
  ON "albums" USING gin ("normalized_title" gin_trgm_ops);
--> statement-breakpoint
DROP INDEX IF EXISTS "albums_title_trgm_index";
--> statement-breakpoint
CREATE INDEX "albums_title_trgm_index"
  ON "albums" USING gin ("title" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "artists_normalized_name_trgm_index"
  ON "artists" USING gin ("normalized_name" gin_trgm_ops);
--> statement-breakpoint
DROP INDEX IF EXISTS "artists_name_trgm_index";
--> statement-breakpoint
CREATE INDEX "artists_name_trgm_index"
  ON "artists" USING gin ("name" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "users_username_trgm_index"
  ON "users" USING gin ("username" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "user_profiles_display_name_trgm_index"
  ON "user_profiles" USING gin ("display_name" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_logs_created_id_index"
  ON "audit_logs" ("created_at" DESC, "id" DESC);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_logs_target_time_index"
  ON "audit_logs" ("target_id", "created_at" DESC, "id" DESC);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_logs_result_time_index"
  ON "audit_logs" ("result", "created_at" DESC, "id" DESC);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_logs_action_trgm_index"
  ON "audit_logs" USING gin ("action" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_logs_target_type_trgm_index"
  ON "audit_logs" USING gin ("target_type" gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_logs_trace_id_trgm_index"
  ON "audit_logs" USING gin ("trace_id" gin_trgm_ops);
