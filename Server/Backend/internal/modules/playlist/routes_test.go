package playlist

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesExposeEightEndpointsWithIdempotencyAndIgnoreUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &playlistAPIStub{calls: make(map[string]int)}
	auth := &playlistAuthStub{userID: "owner-1"}
	idempotency := &idempotencyStub{}
	routes, err := NewRoutes(api, auth, idempotency)
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	engine := gin.New()
	routes.Register(engine)

	playlistID := "00000000-0000-0000-0000-000000000001"
	entryID := "00000000-0000-0000-0000-000000000002"
	trackID := "00000000-0000-0000-0000-000000000003"
	requests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/playlists?sort=INVALID&sort=UPDATED_DESC&limit=bad&limit=5&cursor=&cursor=list-cursor&unknown=true", "", http.StatusOK},
		{http.MethodPost, "/api/v1/playlists", `{"name":"Mix","visibility":"PRIVATE","unknown":true}`, http.StatusCreated},
		{http.MethodGet, "/api/v1/playlists/" + playlistID + "?limit=bad&limit=5&cursor=&cursor=detail-cursor&unknown=true", "", http.StatusOK},
		{http.MethodPatch, "/api/v1/playlists/" + playlistID, `{"expectedVersion":1,"name":"New","unknown":true}`, http.StatusOK},
		{http.MethodDelete, "/api/v1/playlists/" + playlistID + "?expectedVersion=bad&expectedVersion=2&unknown=true", "", http.StatusNoContent},
		{http.MethodPost, "/api/v1/playlists/" + playlistID + "/tracks", `{"expectedVersion":2,"trackId":"` + trackID + `","unknown":true}`, http.StatusCreated},
		{http.MethodDelete, "/api/v1/playlists/" + playlistID + "/tracks/" + entryID + "?expectedVersion=bad&expectedVersion=3&unknown=true", "", http.StatusOK},
		{http.MethodPatch, "/api/v1/playlists/" + playlistID + "/tracks/order", `{"expectedVersion":4,"orderedEntryIds":["` + entryID + `"],"unknown":true}`, http.StatusOK},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer token")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.method != http.MethodGet {
			request.Header.Set("Idempotency-Key", "request-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s = %d, body = %s", item.method, item.path, response.Code, response.Body.String())
		}
		if item.method != http.MethodGet && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("%s %s replay header = %q", item.method, item.path, response.Header().Get("X-Idempotent-Replay"))
		}
	}
	for _, name := range []string{"list", "create", "get", "update", "delete", "add", "remove", "reorder"} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls = %d", name, api.calls[name])
		}
	}
	if auth.calls != 8 {
		t.Fatalf("auth calls = %d", auth.calls)
	}
	expectedScopes := []string{
		"playlist.create",
		"playlist.update:" + playlistID,
		"playlist.delete:" + playlistID,
		"playlist.add-track:" + playlistID,
		"playlist.remove-track:" + playlistID + ":" + entryID,
		"playlist.reorder:" + playlistID,
	}
	if !reflect.DeepEqual(idempotency.scopes, expectedScopes) {
		t.Fatalf("idempotency scopes = %#v", idempotency.scopes)
	}
	for _, payload := range idempotency.payloads {
		if object, ok := payload.(map[string]any); ok {
			if _, exists := object["unknown"]; exists {
				t.Fatalf("unknown field reached idempotency payload: %#v", object)
			}
		}
	}
}

func TestReadOnlyContractProbesReturnLegacyValidationDetailBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	playlistID := "00000000-0000-4000-8000-000000000001"
	entryID := "00000000-0000-4000-8000-000000000003"
	tests := []struct {
		name, method, path, body string
	}{
		{"list", http.MethodGet, "/api/v1/playlists", ""},
		{"create", http.MethodPost, "/api/v1/playlists", `{}`},
		{"update", http.MethodPatch, "/api/v1/playlists/" + playlistID, `{}`},
		{"delete", http.MethodDelete, "/api/v1/playlists/" + playlistID, ""},
		{"add track", http.MethodPost, "/api/v1/playlists/" + playlistID + "/tracks", `{}`},
		{"remove track", http.MethodDelete, "/api/v1/playlists/" + playlistID + "/tracks/" + entryID, ""},
		{"reorder", http.MethodPatch, "/api/v1/playlists/" + playlistID + "/tracks/order", `{}`},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			api := &playlistAPIStub{calls: make(map[string]int)}
			auth := &playlistAuthStub{userID: "owner-1"}
			idempotency := &idempotencyStub{}
			routes, _ := NewRoutes(api, auth, idempotency)
			engine := gin.New()
			routes.Register(engine)
			request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
			if item.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assertPlaylistProblem(t, response, http.StatusBadRequest, string(apperror.CodeValidationError), "请求参数不符合接口要求")
			if auth.calls != 0 || len(idempotency.scopes) != 0 {
				t.Fatalf("authentication/idempotency calls = %d/%d", auth.calls, len(idempotency.scopes))
			}
			for operation, calls := range api.calls {
				if calls != 0 {
					t.Fatalf("service %s calls = %d", operation, calls)
				}
			}
		})
	}
}

func TestPlaylistJSONContractPreservesSchemaParseAndPayloadErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newEngine := func(t *testing.T) (*gin.Engine, *playlistAuthStub) {
		t.Helper()
		api := &playlistAPIStub{calls: make(map[string]int)}
		auth := &playlistAuthStub{userID: "owner-1"}
		routes, _ := NewRoutes(api, auth, &idempotencyStub{})
		engine := gin.New()
		limiter, err := httpserver.RequestSizeLimiter(httpserver.DefaultRequestLimits())
		if err != nil {
			t.Fatal(err)
		}
		engine.Use(limiter)
		routes.Register(engine)
		return engine, auth
	}
	tests := []struct {
		name, body, detail string
	}{
		{"wrong JSON type is schema validation", `{"name":1,"visibility":"PRIVATE"}`, "请求参数不符合接口要求"},
		{"malformed JSON is parse validation", `{`, "请求内容无法解析"},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			engine, auth := newEngine(t)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/playlists", bytes.NewBufferString(item.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assertPlaylistProblem(t, response, http.StatusBadRequest, string(apperror.CodeValidationError), item.detail)
			if auth.calls != 0 {
				t.Fatalf("authentication calls = %d", auth.calls)
			}
		})
	}
	t.Run("chunked oversized JSON remains payload too large", func(t *testing.T) {
		engine, auth := newEngine(t)
		body := bytes.Repeat([]byte{' '}, int(httpserver.MaxStructuredRequestBodyBytes)+1)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/playlists", bytes.NewReader(body))
		request.ContentLength = -1
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assertPlaylistProblem(t, response, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge), "请求内容超过 2 MiB，请缩小后重试")
		if auth.calls != 0 {
			t.Fatalf("authentication calls = %d", auth.calls)
		}
	})
}

func assertPlaylistProblem(t *testing.T, response *httptest.ResponseRecorder, status int, code, detail string) {
	t.Helper()
	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if response.Code != status || problem.Status != status || problem.Code != code || problem.Detail != detail {
		t.Fatalf("response/problem = %d/%#v", response.Code, problem)
	}
}

func TestMutationRequiresIdempotencyKeyAfterAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &playlistAPIStub{calls: make(map[string]int)}
	auth := &playlistAuthStub{userID: "owner-1"}
	idempotency := &idempotencyStub{}
	routes, _ := NewRoutes(api, auth, idempotency)
	engine := gin.New()
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/playlists", bytes.NewBufferString(`{"name":"Mix","visibility":"PRIVATE"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if auth.calls != 1 || len(idempotency.scopes) != 0 || api.calls["create"] != 0 {
		t.Fatalf("auth/idempotency/service = %d/%d/%d", auth.calls, len(idempotency.scopes), api.calls["create"])
	}
}

type playlistAuthStub struct {
	userID string
	err    error
	calls  int
}

func (auth *playlistAuthStub) Authenticate(context.Context, string) (string, error) {
	auth.calls++
	return auth.userID, auth.err
}

type idempotencyStub struct {
	scopes   []string
	payloads []any
}

func (stub *idempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.scopes = append(stub.scopes, input.Scope)
	stub.payloads = append(stub.payloads, input.Payload)
	response, err := operation()
	return IdempotencyResult{Status: response.Status, Body: response.Body, Replayed: true}, err
}

type playlistAPIStub struct {
	calls map[string]int
}

func (api *playlistAPIStub) ListOwned(context.Context, string, ListOwnedInput) (PageDTO, error) {
	api.calls["list"]++
	return PageDTO{Items: []SummaryDTO{}}, nil
}

func (api *playlistAPIStub) Create(context.Context, string, CreateInput) (SummaryDTO, error) {
	api.calls["create"]++
	return SummaryDTO{ID: "playlist"}, nil
}

func (api *playlistAPIStub) Get(context.Context, string, string, GetInput) (DetailDTO, error) {
	api.calls["get"]++
	return DetailDTO{Entries: []EntryDTO{}}, nil
}

func (api *playlistAPIStub) Update(context.Context, string, string, UpdateInput) (SummaryDTO, error) {
	api.calls["update"]++
	return SummaryDTO{ID: "playlist"}, nil
}

func (api *playlistAPIStub) Delete(context.Context, string, string, int) error {
	api.calls["delete"]++
	return nil
}

func (api *playlistAPIStub) AddTrack(context.Context, string, string, AddTrackInput) (AddTrackDTO, error) {
	api.calls["add"]++
	return AddTrackDTO{PlaylistID: "playlist"}, nil
}

func (api *playlistAPIStub) RemoveTrack(context.Context, string, string, string, int) (VersionDTO, error) {
	api.calls["remove"]++
	return VersionDTO{PlaylistID: "playlist"}, nil
}

func (api *playlistAPIStub) Reorder(context.Context, string, string, ReorderInput) (VersionDTO, error) {
	api.calls["reorder"]++
	return VersionDTO{PlaylistID: "playlist"}, nil
}
