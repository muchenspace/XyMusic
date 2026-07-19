package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/ratelimit"
)

type IdentityApplication interface {
	Register(context.Context, RegisterInput) (RegistrationDTO, error)
	Login(context.Context, LoginInput) (AuthSessionDTO, error)
	Refresh(context.Context, string, string) (RefreshResult, error)
	Authenticate(context.Context, string) (AuthenticatedActor, error)
	Logout(context.Context, AuthenticatedActor) error
	LogoutAll(context.Context, AuthenticatedActor) error
}

type Routes struct {
	identity IdentityApplication
	limiter  ratelimit.Limiter
}

func NewRoutes(identity IdentityApplication, limiter ratelimit.Limiter) *Routes {
	return &Routes{identity: identity, limiter: limiter}
}

func (routes *Routes) Register(engine *gin.Engine) {
	auth := engine.Group("/api/v1/auth")
	auth.POST("/register", httpserver.Handle(routes.register))
	auth.POST("/login", httpserver.Handle(routes.login))
	auth.POST("/refresh", httpserver.Handle(routes.refresh))
	auth.POST("/logout", httpserver.Handle(routes.logout))
	auth.POST("/logout-all", httpserver.Handle(routes.logoutAll))
}

func (routes *Routes) register(c *gin.Context) error {
	var input RegisterInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validateRegistration(trimSpace(input.Username), input.Password); err != nil {
		return routeValidationError()
	}
	if err := routes.consume(c, "register:"+clientAddress(c), 10, time.Hour); err != nil {
		return err
	}
	result, err := routes.identity.Register(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}

func (routes *Routes) login(c *gin.Context) error {
	var input LoginInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validateLogin(input); err != nil {
		return routeValidationError()
	}
	if input.Device.Platform != DevicePlatformAndroid && input.Device.Platform != DevicePlatformWindows {
		return routeValidationError()
	}
	if err := routes.consume(c, "login:"+clientAddress(c), 30, 15*time.Minute); err != nil {
		return err
	}
	if err := routes.consume(c, "login-account:"+RateLimitSubject(input.Username), 10, 15*time.Minute); err != nil {
		return err
	}
	result, err := routes.identity.Login(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) refresh(c *gin.Context) error {
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validateRefreshInput(input.RefreshToken, c.GetHeader("Idempotency-Key")); err != nil {
		return routeValidationError()
	}
	if err := routes.consume(c, "refresh:"+clientAddress(c), 60, 15*time.Minute); err != nil {
		return err
	}
	result, err := routes.identity.Refresh(c.Request.Context(), input.RefreshToken, c.GetHeader("Idempotency-Key"))
	if err != nil {
		return err
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Idempotent-Replay", formatBoolean(result.Replayed))
	c.JSON(http.StatusOK, result.Session)
	return nil
}

func (routes *Routes) logout(c *gin.Context) error {
	actor, err := routes.identity.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
	if err != nil {
		return err
	}
	if err := routes.identity.Logout(c.Request.Context(), actor); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (routes *Routes) logoutAll(c *gin.Context) error {
	actor, err := routes.identity.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
	if err != nil {
		return err
	}
	if err := routes.identity.LogoutAll(c.Request.Context(), actor); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (routes *Routes) consume(c *gin.Context, key string, maximum int, window time.Duration) error {
	if routes.limiter == nil {
		return apperror.DependencyUnavailable("限流状态暂时不可用")
	}
	return routes.limiter.Consume(c.Request.Context(), key, maximum, window)
}

func RateLimitSubject(value string) string {
	normalized := NormalizeUsername(value)
	digest := sha256.Sum256([]byte(normalized))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:22]
}

func clientAddress(c *gin.Context) string {
	address := strings.TrimSpace(c.ClientIP())
	if address == "" {
		return "unknown"
	}
	return address
}

func formatBoolean(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func routeValidationError() error {
	return apperror.Validation("请求参数不符合接口要求")
}
