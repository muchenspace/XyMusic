package profile

import (
	"context"
	"time"

	"xymusic/server/internal/modules/identity"
)

type Authenticator interface {
	Authenticate(context.Context, string) (identity.AuthenticatedActor, error)
}

type CurrentUserReader interface {
	CurrentUser(context.Context, string) (identity.CurrentUserDTO, error)
}

type Store interface {
	UpdateProfile(context.Context, string, int, ProfileChanges, time.Time) error
	CreateAvatarUpload(context.Context, CreateUploadParams) (AvatarUpload, error)
	MarkAvatarUploadFailed(context.Context, string, string) error
	ClaimAvatarCompletion(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error)
	AvatarCompletionStatus(context.Context, string, string) (string, error)
	FinalizeAvatarCompletion(context.Context, FinalizeAvatarParams) error
	FailAvatarCompletion(context.Context, string, string, bool, []string, string, time.Time) error
}

type AvatarObjectStorage interface {
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

type AvatarInspector interface {
	Inspect(context.Context, AvatarUpload, string) (InspectedAvatar, error)
}

type IdempotencyInput struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
}

type Idempotency interface {
	ExecuteCurrentUser(
		context.Context,
		IdempotencyInput,
		int,
		func() (identity.CurrentUserDTO, error),
	) (MutationResult[identity.CurrentUserDTO], error)
	ExecuteAvatarUpload(
		context.Context,
		IdempotencyInput,
		int,
		func() (AvatarUploadDTO, error),
	) (MutationResult[AvatarUploadDTO], error)
}

type Clock interface {
	Now() time.Time
}

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}
