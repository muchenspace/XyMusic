package playback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
)

var (
	ErrInvalidTicket = apperror.Unauthorized(apperror.CodeAuthenticationRequired, "Playback ticket is invalid")
	ErrExpiredTicket = apperror.Unauthorized(apperror.CodeAccessTokenExpired, "Playback ticket has expired")
)

type TicketClaims struct {
	UserID    string `json:"u"`
	TrackID   string `json:"t"`
	SessionID string `json:"s"`
	Quality   string `json:"q"`
	Codec     string `json:"c"`
	ExpiresAt int64  `json:"e"`
}

type TicketSigner struct {
	secret []byte
	now    func() time.Time
}

func NewTicketSigner(secret string) (*TicketSigner, error) {
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("playback ticket secret must contain at least 32 characters")
	}
	return &TicketSigner{
		secret: []byte(secret),
		now:    time.Now,
	}, nil
}

func (s *TicketSigner) Sign(claims TicketClaims) (string, error) {
	data, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal ticket claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + signature, nil
}

func (s *TicketSigner) Verify(token string) (*TicketClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidTicket
	}
	payload, signature := parts[0], parts[1]

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	expectedSig := mac.Sum(nil)
	actualSig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(actualSig, expectedSig) {
		return nil, ErrInvalidTicket
	}

	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidTicket
	}

	var claims TicketClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, ErrInvalidTicket
	}

	now := s.now().UTC().Unix()
	if strings.TrimSpace(claims.UserID) == "" || strings.TrimSpace(claims.TrackID) == "" ||
		strings.TrimSpace(claims.SessionID) == "" || strings.TrimSpace(claims.Quality) == "" ||
		strings.TrimSpace(claims.Codec) == "" {
		return nil, ErrInvalidTicket
	}
	if claims.ExpiresAt <= now {
		return nil, ErrExpiredTicket
	}

	return &claims, nil
}
