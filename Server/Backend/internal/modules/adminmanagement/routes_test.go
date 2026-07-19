package adminmanagement

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesExposeNineEndpointsAndPreserveMutationContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &managementAPIStub{calls: make(map[string]int)}
	identityService := &managementIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	idempotency := &managementIdempotencyStub{replayed: true}
	routes, err := NewRoutes(api, identityService, idempotency)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)

	userID := "00000000-0000-0000-0000-000000000001"
	sessionID := "00000000-0000-0000-0000-000000000002"
	requests := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/v1/admin/dashboard", "", http.StatusOK},
		{http.MethodGet, "/api/v1/admin/users?page=bad&page=1&pageSize=bad&pageSize=25&role=OWNER&role=ADMIN&status=INVALID&status=ACTIVE&unknown=true", "", http.StatusOK},
		{http.MethodPost, "/api/v1/admin/users", `{"username":"Alice_1","password":"secret1","displayName":"Alice","role":"USER","unknown":true}`, http.StatusCreated},
		{http.MethodGet, "/api/v1/admin/users/" + userID + "?page=bad&page=2&pageSize=bad&pageSize=10", "", http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/users/" + userID, `{"expectedVersion":1,"displayName":"Alice","reason":"test","unknown":true}`, http.StatusOK},
		{http.MethodPost, "/api/v1/admin/users/" + userID + "/password", `{"expectedVersion":1,"password":"secret2","reason":"test"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/admin/users/" + userID + "/sessions/" + sessionID + "/revoke", `{"reason":"test"}`, http.StatusOK},
		{http.MethodDelete, "/api/v1/admin/users/" + userID, `{"expectedVersion":1,"reason":"test"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/admin/users/" + userID + "/restore", `{"expectedVersion":2,"reason":"test"}`, http.StatusOK},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.method != http.MethodGet {
			request.Header.Set("Idempotency-Key", "request-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s = %d, body=%s", item.method, item.path, response.Code, response.Body.String())
		}
		if item.method != http.MethodGet && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("%s %s replay=%q", item.method, item.path, response.Header().Get("X-Idempotent-Replay"))
		}
	}
	for _, name := range []string{"dashboard", "list", "create", "user", "update", "password", "session", "delete", "restore"} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls=%d", name, api.calls[name])
		}
	}
	if identityService.calls != 9 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
	if api.sessionPage.Page != 2 || api.sessionPage.PageSize != 10 {
		t.Fatalf("session page=%#v", api.sessionPage)
	}
	expectedScopes := []string{
		"admin.user.create", "admin.user.update:" + userID, "admin.user.password:" + userID,
		"admin.user.session.revoke:" + sessionID, "admin.user.delete:" + userID, "admin.user.restore:" + userID,
	}
	if !reflect.DeepEqual(idempotency.scopes, expectedScopes) {
		t.Fatalf("scopes=%#v", idempotency.scopes)
	}
	if api.deleted.Status.Value != StatusDeleted || api.restored.Status.Value != StatusActive {
		t.Fatalf("delete/restore=%#v/%#v", api.deleted, api.restored)
	}
	if payload, ok := idempotency.payloads[4].(VersionReasonInput); !ok || payload.ExpectedVersion != 1 {
		t.Fatalf("delete idempotency payload=%#v", idempotency.payloads[4])
	}
}

func TestRouteValidationRunsBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &managementAPIStub{calls: make(map[string]int)}
	identityService := &managementIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(api, identityService, &managementIdempotencyStub{})
	engine := gin.New()
	routes.Register(engine)
	for _, target := range []string{
		"/api/v1/admin/users?page=0",
		"/api/v1/admin/users?role=OWNER",
		"/api/v1/admin/users/00000000-0000-0000-0000-000000000001?page=0",
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(`{"username":"bad","password":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || identityService.calls != 0 {
		t.Fatalf("status/auth=%d/%d body=%s", response.Code, identityService.calls, response.Body.String())
	}
}

func TestMutationAuthenticatesBeforeRequiringIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"username":"Alice_1","password":"secret1","displayName":"Alice","role":"USER"}`
	api := &managementAPIStub{calls: make(map[string]int)}
	identityService := &managementIdentityStub{err: apperror.Unauthorized(apperror.CodeAuthenticationRequired, "Authentication is required")}
	idempotency := &managementIdempotencyStub{}
	routes, _ := NewRoutes(api, identityService, idempotency)
	engine := gin.New()
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || len(idempotency.scopes) != 0 {
		t.Fatalf("status/idempotency=%d/%d body=%s", response.Code, len(idempotency.scopes), response.Body.String())
	}

	identityService.err = nil
	identityService.actor = identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || len(idempotency.scopes) != 0 {
		t.Fatalf("missing key status/idempotency=%d/%d body=%s", response.Code, len(idempotency.scopes), response.Body.String())
	}
}

type managementAPIStub struct {
	calls             map[string]int
	deleted, restored UpdateUserInput
	sessionPage       SessionPageInput
}

func (stub *managementAPIStub) Dashboard(context.Context) (DashboardDTO, error) {
	stub.calls["dashboard"]++
	return DashboardDTO{}, nil
}
func (stub *managementAPIStub) ListUsers(context.Context, ListUsersInput) (UserPageDTO, error) {
	stub.calls["list"]++
	return UserPageDTO{Items: []UserDTO{}}, nil
}
func (stub *managementAPIStub) User(_ context.Context, _ string, input SessionPageInput) (UserDetailDTO, error) {
	stub.calls["user"]++
	stub.sessionPage = input
	return UserDetailDTO{Sessions: []SessionDTO{}}, nil
}
func (stub *managementAPIStub) CreateUser(context.Context, string, string, CreateUserInput) (UserDetailDTO, error) {
	stub.calls["create"]++
	return UserDetailDTO{Sessions: []SessionDTO{}}, nil
}
func (stub *managementAPIStub) UpdateUser(_ context.Context, _, _, _ string, input UpdateUserInput) (UserDetailDTO, error) {
	if input.Status.Set && input.Status.Value == StatusDeleted {
		stub.calls["delete"]++
		stub.deleted = input
	} else if input.Status.Set && input.Status.Value == StatusActive {
		stub.calls["restore"]++
		stub.restored = input
	} else {
		stub.calls["update"]++
	}
	return UserDetailDTO{Sessions: []SessionDTO{}}, nil
}
func (stub *managementAPIStub) ResetPassword(context.Context, string, string, string, PasswordInput) (UpdatedDTO, error) {
	stub.calls["password"]++
	return UpdatedDTO{Updated: true}, nil
}
func (stub *managementAPIStub) RevokeSession(context.Context, string, string, string, string, string) (RevokedDTO, error) {
	stub.calls["session"]++
	return RevokedDTO{Revoked: true}, nil
}

type managementIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *managementIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*managementIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*managementIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*managementIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*managementIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}

type managementIdempotencyStub struct {
	scopes   []string
	payloads []any
	replayed bool
}

func (stub *managementIdempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.scopes = append(stub.scopes, input.Scope)
	stub.payloads = append(stub.payloads, input.Payload)
	response, err := operation()
	return IdempotencyResult{Status: response.Status, Body: response.Body, Replayed: stub.replayed}, err
}
