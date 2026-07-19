package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

func TestRegisterRoutesPreservesNineEndpointContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &fakeSetupAPI{}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-setup-routes" }))
	RegisterRoutes(engine, api)
	routes := engine.Routes()
	actual := make([]string, 0, len(routes))
	for _, route := range routes {
		actual = append(actual, route.Method+" "+route.Path)
	}
	expected := []string{
		"GET /api/setup/status",
		"POST /api/setup/http/test",
		"POST /api/setup/paths/test",
		"POST /api/setup/database/test",
		"POST /api/setup/storage/test",
		"POST /api/setup/media/test",
		"POST /api/setup/source/test",
		"POST /api/setup/administrator/test",
		"POST /api/setup/complete",
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("route contract mismatch\nactual:   %#v\nexpected: %#v", actual, expected)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	statusRecorder := httptest.NewRecorder()
	engine.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status route failed: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	input := validSetupInput()
	for path, body := range map[string]any{
		"/api/setup/http/test":          input.HTTP,
		"/api/setup/storage/test":       input.Storage,
		"/api/setup/media/test":         input.Media,
		"/api/setup/source/test":        input.Source,
		"/api/setup/administrator/test": input.Administrator,
	} {
		response := performJSON(engine, path, body)
		if response.Code != http.StatusOK {
			t.Fatalf("%s failed: %d %s", path, response.Code, response.Body.String())
		}
	}

	paths := PathsInput{MigrationsDirectory: "migrations", AdminWebDirectory: "admin"}
	response := performJSON(engine, "/api/setup/paths/test", paths)
	if response.Code != http.StatusOK || !reflect.DeepEqual(api.paths, paths) {
		t.Fatalf("paths route mismatch: status=%d input=%#v", response.Code, api.paths)
	}
	database := DatabaseTestInput{
		Database: DatabaseInput{
			Host: "127.0.0.1", Port: 5432, Database: "xymusic", Username: "xymusic",
			Password: "secret", SSLMode: "disable", MaxConnections: 10,
		},
		MigrationsDirectory: "migrations",
	}
	response = performJSON(engine, "/api/setup/database/test", database)
	if response.Code != http.StatusOK || !reflect.DeepEqual(api.database, database) {
		t.Fatalf("database route mismatch: status=%d input=%#v", response.Code, api.database)
	}

	response = performRawJSON(engine, "/api/setup/paths/test", []byte(`{"migrationsDirectory":"migrations","adminWebDirectory":"admin","unknown":true}`))
	if response.Code != http.StatusOK {
		t.Fatalf("legacy runtime ignores unknown request fields, got %d: %s", response.Code, response.Body.String())
	}
	for _, expectedCall := range []string{"status", "http", "paths", "database", "storage", "media", "source", "administrator"} {
		if !containsString(api.calls, expectedCall) {
			t.Fatalf("route %s was not dispatched: %#v", expectedCall, api.calls)
		}
	}
}

func TestCompleteRouteForwardsTraceIDAndOriginalResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &fakeSetupAPI{}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-generated" }))
	RegisterRoutes(engine, api)
	body, err := json.Marshal(validSetupInput())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Trace-Id", "trace-client-setup")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("complete route failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if api.traceID != "trace-client-setup" {
		t.Fatalf("trace id was not forwarded: %q", api.traceID)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"configured", "runtimeGeneration", "actualListener", "restartRequiredFields"} {
		if _, exists := response[field]; !exists {
			t.Fatalf("completion response is missing %s: %#v", field, response)
		}
	}
}

func TestDatabaseRoutePublishesSpecificSafeProblem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &fakeSetupAPI{databaseErr: databaseConnectionFailure(&pgconn.PgError{Code: "3D000"})}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-database-missing" }))
	RegisterRoutes(engine, api)
	input := validSetupInput()
	response := performJSON(engine, "/api/setup/database/test", DatabaseTestInput{
		Database: input.Database, MigrationsDirectory: input.Paths.MigrationsDirectory,
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != httpserver.ProblemMediaType {
		t.Fatalf("content type = %q, want %q", contentType, httpserver.ProblemMediaType)
	}
	var problem struct {
		Code        string              `json:"code"`
		Detail      string              `json:"detail"`
		Suggestion  string              `json:"suggestion"`
		TraceID     string              `json:"traceId"`
		FieldErrors map[string][]string `json:"fieldErrors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != string(apperror.CodeDatabaseNotFound) ||
		problem.Detail != "数据库名不存在，请确认数据库已经创建且名称填写正确。" ||
		problem.Suggestion == "" || problem.TraceID != "trace-database-missing" {
		t.Fatalf("unexpected database problem: %#v", problem)
	}
	if len(problem.FieldErrors["database"]) != 1 {
		t.Fatalf("database field error missing: %#v", problem.FieldErrors)
	}
	if body := response.Body.String(); strings.Contains(body, input.Database.Password) || strings.Contains(body, "postgresql://") {
		t.Fatalf("database secret leaked in problem response: %s", body)
	}
}

func TestSetupPostRoutesReturnLegacyDetailForMalformedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	paths := []string{
		"/api/setup/http/test",
		"/api/setup/paths/test",
		"/api/setup/database/test",
		"/api/setup/storage/test",
		"/api/setup/media/test",
		"/api/setup/source/test",
		"/api/setup/administrator/test",
		"/api/setup/complete",
	}
	bodies := map[string][]byte{
		"malformed": []byte(`{"value":]}`),
		"truncated": []byte(`{"value":`),
	}

	for _, path := range paths {
		for name, body := range bodies {
			t.Run(path+"/"+name, func(t *testing.T) {
				api := &fakeSetupAPI{requireErr: apperror.Forbidden("Initial setup has already been completed")}
				engine := gin.New()
				engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-setup-invalid-json" }))
				RegisterRoutes(engine, api)

				response := performRawJSON(engine, path, body)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
				}
				if contentType := response.Header().Get("Content-Type"); contentType != httpserver.ProblemMediaType {
					t.Fatalf("content type = %q, want %q", contentType, httpserver.ProblemMediaType)
				}
				var problem httpserver.Problem
				if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if problem.Code != string(apperror.CodeValidationError) {
					t.Fatalf("code = %q, want %q", problem.Code, apperror.CodeValidationError)
				}
				if problem.Detail != "请求内容无法解析" {
					t.Fatalf("detail = %q, want %q", problem.Detail, "请求内容无法解析")
				}
				if api.requireCalls != 0 || len(api.calls) != 0 {
					t.Fatalf("malformed request reached setup service: requireCalls=%d calls=%#v", api.requireCalls, api.calls)
				}
			})
		}
	}
}

func TestRoutesDisableProbesAfterConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &fakeSetupAPI{requireErr: apperror.Forbidden("Initial setup has already been completed")}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-setup-disabled" }))
	RegisterRoutes(engine, api)
	response := performJSON(engine, "/api/setup/http/test", validSetupInput().HTTP)
	if response.Code != http.StatusForbidden {
		t.Fatalf("configured setup probe was not forbidden: %d %s", response.Code, response.Body.String())
	}
	if containsString(api.calls, "http") {
		t.Fatalf("configured route reached the probe service: %#v", api.calls)
	}
}

func performJSON(engine http.Handler, path string, input any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(input)
	return performRawJSON(engine, path, body)
}

func performRawJSON(engine http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

type fakeSetupAPI struct {
	paths        PathsInput
	database     DatabaseTestInput
	traceID      string
	calls        []string
	requireCalls int
	requireErr   error
	databaseErr  error
}

func (api *fakeSetupAPI) Status() StatusResponse {
	api.calls = append(api.calls, "status")
	return StatusResponse{SetupRequired: true, ConfigurationSource: RuntimeSourceSetup}
}
func (api *fakeSetupAPI) RequireSetup() error {
	api.requireCalls++
	return api.requireErr
}
func (api *fakeSetupAPI) TestHTTP(context.Context, HTTPInput) (OKResponse, error) {
	api.calls = append(api.calls, "http")
	return OKResponse{OK: true}, nil
}
func (api *fakeSetupAPI) TestPaths(_ context.Context, input PathsInput) (PathsTestResponse, error) {
	api.calls = append(api.calls, "paths")
	api.paths = input
	return PathsTestResponse{OK: true}, nil
}
func (api *fakeSetupAPI) TestDatabase(_ context.Context, input DatabaseTestInput) (DatabaseTestResponse, error) {
	api.calls = append(api.calls, "database")
	api.database = input
	if api.databaseErr != nil {
		return DatabaseTestResponse{}, api.databaseErr
	}
	return DatabaseTestResponse{OK: true, ServerTimeMS: 1}, nil
}
func (api *fakeSetupAPI) TestStorage(context.Context, StorageInput) (StorageTestResponse, error) {
	api.calls = append(api.calls, "storage")
	return StorageTestResponse{OK: true}, nil
}
func (api *fakeSetupAPI) TestMedia(context.Context, MediaInput) (MediaTestResponse, error) {
	api.calls = append(api.calls, "media")
	return MediaTestResponse{OK: true}, nil
}
func (api *fakeSetupAPI) TestSource(context.Context, SourceInput) (SourceTestResponse, error) {
	api.calls = append(api.calls, "source")
	return SourceTestResponse{OK: true}, nil
}
func (api *fakeSetupAPI) TestAdministrator(context.Context, AdministratorInput) (OKResponse, error) {
	api.calls = append(api.calls, "administrator")
	return OKResponse{OK: true}, nil
}
func (api *fakeSetupAPI) Complete(_ context.Context, _ SetupInput, traceID string) (CompletionResponse, error) {
	api.calls = append(api.calls, "complete")
	api.traceID = traceID
	return CompletionResponse{
		Configured: true, RuntimeGeneration: 1,
		ActualListener: ActualListener{
			IPv4: ListenerAddress{Host: "0.0.0.0", Port: 3000},
			IPv6: ListenerAddress{Host: "::", Port: 3000},
		},
		RestartRequiredFields: []string{},
	}, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
