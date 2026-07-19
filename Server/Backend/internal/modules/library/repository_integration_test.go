package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/testsupport"
)

// TestLibraryRepositoryProductionSideEffects is opt-in because it writes
// isolated rows to the configured PostgreSQL database. Every row is keyed by
// fresh UUIDs and removed even when an assertion fails.
func TestLibraryRepositoryProductionSideEffects(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production library lifecycle")
	}
	testsupport.RequireWriteIntegration(t)
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absoluteEnvironmentPath))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userID := uuid.NewString()
	trackID := uuid.NewString()
	username := "library_it_" + strings.ReplaceAll(userID[:13], "-", "")
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM tracks WHERE id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM users WHERE id = $1", userID)
	}
	cleanup()
	t.Cleanup(cleanup)
	passwordHash, err := platformsecurity.HashPassword("library-integration-" + userID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO users (
			id, username, normalized_username, password_hash, role, status,
			auth_version, version
		) VALUES ($1, $2, $2, $3, 'USER', 'ACTIVE', 1, 1)`,
		userID, username, passwordHash,
	); err != nil {
		t.Fatal(err)
	}
	publishedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (
			id, title, normalized_title, duration_ms, status, published_at
		) VALUES ($1, 'Library Integration Track', 'library integration track', 123000, 'READY', $2)`,
		trackID, publishedAt,
	); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	playable, err := repository.PlayableTrackExists(ctx, trackID)
	if err != nil || !playable {
		t.Fatalf("PlayableTrackExists() = %v, %v", playable, err)
	}
	exists, err := repository.TrackExists(ctx, trackID)
	if err != nil || !exists {
		t.Fatalf("TrackExists() = %v, %v", exists, err)
	}
	firstFavorite, err := repository.AddFavorite(ctx, userID, trackID)
	if err != nil {
		t.Fatal(err)
	}
	secondFavorite, err := repository.AddFavorite(ctx, userID, trackID)
	if err != nil || !secondFavorite.Equal(firstFavorite) {
		t.Fatalf("idempotent favorite timestamp = %v/%v, %v", firstFavorite, secondFavorite, err)
	}
	for _, sort := range []FavoriteSort{FavoriteSortFavoritedDesc, FavoriteSortTitleAsc} {
		favorites, err := repository.ListFavorites(ctx, ListFavoritesQuery{UserID: userID, Sort: sort, Limit: 2})
		if err != nil || len(favorites) != 1 || favorites[0].TrackID != trackID {
			t.Fatalf("ListFavorites(%s) = %#v, %v", sort, favorites, err)
		}
		cursor := &FavoriteCursor{TrackID: trackID}
		if sort == FavoriteSortFavoritedDesc {
			cursor.CreatedAt = &favorites[0].FavoritedAt
		} else {
			cursor.Title = &favorites[0].NormalizedTitle
		}
		if _, err := repository.ListFavorites(ctx, ListFavoritesQuery{UserID: userID, Sort: sort, After: cursor, Limit: 1}); err != nil {
			t.Fatalf("ListFavorites(%s) cursor: %v", sort, err)
		}
	}

	sessionOne := uuid.NewString()
	sessionTwo := uuid.NewString()
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
	write := func(session string, position int64, occurredAt time.Time, event PlaybackEvent, updatedAt time.Time) HistoryRecord {
		t.Helper()
		result, err := repository.UpsertPlayback(ctx, PlaybackWrite{
			UserID: userID, TrackID: trackID, PlaybackSessionID: session,
			PositionMS: position, OccurredAt: occurredAt, Event: event, UpdatedAt: updatedAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	started := write(sessionOne, 100, base, PlaybackEventStarted, base.Add(time.Second))
	if started.PlayCount != 1 || started.Completed {
		t.Fatalf("initial STARTED = %#v", started)
	}
	progress := write(sessionOne, 40, base.Add(time.Minute), PlaybackEventProgress, base.Add(time.Minute+time.Second))
	if progress.PlayCount != 1 || progress.LastPositionMS != 40 {
		t.Fatalf("backward seek = %#v", progress)
	}
	completed := write(sessionOne, 1_000, base.Add(2*time.Minute), PlaybackEventCompleted, base.Add(2*time.Minute+time.Second))
	if completed.PlayCount != 1 || !completed.Completed {
		t.Fatalf("COMPLETED = %#v", completed)
	}
	stale := write(sessionTwo, 1, base.Add(time.Minute+30*time.Second), PlaybackEventPaused, base.Add(3*time.Minute))
	if stale.LastPositionMS != 1_000 || !stale.Completed || !stale.LastPlayedAt.Equal(completed.LastPlayedAt) || !stale.UpdatedAt.Equal(completed.UpdatedAt) {
		t.Fatalf("stale event mutated history = %#v", stale)
	}
	sameSession := write(sessionOne, 0, base.Add(3*time.Minute), PlaybackEventStarted, base.Add(3*time.Minute+time.Second))
	if sameSession.PlayCount != 1 || sameSession.Completed {
		t.Fatalf("same-session STARTED = %#v", sameSession)
	}
	newSession := write(sessionTwo, 0, base.Add(4*time.Minute), PlaybackEventStarted, base.Add(4*time.Minute+time.Second))
	if newSession.PlayCount != 2 {
		t.Fatalf("new-session STARTED = %#v", newSession)
	}
	history, err := repository.ListHistory(ctx, ListHistoryQuery{UserID: userID, Limit: 2})
	if err != nil || len(history) != 1 || history[0].PlayCount != 2 {
		t.Fatalf("ListHistory() = %#v, %v", history, err)
	}
	if _, err := repository.ListHistory(ctx, ListHistoryQuery{
		UserID: userID,
		After:  &HistoryCursor{LastPlayedAt: history[0].LastPlayedAt, TrackID: trackID},
		Limit:  1,
	}); err != nil {
		t.Fatalf("ListHistory() cursor: %v", err)
	}

	if err := repository.RemoveFavorite(ctx, userID, trackID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RemoveFavorite(ctx, userID, trackID); err != nil {
		t.Fatalf("idempotent RemoveFavorite(): %v", err)
	}
	var favoriteCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int FROM favorite_tracks WHERE user_id = $1 AND track_id = $2`,
		userID, trackID,
	).Scan(&favoriteCount); err != nil || favoriteCount != 0 {
		t.Fatalf("favorite side effect count = %d, %v", favoriteCount, err)
	}
	var persistedCount int64
	var persistedCompleted bool
	if err := pool.QueryRow(ctx, `
		SELECT play_count, completed FROM play_history WHERE user_id = $1 AND track_id = $2`,
		userID, trackID,
	).Scan(&persistedCount, &persistedCompleted); err != nil || persistedCount != 2 || persistedCompleted {
		t.Fatalf("history side effect = %d/%v, %v", persistedCount, persistedCompleted, err)
	}
}
