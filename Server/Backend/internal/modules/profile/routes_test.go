package profile

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
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
	directUploaded bool
	uploadID       string
	idempotencyKey string
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
	key string,
	input CreateAvatarUploadInput,
) (MutationResult[AvatarUploadDTO], error) {
	stub.created = input
	stub.idempotencyKey = key
	return MutationResult[AvatarUploadDTO]{
		Body: AvatarUploadDTO{
			ID:         "upload-1",
			Purpose:    "USER_AVATAR",
			TargetID:   "user-1",
			Status:     "CREATED",
			Method:     "PUT",
			UploadURL:  "/api/v1/users/me/avatar/uploads/upload-1",
			UploadPath: "/api/v1/users/me/avatar/uploads/upload-1",
			ExpiresAt:  "2026-01-01T00:00:00.000Z",
		},
		Replayed: false,
	}, nil
}

func (stub *routeApplicationStub) UploadDirect(
	_ context.Context,
	_ string,
	uploadID string,
	_ io.Reader,
	_ int64,
) error {
	stub.uploadID = uploadID
	stub.directUploaded = true
	return nil
}

func (stub *routeApplicationStub) CompleteAvatarUpload(
	_ context.Context,
	_ string,
	uploadID string,
	key string,
	input CompleteAvatarUploadInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	stub.uploadID = uploadID
	stub.idempotencyKey = key
	stub.completed = input
	return MutationResult[identity.CurrentUserDTO]{Body: compatibleCurrentUser(), Replayed: false}, nil
}

func TestProfileRoutesPreserveFourEndpointContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &routeApplicationStub{}
	auth := &routeAuthenticatorStub{}
	routes := NewRoutes(auth, app)

	engine := gin.New()
	routes.Register(engine)

	// Test GET /api/v1/users/me
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer token123")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Test POST /api/v1/users/me/avatar/uploads
	req = httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar/uploads", strings.NewReader(`{
		"fileName": "avatar.png",
		"contentType": "image/png",
		"sizeBytes": 1024,
		"checksumSha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}`))
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"uploadPath"`) {
		t.Fatalf("missing uploadPath in response: %s", rec.Body.String())
	}

	// Test PUT /api/v1/users/me/avatar/uploads/:id
	req = httptest.NewRequest(http.MethodPut, "/api/v1/users/me/avatar/uploads/88888888-4444-4444-4444-121212121212", bytes.NewReader([]byte("raw avatar data")))
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "image/png")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for direct upload, got %d: %s", rec.Code, rec.Body.String())
	}
	if !app.directUploaded || app.uploadID != "88888888-4444-4444-4444-121212121212" {
		t.Fatalf("direct upload was not called: %#v", app)
	}

	// Test POST /api/v1/users/me/avatar/uploads/:id/complete
	req = httptest.NewRequest(http.MethodPost, "/api/v1/users/me/avatar/uploads/88888888-4444-4444-4444-121212121212/complete", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
