package admincatalog

import (
	"context"
	"fmt"
	"strings"

	"xymusic/server/internal/shared/apperror"
)

func (repository *Repository) ListAlbums(
	ctx context.Context,
	query AlbumQuery,
) ([]AlbumRecord, int, error) {
	arguments := make([]any, 0, 5)
	conditions := []string{adminAlbumHasActiveTracksSQL}
	if query.Search != "" {
		position := appendArgument(&arguments, "%"+escapeLike(query.Search)+"%")
		conditions = append(conditions, fmt.Sprintf(`(al.title ILIKE $%d ESCAPE E'\\' OR EXISTS (
			SELECT 1 FROM album_artists credit
			JOIN artists artist ON artist.id = credit.artist_id
			WHERE credit.album_id = al.id AND artist.name ILIKE $%d ESCAPE E'\\'
		))`, position, position))
	}
	column := map[string]string{
		"title": "al.normalized_title", "createdAt": "al.created_at",
		"updatedAt": "al.updated_at", "releaseDate": "al.release_date",
	}[query.Sort]
	if column == "" {
		return nil, 0, fmt.Errorf("unsupported album sort %q", query.Sort)
	}
	baseConditions := append([]string(nil), conditions...)
	countArguments := append([]any(nil), arguments...)
	if query.CursorMode && query.After != nil {
		condition, err := catalogSeekCondition(column, "al.id", query.Sort, query.Order, query.After, query.Sort == "releaseDate", &arguments)
		if err != nil {
			return nil, 0, err
		}
		conditions = append(conditions, condition)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	direction := sqlDirection(query.Order)
	limitPosition := appendArgument(&arguments, query.Limit)
	statement := albumSelectSQL + where + fmt.Sprintf(
		" ORDER BY %s %s, al.id %s LIMIT $%d", column, direction, direction, limitPosition,
	)
	if !query.CursorMode {
		offsetPosition := appendArgument(&arguments, query.Offset)
		statement += fmt.Sprintf(" OFFSET $%d", offsetPosition)
	}
	countDone, countCancel := startPageCount(ctx, query.TotalHint == nil, func(countCtx context.Context) (int, error) {
		var total int
		err := repository.pool.QueryRow(countCtx, "SELECT count(*)::int FROM albums al WHERE "+strings.Join(baseConditions, " AND "), countArguments...).Scan(&total)
		return total, err
	})
	if countCancel != nil {
		defer countCancel()
	}
	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin albums: %w", err)
	}
	records, err := scanAlbumsWithCapacity(rows, query.Limit)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	enrichmentRecords := recordsWithoutLookahead(records, query.Limit, query.CursorMode, query.HasNextProbe)
	if err := repository.enrichAlbums(ctx, enrichmentRecords); err != nil {
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
			return nil, 0, fmt.Errorf("count admin albums: %w", count.err)
		}
		total = count.total
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
	groupLimit := query.Limit
	if groupLimit <= 0 {
		groupLimit = 100
	}
	var result DuplicateAlbumPage
	var countedTotal int
	if err := repository.pool.QueryRow(ctx, `
		WITH duplicate_groups AS (
			SELECT album.normalized_title, count(*)::int AS album_count
			FROM albums album
			WHERE EXISTS (
				SELECT 1 FROM tracks track
				WHERE track.album_id = album.id AND track.status <> 'ARCHIVED'
			)
			GROUP BY album.normalized_title HAVING count(*) > 1
		)
		SELECT count(*)::int,
		       COALESCE(sum(album_count - 1), 0)::int,
		       count(*) FILTER (WHERE $1::uuid IS NULL OR normalized_title = (
			   SELECT album.normalized_title FROM albums album
			   WHERE album.id = $1::uuid AND EXISTS (
				   SELECT 1 FROM tracks track
				   WHERE track.album_id = album.id AND track.status <> 'ARCHIVED'
			   )
		   ))::int
		FROM duplicate_groups`, albumID).Scan(
		&result.GroupCount, &result.DuplicateAlbumCount, &countedTotal,
	); err != nil {
		return DuplicateAlbumPage{}, fmt.Errorf("count duplicate album groups: %w", err)
	}
	if query.TotalHint != nil {
		if *query.TotalHint < 0 {
			return DuplicateAlbumPage{}, fmt.Errorf("pagination total hint is invalid")
		}
		result.Total = *query.TotalHint
	} else {
		result.Total = countedTotal
	}

	groupArguments := []any{groupLimit, albumID}
	groupWhere := `($2::uuid IS NULL OR normalized_title = (
		SELECT album.normalized_title FROM albums album
		WHERE album.id = $2::uuid AND EXISTS (
			SELECT 1 FROM tracks track
			WHERE track.album_id = album.id AND track.status <> 'ARCHIVED'
		)
	))`
	if query.CursorMode && query.After != nil {
		groupArguments = append(groupArguments, query.After.Key)
		groupWhere += ` AND normalized_title > $3`
	}
	groupStatement := `
		WITH duplicate_groups AS (
			SELECT album.normalized_title, min(album.title) AS title, count(*)::int AS album_count
			FROM albums album
			WHERE EXISTS (
				SELECT 1 FROM tracks track
				WHERE track.album_id = album.id AND track.status <> 'ARCHIVED'
			)
			GROUP BY album.normalized_title HAVING count(*) > 1
		)
		SELECT normalized_title, title, album_count FROM duplicate_groups
		WHERE ` + groupWhere + `
		ORDER BY normalized_title ASC LIMIT $1`
	if !query.CursorMode {
		groupArguments = append(groupArguments, max(0, query.Offset))
		groupStatement += ` OFFSET $` + fmt.Sprint(len(groupArguments))
	}
	groupRows, err := repository.pool.Query(ctx, groupStatement, groupArguments...)
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
	memberArguments := []any{keys, albumLimit}
	memberCondition := ""
	if query.AlbumCursorMode && query.AlbumAfter != nil {
		memberArguments = append(memberArguments, query.AlbumAfter.ID)
		memberCondition = ` AND source.id > $3`
	}
	memberStatement := `
		SELECT al.id, al.title, al.normalized_title, al.description, al.cover_asset_id,
		       al.release_date::text, al.version, al.created_at, al.updated_at
		FROM unnest($1::text[]) WITH ORDINALITY selected(normalized_title, position)
		JOIN LATERAL (
			SELECT source.id, source.title, source.normalized_title, source.description,
			       source.cover_asset_id, source.release_date, source.version,
			       source.created_at, source.updated_at
			FROM albums source
			WHERE source.normalized_title = selected.normalized_title
			  AND EXISTS (
				  SELECT 1 FROM tracks track
				  WHERE track.album_id = source.id AND track.status <> 'ARCHIVED'
			  )` + memberCondition + `
			ORDER BY source.id ASC LIMIT $2`
	if !query.AlbumCursorMode {
		memberArguments = append(memberArguments, albumOffset)
		memberStatement += ` OFFSET $` + fmt.Sprint(len(memberArguments))
	}
	memberStatement += `
		) al ON TRUE
		ORDER BY selected.position ASC, al.id ASC`
	rows, err := repository.pool.Query(ctx, memberStatement, memberArguments...)
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
	rows, err := repository.pool.Query(ctx, albumSelectSQL+" WHERE al.id = $1 AND "+adminAlbumHasActiveTracksSQL+" LIMIT 1", id)
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

func (repository *Repository) FindAlbumCursor(ctx context.Context, id string, limit int, after *AlbumTrackCursor, totalHint *int) (AlbumRecord, []TrackRecord, int, error) {
	rows, err := repository.pool.Query(ctx, albumSelectSQL+" WHERE al.id = $1 AND "+adminAlbumHasActiveTracksSQL+" LIMIT 1", id)
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
	if limit <= 0 {
		limit = 1
	}
	arguments := []any{id}
	where := "t.album_id = $1"
	if after != nil {
		var disc any
		if after.DiscNumber != nil {
			disc = *after.DiscNumber
		}
		var track any
		if after.TrackNumber != nil {
			track = *after.TrackNumber
		}
		arguments = append(arguments, disc, track, after.NormalizedTitle, after.ID)
		where += ` AND (
			(COALESCE(t.disc_number, 2147483647), COALESCE(t.track_number, 2147483647), t.normalized_title, t.id)
			> (COALESCE($2::int, 2147483647), COALESCE($3::int, 2147483647), $4, $5)
		)`
	}
	limitPosition := len(arguments) + 1
	arguments = append(arguments, limit)
	trackRows, err := repository.pool.Query(ctx, trackSelectSQL+`
		WHERE `+where+`
		ORDER BY t.disc_number ASC NULLS LAST, t.track_number ASC NULLS LAST,
		         t.normalized_title ASC, t.id ASC
		LIMIT $`+fmt.Sprint(limitPosition), arguments...)
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
	total := records[0].TrackCount
	if totalHint != nil {
		if *totalHint < 0 {
			return AlbumRecord{}, nil, 0, fmt.Errorf("pagination total hint is invalid")
		}
		total = *totalHint
	}
	return records[0], tracks, total, nil
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
