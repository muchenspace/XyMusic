package adminmanagement

import (
	"context"
	"errors"
	"time"

	sharedidempotency "xymusic/server/internal/shared/idempotency"
)

type PersistentIdempotency struct {
	service *sharedidempotency.Service
}

func NewPersistentIdempotency(service *sharedidempotency.Service) *PersistentIdempotency {
	return &PersistentIdempotency{service: service}
}

func (adapter *PersistentIdempotency) Execute(
	ctx context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	if adapter == nil || adapter.service == nil {
		return IdempotencyResult{}, errors.New("admin management idempotency service is required")
	}
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID, Scope: input.Scope, Key: input.Key, Payload: input.Payload, TTL: 24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[rawJSON], error) {
		response, err := operation()
		return sharedidempotency.HTTPResult[rawJSON]{
			Status: response.Status, Body: rawJSON(response.Body),
		}, err
	})
	if err != nil {
		return IdempotencyResult{}, err
	}
	return IdempotencyResult{
		Status: result.Status, Body: append([]byte(nil), result.Body...), Replayed: result.Replayed,
	}, nil
}

type rawJSON []byte

func (message rawJSON) MarshalJSON() ([]byte, error) {
	if len(message) == 0 {
		return []byte("null"), nil
	}
	return message, nil
}

func (message *rawJSON) UnmarshalJSON(raw []byte) error {
	*message = append((*message)[:0], raw...)
	return nil
}
