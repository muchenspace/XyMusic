package playlist

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
		return IdempotencyResult{}, errors.New("playlist idempotency service is required")
	}
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[jsonRawMessage], error) {
		response, err := operation()
		return sharedidempotency.HTTPResult[jsonRawMessage]{
			Status: response.Status,
			Body:   jsonRawMessage(response.Body),
		}, err
	})
	if err != nil {
		return IdempotencyResult{}, err
	}
	return IdempotencyResult{
		Status:   result.Status,
		Body:     append([]byte(nil), result.Body...),
		Replayed: result.Replayed,
	}, nil
}

// A named byte slice preserves json.RawMessage's marshal/unmarshal behavior
// through the generic shared idempotency service without exposing it there.
type jsonRawMessage []byte

func (message jsonRawMessage) MarshalJSON() ([]byte, error) {
	if len(message) == 0 {
		return []byte("null"), nil
	}
	return message, nil
}

func (message *jsonRawMessage) UnmarshalJSON(raw []byte) error {
	*message = append((*message)[:0], raw...)
	return nil
}
