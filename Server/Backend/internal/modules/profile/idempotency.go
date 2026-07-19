package profile

import (
	"context"
	"errors"
	"time"

	"xymusic/server/internal/modules/identity"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
)

// PersistentIdempotency reuses the encrypted PostgreSQL idempotency store and
// keeps typed replay payloads for both profile and avatar mutations.
type PersistentIdempotency struct {
	service *sharedidempotency.Service
}

var _ Idempotency = (*PersistentIdempotency)(nil)

func NewPersistentIdempotency(service *sharedidempotency.Service) *PersistentIdempotency {
	return &PersistentIdempotency{service: service}
}

func (adapter *PersistentIdempotency) ExecuteCurrentUser(
	ctx context.Context,
	input IdempotencyInput,
	status int,
	operation func() (identity.CurrentUserDTO, error),
) (MutationResult[identity.CurrentUserDTO], error) {
	if adapter == nil || adapter.service == nil {
		return MutationResult[identity.CurrentUserDTO]{}, errors.New("profile idempotency service is required")
	}
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[identity.CurrentUserDTO], error) {
		body, err := operation()
		return sharedidempotency.HTTPResult[identity.CurrentUserDTO]{Status: status, Body: body}, err
	})
	if err != nil {
		return MutationResult[identity.CurrentUserDTO]{}, err
	}
	return MutationResult[identity.CurrentUserDTO]{Body: result.Body, Replayed: result.Replayed}, nil
}

func (adapter *PersistentIdempotency) ExecuteAvatarUpload(
	ctx context.Context,
	input IdempotencyInput,
	status int,
	operation func() (AvatarUploadDTO, error),
) (MutationResult[AvatarUploadDTO], error) {
	if adapter == nil || adapter.service == nil {
		return MutationResult[AvatarUploadDTO]{}, errors.New("profile idempotency service is required")
	}
	result, err := sharedidempotency.Execute(ctx, adapter.service, sharedidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (sharedidempotency.HTTPResult[AvatarUploadDTO], error) {
		body, err := operation()
		return sharedidempotency.HTTPResult[AvatarUploadDTO]{Status: status, Body: body}, err
	})
	if err != nil {
		return MutationResult[AvatarUploadDTO]{}, err
	}
	return MutationResult[AvatarUploadDTO]{Body: result.Body, Replayed: result.Replayed}, nil
}
