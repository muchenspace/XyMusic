package playlist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) ListOwned(ctx context.Context, input ListOwnedQuery) ([]PlaylistRecord, error) {
	conditions := []string{"p.owner_id = $1"}
	arguments := []any{input.OwnerID}
	if input.After != nil {
		condition, err := playlistAfterCondition(input.Sort, input.After, &arguments)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	order, err := playlistOrderSQL(input.Sort)
	if err != nil {
		return nil, err
	}
	limitPosition := appendArgument(&arguments, input.Limit)
	statement := playlistSelectSQL + " WHERE " + strings.Join(conditions, " AND ") +
		" ORDER BY " + order + fmt.Sprintf(" LIMIT $%d", limitPosition)
	return repository.queryPlaylists(ctx, statement, arguments...)
}

func (repository *Repository) CreatePlaylist(ctx context.Context, input CreatePlaylistParams) (PlaylistRecord, error) {
	var playlistID string
	err := repository.pool.QueryRow(ctx, `
		INSERT INTO playlists (owner_id, name, description, visibility)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, input.OwnerID, input.Name, input.Description, input.Visibility).Scan(&playlistID)
	if err != nil {
		return PlaylistRecord{}, fmt.Errorf("create playlist: %w", err)
	}
	return repository.FindPlaylist(ctx, playlistID)
}

func (repository *Repository) FindPlaylist(ctx context.Context, playlistID string) (PlaylistRecord, error) {
	rows, err := repository.queryPlaylists(ctx, playlistSelectSQL+" WHERE p.id = $1 LIMIT 1", playlistID)
	if err != nil {
		return PlaylistRecord{}, err
	}
	if len(rows) == 0 {
		return PlaylistRecord{}, ErrNotFound
	}
	return rows[0], nil
}

func (repository *Repository) ListEntries(ctx context.Context, input ListEntriesQuery) ([]EntryRecord, error) {
	conditions := []string{"playlist_id = $1"}
	arguments := []any{input.PlaylistID}
	if input.After != nil {
		positionArgument := appendArgument(&arguments, input.After.Position)
		idArgument := appendArgument(&arguments, input.After.ID)
		conditions = append(conditions, fmt.Sprintf(
			"(position > $%d OR (position = $%d AND id > $%d))",
			positionArgument, positionArgument, idArgument,
		))
	}
	limitArgument := appendArgument(&arguments, input.Limit)
	rows, err := repository.pool.Query(ctx, `
		SELECT id, playlist_id, track_id, position, added_by, added_at
		FROM playlist_tracks
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY position ASC, id ASC
		LIMIT $`+fmt.Sprintf("%d", limitArgument), arguments...)
	if err != nil {
		return nil, fmt.Errorf("query playlist entries: %w", err)
	}
	defer rows.Close()
	result := make([]EntryRecord, 0)
	for rows.Next() {
		var entry EntryRecord
		if err := rows.Scan(&entry.ID, &entry.PlaylistID, &entry.TrackID, &entry.Position, &entry.AddedBy, &entry.AddedAt); err != nil {
			return nil, fmt.Errorf("scan playlist entry: %w", err)
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playlist entries: %w", err)
	}
	return result, nil
}

func (repository *Repository) UpdatePlaylist(ctx context.Context, input UpdatePlaylistParams) (PlaylistRecord, error) {
	arguments := []any{input.PlaylistID, input.OwnerID, input.ExpectedVersion}
	sets := make([]string, 0, 5)
	if input.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", appendArgument(&arguments, *input.Name)))
	}
	if input.SetDescription {
		sets = append(sets, fmt.Sprintf("description = $%d", appendArgument(&arguments, input.Description)))
	}
	if input.Visibility != nil {
		sets = append(sets, fmt.Sprintf("visibility = $%d", appendArgument(&arguments, *input.Visibility)))
	}
	sets = append(sets, "version = version + 1", "updated_at = now()")
	var playlistID string
	err := repository.pool.QueryRow(ctx, `
		UPDATE playlists
		SET `+strings.Join(sets, ", ")+`
		WHERE id = $1 AND owner_id = $2 AND version = $3
		RETURNING id
	`, arguments...).Scan(&playlistID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaylistRecord{}, repository.ownershipOrVersion(ctx, input.OwnerID, input.PlaylistID, input.ExpectedVersion)
	}
	if err != nil {
		return PlaylistRecord{}, fmt.Errorf("update playlist: %w", err)
	}
	return repository.FindPlaylist(ctx, playlistID)
}

func (repository *Repository) DeletePlaylist(ctx context.Context, ownerID, playlistID string, expectedVersion int) error {
	command, err := repository.pool.Exec(ctx, `
		DELETE FROM playlists
		WHERE id = $1 AND owner_id = $2 AND version = $3
	`, playlistID, ownerID, expectedVersion)
	if err != nil {
		return fmt.Errorf("delete playlist: %w", err)
	}
	if command.RowsAffected() == 0 {
		return repository.ownershipOrVersion(ctx, ownerID, playlistID, expectedVersion)
	}
	return nil
}

func (repository *Repository) ReadyTrackExists(ctx context.Context, trackID string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tracks
			WHERE id = $1 AND status = 'READY' AND published_at IS NOT NULL
		)
	`, trackID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check playlist track: %w", err)
	}
	return exists, nil
}

func (repository *Repository) AddTrack(ctx context.Context, input AddTrackParams) (AddTrackMutation, error) {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AddTrackMutation{}, fmt.Errorf("begin add playlist track: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	currentVersion, err := lockOwnedPlaylist(ctx, transaction, input.OwnerID, input.PlaylistID)
	if err != nil {
		return AddTrackMutation{}, err
	}
	if currentVersion != input.ExpectedVersion {
		return AddTrackMutation{}, versionConflict(input.PlaylistID, input.ExpectedVersion, currentVersion)
	}
	var duplicate bool
	if err := transaction.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2
		)
	`, input.PlaylistID, input.TrackID).Scan(&duplicate); err != nil {
		return AddTrackMutation{}, fmt.Errorf("check duplicate playlist track: %w", err)
	}
	if duplicate {
		return AddTrackMutation{}, ErrDuplicateTrack
	}
	var count int
	var maximumPosition *int
	if err := transaction.QueryRow(ctx, `
		SELECT count(*)::int, max(position)::int
		FROM playlist_tracks
		WHERE playlist_id = $1
	`, input.PlaylistID).Scan(&count, &maximumPosition); err != nil {
		return AddTrackMutation{}, fmt.Errorf("query playlist entry statistics: %w", err)
	}
	if count >= MaxPlaylistEntries {
		return AddTrackMutation{}, ErrPlaylistFull
	}
	insertPosition := 0
	if maximumPosition != nil {
		insertPosition = *maximumPosition + 1
	}
	if input.InsertAfterEntryID != nil {
		var afterPosition int
		err := transaction.QueryRow(ctx, `
			SELECT position FROM playlist_tracks WHERE id = $1 AND playlist_id = $2
		`, *input.InsertAfterEntryID, input.PlaylistID).Scan(&afterPosition)
		if errors.Is(err, pgx.ErrNoRows) {
			return AddTrackMutation{}, ErrInsertAfterMissing
		}
		if err != nil {
			return AddTrackMutation{}, fmt.Errorf("find insert-after playlist entry: %w", err)
		}
		insertPosition = afterPosition + 1
	}
	if err := shiftPositionsForInsert(ctx, transaction, input.PlaylistID, insertPosition); err != nil {
		return AddTrackMutation{}, err
	}
	var entry EntryRecord
	err = transaction.QueryRow(ctx, `
		INSERT INTO playlist_tracks (playlist_id, track_id, position, added_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, playlist_id, track_id, position, added_by, added_at
	`, input.PlaylistID, input.TrackID, insertPosition, input.OwnerID).Scan(
		&entry.ID, &entry.PlaylistID, &entry.TrackID, &entry.Position, &entry.AddedBy, &entry.AddedAt,
	)
	if err != nil {
		return AddTrackMutation{}, fmt.Errorf("insert playlist track: %w", err)
	}
	mutation, err := advancePlaylistVersion(ctx, transaction, input.PlaylistID, input.Now)
	if err != nil {
		return AddTrackMutation{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return AddTrackMutation{}, fmt.Errorf("commit add playlist track: %w", err)
	}
	return AddTrackMutation{
		PlaylistID: mutation.PlaylistID,
		Version:    mutation.Version,
		UpdatedAt:  mutation.UpdatedAt,
		Entry:      entry,
	}, nil
}

func (repository *Repository) RemoveTrack(ctx context.Context, input RemoveTrackParams) (VersionMutation, error) {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VersionMutation{}, fmt.Errorf("begin remove playlist track: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	currentVersion, err := lockOwnedPlaylist(ctx, transaction, input.OwnerID, input.PlaylistID)
	if err != nil {
		return VersionMutation{}, err
	}
	if currentVersion != input.ExpectedVersion {
		return VersionMutation{}, versionConflict(input.PlaylistID, input.ExpectedVersion, currentVersion)
	}
	var removedPosition int
	err = transaction.QueryRow(ctx, `
		SELECT position FROM playlist_tracks WHERE id = $1 AND playlist_id = $2
	`, input.EntryID, input.PlaylistID).Scan(&removedPosition)
	if errors.Is(err, pgx.ErrNoRows) {
		return VersionMutation{}, ErrEntryNotFound
	}
	if err != nil {
		return VersionMutation{}, fmt.Errorf("find playlist entry to remove: %w", err)
	}
	if _, err := transaction.Exec(ctx, "DELETE FROM playlist_tracks WHERE id = $1", input.EntryID); err != nil {
		return VersionMutation{}, fmt.Errorf("delete playlist entry: %w", err)
	}
	if err := compactPositions(ctx, transaction, input.PlaylistID, removedPosition); err != nil {
		return VersionMutation{}, err
	}
	mutation, err := advancePlaylistVersion(ctx, transaction, input.PlaylistID, input.Now)
	if err != nil {
		return VersionMutation{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return VersionMutation{}, fmt.Errorf("commit remove playlist track: %w", err)
	}
	return mutation, nil
}

func (repository *Repository) Reorder(ctx context.Context, input ReorderParams) (VersionMutation, error) {
	transaction, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VersionMutation{}, fmt.Errorf("begin reorder playlist: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	currentVersion, err := lockOwnedPlaylist(ctx, transaction, input.OwnerID, input.PlaylistID)
	if err != nil {
		return VersionMutation{}, err
	}
	if currentVersion != input.ExpectedVersion {
		return VersionMutation{}, versionConflict(input.PlaylistID, input.ExpectedVersion, currentVersion)
	}
	rows, err := transaction.Query(ctx, "SELECT id FROM playlist_tracks WHERE playlist_id = $1", input.PlaylistID)
	if err != nil {
		return VersionMutation{}, fmt.Errorf("query entries for playlist reorder: %w", err)
	}
	current := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return VersionMutation{}, fmt.Errorf("scan entry for playlist reorder: %w", err)
		}
		current[id] = struct{}{}
	}
	iterationErr := rows.Err()
	rows.Close()
	if iterationErr != nil {
		return VersionMutation{}, fmt.Errorf("iterate entries for playlist reorder: %w", iterationErr)
	}
	if len(current) != len(input.OrderedEntryIDs) {
		return VersionMutation{}, ErrIncompleteOrder
	}
	for _, id := range input.OrderedEntryIDs {
		if _, exists := current[id]; !exists {
			return VersionMutation{}, ErrUnknownOrderEntry
		}
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE playlist_tracks SET position = position + $2 WHERE playlist_id = $1
	`, input.PlaylistID, positionShift); err != nil {
		return VersionMutation{}, fmt.Errorf("shift playlist positions for reorder: %w", err)
	}
	if len(input.OrderedEntryIDs) > 0 {
		if _, err := transaction.Exec(ctx, `
			UPDATE playlist_tracks AS entry
			SET position = (ordered.ordinality - 1)::int
			FROM unnest($2::uuid[]) WITH ORDINALITY AS ordered(id, ordinality)
			WHERE entry.playlist_id = $1 AND entry.id = ordered.id
		`, input.PlaylistID, input.OrderedEntryIDs); err != nil {
			return VersionMutation{}, fmt.Errorf("write playlist order: %w", err)
		}
	}
	mutation, err := advancePlaylistVersion(ctx, transaction, input.PlaylistID, input.Now)
	if err != nil {
		return VersionMutation{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return VersionMutation{}, fmt.Errorf("commit playlist reorder: %w", err)
	}
	return mutation, nil
}

func (repository *Repository) queryPlaylists(ctx context.Context, statement string, arguments ...any) ([]PlaylistRecord, error) {
	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query playlists: %w", err)
	}
	defer rows.Close()
	result := make([]PlaylistRecord, 0)
	for rows.Next() {
		var playlist PlaylistRecord
		if err := rows.Scan(
			&playlist.ID,
			&playlist.OwnerID,
			&playlist.Name,
			&playlist.Description,
			&playlist.Visibility,
			&playlist.Version,
			&playlist.CreatedAt,
			&playlist.UpdatedAt,
			&playlist.TrackCount,
			&playlist.CoverAssetID,
		); err != nil {
			return nil, fmt.Errorf("scan playlist: %w", err)
		}
		result = append(result, playlist)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playlists: %w", err)
	}
	return result, nil
}

func (repository *Repository) ownershipOrVersion(ctx context.Context, ownerID, playlistID string, expectedVersion int) error {
	var currentVersion int
	err := repository.pool.QueryRow(ctx, `
		SELECT version FROM playlists WHERE id = $1 AND owner_id = $2
	`, playlistID, ownerID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect playlist ownership and version: %w", err)
	}
	return versionConflict(playlistID, expectedVersion, currentVersion)
}

func lockOwnedPlaylist(ctx context.Context, transaction pgx.Tx, ownerID, playlistID string) (int, error) {
	var currentVersion int
	err := transaction.QueryRow(ctx, `
		SELECT version FROM playlists WHERE id = $1 AND owner_id = $2 FOR UPDATE
	`, playlistID, ownerID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock owned playlist: %w", err)
	}
	return currentVersion, nil
}

func shiftPositionsForInsert(ctx context.Context, transaction pgx.Tx, playlistID string, position int) error {
	if _, err := transaction.Exec(ctx, `
		UPDATE playlist_tracks
		SET position = position + $3
		WHERE playlist_id = $1 AND position > $2 - 1
	`, playlistID, position, positionShift); err != nil {
		return fmt.Errorf("stage playlist positions for insert: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE playlist_tracks
		SET position = position - $2
		WHERE playlist_id = $1 AND position > $3
	`, playlistID, positionShift-1, positionShift-1); err != nil {
		return fmt.Errorf("commit playlist positions for insert: %w", err)
	}
	return nil
}

func compactPositions(ctx context.Context, transaction pgx.Tx, playlistID string, removedPosition int) error {
	if _, err := transaction.Exec(ctx, `
		UPDATE playlist_tracks
		SET position = position + $3
		WHERE playlist_id = $1 AND position > $2
	`, playlistID, removedPosition, positionShift); err != nil {
		return fmt.Errorf("stage playlist positions for removal: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE playlist_tracks
		SET position = position - $2
		WHERE playlist_id = $1 AND position > $3
	`, playlistID, positionShift+1, positionShift); err != nil {
		return fmt.Errorf("commit playlist positions for removal: %w", err)
	}
	return nil
}

func advancePlaylistVersion(ctx context.Context, transaction pgx.Tx, playlistID string, now time.Time) (VersionMutation, error) {
	var mutation VersionMutation
	err := transaction.QueryRow(ctx, `
		UPDATE playlists
		SET version = version + 1, updated_at = $2
		WHERE id = $1
		RETURNING id, version, updated_at
	`, playlistID, now).Scan(&mutation.PlaylistID, &mutation.Version, &mutation.UpdatedAt)
	if err != nil {
		return VersionMutation{}, fmt.Errorf("advance playlist version: %w", err)
	}
	return mutation, nil
}

func playlistAfterCondition(sort Sort, cursor *PlaylistCursor, arguments *[]any) (string, error) {
	if cursor.ID == "" {
		return "", errors.New("playlist cursor id is required")
	}
	switch sort {
	case SortUpdatedDesc:
		if cursor.UpdatedAt == nil {
			return "", errors.New("playlist updated cursor is required")
		}
		valuePosition := appendArgument(arguments, *cursor.UpdatedAt)
		idPosition := appendArgument(arguments, cursor.ID)
		return fmt.Sprintf("(p.updated_at < $%d OR (p.updated_at = $%d AND p.id < $%d))", valuePosition, valuePosition, idPosition), nil
	case SortNameAsc, SortNameDesc:
		if cursor.Name == nil {
			return "", errors.New("playlist name cursor is required")
		}
		operator := ">"
		if sort == SortNameDesc {
			operator = "<"
		}
		valuePosition := appendArgument(arguments, *cursor.Name)
		idPosition := appendArgument(arguments, cursor.ID)
		return fmt.Sprintf("(p.name %s $%d OR (p.name = $%d AND p.id %s $%d))", operator, valuePosition, valuePosition, operator, idPosition), nil
	default:
		return "", fmt.Errorf("unsupported playlist sort %q", sort)
	}
}

func playlistOrderSQL(sort Sort) (string, error) {
	switch sort {
	case SortUpdatedDesc:
		return "p.updated_at DESC, p.id DESC", nil
	case SortNameAsc:
		return "p.name ASC, p.id ASC", nil
	case SortNameDesc:
		return "p.name DESC, p.id DESC", nil
	default:
		return "", fmt.Errorf("unsupported playlist sort %q", sort)
	}
}

func appendArgument(arguments *[]any, value any) int {
	*arguments = append(*arguments, value)
	return len(*arguments)
}

func versionConflict(playlistID string, expectedVersion, currentVersion int) error {
	return &VersionConflictError{
		PlaylistID:      playlistID,
		ExpectedVersion: expectedVersion,
		CurrentVersion:  currentVersion,
	}
}

const playlistSelectSQL = `
	SELECT
		p.id, p.owner_id, p.name, p.description, p.visibility::text,
		p.version, p.created_at, p.updated_at,
		(SELECT count(*)::int FROM playlist_tracks counted WHERE counted.playlist_id = p.id) AS track_count,
		(
			SELECT album.cover_asset_id
			FROM playlist_tracks latest
			JOIN tracks track ON track.id = latest.track_id
			LEFT JOIN albums album ON album.id = track.album_id
			WHERE latest.playlist_id = p.id
			ORDER BY latest.added_at DESC, latest.id DESC
			LIMIT 1
		) AS cover_asset_id
	FROM playlists p
`
