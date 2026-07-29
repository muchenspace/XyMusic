package adminmetadata

import (
	"context"
	"encoding/json"
	"time"
)

type Store interface {
	EnsureMetadata(context.Context, []string) error
	FindMetadata(context.Context, string) (MetadataRecord, error)
	UpdateMetadata(context.Context, string, string, string, MetadataMutationInput) (MetadataRecord, error)
	BatchUpdateMetadata(context.Context, string, string, BatchMetadataMutationInput) ([]BatchUpdateRecord, error)
	ListRevisions(context.Context, string, int, int) ([]RevisionRecord, int, error)
	FindRevision(context.Context, string, string) (RevisionRecord, error)
	RestoreMetadata(context.Context, string, string, string, string, VersionReasonInput) (MetadataRecord, error)
	EnqueueWriteback(context.Context, string, string, string, VersionReasonInput) (WritebackJob, error)
	ListWritebacks(context.Context, WritebackListQuery) ([]WritebackJob, int, error)
	FindWriteback(context.Context, string) (WritebackJob, error)
	RetryWriteback(context.Context, string, string, string, VersionReasonInput) (WritebackJob, error)
	CancelWriteback(context.Context, string, string, string, VersionReasonInput) (WritebackJob, error)
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

type ProcessResult struct {
	Stdout          string
	Stderr          string
	ExitCode        int
	TimedOut        bool
	StdoutTruncated bool
}

type ProcessRunner interface {
	Run(context.Context, string, []string, time.Duration) (ProcessResult, error)
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
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
