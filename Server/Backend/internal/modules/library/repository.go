package library

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

func (repository *Repository) ListFavorites(
	ctx context.Context,
	input ListFavoritesQuery,
) ([]FavoriteRecord, error) {
	conditions := []string{
		"favorite.user_id = $1",
		"track.status = 'READY'",
		"track.published_at IS NOT NULL",
	}
	arguments := []any{input.UserID}
	order := "favorite.created_at DESC, favorite.track_id DESC"
	if input.After != nil {
		switch input.Sort {
		case FavoriteSortFavoritedDesc:
			if input.After.CreatedAt == nil {
				return nil, errors.New("favorite time cursor is incomplete")
			}
			timePosition := appendRepositoryArgument(&arguments, *input.After.CreatedAt)
			idPosition := appendRepositoryArgument(&arguments, input.After.TrackID)
			conditions = append(conditions, fmt.Sprintf(
				"(favorite.created_at < $%d OR (favorite.created_at = $%d AND favorite.track_id < $%d))",
				timePosition, timePosition, idPosition,
			))
		case FavoriteSortTitleAsc:
			if input.After.Title == nil {
				return nil, errors.New("favorite title cursor is incomplete")
			}
			titlePosition := appendRepositoryArgument(&arguments, *input.After.Title)
			idPosition := appendRepositoryArgument(&arguments, input.After.TrackID)
			conditions = append(conditions, fmt.Sprintf(
				"(track.normalized_title > $%d OR (track.normalized_title = $%d AND track.id > $%d))",
				titlePosition, titlePosition, idPosition,
			))
			order = "track.normalized_title ASC, track.id ASC"
		default:
			return nil, fmt.Errorf("unsupported favorite sort %q", input.Sort)
		}
	} else if input.Sort == FavoriteSortTitleAsc {
		order = "track.normalized_title ASC, track.id ASC"
	} else if input.Sort != FavoriteSortFavoritedDesc {
		return nil, fmt.Errorf("unsupported favorite sort %q", input.Sort)
	}
	limitPosition := appendRepositoryArgument(&arguments, input.Limit)
	rows, err := repository.pool.Query(ctx, `
		SELECT favorite.track_id, favorite.created_at, track.normalized_title
		FROM favorite_tracks favorite
		JOIN tracks track ON track.id = favorite.track_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY `+order+`
		LIMIT $`+fmt.Sprintf("%d", limitPosition), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list favorite tracks: %w", err)
	}
	defer rows.Close()
	result := make([]FavoriteRecord, 0)
	for rows.Next() {
		var item FavoriteRecord
		if err := rows.Scan(&item.TrackID, &item.FavoritedAt, &item.NormalizedTitle); err != nil {
			return nil, fmt.Errorf("scan favorite track: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate favorite tracks: %w", err)
	}
	return result, nil
}

func (repository *Repository) PlayableTrackExists(ctx context.Context, trackID string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tracks
			WHERE id = $1 AND status = 'READY' AND published_at IS NOT NULL
		)`, trackID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check playable track: %w", err)
	}
	return exists, nil
}

func (repository *Repository) TrackExists(ctx context.Context, trackID string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM tracks WHERE id = $1)", trackID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check track existence: %w", err)
	}
	return exists, nil
}

func (repository *Repository) AddFavorite(ctx context.Context, userID, trackID string) (time.Time, error) {
	var createdAt time.Time
	err := repository.pool.QueryRow(ctx, `
		INSERT INTO favorite_tracks (user_id, track_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, track_id) DO NOTHING
		RETURNING created_at`, userID, trackID).Scan(&createdAt)
	if err == nil {
		return createdAt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("add favorite track: %w", err)
	}
	err = repository.pool.QueryRow(ctx, `
		SELECT created_at
		FROM favorite_tracks
		WHERE user_id = $1 AND track_id = $2`, userID, trackID).Scan(&createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("read favorite track after insert: %w", err)
	}
	return createdAt, nil
}

func (repository *Repository) RemoveFavorite(ctx context.Context, userID, trackID string) error {
	if _, err := repository.pool.Exec(ctx, `
		DELETE FROM favorite_tracks
		WHERE user_id = $1 AND track_id = $2`, userID, trackID); err != nil {
		return fmt.Errorf("remove favorite track: %w", err)
	}
	return nil
}

func (repository *Repository) ListHistory(
	ctx context.Context,
	input ListHistoryQuery,
) ([]HistoryRecord, error) {
	conditions := []string{"user_id = $1"}
	arguments := []any{input.UserID}
	if input.After != nil {
		timePosition := appendRepositoryArgument(&arguments, input.After.LastPlayedAt)
		idPosition := appendRepositoryArgument(&arguments, input.After.TrackID)
		conditions = append(conditions, fmt.Sprintf(
			"(last_played_at < $%d OR (last_played_at = $%d AND track_id < $%d))",
			timePosition, timePosition, idPosition,
		))
	}
	limitPosition := appendRepositoryArgument(&arguments, input.Limit)
	rows, err := repository.pool.Query(ctx, `
		SELECT track_id, last_position_ms, play_count, last_played_at,
		       completed, last_playback_session_id, updated_at
		FROM play_history
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY last_played_at DESC, track_id DESC
		LIMIT $`+fmt.Sprintf("%d", limitPosition), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list play history: %w", err)
	}
	defer rows.Close()
	result := make([]HistoryRecord, 0)
	for rows.Next() {
		item, err := scanHistory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan play history: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate play history: %w", err)
	}
	return result, nil
}

func (repository *Repository) UpsertPlayback(ctx context.Context, input PlaybackWrite) (HistoryRecord, error) {
	started := input.Event == PlaybackEventStarted
	completed := input.Event == PlaybackEventCompleted
	row := repository.pool.QueryRow(ctx, `
		INSERT INTO play_history (
			user_id, track_id, last_position_ms, play_count, last_played_at,
			completed, last_playback_session_id, updated_at
		) VALUES ($1, $2, $3, CASE WHEN $6 THEN 1 ELSE 0 END, $4, $7, $5, $8)
		ON CONFLICT (user_id, track_id) DO UPDATE SET
			last_position_ms = excluded.last_position_ms,
			play_count = CASE
				WHEN $6 AND play_history.last_playback_session_id <> excluded.last_playback_session_id
					THEN play_history.play_count + 1
				ELSE play_history.play_count
			END,
			last_played_at = excluded.last_played_at,
			completed = excluded.completed,
			last_playback_session_id = excluded.last_playback_session_id,
			updated_at = $8
		WHERE play_history.last_played_at <= excluded.last_played_at
		RETURNING track_id, last_position_ms, play_count, last_played_at,
		          completed, last_playback_session_id, updated_at`,
		input.UserID,
		input.TrackID,
		input.PositionMS,
		input.OccurredAt,
		input.PlaybackSessionID,
		started,
		completed,
		input.UpdatedAt,
	)
	result, err := scanHistory(row)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return HistoryRecord{}, fmt.Errorf("upsert play history: %w", err)
	}
	result, err = repository.findHistory(ctx, input.UserID, input.TrackID)
	if err != nil {
		return HistoryRecord{}, fmt.Errorf("read play history after stale event: %w", err)
	}
	return result, nil
}

func (repository *Repository) findHistory(ctx context.Context, userID, trackID string) (HistoryRecord, error) {
	return scanHistory(repository.pool.QueryRow(ctx, `
		SELECT track_id, last_position_ms, play_count, last_played_at,
		       completed, last_playback_session_id, updated_at
		FROM play_history
		WHERE user_id = $1 AND track_id = $2`, userID, trackID))
}

type historyScanner interface {
	Scan(...any) error
}

func scanHistory(scanner historyScanner) (HistoryRecord, error) {
	var result HistoryRecord
	err := scanner.Scan(
		&result.TrackID,
		&result.LastPositionMS,
		&result.PlayCount,
		&result.LastPlayedAt,
		&result.Completed,
		&result.LastPlaybackSessionID,
		&result.UpdatedAt,
	)
	return result, err
}

func appendRepositoryArgument(arguments *[]any, value any) int {
	*arguments = append(*arguments, value)
	return len(*arguments)
}
