package adminsettings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type API interface {
	Settings() (SettingsDTO, error)
	TestDatabase(context.Context, DatabaseInput) (TestResponse, error)
	TestStorage(context.Context, StorageInput) (StorageTestResponse, error)
	TestMediaTools(context.Context, MediaToolsInput) (TestResponse, error)
	TestLocalLibrary(context.Context, *string) (LocalLibraryTestResponse, error)
	ApplyIdempotently(context.Context, string, string, string, UpdateInput) (IdempotentSettingsResult, error)
	SystemInformation(context.Context) (SystemInformationDTO, error)
}

type Routes struct {
	service  API
	identity adminauth.Identity
}

func NewRoutes(service API, identity adminauth.Identity) (*Routes, error) {
	if service == nil || identity == nil {
		return nil, errors.New("admin settings service and identity are required")
	}
	return &Routes{service: service, identity: identity}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.GET("/settings", httpserver.Handle(routes.settings))
	admin.PATCH("/settings", httpserver.Handle(routes.apply))
	admin.POST("/settings/test/database", httpserver.Handle(routes.testDatabase))
	admin.POST("/settings/test/storage", httpserver.Handle(routes.testStorage))
	admin.POST("/settings/test/media-tools", httpserver.Handle(routes.testMediaTools))
	admin.POST("/settings/test/local-library", httpserver.Handle(routes.testLocalLibrary))
	admin.GET("/system", httpserver.Handle(routes.systemInformation))
}

func (routes *Routes) settings(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.Settings()
	if err != nil {
		return err
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) apply(c *gin.Context) error {
	var input UpdateInput
	if err := decodeContractJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 || !input.hasUpdate() {
		return routeValidationError()
	}
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !idempotencyKey.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	result, err := routes.service.ApplyIdempotently(
		c.Request.Context(), actor.UserID, httpserver.TraceID(c), key, input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", boolText(result.Replayed))
	c.Header("X-Trace-Id", httpserver.TraceID(c))
	c.JSON(result.Status, result.Body)
	return nil
}

func (routes *Routes) testDatabase(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	var input DatabaseInput
	if err := decodeContractJSON(c, &input); err != nil {
		return err
	}
	result, err := routes.service.TestDatabase(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) testStorage(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	var input StorageInput
	if err := decodeContractJSON(c, &input); err != nil {
		return err
	}
	result, err := routes.service.TestStorage(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) testMediaTools(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	var input MediaToolsInput
	if err := decodeContractJSON(c, &input); err != nil {
		return err
	}
	result, err := routes.service.TestMediaTools(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) testLocalLibrary(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, true); err != nil {
		return err
	}
	var input struct {
		Directory *string `json:"directory,omitempty"`
	}
	if err := decodeContractJSON(c, &input); err != nil {
		return err
	}
	result, err := routes.service.TestLocalLibrary(c.Request.Context(), input.Directory)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) systemInformation(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.SystemInformation(c.Request.Context())
	if err != nil {
		return err
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
	return nil
}

func (input UpdateInput) hasUpdate() bool {
	return input.Database != nil || input.Storage != nil || input.MediaTools != nil ||
		input.Scraping != nil || input.LocalLibrary != nil || input.Registration != nil ||
		input.Security != nil || input.HTTP != nil
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func routeValidationError() error {
	return apperror.Validation("请求参数不符合接口要求")
}

func decodeContractJSON(c *gin.Context, destination any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return routeValidationError()
	}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(destination); err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return apperror.PayloadTooLarge("Request body exceeds the permitted size")
		}
		return routeValidationError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return routeValidationError()
	}
	return nil
}

var idempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
