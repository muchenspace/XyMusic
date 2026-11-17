package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const performanceIndexLockName = "xymusic.performance-indexes"

type managedPerformanceIndex struct {
	name      string
	statement string
}

// largeLibraryIndexes are kept outside the squashed baseline migration so
// existing databases can build or heal them non-blockingly under an advisory
// lock during startup without rewriting applied migration history.
var largeLibraryIndexes = []managedPerformanceIndex{
	{
		name:      "tracks_admin_active_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_admin_active_updated_at_id_index ON tracks (updated_at DESC, id DESC) WHERE status <> 'ARCHIVED'`,
	},
	{
		name:      "tracks_admin_archived_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_admin_archived_updated_at_id_index ON tracks (updated_at DESC, id DESC) WHERE status = 'ARCHIVED'`,
	},
	{
		name:      "tracks_admin_ready_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_admin_ready_updated_at_id_index ON tracks (updated_at DESC, id DESC) WHERE status = 'READY' AND published_at IS NOT NULL AND duration_ms > 0`,
	},
	{
		name:      "tracks_created_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_created_at_id_index ON tracks (created_at, id)`,
	},
	{
		name:      "tracks_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_updated_at_id_index ON tracks (updated_at, id)`,
	},
	{
		name:      "albums_created_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_created_at_id_index ON albums (created_at, id)`,
	},
	{
		name:      "albums_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_updated_at_id_index ON albums (updated_at, id)`,
	},
	{
		name:      "albums_release_date_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_release_date_id_index ON albums (release_date, id)`,
	},
	{
		name:      "artists_created_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_created_at_id_index ON artists (created_at, id)`,
	},
	{
		name:      "artists_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_updated_at_id_index ON artists (updated_at, id)`,
	},
	{
		name:      "tracks_status_updated_at_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_status_updated_at_id_index ON tracks (status, updated_at, id)`,
	},
	{
		name:      "tracks_raw_title_trgm_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_raw_title_trgm_index ON tracks USING gin (title gin_trgm_ops)`,
	},
	{
		name:      "albums_raw_title_trgm_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_raw_title_trgm_index ON albums USING gin (title gin_trgm_ops)`,
	},
	{
		name:      "artists_raw_name_trgm_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_raw_name_trgm_index ON artists USING gin (name gin_trgm_ops)`,
	},
	{
		name:      "local_music_sources_source_path_trgm_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_source_path_trgm_index ON local_music_sources USING gin (source_path gin_trgm_ops)`,
	},
	{
		name:      "track_artists_artist_track_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS track_artists_artist_track_index ON track_artists (artist_id, track_id)`,
	},
	{
		name:      "album_artists_artist_album_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS album_artists_artist_album_index ON album_artists (artist_id, album_id)`,
	},
	{
		name:      "local_music_sources_root_checksum_status_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_root_checksum_status_index ON local_music_sources (root_id, checksum_sha256, status, last_seen_at)`,
	},
	{
		name:      "local_music_sources_root_status_seen_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_root_status_seen_index ON local_music_sources (root_id, status, last_seen_at)`,
	},
	{
		name:      "local_music_source_tracks_track_source_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_source_tracks_track_source_index ON local_music_source_tracks (track_id, source_id)`,
	},
	{
		name:      "library_scan_runs_root_status_lease_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS library_scan_runs_root_status_lease_index ON library_scan_runs (root_id, status, locked_until, created_at)`,
	},
	{
		name:      "library_scan_runs_claim_order_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS library_scan_runs_claim_order_index ON library_scan_runs (status, created_at, id) WHERE status IN ('PENDING', 'RUNNING')`,
	},
	{
		name:      "metadata_writeback_jobs_claim_order_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_claim_order_index ON metadata_writeback_jobs (status, next_attempt_at, created_at, id) WHERE status IN ('PENDING', 'PROCESSING')`,
	},
	{
		name:      "metadata_writeback_jobs_created_at_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_created_at_index ON metadata_writeback_jobs (created_at DESC, id DESC)`,
	},
	{
		name:      "metadata_writeback_jobs_track_latest_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_track_latest_index ON metadata_writeback_jobs (track_id, created_at DESC, id DESC)`,
	},
	{
		name:      "metadata_writeback_jobs_source_status_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_source_status_index ON metadata_writeback_jobs (source_id, status, created_at DESC)`,
	},
	{
		name:      "track_metadata_source_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS track_metadata_source_id_index ON track_metadata (source_id, track_id)`,
	},
	{
		name:      "track_metadata_has_artwork_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS track_metadata_has_artwork_index ON track_metadata ((raw_tags->>'hasArtwork')) WHERE raw_tags->>'hasArtwork' = 'true'`,
	},
	{
		name:      "tag_scraping_jobs_claim_order_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS tag_scraping_jobs_claim_order_index ON tag_scraping_jobs (status, next_attempt_at, created_at, id) WHERE status IN ('PENDING', 'RUNNING')`,
	},
	{
		name:      "artist_artwork_scraping_jobs_claim_order_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS artist_artwork_scraping_jobs_claim_order_index ON artist_artwork_scraping_jobs (status, next_attempt_at, created_at, id) WHERE status IN ('PENDING', 'RUNNING')`,
	},
	{
		name:      "lyrics_track_origin_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS lyrics_track_origin_index ON lyrics (track_id, origin)`,
	},
	{
		name:      "local_music_sources_source_asset_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_source_asset_index ON local_music_sources (source_asset_id) WHERE source_asset_id IS NOT NULL`,
	},
	{
		name:      "lyrics_asset_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS lyrics_asset_id_index ON lyrics (asset_id) WHERE asset_id IS NOT NULL`,
	},
	{
		name:      "media_uploads_asset_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS media_uploads_asset_id_index ON media_uploads (asset_id) WHERE asset_id IS NOT NULL`,
	},
	{
		name:      "artists_artwork_asset_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_artwork_asset_id_index ON artists (artwork_asset_id) WHERE artwork_asset_id IS NOT NULL`,
	},
	{
		name:      "albums_cover_asset_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_cover_asset_id_index ON albums (cover_asset_id) WHERE cover_asset_id IS NOT NULL`,
	},
	{
		name:      "playlists_cover_asset_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS playlists_cover_asset_id_index ON playlists (cover_asset_id) WHERE cover_asset_id IS NOT NULL`,
	},
	{
		name:      "user_profiles_avatar_asset_id_index",
		statement: `CREATE INDEX CONCURRENTLY IF NOT EXISTS user_profiles_avatar_asset_id_index ON user_profiles (avatar_asset_id) WHERE avatar_asset_id IS NOT NULL`,
	},
}

var obsoleteDuplicateIndexes = []string{
	"local_music_source_tracks_source_track_index",
}

// EnsureLargeLibraryIndexes upgrades both fresh and existing installations
// under a session-level advisory lock so concurrent serve/worker processes do
// not race on CREATE INDEX CONCURRENTLY, and heals interrupted invalid indexes.
func EnsureLargeLibraryIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool is required")
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire performance index connection: %w", err)
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", performanceIndexLockName); err != nil {
		return fmt.Errorf("acquire performance index advisory lock: %w", err)
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), migrationCleanupTimeout)
		defer cancel()
		_, _ = connection.Exec(cleanupContext, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", performanceIndexLockName)
	}()

	names := make([]string, 0, len(largeLibraryIndexes)+len(obsoleteDuplicateIndexes))
	for _, index := range largeLibraryIndexes {
		names = append(names, index.name)
	}
	names = append(names, obsoleteDuplicateIndexes...)

	existing, err := querySchemaIndexValidity(ctx, connection, names)
	if err != nil {
		return err
	}

	for _, obsolete := range obsoleteDuplicateIndexes {
		if _, present := existing[obsolete]; !present {
			continue
		}
		dropSQL := "DROP INDEX CONCURRENTLY IF EXISTS " + pgx.Identifier{obsolete}.Sanitize()
		if _, err := connection.Exec(ctx, dropSQL); err != nil {
			return fmt.Errorf("drop duplicate index %s: %w", obsolete, err)
		}
	}

	for _, index := range largeLibraryIndexes {
		valid, present := existing[index.name]
		if present && valid {
			continue
		}
		if present && !valid {
			dropSQL := "DROP INDEX CONCURRENTLY IF EXISTS " + pgx.Identifier{index.name}.Sanitize()
			if _, err := connection.Exec(ctx, dropSQL); err != nil {
				return fmt.Errorf("drop invalid performance index %s: %w", index.name, err)
			}
		}
		if _, err := connection.Exec(ctx, index.statement); err != nil {
			return fmt.Errorf("ensure large-library index %s: %w", index.name, err)
		}
	}
	return nil
}

func querySchemaIndexValidity(ctx context.Context, connection *pgxpool.Conn, names []string) (map[string]bool, error) {
	rows, err := connection.Query(ctx, `
		SELECT cls.relname, idx.indisvalid
		FROM pg_index idx
		JOIN pg_class cls ON cls.oid = idx.indexrelid
		JOIN pg_namespace nsp ON nsp.oid = cls.relnamespace
		WHERE nsp.nspname = current_schema()
		  AND cls.relname = ANY($1::text[])
	`, names)
	if err != nil {
		return nil, fmt.Errorf("query existing performance indexes: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool, len(names))
	for rows.Next() {
		var name string
		var valid bool
		if err := rows.Scan(&name, &valid); err != nil {
			return nil, fmt.Errorf("scan existing performance index: %w", err)
		}
		result[name] = valid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing performance indexes: %w", err)
	}
	return result, nil
}
