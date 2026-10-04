package admintagscraping

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

// mediaTxAdapter narrows the concrete pgx transaction to the module MediaTx
// contract passed to batch mutation fences.
type mediaTxAdapter struct {
	tx pgx.Tx
}

func (adapter mediaTxAdapter) QueryRow(ctx context.Context, sql string, args ...any) MediaRow {
	return adapter.tx.QueryRow(ctx, sql, args...)
}

func (adapter mediaTxAdapter) Exec(ctx context.Context, sql string, args ...any) (MediaCommandTag, error) {
	return adapter.tx.Exec(ctx, sql, args...)
}

type metadataDatabase interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

var _ Store = (*Repository)(nil)
var _ BatchClaimStore = (*Repository)(nil)
var _ BatchCompleteStore = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

var metadataFields = []string{
	"title", "credits", "albumArtists", "album", "releaseDate", "trackNumber", "trackTotal",
	"discNumber", "discTotal", "genres", "bpm", "isrc", "comment", "copyright", "lyrics",
}

const batchJobSelect = `
	SELECT id, requested_by, options, status::text, total, processed, succeeded, failed,
	       cancel_requested, started_at, completed_at, created_at, updated_at
	FROM tag_scraping_jobs`

const batchItemSelect = `
	SELECT id, job_id, track_id, expected_version, position, status::text,
	       attempts, max_attempts, next_attempt_at, attempt_id, locked_by, locked_until, candidate, source, message,
	       started_at, completed_at, created_at, updated_at
	FROM tag_scraping_job_items`

const claimedBatchItemSelect = `
	SELECT item.id, item.job_id, item.track_id, item.expected_version, item.position, item.status::text,
	       item.attempts, item.max_attempts, item.next_attempt_at,
	       item.attempt_id, item.locked_by, item.locked_until, item.candidate, item.source, item.message,
	       item.started_at, item.completed_at, item.created_at, item.updated_at,
	       item.track_id, COALESCE(metadata.raw_tags, '{}'::jsonb), COALESCE(metadata.overrides, '{}'::jsonb),
	       COALESCE(metadata.version, 0), metadata.last_scanned_at, metadata.updated_by,
	       COALESCE(metadata.created_at, item.created_at), COALESCE(metadata.updated_at, item.updated_at),
	       source.id, source.root_id, source.source_path, source.status, source.checksum_sha256,
	       root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
			       ), track.status::text,
	       COALESCE(mapping_stats.mapping_count, 0)
	FROM tag_scraping_job_items item
	JOIN tracks track ON track.id = item.track_id
	LEFT JOIN track_metadata metadata ON metadata.track_id = item.track_id
	LEFT JOIN local_music_sources source ON source.id = metadata.source_id
	LEFT JOIN library_roots root ON root.id = source.root_id
	LEFT JOIN LATERAL (
		SELECT count(*)::int AS mapping_count
		FROM local_music_source_tracks mapping WHERE mapping.source_id = source.id
	) mapping_stats ON true`
