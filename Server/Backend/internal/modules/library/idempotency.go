package library

import (
	"context"
	"errors"
	"time"

	sharedidempotency "xymusic/server/internal/shared/idempotency"
)

type PersistentIdempotency struct {
	service *sharedidempotency.Service
}

var _ Idempotency = (*PersistentIdempotency)(nil)

func NewPersistentIdempotency(service *sharedidempotency.Service) *PersistentIdempotency {
	return &PersistentIdempotency{service: service}
}

func (adapter *PersistentIdempotency) ExecutePlayback(
	ctx context.Context,
	input IdempotencyInput,
	operation func() (HistoryItemDTO, error),
) (MutationResult[HistoryItemDTO], error) {
	if adapter == nil || adapter.service == nil {
		return MutationResult[HistoryItemDTO]{}, errors.New("library idempotency service is required")
	}
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[HistoryItemDTO], error) {
		body, err := operation()
		return sharedidempotency.HTTPResult[HistoryItemDTO]{Status: 200, Body: body}, err
	})
	if err != nil {
		return MutationResult[HistoryItemDTO]{}, err
	}
	return MutationResult[HistoryItemDTO]{Body: result.Body, Replayed: result.Replayed}, nil
}
