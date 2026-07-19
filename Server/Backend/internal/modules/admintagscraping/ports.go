package admintagscraping

import (
	"context"
	"time"
)

type MusicPlatform interface {
	Search(context.Context, Source, string) ([]Candidate, error)
	SearchArtists(context.Context, Source, string) ([]ArtistCandidate, error)
	Lyric(context.Context, Source, string) (string, error)
	AcoustID(context.Context, float64, string) ([]Candidate, error)
	DownloadArtwork(context.Context, string) (DownloadedArtwork, error)
}

type Fingerprinter interface {
	Fingerprint(context.Context, string, int, *int) (FingerprintResult, error)
}

type ArtworkApplier interface {
	ApplyAlbumArtwork(context.Context, string, string, string, DownloadedArtwork) error
	ApplyArtistArtwork(context.Context, string, string, string, int, bool, DownloadedArtwork) error
}

type Logger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
	Error(string, map[string]any)
}

type Store interface {
	FingerprintSource(context.Context, string) (FingerprintSource, error)
	Metadata(context.Context, string) (TrackMetadata, error)
	UpdateMetadata(context.Context, string, string, string, int, MetadataPatch, string) (TrackMetadata, error)
	TrackAlbumID(context.Context, string) (*string, error)
	EnqueueWriteback(context.Context, string, string, string, int, string) (WritebackJob, error)

	ValidateBatchWriteback(context.Context, []BatchItemInput) error
	CreateBatch(context.Context, string, CreateBatchInput) (string, error)
	Batch(context.Context, string, *time.Time) (BatchJobRecord, []BatchItemRecord, error)
	RequestBatchCancel(context.Context, string) error
	RetryBatch(context.Context, string) error
	RecoverExpiredBatchItems(context.Context, time.Time) error
	ClaimBatchItem(context.Context, string, time.Time, time.Duration) (ClaimResult, error)
	RenewBatchItemLease(context.Context, string, string, string, string, time.Time) (BatchLeaseControl, error)
	BatchCancelRequested(context.Context, string) (bool, error)
	CompleteBatchItem(context.Context, string, string, string, string, ItemStatus, *Candidate, string, time.Time) (bool, error)
	ReleaseBatchItem(context.Context, string, string, string, time.Time) error
	FinishBatch(context.Context, string, time.Time) (bool, error)
}

type Idempotency interface {
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
