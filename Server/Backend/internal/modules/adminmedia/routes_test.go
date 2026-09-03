package adminmedia

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

type mediaAuthStub struct {
	actor identity.AuthenticatedActor
	err   error
}

func (stub *mediaAuthStub) Authenticate(_ context.Context, header string) (identity.AuthenticatedActor, error) {
	if stub.err != nil {
		return identity.AuthenticatedActor{}, stub.err
	}
	if header == "" {
		return identity.AuthenticatedActor{}, apperror.Unauthorized(apperror.CodeAuthenticationRequired, "auth required")
	}
	return stub.actor, nil
}

func (*mediaAuthStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, apperror.Validation("unexpected Login call")
}

func (*mediaAuthStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, apperror.Validation("unexpected Refresh call")
}

func (*mediaAuthStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return apperror.Validation("unexpected Logout call")
}

func (*mediaAuthStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, apperror.Validation("unexpected GetAuthenticatedUser call")
}

type mediaAppStub struct {
	created        CreateUploadInput
	completed      CompleteUploadInput
	directUploaded bool
	uploadID       string
}

func (stub *mediaAppStub) CreateUpload(_ context.Context, _ string, _ string, input CreateUploadInput) (UploadReservationDTO, bool, error) {
	stub.created = input
	return UploadReservationDTO{
		ID:         "upload-1",
		Purpose:    input.Purpose,
		TargetID:   input.TargetID,
		Status:     "CREATED",
		Method:     "PUT",
		UploadURL:  "/api/v1/admin/media/uploads/upload-1/content",
		UploadPath: "/api/v1/admin/media/uploads/upload-1/content",
		ExpiresAt:  "2026-01-01T00:00:00.000Z",
	}, true, nil
}

func (stub *mediaAppStub) UploadDirect(_ context.Context, uploadID string, _ io.Reader, _ int64) error {
	stub.uploadID = uploadID
	stub.directUploaded = true
	return nil
}

func (stub *mediaAppStub) CompleteUpload(_ context.Context, _ string, uploadID string, _ string, input CompleteUploadInput) (UploadCompletionDTO, bool, error) {
	stub.uploadID = uploadID
	stub.completed = input
	return UploadCompletionDTO{
		UploadID: uploadID,
		Status:   "COMPLETED",
		AssetID:  "asset-1",
	}, true, nil
}

func TestRoutesExposeAdminMediaEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &mediaAppStub{}
	auth := &mediaAuthStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	routes := NewRoutes(auth, app)

	engine := gin.New()
	routes.Register(engine)

	checksum := strings.Repeat("a", 64)
	targetID := "00000000-0000-0000-0000-000000000001"

	// 1. POST /api/v1/admin/media/uploads
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/media/uploads",
		strings.NewReader(`{"purpose":"TRACK_SOURCE","targetId":"`+targetID+`","fileName":"source.flac","contentType":"audio/flac","sizeBytes":1024,"checksumSha256":"`+checksum+`"}`),
	)
	createReq.Header.Set("Authorization", "Bearer token")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	engine.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	if !strings.Contains(createRec.Body.String(), `"uploadPath"`) {
		t.Fatalf("missing uploadPath: %s", createRec.Body.String())
	}

	// 2. PUT /api/v1/admin/media/uploads/:id/content
	putReq := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/media/uploads/"+targetID+"/content",
		bytes.NewReader([]byte("raw audio data")),
	)
	putReq.Header.Set("Authorization", "Bearer token")
	putReq.Header.Set("Content-Type", "audio/flac")
	putRec := httptest.NewRecorder()
	engine.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for PUT direct upload, got %d: %s", putRec.Code, putRec.Body.String())
	}
	if !app.directUploaded {
		t.Fatal("direct upload was not called")
	}

	// 3. POST /api/v1/admin/media/uploads/:id/complete
	compReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/media/uploads/"+targetID+"/complete",
		strings.NewReader(`{}`),
	)
	compReq.Header.Set("Authorization", "Bearer token")
	compReq.Header.Set("Content-Type", "application/json")
	compRec := httptest.NewRecorder()
	engine.ServeHTTP(compRec, compReq)

	if compRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for complete, got %d: %s", compRec.Code, compRec.Body.String())
	}
	if !strings.Contains(compRec.Body.String(), `"assetId"`) || strings.Contains(compRec.Body.String(), `"jobId"`) {
		t.Fatalf("unexpected completion body: %s", compRec.Body.String())
	}
}

func TestRoutesUserAvatarUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &mediaAppStub{}
	auth := &mediaAuthStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	routes := NewRoutes(auth, app)

	engine := gin.New()
	routes.Register(engine)

	checksum := strings.Repeat("b", 64)
	targetUserID := "00000000-0000-0000-0000-000000000002"

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/media/uploads",
		strings.NewReader(`{"purpose":"USER_AVATAR","targetId":"`+targetUserID+`","fileName":"avatar.png","contentType":"image/png","sizeBytes":2048,"checksumSha256":"`+checksum+`"}`),
	)
	createReq.Header.Set("Authorization", "Bearer token")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	engine.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	if app.created.Purpose != PurposeUserAvatar || app.created.TargetID != targetUserID {
		t.Fatalf("unexpected app.created: %#v", app.created)
	}
}

func TestRoutesCookieAndCSRFAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &mediaAppStub{}
	auth := &mediaAuthStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	routes := NewRoutes(auth, app)

	engine := gin.New()
	routes.Register(engine)

	checksum := strings.Repeat("c", 64)
	targetUserID := "00000000-0000-0000-0000-000000000002"

	// 1. Success with cookie + CSRF header
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/media/uploads",
		strings.NewReader(`{"purpose":"USER_AVATAR","targetId":"`+targetUserID+`","fileName":"avatar.png","contentType":"image/png","sizeBytes":2048,"checksumSha256":"`+checksum+`"}`),
	)
	req.Header.Set("Cookie", "xymusic_admin_access=token-val; xymusic_admin_csrf=csrf-token-12345678")
	req.Header.Set("X-CSRF-Token", "csrf-token-12345678")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for cookie+csrf, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Reject cookie mutation without CSRF token
	noCsrfReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/media/uploads",
		strings.NewReader(`{"purpose":"USER_AVATAR","targetId":"`+targetUserID+`","fileName":"avatar.png","contentType":"image/png","sizeBytes":2048,"checksumSha256":"`+checksum+`"}`),
	)
	noCsrfReq.Header.Set("Cookie", "xymusic_admin_access=token-val; xymusic_admin_csrf=csrf-token-12345678")
	noCsrfReq.Header.Set("Content-Type", "application/json")
	noCsrfRec := httptest.NewRecorder()
	engine.ServeHTTP(noCsrfRec, noCsrfReq)

	if noCsrfRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing CSRF token, got %d: %s", noCsrfRec.Code, noCsrfRec.Body.String())
	}
}


