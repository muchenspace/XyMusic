package profile

import (
	"context"
	"errors"
	"time"

	"xymusic/server/internal/modules/identity"
	platformidempotency "xymusic/server/internal/platform/idempotency"
)

// PersistentIdempotency reuses the encrypted PostgreSQL idempotency store and
// keeps typed replay payloads for both profile and avatar mutations.
type PersistentIdempotency struct {
	service *platformidempotency.Service
}

var _ Idempotency = (*PersistentIdempotency)(nil)

func NewPersistentIdempotency(service *platformidempotency.Service) *PersistentIdempotency {
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
	result, err := platformidempotency.Execute(ctx, adapter.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (platformidempotency.HTTPResult[identity.CurrentUserDTO], error) {
		body, err := operation()
		return platformidempotency.HTTPResult[identity.CurrentUserDTO]{Status: status, Body: body}, err
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
	result, err := platformidempotency.Execute(ctx, adapter.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
		TTL:     24 * time.Hour,
	}, func() (platformidempotency.HTTPResult[AvatarUploadDTO], error) {
		body, err := operation()
		return platformidempotency.HTTPResult[AvatarUploadDTO]{Status: status, Body: body}, err
	})
	if err != nil {
		return MutationResult[AvatarUploadDTO]{}, err
	}
	return MutationResult[AvatarUploadDTO]{Body: result.Body, Replayed: result.Replayed}, nil
}
