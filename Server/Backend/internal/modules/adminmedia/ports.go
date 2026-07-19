package adminmedia

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

// CompletionFence serializes a media asset's visible commit with its owning
// operation. Implementations must lock and validate ownership in the supplied
// transaction before FinalizeCompletion mutates any media or target rows.
type CompletionFence interface {
	Lock(context.Context, pgx.Tx) error
}

type Store interface {
	CreateUpload(context.Context, CreateUploadParams) (MediaUpload, error)
	MarkUploadFailed(context.Context, string, string) error
	AbandonUpload(context.Context, string, string, time.Time) error
	FindUploadForContent(context.Context, string, string) (MediaUpload, error)
	ClaimCompletion(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error)
	CompletionStatus(context.Context, string, string) (MediaUpload, error)
	FinalizeCompletion(context.Context, FinalizeCompletionParams) (CompletedUpload, error)
	FailCompletion(context.Context, string, string, bool, []string, string, time.Time) error
	FindJob(context.Context, string) (MediaJob, error)
	RetryJob(context.Context, RetryJobParams) (MediaJob, error)
	WriteAudit(context.Context, AuditWrite) error
}

type ObjectStorage interface {
	CreateUploadURL(context.Context, UploadURLRequest) (string, error)
	DownloadToFile(context.Context, string, string, int64) (StoredObject, error)
	UploadFile(context.Context, string, string, string, string) (int64, error)
}

type UploadURLRequest struct {
	ObjectKey      string
	ContentType    string
	ContentLength  int64
	ChecksumSHA256 string
	Expires        time.Duration
}

type UploadInspector interface {
	Inspect(context.Context, MediaUpload, string) (InspectedUpload, error)
}

type Clock interface {
	Now() time.Time
}

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
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

// ReadCloser is kept as a named transport-neutral boundary for the streaming
// upload endpoint and allows service tests to verify close behavior.
type ReadCloser interface {
	io.Reader
	io.Closer
}
