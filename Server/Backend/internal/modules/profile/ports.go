package profile

import (
	"context"
	"io"
	"time"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/idempotencyport"
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
	FindAvatarUpload(context.Context, string, string) (AvatarUpload, error)
	ClaimAvatarCompletion(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error)
	AvatarCompletionStatus(context.Context, string, string) (string, error)
	FinalizeAvatarCompletion(context.Context, FinalizeAvatarParams) error
	FailAvatarCompletion(context.Context, string, string, bool, string, time.Time) error
}

type AvatarInspector interface {
	Inspect(context.Context, AvatarUpload) (InspectedAvatar, error)
}

type AssetStore interface {
	ResolveAssetPath(string) (string, error)
	WriteUploadStream(context.Context, io.Reader, int64, string, string) (int64, string, error)
	CommitUpload(context.Context, string, string) (string, error)
	AssetDirectory() string
}

type IdempotencyInput = idempotencyport.Input

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
