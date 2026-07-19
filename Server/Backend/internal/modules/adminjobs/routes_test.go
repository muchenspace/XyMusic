package adminjobs

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/sse"
)

func TestRoutesExposeJobQueriesAndIdempotentMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &jobsAPIStub{}
	identityService := &jobsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	idempotency := &jobsIdempotencyStub{replayed: true}
	broadcaster := sse.MustNew(sse.Options{MaxTopics: 1})
	defer broadcaster.Close()
	routes, err := NewRoutes(api, identityService, idempotency, broadcaster)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-jobs-123" }))
	routes.Register(engine)
	jobID := "00000000-0000-0000-0000-000000000001"
	requests := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/admin/jobs?page=2&pageSize=10&search=track&status=FAILED&type=MEDIA_PROCESS&sort=updatedAt&order=asc", ""},
		{http.MethodGet, "/api/v1/admin/jobs/" + jobID, ""},
		{http.MethodPost, "/api/v1/admin/jobs/" + jobID + "/retry", ""},
		{http.MethodPost, "/api/v1/admin/jobs/" + jobID + "/cancel", `{"reason":"stop"}`},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.method == http.MethodPost {
			request.Header.Set("Idempotency-Key", "request-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Trace-Id") != "trace-jobs-123" {
			t.Fatalf("%s %s trace=%q", item.method, item.path, response.Header().Get("X-Trace-Id"))
		}
		if item.method == http.MethodPost && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("%s replay=%q", item.path, response.Header().Get("X-Idempotent-Replay"))
		}
	}
	if api.listCalls != 1 || api.jobCalls != 1 || api.retryCalls != 1 || api.cancelCalls != 1 {
		t.Fatalf("calls=%d/%d/%d/%d", api.listCalls, api.jobCalls, api.retryCalls, api.cancelCalls)
	}
	if api.listInput.Page != 2 || api.listInput.PageSize != 10 || api.listInput.Sort != SortUpdatedAt ||
		api.listInput.Order != SortAscending || api.listInput.Status != JobStatusFailed {
		t.Fatalf("list input=%+v", api.listInput)
	}
	if api.retryReason != nil || api.cancelReason == nil || *api.cancelReason != "stop" {
		t.Fatalf("reasons=%v/%v", api.retryReason, api.cancelReason)
	}
	expectedScopes := []string{"admin.job.retry:" + jobID, "admin.job.cancel:" + jobID}
	if !reflect.DeepEqual(idempotency.scopes, expectedScopes) {
		t.Fatalf("scopes=%v", idempotency.scopes)
	}
	if len(idempotency.payloads) != 2 {
		t.Fatalf("payloads=%v", idempotency.payloads)
	}
	if payload, ok := idempotency.payloads[0].(ReasonInput); !ok || payload.Reason != nil {
		t.Fatalf("retry payload=%#v", idempotency.payloads[0])
	}
	if identityService.calls != 4 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
}

func TestJobsEventsStreamsRetryAndSharedStateFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	updatedAt := "2026-07-16T01:02:03.000Z"
	api := &jobsAPIStub{eventState: EventStateDTO{
		Fingerprint: updatedAt + ":2", UpdatedAt: &updatedAt, Active: 2,
	}}
	identityService := &jobsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin-1", Role: identity.RoleAdmin}}
	broadcaster := sse.MustNew(sse.Options{MaxTopics: 1})
	defer broadcaster.Close()
	routes, _ := NewRoutes(api, identityService, &jobsIdempotencyStub{}, broadcaster)
	engine := gin.New()
	routes.Register(engine)
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/events", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	time.AfterFunc(100*time.Millisecond, cancel)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" ||
		response.Header().Get("Cache-Control") != "no-cache, no-transform" ||
		response.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers=%v", response.Header())
	}
	body := response.Body.String()
	if !strings.Contains(body, "retry: 3000\n\n") ||
		!strings.Contains(body, `data: {"updatedAt":"2026-07-16T01:02:03.000Z","active":2}`) {
		t.Fatalf("SSE body=%q", body)
	}
	if api.eventCalls() != 1 {
		t.Fatalf("event calls=%d", api.eventCalls())
	}
}

func TestRouteContractsFailBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &jobsAPIStub{}
	identityService := &jobsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	broadcaster := sse.MustNew(sse.Options{MaxTopics: 1})
	defer broadcaster.Close()
	routes, _ := NewRoutes(api, identityService, &jobsIdempotencyStub{}, broadcaster)
	engine := gin.New()
	routes.Register(engine)
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs?page=0", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/not-a-uuid", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/00000000-0000-0000-0000-000000000001/retry", strings.NewReader(`{"reason":null}`)),
	}
	for _, request := range requests {
		if request.Body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", request.URL, response.Code, response.Body.String())
		}
	}
	if identityService.calls != 0 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
	unknownQuery := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs?unknown=true", nil)
	unknownQuery.Header.Set("Authorization", "Bearer admin")
	unknownQueryResponse := httptest.NewRecorder()
	engine.ServeHTTP(unknownQueryResponse, unknownQuery)
	if unknownQueryResponse.Code != http.StatusOK {
		t.Fatalf("unknown query status=%d body=%s", unknownQueryResponse.Code, unknownQueryResponse.Body.String())
	}
	unknownBody := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/jobs/00000000-0000-0000-0000-000000000001/retry",
		strings.NewReader(`{"unknown":true}`),
	)
	unknownBody.Header.Set("Authorization", "Bearer admin")
	unknownBody.Header.Set("Content-Type", "application/json")
	unknownBody.Header.Set("Idempotency-Key", "unknown-job-key")
	unknownBodyResponse := httptest.NewRecorder()
	engine.ServeHTTP(unknownBodyResponse, unknownBody)
	if unknownBodyResponse.Code != http.StatusOK {
		t.Fatalf("unknown body status=%d body=%s", unknownBodyResponse.Code, unknownBodyResponse.Body.String())
	}
}

func TestJobListQueryIgnoresUnknownFieldsAndUsesLastRepeatedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &jobsAPIStub{}
	identityService := &jobsIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	broadcaster := sse.MustNew(sse.Options{MaxTopics: 1})
	defer broadcaster.Close()
	routes, _ := NewRoutes(api, identityService, &jobsIdempotencyStub{}, broadcaster)
	engine := gin.New()
	routes.Register(engine)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/jobs?page=invalid&page=3&pageSize=invalid&pageSize=20&status=invalid&status=FAILED&sort=invalid&sort=updatedAt&order=invalid&order=asc&unknown=true",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if api.listInput.Page != 3 || api.listInput.PageSize != 20 || api.listInput.Status != JobStatusFailed ||
		api.listInput.Sort != SortUpdatedAt || api.listInput.Order != SortAscending {
		t.Fatalf("list input=%+v", api.listInput)
	}
}

type jobsAPIStub struct {
	listCalls, jobCalls, retryCalls, cancelCalls int
	listInput                                    ListInput
	retryReason, cancelReason                    *string
	eventState                                   EventStateDTO
	eventMu                                      sync.Mutex
	events                                       int
}

func (stub *jobsAPIStub) List(_ context.Context, input ListInput) (JobPageDTO, error) {
	stub.listCalls++
	stub.listInput = input
	return JobPageDTO{Items: []JobDTO{}}, nil
}

func (stub *jobsAPIStub) Job(context.Context, string) (JobDetailDTO, error) {
	stub.jobCalls++
	return JobDetailDTO{}, nil
}

func (stub *jobsAPIStub) Retry(_ context.Context, _, _, _ string, reason *string) (JobDetailDTO, error) {
	stub.retryCalls++
	stub.retryReason = cloneString(reason)
	return JobDetailDTO{}, nil
}

func (stub *jobsAPIStub) Cancel(_ context.Context, _, _, _ string, reason *string) (JobDetailDTO, error) {
	stub.cancelCalls++
	stub.cancelReason = cloneString(reason)
	return JobDetailDTO{}, nil
}

func (stub *jobsAPIStub) EventState(context.Context) (EventStateDTO, error) {
	stub.eventMu.Lock()
	defer stub.eventMu.Unlock()
	stub.events++
	return stub.eventState, nil
}

func (stub *jobsAPIStub) eventCalls() int {
	stub.eventMu.Lock()
	defer stub.eventMu.Unlock()
	return stub.events
}

type jobsIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *jobsIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}

func (*jobsIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}

func (*jobsIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}

func (*jobsIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}

func (*jobsIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}

type jobsIdempotencyStub struct {
	scopes   []string
	payloads []any
	replayed bool
}

func (stub *jobsIdempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.scopes = append(stub.scopes, input.Scope)
	stub.payloads = append(stub.payloads, input.Payload)
	response, err := operation()
	return IdempotencyResult{Status: response.Status, Body: response.Body, Replayed: stub.replayed}, err
}
