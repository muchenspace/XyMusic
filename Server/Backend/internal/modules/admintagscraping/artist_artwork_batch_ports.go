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

type ArtistArtworkBatchProcessor interface {
	SearchArtists(context.Context, ArtistSearchInput) ([]ArtistCandidate, error)
	ApplyArtistArtwork(context.Context, string, string, string, ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error)
}
