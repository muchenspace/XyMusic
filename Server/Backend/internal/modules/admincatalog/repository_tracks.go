package admincatalog

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"xymusic/server/internal/shared/apperror"
)

func (repository *Repository) ListTracks(
	ctx context.Context,
	query TrackQuery,
) ([]TrackRecord, int, error) {
	arguments := make([]any, 0, 10)
	conditions := make([]string, 0, 5)
	switch query.Status {
	case "":
		// The derived status can only be ARCHIVED when the catalog row itself is
		// archived, so the default catalog view can use a cheap base-column
		// predicate instead of evaluating the CASE for every row.
		conditions = append(conditions, "t.status <> 'ARCHIVED'")
	case AudioStatusArchived:
		// ARCHIVED is a direct property of tracks.status. Keeping this branch
		// out of the derived audio-status CASE lets PostgreSQL use the status
		// index before evaluating the more expensive source/scan checks.
		conditions = append(conditions, "t.status = 'ARCHIVED'")
	case AudioStatusReady:
		// READY is the hot path in the admin UI. Express its CASE branch as
		// indexed base predicates so PostgreSQL can discard non-ready rows
		// before calculating audio_status for the selected page.
		conditions = append(conditions, adminReadyTrackConditionSQL)
	default:
		conditions = append(conditions, fmt.Sprintf("audio_status.value = $%d", appendArgument(&arguments, query.Status)))
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
	column := map[string]string{
		"title": "t.normalized_title", "createdAt": "t.created_at",
		"updatedAt": "t.updated_at", "status": "audio_status.value",
	}[query.Sort]
	if column == "" {
		return nil, 0, fmt.Errorf("unsupported track sort %q", query.Sort)
	}
	baseConditions := append([]string(nil), conditions...)
	countArguments := append([]any(nil), arguments...)
	if query.CursorMode && query.After != nil {
		condition, err := trackSeekCondition(column, query.Sort, query.Order, query.After, &arguments)
		if err != nil {
			return nil, 0, err
		}
		conditions = append(conditions, condition)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	direction := sqlDirection(query.Order)
	limitPosition := appendArgument(&arguments, query.Limit)
	statement := trackSelectSQL + where + fmt.Sprintf(
		" ORDER BY %s %s, t.id %s LIMIT $%d", column, direction, direction, limitPosition,
	)
	if !query.CursorMode {
		offsetPosition := appendArgument(&arguments, query.Offset)
		statement += fmt.Sprintf(" OFFSET $%d", offsetPosition)
	}
	// The first cursor page is the only page that needs an exact total. Run
	// that count on another pool connection while the page projection is being
	// read and enriched so the user does not pay both costs serially.
	var countDone chan pageCountResult
	var countCancel context.CancelFunc
	if query.TotalHint == nil {
		var countCtx context.Context
		countCtx, countCancel = context.WithCancel(ctx)
		countDone = make(chan pageCountResult, 1)
		go func() {
			var total int
			err := repository.countAdminTracks(countCtx, query, baseConditions, countArguments, &total)
			countDone <- pageCountResult{total: total, err: err}
		}()
	}
	if countCancel != nil {
		defer countCancel()
	}

	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin tracks: %w", err)
	}
	records, err := scanTracksWithCapacity(rows, query.Limit)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	// Cursor pages request one look-ahead row. That row is only used to
	// determine hasNext and must not trigger the four enrichment queries or
	// artwork/metadata work performed for visible rows.
	enrichmentRecords := recordsWithoutLookahead(records, query.Limit, query.CursorMode, query.HasNextProbe)
	if err := repository.enrichTracks(ctx, enrichmentRecords); err != nil {
		return nil, 0, err
	}
	var total int
	if query.TotalHint != nil {
		if *query.TotalHint < 0 {
			return nil, 0, fmt.Errorf("pagination total hint is invalid")
		}
		total = *query.TotalHint
	} else {
		count := <-countDone
		if count.err != nil {
			return nil, 0, count.err
		}
		total = count.total
	}
	return records, total, nil
}

// trackSeekCondition mirrors the ORDER BY tuple (ordered value, id). The
// cursor value is validated and signed by the service; casts are kept here so
// PostgreSQL can use the btree indexes for timestamp orderings.
func trackSeekCondition(column, sort string, order SortOrder, cursor *ListCursor, arguments *[]any) (string, error) {
	if cursor == nil || cursor.ID == "" {
		return "", fmt.Errorf("track cursor is invalid")
	}
	valuePosition := appendArgument(arguments, cursor.Value)
	idPosition := appendArgument(arguments, cursor.ID)
	value := fmt.Sprintf("$%d", valuePosition)
	if sort == "createdAt" || sort == "updatedAt" {
		value += "::timestamptz"
	}
	operator := ">"
	if order == SortDescending {
		operator = "<"
	}
	return fmt.Sprintf("(%s %s %s OR (%s = %s AND t.id %s $%d))",
		column, operator, value, column, value, operator, idPosition), nil
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

func (repository *Repository) FindTrackCursor(ctx context.Context, id string, lyricLimit int, after *TrackLyricCursor, totalHint *int) (TrackRecord, int, error) {
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
	lyrics, total, err := repository.listLyricsCursor(ctx, id, lyricLimit, after, totalHint)
	if err != nil {
		return TrackRecord{}, 0, err
	}
	records[0].Lyrics = lyrics
	return records[0], total, nil
}

func (repository *Repository) listLyrics(ctx context.Context, trackID string, limit, offset int) ([]LyricRecord, int, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id, language, format::text, timing::text, content, is_default, version, updated_at
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
			&lyric.ID, &lyric.Language, &lyric.Format, &lyric.Timing, &lyric.Content,
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

func (repository *Repository) listLyricsCursor(ctx context.Context, trackID string, limit int, after *TrackLyricCursor, totalHint *int) ([]LyricRecord, int, error) {
	if limit <= 0 {
		limit = 1
	}
	arguments := []any{trackID}
	where := "track_id = $1"
	if after != nil {
		defaultKey := 1
		if after.IsDefault {
			defaultKey = 0
		}
		arguments = append(arguments, defaultKey, after.Language, after.ID)
		where += ` AND (CASE WHEN is_default THEN 0 ELSE 1 END, language, id) > ($2::int, $3, $4)`
	}
	limitPosition := len(arguments) + 1
	arguments = append(arguments, limit)
	rows, err := repository.pool.Query(ctx, `
		SELECT id, language, format::text, timing::text, content, is_default, version, updated_at
		FROM lyrics
		WHERE `+where+`
		ORDER BY is_default DESC, language ASC, id ASC
		LIMIT $`+fmt.Sprint(limitPosition), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin track lyrics: %w", err)
	}
	lyrics := make([]LyricRecord, 0, limit)
	for rows.Next() {
		var lyric LyricRecord
		if err := rows.Scan(
			&lyric.ID, &lyric.Language, &lyric.Format, &lyric.Timing, &lyric.Content,
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
	if totalHint != nil {
		if *totalHint < 0 {
			return nil, 0, fmt.Errorf("pagination total hint is invalid")
		}
		total = *totalHint
	} else if err := repository.pool.QueryRow(ctx, `SELECT count(*)::int FROM lyrics WHERE track_id = $1`, trackID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin track lyrics: %w", err)
	}
	return lyrics, total, nil
}

func (repository *Repository) enrichTracks(ctx context.Context, records []TrackRecord) error {
	if len(records) == 0 {
		return nil
	}
	ids, byID := initializeTrackEnrichment(records)
	group, groupCtx := errgroup.WithContext(ctx)
	// Credits, source selection, metadata and writeback status are independent
	// projections. Running them on separate pool connections removes the
	// serial four-query tail that made 10k-row pages feel sluggish.
	group.Go(func() error { return repository.enrichTrackCredits(groupCtx, ids, byID) })
	group.Go(func() error { return repository.enrichTrackAttributes(groupCtx, ids, byID) })
	return group.Wait()
}

func initializeTrackEnrichment(records []TrackRecord) ([]string, map[string]*TrackRecord) {
	ids := trackIDs(records)
	byID := make(map[string]*TrackRecord, len(records))
	for index := range records {
		records[index].Credits = []CreditRecord{}
		records[index].MetadataStatus = MetadataNormal
		records[index].Lyrics = []LyricRecord{}
		byID[records[index].ID] = &records[index]
	}
	return ids, byID
}

func (repository *Repository) enrichTrackCredits(
	ctx context.Context,
	ids []string,
	byID map[string]*TrackRecord,
) error {
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
	return closeRows(creditRows, "iterate admin track credits")
}

func (repository *Repository) enrichTrackAttributes(
	ctx context.Context,
	ids []string,
	byID map[string]*TrackRecord,
) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return repository.enrichTrackSources(groupCtx, ids, byID) })
	group.Go(func() error { return repository.enrichTrackMetadata(groupCtx, ids, byID) })
	group.Go(func() error { return repository.enrichTrackWritebacks(groupCtx, ids, byID) })
	return group.Wait()
}

func (repository *Repository) enrichTrackSources(
	ctx context.Context,
	ids []string,
	byID map[string]*TrackRecord,
) error {
	sourceRows, err := repository.pool.Query(ctx, `
		WITH chosen AS (
			SELECT DISTINCT ON (mapped.track_id)
				mapped.track_id, mapped.source_id
			FROM local_music_source_tracks mapped
			JOIN local_music_sources source ON source.id = mapped.source_id
			LEFT JOIN track_metadata metadata ON metadata.track_id = mapped.track_id
			WHERE mapped.track_id = ANY($1::uuid[])
			ORDER BY mapped.track_id,
			         CASE WHEN source.id = metadata.source_id THEN 0 ELSE 1 END,
			         CASE source.status WHEN 'READY' THEN 0 WHEN 'PROCESSING' THEN 1 WHEN 'FAILED' THEN 2 ELSE 3 END,
			         source.updated_at DESC, source.id ASC
		)
		SELECT chosen.track_id, source.id, source.root_id, root.name, source.source_path,
		       source.status, source.last_error, source.checksum_sha256, root.mode::text, root.enabled,
		       EXISTS (
		         SELECT 1 FROM library_scan_runs active_scan
		         WHERE active_scan.root_id = root.id
		           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
		       ), (SELECT count(*)::int FROM local_music_source_tracks mapping_count
		          WHERE mapping_count.source_id = source.id)
		FROM chosen
		JOIN local_music_sources source ON source.id = chosen.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		ORDER BY chosen.track_id
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track sources: %w", err)
	}
	for sourceRows.Next() {
		var trackID string
		var source SourceRecord
		if err := sourceRows.Scan(
			&trackID, &source.ID, &source.RootID, &source.RootName, &source.RelativePath,
			&source.Status, &source.LastError, &source.ChecksumSHA256, &source.Mode, &source.RootEnabled,
			&source.ScanActive, &source.MappingCount,
		); err != nil {
			sourceRows.Close()
			return fmt.Errorf("scan admin track source: %w", err)
		}
		if record := byID[trackID]; record != nil {
			copy := source
			record.Source = &copy
		}
	}
	return closeRows(sourceRows, "iterate admin track sources")
}

func (repository *Repository) enrichTrackMetadata(
	ctx context.Context,
	ids []string,
	byID map[string]*TrackRecord,
) error {
	metadataRows, err := repository.pool.Query(ctx, `
		SELECT track_id, version FROM track_metadata
		WHERE track_id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return fmt.Errorf("query admin track metadata: %w", err)
	}
	for metadataRows.Next() {
		var trackID string
		var version int
		if err := metadataRows.Scan(&trackID, &version); err != nil {
			metadataRows.Close()
			return fmt.Errorf("scan admin track metadata: %w", err)
		}
		if record := byID[trackID]; record != nil {
			record.MetadataVersion = &version
		}
	}
	return closeRows(metadataRows, "iterate admin track metadata")
}

func (repository *Repository) enrichTrackWritebacks(
	ctx context.Context,
	ids []string,
	byID map[string]*TrackRecord,
) error {
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
	return closeRows(writebackRows, "iterate admin track writebacks")
}
