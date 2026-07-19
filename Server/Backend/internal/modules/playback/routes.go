package playback

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type API interface {
	CreateGrant(context.Context, string, Input) (GrantDTO, error)
}
type Authenticator interface {
	Authenticate(context.Context, string) error
}
type AuthenticateFunc func(context.Context, string) error

func (function AuthenticateFunc) Authenticate(ctx context.Context, authorization string) error {
	return function(ctx, authorization)
}

type Routes struct {
	service      API
	authenticate Authenticator
}

func NewRoutes(service API, authenticate Authenticator) (*Routes, error) {
	if service == nil || authenticate == nil {
		return nil, apperror.Internal("playback routes require service and authenticator", nil)
	}
	return &Routes{service: service, authenticate: authenticate}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	router.POST("/api/v1/tracks/:id/playback", httpserver.Handle(routes.createGrant))
}

func (routes *Routes) createGrant(c *gin.Context) error {
	trackID := c.Param("id")
	if _, err := uuid.Parse(trackID); err != nil {
		return routeValidationError()
	}
	var input Input
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if !validQuality(input.PreferredQuality) {
		return routeValidationError()
	}
	if _, err := normalizeCodecs(input.AcceptedCodecs); err != nil {
		return routeValidationError()
	}
	if err := routes.authenticate.Authenticate(c.Request.Context(), c.GetHeader("Authorization")); err != nil {
		return err
	}
	result, err := routes.service.CreateGrant(c.Request.Context(), trackID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func validQuality(value PreferredQuality) bool {
	return value == QualityAuto || value == QualityDataSaver || value == QualityStandard || value == QualityHigh || value == QualityLossless
}

func routeValidationError() error { return apperror.Validation("请求参数不符合接口要求") }
