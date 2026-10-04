package adminmedia

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

func (adapter *PersistentIdempotency) ExecuteReservation(
	ctx context.Context,
	input IdempotencyInput,
	operation func() (UploadReservationDTO, error),
) (UploadReservationDTO, bool, error) {
	if adapter == nil || adapter.service == nil {
		return UploadReservationDTO{}, false, errors.New("admin media idempotency service is required")
	}
	result, err := platformidempotency.Execute(ctx, adapter.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (platformidempotency.HTTPResult[UploadReservationDTO], error) {
		body, operationErr := operation()
		return platformidempotency.HTTPResult[UploadReservationDTO]{
			Status: 201,
			Body:   body,
		}, operationErr
	})
	if err != nil {
		return UploadReservationDTO{}, false, err
	}
	return result.Body, result.Replayed, nil
}

func (adapter *PersistentIdempotency) ExecuteCompletion(
	ctx context.Context,
	input IdempotencyInput,
	operation func() (UploadCompletionDTO, error),
) (UploadCompletionDTO, bool, error) {
	if adapter == nil || adapter.service == nil {
		return UploadCompletionDTO{}, false, errors.New("admin media idempotency service is required")
	}
	result, err := platformidempotency.Execute(ctx, adapter.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (platformidempotency.HTTPResult[UploadCompletionDTO], error) {
		body, operationErr := operation()
		return platformidempotency.HTTPResult[UploadCompletionDTO]{
			Status: 200,
			Body:   body,
		}, operationErr
	})
	if err != nil {
		return UploadCompletionDTO{}, false, err
	}
	return result.Body, result.Replayed, nil
}
