package adminauth

import (
	"context"

	"xymusic/server/internal/modules/identity"
)

// Identity is the administrator authentication contract consumed by the
// routes layer. It is declared here so the transport file stays focused on the
// HTTP contract.
type Identity interface {
	Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error)
	Refresh(context.Context, string, string) (identity.RefreshResult, error)
	Authenticate(context.Context, string) (identity.AuthenticatedActor, error)
	Logout(context.Context, identity.AuthenticatedActor) error
	GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error)
}
