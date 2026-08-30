package adminmedia

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/modules/identity"
)

type Authenticator interface {
	Authenticate(context.Context, string) (identity.AuthenticatedActor, error)
}

type CompletionFence interface {
	Lock(context.Context, pgx.Tx) error
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

type IdempotencyInput struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
}

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
