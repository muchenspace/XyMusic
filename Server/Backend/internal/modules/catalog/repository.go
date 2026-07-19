package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("catalog record not found")

// Store is the catalog persistence boundary. Repository is the production
// pgxpool implementation; the interface also keeps service tests independent
// from PostgreSQL.
type Store interface {
	ListTracks(ctx context.Context, query ListTracksQuery) ([]TrackRecord, error)
	RandomTracks(ctx context.Context, userID string, anchor float64, atOrAfter bool, limit int) ([]TrackRecord, error)
	FindTracks(ctx context.Context, userID string, trackIDs []string) ([]TrackRecord, error)
	FindTrack(ctx context.Context, userID, trackID string) (TrackRecord, error)
	ListLyrics(ctx context.Context, query ListLyricsQuery) ([]LyricRecord, int, error)
	ListArtists(ctx context.Context, query ListArtistsQuery) ([]ArtistRecord, error)
	FindArtist(ctx context.Context, artistID string) (ArtistRecord, error)
	ListAlbums(ctx context.Context, query ListAlbumsQuery) ([]AlbumRecord, error)
	RandomAlbums(ctx context.Context, anchor float64, atOrAfter bool, limit int) ([]AlbumRecord, error)
	FindAlbum(ctx context.Context, albumID string) (AlbumRecord, error)
	SearchTracks(ctx context.Context, query SearchQuery) ([]TrackRecord, error)
	SearchArtists(ctx context.Context, query SearchQuery) ([]ArtistRecord, error)
	SearchAlbums(ctx context.Context, query SearchQuery) ([]AlbumRecord, error)
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListTracks(ctx context.Context, input ListTracksQuery) ([]TrackRecord, error) {
	conditions := []string{"t.status = 'READY'", "t.published_at IS NOT NULL"}
	arguments := []any{input.UserID}
	if input.AlbumID != "" {
		conditions = append(conditions, fmt.Sprintf("t.album_id = $%d", appendArgument(&arguments, input.AlbumID)))
	}
	if input.ArtistID != "" {
		position := appendArgument(&arguments, input.ArtistID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM track_artists filter_credit
			WHERE filter_credit.track_id = t.id AND filter_credit.artist_id = $%d
		)`, position))
	}
	if input.After != nil {
		condition, err := trackAfterCondition(input.Sort, input.After, &arguments)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	order, err := trackOrderSQL(input.Sort)
	if err != nil {
		return nil, err
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	statement := trackSelectSQL + " WHERE " + strings.Join(conditions, " AND ") +
		" ORDER BY " + order + fmt.Sprintf(" LIMIT $%d", limitPosition)
	return r.queryTracks(ctx, statement, arguments...)
}

func (r *Repository) RandomTracks(
	ctx context.Context,
	userID string,
	anchor float64,
	atOrAfter bool,
	limit int,
) ([]TrackRecord, error) {
	operator := "<"
	if atOrAfter {
		operator = ">="
	}
	statement := trackSelectSQL + fmt.Sprintf(`
		WHERE t.status = 'READY'
		  AND t.published_at IS NOT NULL
		  AND t.random_key %s $2
		ORDER BY t.random_key ASC, t.id ASC
		LIMIT $3`, operator)
	return r.queryTracks(ctx, statement, userID, anchor, limit)
}

func (r *Repository) FindTracks(ctx context.Context, userID string, trackIDs []string) ([]TrackRecord, error) {
	if len(trackIDs) == 0 {
		return []TrackRecord{}, nil
	}
	return r.queryTracks(ctx, trackSelectSQL+`
		WHERE t.id = ANY($2::uuid[])
		  AND t.status = 'READY'
		  AND t.published_at IS NOT NULL
	`, userID, trackIDs)
}

func (r *Repository) FindTrack(ctx context.Context, userID, trackID string) (TrackRecord, error) {
	rows, err := r.queryTracks(ctx, trackSelectSQL+`
		WHERE t.id = $2 AND t.status = 'READY' AND t.published_at IS NOT NULL
		LIMIT 1`, userID, trackID)
	if err != nil {
		return TrackRecord{}, err
	}
	if len(rows) == 0 {
		return TrackRecord{}, ErrNotFound
	}
	return rows[0], nil
}

func (r *Repository) ListLyrics(ctx context.Context, query ListLyricsQuery) ([]LyricRecord, int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, track_id, language, format::text, content, is_default, updated_at
		FROM lyrics
		WHERE track_id = $1 AND content IS NOT NULL
		ORDER BY is_default DESC, language ASC, id ASC
		LIMIT $2 OFFSET $3
	`, query.TrackID, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query track lyrics: %w", err)
	}
	result := make([]LyricRecord, 0)
	for rows.Next() {
		var item LyricRecord
		if err := rows.Scan(
			&item.ID,
			&item.TrackID,
			&item.Language,
			&item.Format,
			&item.Content,
			&item.IsDefault,
			&item.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan track lyric: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("iterate track lyrics: %w", err)
	}
	rows.Close()
	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM lyrics
		WHERE track_id = $1 AND content IS NOT NULL
	`, query.TrackID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count track lyrics: %w", err)
	}
	return result, total, nil
}

func (r *Repository) ListArtists(ctx context.Context, input ListArtistsQuery) ([]ArtistRecord, error) {
	conditions := make([]string, 0, 1)
	arguments := make([]any, 0, 3)
	if input.After != nil {
		valuePosition := appendArgument(&arguments, input.After.Value)
		idPosition := appendArgument(&arguments, input.After.ID)
		operator := ">"
		if input.Sort == ArtistSortNameDesc {
			operator = "<"
		}
		conditions = append(conditions, fmt.Sprintf(
			"(ar.normalized_name %s $%d OR (ar.normalized_name = $%d AND ar.id %s $%d))",
			operator, valuePosition, valuePosition, operator, idPosition,
		))
	}
	order := "ar.normalized_name ASC, ar.id ASC"
	if input.Sort == ArtistSortNameDesc {
		order = "ar.normalized_name DESC, ar.id DESC"
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	return r.queryArtists(ctx, artistSelectSQL+where+" ORDER BY "+order+fmt.Sprintf(" LIMIT $%d", limitPosition), arguments...)
}

func (r *Repository) FindArtist(ctx context.Context, artistID string) (ArtistRecord, error) {
	rows, err := r.queryArtists(ctx, artistSelectSQL+" WHERE ar.id = $1 LIMIT 1", artistID)
	if err != nil {
		return ArtistRecord{}, err
	}
	if len(rows) == 0 {
		return ArtistRecord{}, ErrNotFound
	}
	return rows[0], nil
}

func (r *Repository) ListAlbums(ctx context.Context, input ListAlbumsQuery) ([]AlbumRecord, error) {
	conditions := []string{visibleAlbumSQL}
	arguments := make([]any, 0, 5)
	if input.ArtistID != "" {
		position := appendArgument(&arguments, input.ArtistID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM album_artists filter_credit
			WHERE filter_credit.album_id = al.id AND filter_credit.artist_id = $%d
		)`, position))
	}
	if input.After != nil {
		condition, err := albumAfterCondition(input.Sort, input.After, &arguments)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	order, err := albumOrderSQL(input.Sort)
	if err != nil {
		return nil, err
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	statement := albumSelectSQL + " WHERE " + strings.Join(conditions, " AND ") +
		" ORDER BY " + order + fmt.Sprintf(" LIMIT $%d", limitPosition)
	return r.queryAlbums(ctx, statement, arguments...)
}

func (r *Repository) RandomAlbums(
	ctx context.Context,
	anchor float64,
	atOrAfter bool,
	limit int,
) ([]AlbumRecord, error) {
	operator := "<"
	if atOrAfter {
		operator = ">="
	}
	statement := albumSelectSQL + fmt.Sprintf(`
		WHERE %s AND al.random_key %s $1
		ORDER BY al.random_key ASC, al.id ASC
		LIMIT $2`, visibleAlbumSQL, operator)
	return r.queryAlbums(ctx, statement, anchor, limit)
}

func (r *Repository) FindAlbum(ctx context.Context, albumID string) (AlbumRecord, error) {
	rows, err := r.queryAlbums(ctx, albumSelectSQL+" WHERE al.id = $1 AND "+visibleAlbumSQL+" LIMIT 1", albumID)
	if err != nil {
		return AlbumRecord{}, err
	}
	if len(rows) == 0 {
		return AlbumRecord{}, ErrNotFound
	}
	return rows[0], nil
}

func (r *Repository) SearchTracks(ctx context.Context, input SearchQuery) ([]TrackRecord, error) {
	arguments := []any{input.UserID}
	queryPosition := 0
	if input.UseTrigram {
		queryPosition = appendArgument(&arguments, input.NormalizedQuery)
	}
	patternPosition := appendArgument(&arguments, input.Pattern)
	conditions := []string{
		"t.status = 'READY'",
		"t.published_at IS NOT NULL",
		"(" + fuzzySQL("t.normalized_title", queryPosition, patternPosition, input.UseTrigram) +
			" OR EXISTS (SELECT 1 FROM track_artists search_credit JOIN artists search_artist ON search_artist.id = search_credit.artist_id WHERE search_credit.track_id = t.id AND " +
			fuzzySQL("search_artist.normalized_name", queryPosition, patternPosition, input.UseTrigram) + ")" +
			" OR EXISTS (SELECT 1 FROM albums search_album WHERE search_album.id = t.album_id AND " +
			fuzzySQL("search_album.normalized_title", queryPosition, patternPosition, input.UseTrigram) + "))",
	}
	if input.After != nil {
		valuePosition := appendArgument(&arguments, input.After.Value)
		idPosition := appendArgument(&arguments, input.After.ID)
		conditions = append(conditions, fmt.Sprintf(
			"(t.normalized_title > $%d OR (t.normalized_title = $%d AND t.id > $%d))",
			valuePosition, valuePosition, idPosition,
		))
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	statement := trackSelectSQL + " WHERE " + strings.Join(conditions, " AND ") +
		fmt.Sprintf(" ORDER BY t.normalized_title ASC, t.id ASC LIMIT $%d", limitPosition)
	return r.queryTracks(ctx, statement, arguments...)
}

func (r *Repository) SearchArtists(ctx context.Context, input SearchQuery) ([]ArtistRecord, error) {
	arguments := make([]any, 0, 4)
	queryPosition := 0
	if input.UseTrigram {
		queryPosition = appendArgument(&arguments, input.NormalizedQuery)
	}
	patternPosition := appendArgument(&arguments, input.Pattern)
	conditions := []string{fuzzySQL("ar.normalized_name", queryPosition, patternPosition, input.UseTrigram)}
	if input.After != nil {
		valuePosition := appendArgument(&arguments, input.After.Value)
		idPosition := appendArgument(&arguments, input.After.ID)
		conditions = append(conditions, fmt.Sprintf(
			"(ar.normalized_name > $%d OR (ar.normalized_name = $%d AND ar.id > $%d))",
			valuePosition, valuePosition, idPosition,
		))
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	statement := artistSelectSQL + " WHERE " + strings.Join(conditions, " AND ") +
		fmt.Sprintf(" ORDER BY ar.normalized_name ASC, ar.id ASC LIMIT $%d", limitPosition)
	return r.queryArtists(ctx, statement, arguments...)
}

func (r *Repository) SearchAlbums(ctx context.Context, input SearchQuery) ([]AlbumRecord, error) {
	arguments := make([]any, 0, 4)
	queryPosition := 0
	if input.UseTrigram {
		queryPosition = appendArgument(&arguments, input.NormalizedQuery)
	}
	patternPosition := appendArgument(&arguments, input.Pattern)
	conditions := []string{
		visibleAlbumSQL,
		"(" + fuzzySQL("al.normalized_title", queryPosition, patternPosition, input.UseTrigram) +
			" OR EXISTS (SELECT 1 FROM album_artists search_credit JOIN artists search_artist ON search_artist.id = search_credit.artist_id WHERE search_credit.album_id = al.id AND " +
			fuzzySQL("search_artist.normalized_name", queryPosition, patternPosition, input.UseTrigram) + "))",
	}
	if input.After != nil {
		valuePosition := appendArgument(&arguments, input.After.Value)
		idPosition := appendArgument(&arguments, input.After.ID)
		conditions = append(conditions, fmt.Sprintf(
			"(al.normalized_title > $%d OR (al.normalized_title = $%d AND al.id > $%d))",
			valuePosition, valuePosition, idPosition,
		))
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	statement := albumSelectSQL + " WHERE " + strings.Join(conditions, " AND ") +
		fmt.Sprintf(" ORDER BY al.normalized_title ASC, al.id ASC LIMIT $%d", limitPosition)
	return r.queryAlbums(ctx, statement, arguments...)
}

func (r *Repository) queryTracks(ctx context.Context, statement string, arguments ...any) ([]TrackRecord, error) {
	rows, err := r.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query catalog tracks: %w", err)
	}
	defer rows.Close()
	result := make([]TrackRecord, 0)
	for rows.Next() {
		item, err := scanTrack(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog track: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog tracks: %w", err)
	}
	if err := r.attachTrackCredits(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) queryArtists(ctx context.Context, statement string, arguments ...any) ([]ArtistRecord, error) {
	rows, err := r.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query catalog artists: %w", err)
	}
	defer rows.Close()
	result := make([]ArtistRecord, 0)
	for rows.Next() {
		item, err := scanArtist(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog artist: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog artists: %w", err)
	}
	return result, nil
}

func (r *Repository) queryAlbums(ctx context.Context, statement string, arguments ...any) ([]AlbumRecord, error) {
	rows, err := r.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query catalog albums: %w", err)
	}
	defer rows.Close()
	result := make([]AlbumRecord, 0)
	for rows.Next() {
		item, err := scanAlbum(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog album: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog albums: %w", err)
	}
	if err := r.attachAlbumCredits(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) attachTrackCredits(ctx context.Context, records []TrackRecord) error {
	ids := trackIDs(records)
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT credit.track_id, ar.id, ar.name
		FROM track_artists credit
		JOIN artists ar ON ar.id = credit.artist_id
		WHERE credit.track_id = ANY($1::uuid[])
		ORDER BY credit.sort_order ASC, ar.id ASC
	`, ids)
	if err != nil {
		return fmt.Errorf("query track credits: %w", err)
	}
	defer rows.Close()
	grouped := make(map[string][]ArtistReferenceRecord, len(ids))
	for rows.Next() {
		var parentID string
		var credit ArtistReferenceRecord
		if err := rows.Scan(&parentID, &credit.ID, &credit.Name); err != nil {
			return fmt.Errorf("scan track credit: %w", err)
		}
		grouped[parentID] = appendUniqueCredit(grouped[parentID], credit)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate track credits: %w", err)
	}
	for index := range records {
		records[index].Artists = nonNilCredits(grouped[records[index].ID])
	}
	return nil
}

func (r *Repository) attachAlbumCredits(ctx context.Context, records []AlbumRecord) error {
	ids := albumIDs(records)
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT credit.album_id, ar.id, ar.name
		FROM album_artists credit
		JOIN artists ar ON ar.id = credit.artist_id
		WHERE credit.album_id = ANY($1::uuid[])
		ORDER BY credit.sort_order ASC, ar.id ASC
	`, ids)
	if err != nil {
		return fmt.Errorf("query album credits: %w", err)
	}
	defer rows.Close()
	grouped := make(map[string][]ArtistReferenceRecord, len(ids))
	for rows.Next() {
		var parentID string
		var credit ArtistReferenceRecord
		if err := rows.Scan(&parentID, &credit.ID, &credit.Name); err != nil {
			return fmt.Errorf("scan album credit: %w", err)
		}
		grouped[parentID] = appendUniqueCredit(grouped[parentID], credit)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate album credits: %w", err)
	}
	for index := range records {
		records[index].Artists = nonNilCredits(grouped[records[index].ID])
	}
	return nil
}

func scanTrack(row pgx.Row) (TrackRecord, error) {
	var record TrackRecord
	var albumID, albumTitle *string
	var assetID, objectKey, mimeType, checksum *string
	var width, height *int
	var assetUpdatedAt *time.Time
	err := row.Scan(
		&record.ID,
		&record.Title,
		&record.NormalizedTitle,
		&record.DurationMS,
		&record.TrackNumber,
		&record.DiscNumber,
		&record.PublishedAt,
		&record.Version,
		&albumID,
		&albumTitle,
		&record.Favorite,
		&assetID,
		&objectKey,
		&mimeType,
		&checksum,
		&width,
		&height,
		&assetUpdatedAt,
	)
	if err != nil {
		return TrackRecord{}, err
	}
	if albumID != nil && albumTitle != nil {
		record.Album = &AlbumReferenceRecord{ID: *albumID, Title: *albumTitle}
	}
	record.Artwork = artworkFromScan(assetID, objectKey, mimeType, checksum, width, height, assetUpdatedAt)
	record.Artists = make([]ArtistReferenceRecord, 0)
	return record, nil
}

func scanArtist(row pgx.Row) (ArtistRecord, error) {
	var record ArtistRecord
	var assetID, objectKey, mimeType, checksum *string
	var width, height *int
	var updatedAt *time.Time
	err := row.Scan(
		&record.ID,
		&record.Name,
		&record.NormalizedName,
		&record.Description,
		&assetID,
		&objectKey,
		&mimeType,
		&checksum,
		&width,
		&height,
		&updatedAt,
	)
	if err != nil {
		return ArtistRecord{}, err
	}
	record.Artwork = artworkFromScan(assetID, objectKey, mimeType, checksum, width, height, updatedAt)
	return record, nil
}

func scanAlbum(row pgx.Row) (AlbumRecord, error) {
	var record AlbumRecord
	var assetID, objectKey, mimeType, checksum *string
	var width, height *int
	var updatedAt *time.Time
	err := row.Scan(
		&record.ID,
		&record.Title,
		&record.NormalizedTitle,
		&record.Description,
		&record.ReleaseDate,
		&record.TrackCount,
		&assetID,
		&objectKey,
		&mimeType,
		&checksum,
		&width,
		&height,
		&updatedAt,
	)
	if err != nil {
		return AlbumRecord{}, err
	}
	record.Cover = artworkFromScan(assetID, objectKey, mimeType, checksum, width, height, updatedAt)
	record.Artists = make([]ArtistReferenceRecord, 0)
	return record, nil
}

func artworkFromScan(
	assetID, objectKey, mimeType, checksum *string,
	width, height *int,
	updatedAt *time.Time,
) *ArtworkAsset {
	if assetID == nil || objectKey == nil || mimeType == nil || updatedAt == nil {
		return nil
	}
	return &ArtworkAsset{
		ID:             *assetID,
		ObjectKey:      *objectKey,
		MimeType:       *mimeType,
		ChecksumSHA256: checksum,
		Width:          width,
		Height:         height,
		UpdatedAt:      *updatedAt,
	}
}

func trackAfterCondition(sort TrackSort, cursor *TrackCursor, arguments *[]any) (string, error) {
	switch sort {
	case TrackSortPublishedDesc:
		if cursor.PublishedAt == nil || cursor.ID == "" {
			return "", errors.New("invalid published track cursor")
		}
		valuePosition := appendArgument(arguments, *cursor.PublishedAt)
		idPosition := appendArgument(arguments, cursor.ID)
		return fmt.Sprintf("(t.published_at < $%d OR (t.published_at = $%d AND t.id < $%d))", valuePosition, valuePosition, idPosition), nil
	case TrackSortTitleAsc, TrackSortTitleDesc:
		if cursor.Title == nil || cursor.ID == "" {
			return "", errors.New("invalid title track cursor")
		}
		operator := ">"
		if sort == TrackSortTitleDesc {
			operator = "<"
		}
		valuePosition := appendArgument(arguments, *cursor.Title)
		idPosition := appendArgument(arguments, cursor.ID)
		return fmt.Sprintf("(t.normalized_title %s $%d OR (t.normalized_title = $%d AND t.id %s $%d))", operator, valuePosition, valuePosition, operator, idPosition), nil
	case TrackSortAlbumOrderAsc:
		if cursor.DiscNumber == nil || cursor.TrackNumber == nil || cursor.ID == "" {
			return "", errors.New("invalid album-order track cursor")
		}
		discPosition := appendArgument(arguments, *cursor.DiscNumber)
		trackPosition := appendArgument(arguments, *cursor.TrackNumber)
		idPosition := appendArgument(arguments, cursor.ID)
		return fmt.Sprintf("(COALESCE(t.disc_number, 1), COALESCE(t.track_number, 0), t.id) > ($%d, $%d, $%d)", discPosition, trackPosition, idPosition), nil
	default:
		return "", fmt.Errorf("unsupported track sort %q", sort)
	}
}

func albumAfterCondition(sort AlbumSort, cursor *AlbumCursor, arguments *[]any) (string, error) {
	switch sort {
	case AlbumSortTitleAsc, AlbumSortTitleDesc:
		if cursor.Title == nil || cursor.ID == "" {
			return "", errors.New("invalid title album cursor")
		}
		operator := ">"
		if sort == AlbumSortTitleDesc {
			operator = "<"
		}
		valuePosition := appendArgument(arguments, *cursor.Title)
		idPosition := appendArgument(arguments, cursor.ID)
		return fmt.Sprintf("(al.normalized_title %s $%d OR (al.normalized_title = $%d AND al.id %s $%d))", operator, valuePosition, valuePosition, operator, idPosition), nil
	case AlbumSortReleaseDateDesc:
		if cursor.ID == "" {
			return "", errors.New("invalid release-date album cursor")
		}
		idPosition := appendArgument(arguments, cursor.ID)
		if cursor.NullRelease {
			return fmt.Sprintf("(al.release_date IS NULL AND al.id < $%d)", idPosition), nil
		}
		if cursor.ReleaseDate == nil {
			return "", errors.New("invalid release-date album cursor")
		}
		datePosition := appendArgument(arguments, *cursor.ReleaseDate)
		return fmt.Sprintf("(al.release_date < $%d::date OR (al.release_date = $%d::date AND al.id < $%d) OR al.release_date IS NULL)", datePosition, datePosition, idPosition), nil
	default:
		return "", fmt.Errorf("unsupported album sort %q", sort)
	}
}

func trackOrderSQL(sort TrackSort) (string, error) {
	switch sort {
	case TrackSortPublishedDesc:
		return "t.published_at DESC, t.id DESC", nil
	case TrackSortTitleAsc:
		return "t.normalized_title ASC, t.id ASC", nil
	case TrackSortTitleDesc:
		return "t.normalized_title DESC, t.id DESC", nil
	case TrackSortAlbumOrderAsc:
		return "COALESCE(t.disc_number, 1) ASC, COALESCE(t.track_number, 0) ASC, t.id ASC", nil
	default:
		return "", fmt.Errorf("unsupported track sort %q", sort)
	}
}

func albumOrderSQL(sort AlbumSort) (string, error) {
	switch sort {
	case AlbumSortReleaseDateDesc:
		return "al.release_date DESC NULLS LAST, al.id DESC", nil
	case AlbumSortTitleAsc:
		return "al.normalized_title ASC, al.id ASC", nil
	case AlbumSortTitleDesc:
		return "al.normalized_title DESC, al.id DESC", nil
	default:
		return "", fmt.Errorf("unsupported album sort %q", sort)
	}
}

func fuzzySQL(column string, queryPosition, patternPosition int, trigram bool) string {
	contains := fmt.Sprintf(`%s ILIKE $%d ESCAPE '\'`, column, patternPosition)
	if !trigram {
		return contains
	}
	return fmt.Sprintf("(%s OR %s %% $%d)", contains, column, queryPosition)
}

func appendArgument(arguments *[]any, value any) int {
	*arguments = append(*arguments, value)
	return len(*arguments)
}

func appendUniqueCredit(items []ArtistReferenceRecord, candidate ArtistReferenceRecord) []ArtistReferenceRecord {
	for _, item := range items {
		if item.ID == candidate.ID {
			return items
		}
	}
	return append(items, candidate)
}

func nonNilCredits(items []ArtistReferenceRecord) []ArtistReferenceRecord {
	if items == nil {
		return make([]ArtistReferenceRecord, 0)
	}
	return items
}

func trackIDs(records []TrackRecord) []string {
	result := make([]string, len(records))
	for index := range records {
		result[index] = records[index].ID
	}
	return result
}

func albumIDs(records []AlbumRecord) []string {
	result := make([]string, len(records))
	for index := range records {
		result[index] = records[index].ID
	}
	return result
}

const trackSelectSQL = `
	SELECT
		t.id, t.title, t.normalized_title, t.duration_ms, t.track_number,
		t.disc_number, t.published_at, t.version,
		al.id, al.title,
		(favorite.track_id IS NOT NULL) AS is_favorite,
		asset.id, asset.object_key, asset.mime_type, asset.checksum_sha256,
		asset.width, asset.height, asset.updated_at
	FROM tracks t
	LEFT JOIN albums al ON al.id = t.album_id
	LEFT JOIN favorite_tracks favorite ON favorite.track_id = t.id AND favorite.user_id = $1
	LEFT JOIN media_assets asset ON asset.id = al.cover_asset_id AND asset.status = 'READY'
`

const artistSelectSQL = `
	SELECT
		ar.id, ar.name, ar.normalized_name, ar.description,
		asset.id, asset.object_key, asset.mime_type, asset.checksum_sha256,
		asset.width, asset.height, asset.updated_at
	FROM artists ar
	LEFT JOIN media_assets asset ON asset.id = ar.artwork_asset_id AND asset.status = 'READY'
`

const albumSelectSQL = `
	SELECT
		al.id, al.title, al.normalized_title, al.description, al.release_date::text,
		(SELECT count(*)::int FROM tracks counted_track WHERE counted_track.album_id = al.id AND counted_track.status = 'READY') AS track_count,
		asset.id, asset.object_key, asset.mime_type, asset.checksum_sha256,
		asset.width, asset.height, asset.updated_at
	FROM albums al
	LEFT JOIN media_assets asset ON asset.id = al.cover_asset_id AND asset.status = 'READY'
`

const visibleAlbumSQL = `EXISTS (
	SELECT 1 FROM tracks visible_track
	WHERE visible_track.album_id = al.id
	  AND visible_track.status = 'READY'
	  AND visible_track.published_at IS NOT NULL
)`
