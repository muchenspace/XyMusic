package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	accessTokenIssuer   = "xymusic-server"
	accessTokenAudience = "xymusic-android"
)

var (
	ErrInvalidAccessToken = errors.New("access token is invalid")
	ErrExpiredAccessToken = errors.New("access token has expired")
)

type Principal struct {
	UserID      string
	SessionID   string
	AuthVersion int
	Role        string
}

type AccessTokenService struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

type accessClaims struct {
	SessionID   string `json:"sid"`
	AuthVersion int    `json:"av"`
	Role        string `json:"role"`
	jwt.RegisteredClaims
}

func NewAccessTokenService(secret string, ttl time.Duration) *AccessTokenService {
	return &AccessTokenService{secret: []byte(secret), ttl: ttl, now: time.Now}
}

func (s *AccessTokenService) Issue(principal Principal) (string, time.Time, error) {
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)
	claims := accessClaims{
		SessionID:   principal.SessionID,
		AuthVersion: principal.AuthVersion,
		Role:        principal.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   principal.UserID,
			Issuer:    accessTokenIssuer,
			Audience:  jwt.ClaimStrings{accessTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, expiresAt, nil
}

func (s *AccessTokenService) Verify(raw string) (Principal, error) {
	claims := &accessClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidAccessToken
		}
		return s.secret, nil
	},
		jwt.WithAudience(accessTokenAudience),
		jwt.WithIssuer(accessTokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Principal{}, ErrExpiredAccessToken
		}
		return Principal{}, ErrInvalidAccessToken
	}
	if claims.Subject == "" || claims.SessionID == "" || claims.AuthVersion < 1 || (claims.Role != "USER" && claims.Role != "ADMIN") {
		return Principal{}, ErrInvalidAccessToken
	}
	return Principal{UserID: claims.Subject, SessionID: claims.SessionID, AuthVersion: claims.AuthVersion, Role: claims.Role}, nil
}
