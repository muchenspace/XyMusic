package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type routeAuthenticatorStub struct {
	header string
	calls  int
}

func (stub *routeAuthenticatorStub) Authenticate(
	_ context.Context,
	header string,
) (identity.AuthenticatedActor, error) {
	stub.calls++
	stub.header = header
	if header == "" {
		return identity.AuthenticatedActor{}, apperror.Unauthorized(
			apperror.CodeAuthenticationRequired,
			"Authentication is required",
		)
	}
	return identity.AuthenticatedActor{UserID: "user-1", SessionID: "session-1"}, nil
}

type routeApplicationStub struct {
	updated        UpdateProfileInput
	created        CreateAvatarUploadInput
	completed      CompleteAvatarUploadInput
	uploadID       string
	idempotencyKey string
	traceID        string
}

func (stub *routeApplicationStub) GetCurrentUser(
	context.Context,
	string,
) (identity.CurrentUserDTO, error) {
	return compatibleCurrentUser(), nil
}

func (stub *routeApplicationStub) UpdateCurrentUser(
	_ context.Context,
	_ string,
	key string,
	input UpdateProfileInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	stub.updated = input
	stub.idempotencyKey = key
	return MutationResult[identity.CurrentUserDTO]{Body: compatibleCurrentUser(), Replayed: true}, nil
}

func (stub *routeApplicationStub) CreateAvatarUpload(
	_ context.Context,
	_ string,
	traceID string,
	key string,
	input CreateAvatarUploadInput,
) (MutationResult[AvatarUploadDTO], error) {
	stub.created = input
	stub.idempotencyKey = key
	stub.traceID = traceID
	return MutationResult[AvatarUploadDTO]{Body: AvatarUploadDTO{
		ID:       "upload-1",
		Purpose:  AvatarUploadPurpose,
		TargetID: "user-1",
		Status:   UploadStatusCreated,
		Method:   http.MethodPut,
	}, Replayed: false}, nil
}

func (stub *routeApplicationStub) CompleteAvatarUpload(
	_ context.Context,
	_ string,
	traceID string,
	uploadID string,
	key string,
	input CompleteAvatarUploadInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	stub.completed = input
	stub.uploadID = uploadID
	stub.idempotencyKey = key
	stub.traceID = traceID
	return MutationResult[identity.CurrentUserDTO]{Body: compatibleCurrentUser()}, nil
}

func TestProfileRoutesPreserveFourEndpointContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticator := &routeAuthenticatorStub{}
	application := &routeApplicationStub{}
	routes := NewRoutes(authenticator, application)
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: routes.Register})
	if err != nil {
		t.Fatal(err)
	}

	response := performProfileRequest(engine, http.MethodGet, "/api/v1/users/me", "", map[string]string{
		"Authorization": "Bearer access-token",
	})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"displayName":"Alice"`) {
		t.Fatalf("GET /users/me = %d %s", response.Code, response.Body.String())
	}
	if authenticator.header != "Bearer access-token" {
		t.Fatalf("authorization header = %q", authenticator.header)
	}

	response = performProfileRequest(engine, http.MethodPatch, "/api/v1/users/me", `{
		"expectedVersion":3,"displayName":" Alice ","bio":null
	}`, map[string]string{
		"Authorization":   "Bearer access-token",
		"Idempotency-Key": "profile-update-1",
	})
	if response.Code != http.StatusOK || response.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatalf("PATCH /users/me = %d %s", response.Code, response.Body.String())
	}
	if !application.updated.DisplayName.Set || application.updated.DisplayName.Value != " Alice " ||
		!application.updated.Bio.Set || application.updated.Bio.Value != nil ||
		application.idempotencyKey != "profile-update-1" {
		t.Fatalf("unexpected update input: %#v", application.updated)
	}

	response = performProfileRequest(engine, http.MethodPost, "/api/v1/users/me/avatar/uploads", `{
		"fileName":"avatar.png","contentType":"image/png","sizeBytes":128,
		"checksumSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}`, map[string]string{
		"Authorization":   "Bearer access-token",
		"Idempotency-Key": "avatar-create-1",
		"X-Trace-Id":      "profile-trace-1",
	})
	if response.Code != http.StatusCreated || response.Header().Get("X-Idempotent-Replay") != "false" {
		t.Fatalf("POST avatar upload = %d %s", response.Code, response.Body.String())
	}
	if application.created.ContentType != "image/png" || application.traceID != "profile-trace-1" {
		t.Fatalf("unexpected avatar create input: %#v trace=%q", application.created, application.traceID)
	}

	uploadID := "550e8400-e29b-41d4-a716-446655440000"
	response = performProfileRequest(engine, http.MethodPost, "/api/v1/users/me/avatar/uploads/"+uploadID+"/complete", `{
		"observedEtag":"\"etag-1\""
	}`, map[string]string{
		"Authorization":   "Bearer access-token",
		"Idempotency-Key": "avatar-complete-1",
		"X-Trace-Id":      "profile-trace-2",
	})
	if response.Code != http.StatusOK || response.Header().Get("X-Idempotent-Replay") != "false" {
		t.Fatalf("POST avatar complete = %d %s", response.Code, response.Body.String())
	}
	if application.uploadID != uploadID || !application.completed.ObservedETag.Set ||
		application.completed.ObservedETag.Value != `"etag-1"` || application.traceID != "profile-trace-2" {
		t.Fatalf("unexpected completion input: %#v upload=%q", application.completed, application.uploadID)
	}
}

func TestProfileRoutesIgnoreUnknownFieldsLikeLegacyElysia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	application := &routeApplicationStub{}
	routes := NewRoutes(&routeAuthenticatorStub{}, application)
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: routes.Register})
	if err != nil {
		t.Fatal(err)
	}
	response := performProfileRequest(engine, http.MethodPatch, "/api/v1/users/me", `{"expectedVersion":1,"displayName":"Alice","extra":true}`, map[string]string{
		"Authorization":   "Bearer access-token",
		"Idempotency-Key": "request-key-1",
	})
	if response.Code != http.StatusOK || !application.updated.DisplayName.Set || application.updated.DisplayName.Value != "Alice" {
		t.Fatalf("unknown-field request = %d %s input=%#v", response.Code, response.Body.String(), application.updated)
	}
	response = performProfileRequest(engine, http.MethodPatch, "/api/v1/users/me", `{"expectedVersion":1,"displayName":"Alice","extra":true}`, map[string]string{
		"Idempotency-Key": "request-key-2",
	})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown-field unauthenticated request = %d %s", response.Code, response.Body.String())
	}
}

func TestProfileRoutesRejectNullOptionalAndInvalidUploadID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routes := NewRoutes(&routeAuthenticatorStub{}, &routeApplicationStub{})
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: routes.Register})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPatch, "/api/v1/users/me", `{"expectedVersion":1,"displayName":null}`},
		{http.MethodPost, "/api/v1/users/me/avatar/uploads/not-a-uuid/complete", `{}`},
		{http.MethodPost, "/api/v1/users/me/avatar/uploads/550e8400-e29b-41d4-a716-446655440000/complete", `{"observedEtag":null}`},
	}
	for _, test := range tests {
		response := performProfileRequest(engine, test.method, test.path, test.body, map[string]string{
			"Authorization":   "Bearer access-token",
			"Idempotency-Key": "request-key-1",
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s body=%s = %d %s", test.method, test.path, test.body, response.Code, response.Body.String())
		}
	}
}

func TestProfileMutationContractValidationPrecedesAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticator := &routeAuthenticatorStub{}
	routes := NewRoutes(authenticator, &routeApplicationStub{})
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: routes.Register})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPatch, "/api/v1/users/me", `{}`},
		{http.MethodPost, "/api/v1/users/me/avatar/uploads", `{}`},
	} {
		response := performProfileRequest(engine, test.method, test.path, test.body, nil)
		if response.Code != http.StatusBadRequest ||
			response.Header().Get("WWW-Authenticate") != "" ||
			!strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) ||
			!strings.Contains(response.Body.String(), `"detail":"请求参数不符合接口要求"`) {
			t.Fatalf("%s %s = %d headers=%v body=%s", test.method, test.path, response.Code, response.Header(), response.Body.String())
		}
	}
	if authenticator.calls != 0 {
		t.Fatalf("invalid contracts authenticated %d time(s)", authenticator.calls)
	}

	response := performProfileRequest(engine, http.MethodPatch, "/api/v1/users/me", `{"expectedVersion":1,"displayName":"Alice"}`, nil)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("valid unauthenticated request = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if authenticator.calls != 1 {
		t.Fatalf("valid contract authentication calls = %d", authenticator.calls)
	}

	oversized := strings.Repeat("x", int(httpserver.MaxStructuredRequestBodyBytes)+1)
	response = performProfileRequest(engine, http.MethodPost, "/api/v1/users/me/avatar/uploads", oversized, nil)
	if response.Code != http.StatusRequestEntityTooLarge || response.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("oversized request = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if authenticator.calls != 1 {
		t.Fatalf("oversized request reached authentication: calls=%d", authenticator.calls)
	}
}

func compatibleCurrentUser() identity.CurrentUserDTO {
	return identity.CurrentUserDTO{
		ID:          "user-1",
		Username:    "alice",
		DisplayName: "Alice",
		Bio:         nil,
		Avatar:      nil,
		Role:        identity.RoleUser,
		Status:      identity.UserStatusActive,
		Version:     3,
		CreatedAt:   "2026-07-16T00:00:00.000Z",
		UpdatedAt:   "2026-07-16T01:00:00.000Z",
	}
}

func performProfileRequest(
	engine http.Handler,
	method string,
	path string,
	body string,
	headers map[string]string,
) *httptest.ResponseRecorder {
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
