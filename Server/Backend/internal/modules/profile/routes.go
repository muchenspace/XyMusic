package profile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type Application interface {
	GetCurrentUser(context.Context, string) (identity.CurrentUserDTO, error)
	UpdateCurrentUser(context.Context, string, string, UpdateProfileInput) (MutationResult[identity.CurrentUserDTO], error)
	CreateAvatarUpload(context.Context, string, string, string, CreateAvatarUploadInput) (MutationResult[AvatarUploadDTO], error)
	CompleteAvatarUpload(context.Context, string, string, string, string, CompleteAvatarUploadInput) (MutationResult[identity.CurrentUserDTO], error)
}

type Routes struct {
	authenticator Authenticator
	application   Application
}

func NewRoutes(authenticator Authenticator, application Application) *Routes {
	return &Routes{authenticator: authenticator, application: application}
}

func (routes *Routes) Register(engine *gin.Engine) {
	users := engine.Group("/api/v1/users")
	users.GET("/me", httpserver.Handle(routes.getCurrentUser))
	users.PATCH("/me", httpserver.Handle(routes.updateCurrentUser))
	users.POST("/me/avatar/uploads", httpserver.Handle(routes.createAvatarUpload))
	users.POST("/me/avatar/uploads/:id/complete", httpserver.Handle(routes.completeAvatarUpload))
}

func (routes *Routes) getCurrentUser(c *gin.Context) error {
	actor, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.application.GetCurrentUser(c.Request.Context(), actor.UserID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) updateCurrentUser(c *gin.Context) error {
	var input UpdateProfileInput
	if err := decodeProfileJSON(c, &input); err != nil {
		return err
	}
	if err := validateUpdateProfileContract(input); err != nil {
		return err
	}
	actor, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.application.UpdateCurrentUser(
		c.Request.Context(),
		actor.UserID,
		c.GetHeader("Idempotency-Key"),
		input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", formatReplay(result.Replayed))
	c.JSON(http.StatusOK, result.Body)
	return nil
}

func (routes *Routes) createAvatarUpload(c *gin.Context) error {
	var input CreateAvatarUploadInput
	if err := decodeProfileJSON(c, &input); err != nil {
		return err
	}
	if err := validateCreateAvatarUploadContract(input); err != nil {
		return err
	}
	actor, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.application.CreateAvatarUpload(
		c.Request.Context(),
		actor.UserID,
		httpserver.TraceID(c),
		c.GetHeader("Idempotency-Key"),
		input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", formatReplay(result.Replayed))
	c.JSON(http.StatusCreated, result.Body)
	return nil
}

func (routes *Routes) completeAvatarUpload(c *gin.Context) error {
	uploadID := c.Param("id")
	if _, err := uuid.Parse(uploadID); err != nil {
		return apperror.Validation("id must be a UUID")
	}
	var input CompleteAvatarUploadInput
	if err := decodeProfileJSON(c, &input); err != nil {
		return err
	}
	actor, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.application.CompleteAvatarUpload(
		c.Request.Context(),
		actor.UserID,
		httpserver.TraceID(c),
		uploadID,
		c.GetHeader("Idempotency-Key"),
		input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", formatReplay(result.Replayed))
	c.JSON(http.StatusOK, result.Body)
	return nil
}

func (routes *Routes) authenticate(c *gin.Context) (identity.AuthenticatedActor, error) {
	if routes.authenticator == nil {
		return identity.AuthenticatedActor{}, apperror.Unauthorized(
			apperror.CodeAuthenticationRequired,
			"Authentication is required",
		)
	}
	return routes.authenticator.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
}

func formatReplay(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func decodeProfileJSON(c *gin.Context, destination any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return profileRouteValidationError()
	}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(destination); err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return apperror.PayloadTooLarge("Request body exceeds the permitted size")
		}
		return profileRouteValidationError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return profileRouteValidationError()
	}
	return nil
}

func validateUpdateProfileContract(input UpdateProfileInput) error {
	if input.ExpectedVersion < 1 || (!input.DisplayName.Set && !input.Bio.Set) {
		return profileRouteValidationError()
	}
	if input.DisplayName.Set {
		length := javascriptStringLength(input.DisplayName.Value)
		if length < 1 || length > 64 {
			return profileRouteValidationError()
		}
	}
	if input.Bio.Set && input.Bio.Value != nil && javascriptStringLength(*input.Bio.Value) > 500 {
		return profileRouteValidationError()
	}
	return nil
}

func validateCreateAvatarUploadContract(input CreateAvatarUploadInput) error {
	fileNameLength := javascriptStringLength(input.FileName)
	if fileNameLength < 1 || fileNameLength > 255 ||
		(input.ContentType != "image/jpeg" && input.ContentType != "image/png" && input.ContentType != "image/webp") ||
		input.SizeBytes < 1 || input.SizeBytes > AvatarMaximumBytes ||
		!checksumPattern.MatchString(input.ChecksumSHA256) {
		return profileRouteValidationError()
	}
	return nil
}

func profileRouteValidationError() error {
	return apperror.Validation("请求参数不符合接口要求")
}
