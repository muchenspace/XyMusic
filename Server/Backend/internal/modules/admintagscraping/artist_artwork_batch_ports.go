package admintagscraping

import (
	"context"
	"time"
)

type ArtistArtworkBatchStore interface {
	CreateArtistArtworkBatch(context.Context, string, CreateArtistArtworkBatchInput, int) (string, int, int, error)
	ArtistArtworkBatch(context.Context, string, *time.Time) (ArtistArtworkBatchJobRecord, []ArtistArtworkBatchItemRecord, error)
	RequestArtistArtworkBatchCancel(context.Context, string) error
	RetryArtistArtworkBatch(context.Context, string) error
	RecoverExpiredArtistArtworkBatchItems(context.Context, time.Time) error
	ClaimArtistArtworkBatchItem(context.Context, string, time.Time, time.Duration) (ArtistArtworkBatchClaimResult, error)
	RenewArtistArtworkBatchItemLease(context.Context, string, string, string, string, time.Time) (BatchLeaseControl, error)
	ArtistArtworkBatchCancelRequested(context.Context, string) (bool, error)
	RetryArtistArtworkBatchItem(context.Context, string, string, string, string, *ArtistCandidate, string, time.Time, time.Time) (BatchLeaseControl, error)
	CompleteArtistArtworkBatchItem(context.Context, string, string, string, string, ItemStatus, *ArtistCandidate, string, time.Time) (bool, error)
	ReleaseArtistArtworkBatchItem(context.Context, string, string, string, time.Time) error
	FinishArtistArtworkBatch(context.Context, string, time.Time) (bool, error)
}

// ArtistArtworkBatchClaimStore is an optional store fast path. Implementations
// can claim the service's worker window in one transaction while older stores
// keep the single-item contract above.
type ArtistArtworkBatchClaimStore interface {
	ClaimArtistArtworkBatchItems(context.Context, string, time.Time, time.Duration, int) (ArtistArtworkBatchClaimResult, error)
}

// ArtistArtworkBatchCompleteStore is an optional completion fast path. Stores
// that do not implement it continue to use the item-level completion method.
type ArtistArtworkBatchCompleteStore interface {
	CompleteArtistArtworkBatchItems(context.Context, string, string, []ArtistArtworkBatchItemCompletion, time.Time) ([]string, error)
}

type ArtistArtworkBatchProcessor interface {
	SearchArtists(context.Context, ArtistSearchInput) ([]ArtistCandidate, error)
	ApplyArtistArtwork(context.Context, string, string, string, ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error)
}
