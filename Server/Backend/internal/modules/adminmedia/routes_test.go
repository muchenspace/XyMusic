package adminmedia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesExposeFiveAdminMediaEndpointsAndContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &mediaAPIStub{calls: make(map[string]int)}
	auth := &mediaIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	idempotency := &mediaIdempotencyStub{replayed: true}
	routes, err := NewRoutes(api, auth, idempotency)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	checksum := stringOf('a', 64)
	requests := []struct {
		method, path, body, contentType string
		status                          int
		idempotent                      bool
	}{
		{http.MethodPost, "/api/v1/admin/media/uploads", `{"purpose":"TRACK_SOURCE","targetId":"` + id + `","fileName":"source.flac","contentType":"audio/flac","sizeBytes":4,"checksumSha256":"` + checksum + `"}`, "application/json", http.StatusCreated, true},
		{http.MethodPut, "/api/v1/admin/media/uploads/" + id + "/content", "FLAC", "audio/flac", http.StatusNoContent, false},
		{http.MethodPost, "/api/v1/admin/media/uploads/" + id + "/complete", `{}`, "application/json", http.StatusAccepted, true},
		{http.MethodGet, "/api/v1/admin/media/jobs/" + id, "", "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/media/jobs/" + id + "/retry", `{"expectedVersion":2,"reason":"operator retry"}`, "application/json", http.StatusAccepted, true},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.contentType != "" {
			request.Header.Set("Content-Type", item.contentType)
		}
		if item.idempotent {
			request.Header.Set("Idempotency-Key", "request-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s = %d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
		if item.idempotent && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("%s %s replay=%q", item.method, item.path, response.Header().Get("X-Idempotent-Replay"))
		}
	}
	if api.calls["create"] != 1 || api.calls["content"] != 1 || api.calls["complete"] != 1 ||
		api.calls["job"] != 1 || api.calls["retry"] != 1 {
		t.Fatalf("calls = %#v", api.calls)
	}
	if api.contentType != "audio/flac" || api.contentLength != 4 || string(api.content) != "FLAC" {
		t.Fatalf("content = %q %d %q", api.contentType, api.contentLength, api.content)
	}
	expectedScopes := []string{
		"admin.media.upload.create",
		"admin.media.upload.complete:" + id,
		"admin.media.job.retry:" + id,
	}
	if !reflect.DeepEqual(idempotency.scopes, expectedScopes) {
		t.Fatalf("scopes = %#v", idempotency.scopes)
	}
	if auth.calls != 5 {
		t.Fatalf("auth calls = %d", auth.calls)
	}
	if payload, ok := idempotency.payloads[1].(map[string]any); !ok || len(payload) != 0 {
		t.Fatalf("complete payload = %#v", idempotency.payloads[1])
	}
	if payload, ok := idempotency.payloads[2].(map[string]any); !ok || payload["expectedVersion"] != 2 {
		t.Fatalf("retry payload = %#v", idempotency.payloads[2])
	}
}

func TestRouteValidationIsStrictAndRunsBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &mediaAPIStub{calls: make(map[string]int)}
	auth := &mediaIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(api, auth, &mediaIdempotencyStub{})
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	checksum := stringOf('a', 64)
	invalid := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/admin/media/uploads/not-a-uuid/complete", `{}`},
		{http.MethodPost, "/api/v1/admin/media/uploads/" + id + "/complete", `{"observedEtag":null}`},
		{http.MethodPost, "/api/v1/admin/media/jobs/" + id + "/retry", `{"expectedVersion":0}`},
	}
	for _, item := range invalid {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", item.path, response.Code, response.Body.String())
		}
	}
	if auth.calls != 0 {
		t.Fatalf("validation authenticated %d times", auth.calls)
	}
	unknown := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/media/uploads",
		bytes.NewBufferString(`{"purpose":"TRACK_SOURCE","targetId":"`+id+`","fileName":"source.flac","contentType":"audio/flac","sizeBytes":4,"checksumSha256":"`+checksum+`","unknown":true}`),
	)
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set("Authorization", "Bearer admin")
	unknown.Header.Set("Idempotency-Key", "request-key-unknown")
	unknownResponse := httptest.NewRecorder()
	engine.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusCreated || auth.calls != 1 {
		t.Fatalf("unknown-field status=%d body=%s", unknownResponse.Code, unknownResponse.Body.String())
	}
}

func TestMutationAuthenticatesBeforeRequiringIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &mediaAPIStub{calls: make(map[string]int)}
	auth := &mediaIdentityStub{err: apperror.Unauthorized(apperror.CodeAuthenticationRequired, "Authentication is required")}
	idempotency := &mediaIdempotencyStub{}
	routes, _ := NewRoutes(api, auth, idempotency)
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	body := `{"expectedVersion":1}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/jobs/"+id+"/retry", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || len(idempotency.scopes) != 0 {
		t.Fatalf("unauthenticated status/scopes = %d/%d body=%s", response.Code, len(idempotency.scopes), response.Body.String())
	}

	auth.err = nil
	auth.actor = identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/jobs/"+id+"/retry", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || len(idempotency.scopes) != 0 {
		t.Fatalf("missing key status/scopes = %d/%d body=%s", response.Code, len(idempotency.scopes), response.Body.String())
	}
}

type mediaAPIStub struct {
	calls         map[string]int
	content       []byte
	contentType   string
	contentLength int64
}

func (stub *mediaAPIStub) CreateUpload(context.Context, string, string, CreateUploadInput) (UploadReservationDTO, error) {
	stub.calls["create"]++
	return UploadReservationDTO{ID: "upload", RequiredHeaders: map[string]string{}}, nil
}
func (stub *mediaAPIStub) UploadContent(_ context.Context, _ string, _ string, contentType string, contentLength int64, body io.Reader) error {
	stub.calls["content"]++
	stub.contentType = contentType
	stub.contentLength = contentLength
	stub.content, _ = io.ReadAll(body)
	return nil
}
func (stub *mediaAPIStub) CompleteUpload(context.Context, string, string, string, CompleteUploadInput) (UploadCompletionDTO, error) {
	stub.calls["complete"]++
	return UploadCompletionDTO{UploadID: "upload", AssetID: "asset", Status: UploadStatusCompleted}, nil
}
func (stub *mediaAPIStub) GetJob(context.Context, string) (MediaJobDTO, error) {
	stub.calls["job"]++
	return MediaJobDTO{ID: "job", Status: JobStatusPending}, nil
}
func (stub *mediaAPIStub) RetryJob(context.Context, string, string, string, RetryJobInput) (MediaJobDTO, error) {
	stub.calls["retry"]++
	return MediaJobDTO{ID: "job", Status: JobStatusPending}, nil
}

type mediaIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *mediaIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*mediaIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*mediaIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*mediaIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*mediaIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}

type mediaIdempotencyStub struct {
	scopes   []string
	payloads []any
	replayed bool
}

func (stub *mediaIdempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.scopes = append(stub.scopes, input.Scope)
	stub.payloads = append(stub.payloads, input.Payload)
	response, err := operation()
	return IdempotencyResult{Status: response.Status, Body: response.Body, Replayed: stub.replayed}, err
}
