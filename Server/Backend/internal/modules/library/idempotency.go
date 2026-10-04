package library

import (
	"context"
	"errors"
	"time"

	platformidempotency "xymusic/server/internal/platform/idempotency"
)

type PersistentIdempotency struct {
	service *platformidempotency.Service
}

var _ Idempotency = (*PersistentIdempotency)(nil)

func NewPersistentIdempotency(service *platformidempotency.Service) *PersistentIdempotency {
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
	result, err := platformidempotency.Execute(ctx, adapter.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (platformidempotency.HTTPResult[HistoryItemDTO], error) {
		body, err := operation()
		return platformidempotency.HTTPResult[HistoryItemDTO]{Status: 200, Body: body}, err
	})
	if err != nil {
		return MutationResult[HistoryItemDTO]{}, err
	}
	return MutationResult[HistoryItemDTO]{Body: result.Body, Replayed: result.Replayed}, nil
}
