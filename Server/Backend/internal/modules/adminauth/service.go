package adminauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/apperror"
)

// Service owns admin authentication orchestration: login, refresh and logout,
// including the admin role checks and the logout compensation that revokes a
// non-admin session issued by login. Transport concerns (cookies, CSRF
// headers, client-address rate-limit keys) stay in the routes layer.
type Service struct {
	identity Identity
	limiter  RateLimiter
	config   configView
}

// RateLimiter is the transport-independent rate limit contract used by admin
// authentication.
type RateLimiter interface {
	Consume(context.Context, string, int, time.Duration) error
}

type configView struct {
	accessTokenTTLSeconds  int
	refreshTokenTTLSeconds int
}

// LoginInput is the transport-neutral login payload after route-level decoding
// and length validation.
type LoginInput struct {
	Username       string
	Password       string
	InstallationID string
	DeviceName     string
}

// LoginResult carries the session and CSRF token for the transport layer to
// persist.
type LoginResult struct {
	Session   identity.AuthSessionDTO
	CSRFToken string
}

// RefreshResult carries the rotated session and CSRF token.
type RefreshResult struct {
	Session   identity.AuthSessionDTO
	CSRFToken string
}

func NewService(
	identityService Identity,
	limiter RateLimiter,
	accessTokenTTLSeconds, refreshTokenTTLSeconds int,
) (*Service, error) {
	if identityService == nil || limiter == nil {
		return nil, errors.New("admin auth identity and limiter are required")
	}
	return &Service{
		identity: identityService,
		limiter:  limiter,
		config: configView{
			accessTokenTTLSeconds:  accessTokenTTLSeconds,
			refreshTokenTTLSeconds: refreshTokenTTLSeconds,
		},
	}, nil
}

func (service *Service) AccessTokenTTLSeconds() int  { return service.config.accessTokenTTLSeconds }
func (service *Service) RefreshTokenTTLSeconds() int { return service.config.refreshTokenTTLSeconds }

// Login authenticates administrator credentials, consumes both login rate
// limits and verifies that the issued session actually carries the admin role.
// A non-admin session is revoked before the error is returned so no privileged
// cookie is ever handed to a regular user.
func (service *Service) Login(ctx context.Context, clientIP string, input LoginInput) (LoginResult, error) {
	if err := service.limiter.Consume(ctx, "admin-login:"+clientIP, 20, 15*time.Minute); err != nil {
		return LoginResult{}, err
	}
	if err := service.limiter.Consume(ctx, "admin-login-account:"+identity.RateLimitSubject(input.Username), 10, 15*time.Minute); err != nil {
		return LoginResult{}, err
	}
	deviceName := input.DeviceName
	if deviceName == "" {
		deviceName = "Web administration console"
	}
	session, err := service.identity.Login(ctx, identity.LoginInput{
		Username: input.Username, Password: input.Password,
		Device: identity.DeviceInfoInput{InstallationID: input.InstallationID, Name: deviceName, Platform: identity.DevicePlatformWeb, AppVersion: "admin-web/1"},
	})
	if err != nil {
		return LoginResult{}, err
	}
	actor, err := service.identity.Authenticate(ctx, "Bearer "+session.Tokens.AccessToken)
	if err != nil {
		return LoginResult{}, err
	}
	if actor.Role != identity.RoleAdmin {
		_ = service.identity.Logout(ctx, actor)
		return LoginResult{}, apperror.Forbidden("Administrator role is required")
	}
	csrf, err := newCSRFToken()
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Session: session, CSRFToken: csrf}, nil
}

// Refresh rotates an administrator refresh session. The refresh rate limit and
// CSRF proof are checked in the same order as the HTTP contract: rate limit,
// CSRF, refresh-token presence, rotation, then the admin role check.
func (service *Service) Refresh(
	ctx context.Context,
	clientIP, refreshToken, idempotencyKey string,
	csrf CSRFProof,
) (RefreshResult, error) {
	if err := service.limiter.Consume(ctx, "admin-refresh:"+clientIP, 60, 15*time.Minute); err != nil {
		return RefreshResult{}, err
	}
	if err := VerifyCSRF(csrf.Cookie, csrf.Header); err != nil {
		return RefreshResult{}, err
	}
	if refreshToken == "" {
		return RefreshResult{}, apperror.Unauthorized(apperror.CodeSessionRevoked, "Refresh session is unavailable")
	}
	result, err := service.identity.Refresh(ctx, refreshToken, idempotencyKey)
	if err != nil {
		return RefreshResult{}, err
	}
	if result.Session.User.Role != identity.RoleAdmin {
		return RefreshResult{}, apperror.Forbidden("Administrator role is required")
	}
	csrfToken := csrf.Cookie
	if csrfToken == "" {
		csrfToken, err = newCSRFToken()
		if err != nil {
			return RefreshResult{}, err
		}
	}
	return RefreshResult{Session: result.Session, CSRFToken: csrfToken}, nil
}

// Logout revokes the authenticated session.
func (service *Service) Logout(ctx context.Context, actor identity.AuthenticatedActor) error {
	return service.identity.Logout(ctx, actor)
}

// Session returns the authenticated user for the current admin session.
func (service *Service) Session(ctx context.Context, actor identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return service.identity.GetAuthenticatedUser(ctx, actor)
}

// CSRFProof carries the cookie/header pair compared by the double-submit CSRF
// check.
type CSRFProof struct {
	Cookie string
	Header string
}

// VerifyCSRF performs the constant-time double-submit comparison shared by the
// session and refresh flows.
func VerifyCSRF(cookieValue, headerValue string) error {
	if len(cookieValue) < 16 || len(cookieValue) != len(headerValue) || subtle.ConstantTimeCompare([]byte(cookieValue), []byte(headerValue)) != 1 {
		return apperror.Forbidden("CSRF token is invalid")
	}
	return nil
}
