package identity

import (
	"context"
	"errors"

	platformidempotency "xymusic/server/internal/platform/idempotency"
)

// PersistentRefreshIdempotency adapts the shared encrypted PostgreSQL
// idempotency service to the identity module's narrow refresh-token boundary.
type PersistentRefreshIdempotency struct {
	service *platformidempotency.Service
}

var _ RefreshIdempotency = (*PersistentRefreshIdempotency)(nil)

func NewPersistentRefreshIdempotency(service *platformidempotency.Service) *PersistentRefreshIdempotency {
	return &PersistentRefreshIdempotency{service: service}
}

func (p *PersistentRefreshIdempotency) ExecuteRefresh(
	ctx context.Context,
	input RefreshIdempotencyInput,
	operation RefreshOperation,
) (AuthSessionDTO, bool, error) {
	if p == nil || p.service == nil {
		return AuthSessionDTO{}, false, errors.New("persistent refresh idempotency service is required")
	}
	result, err := platformidempotency.Execute(ctx, p.service, platformidempotency.Input{
		ActorID: input.ActorID,
		Scope:   input.Scope,
		Key:     input.Key,
		Payload: input.Payload,
	}, func() (platformidempotency.HTTPResult[AuthSessionDTO], error) {
		session, err := operation(ctx)
		if err != nil {
			return platformidempotency.HTTPResult[AuthSessionDTO]{}, err
		}
		return platformidempotency.HTTPResult[AuthSessionDTO]{Status: 200, Body: session}, nil
	})
	if err != nil {
		return AuthSessionDTO{}, false, err
	}
	return result.Body, result.Replayed, nil
}
