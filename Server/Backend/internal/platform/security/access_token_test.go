package security

import (
	"errors"
	"testing"
	"time"
)

func TestAccessTokenRoundTripAndExpiry(t *testing.T) {
	service := NewAccessTokenService("01234567890123456789012345678901", 15*time.Minute)
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	raw, expiresAt, err := service.Issue(Principal{UserID: "user", SessionID: "session", AuthVersion: 2, Role: "ADMIN"})
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("unexpected expiry: %s", expiresAt)
	}
	principal, err := service.Verify(raw)
	if err != nil || principal.UserID != "user" || principal.Role != "ADMIN" {
		t.Fatalf("unexpected principal: %#v %v", principal, err)
	}
	service.now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, err := service.Verify(raw); !errors.Is(err, ErrExpiredAccessToken) {
		t.Fatalf("expected expiry error, got %v", err)
	}
}
