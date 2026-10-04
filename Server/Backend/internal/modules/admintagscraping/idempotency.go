package admintagscraping

import (
	"context"
	"errors"
	"time"

	platformidempotency "xymusic/server/internal/platform/idempotency"
)

type PersistentIdempotency struct {
	service *platformidempotency.Service
}

func NewPersistentIdempotency(service *platformidempotency.Service) *PersistentIdempotency {
	return &PersistentIdempotency{service: service}
}

func (adapter *PersistentIdempotency) Execute(
	ctx context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	if adapter == nil || adapter.service == nil {
		return IdempotencyResult{}, errors.New("admin tag scraping idempotency service is required")
	}
	result, err := platformidempotency.Execute(ctx, adapter.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (platformidempotency.HTTPResult[rawJSON], error) {
		response, operationErr := operation()
		return platformidempotency.HTTPResult[rawJSON]{Status: response.Status, Body: rawJSON(response.Body)}, operationErr
	})
	if err != nil {
		return IdempotencyResult{}, err
	}
	return IdempotencyResult{Status: result.Status, Body: append([]byte(nil), result.Body...), Replayed: result.Replayed}, nil
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
