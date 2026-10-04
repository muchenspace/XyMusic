package adminmanagement

import (
	"context"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/idempotencyport"
)

type Store interface {
	Dashboard(context.Context) (DashboardCounts, error)
	ListUsers(context.Context, ListUsersQuery) ([]UserRecord, int, error)
	FindUser(context.Context, string, SessionQuery) (UserRecord, []SessionRecord, int, error)
	CreateUser(context.Context, CreateUserParams) (string, error)
	UpdateUser(context.Context, UpdateUserParams) error
	ResetPassword(context.Context, string, int, string) error
	RevokeSession(context.Context, string, string) error
	UpdateStatus(context.Context, string, string, int, UserStatus) error
}

type ArtworkPresenter interface {
	Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error)
}

type IdempotencyInput = idempotencyport.Input

type IdempotencyResponse = idempotencyport.Response

type IdempotencyResult = idempotencyport.Result

type Idempotency interface {
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
