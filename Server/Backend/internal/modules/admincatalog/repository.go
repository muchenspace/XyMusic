package admincatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/audiostatus"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) ListArtists(
	ctx context.Context,
	query ArtistQuery,
) ([]ArtistRecord, int, error) {
	arguments := make([]any, 0, 3)
	where := ""
	if query.Search != "" {
		position := appendArgument(&arguments, "%"+escapeLike(query.Search)+"%")
		where = fmt.Sprintf(` WHERE (name ILIKE $%d ESCAPE E'\\' OR description ILIKE $%d ESCAPE E'\\')`, position, position)
	}
	countArguments := append([]any(nil), arguments...)
	column := map[string]string{"name": "normalized_name", "createdAt": "created_at", "updatedAt": "updated_at"}[query.Sort]
	if column == "" {
		return nil, 0, fmt.Errorf("unsupported artist sort %q", query.Sort)
	}
	direction := sqlDirection(query.Order)
	limitPosition := appendArgument(&arguments, query.Limit)
	offsetPosition := appendArgument(&arguments, query.Offset)
	rows, err := repository.pool.Query(ctx, artistSelectSQL+where+fmt.Sprintf(
		" ORDER BY %s %s, id %s LIMIT $%d OFFSET $%d", column, direction, direction, limitPosition, offsetPosition,
	), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin artists: %w", err)
	}
	records, err := scanArtists(rows)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err := repository.enrichArtists(ctx, records); err != nil {
		return nil, 0, err
	}
	var total int
	if err := repository.pool.QueryRow(ctx, "SELECT count(*)::int FROM artists"+where, countArguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin artists: %w", err)
	}
	return records, total, nil
}

func (repository *Repository) FindArtist(ctx context.Context, id string) (ArtistRecord, error) {
	rows, err := repository.pool.Query(ctx, artistSelectSQL+" WHERE id = $1 LIMIT 1", id)
	if err != nil {
		return ArtistRecord{}, fmt.Errorf("query admin artist: %w", err)
	}
	records, scanErr := scanArtists(rows)
	rows.Close()
	if scanErr != nil {
		return ArtistRecord{}, scanErr
	}
	if len(records) == 0 {
		return ArtistRecord{}, apperror.NotFound("Artist was not found")
	}
	if err := repository.enrichArtists(ctx, records); err != nil {
		return ArtistRecord{}, err
	}
	return records[0], nil
}

func (repository *Repository) enrichArtists(ctx context.Context, records []ArtistRecord) error {
	if len(records) == 0 {
		return nil
	}
	ids := artistIDs(records)
	for _, aggregate := range []struct {
		statement string
		album     bool
	}{
		{`SELECT artist_id, count(DISTINCT album_id)::int FROM album_artists WHERE artist_id = ANY($1::uuid[]) GROUP BY artist_id`, true},
		{`SELECT artist_id, count(DISTINCT track_id)::int FROM track_artists WHERE artist_id = ANY($1::uuid[]) GROUP BY artist_id`, false},
	} {
		rows, err := repository.pool.Query(ctx, aggregate.statement, ids)
		if err != nil {
			return fmt.Errorf("query admin artist counts: %w", err)
		}
		counts := make(map[string]int, len(records))
		for rows.Next() {
			var id string
			var count int
			if err := rows.Scan(&id, &count); err != nil {
				rows.Close()
				return fmt.Errorf("scan admin artist count: %w", err)
			}
			counts[id] = count
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("iterate admin artist counts: %w", err)
		}
		for index := range records {
			if aggregate.album {
				records[index].AlbumCount = counts[records[index].ID]
			} else {
				records[index].TrackCount = counts[records[index].ID]
			}
		}
	}
	return nil
}

func (repository *Repository) ListAlbums(
	ctx context.Context,
	query AlbumQuery,
) ([]AlbumRecord, int, error) {
	arguments := make([]any, 0, 3)
	where := ""
	if query.Search != "" {
		position := appendArgument(&arguments, "%"+escapeLike(query.Search)+"%")
		where = fmt.Sprintf(` WHERE (al.title ILIKE $%d ESCAPE E'\\' OR EXISTS (
			SELECT 1 FROM album_artists credit
			JOIN artists artist ON artist.id = credit.artist_id
			WHERE credit.album_id = al.id AND artist.name ILIKE $%d ESCAPE E'\\'
		))`, position, position)
	}
	countArguments := append([]any(nil), arguments...)
	column := map[string]string{
		"title": "al.normalized_title", "createdAt": "al.created_at",
		"updatedAt": "al.updated_at", "releaseDate": "al.release_date",
	}[query.Sort]
	if column == "" {
		return nil, 0, fmt.Errorf("unsupported album sort %q", query.Sort)
	}
	direction := sqlDirection(query.Order)
	limitPosition := appendArgument(&arguments, query.Limit)
	offsetPosition := appendArgument(&arguments, query.Offset)
	rows, err := repository.pool.Query(ctx, albumSelectSQL+where+fmt.Sprintf(
		" ORDER BY %s %s, al.id %s LIMIT $%d OFFSET $%d", column, direction, direction, limitPosition, offsetPosition,
	), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin albums: %w", err)
	}
	records, err := scanAlbums(rows)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err := repository.enrichAlbums(ctx, records); err != nil {
		return nil, 0, err
	}
	var total int
	if err := repository.pool.QueryRow(ctx, "SELECT count(*)::int FROM albums al"+where, countArguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin albums: %w", err)
	}
	return records, total, nil
}

func (repository *Repository) FindDuplicateAlbums(ctx context.Context, query DuplicateAlbumQuery) (DuplicateAlbumPage, error) {
	var albumID any
	if query.AlbumID != "" {
		albumID = query.AlbumID
	}
	albumLimit := query.AlbumLimit
	if albumLimit <= 0 {
		albumLimit = 100
	}
	albumOffset := max(0, query.AlbumOffset)
	var result DuplicateAlbumPage
	if err := repository.pool.QueryRow(ctx, `
		WITH duplicate_groups AS (
			SELECT normalized_title, count(*)::int AS album_count
			FROM albums GROUP BY normalized_title HAVING count(*) > 1
		)
		SELECT count(*)::int,
		       COALESCE(sum(album_count - 1), 0)::int,
		       count(*) FILTER (WHERE $1::uuid IS NULL OR normalized_title = (
			   SELECT normalized_title FROM albums WHERE id = $1::uuid
		   ))::int
		FROM duplicate_groups`, albumID).Scan(
		&result.GroupCount, &result.DuplicateAlbumCount, &result.Total,
	); err != nil {
		return DuplicateAlbumPage{}, fmt.Errorf("count duplicate album groups: %w", err)
	}
	groupRows, err := repository.pool.Query(ctx, `
		WITH duplicate_groups AS (
			SELECT normalized_title, min(title) AS title, count(*)::int AS album_count
			FROM albums GROUP BY normalized_title HAVING count(*) > 1
		)
		SELECT normalized_title, title, album_count FROM duplicate_groups
		WHERE $3::uuid IS NULL OR normalized_title = (
			SELECT normalized_title FROM albums WHERE id = $3::uuid
		)
		ORDER BY normalized_title ASC LIMIT $1 OFFSET $2
	`, query.Limit, query.Offset, albumID)
	if err != nil {
		return DuplicateAlbumPage{}, fmt.Errorf("query duplicate album groups: %w", err)
	}
	keys := make([]string, 0)
	groupsByKey := make(map[string]int)
	for groupRows.Next() {
		var group DuplicateAlbumGroupPage
		if err := groupRows.Scan(&group.Key, &group.Title, &group.AlbumTotal); err != nil {
			groupRows.Close()
			return DuplicateAlbumPage{}, fmt.Errorf("scan duplicate album group: %w", err)
		}
		group.Albums = []AlbumRecord{}
		groupsByKey[group.Key] = len(result.Groups)
		keys = append(keys, group.Key)
		result.Groups = append(result.Groups, group)
	}
	if err := closeRows(groupRows, "iterate duplicate album groups"); err != nil {
		return DuplicateAlbumPage{}, err
	}
	if len(keys) == 0 {
		return result, nil
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT al.id, al.title, al.normalized_title, al.description, al.cover_asset_id,
		       al.release_date::text, al.version, al.created_at, al.updated_at
		FROM unnest($1::text[]) WITH ORDINALITY selected(normalized_title, position)
		JOIN LATERAL (
			SELECT source.id, source.title, source.normalized_title, source.description,
			       source.cover_asset_id, source.release_date, source.version,
			       source.created_at, source.updated_at
			FROM albums source
			WHERE source.normalized_title = selected.normalized_title
			ORDER BY source.id ASC LIMIT $2 OFFSET $3
		) al ON TRUE
		ORDER BY selected.position ASC, al.id ASC
	`, keys, albumLimit, albumOffset)
	if err != nil {
		return DuplicateAlbumPage{}, fmt.Errorf("query duplicate album members: %w", err)
	}
	records, err := scanAlbums(rows)
	rows.Close()
	if err != nil {
		return DuplicateAlbumPage{}, err
	}
	if err := repository.enrichAlbums(ctx, records); err != nil {
		return DuplicateAlbumPage{}, err
	}
	for _, record := range records {
		if index, exists := groupsByKey[record.NormalizedTitle]; exists {
			result.Groups[index].Albums = append(result.Groups[index].Albums, record)
		}
	}
	return result, nil
}

func (repository *Repository) FindAlbum(ctx context.Context, id string, limit, offset int) (AlbumRecord, []TrackRecord, int, error) {
	rows, err := repository.pool.Query(ctx, albumSelectSQL+" WHERE al.id = $1 LIMIT 1", id)
	if err != nil {
		return AlbumRecord{}, nil, 0, fmt.Errorf("query admin album: %w", err)
	}
	records, scanErr := scanAlbums(rows)
	rows.Close()
	if scanErr != nil {
		return AlbumRecord{}, nil, 0, scanErr
	}
	if len(records) == 0 {
		return AlbumRecord{}, nil, 0, apperror.NotFound("Album was not found")
	}
	if err := repository.enrichAlbums(ctx, records); err != nil {
		return AlbumRecord{}, nil, 0, err
	}
	trackRows, err := repository.pool.Query(ctx, trackSelectSQL+`
		WHERE t.album_id = $1
		ORDER BY t.disc_number ASC NULLS LAST, t.track_number ASC NULLS LAST,
		         t.normalized_title ASC, t.id ASC
		LIMIT $2 OFFSET $3
	`, id, limit, offset)
	if err != nil {
		return AlbumRecord{}, nil, 0, fmt.Errorf("query admin album tracks: %w", err)
	}
	tracks, err := scanTracks(trackRows)
	trackRows.Close()
	if err != nil {
		return AlbumRecord{}, nil, 0, err
	}
	if err := repository.enrichTracks(ctx, tracks); err != nil {
		return AlbumRecord{}, nil, 0, err
	}
	return records[0], tracks, records[0].TrackCount, nil
}

func (repository *Repository) enrichAlbums(ctx context.Context, records []AlbumRecord) error {
	if len(records) == 0 {
		return nil
	}
	ids := albumIDs(records)
	rows, err := repository.pool.Query(ctx, `
		SELECT credit.album_id, artist.id, artist.name, credit.role::text, credit.sort_order
		FROM album_artists credit
		JOIN artists artist ON artist.id = credit.artist_id
		WHERE credit.album_id = ANY($1::uuid[])
		ORDER BY credit.album_id, credit.sort_order, artist.name
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin album credits: %w", err)
	}
	credits := make(map[string][]CreditRecord, len(records))
	for rows.Next() {
		var albumID string
		var credit CreditRecord
		if err := rows.Scan(&albumID, &credit.ArtistID, &credit.ArtistName, &credit.Role, &credit.SortOrder); err != nil {
			rows.Close()
			return fmt.Errorf("scan admin album credit: %w", err)
		}
		credits[albumID] = append(credits[albumID], credit)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("iterate admin album credits: %w", err)
	}
	countRows, err := repository.pool.Query(ctx, `
		SELECT album_id, count(*)::int FROM tracks
		WHERE album_id = ANY($1::uuid[]) GROUP BY album_id
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin album track counts: %w", err)
	}
	counts := make(map[string]int, len(records))
	for countRows.Next() {
		var albumID string
		var count int
		if err := countRows.Scan(&albumID, &count); err != nil {
			countRows.Close()
			return fmt.Errorf("scan admin album track count: %w", err)
		}
		counts[albumID] = count
	}
	err = countRows.Err()
	countRows.Close()
	if err != nil {
		return fmt.Errorf("iterate admin album track counts: %w", err)
	}
	for index := range records {
		records[index].Credits = nonNilCredits(credits[records[index].ID])
		records[index].TrackCount = counts[records[index].ID]
	}
	return nil
}

func (repository *Repository) ListTracks(
	ctx context.Context,
	query TrackQuery,
) ([]TrackRecord, int, error) {
	arguments := make([]any, 0, 8)
	conditions := make([]string, 0, 4)
	if query.Status != "" {
		conditions = append(conditions, fmt.Sprintf("audio_status.value = $%d", appendArgument(&arguments, query.Status)))
	} else {
		conditions = append(conditions, "audio_status.value <> 'ARCHIVED'")
	}
	if query.SourceID != "" {
		position := appendArgument(&arguments, query.SourceID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM local_music_source_tracks mapped
			JOIN local_music_sources source ON source.id = mapped.source_id
			WHERE mapped.track_id = t.id AND source.root_id = $%d
		)`, position))
	}
	if query.MetadataStatus != "" {
		position := appendArgument(&arguments, query.MetadataStatus)
		conditions = append(conditions, fmt.Sprintf(metadataStatusConditionSQL, position))
	}
	if query.Search != "" {
		position := appendArgument(&arguments, "%"+escapeLike(query.Search)+"%")
		conditions = append(conditions, fmt.Sprintf(`(
			t.title ILIKE $%d ESCAPE E'\\' OR al.title ILIKE $%d ESCAPE E'\\' OR
			EXISTS (SELECT 1 FROM track_artists credit JOIN artists artist ON artist.id = credit.artist_id
			        WHERE credit.track_id = t.id AND artist.name ILIKE $%d ESCAPE E'\\') OR
			EXISTS (SELECT 1 FROM local_music_source_tracks mapped
			        JOIN local_music_sources source ON source.id = mapped.source_id
			        WHERE mapped.track_id = t.id AND source.source_path ILIKE $%d ESCAPE E'\\')
		)`, position, position, position, position))
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	countArguments := append([]any(nil), arguments...)
	column := map[string]string{
		"title": "t.normalized_title", "createdAt": "t.created_at",
		"updatedAt": "t.updated_at", "status": "audio_status.value",
	}[query.Sort]
	if column == "" {
		return nil, 0, fmt.Errorf("unsupported track sort %q", query.Sort)
	}
	direction := sqlDirection(query.Order)
	limitPosition := appendArgument(&arguments, query.Limit)
	offsetPosition := appendArgument(&arguments, query.Offset)
	rows, err := repository.pool.Query(ctx, trackSelectSQL+where+fmt.Sprintf(
		" ORDER BY %s %s, t.id %s LIMIT $%d OFFSET $%d", column, direction, direction, limitPosition, offsetPosition,
	), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin tracks: %w", err)
	}
	records, err := scanTracks(rows)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err := repository.enrichTracks(ctx, records); err != nil {
		return nil, 0, err
	}
	var total int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*)::int`+trackFromSQL+where, countArguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin tracks: %w", err)
	}
	return records, total, nil
}

func (repository *Repository) FindTrack(ctx context.Context, id string, lyricLimit, lyricOffset int) (TrackRecord, int, error) {
	rows, err := repository.pool.Query(ctx, trackSelectSQL+" WHERE t.id = $1 LIMIT 1", id)
	if err != nil {
		return TrackRecord{}, 0, fmt.Errorf("query admin track: %w", err)
	}
	records, scanErr := scanTracks(rows)
	rows.Close()
	if scanErr != nil {
		return TrackRecord{}, 0, scanErr
	}
	if len(records) == 0 {
		return TrackRecord{}, 0, apperror.NotFound("Track was not found")
	}
	if err := repository.enrichTracks(ctx, records); err != nil {
		return TrackRecord{}, 0, err
	}
	lyrics, lyricTotal, err := repository.listLyrics(ctx, id, lyricLimit, lyricOffset)
	if err != nil {
		return TrackRecord{}, 0, err
	}
	records[0].Lyrics = lyrics
	return records[0], lyricTotal, nil
}

func (repository *Repository) listLyrics(ctx context.Context, trackID string, limit, offset int) ([]LyricRecord, int, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id, language, format::text, content, is_default, version, updated_at
		FROM lyrics
		WHERE track_id = $1
		ORDER BY is_default DESC, language ASC, id ASC
		LIMIT $2 OFFSET $3
	`, trackID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin track lyrics: %w", err)
	}
	lyrics := make([]LyricRecord, 0)
	for rows.Next() {
		var lyric LyricRecord
		if err := rows.Scan(
			&lyric.ID, &lyric.Language, &lyric.Format, &lyric.Content,
			&lyric.IsDefault, &lyric.Version, &lyric.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan admin track lyric: %w", err)
		}
		lyrics = append(lyrics, lyric)
	}
	if err := closeRows(rows, "iterate admin track lyrics"); err != nil {
		return nil, 0, err
	}
	var total int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*)::int FROM lyrics WHERE track_id = $1`, trackID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin track lyrics: %w", err)
	}
	return lyrics, total, nil
}

func (repository *Repository) enrichTracks(ctx context.Context, records []TrackRecord) error {
	if len(records) == 0 {
		return nil
	}
	ids := trackIDs(records)
	for index := range records {
		records[index].Credits = []CreditRecord{}
		records[index].Variants = []VariantRecord{}
		records[index].MetadataStatus = MetadataOriginal
		records[index].Lyrics = []LyricRecord{}
	}
	byID := make(map[string]*TrackRecord, len(records))
	for index := range records {
		byID[records[index].ID] = &records[index]
	}
	creditRows, err := repository.pool.Query(ctx, `
		SELECT credit.track_id, artist.id, artist.name, credit.role::text, credit.sort_order
		FROM track_artists credit JOIN artists artist ON artist.id = credit.artist_id
		WHERE credit.track_id = ANY($1::uuid[])
		ORDER BY credit.track_id, credit.sort_order, artist.name
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track credits: %w", err)
	}
	for creditRows.Next() {
		var trackID string
		var credit CreditRecord
		if err := creditRows.Scan(&trackID, &credit.ArtistID, &credit.ArtistName, &credit.Role, &credit.SortOrder); err != nil {
			creditRows.Close()
			return fmt.Errorf("scan admin track credit: %w", err)
		}
		if record := byID[trackID]; record != nil {
			record.Credits = append(record.Credits, credit)
		}
	}
	if err := closeRows(creditRows, "iterate admin track credits"); err != nil {
		return err
	}
	sourceRows, err := repository.pool.Query(ctx, `
		SELECT mapped.track_id, source.id, source.root_id, root.name, source.source_path,
		       source.status, source.checksum_sha256, root.mode::text, root.enabled,
		       root.status::text, mapping_stats.mapping_count, mapping_stats.cue
		FROM local_music_source_tracks mapped
		JOIN local_music_sources source ON source.id = mapped.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN track_metadata metadata ON metadata.track_id = mapped.track_id
		LEFT JOIN LATERAL (
			SELECT count(*)::int AS mapping_count,
			       COALESCE(bool_or(source_mapping.cue_path IS NOT NULL), false) AS cue
			FROM local_music_source_tracks source_mapping
			WHERE source_mapping.source_id = source.id
		) mapping_stats ON true
		WHERE mapped.track_id = ANY($1::uuid[])
		ORDER BY mapped.track_id,
		         CASE WHEN source.id = metadata.source_id THEN 0 ELSE 1 END,
		         CASE source.status WHEN 'READY' THEN 0 WHEN 'PROCESSING' THEN 1 WHEN 'FAILED' THEN 2 ELSE 3 END,
		         source.updated_at DESC, source.id ASC
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track sources: %w", err)
	}
	for sourceRows.Next() {
		var trackID string
		var source SourceRecord
		if err := sourceRows.Scan(
			&trackID, &source.ID, &source.RootID, &source.RootName, &source.RelativePath,
			&source.Status, &source.ChecksumSHA256, &source.Mode, &source.RootEnabled,
			&source.RootStatus, &source.MappingCount, &source.Cue,
		); err != nil {
			sourceRows.Close()
			return fmt.Errorf("scan admin track source: %w", err)
		}
		if record := byID[trackID]; record != nil && record.Source == nil {
			copy := source
			record.Source = &copy
		}
	}
	if err := closeRows(sourceRows, "iterate admin track sources"); err != nil {
		return err
	}
	metadataRows, err := repository.pool.Query(ctx, `
		SELECT track_id, version, overrides <> '{}'::jsonb FROM track_metadata
		WHERE track_id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track metadata: %w", err)
	}
	for metadataRows.Next() {
		var trackID string
		var version int
		var overridden bool
		if err := metadataRows.Scan(&trackID, &version, &overridden); err != nil {
			metadataRows.Close()
			return fmt.Errorf("scan admin track metadata: %w", err)
		}
		if record := byID[trackID]; record != nil {
			record.MetadataVersion = &version
			if overridden {
				record.MetadataStatus = MetadataOverridden
			}
		}
	}
	if err := closeRows(metadataRows, "iterate admin track metadata"); err != nil {
		return err
	}
	writebackRows, err := repository.pool.Query(ctx, `
		SELECT DISTINCT ON (track_id) id, track_id, status::text, metadata_version,
		       last_error_code,last_error
		FROM metadata_writeback_jobs
		WHERE track_id = ANY($1::uuid[])
		ORDER BY track_id, created_at DESC, id DESC
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track writebacks: %w", err)
	}
	for writebackRows.Next() {
		var id, trackID, status string
		var metadataVersion int
		var lastErrorCode, lastError *string
		if err := writebackRows.Scan(
			&id, &trackID, &status, &metadataVersion, &lastErrorCode, &lastError,
		); err != nil {
			writebackRows.Close()
			return fmt.Errorf("scan admin track writeback: %w", err)
		}
		if record := byID[trackID]; record != nil {
			if status == "PENDING" || status == "PROCESSING" {
				record.MetadataStatus = MetadataPendingWrite
				record.ActiveWritebackJobID = &id
			} else if writebackHasTerminalError(status, lastErrorCode, lastError) {
				record.LatestWritebackErrorCode = lastErrorCode
				record.LatestWritebackError = lastError
			}
			if writebackHasTerminalError(status, lastErrorCode, lastError) &&
				record.MetadataVersion != nil && *record.MetadataVersion == metadataVersion {
				record.MetadataStatus = MetadataWriteFailed
			}
		}
	}
	if err := closeRows(writebackRows, "iterate admin track writebacks"); err != nil {
		return err
	}
	variantRows, err := repository.pool.Query(ctx, `
		SELECT track_id, id, quality, mime_type, codec, container, bitrate, sample_rate, status::text, updated_at
		FROM track_variants WHERE track_id = ANY($1::uuid[])
		ORDER BY track_id, quality ASC
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track variants: %w", err)
	}
	for variantRows.Next() {
		var trackID string
		var variant VariantRecord
		if err := variantRows.Scan(
			&trackID, &variant.ID, &variant.Quality, &variant.MimeType, &variant.Codec,
			&variant.Container, &variant.Bitrate, &variant.SampleRate, &variant.Status, &variant.UpdatedAt,
		); err != nil {
			variantRows.Close()
			return fmt.Errorf("scan admin track variant: %w", err)
		}
		if record := byID[trackID]; record != nil {
			record.Variants = append(record.Variants, variant)
		}
	}
	if err := closeRows(variantRows, "iterate admin track variants"); err != nil {
		return err
	}
	jobRows, err := repository.pool.Query(ctx, `
		SELECT DISTINCT ON (job.track_id) job.track_id, job.status::text, job.attempts, job.max_attempts,
		       job.last_error, job.last_error_code, job.updated_at
		FROM media_jobs job
		JOIN tracks current_track ON current_track.id = job.track_id
			AND current_track.media_generation = job.generation
		WHERE job.track_id = ANY($1::uuid[])
		ORDER BY job.track_id, job.created_at DESC, job.id DESC
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track media jobs: %w", err)
	}
	for jobRows.Next() {
		var trackID string
		var job MediaProcessingRecord
		if err := jobRows.Scan(
			&trackID, &job.Status, &job.Attempts, &job.MaxAttempts,
			&job.LastError, &job.LastErrorCode, &job.UpdatedAt,
		); err != nil {
			jobRows.Close()
			return fmt.Errorf("scan admin track media job: %w", err)
		}
		if record := byID[trackID]; record != nil {
			copy := job
			record.MediaProcessing = &copy
		}
	}
	if err := closeRows(jobRows, "iterate admin track media jobs"); err != nil {
		return err
	}
	return nil
}

func scanArtists(rows pgx.Rows) ([]ArtistRecord, error) {
	result := make([]ArtistRecord, 0)
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
	result := make([]AlbumRecord, 0)
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
	result := make([]TrackRecord, 0)
	for rows.Next() {
		var record TrackRecord
		if err := rows.Scan(
			&record.ID, &record.AlbumID, &record.AlbumTitle, &record.AlbumCoverAssetID,
			&record.Title, &record.TrackNumber, &record.DiscNumber, &record.DurationMS,
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
	SELECT t.id, t.album_id, al.title, al.cover_asset_id, t.title,
	       t.track_number, t.disc_number, t.duration_ms, t.status::text,
	       audio_status.value, t.version, t.published_at, t.created_at, t.updated_at
` + trackFromSQL

const metadataStatusConditionSQL = `COALESCE((
	SELECT CASE
		WHEN latest.status IN ('PENDING', 'PROCESSING') THEN 'PENDING_WRITE'
		WHEN latest.status = 'FAILED' AND latest.metadata_version = metadata.version THEN 'WRITE_FAILED'
		WHEN latest.status = 'CANCELLED' AND latest.metadata_version = metadata.version
			AND (latest.last_error_code IS NOT NULL OR latest.last_error IS NOT NULL) THEN 'WRITE_FAILED'
		WHEN metadata.overrides <> '{}'::jsonb THEN 'OVERRIDDEN'
		ELSE 'ORIGINAL'
	END
	FROM track_metadata metadata
	LEFT JOIN LATERAL (
		SELECT job.status::text AS status, job.metadata_version, job.last_error_code, job.last_error
		FROM metadata_writeback_jobs job
		WHERE job.track_id = metadata.track_id
		ORDER BY job.created_at DESC, job.id DESC LIMIT 1
	) latest ON true
	WHERE metadata.track_id = t.id
), 'ORIGINAL') = $%d`

func writebackHasTerminalError(status string, errorCode, message *string) bool {
	return status == "FAILED" || status == "CANCELLED" && (errorCode != nil || message != nil)
}

var _ Store = (*Repository)(nil)
