package adminauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
)

func TestAdminAuthLoginRouteReturnsAdminSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newAdminAuthHTTPIdentityStub()
	limiter := &adminAuthHTTPLimiterStub{}
	engine := newAdminAuthHTTPServer(t, service, limiter)
	body := `{"username":"administrator","password":"secret","installationId":"00000000-0000-4000-8000-000000000001","deviceName":"Admin browser","unknown":"ignored"}`
	response := performAdminAuthHTTPRequest(engine, http.MethodPost, "/api/v1/admin/auth/login", body, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if service.loginCalls != 1 || service.loginInput.Username != "administrator" || service.loginInput.Password != "secret" ||
		service.loginInput.Device.InstallationID != "00000000-0000-4000-8000-000000000001" ||
		service.loginInput.Device.Name != "Admin browser" || service.loginInput.Device.Platform != identity.DevicePlatformWeb ||
		service.loginInput.Device.AppVersion != "admin-web/1" {
		t.Fatalf("login calls/input=%d/%+v", service.loginCalls, service.loginInput)
	}
	if len(service.authorizations) != 1 || service.authorizations[0] != "Bearer login-access-token" {
		t.Fatalf("authorizations=%v", service.authorizations)
	}
	if len(limiter.calls) != 2 || limiter.calls[0].maximum != 20 || limiter.calls[1].maximum != 10 {
		t.Fatalf("limiter calls=%+v", limiter.calls)
	}
	assertAdminAuthResponse(t, response, "admin-1", "login-access-token", "login-refresh-token")
}

func TestAdminAuthSessionRouteReturnsAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newAdminAuthHTTPIdentityStub()
	service.currentUser.DisplayName = "Current administrator"
	engine := newAdminAuthHTTPServer(t, service, &adminAuthHTTPLimiterStub{})
	csrf := "abcdefghijklmnop-session"
	response := performAdminAuthHTTPRequest(engine, http.MethodGet, "/api/v1/admin/auth/session", "", map[string]string{
		"Authorization": "Bearer session-access-token",
		"Cookie":        CSRFCookie + "=" + csrf,
	})

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(service.authorizations) != 1 || service.authorizations[0] != "Bearer session-access-token" || service.getUserCalls != 1 {
		t.Fatalf("authorizations/get-user=%v/%d", service.authorizations, service.getUserCalls)
	}
	if service.getUserActor.UserID != "admin-1" || service.getUserActor.Role != identity.RoleAdmin {
		t.Fatalf("get-user actor=%+v", service.getUserActor)
	}
	var document struct {
		User      identity.CurrentUserDTO `json:"user"`
		CSRFToken string                  `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.User.ID != "admin-1" || document.User.DisplayName != "Current administrator" || document.CSRFToken != csrf {
		t.Fatalf("document=%+v", document)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", response.Header().Get("Cache-Control"))
	}
}

func TestAdminAuthRefreshRouteReplaysAndRejectsPayloadConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newAdminAuthHTTPIdentityStub()
	limiter := &adminAuthHTTPLimiterStub{}
	engine := newAdminAuthHTTPServer(t, service, limiter)
	csrf := "abcdefghijklmnop-refresh"
	key := "admin-refresh-key-1"
	headers := map[string]string{
		"Cookie":          RefreshCookie + "=original-refresh-token; " + CSRFCookie + "=" + csrf,
		"X-CSRF-Token":    csrf,
		"Idempotency-Key": key,
	}

	first := performAdminAuthHTTPRequest(engine, http.MethodPost, "/api/v1/admin/auth/refresh", `{"unknown":"first"}`, headers)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	assertAdminAuthResponse(t, first, "admin-1", "refreshed-access-token", "refreshed-refresh-token")
	second := performAdminAuthHTTPRequest(engine, http.MethodPost, "/api/v1/admin/auth/refresh", `{"unknown":"second","another":true}`, headers)
	if second.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", second.Code, second.Body.String())
	}
	assertAdminAuthResponse(t, second, "admin-1", "refreshed-access-token", "refreshed-refresh-token")

	conflictHeaders := map[string]string{
		"Cookie":          RefreshCookie + "=different-refresh-token; " + CSRFCookie + "=" + csrf,
		"X-CSRF-Token":    csrf,
		"Idempotency-Key": key,
	}
	conflict := performAdminAuthHTTPRequest(engine, http.MethodPost, "/api/v1/admin/auth/refresh", `{"unknown":"second"}`, conflictHeaders)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), string(apperror.CodeIdempotencyKeyReused)) {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	if service.refreshRotations != 1 || len(service.refreshCalls) != 3 {
		t.Fatalf("refresh rotations/calls=%d/%+v", service.refreshRotations, service.refreshCalls)
	}
	if service.refreshCalls[0].key != key || service.refreshCalls[0].scope != "auth.refresh" || service.refreshCalls[0].replayed ||
		service.refreshCalls[1].key != key || service.refreshCalls[1].scope != "auth.refresh" || !service.refreshCalls[1].replayed {
		t.Fatalf("refresh calls=%+v", service.refreshCalls)
	}
	wantPayload := security.HashSecret("original-refresh-token")
	if service.refreshCalls[0].payloadHash != wantPayload || service.refreshCalls[1].payloadHash != wantPayload ||
		service.refreshCalls[2].payloadHash == wantPayload {
		t.Fatalf("refresh payload hashes=%+v", service.refreshCalls)
	}
	if len(limiter.calls) != 3 || limiter.calls[0].maximum != 60 {
		t.Fatalf("limiter calls=%+v", limiter.calls)
	}
}

func TestAdminAuthLogoutRouteClearsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newAdminAuthHTTPIdentityStub()
	engine := newAdminAuthHTTPServer(t, service, &adminAuthHTTPLimiterStub{})
	response := performAdminAuthHTTPRequest(engine, http.MethodPost, "/api/v1/admin/auth/logout", `{"unknown":true}`, map[string]string{
		"Authorization": "Bearer logout-access-token",
	})

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(service.authorizations) != 1 || service.authorizations[0] != "Bearer logout-access-token" || len(service.logoutActors) != 1 {
		t.Fatalf("authorizations/logout=%v/%+v", service.authorizations, service.logoutActors)
	}
	if service.logoutActors[0].UserID != "admin-1" || service.logoutActors[0].Role != identity.RoleAdmin {
		t.Fatalf("logout actor=%+v", service.logoutActors[0])
	}
	for _, name := range []string{AccessCookie, RefreshCookie, CSRFCookie} {
		cookie := adminAuthResponseCookie(response, name)
		if cookie == nil || cookie.Value != "" || cookie.MaxAge >= 0 {
			t.Fatalf("cleared cookie %s=%+v", name, cookie)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", response.Header().Get("Cache-Control"))
	}
}

type adminAuthHTTPRefreshCall struct {
	token       string
	key         string
	scope       string
	payloadHash string
	replayed    bool
}

type adminAuthHTTPIdentityStub struct {
	actor          identity.AuthenticatedActor
	loginSession   identity.AuthSessionDTO
	refreshSession identity.AuthSessionDTO
	currentUser    identity.CurrentUserDTO

	loginCalls     int
	loginInput     identity.LoginInput
	authorizations []string
	getUserCalls   int
	getUserActor   identity.AuthenticatedActor
	logoutActors   []identity.AuthenticatedActor

	refreshCalls     []adminAuthHTTPRefreshCall
	refreshRotations int
	refreshRecords   map[string]adminAuthHTTPRefreshRecord
}

type adminAuthHTTPRefreshRecord struct {
	payloadHash string
	session     identity.AuthSessionDTO
}

func newAdminAuthHTTPIdentityStub() *adminAuthHTTPIdentityStub {
	actor := identity.AuthenticatedActor{UserID: "admin-1", SessionID: "session-1", Role: identity.RoleAdmin, AuthVersion: 3}
	return &adminAuthHTTPIdentityStub{
		actor:          actor,
		loginSession:   adminAuthHTTPSession("login-access-token", "login-refresh-token"),
		refreshSession: adminAuthHTTPSession("refreshed-access-token", "refreshed-refresh-token"),
		currentUser:    adminAuthHTTPUser(),
		refreshRecords: make(map[string]adminAuthHTTPRefreshRecord),
	}
}

func (stub *adminAuthHTTPIdentityStub) Login(_ context.Context, input identity.LoginInput) (identity.AuthSessionDTO, error) {
	stub.loginCalls++
	stub.loginInput = input
	return stub.loginSession, nil
}

func (stub *adminAuthHTTPIdentityStub) Refresh(_ context.Context, token, key string) (identity.RefreshResult, error) {
	payloadHash := security.HashSecret(token)
	call := adminAuthHTTPRefreshCall{token: token, key: key, scope: "auth.refresh", payloadHash: payloadHash}
	if record, exists := stub.refreshRecords[key]; exists {
		if record.payloadHash != payloadHash {
			stub.refreshCalls = append(stub.refreshCalls, call)
			return identity.RefreshResult{}, apperror.Conflict(apperror.CodeIdempotencyKeyReused, "idempotency key payload changed", nil)
		}
		call.replayed = true
		stub.refreshCalls = append(stub.refreshCalls, call)
		return identity.RefreshResult{Session: record.session, Replayed: true}, nil
	}
	stub.refreshRotations++
	stub.refreshRecords[key] = adminAuthHTTPRefreshRecord{payloadHash: payloadHash, session: stub.refreshSession}
	stub.refreshCalls = append(stub.refreshCalls, call)
	return identity.RefreshResult{Session: stub.refreshSession}, nil
}

func (stub *adminAuthHTTPIdentityStub) Authenticate(_ context.Context, authorization string) (identity.AuthenticatedActor, error) {
	stub.authorizations = append(stub.authorizations, authorization)
	return stub.actor, nil
}

func (stub *adminAuthHTTPIdentityStub) Logout(_ context.Context, actor identity.AuthenticatedActor) error {
	stub.logoutActors = append(stub.logoutActors, actor)
	return nil
}

func (stub *adminAuthHTTPIdentityStub) GetAuthenticatedUser(_ context.Context, actor identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	stub.getUserCalls++
	stub.getUserActor = actor
	return stub.currentUser, nil
}

type adminAuthHTTPLimiterCall struct {
	key     string
	maximum int
	window  time.Duration
}

type adminAuthHTTPLimiterStub struct{ calls []adminAuthHTTPLimiterCall }

func (stub *adminAuthHTTPLimiterStub) Consume(_ context.Context, key string, maximum int, window time.Duration) error {
	stub.calls = append(stub.calls, adminAuthHTTPLimiterCall{key: key, maximum: maximum, window: window})
	return nil
}

func newAdminAuthHTTPServer(t *testing.T, service Identity, limiter *adminAuthHTTPLimiterStub) http.Handler {
	t.Helper()
	routes, err := NewRoutes(service, config.Config{Security: config.Security{
		AccessTokenTTLSeconds:  900,
		RefreshTokenTTLSeconds: 3600,
	}}, limiter)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: func(engine *gin.Engine) {
		routes.Register(engine)
	}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func performAdminAuthHTTPRequest(engine http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
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

func assertAdminAuthResponse(t *testing.T, response *httptest.ResponseRecorder, userID, accessToken, refreshToken string) {
	t.Helper()
	var document struct {
		User      identity.CurrentUserDTO `json:"user"`
		CSRFToken string                  `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.User.ID != userID || len(document.CSRFToken) < 16 {
		t.Fatalf("document=%+v", document)
	}
	access := adminAuthResponseCookie(response, AccessCookie)
	refresh := adminAuthResponseCookie(response, RefreshCookie)
	csrf := adminAuthResponseCookie(response, CSRFCookie)
	if access == nil || access.Value != accessToken || access.Path != "/api/v1/admin" || !access.HttpOnly || access.MaxAge != 900 {
		t.Fatalf("access cookie=%+v", access)
	}
	if refresh == nil || refresh.Value != refreshToken || refresh.Path != "/api/v1/admin/auth/refresh" || !refresh.HttpOnly || refresh.MaxAge != 3600 {
		t.Fatalf("refresh cookie=%+v", refresh)
	}
	if csrf == nil || csrf.Value != document.CSRFToken || csrf.Path != "/" || csrf.HttpOnly || csrf.MaxAge != 3600 {
		t.Fatalf("csrf cookie=%+v", csrf)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", response.Header().Get("Cache-Control"))
	}
}

func adminAuthResponseCookie(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestSessionCookiesSupportSecureCrossSiteRequestsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name     string
		secure   bool
		sameSite http.SameSite
	}{
		{name: "HTTPS allows cross-site credentials", secure: true, sameSite: http.SameSiteNoneMode},
		{name: "HTTP remains strict", secure: false, sameSite: http.SameSiteStrictMode},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			appendSessionCookie(context, AccessCookie, "token", "/api/v1/admin", 900, true, test.secure)
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %#v", cookies)
			}
			cookie := cookies[0]
			if cookie.Secure != test.secure || cookie.SameSite != test.sameSite {
				t.Fatalf("cookie = %+v, want secure=%t sameSite=%v", cookie, test.secure, test.sameSite)
			}
		})
	}
}

func adminAuthHTTPSession(accessToken, refreshToken string) identity.AuthSessionDTO {
	return identity.AuthSessionDTO{
		User:    adminAuthHTTPUser(),
		Session: identity.SessionDTO{ID: "session-1", DeviceName: "Admin browser", CreatedAt: "2026-07-16T08:00:00.000Z"},
		Tokens: identity.TokensDTO{
			TokenType:             "Bearer",
			AccessToken:           accessToken,
			AccessTokenExpiresAt:  "2026-07-16T08:15:00.000Z",
			RefreshToken:          refreshToken,
			RefreshTokenExpiresAt: "2026-07-16T09:00:00.000Z",
		},
	}
}

func adminAuthHTTPUser() identity.CurrentUserDTO {
	return identity.CurrentUserDTO{
		ID:          "admin-1",
		Username:    "administrator",
		DisplayName: "Administrator",
		Role:        identity.RoleAdmin,
		Status:      identity.UserStatusActive,
		Version:     3,
		CreatedAt:   "2026-07-16T07:00:00.000Z",
		UpdatedAt:   "2026-07-16T08:00:00.000Z",
	}
}
