package adminjobs

import (
	"context"
	"encoding/json"
)

type Store interface {
	ListJobs(context.Context, ListQuery) ([]JobRecord, int, error)
	FindJob(context.Context, string) (JobRecord, error)
	FindMetadataVersion(context.Context, string) (int, bool, error)
	RetryMediaOrScan(context.Context, string, string, string, *string) error
	CancelMediaOrScan(context.Context, string, string, string, *string) error
	EventState(context.Context) (EventRecord, error)
}

type MetadataMutator interface {
	Retry(context.Context, string, string, string, MetadataMutationInput) error
	Cancel(context.Context, string, string, string, MetadataMutationInput) error
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
