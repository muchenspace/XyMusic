package adminmetadata

import (
	"context"
	"time"

	"xymusic/server/internal/shared/idempotencyport"
)

type Store interface {
	EnsureMetadata(context.Context, []string) error
	FindMetadata(context.Context, string) (MetadataRecord, error)
	UpdateMetadata(context.Context, string, string, MetadataMutationInput) (MetadataRecord, error)
	BatchUpdateMetadata(context.Context, string, BatchMetadataMutationInput) ([]BatchUpdateRecord, error)
	EnqueueWriteback(context.Context, string, string, VersionReasonInput) (WritebackJob, error)
	ListWritebacks(context.Context, WritebackListQuery) ([]WritebackJob, int, error)
	FindWriteback(context.Context, string) (WritebackJob, error)
	RetryWriteback(context.Context, string, string, VersionReasonInput) (WritebackJob, error)
	CancelWriteback(context.Context, string, VersionInput) (WritebackJob, error)
}

type WorkerStore interface {
	FindWriteback(context.Context, string) (WritebackJob, error)
	ClaimWriteback(context.Context, string, time.Duration) (*WritebackJob, error)
	LoadWritebackContext(context.Context, string, string, string) (WritebackContext, error)
	RenewWritebackLease(context.Context, string, string, string, time.Duration) error
	WritebackCancellationRequested(context.Context, string, string, string) (bool, error)
	MarkWritebackPrepared(context.Context, string, string, string, string) error
	MarkWritebackFileReplaced(context.Context, string, string, string, string) error
	CompleteTransientRollback(context.Context, string, string, string) error
	ReleaseTransientRollback(context.Context, string, string, string, error, time.Duration) error
	CommitWriteback(context.Context, WritebackCommit) error
	CompleteCommittedRollback(context.Context, string, string, string) error
	ReleaseCommittedRollback(context.Context, string, string, string, error, time.Duration) error
	FailWriteback(context.Context, string, string, string, error, time.Time) error
}

type WritebackCommit struct {
	JobID          string
	WorkerID       string
	AttemptID      string
	OriginalSHA256 string
	OutputSHA256   string
	OutputSize     int64
	OutputModified time.Time
	Metadata       MetadataSnapshot
}

type ArtworkDownloader interface {
	DownloadToFile(context.Context, string, string, int64) error
}

type Clock interface {
	Now() time.Time
}

type Logger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
	Error(string, map[string]any)
}

type IdempotencyInput = idempotencyport.Input

type IdempotencyResponse = idempotencyport.Response

type IdempotencyResult = idempotencyport.Result

type Idempotency interface {
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
