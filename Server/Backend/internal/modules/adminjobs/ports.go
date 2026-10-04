package adminjobs

import (
	"context"

	"xymusic/server/internal/shared/idempotencyport"
)

type Store interface {
	ListJobs(context.Context, ListQuery) ([]JobRecord, int, error)
	FindJob(context.Context, string) (JobRecord, error)
	FindMetadataVersion(context.Context, string) (int, bool, error)
	RetryMediaOrScan(context.Context, string) error
	CancelMediaOrScan(context.Context, string) error
	EventState(context.Context) (EventRecord, error)
}

type MetadataMutator interface {
	Retry(context.Context, string, string, MetadataMutationInput) error
	Cancel(context.Context, string, MetadataCancelInput) error
}

type IdempotencyInput = idempotencyport.Input

type IdempotencyResponse = idempotencyport.Response

type IdempotencyResult = idempotencyport.Result

type Idempotency interface {
	Execute(context.Context, IdempotencyInput, func() (IdempotencyResponse, error)) (IdempotencyResult, error)
}
