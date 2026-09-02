package admintagscraping

import (
	"context"
	"time"
)

type MusicPlatform interface {
	Search(context.Context, Source, string) ([]Candidate, error)
	SearchArtists(context.Context, Source, string) ([]ArtistCandidate, error)
	Lyric(context.Context, Source, Candidate, bool) (LyricResult, error)
	DownloadArtwork(context.Context, string) (DownloadedArtwork, error)
}

type ArtworkApplier interface {
	ApplyAlbumArtwork(context.Context, string, string, DownloadedArtwork) error
	ApplyArtistArtwork(context.Context, string, string, int, bool, DownloadedArtwork) error
}

type Logger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
	Error(string, map[string]any)
}

// BatchClaimStore is an optional store fast path. Implementations can claim
// the service's worker window in one transaction while older stores keep the
// single-item contract below.
type BatchClaimStore interface {
	ClaimBatchItems(context.Context, string, time.Time, time.Duration, int) (BatchClaimResult, error)
}

// BatchCompleteStore is an optional fast path for completing one claimed
// worker window. Implementations must preserve the same per-item fencing as
// CompleteBatchItem and return only item IDs accepted by that fence.
type BatchCompleteStore interface {
	CompleteBatchItems(context.Context, string, string, []BatchItemCompletion, time.Time) ([]string, error)
}

type Store interface {
	Metadata(context.Context, string) (TrackMetadata, error)
	UpdateMetadata(context.Context, string, string, int, MetadataPatch, string) (TrackMetadata, error)
	TrackAlbumID(context.Context, string) (*string, error)
	EnqueueWriteback(context.Context, string, string, int, string) (WritebackJob, error)

	ValidateBatchWriteback(context.Context, []BatchItemInput) error
	CreateBatch(context.Context, string, CreateBatchInput) (string, error)
	Batch(context.Context, string, *time.Time) (BatchJobRecord, []BatchItemRecord, error)
	RequestBatchCancel(context.Context, string) error
	RetryBatch(context.Context, string) error
	RecoverExpiredBatchItems(context.Context, time.Time) error
	ClaimBatchItem(context.Context, string, time.Time, time.Duration) (ClaimResult, error)
	RenewBatchItemLease(context.Context, string, string, string, string, time.Time) (BatchLeaseControl, error)
	BatchCancelRequested(context.Context, string) (bool, error)
	// RetryBatchItem atomically releases the current lease and makes the item
	// claimable after nextAttemptAt. It preserves the consumed attempt count
	// so retries remain bounded by the durable item limit.
	RetryBatchItem(context.Context, string, string, string, string, *Candidate, string, time.Time, time.Time) (BatchLeaseControl, error)
	CompleteBatchItem(context.Context, string, string, string, string, ItemStatus, *Candidate, string, time.Time) (bool, error)
	ReleaseBatchItem(context.Context, string, string, string, time.Time) error
	FinishBatch(context.Context, string, time.Time) (bool, error)
}

type Idempotency interface {
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
