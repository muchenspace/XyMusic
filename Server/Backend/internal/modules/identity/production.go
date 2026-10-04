package identity

import (
	"errors"
	"time"

	"xymusic/server/internal/platform/security"
)

// This file holds the production adapters that translate the identity module
// ports to internal/platform/security. The composition root injects them so
// service.go stays free of platform/security imports.

var _ AccessTokenManager = (*AccessTokenService)(nil)

// AccessTokenService adapts platform/security.AccessTokenService to the
// module-local access-token contract.
type AccessTokenService struct {
	delegate *security.AccessTokenService
}

func NewAccessTokenService(secret string, ttl time.Duration) *AccessTokenService {
	return &AccessTokenService{delegate: security.NewAccessTokenService(secret, ttl)}
}

func (service *AccessTokenService) Issue(principal Principal) (string, time.Time, error) {
	return service.delegate.Issue(security.Principal{
		UserID:      principal.UserID,
		SessionID:   principal.SessionID,
		AuthVersion: principal.AuthVersion,
		Role:        principal.Role,
	})
}

func (service *AccessTokenService) Verify(raw string) (Principal, error) {
	principal, err := service.delegate.Verify(raw)
	if err != nil {
		if errors.Is(err, security.ErrExpiredAccessToken) {
			return Principal{}, ErrExpiredAccessToken
		}
		return Principal{}, err
	}
	return Principal{
		UserID:      principal.UserID,
		SessionID:   principal.SessionID,
		AuthVersion: principal.AuthVersion,
		Role:        principal.Role,
	}, nil
}

// SecurityPasswordManager adapts platform/security password hashing.
type SecurityPasswordManager struct{}

func (SecurityPasswordManager) Hash(password string) (string, error) {
	return security.HashPassword(password)
}

func (SecurityPasswordManager) Verify(password, encoded string) (bool, error) {
	return security.VerifyPassword(password, encoded)
}

// SecuritySecretHasher adapts platform/security.HashSecret.
type SecuritySecretHasher struct{}

func (SecuritySecretHasher) HashSecret(value string) string {
	return security.HashSecret(value)
}

// CreateOpaqueToken is the production refresh-token generator.
func CreateOpaqueToken() (string, error) {
	return security.CreateOpaqueToken()
}
