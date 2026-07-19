CREATE INDEX "playlist_tracks_latest_cover_index"
ON "playlist_tracks" ("playlist_id", "added_at" DESC, "id" DESC);
