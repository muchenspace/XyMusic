package adminmedia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type Application interface {
	CreateUpload(c context.Context, actorID string, idempotencyKey string, input CreateUploadInput) (UploadReservationDTO, bool, error)
	UploadDirect(c context.Context, uploadID string, body io.Reader, contentLength int64) error
	CompleteUpload(c context.Context, actorID string, uploadID string, idempotencyKey string, input CompleteUploadInput) (UploadCompletionDTO, bool, error)
}

type Routes struct {
	identity    Identity
	application Application
}

func NewRoutes(identity Identity, application Application) *Routes {
	return &Routes{identity: identity, application: application}
}

func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.POST("/media/uploads", httpserver.Handle(routes.createUpload))
	admin.PUT("/media/uploads/:id/content", httpserver.Handle(routes.uploadDirect))
	admin.POST("/media/uploads/:id/complete", httpserver.Handle(routes.completeUpload))
}

func (routes *Routes) createUpload(c *gin.Context) error {
	var input CreateUploadInput
	if err := decodeMediaJSON(c, &input); err != nil {
		return err
	}
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	result, replayed, err := routes.application.CreateUpload(
		c.Request.Context(),
		actor.UserID,
		c.GetHeader("Idempotency-Key"),
		input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", formatReplay(replayed))
	c.JSON(http.StatusCreated, result)
	return nil
}

func (routes *Routes) uploadDirect(c *gin.Context) error {
	uploadID := c.Param("id")
	if _, err := uuid.Parse(uploadID); err != nil {
		return apperror.Validation("id must be a UUID")
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	err := routes.application.UploadDirect(c.Request.Context(), uploadID, c.Request.Body, c.Request.ContentLength)
	if err != nil {
		return err
	}
	c.Status(http.StatusOK)
	return nil
}

func (routes *Routes) completeUpload(c *gin.Context) error {
	uploadID := c.Param("id")
	if _, err := uuid.Parse(uploadID); err != nil {
		return apperror.Validation("id must be a UUID")
	}
	var input CompleteUploadInput
	if err := decodeMediaJSON(c, &input); err != nil {
		return err
	}
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	result, replayed, err := routes.application.CompleteUpload(
		c.Request.Context(),
		actor.UserID,
		uploadID,
		c.GetHeader("Idempotency-Key"),
		input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", formatReplay(replayed))
	c.JSON(http.StatusOK, result)
	return nil
}

func decodeMediaJSON(c *gin.Context, destination any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return apperror.Validation("Request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(destination); err != nil {
		return apperror.Validation("Request body is invalid")
	}
	return nil
}

func formatReplay(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
