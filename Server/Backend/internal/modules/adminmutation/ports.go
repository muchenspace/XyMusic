package adminmutation

import (
	"context"
	"encoding/json"
	"time"

	"xymusic/server/internal/modules/catalog"
)

type Store interface {
	ArtistsExist(context.Context, []string) (bool, error)
	AlbumExists(context.Context, string) (bool, error)
	CreateArtist(context.Context, CreateArtistParams) (string, error)
	UpdateArtist(context.Context, UpdateArtistParams) error
	CreateAlbum(context.Context, CreateAlbumParams) (string, error)
	UpdateAlbum(context.Context, UpdateAlbumParams) error
	MergeAlbums(context.Context, string, string, MergeAlbumsInput) (MergeResultDTO, error)
	CreateTrack(context.Context, CreateTrackParams) (string, error)
	UpdateTrack(context.Context, UpdateTrackParams) error
	PublishTrack(context.Context, string, int) error
	ArchiveTrack(context.Context, string, int) error
	RestoreTrack(context.Context, string, int) error
	RestoreTracksBatch(context.Context, string, string, []BatchTrackItemInput) ([]BatchRestoreItemRecord, error)
	DeleteTrackPermanently(context.Context, string, int, string) (DeleteResult, error)
	CreatePermanentDeleteBatch(context.Context, string, string, []BatchTrackItemInput) (PermanentDeleteBatchRecord, []PermanentDeleteBatchItemRecord, error)
	FindPermanentDeleteBatch(context.Context, string) (PermanentDeleteBatchRecord, []PermanentDeleteBatchItemRecord, error)
	UpsertLyrics(context.Context, string, LyricsInput) (StoredLyric, error)
	UpdateUserStatus(context.Context, string, string, int, UserStatus) error
	FindArtist(context.Context, string) (ArtistRecord, error)
	FindAlbum(context.Context, string) (AlbumRecord, error)
	FindTrack(context.Context, string) (TrackRecord, error)
	FindUser(context.Context, string) (UserRecord, error)
	WriteAudit(context.Context, string, string, string, string, string, map[string]any) error
}

type PermanentDeleteBatchWorkerStore interface {
	InitializePermanentDeleteBatches(context.Context, time.Time) error
	ClaimPermanentDeleteBatchItem(context.Context, string, time.Time, time.Duration) (*ClaimedPermanentDeleteItem, error)
	RenewPermanentDeleteBatchItem(context.Context, string, string, string, time.Time, time.Time) (bool, error)
	RetryPermanentDeleteBatchItem(context.Context, string, string, string, string, string, time.Time, time.Time) error
	ReleasePermanentDeleteBatchItem(context.Context, string, string, string, time.Time) error
	CompletePermanentDeleteBatchItemSuccess(context.Context, ClaimedPermanentDeleteItem, string, DeleteResult, *string, time.Time) error
	CompletePermanentDeleteBatchItemFailure(context.Context, ClaimedPermanentDeleteItem, string, string, string, time.Time) error
}

type PermanentTrackDeleter interface {
	DeleteTrackPermanently(context.Context, string, int, string) (DeleteResult, error)
}

type ArtworkPresenter interface {
	Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error)
}

type IdempotencyInput struct {
	ActorID, Scope, Key string
	Payload             any
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
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
