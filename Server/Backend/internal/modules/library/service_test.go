package library

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

func TestListFavoritesPreservesSortCursorAndTrackPresenterShape(t *testing.T) {
	favoritedAt := time.Date(2026, 7, 16, 1, 2, 3, 456_000_000, time.UTC)
	calls := 0
	store := &libraryStoreStub{
		listFavorites: func(_ context.Context, query ListFavoritesQuery) ([]FavoriteRecord, error) {
			calls++
			if query.UserID != "user-1" || query.Sort != FavoriteSortTitleAsc || query.Limit != 2 {
				t.Fatalf("favorite query = %#v", query)
			}
			if calls == 1 {
				if query.After != nil {
					t.Fatalf("first cursor = %#v", query.After)
				}
				return []FavoriteRecord{
					{TrackID: "track-1", FavoritedAt: favoritedAt, NormalizedTitle: "alpha"},
					{TrackID: "track-2", FavoritedAt: favoritedAt.Add(-time.Hour), NormalizedTitle: "beta"},
				}, nil
			}
			if query.After == nil || query.After.Title == nil || *query.After.Title != "alpha" || query.After.TrackID != "track-1" {
				t.Fatalf("decoded favorite cursor = %#v", query.After)
			}
			return []FavoriteRecord{}, nil
		},
	}
	presenter := &trackPresenterStub{summaries: map[string]catalog.TrackSummaryDTO{
		"track-1": testTrack("track-1", true),
		"track-2": testTrack("track-2", true),
	}}
	service := newLibraryTestService(t, store, presenter, &passthroughIdempotency{})
	limit := 1
	first, err := service.ListFavorites(context.Background(), "user-1", ListFavoritesInput{
		Sort:  FavoriteSortTitleAsc,
		Limit: &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("favorite page = %#v", first)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(*first.NextCursor, ".")[0])
	if err != nil || !strings.Contains(string(payload), `"scope":"favorites:user-1:TITLE_ASC","value":{"title":"alpha","trackId":"track-1"}`) {
		t.Fatalf("favorite cursor payload = %s, %v", payload, err)
	}
	if first.Items[0].Track.Title != "Title track-1" || !first.Items[0].Track.IsFavorite || first.Items[0].FavoritedAt != "2026-07-16T01:02:03.456Z" {
		t.Fatalf("favorite item = %#v", first.Items[0])
	}
	if !reflect.DeepEqual(presenter.lastIDs, []string{"track-1"}) {
		t.Fatalf("presenter IDs = %#v", presenter.lastIDs)
	}
	if _, err := service.ListFavorites(context.Background(), "user-1", ListFavoritesInput{
		Sort: FavoriteSortTitleAsc, Limit: &limit, Cursor: *first.NextCursor,
	}); err != nil {
		t.Fatalf("round-trip cursor: %v", err)
	}
	if _, err := service.ListFavorites(context.Background(), "other-user", ListFavoritesInput{
		Sort: FavoriteSortTitleAsc, Limit: &limit, Cursor: *first.NextCursor,
	}); !apperror.IsCode(err, apperror.CodeInvalidCursor) {
		t.Fatalf("cross-user cursor error = %v", err)
	}
}

func TestListFavoritesFavoritedDescCursorUsesTimestampAndDescendingTieBreaker(t *testing.T) {
	favoritedAt := time.Date(2026, 7, 16, 1, 2, 3, 456_000_000, time.UTC)
	calls := 0
	store := &libraryStoreStub{
		listFavorites: func(_ context.Context, query ListFavoritesQuery) ([]FavoriteRecord, error) {
			calls++
			if calls == 1 {
				return []FavoriteRecord{
					{TrackID: "track-2", FavoritedAt: favoritedAt, NormalizedTitle: "two"},
					{TrackID: "track-1", FavoritedAt: favoritedAt, NormalizedTitle: "one"},
				}, nil
			}
			if query.After == nil || query.After.CreatedAt == nil || !query.After.CreatedAt.Equal(favoritedAt) || query.After.TrackID != "track-2" || query.After.Title != nil {
				t.Fatalf("descending cursor = %#v", query.After)
			}
			return []FavoriteRecord{}, nil
		},
	}
	presenter := &trackPresenterStub{summaries: map[string]catalog.TrackSummaryDTO{
		"track-2": testTrack("track-2", true),
	}}
	service := newLibraryTestService(t, store, presenter, &passthroughIdempotency{})
	limit := 1
	first, err := service.ListFavorites(context.Background(), "user-1", ListFavoritesInput{
		Sort: FavoriteSortFavoritedDesc, Limit: &limit,
	})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(*first.NextCursor, ".")[0])
	if err != nil || !strings.Contains(string(payload), `"value":{"createdAt":"2026-07-16T01:02:03.456Z","trackId":"track-2"}`) {
		t.Fatalf("descending cursor payload = %s, %v", payload, err)
	}
	if _, err := service.ListFavorites(context.Background(), "user-1", ListFavoritesInput{
		Sort: FavoriteSortFavoritedDesc, Limit: &limit, Cursor: *first.NextCursor,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAddAndRemoveFavoriteKeepLegacyIdempotencyAndNotFoundRules(t *testing.T) {
	favoritedAt := time.Date(2026, 7, 15, 4, 5, 6, 789_000_000, time.UTC)
	addCalls := 0
	removeCalls := 0
	store := &libraryStoreStub{
		playableTrackExists: func(context.Context, string) (bool, error) { return true, nil },
		trackExists:         func(context.Context, string) (bool, error) { return true, nil },
		addFavorite: func(context.Context, string, string) (time.Time, error) {
			addCalls++
			return favoritedAt, nil
		},
		removeFavorite: func(context.Context, string, string) error {
			removeCalls++
			return nil
		},
	}
	presenter := &trackPresenterStub{summaries: map[string]catalog.TrackSummaryDTO{"track-1": testTrack("track-1", true)}}
	service := newLibraryTestService(t, store, presenter, &passthroughIdempotency{})
	for attempt := 0; attempt < 2; attempt++ {
		item, err := service.AddFavorite(context.Background(), "user-1", "track-1")
		if err != nil || item.FavoritedAt != "2026-07-15T04:05:06.789Z" {
			t.Fatalf("AddFavorite() = %#v, %v", item, err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := service.RemoveFavorite(context.Background(), "user-1", "track-1"); err != nil {
			t.Fatal(err)
		}
	}
	if addCalls != 2 || removeCalls != 2 {
		t.Fatalf("favorite calls = %d/%d", addCalls, removeCalls)
	}
	store.playableTrackExists = func(context.Context, string) (bool, error) { return false, nil }
	if _, err := service.AddFavorite(context.Background(), "user-1", "missing"); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("missing playable track error = %v", err)
	}
	store.trackExists = func(context.Context, string) (bool, error) { return false, nil }
	if err := service.RemoveFavorite(context.Background(), "user-1", "missing"); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("missing track delete error = %v", err)
	}
}

func TestListHistoryPaginatesByStoredRowsAndOmitsUnpresentableTracks(t *testing.T) {
	playedAt := time.Date(2026, 7, 16, 2, 0, 0, 123_000_000, time.UTC)
	updatedAt := playedAt.Add(time.Minute)
	calls := 0
	store := &libraryStoreStub{
		listHistory: func(_ context.Context, query ListHistoryQuery) ([]HistoryRecord, error) {
			calls++
			if query.UserID != "user-1" || query.Limit != 3 {
				t.Fatalf("history query = %#v", query)
			}
			if calls == 2 {
				expected := playedAt.Add(-time.Hour)
				if query.After == nil || !query.After.LastPlayedAt.Equal(expected) || query.After.TrackID != "track-archived" {
					t.Fatalf("history cursor = %#v", query.After)
				}
				return []HistoryRecord{}, nil
			}
			return []HistoryRecord{
				{TrackID: "track-visible", LastPositionMS: 321, PlayCount: 4, LastPlayedAt: playedAt, Completed: true, UpdatedAt: updatedAt},
				{TrackID: "track-archived", LastPlayedAt: playedAt.Add(-time.Hour), UpdatedAt: updatedAt},
				{TrackID: "track-next", LastPlayedAt: playedAt.Add(-2 * time.Hour), UpdatedAt: updatedAt},
			}, nil
		},
	}
	presenter := &trackPresenterStub{summaries: map[string]catalog.TrackSummaryDTO{
		"track-visible": testTrack("track-visible", false),
	}}
	service := newLibraryTestService(t, store, presenter, &passthroughIdempotency{})
	limit := 2
	result, err := service.ListHistory(context.Background(), "user-1", ListHistoryInput{Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.NextCursor == nil {
		t.Fatalf("history page = %#v", result)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(*result.NextCursor, ".")[0])
	if err != nil || !strings.Contains(string(payload), `"scope":"history:user-1","value":{"lastPlayedAt":"2026-07-16T01:00:00.123Z","trackId":"track-archived"}`) {
		t.Fatalf("history cursor payload = %s, %v", payload, err)
	}
	item := result.Items[0]
	if item.LastPositionMS != 321 || item.PlayCount != 4 || !item.Completed || item.LastPlayedAt != "2026-07-16T02:00:00.123Z" || item.UpdatedAt != "2026-07-16T02:01:00.123Z" {
		t.Fatalf("history item = %#v", item)
	}
	if _, err := service.ListHistory(context.Background(), "user-1", ListHistoryInput{Limit: &limit, Cursor: *result.NextCursor}); err != nil {
		t.Fatalf("round-trip history cursor: %v", err)
	}
}

func TestRecordPlaybackPreservesEventOrderingPlayCountAndCompleted(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	store := newPlaybackStateStore()
	presenter := &trackPresenterStub{summaries: map[string]catalog.TrackSummaryDTO{"track-1": testTrack("track-1", false)}}
	idempotency := &passthroughIdempotency{}
	service := newLibraryTestServiceAt(t, store, presenter, idempotency, now)
	sessionOne := "00000000-0000-0000-0000-000000000001"
	sessionTwo := "00000000-0000-0000-0000-000000000002"
	record := func(key, session string, position int64, at time.Time, event PlaybackEvent) HistoryItemDTO {
		t.Helper()
		result, err := service.RecordPlayback(context.Background(), "user-1", "track-1", key, RecordPlaybackInput{
			PlaybackSessionID: session,
			PositionMS:        position,
			OccurredAt:        at.Format(time.RFC3339Nano),
			Event:             event,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.Body
	}
	started := record("started-1", sessionOne, 100, now.Add(-5*time.Minute), PlaybackEventStarted)
	if started.PlayCount != 1 || started.Completed {
		t.Fatalf("initial STARTED = %#v", started)
	}
	progress := record("progress-1", sessionOne, 40, now.Add(-4*time.Minute), PlaybackEventProgress)
	if progress.PlayCount != 1 || progress.LastPositionMS != 40 {
		t.Fatalf("backward seek PROGRESS = %#v", progress)
	}
	completed := record("completed-1", sessionOne, 1_000, now.Add(-3*time.Minute), PlaybackEventCompleted)
	if !completed.Completed || completed.PlayCount != 1 {
		t.Fatalf("COMPLETED = %#v", completed)
	}
	stale := record("stale-1", sessionTwo, 1, now.Add(-4*time.Minute), PlaybackEventPaused)
	if !stale.Completed || stale.LastPositionMS != 1_000 || stale.LastPlayedAt != completed.LastPlayedAt {
		t.Fatalf("stale event overwrote history = %#v", stale)
	}
	sameSession := record("started-same", sessionOne, 0, now.Add(-2*time.Minute), PlaybackEventStarted)
	if sameSession.PlayCount != 1 || sameSession.Completed {
		t.Fatalf("same-session STARTED = %#v", sameSession)
	}
	newSession := record("started-new", sessionTwo, 0, now.Add(-time.Minute), PlaybackEventStarted)
	if newSession.PlayCount != 2 {
		t.Fatalf("new-session STARTED = %#v", newSession)
	}
	if idempotency.lastInput.Scope != "library.history:track-1" || idempotency.lastInput.ActorID != "user-1" || idempotency.lastInput.Key != "started-new" {
		t.Fatalf("idempotency input = %#v", idempotency.lastInput)
	}
	if _, err := service.RecordPlayback(context.Background(), "user-1", "track-1", "future-key", RecordPlaybackInput{
		PlaybackSessionID: sessionOne,
		OccurredAt:        now.Add(5*time.Minute + time.Millisecond).Format(time.RFC3339Nano),
		Event:             PlaybackEventProgress,
	}); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("future event error = %v", err)
	}
}

func TestRecordPlaybackReturnsIdempotentReplayWithoutRepeatingSideEffect(t *testing.T) {
	store := newPlaybackStateStore()
	presenter := &trackPresenterStub{summaries: map[string]catalog.TrackSummaryDTO{"track-1": testTrack("track-1", false)}}
	idempotency := &replayIdempotency{body: HistoryItemDTO{Track: testTrack("track-1", false), PlayCount: 7}}
	service := newLibraryTestService(t, store, presenter, idempotency)
	result, err := service.RecordPlayback(context.Background(), "user-1", "track-1", "replay-key", RecordPlaybackInput{})
	if err != nil || !result.Replayed || result.Body.PlayCount != 7 {
		t.Fatalf("replay = %#v, %v", result, err)
	}
	if store.upsertCalls != 0 || presenter.calls != 0 {
		t.Fatalf("replay repeated operation: upsert/presenter = %d/%d", store.upsertCalls, presenter.calls)
	}
}

func newLibraryTestService(t *testing.T, store Store, tracks TrackPresenter, idempotency Idempotency) *Service {
	return newLibraryTestServiceAt(t, store, tracks, idempotency, time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC))
}

func newLibraryTestServiceAt(
	t *testing.T,
	store Store,
	tracks TrackPresenter,
	idempotency Idempotency,
	now time.Time,
) *Service {
	t.Helper()
	service, err := NewService(ServiceDependencies{
		Repository:  store,
		Cursors:     pagination.NewCursorCodec("library-test-cursor-secret"),
		Tracks:      tracks,
		Idempotency: idempotency,
		Clock:       fixedLibraryClock{now: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type fixedLibraryClock struct{ now time.Time }

func (clock fixedLibraryClock) Now() time.Time { return clock.now }

type trackPresenterStub struct {
	summaries map[string]catalog.TrackSummaryDTO
	err       error
	calls     int
	lastIDs   []string
}

func (presenter *trackPresenterStub) TrackSummaries(
	_ context.Context,
	_ string,
	trackIDs []string,
) ([]catalog.TrackSummaryDTO, error) {
	presenter.calls++
	presenter.lastIDs = append([]string(nil), trackIDs...)
	if presenter.err != nil {
		return nil, presenter.err
	}
	result := make([]catalog.TrackSummaryDTO, 0, len(trackIDs))
	for _, id := range trackIDs {
		if item, found := presenter.summaries[id]; found {
			result = append(result, item)
		}
	}
	return result, nil
}

type passthroughIdempotency struct {
	lastInput IdempotencyInput
}

func (adapter *passthroughIdempotency) ExecutePlayback(
	_ context.Context,
	input IdempotencyInput,
	operation func() (HistoryItemDTO, error),
) (MutationResult[HistoryItemDTO], error) {
	adapter.lastInput = input
	body, err := operation()
	return MutationResult[HistoryItemDTO]{Body: body}, err
}

type replayIdempotency struct{ body HistoryItemDTO }

func (adapter *replayIdempotency) ExecutePlayback(
	context.Context,
	IdempotencyInput,
	func() (HistoryItemDTO, error),
) (MutationResult[HistoryItemDTO], error) {
	return MutationResult[HistoryItemDTO]{Body: adapter.body, Replayed: true}, nil
}

type libraryStoreStub struct {
	listFavorites       func(context.Context, ListFavoritesQuery) ([]FavoriteRecord, error)
	playableTrackExists func(context.Context, string) (bool, error)
	trackExists         func(context.Context, string) (bool, error)
	addFavorite         func(context.Context, string, string) (time.Time, error)
	removeFavorite      func(context.Context, string, string) error
	listHistory         func(context.Context, ListHistoryQuery) ([]HistoryRecord, error)
	upsertPlayback      func(context.Context, PlaybackWrite) (HistoryRecord, error)
}

func (store *libraryStoreStub) ListFavorites(ctx context.Context, query ListFavoritesQuery) ([]FavoriteRecord, error) {
	if store.listFavorites == nil {
		return nil, errors.New("unexpected ListFavorites call")
	}
	return store.listFavorites(ctx, query)
}

func (store *libraryStoreStub) PlayableTrackExists(ctx context.Context, trackID string) (bool, error) {
	if store.playableTrackExists == nil {
		return false, errors.New("unexpected PlayableTrackExists call")
	}
	return store.playableTrackExists(ctx, trackID)
}

func (store *libraryStoreStub) TrackExists(ctx context.Context, trackID string) (bool, error) {
	if store.trackExists == nil {
		return false, errors.New("unexpected TrackExists call")
	}
	return store.trackExists(ctx, trackID)
}

func (store *libraryStoreStub) AddFavorite(ctx context.Context, userID, trackID string) (time.Time, error) {
	if store.addFavorite == nil {
		return time.Time{}, errors.New("unexpected AddFavorite call")
	}
	return store.addFavorite(ctx, userID, trackID)
}

func (store *libraryStoreStub) RemoveFavorite(ctx context.Context, userID, trackID string) error {
	if store.removeFavorite == nil {
		return errors.New("unexpected RemoveFavorite call")
	}
	return store.removeFavorite(ctx, userID, trackID)
}

func (store *libraryStoreStub) ListHistory(ctx context.Context, query ListHistoryQuery) ([]HistoryRecord, error) {
	if store.listHistory == nil {
		return nil, errors.New("unexpected ListHistory call")
	}
	return store.listHistory(ctx, query)
}

func (store *libraryStoreStub) UpsertPlayback(ctx context.Context, input PlaybackWrite) (HistoryRecord, error) {
	if store.upsertPlayback == nil {
		return HistoryRecord{}, errors.New("unexpected UpsertPlayback call")
	}
	return store.upsertPlayback(ctx, input)
}

type playbackStateStore struct {
	libraryStoreStub
	history     *HistoryRecord
	upsertCalls int
}

func newPlaybackStateStore() *playbackStateStore {
	store := &playbackStateStore{}
	store.upsertPlayback = store.writePlayback
	return store
}

func (store *playbackStateStore) writePlayback(_ context.Context, input PlaybackWrite) (HistoryRecord, error) {
	store.upsertCalls++
	if store.history == nil {
		count := int64(0)
		if input.Event == PlaybackEventStarted {
			count = 1
		}
		store.history = &HistoryRecord{
			TrackID: input.TrackID, LastPositionMS: input.PositionMS, PlayCount: count,
			LastPlayedAt: input.OccurredAt, Completed: input.Event == PlaybackEventCompleted,
			LastPlaybackSessionID: input.PlaybackSessionID, UpdatedAt: input.UpdatedAt,
		}
		return *store.history, nil
	}
	if store.history.LastPlayedAt.After(input.OccurredAt) {
		return *store.history, nil
	}
	if input.Event == PlaybackEventStarted && store.history.LastPlaybackSessionID != input.PlaybackSessionID {
		store.history.PlayCount++
	}
	store.history.LastPositionMS = input.PositionMS
	store.history.LastPlayedAt = input.OccurredAt
	store.history.Completed = input.Event == PlaybackEventCompleted
	store.history.LastPlaybackSessionID = input.PlaybackSessionID
	store.history.UpdatedAt = input.UpdatedAt
	return *store.history, nil
}

func testTrack(id string, favorite bool) catalog.TrackSummaryDTO {
	return catalog.TrackSummaryDTO{
		ID: id, Title: "Title " + id, Artists: []catalog.ArtistReferenceDTO{},
		DurationMS: 123, DiscNumber: 1, IsFavorite: favorite,
		PublishedAt: "2026-07-01T00:00:00.000Z",
	}
}
