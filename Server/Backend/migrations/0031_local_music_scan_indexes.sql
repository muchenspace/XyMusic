CREATE INDEX IF NOT EXISTS "local_music_sources_root_seen_index"
  ON "local_music_sources" ("root_id", "last_seen_at");
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "local_music_sources_root_checksum_index"
  ON "local_music_sources" ("root_id", "checksum_sha256", "last_seen_at");
