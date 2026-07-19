package adminmanagement

import (
	"context"
	"encoding/json"

	"xymusic/server/internal/modules/catalog"
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
	WriteAudit(context.Context, AuditWrite) error
}

type ArtworkPresenter interface {
	Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error)
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
