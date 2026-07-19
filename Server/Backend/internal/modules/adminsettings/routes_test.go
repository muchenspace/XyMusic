package adminsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/runtimemetrics"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesExposeSevenSystemSettingsEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &settingsAPIStub{}
	identityService := &settingsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	routes, err := NewRoutes(api, identityService)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	requests := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/admin/settings", ""},
		{http.MethodPatch, "/api/v1/admin/settings", `{"expectedVersion":1,"registration":{"enabled":true}}`},
		{http.MethodPost, "/api/v1/admin/settings/test/database", `{}`},
		{http.MethodPost, "/api/v1/admin/settings/test/storage", `{}`},
		{http.MethodPost, "/api/v1/admin/settings/test/media-tools", `{}`},
		{http.MethodPost, "/api/v1/admin/settings/test/local-library", `{}`},
		{http.MethodGet, "/api/v1/admin/system", ""},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.method == http.MethodPatch {
			request.Header.Set("Idempotency-Key", "settings-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", item.method, item.path, response.Code, response.Body.String())
		}
		if item.method == http.MethodPatch && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("settings replay header = %q", response.Header().Get("X-Idempotent-Replay"))
		}
		if item.method == http.MethodGet && (item.path == "/api/v1/admin/settings" || item.path == "/api/v1/admin/system") && response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s cache control = %q", item.path, response.Header().Get("Cache-Control"))
		}
	}
	if api.calls != 7 || identityService.calls != 7 || api.actorID != "admin-1" || api.key != "settings-key-123" {
		t.Fatalf("calls/auth/apply = %d/%d/%q/%q", api.calls, identityService.calls, api.actorID, api.key)
	}
}

func TestSettingsMutationRequiresContractAndIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &settingsAPIStub{}
	identityService := &settingsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(api, identityService)
	engine := gin.New()
	routes.Register(engine)
	for _, body := range []string{
		`{"expectedVersion":0,"registration":{"enabled":true}}`,
		`{"expectedVersion":1}`,
	} {
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/settings", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer admin")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "settings-key-123")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/settings", strings.NewReader(`{"expectedVersion":1,"registration":{"enabled":true}}`))
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || api.calls != 0 {
		t.Fatalf("missing key status/calls = %d/%d body=%s", response.Code, api.calls, response.Body.String())
	}
	unknown := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/admin/settings",
		strings.NewReader(`{"expectedVersion":1,"registration":{"enabled":true},"unknown":true}`),
	)
	unknown.Header.Set("Authorization", "Bearer admin")
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set("Idempotency-Key", "settings-key-unknown")
	unknownResponse := httptest.NewRecorder()
	engine.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusOK || api.calls != 1 {
		t.Fatalf("unknown field status/calls = %d/%d body=%s", unknownResponse.Code, api.calls, unknownResponse.Body.String())
	}
}

func TestSettingsMutationContractValidationPrecedesAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	identityService := &settingsIdentityStub{err: apperror.Unauthorized(
		apperror.CodeAuthenticationRequired,
		"Authentication is required",
	)}
	routes, err := NewRoutes(&settingsAPIStub{}, identityService)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)

	for _, body := range []string{`{}`, `{`} {
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/settings", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest ||
			response.Header().Get("WWW-Authenticate") != "" ||
			!strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) ||
			!strings.Contains(response.Body.String(), `"detail":"请求参数不符合接口要求"`) {
			t.Fatalf("body %q = %d headers=%v response=%s", body, response.Code, response.Header(), response.Body.String())
		}
	}
	if identityService.calls != 0 {
		t.Fatalf("invalid contracts authenticated %d time(s)", identityService.calls)
	}

	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/admin/settings",
		strings.NewReader(`{"expectedVersion":1,"registration":{"enabled":true}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "settings-key-123")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("valid unauthenticated request = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if identityService.calls != 1 {
		t.Fatalf("valid contract authentication calls = %d", identityService.calls)
	}
}

func TestSystemRouteReportsCurrentRequestAndCompletesMetricOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics, err := runtimemetrics.New(runtimemetrics.Options{SampleLimit: 32, SampleInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.Close()
	api := &settingsAPIStub{metrics: metrics}
	routes, err := NewRoutes(
		api,
		&settingsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}},
	)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{
		Metrics: metrics,
		RegisterRoutes: func(engine *gin.Engine) {
			routes.Register(engine)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Metrics runtimemetrics.Snapshot `json:"metrics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Metrics.Requests.Total != 0 || body.Metrics.Requests.InFlight != 1 {
		t.Fatalf("metrics during system request = %+v", body.Metrics.Requests)
	}
	after := metrics.Snapshot()
	if after.Requests.Total != 1 || after.Requests.InFlight != 0 || after.Requests.Sampled != 1 {
		t.Fatalf("metrics after system request = %+v", after.Requests)
	}
}

type settingsAPIStub struct {
	calls   int
	actorID string
	key     string
	metrics RuntimeMetrics
}

func (stub *settingsAPIStub) Settings() (SettingsDTO, error) {
	stub.calls++
	return SettingsDTO{Version: 1}, nil
}
func (stub *settingsAPIStub) TestDatabase(context.Context, DatabaseInput) (TestResponse, error) {
	stub.calls++
	return TestResponse{OK: true}, nil
}
func (stub *settingsAPIStub) TestStorage(context.Context, StorageInput) (StorageTestResponse, error) {
	stub.calls++
	return StorageTestResponse{OK: true}, nil
}
func (stub *settingsAPIStub) TestMediaTools(context.Context, MediaToolsInput) (TestResponse, error) {
	stub.calls++
	return TestResponse{OK: true}, nil
}
func (stub *settingsAPIStub) TestLocalLibrary(context.Context, *string) (LocalLibraryTestResponse, error) {
	stub.calls++
	return LocalLibraryTestResponse{OK: true}, nil
}
func (stub *settingsAPIStub) ApplyIdempotently(_ context.Context, actorID, _, key string, _ UpdateInput) (IdempotentSettingsResult, error) {
	stub.calls++
	stub.actorID = actorID
	stub.key = key
	return IdempotentSettingsResult{Status: http.StatusOK, Body: SettingsDTO{Version: 2}, Replayed: true}, nil
}
func (stub *settingsAPIStub) SystemInformation(context.Context) (SystemInformationDTO, error) {
	stub.calls++
	result := SystemInformationDTO{}
	if stub.metrics != nil {
		result.Metrics = stub.metrics.Snapshot()
	}
	return result, nil
}

type settingsIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *settingsIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*settingsIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, nil
}
func (*settingsIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, nil
}
func (*settingsIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error { return nil }
func (*settingsIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, nil
}
