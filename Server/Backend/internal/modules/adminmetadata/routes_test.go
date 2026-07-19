package adminmetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesExposeElevenMetadataEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &metadataAPIStub{calls: make(map[string]int)}
	identityService := &metadataIdentityStub{actor: identity.AuthenticatedActor{
		UserID: "admin-1", Role: identity.RoleAdmin,
	}}
	idempotency := &metadataIdempotencyStub{replayed: true}
	routes, err := NewRoutes(api, identityService, idempotency)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-metadata-123" }))
	routes.Register(engine)
	trackID := "00000000-0000-0000-0000-000000000001"
	revisionID := "00000000-0000-0000-0000-000000000002"
	jobID := "00000000-0000-0000-0000-000000000003"
	requests := []struct {
		method, path, body string
		status             int
		mutation           bool
	}{
		{http.MethodGet, "/api/v1/admin/tracks/" + trackID + "/metadata", "", http.StatusOK, false},
		{http.MethodPatch, "/api/v1/admin/tracks/" + trackID + "/metadata", `{"expectedVersion":1,"patch":{"title":"New title"},"reason":"edit"}`, http.StatusOK, true},
		{http.MethodPost, "/api/v1/admin/metadata/batch", `{"items":[{"trackId":"` + trackID + `","expectedVersion":2}],"patch":{"genres":["Rock"]},"reason":"batch"}`, http.StatusOK, true},
		{http.MethodGet, "/api/v1/admin/tracks/" + trackID + "/metadata/revisions?page=2&pageSize=10", "", http.StatusOK, false},
		{http.MethodGet, "/api/v1/admin/tracks/" + trackID + "/metadata/revisions/" + revisionID, "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tracks/" + trackID + "/metadata/revisions/" + revisionID + "/restore", `{"expectedVersion":3,"reason":"restore"}`, http.StatusOK, true},
		{http.MethodPost, "/api/v1/admin/tracks/" + trackID + "/metadata/writeback", `{"expectedVersion":4,"reason":"write"}`, http.StatusAccepted, true},
		{http.MethodGet, "/api/v1/admin/metadata/writeback-jobs?page=1&pageSize=20&status=FAILED&trackId=" + trackID, "", http.StatusOK, false},
		{http.MethodGet, "/api/v1/admin/metadata/writeback-jobs/" + jobID, "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/metadata/writeback-jobs/" + jobID + "/retry", `{"expectedVersion":2,"reason":"retry"}`, http.StatusAccepted, true},
		{http.MethodPost, "/api/v1/admin/metadata/writeback-jobs/" + jobID + "/cancel", `{"expectedVersion":3,"reason":"cancel"}`, http.StatusAccepted, true},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.mutation {
			request.Header.Set("Idempotency-Key", "metadata-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Trace-Id") != "trace-metadata-123" {
			t.Fatalf("%s trace=%q", item.path, response.Header().Get("X-Trace-Id"))
		}
		if item.mutation && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("%s replay=%q", item.path, response.Header().Get("X-Idempotent-Replay"))
		}
	}
	for _, name := range []string{
		"metadata", "update", "batch", "revisions", "revision", "restore",
		"enqueue", "writebacks", "writeback", "retry", "cancel",
	} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls=%d all=%v", name, api.calls[name], api.calls)
		}
	}
	if identityService.calls != 11 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
	wantScopes := []string{
		"admin.track.metadata.update:" + trackID,
		"admin.track.metadata.batch",
		"admin.track.metadata.restore:" + trackID + ":" + revisionID,
		"admin.track.metadata.writeback:" + trackID,
		"admin.track.metadata.writeback.retry:" + jobID,
		"admin.track.metadata.writeback.cancel:" + jobID,
	}
	if !reflect.DeepEqual(idempotency.scopes, wantScopes) {
		t.Fatalf("scopes=%v", idempotency.scopes)
	}
	if api.revisionPage != 2 || api.revisionPageSize != 10 || api.writebackFilter.Status != WritebackFailed ||
		api.writebackFilter.TrackID != trackID {
		t.Fatalf("queries=%d/%d %+v", api.revisionPage, api.revisionPageSize, api.writebackFilter)
	}
}

func TestRoutesStripUnknownJSONFieldsBeforeIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &metadataAPIStub{calls: make(map[string]int)}
	identityService := &metadataIdentityStub{actor: identity.AuthenticatedActor{
		UserID: "admin-1", Role: identity.RoleAdmin,
	}}
	idempotency := &metadataIdempotencyStub{}
	routes, _ := NewRoutes(api, identityService, idempotency)
	engine := gin.New()
	routes.Register(engine)
	trackID := "00000000-0000-0000-0000-000000000001"
	body := `{
		"expectedVersion":1,
		"patch":{
			"credits":[{"name":"Artist","role":"PRIMARY","nestedUnknown":true}],
			"lyrics":{"content":"text","format":"PLAIN","language":"en","lyricUnknown":true},
			"patchUnknown":"discard"
		},
		"reason":"edit",
		"topUnknown":{"secret":"discard"}
	}`
	request := httptest.NewRequest(
		http.MethodPatch, "/api/v1/admin/tracks/"+trackID+"/metadata", bytes.NewBufferString(body),
	)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "metadata-unknown-1")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(idempotency.payloads) != 1 {
		t.Fatalf("payloads=%v", idempotency.payloads)
	}
	encoded, err := json.Marshal(idempotency.payloads[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, unknown := range []string{"topUnknown", "patchUnknown", "nestedUnknown", "lyricUnknown", "secret"} {
		if bytes.Contains(encoded, []byte(unknown)) {
			t.Fatalf("unknown %q leaked into idempotency payload: %s", unknown, encoded)
		}
	}
	if _, found := api.updateInput.Patch["patchUnknown"]; found {
		t.Fatalf("unknown patch reached service: %#v", api.updateInput.Patch)
	}
}

func TestRoutesRejectInvalidContractsBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &metadataAPIStub{calls: make(map[string]int)}
	identityService := &metadataIdentityStub{actor: identity.AuthenticatedActor{
		UserID: "admin-1", Role: identity.RoleAdmin,
	}}
	routes, _ := NewRoutes(api, identityService, &metadataIdempotencyStub{})
	engine := gin.New()
	routes.Register(engine)
	trackID := "00000000-0000-0000-0000-000000000001"
	invalid := []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/admin/tracks/not-a-uuid/metadata", `{"expectedVersion":1,"patch":{"title":"x"},"reason":"x"}`},
		{http.MethodPatch, "/api/v1/admin/tracks/" + trackID + "/metadata", `{"expectedVersion":0,"patch":{"title":"x"},"reason":"x"}`},
		{http.MethodPatch, "/api/v1/admin/tracks/" + trackID + "/metadata", `{"expectedVersion":1,"patch":{"title":1},"reason":"x"}`},
		{http.MethodPatch, "/api/v1/admin/tracks/" + trackID + "/metadata", `{"expectedVersion":1,"patch":{"title":"x"},"resetFields":null,"reason":"x"}`},
		{http.MethodPost, "/api/v1/admin/metadata/batch", `{"items":[],"patch":{"title":"x"},"reason":"x"}`},
	}
	for _, item := range invalid {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", item.path, response.Code, response.Body.String())
		}
	}
	if identityService.calls != 0 {
		t.Fatalf("invalid requests authenticated %d times", identityService.calls)
	}
}

func TestRoutesIgnoreUnknownQueryAndUseLastDuplicateValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &metadataAPIStub{calls: make(map[string]int)}
	identityService := &metadataIdentityStub{actor: identity.AuthenticatedActor{
		UserID: "admin-1", Role: identity.RoleAdmin,
	}}
	routes, _ := NewRoutes(api, identityService, &metadataIdempotencyStub{})
	engine := gin.New()
	routes.Register(engine)
	trackID := "00000000-0000-0000-0000-000000000001"
	requests := []string{
		"/api/v1/admin/tracks/" + trackID + "/metadata/revisions?page=bad&page=3&pageSize=5&pageSize=7&unknown=true",
		"/api/v1/admin/metadata/writeback-jobs?status=PENDING&status=FAILED&trackId=bad&trackId=" + trackID + "&unknown=true",
	}
	for _, path := range requests {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer admin")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	if api.revisionPage != 3 || api.revisionPageSize != 7 {
		t.Fatalf("revision query=%d/%d", api.revisionPage, api.revisionPageSize)
	}
	if api.writebackFilter.Status != WritebackFailed || api.writebackFilter.TrackID != trackID {
		t.Fatalf("writeback query=%+v", api.writebackFilter)
	}
}

func TestMutationAuthenticatesBeforeIdempotencyKeyValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &metadataAPIStub{calls: make(map[string]int)}
	identityService := &metadataIdentityStub{err: apperror.Unauthorized(
		apperror.CodeAuthenticationRequired, "Authentication is required",
	)}
	idempotency := &metadataIdempotencyStub{}
	routes, _ := NewRoutes(api, identityService, idempotency)
	engine := gin.New()
	routes.Register(engine)
	trackID := "00000000-0000-0000-0000-000000000001"
	body := `{"expectedVersion":1,"patch":{"title":"x"},"reason":"x"}`
	request := httptest.NewRequest(
		http.MethodPatch, "/api/v1/admin/tracks/"+trackID+"/metadata", bytes.NewBufferString(body),
	)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || len(idempotency.scopes) != 0 {
		t.Fatalf("status/scopes=%d/%v body=%s", response.Code, idempotency.scopes, response.Body.String())
	}
}

type metadataAPIStub struct {
	calls                          map[string]int
	updateInput                    MetadataMutationInput
	revisionPage, revisionPageSize int
	writebackFilter                WritebackListInput
}

func (stub *metadataAPIStub) Metadata(context.Context, string) (MetadataDTO, error) {
	stub.calls["metadata"]++
	return MetadataDTO{}, nil
}
func (stub *metadataAPIStub) Update(_ context.Context, _, _, _ string, input MetadataMutationInput) (MetadataDTO, error) {
	stub.calls["update"]++
	stub.updateInput = input
	return MetadataDTO{}, nil
}
func (stub *metadataAPIStub) BatchUpdate(context.Context, string, string, BatchMetadataMutationInput) (BatchUpdateDTO, error) {
	stub.calls["batch"]++
	return BatchUpdateDTO{Items: []BatchUpdateItemDTO{}}, nil
}
func (stub *metadataAPIStub) Revisions(_ context.Context, _ string, page, pageSize int) (RevisionPageDTO, error) {
	stub.calls["revisions"]++
	stub.revisionPage, stub.revisionPageSize = page, pageSize
	return RevisionPageDTO{Items: []RevisionSummaryDTO{}}, nil
}
func (stub *metadataAPIStub) Revision(context.Context, string, string) (RevisionDetailDTO, error) {
	stub.calls["revision"]++
	return RevisionDetailDTO{}, nil
}
func (stub *metadataAPIStub) Restore(context.Context, string, string, string, string, VersionReasonInput) (MetadataDTO, error) {
	stub.calls["restore"]++
	return MetadataDTO{}, nil
}
func (stub *metadataAPIStub) EnqueueWriteback(context.Context, string, string, string, VersionReasonInput) (WritebackJobDTO, error) {
	stub.calls["enqueue"]++
	return WritebackJobDTO{}, nil
}
func (stub *metadataAPIStub) ListWritebacks(_ context.Context, input WritebackListInput) (WritebackJobPageDTO, error) {
	stub.calls["writebacks"]++
	stub.writebackFilter = input
	return WritebackJobPageDTO{Items: []WritebackJobDTO{}}, nil
}
func (stub *metadataAPIStub) WritebackJob(context.Context, string) (WritebackJobDTO, error) {
	stub.calls["writeback"]++
	return WritebackJobDTO{}, nil
}
func (stub *metadataAPIStub) RetryWriteback(context.Context, string, string, string, VersionReasonInput) (WritebackJobDTO, error) {
	stub.calls["retry"]++
	return WritebackJobDTO{}, nil
}
func (stub *metadataAPIStub) CancelWriteback(context.Context, string, string, string, VersionReasonInput) (WritebackJobDTO, error) {
	stub.calls["cancel"]++
	return WritebackJobDTO{}, nil
}

type metadataIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *metadataIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*metadataIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*metadataIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*metadataIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*metadataIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}

type metadataIdempotencyStub struct {
	scopes   []string
	payloads []any
	replayed bool
}

func (stub *metadataIdempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.scopes = append(stub.scopes, input.Scope)
	stub.payloads = append(stub.payloads, input.Payload)
	result, err := operation()
	return IdempotencyResult{
		Status: result.Status, Body: result.Body, Replayed: stub.replayed,
	}, err
}
