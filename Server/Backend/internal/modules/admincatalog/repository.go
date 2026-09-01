package admincatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

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
	arguments := make([]any, 0, 5)
	conditions := []string{adminArtistHasActiveTracksSQL}
	if query.Search != "" {
		position := appendArgument(&arguments, "%"+escapeLike(query.Search)+"%")
		conditions = append(conditions, fmt.Sprintf(`(name ILIKE $%d ESCAPE E'\\' OR description ILIKE $%d ESCAPE E'\\')`, position, position))
	}
	column := map[string]string{"name": "normalized_name", "createdAt": "created_at", "updatedAt": "updated_at"}[query.Sort]
	if column == "" {
		return nil, 0, fmt.Errorf("unsupported artist sort %q", query.Sort)
	}
	baseConditions := append([]string(nil), conditions...)
	countArguments := append([]any(nil), arguments...)
	if query.CursorMode && query.After != nil {
		condition, err := catalogSeekCondition(column, "artists.id", query.Sort, query.Order, query.After, false, &arguments)
		if err != nil {
			return nil, 0, err
		}
		conditions = append(conditions, condition)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	direction := sqlDirection(query.Order)
	limitPosition := appendArgument(&arguments, query.Limit)
	statement := artistSelectSQL + where + fmt.Sprintf(
		" ORDER BY %s %s, id %s LIMIT $%d", column, direction, direction, limitPosition,
	)
	if !query.CursorMode {
		offsetPosition := appendArgument(&arguments, query.Offset)
		statement += fmt.Sprintf(" OFFSET $%d", offsetPosition)
	}
	countDone, countCancel := startPageCount(ctx, query.TotalHint == nil, func(countCtx context.Context) (int, error) {
		var total int
		err := repository.pool.QueryRow(countCtx, "SELECT count(*)::int FROM artists WHERE "+strings.Join(baseConditions, " AND "), countArguments...).Scan(&total)
		return total, err
	})
	if countCancel != nil {
		defer countCancel()
	}
	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("query admin artists: %w", err)
	}
	records, err := scanArtistsWithCapacity(rows, query.Limit)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	enrichmentRecords := recordsWithoutLookahead(records, query.Limit, query.CursorMode, query.HasNextProbe)
	if err := repository.enrichArtists(ctx, enrichmentRecords); err != nil {
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
			return nil, 0, fmt.Errorf("count admin artists: %w", count.err)
		}
		total = count.total
	}
	return records, total, nil
}

func (repository *Repository) FindArtist(ctx context.Context, id string) (ArtistRecord, error) {
	rows, err := repository.pool.Query(ctx, artistSelectSQL+" WHERE id = $1 AND "+adminArtistHasActiveTracksSQL+" LIMIT 1", id)
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
		{`SELECT artist_id, count(DISTINCT album_id)::int
			FROM album_artists
			WHERE artist_id = ANY($1::uuid[]) GROUP BY artist_id`, true},
		{`SELECT artist_id, count(DISTINCT track_id)::int
			FROM track_artists
			WHERE artist_id = ANY($1::uuid[]) GROUP BY artist_id`, false},
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
		), mapping_stats AS (
			SELECT mapping.source_id, count(*)::int AS mapping_count,
			       COALESCE(bool_or(mapping.cue_path IS NOT NULL), false) AS cue
			FROM local_music_source_tracks mapping
			JOIN (SELECT DISTINCT source_id FROM chosen) selected ON selected.source_id = mapping.source_id
			GROUP BY mapping.source_id
		)
		SELECT chosen.track_id, source.id, source.root_id, root.name, source.source_path,
		       source.status, source.last_error, source.checksum_sha256, root.mode::text, root.enabled,
		       EXISTS (
		         SELECT 1 FROM library_scan_runs active_scan
		         WHERE active_scan.root_id = root.id
		           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
		       ), COALESCE(mapping_stats.mapping_count, 0), COALESCE(mapping_stats.cue, false)
		FROM chosen
		JOIN local_music_sources source ON source.id = chosen.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN mapping_stats ON mapping_stats.source_id = source.id
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
			&source.ScanActive, &source.MappingCount, &source.Cue,
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
