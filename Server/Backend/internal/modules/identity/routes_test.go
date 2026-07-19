package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
)

type routeIdentityStub struct {
	registered   RegisterInput
	login        LoginInput
	refreshed    bool
	loggedOut    bool
	loggedOutAll bool
}

func (stub *routeIdentityStub) Register(_ context.Context, input RegisterInput) (RegistrationDTO, error) {
	stub.registered = input
	return RegistrationDTO{UserID: "user-id", Username: input.Username, Status: UserStatusActive}, nil
}
func (stub *routeIdentityStub) Login(_ context.Context, input LoginInput) (AuthSessionDTO, error) {
	stub.login = input
	return AuthSessionDTO{Tokens: TokensDTO{TokenType: "Bearer"}}, nil
}
func (stub *routeIdentityStub) Refresh(_ context.Context, token, key string) (RefreshResult, error) {
	stub.refreshed = token == strings.Repeat("x", 32) && key == "request-key-123"
	return RefreshResult{Session: AuthSessionDTO{}, Replayed: true}, nil
}
func (stub *routeIdentityStub) Authenticate(context.Context, string) (AuthenticatedActor, error) {
	return AuthenticatedActor{UserID: "user-id", SessionID: "session-id"}, nil
}
func (stub *routeIdentityStub) Logout(context.Context, AuthenticatedActor) error {
	stub.loggedOut = true
	return nil
}
func (stub *routeIdentityStub) LogoutAll(context.Context, AuthenticatedActor) error {
	stub.loggedOutAll = true
	return nil
}

type routeLimiterStub struct{ calls int }

func (stub *routeLimiterStub) Consume(context.Context, string, int, time.Duration) error {
	stub.calls++
	return nil
}

func TestIdentityRoutesPreserveHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &routeIdentityStub{}
	limiter := &routeLimiterStub{}
	routes := NewRoutes(application, limiter)
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: routes.Register})
	if err != nil {
		t.Fatal(err)
	}

	response := performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/register", `{"username":"listener","password":"password1"}`, nil)
	if response.Code != http.StatusCreated || application.registered.Username != "listener" {
		t.Fatalf("register = %d %s", response.Code, response.Body.String())
	}

	loginBody := `{"username":"listener","password":"password1","device":{"installationId":"00000000-0000-4000-8000-000000000001","name":"Windows","platform":"WINDOWS","appVersion":"1.0"}}`
	response = performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/login", loginBody, nil)
	if response.Code != http.StatusOK || application.login.Device.Platform != DevicePlatformWindows {
		t.Fatalf("login = %d %s", response.Code, response.Body.String())
	}

	response = performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+strings.Repeat("x", 32)+`"}`, map[string]string{"Idempotency-Key": "request-key-123"})
	if response.Code != http.StatusOK || response.Header().Get("X-Idempotent-Replay") != "true" || !application.refreshed {
		t.Fatalf("refresh = %d %s", response.Code, response.Body.String())
	}

	response = performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/logout", "", map[string]string{"Authorization": "Bearer token"})
	if response.Code != http.StatusNoContent || !application.loggedOut {
		t.Fatalf("logout = %d %s", response.Code, response.Body.String())
	}

	response = performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/logout-all", "", map[string]string{"Authorization": "Bearer token"})
	if response.Code != http.StatusNoContent || !application.loggedOutAll {
		t.Fatalf("logout-all = %d %s", response.Code, response.Body.String())
	}
	if limiter.calls != 4 {
		t.Fatalf("expected four limiter calls, got %d", limiter.calls)
	}
}

func TestIdentityLoginRouteAllowsUnknownFieldsButRejectsWebPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &routeIdentityStub{}
	routes := NewRoutes(application, &routeLimiterStub{})
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: routes.Register})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"username":"listener","password":"password1","extra":true,"device":{"installationId":"00000000-0000-4000-8000-000000000001","name":"Windows","platform":"WINDOWS","appVersion":"1.0"}}`
	response := performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/login", body, nil)
	if response.Code != http.StatusOK || application.login.Device.Platform != DevicePlatformWindows {
		t.Fatalf("login with unknown field = %d %s", response.Code, response.Body.String())
	}
	body = `{"username":"listener","password":"password1","device":{"installationId":"00000000-0000-4000-8000-000000000001","name":"Web","platform":"WEB","appVersion":"1.0"}}`
	response = performIdentityRequest(engine, http.MethodPost, "/api/v1/auth/login", body, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("login = %d %s", response.Code, response.Body.String())
	}
}

func performIdentityRequest(engine http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}
