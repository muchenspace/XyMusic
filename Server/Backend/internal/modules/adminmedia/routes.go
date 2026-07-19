package adminmedia

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type API interface {
	CreateUpload(context.Context, string, string, CreateUploadInput) (UploadReservationDTO, error)
	UploadContent(context.Context, string, string, string, int64, io.Reader) error
	CompleteUpload(context.Context, string, string, string, CompleteUploadInput) (UploadCompletionDTO, error)
	GetJob(context.Context, string) (MediaJobDTO, error)
	RetryJob(context.Context, string, string, string, RetryJobInput) (MediaJobDTO, error)
}

type Routes struct {
	service     API
	identity    adminauth.Identity
	idempotency Idempotency
}

func NewRoutes(service API, identity adminauth.Identity, idempotency Idempotency) (*Routes, error) {
	if service == nil {
		return nil, errors.New("admin media API service is required")
	}
	if identity == nil {
		return nil, errors.New("admin media identity service is required")
	}
	if idempotency == nil {
		return nil, errors.New("admin media idempotency service is required")
	}
	return &Routes{service: service, identity: identity, idempotency: idempotency}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	media := router.Group("/api/v1/admin/media")
	media.POST("/uploads", httpserver.Handle(routes.createUpload))
	media.PUT("/uploads/:id/content", httpserver.Handle(routes.uploadContent))
	media.POST("/uploads/:id/complete", httpserver.Handle(routes.completeUpload))
	media.GET("/jobs/:id", httpserver.Handle(routes.getJob))
	media.POST("/jobs/:id/retry", httpserver.Handle(routes.retryJob))
}

func (routes *Routes) createUpload(c *gin.Context) error {
	var input CreateUploadInput
	if err := decodeStrictJSON(c, &input); err != nil {
		return err
	}
	if !validPurpose(input.Purpose) || !routeUUIDPattern.MatchString(input.TargetID) ||
		!routeStringLength(input.FileName, 1, 255) ||
		!routeStringLength(input.ContentType, 3, 100) ||
		input.SizeBytes < 1 || !checksumPattern.MatchString(input.ChecksumSHA256) {
		return routeContractError()
	}
	return routes.mutate(
		c,
		"admin.media.upload.create",
		input,
		http.StatusCreated,
		func(actorID, traceID string) (any, error) {
			return routes.service.CreateUpload(c.Request.Context(), actorID, traceID, input)
		},
	)
}

func (routes *Routes) uploadContent(c *gin.Context) error {
	uploadID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	contentLength, err := requestContentLength(c.Request)
	if err != nil {
		return err
	}
	if err := routes.service.UploadContent(
		c.Request.Context(),
		actor.UserID,
		uploadID,
		c.GetHeader("Content-Type"),
		contentLength,
		c.Request.Body,
	); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (routes *Routes) completeUpload(c *gin.Context) error {
	uploadID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input CompleteUploadInput
	if err := decodeStrictJSON(c, &input); err != nil {
		return err
	}
	if input.ObservedETag.Set && !routeStringLength(input.ObservedETag.Value, 1, 200) {
		return routeContractError()
	}
	payload := make(map[string]any)
	if input.ObservedETag.Set {
		payload["observedEtag"] = input.ObservedETag.Value
	}
	return routes.mutate(
		c,
		"admin.media.upload.complete:"+uploadID,
		payload,
		http.StatusAccepted,
		func(actorID, traceID string) (any, error) {
			return routes.service.CompleteUpload(c.Request.Context(), actorID, traceID, uploadID, input)
		},
	)
}

func (routes *Routes) getJob(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	job, err := routes.service.GetJob(c.Request.Context(), jobID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, job)
	return nil
}

func (routes *Routes) retryJob(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input RetryJobInput
	if err := decodeStrictJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 ||
		(input.Reason.Set && !routeStringLength(input.Reason.Value, 1, 500)) {
		return routeContractError()
	}
	payload := map[string]any{"expectedVersion": input.ExpectedVersion}
	if input.Reason.Set {
		payload["reason"] = input.Reason.Value
	}
	return routes.mutate(
		c,
		"admin.media.job.retry:"+jobID,
		payload,
		http.StatusAccepted,
		func(actorID, traceID string) (any, error) {
			return routes.service.RetryJob(c.Request.Context(), actorID, traceID, jobID, input)
		},
	)
}

func (routes *Routes) mutate(
	c *gin.Context,
	scope string,
	payload any,
	status int,
	operation func(string, string) (any, error),
) error {
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !routeIdempotencyKey.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	traceID := httpserver.TraceID(c)
	result, err := routes.idempotency.Execute(c.Request.Context(), IdempotencyInput{
		ActorID: actor.UserID,
		Scope:   scope,
		Key:     key,
		Payload: payload,
	}, func() (IdempotencyResponse, error) {
		body, operationErr := operation(actor.UserID, traceID)
		if operationErr != nil {
			return IdempotencyResponse{}, operationErr
		}
		encoded, encodeErr := json.Marshal(body)
		if encodeErr != nil {
			return IdempotencyResponse{}, encodeErr
		}
		return IdempotencyResponse{Status: status, Body: encoded}, nil
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.Data(result.Status, "application/json; charset=utf-8", result.Body)
	return nil
}

func decodeStrictJSON(c *gin.Context, destination any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return routeContractError()
	}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(destination); err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return apperror.PayloadTooLarge("Request body exceeds the permitted size")
		}
		return routeContractError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return routeContractError()
	}
	return nil
}

func requestContentLength(request *http.Request) (int64, error) {
	if request == nil {
		return -1, routeContractError()
	}
	values := request.Header.Values("Content-Length")
	if len(values) > 1 {
		return -1, apperror.Validation("Content-Length is invalid")
	}
	if len(values) == 0 {
		return request.ContentLength, nil
	}
	raw := values[0]
	if raw == "" || strings.IndexFunc(raw, func(character rune) bool {
		return character < '0' || character > '9'
	}) >= 0 {
		return -1, apperror.Validation("Content-Length is invalid")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return -1, apperror.Validation("Content-Length is invalid")
	}
	if request.ContentLength >= 0 && request.ContentLength != value {
		return -1, apperror.Validation("Content-Length does not match the request body")
	}
	return value, nil
}

func routeUUID(value string) (string, error) {
	if !routeUUIDPattern.MatchString(value) {
		return "", routeContractError()
	}
	return value, nil
}

func routeStringLength(value string, minimum, maximum int) bool {
	length := javascriptStringLength(value)
	return length >= minimum && length <= maximum
}

func routeContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

var (
	routeUUIDPattern    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	routeIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)
