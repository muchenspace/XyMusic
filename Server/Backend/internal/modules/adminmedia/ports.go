package adminmedia

import (
	"context"
	"io"
	"time"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/shared/idempotencyport"
)

type Identity = adminauth.Identity

// Row and CommandTag mirror the subset of the pgx result contracts that
// completion fences depend on, keeping pgx out of this module's ports.
type Row interface {
	Scan(dest ...any) error
}

type CommandTag interface {
	RowsAffected() int64
}

// Tx is the narrow transaction contract passed to CompletionFence.Lock.
// Persistence adapters wrap their concrete transaction before calling fences.
type Tx interface {
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
}

type CompletionFence interface {
	Lock(context.Context, Tx) error
}

type AssetStore interface {
	ResolveAssetPath(string) (string, error)
	WriteUploadStream(context.Context, io.Reader, int64, string, string) (int64, string, error)
	CommitUpload(context.Context, string, string) (string, error)
	DeleteAsset(string) error
	AssetDirectory() string
}

type Store interface {
	CreateUpload(context.Context, UploadReservation) error
	FindUpload(context.Context, string) (UploadReservation, error)
	ClaimUploadCompletion(context.Context, string, string, time.Time, time.Duration) (CompletionClaim, error)
	UploadCompletionStatus(context.Context, string) (string, error)
	FinalizeUpload(context.Context, FinalizeUploadParams) error
	FailUploadCompletion(context.Context, string, string, bool, string, time.Time) error
	AbandonUpload(context.Context, string, string) error
}

type MediaInspector interface {
	Inspect(context.Context, UploadReservation) (InspectedMedia, error)
}

type IdempotencyInput = idempotencyport.Input

type Idempotency interface {
	ExecuteReservation(
		context.Context,
		IdempotencyInput,
		func() (UploadReservationDTO, error),
	) (UploadReservationDTO, bool, error)
	ExecuteCompletion(
		context.Context,
		IdempotencyInput,
		func() (UploadCompletionDTO, error),
	) (UploadCompletionDTO, bool, error)
}

type Clock interface {
	Now() time.Time
}

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}
