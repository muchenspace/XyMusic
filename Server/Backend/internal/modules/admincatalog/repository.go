package admincatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/audiostatus"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func catalogSeekCondition(
	column, idColumn, sort string, order SortOrder, cursor *ListCursor, nullable bool, arguments *[]any,
) (string, error) {
	if cursor == nil || cursor.ID == "" {
		return "", fmt.Errorf("catalog cursor is invalid")
	}
	idPosition := appendArgument(arguments, cursor.ID)
	operator := ">"
	if order == SortDescending {
		operator = "<"
	}
	if nullable {
		if cursor.Null {
			return fmt.Sprintf("(%s IS NULL AND %s %s $%d)", column, idColumn, operator, idPosition), nil
		}
		valuePosition := appendArgument(arguments, cursor.Value)
		value := fmt.Sprintf("$%d::date", valuePosition)
		if order == SortDescending {
			return fmt.Sprintf("(%s < %s OR (%s = %s AND %s < $%d))", column, value, column, value, idColumn, idPosition), nil
		}
		return fmt.Sprintf("(%s > %s OR (%s = %s AND %s > $%d) OR %s IS NULL)", column, value, column, value, idColumn, idPosition, column), nil
	}
	valuePosition := appendArgument(arguments, cursor.Value)
	value := fmt.Sprintf("$%d", valuePosition)
	if sort == "createdAt" || sort == "updatedAt" {
		value += "::timestamptz"
	}
	return fmt.Sprintf("(%s %s %s OR (%s = %s AND %s %s $%d))",
		column, operator, value, column, value, idColumn, operator, idPosition), nil
}

type pageCountResult struct {
	total int
	err   error
}

func startPageCount(
	ctx context.Context,
	enabled bool,
	count func(context.Context) (int, error),
) (chan pageCountResult, context.CancelFunc) {
	if !enabled {
		return nil, nil
	}
	countCtx, cancel := context.WithCancel(ctx)
	result := make(chan pageCountResult, 1)
	go func() {
		total, err := count(countCtx)
		result <- pageCountResult{total: total, err: err}
	}()
	return result, cancel
}

func (repository *Repository) countAdminTracks(
	ctx context.Context,
	query TrackQuery,
	baseConditions []string,
	arguments []any,
	total *int,
) error {
	if total == nil {
		return fmt.Errorf("track count destination is required")
	}
	var statement string
	var countArguments []any
	// These two common catalog views can be counted without joining albums,
	// source mappings, or evaluating the derived audio state. This matters on
	// million-row libraries, where COUNT over the full projection is needlessly
	// expensive.
	if query.Search == "" && query.SourceID == "" && query.MetadataStatus == "" {
		switch query.Status {
		case "":
			statement = `SELECT count(*)::int FROM tracks t WHERE t.status <> 'ARCHIVED'`
		case AudioStatusArchived:
			statement = `SELECT count(*)::int FROM tracks t WHERE t.status = 'ARCHIVED'`
		case AudioStatusReady:
			statement = `SELECT count(*)::int FROM tracks t WHERE ` + adminReadyTrackConditionSQL
		}
	}
	if statement == "" {
		statement = `SELECT count(*)::int` + trackFromSQL + " WHERE " + strings.Join(baseConditions, " AND ")
		countArguments = arguments
	}
	if err := repository.pool.QueryRow(ctx, statement, countArguments...).Scan(total); err != nil {
		return fmt.Errorf("count admin tracks: %w", err)
	}
	return nil
}

// recordsWithoutLookahead prevents a cursor page's probe row from entering
// expensive secondary queries. The service always requests pageSize+1 when
// HasNextProbe is set, so a full result means the final row is not visible.
func recordsWithoutLookahead[T any](records []T, limit int, cursorMode, hasNextProbe bool) []T {
	if hasNextProbe && cursorMode && limit > 1 && len(records) == limit {
		return records[:len(records)-1]
	}
	return records
}

func scanArtists(rows pgx.Rows) ([]ArtistRecord, error) {
	return scanArtistsWithCapacity(rows, 0)
}

func scanArtistsWithCapacity(rows pgx.Rows, capacity int) ([]ArtistRecord, error) {
	result := make([]ArtistRecord, 0, max(0, capacity))
	for rows.Next() {
		var record ArtistRecord
		if err := rows.Scan(
			&record.ID, &record.Name, &record.NormalizedName, &record.ArtworkAssetID,
			&record.Description, &record.Version, &record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admin artist: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin artists: %w", err)
	}
	return result, nil
}

func scanAlbums(rows pgx.Rows) ([]AlbumRecord, error) {
	return scanAlbumsWithCapacity(rows, 0)
}

func scanAlbumsWithCapacity(rows pgx.Rows, capacity int) ([]AlbumRecord, error) {
	result := make([]AlbumRecord, 0, max(0, capacity))
	for rows.Next() {
		var record AlbumRecord
		if err := rows.Scan(
			&record.ID, &record.Title, &record.NormalizedTitle, &record.Description,
			&record.CoverAssetID, &record.ReleaseDate, &record.Version, &record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admin album: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin albums: %w", err)
	}
	return result, nil
}

func scanTracks(rows pgx.Rows) ([]TrackRecord, error) {
	return scanTracksWithCapacity(rows, 0)
}

func scanTracksWithCapacity(rows pgx.Rows, capacity int) ([]TrackRecord, error) {
	result := make([]TrackRecord, 0, max(0, capacity))
	for rows.Next() {
		var record TrackRecord
		if err := rows.Scan(
			&record.ID, &record.AlbumID, &record.AlbumTitle, &record.AlbumCoverAssetID,
			&record.Title, &record.NormalizedTitle, &record.TrackNumber, &record.DiscNumber, &record.DurationMS,
			&record.Status, &record.AudioStatus, &record.Version, &record.PublishedAt, &record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admin track: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin tracks: %w", err)
	}
	return result, nil
}

func closeRows(rows pgx.Rows, operation string) error {
	err := rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func sqlDirection(order SortOrder) string {
	if order == SortDescending {
		return "DESC"
	}
	return "ASC"
}

func appendArgument(arguments *[]any, value any) int {
	*arguments = append(*arguments, value)
	return len(*arguments)
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func artistIDs(records []ArtistRecord) []string {
	result := make([]string, 0, len(records))
	for _, record := range records {
		result = append(result, record.ID)
	}
	return result
}

func albumIDs(records []AlbumRecord) []string {
	result := make([]string, 0, len(records))
	for _, record := range records {
		result = append(result, record.ID)
	}
	return result
}

func trackIDs(records []TrackRecord) []string {
	result := make([]string, 0, len(records))
	for _, record := range records {
		result = append(result, record.ID)
	}
	return result
}

func nonNilCredits(input []CreditRecord) []CreditRecord {
	if input == nil {
		return []CreditRecord{}
	}
	return input
}

const artistSelectSQL = `
	SELECT id, name, normalized_name, artwork_asset_id, description,
	       version, created_at, updated_at
	FROM artists
`

// Archived tracks remain linked so they can be restored; parent catalog entries
// are therefore considered archived when no non-archived track still references them.
const adminArtistHasActiveTracksSQL = `(
	EXISTS (
		SELECT 1
		FROM track_artists credit
		JOIN tracks track ON track.id = credit.track_id
		WHERE credit.artist_id = artists.id AND track.status <> 'ARCHIVED'
	)
	OR EXISTS (
		SELECT 1
		FROM album_artists credit
		JOIN tracks track ON track.album_id = credit.album_id
		WHERE credit.artist_id = artists.id AND track.status <> 'ARCHIVED'
	)
)`

const adminAlbumHasActiveTracksSQL = `EXISTS (
	SELECT 1
	FROM tracks track
	WHERE track.album_id = al.id AND track.status <> 'ARCHIVED'
)`

const albumSelectSQL = `
	SELECT al.id, al.title, al.normalized_title, al.description, al.cover_asset_id,
	       al.release_date::text, al.version, al.created_at, al.updated_at
	FROM albums al
`

var trackFromSQL = `
	FROM tracks t
	LEFT JOIN albums al ON al.id = t.album_id
	CROSS JOIN LATERAL (
		SELECT ` + audiostatus.Expression("t") + ` AS value
	) audio_status
`

var trackSelectSQL = `
	SELECT t.id, t.album_id, al.title, al.cover_asset_id, t.title, t.normalized_title,
	       t.track_number, t.disc_number, t.duration_ms, t.status::text,
	       audio_status.value, t.version, t.published_at, t.created_at, t.updated_at
` + trackFromSQL

// adminReadyTrackConditionSQL mirrors the READY branch in
// audiostatus.Expression("t"). It keeps the alias fixed because this is
// internal SQL, not request input. The predicate is reused by the page query
// and the exact first-page count.
const adminReadyTrackConditionSQL = `(
	t.status = 'READY'
	AND t.published_at IS NOT NULL
	AND t.duration_ms > 0
	AND NOT EXISTS (
		SELECT 1
		FROM local_music_source_tracks scan_mapping
		JOIN local_music_sources scan_source ON scan_source.id = scan_mapping.source_id
		JOIN library_scan_runs active_scan ON active_scan.root_id = scan_source.root_id
		WHERE scan_mapping.track_id = t.id
		  AND active_scan.status IN ('PENDING', 'RUNNING')
		  AND scan_source.last_seen_at < COALESCE(active_scan.started_at, active_scan.created_at)
	)
	AND (
		EXISTS (
			SELECT 1
			FROM local_music_source_tracks ready_mapping
			JOIN local_music_sources ready_source ON ready_source.id = ready_mapping.source_id
			WHERE ready_mapping.track_id = t.id
			  AND ready_source.status = 'READY'
		)
		OR (
			EXISTS (
				SELECT 1
				FROM media_assets ready_asset
				WHERE ready_asset.id = t.source_asset_id
				  AND ready_asset.status = 'READY'
			)
			AND NOT EXISTS (
				SELECT 1
				FROM local_music_source_tracks failed_mapping
				JOIN local_music_sources failed_source ON failed_source.id = failed_mapping.source_id
				WHERE failed_mapping.track_id = t.id
				  AND failed_source.status IN ('FAILED', 'MISSING')
			)
		)
	)
)`

const metadataStatusConditionSQL = `COALESCE((
	SELECT CASE
		WHEN latest.status IN ('PENDING', 'PROCESSING') THEN 'PENDING_WRITE'
		WHEN latest.status = 'FAILED' AND latest.metadata_version = metadata.version THEN 'WRITE_FAILED'
		WHEN latest.status = 'CANCELLED' AND latest.metadata_version = metadata.version
			AND (latest.last_error_code IS NOT NULL OR latest.last_error IS NOT NULL) THEN 'WRITE_FAILED'
		ELSE 'NORMAL'
	END
	FROM track_metadata metadata
	LEFT JOIN LATERAL (
		SELECT job.status::text AS status, job.metadata_version, job.last_error_code, job.last_error
		FROM metadata_writeback_jobs job
		WHERE job.track_id = metadata.track_id
		ORDER BY job.created_at DESC, job.id DESC LIMIT 1
	) latest ON true
	WHERE metadata.track_id = t.id
), 'NORMAL') = $%d`

func writebackHasTerminalError(status string, errorCode, message *string) bool {
	return status == "FAILED" || status == "CANCELLED" && (errorCode != nil || message != nil)
}

var _ Store = (*Repository)(nil)
