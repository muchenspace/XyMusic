package setup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type API interface {
	Status() StatusResponse
	RequireSetup() error
	TestHTTP(context.Context, HTTPInput) (OKResponse, error)
	TestPaths(context.Context, PathsInput) (PathsTestResponse, error)
	TestDatabase(context.Context, DatabaseTestInput) (DatabaseTestResponse, error)
	TestStorage(context.Context, StorageInput) (StorageTestResponse, error)
	TestMedia(context.Context, MediaInput) (MediaTestResponse, error)
	TestSource(context.Context, SourceInput) (SourceTestResponse, error)
	TestAdministrator(context.Context, AdministratorInput) (OKResponse, error)
	Complete(context.Context, SetupInput, string) (CompletionResponse, error)
}

var _ API = (*Service)(nil)

// RegisterRoutes installs the legacy-compatible first-run HTTP contract.
func RegisterRoutes(router gin.IRouter, service API) {
	setup := router.Group("/api/setup")
	setup.GET("/status", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, service.Status())
	})
	setup.POST("/http/test", setupHandler(service.RequireSetup, service.TestHTTP))
	setup.POST("/paths/test", setupHandler(service.RequireSetup, service.TestPaths))
	setup.POST("/database/test", setupHandler(service.RequireSetup, service.TestDatabase))
	setup.POST("/storage/test", setupHandler(service.RequireSetup, service.TestStorage))
	setup.POST("/media/test", setupHandler(service.RequireSetup, service.TestMedia))
	setup.POST("/source/test", setupHandler(service.RequireSetup, service.TestSource))
	setup.POST("/administrator/test", setupHandler(service.RequireSetup, service.TestAdministrator))
	setup.POST("/complete", httpserver.Handle(func(c *gin.Context) error {
		var input SetupInput
		if err := decodeSetupJSON(c, &input); err != nil {
			return err
		}
		if err := service.RequireSetup(); err != nil {
			return err
		}
		response, err := service.Complete(c.Request.Context(), input, httpserver.TraceID(c))
		if err != nil {
			return err
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, response)
		return nil
	}))
}

func setupHandler[Input any, Output any](
	requireSetup func() error,
	operation func(context.Context, Input) (Output, error),
) gin.HandlerFunc {
	return httpserver.Handle(func(c *gin.Context) error {
		var input Input
		if err := decodeSetupJSON(c, &input); err != nil {
			return err
		}
		if err := requireSetup(); err != nil {
			return err
		}
		response, err := operation(c.Request.Context(), input)
		if err != nil {
			return err
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, response)
		return nil
	})
}

func decodeSetupJSON(c *gin.Context, target any) error {
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return apperror.Validation("Setup requests must use application/json")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.MaxStructuredRequestBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(target); err != nil {
		var maximumBytes *http.MaxBytesError
		if errors.As(err, &maximumBytes) {
			return apperror.PayloadTooLarge("Setup request body is too large")
		}
		return apperror.New(
			apperror.CodeValidationError,
			"请求内容无法解析",
			apperror.WithCause(err),
		)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return apperror.Validation("Setup request body must contain one JSON document")
	}
	return nil
}
