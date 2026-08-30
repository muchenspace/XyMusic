package adminmedia

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

func (adapter *PersistentIdempotency) ExecuteReservation(
	ctx context.Context,
	input IdempotencyInput,
	operation func() (UploadReservationDTO, error),
) (UploadReservationDTO, bool, error) {
	if adapter == nil || adapter.service == nil {
		return UploadReservationDTO{}, false, errors.New("admin media idempotency service is required")
	}
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[UploadReservationDTO], error) {
		body, operationErr := operation()
		return sharedidempotency.HTTPResult[UploadReservationDTO]{
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
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[UploadCompletionDTO], error) {
		body, operationErr := operation()
		return sharedidempotency.HTTPResult[UploadCompletionDTO]{
			Status: 200,
			Body:   body,
		}, operationErr
	})
	if err != nil {
		return UploadCompletionDTO{}, false, err
	}
	return result.Body, result.Replayed, nil
}
