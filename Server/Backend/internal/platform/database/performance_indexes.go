package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// largeLibraryIndexStatements are kept outside the squashed baseline
// migration. The project intentionally ships one immutable baseline migration
// and existing databases must not fail hash compatibility after a performance
// release. These idempotent, non-blocking index builds are therefore applied
// during startup and are safe to run more than once.
var largeLibraryIndexStatements = []string{
	// Partial order indexes keep the common active/archived catalog scans
	// small while preserving the stable (timestamp, id) keyset order.
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_admin_active_updated_at_id_index ON tracks (updated_at DESC, id DESC) WHERE status <> 'ARCHIVED'`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_admin_archived_updated_at_id_index ON tracks (updated_at DESC, id DESC) WHERE status = 'ARCHIVED'`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_admin_ready_updated_at_id_index ON tracks (updated_at DESC, id DESC) WHERE status = 'READY' AND published_at IS NOT NULL AND duration_ms > 0`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_created_at_id_index ON tracks (created_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_updated_at_id_index ON tracks (updated_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_created_at_id_index ON albums (created_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_updated_at_id_index ON albums (updated_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_release_date_id_index ON albums (release_date, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_created_at_id_index ON artists (created_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_updated_at_id_index ON artists (updated_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_status_updated_at_id_index ON tracks (status, updated_at, id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tracks_raw_title_trgm_index ON tracks USING gin (title gin_trgm_ops)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS albums_raw_title_trgm_index ON albums USING gin (title gin_trgm_ops)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS artists_raw_name_trgm_index ON artists USING gin (name gin_trgm_ops)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_source_path_trgm_index ON local_music_sources USING gin (source_path gin_trgm_ops)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS track_artists_artist_track_index ON track_artists (artist_id, track_id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS album_artists_artist_album_index ON album_artists (artist_id, album_id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_root_checksum_status_index ON local_music_sources (root_id, checksum_sha256, status, last_seen_at)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_sources_root_status_seen_index ON local_music_sources (root_id, status, last_seen_at)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_source_tracks_track_source_index ON local_music_source_tracks (track_id, source_id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS local_music_source_tracks_source_track_index ON local_music_source_tracks (source_id, track_id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS library_scan_runs_root_status_lease_index ON library_scan_runs (root_id, status, locked_until, created_at)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS library_scan_runs_claim_order_index ON library_scan_runs (status, created_at, id) WHERE status IN ('PENDING', 'RUNNING')`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_claim_order_index ON metadata_writeback_jobs (status, next_attempt_at, created_at, id) WHERE status IN ('PENDING', 'PROCESSING')`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_created_at_index ON metadata_writeback_jobs (created_at DESC, id DESC)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_track_latest_index ON metadata_writeback_jobs (track_id, created_at DESC, id DESC)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS metadata_writeback_jobs_source_status_index ON metadata_writeback_jobs (source_id, status, created_at DESC)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS track_metadata_source_id_index ON track_metadata (source_id, track_id)`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS track_metadata_has_artwork_index ON track_metadata ((raw_tags->>'hasArtwork')) WHERE raw_tags->>'hasArtwork' = 'true'`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS tag_scraping_jobs_claim_order_index ON tag_scraping_jobs (status, next_attempt_at, created_at, id) WHERE status IN ('PENDING', 'RUNNING')`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS artist_artwork_scraping_jobs_claim_order_index ON artist_artwork_scraping_jobs (status, next_attempt_at, created_at, id) WHERE status IN ('PENDING', 'RUNNING')`,
	`CREATE INDEX CONCURRENTLY IF NOT EXISTS lyrics_track_origin_index ON lyrics (track_id, origin)`,
}

// EnsureLargeLibraryIndexes upgrades both fresh and existing installations
// without changing the immutable migration journal. It deliberately executes
// one statement per call because CREATE INDEX CONCURRENTLY cannot run inside a
// transaction.
func EnsureLargeLibraryIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool is required")
	}
	for index, statement := range largeLibraryIndexStatements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("ensure large-library index %d: %w", index, err)
		}
	}
	return nil
}
