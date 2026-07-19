package library

import (
	"context"
	"time"

	"xymusic/server/internal/modules/catalog"
)

type Store interface {
	ListFavorites(context.Context, ListFavoritesQuery) ([]FavoriteRecord, error)
	PlayableTrackExists(context.Context, string) (bool, error)
	TrackExists(context.Context, string) (bool, error)
	AddFavorite(context.Context, string, string) (time.Time, error)
	RemoveFavorite(context.Context, string, string) error
	ListHistory(context.Context, ListHistoryQuery) ([]HistoryRecord, error)
	UpsertPlayback(context.Context, PlaybackWrite) (HistoryRecord, error)
}

// TrackPresenter is the narrow catalog projection required by the library.
// It must return only playable/published tracks and preserve the requested ID
// order, matching the legacy catalog trackSummaries contract.
type TrackPresenter interface {
	TrackSummaries(context.Context, string, []string) ([]catalog.TrackSummaryDTO, error)
}

type IdempotencyInput struct {
	ActorID string
	Scope   string
	Key     string
	Payload RecordPlaybackInput
}

type Idempotency interface {
	ExecutePlayback(
		context.Context,
		IdempotencyInput,
		func() (HistoryItemDTO, error),
	) (MutationResult[HistoryItemDTO], error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
