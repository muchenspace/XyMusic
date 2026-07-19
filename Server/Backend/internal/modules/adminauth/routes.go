package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/ratelimit"
)

const (
	AccessCookie  = "xymusic_admin_access"
	RefreshCookie = "xymusic_admin_refresh"
	CSRFCookie    = "xymusic_admin_csrf"
)

type Identity interface {
	Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error)
	Refresh(context.Context, string, string) (identity.RefreshResult, error)
	Authenticate(context.Context, string) (identity.AuthenticatedActor, error)
	Logout(context.Context, identity.AuthenticatedActor) error
	GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error)
}

type Routes struct {
	identity Identity
	config   config.Config
	limiter  ratelimit.Limiter
	trusted  map[string]struct{}
}

func NewRoutes(identityService Identity, cfg config.Config, limiter ratelimit.Limiter) (*Routes, error) {
	if identityService == nil || limiter == nil {
		return nil, errors.New("admin auth identity and limiter are required")
	}
	trusted := make(map[string]struct{}, len(cfg.HTTP.TrustedProxyAddresses))
	for _, address := range cfg.HTTP.TrustedProxyAddresses {
		normalized := normalizeIP(address)
		if normalized == "" {
			return nil, errors.New("trusted proxy address is invalid")
		}
		trusted[normalized] = struct{}{}
	}
	return &Routes{identity: identityService, config: cfg, limiter: limiter, trusted: trusted}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	auth := router.Group("/api/v1/admin/auth")
	auth.POST("/login", httpserver.Handle(routes.login))
	auth.GET("/session", httpserver.Handle(routes.session))
	auth.POST("/refresh", httpserver.Handle(routes.refresh))
	auth.POST("/logout", httpserver.Handle(routes.logout))
}

func (routes *Routes) login(c *gin.Context) error {
	var input struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		InstallationID string `json:"installationId"`
		DeviceName     string `json:"deviceName,omitempty"`
	}
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if len([]rune(input.Username)) < 3 || len([]rune(input.Username)) > 254 || len([]rune(input.Password)) < 1 || len([]rune(input.Password)) > 128 {
		return routeValidationError()
	}
	if _, err := uuid.Parse(input.InstallationID); err != nil {
		return routeValidationError()
	}
	if input.DeviceName != "" && (len([]rune(input.DeviceName)) < 1 || len([]rune(input.DeviceName)) > 100) {
		return routeValidationError()
	}
	if err := routes.limiter.Consume(c.Request.Context(), "admin-login:"+c.ClientIP(), 20, 15*time.Minute); err != nil {
		return err
	}
	if err := routes.limiter.Consume(c.Request.Context(), "admin-login-account:"+identity.RateLimitSubject(input.Username), 10, 15*time.Minute); err != nil {
		return err
	}
	deviceName := input.DeviceName
	if deviceName == "" {
		deviceName = "Web administration console"
	}
	session, err := routes.identity.Login(c.Request.Context(), identity.LoginInput{
		Username: input.Username, Password: input.Password,
		Device: identity.DeviceInfoInput{InstallationID: input.InstallationID, Name: deviceName, Platform: identity.DevicePlatformWeb, AppVersion: "admin-web/1"},
	})
	if err != nil {
		return err
	}
	actor, err := routes.identity.Authenticate(c.Request.Context(), "Bearer "+session.Tokens.AccessToken)
	if err != nil {
		return err
	}
	if actor.Role != identity.RoleAdmin {
		_ = routes.identity.Logout(c.Request.Context(), actor)
		return apperror.Forbidden("Administrator role is required")
	}
	csrf, err := newCSRFToken()
	if err != nil {
		return err
	}
	routes.writeSession(c, session, csrf)
	return nil
}

func (routes *Routes) session(c *gin.Context) error {
	actor, err := RequireAdmin(c, routes.identity, false)
	if err != nil {
		return err
	}
	user, err := routes.identity.GetAuthenticatedUser(c.Request.Context(), actor)
	if err != nil {
		return err
	}
	response := gin.H{"user": user}
	if csrf := ParseCookies(c.GetHeader("Cookie"))[CSRFCookie]; csrf != "" {
		response["csrfToken"] = csrf
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
	return nil
}

func (routes *Routes) refresh(c *gin.Context) error {
	if err := routes.limiter.Consume(c.Request.Context(), "admin-refresh:"+c.ClientIP(), 60, 15*time.Minute); err != nil {
		return err
	}
	cookies := ParseCookies(c.GetHeader("Cookie"))
	if err := RequireCSRF(c, cookies); err != nil {
		return err
	}
	refreshToken := cookies[RefreshCookie]
	if refreshToken == "" {
		return apperror.Unauthorized(apperror.CodeSessionRevoked, "Refresh session is unavailable")
	}
	result, err := routes.identity.Refresh(c.Request.Context(), refreshToken, c.GetHeader("Idempotency-Key"))
	if err != nil {
		return err
	}
	if result.Session.User.Role != identity.RoleAdmin {
		return apperror.Forbidden("Administrator role is required")
	}
	csrf := cookies[CSRFCookie]
	if csrf == "" {
		csrf, err = newCSRFToken()
		if err != nil {
			return err
		}
	}
	routes.writeSession(c, result.Session, csrf)
	return nil
}

func (routes *Routes) logout(c *gin.Context) error {
	actor, err := RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	if err := routes.identity.Logout(c.Request.Context(), actor); err != nil {
		return err
	}
	secure := routes.secureRequest(c.Request)
	appendSessionCookie(c, AccessCookie, "", "/api/v1/admin", 0, true, secure)
	appendSessionCookie(c, RefreshCookie, "", "/api/v1/admin/auth/refresh", 0, true, secure)
	appendSessionCookie(c, CSRFCookie, "", "/", 0, false, secure)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
	return nil
}

func (routes *Routes) writeSession(c *gin.Context, session identity.AuthSessionDTO, csrf string) {
	secure := routes.secureRequest(c.Request)
	appendSessionCookie(c, AccessCookie, session.Tokens.AccessToken, "/api/v1/admin", routes.config.Security.AccessTokenTTLSeconds, true, secure)
	appendSessionCookie(c, RefreshCookie, session.Tokens.RefreshToken, "/api/v1/admin/auth/refresh", routes.config.Security.RefreshTokenTTLSeconds, true, secure)
	appendSessionCookie(c, CSRFCookie, csrf, "/", routes.config.Security.RefreshTokenTTLSeconds, false, secure)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"user": session.User, "csrfToken": csrf})
}

func RequireAdmin(c *gin.Context, identityService Identity, mutation bool) (identity.AuthenticatedActor, error) {
	cookies := ParseCookies(c.GetHeader("Cookie"))
	authorization, authorizationPresent := c.Request.Header["Authorization"]
	authorizationValue := ""
	if authorizationPresent {
		if len(authorization) > 0 {
			authorizationValue = authorization[0]
		}
	} else if token := cookies[AccessCookie]; token != "" {
		authorizationValue = "Bearer " + token
	}
	actor, err := identityService.Authenticate(c.Request.Context(), authorizationValue)
	if err != nil {
		return identity.AuthenticatedActor{}, err
	}
	if actor.Role != identity.RoleAdmin {
		return identity.AuthenticatedActor{}, apperror.Forbidden("Administrator role is required")
	}
	if mutation && !authorizationPresent && cookies[AccessCookie] != "" {
		if err := RequireCSRF(c, cookies); err != nil {
			return identity.AuthenticatedActor{}, err
		}
	}
	return actor, nil
}

func RequireCSRF(c *gin.Context, cookies map[string]string) error {
	cookieValue := cookies[CSRFCookie]
	headerValue := c.GetHeader("X-CSRF-Token")
	if len(cookieValue) < 16 || len(cookieValue) != len(headerValue) || subtle.ConstantTimeCompare([]byte(cookieValue), []byte(headerValue)) != 1 {
		return apperror.Forbidden("CSRF token is invalid")
	}
	return nil
}

func ParseCookies(header string) map[string]string {
	result := make(map[string]string)
	for _, pair := range strings.Split(header, ";") {
		separator := strings.IndexByte(pair, '=')
		if separator < 1 {
			continue
		}
		name := strings.TrimSpace(pair[:separator])
		if name == "" {
			continue
		}
		if _, exists := result[name]; exists {
			continue
		}
		value, err := url.PathUnescape(strings.TrimSpace(pair[separator+1:]))
		if err == nil {
			result[name] = value
		}
	}
	return result
}

func appendSessionCookie(c *gin.Context, name, value, path string, maxAge int, httpOnly, secure bool) {
	if maxAge == 0 {
		maxAge = -1
	}
	sameSite := http.SameSiteStrictMode
	if secure {
		sameSite = http.SameSiteNoneMode
	}
	cookie := (&http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: httpOnly, Secure: secure, SameSite: sameSite}).String()
	c.Writer.Header().Add("Set-Cookie", cookie)
}

func (routes *Routes) secureRequest(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	direct := normalizeIP(remoteHost(request.RemoteAddr))
	if _, trusted := routes.trusted[direct]; !trusted {
		return false
	}
	values := strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")
	if len(values) == 0 || len(values) > 32 {
		return false
	}
	for index := range values {
		values[index] = strings.ToLower(strings.TrimSpace(values[index]))
		if values[index] != "http" && values[index] != "https" {
			return false
		}
	}
	return values[len(values)-1] == "https"
}

func normalizeIP(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(value, "::ffff:") && net.ParseIP(value[7:]) != nil {
		value = value[7:]
	}
	if net.ParseIP(value) == nil {
		return ""
	}
	return value
}

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func newCSRFToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func routeValidationError() error { return apperror.Validation("请求参数不符合接口要求") }
