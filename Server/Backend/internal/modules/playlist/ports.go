package playlist

import (
	"context"
	"encoding/json"
	"time"

	"xymusic/server/internal/modules/catalog"
)

type Store interface {
	ListOwned(ctx context.Context, query ListOwnedQuery) ([]PlaylistRecord, error)
	CreatePlaylist(ctx context.Context, params CreatePlaylistParams) (PlaylistRecord, error)
	FindPlaylist(ctx context.Context, playlistID string) (PlaylistRecord, error)
	ListEntries(ctx context.Context, query ListEntriesQuery) ([]EntryRecord, error)
	UpdatePlaylist(ctx context.Context, params UpdatePlaylistParams) (PlaylistRecord, error)
	DeletePlaylist(ctx context.Context, ownerID, playlistID string, expectedVersion int) error
	ReadyTrackExists(ctx context.Context, trackID string) (bool, error)
	AddTrack(ctx context.Context, params AddTrackParams) (AddTrackMutation, error)
	RemoveTrack(ctx context.Context, params RemoveTrackParams) (VersionMutation, error)
	Reorder(ctx context.Context, params ReorderParams) (VersionMutation, error)
}

type CatalogPresenter interface {
	TrackSummaries(ctx context.Context, userID string, trackIDs []string) ([]catalog.TrackSummaryDTO, error)
}

type UserPresenter interface {
	UserSummary(ctx context.Context, userID string) (UserSummaryDTO, error)
	UserSummaries(ctx context.Context, userIDs []string) (map[string]UserSummaryDTO, error)
	Artworks(ctx context.Context, assetIDs []string) (map[string]ArtworkDTO, error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type Authenticator interface {
	Authenticate(ctx context.Context, authorization string) (userID string, err error)
}

type AuthenticateFunc func(ctx context.Context, authorization string) (userID string, err error)

func (function AuthenticateFunc) Authenticate(ctx context.Context, authorization string) (string, error) {
	return function(ctx, authorization)
}

type IdempotencyInput struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
}

type IdempotencyResponse struct {
	Status int
	Body   json.RawMessage
}

type IdempotencyResult struct {
	Status   int
	Body     json.RawMessage
	Replayed bool
}

type Idempotency interface {
	Execute(
		ctx context.Context,
		input IdempotencyInput,
		operation func() (IdempotencyResponse, error),
	) (IdempotencyResult, error)
}
