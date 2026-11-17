CREATE INDEX IF NOT EXISTS local_music_sources_source_asset_index ON local_music_sources (source_asset_id) WHERE source_asset_id IS NOT NULL;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS lyrics_asset_id_index ON lyrics (asset_id) WHERE asset_id IS NOT NULL;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS media_uploads_asset_id_index ON media_uploads (asset_id) WHERE asset_id IS NOT NULL;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS artists_artwork_asset_id_index ON artists (artwork_asset_id) WHERE artwork_asset_id IS NOT NULL;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS albums_cover_asset_id_index ON albums (cover_asset_id) WHERE cover_asset_id IS NOT NULL;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS playlists_cover_asset_id_index ON playlists (cover_asset_id) WHERE cover_asset_id IS NOT NULL;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS user_profiles_avatar_asset_id_index ON user_profiles (avatar_asset_id) WHERE avatar_asset_id IS NOT NULL;
