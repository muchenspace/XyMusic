package adminsources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/sse"
)

func TestRoutesExposeAllThirteenLibrarySourceAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &sourceAPIStub{calls: make(map[string]int)}
	identityService := &sourceIdentityStub{actor: identity.AuthenticatedActor{UserID: testRootID, Role: identity.RoleAdmin}}
	idempotency := &sourceIdempotencyStub{}
	events := sse.MustNew(sse.Options{})
	defer events.Close()
	routes, err := NewRoutes(api, identityService, idempotency, events)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)

	requests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/admin/sources/browse?path=ignored&path=music&page=bad&page=2&pageSize=bad&pageSize=100&unknown=x", "", http.StatusOK},
		{http.MethodGet, "/api/v1/admin/sources?page=bad&page=3&pageSize=bad&pageSize=20", "", http.StatusOK},
		{http.MethodPost, "/api/v1/admin/sources", `{"name":"Music","path":"music","mode":"READ_ONLY","enabled":true,"scanOnStartup":false,"scanIntervalMinutes":5e0,"includePatterns":[],"excludePatterns":[],"ignored":true}`, http.StatusCreated},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID, "", http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/sources/" + testRootID, `{"expectedVersion":1.0,"name":"Updated","ignored":true}`, http.StatusOK},
		{http.MethodDelete, "/api/v1/admin/sources/" + testRootID, `{"expectedVersion":1e0,"archiveCatalog":false,"ignored":true}`, http.StatusOK},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/files?page=0&page=1&pageSize=25&status=READY&unknown=x", "", http.StatusOK},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/processing", "", http.StatusOK},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/scans?page=1&pageSize=25", "", http.StatusOK},
		{http.MethodPost, "/api/v1/admin/sources/" + testRootID + "/scans", "", http.StatusAccepted},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/scans/" + testRunID, "", http.StatusOK},
		{http.MethodPost, "/api/v1/admin/sources/" + testRootID + "/scans/" + testRunID + "/cancel", "", http.StatusAccepted},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/scans/" + testRunID + "/events", "", http.StatusOK},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.method == http.MethodPost || item.method == http.MethodPatch || item.method == http.MethodDelete {
			request.Header.Set("Idempotency-Key", "source-test-key")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
	}
	for _, name := range []string{
		"browse", "list", "create", "root", "update", "delete", "files", "processing",
		"runs", "enqueue", "cancel",
	} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls=%d", name, api.calls[name])
		}
	}
	if api.calls["run"] < 2 {
		t.Fatalf("run calls=%d, want GET and SSE polling", api.calls["run"])
	}
	if identityService.calls != 13 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
	if idempotency.calls != 5 {
		t.Fatalf("idempotency calls=%d", idempotency.calls)
	}
	if idempotency.payloadHadUnknown {
		t.Fatal("ignored JSON property entered idempotency payload")
	}
	if api.browsePath != "music" || api.browsePage.Page != 2 || api.browsePage.PageSize != 100 ||
		api.rootPage.Page != 3 || api.rootPage.PageSize != 20 || api.filePage != 1 {
		t.Fatalf("query projection browse=%q/%#v roots=%#v filePage=%d", api.browsePath, api.browsePage, api.rootPage, api.filePage)
	}
}

func TestRouteValidationPrecedesAuthenticationAndKeepsTypeBoxBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &sourceAPIStub{calls: make(map[string]int)}
	identityService := &sourceIdentityStub{actor: identity.AuthenticatedActor{UserID: testRootID, Role: identity.RoleAdmin}}
	events := sse.MustNew(sse.Options{})
	defer events.Close()
	routes, _ := NewRoutes(api, identityService, &sourceIdempotencyStub{}, events)
	engine := gin.New()
	routes.Register(engine)

	requests := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/admin/sources/browse?path=" + strings.Repeat("a", 4001), ""},
		{http.MethodGet, "/api/v1/admin/sources/browse?page=0", ""},
		{http.MethodGet, "/api/v1/admin/sources?pageSize=101", ""},
		{http.MethodGet, "/api/v1/admin/sources/not-a-uuid", ""},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/files?page=0", ""},
		{http.MethodGet, "/api/v1/admin/sources/" + testRootID + "/files?status=INVALID", ""},
		{http.MethodPost, "/api/v1/admin/sources", `{"name":"Music","path":"music","mode":"BAD","enabled":true,"scanOnStartup":false,"includePatterns":[],"excludePatterns":[]}`},
		{http.MethodPatch, "/api/v1/admin/sources/" + testRootID, `{"expectedVersion":0}`},
		{http.MethodDelete, "/api/v1/admin/sources/" + testRootID, `{"expectedVersion":1}`},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
	}
	if identityService.calls != 0 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
}

type sourceAPIStub struct {
	calls      map[string]int
	browsePath string
	browsePage PageQuery
	rootPage   PageQuery
	filePage   int
}

func (stub *sourceAPIStub) Browse(_ context.Context, path string, page PageQuery) (BrowseDTO, error) {
	stub.calls["browse"]++
	stub.browsePath = path
	stub.browsePage = page
	return BrowseDTO{Directories: []DirectoryDTO{}}, nil
}
func (stub *sourceAPIStub) ListRoots(_ context.Context, page PageQuery) (RootListDTO, error) {
	stub.calls["list"]++
	stub.rootPage = page
	return RootListDTO{Items: []RootDTO{}}, nil
}
func (stub *sourceAPIStub) CreateRoot(context.Context, string, string, CreateRootInput) (RootDTO, error) {
	stub.calls["create"]++
	return RootDTO{IncludePatterns: []string{}, ExcludePatterns: []string{}}, nil
}
func (stub *sourceAPIStub) Root(context.Context, string) (RootDTO, error) {
	stub.calls["root"]++
	return RootDTO{IncludePatterns: []string{}, ExcludePatterns: []string{}}, nil
}
func (stub *sourceAPIStub) UpdateRoot(context.Context, string, string, string, UpdateRootInput) (RootDTO, error) {
	stub.calls["update"]++
	return RootDTO{IncludePatterns: []string{}, ExcludePatterns: []string{}}, nil
}
func (stub *sourceAPIStub) DeleteRoot(context.Context, string, string, string, DeleteRootInput) (DeletedDTO, error) {
	stub.calls["delete"]++
	return DeletedDTO{Deleted: true}, nil
}
func (stub *sourceAPIStub) ListFiles(_ context.Context, _ string, query FileQuery) (SourceFilePageDTO, error) {
	stub.calls["files"]++
	stub.filePage = query.Page
	return SourceFilePageDTO{Items: []SourceFileDTO{}}, nil
}
func (stub *sourceAPIStub) ProcessingSummary(context.Context, string) (ProcessingSummaryDTO, error) {
	stub.calls["processing"]++
	return ProcessingSummaryDTO{Jobs: []ProcessingJobDTO{}}, nil
}
func (stub *sourceAPIStub) ListRuns(context.Context, string, PageQuery) (ScanRunPageDTO, error) {
	stub.calls["runs"]++
	return ScanRunPageDTO{Items: []ScanRunDTO{}}, nil
}
func (stub *sourceAPIStub) EnqueueScan(context.Context, string, string, string) (ScanRunDTO, error) {
	stub.calls["enqueue"]++
	return ScanRunDTO{ID: testRunID, RootID: testRootID, Status: ScanStatusPending}, nil
}
func (stub *sourceAPIStub) ScanRun(context.Context, string, string) (ScanRunDTO, error) {
	stub.calls["run"]++
	return ScanRunDTO{ID: testRunID, RootID: testRootID, Status: ScanStatusCompleted}, nil
}
func (stub *sourceAPIStub) CancelScan(context.Context, string, string, string, string) (CancelledDTO, error) {
	stub.calls["cancel"]++
	return CancelledDTO{Cancelled: true}, nil
}

type sourceIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *sourceIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*sourceIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*sourceIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*sourceIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*sourceIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}

type sourceIdempotencyStub struct {
	calls             int
	payloadHadUnknown bool
}

func (stub *sourceIdempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.calls++
	encoded, _ := json.Marshal(input.Payload)
	stub.payloadHadUnknown = stub.payloadHadUnknown || strings.Contains(string(encoded), "ignored")
	response, err := operation()
	return IdempotencyResult{Status: response.Status, Body: response.Body}, err
}

const (
	testRootID = "00000000-0000-4000-8000-000000000001"
	testRunID  = "00000000-0000-4000-8000-000000000002"
)
