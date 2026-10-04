package admincatalog

import (
	"context"
	"fmt"
	"strings"

	"xymusic/server/internal/shared/apperror"
)

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
