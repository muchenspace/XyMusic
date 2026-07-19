package library

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesRegisterAllFiveLibraryEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &libraryAPIStub{calls: make(map[string]int)}
	auth := &libraryAuthStub{userID: "user-1"}
	engine := gin.New()
	routes, err := NewRoutes(api, auth)
	if err != nil {
		t.Fatal(err)
	}
	routes.Register(engine)
	trackID := "00000000-0000-0000-0000-000000000010"
	requests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/library/favorites?sort=INVALID&sort=FAVORITED_DESC&limit=bad&limit=5&cursor=&cursor=favorite-cursor&unknown=true", "", http.StatusOK},
		{http.MethodPut, "/api/v1/library/favorites/" + trackID, "", http.StatusOK},
		{http.MethodDelete, "/api/v1/library/favorites/" + trackID, "", http.StatusNoContent},
		{http.MethodGet, "/api/v1/library/history?limit=bad&limit=6&cursor=&cursor=history-cursor&unknown=true", "", http.StatusOK},
		{http.MethodPut, "/api/v1/library/history/" + trackID, `{
			"playbackSessionId":"00000000-0000-0000-0000-000000000020",
			"positionMs":321,
			"occurredAt":"2026-07-16T08:00:00.000Z",
			"event":"PROGRESS"
		}`, http.StatusOK},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer library-token")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "library-event-key")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s status = %d, body = %s", item.method, item.path, response.Code, response.Body.String())
		}
		if item.method == http.MethodPut && item.body != "" && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("replay header = %q", response.Header().Get("X-Idempotent-Replay"))
		}
	}
	if auth.calls != 5 || auth.lastAuthorization != "Bearer library-token" {
		t.Fatalf("authentication = %d/%q", auth.calls, auth.lastAuthorization)
	}
	for _, name := range []string{"listFavorites", "addFavorite", "removeFavorite", "listHistory", "recordPlayback"} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls = %d", name, api.calls[name])
		}
	}
	if api.favoriteInput.Sort != FavoriteSortFavoritedDesc || api.favoriteInput.Limit == nil || *api.favoriteInput.Limit != 5 {
		t.Fatalf("favorite input = %#v", api.favoriteInput)
	}
	if api.historyInput.Limit == nil || *api.historyInput.Limit != 6 {
		t.Fatalf("history input = %#v", api.historyInput)
	}
	if api.userID != "user-1" || api.trackID != trackID || api.idempotencyKey != "library-event-key" || api.playbackInput.PositionMS != 321 {
		t.Fatalf("route arguments = %#v", api)
	}
}

func TestRecordPlaybackRouteIgnoresUnknownJSONFieldsLikeLegacyRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &libraryAPIStub{calls: make(map[string]int)}
	auth := &libraryAuthStub{userID: "user-1"}
	engine := gin.New()
	routes, _ := NewRoutes(api, auth)
	routes.Register(engine)
	body := `{
		"playbackSessionId":"00000000-0000-0000-0000-000000000020",
		"positionMs":1e2,
		"occurredAt":"2026-07-16T08:00:00.000Z",
		"event":"STARTED",
		"unknown":{"nested":true}
	}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/library/history/00000000-0000-0000-0000-000000000010", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "unknown-field-key")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if api.calls["recordPlayback"] != 1 || api.playbackInput.Event != PlaybackEventStarted || api.playbackInput.PositionMS != 100 {
		t.Fatalf("record call/input = %d/%#v", api.calls["recordPlayback"], api.playbackInput)
	}
}

func TestRoutesRejectInvalidContractBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"favorites sort is required", http.MethodGet, "/api/v1/library/favorites", ""},
		{"favorite track UUID", http.MethodPut, "/api/v1/library/favorites/not-a-uuid", ""},
		{"history cursor cannot be empty", http.MethodGet, "/api/v1/library/history?cursor=", ""},
		{"history rejects an empty object", http.MethodPut, "/api/v1/library/history/00000000-0000-0000-0000-000000000010", `{}`},
		{"history rejects a wrong JSON type", http.MethodPut, "/api/v1/library/history/00000000-0000-0000-0000-000000000010", `{
			"playbackSessionId":1,"positionMs":0,"occurredAt":"2026-07-16T08:00:00Z","event":"STARTED"
		}`},
		{"history rejects fractional position", http.MethodPut, "/api/v1/library/history/00000000-0000-0000-0000-000000000010", `{
			"playbackSessionId":"00000000-0000-0000-0000-000000000020",
			"positionMs":1.5,"occurredAt":"2026-07-16T08:00:00Z","event":"PROGRESS"
		}`},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			api := &libraryAPIStub{calls: make(map[string]int)}
			auth := &libraryAuthStub{userID: "user-1"}
			engine := gin.New()
			routes, _ := NewRoutes(api, auth)
			routes.Register(engine)
			request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
			if item.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if auth.calls != 0 {
				t.Fatalf("authentication calls = %d", auth.calls)
			}
			var problem struct {
				Status int    `json:"status"`
				Code   string `json:"code"`
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.Status != http.StatusBadRequest || problem.Code != string(apperror.CodeValidationError) || problem.Detail != "请求参数不符合接口要求" {
				t.Fatalf("problem = %#v", problem)
			}
		})
	}
}

func TestRecordPlaybackRoutePreservesParseAndPayloadErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newEngine := func(t *testing.T) (*gin.Engine, *libraryAuthStub) {
		t.Helper()
		api := &libraryAPIStub{calls: make(map[string]int)}
		auth := &libraryAuthStub{userID: "user-1"}
		engine := gin.New()
		limiter, err := httpserver.RequestSizeLimiter(httpserver.DefaultRequestLimits())
		if err != nil {
			t.Fatal(err)
		}
		engine.Use(limiter)
		routes, _ := NewRoutes(api, auth)
		routes.Register(engine)
		return engine, auth
	}
	t.Run("malformed JSON remains a parse error", func(t *testing.T) {
		engine, auth := newEngine(t)
		request := httptest.NewRequest(http.MethodPut, "/api/v1/library/history/00000000-0000-0000-0000-000000000010", bytes.NewBufferString(`{`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assertLibraryProblem(t, response, http.StatusBadRequest, string(apperror.CodeValidationError), "请求内容无法解析")
		if auth.calls != 0 {
			t.Fatalf("authentication calls = %d", auth.calls)
		}
	})
	t.Run("chunked oversized JSON remains payload too large", func(t *testing.T) {
		engine, auth := newEngine(t)
		body := bytes.Repeat([]byte{' '}, int(httpserver.MaxStructuredRequestBodyBytes)+1)
		request := httptest.NewRequest(http.MethodPut, "/api/v1/library/history/00000000-0000-0000-0000-000000000010", bytes.NewReader(body))
		request.ContentLength = -1
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assertLibraryProblem(t, response, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge), "提交内容超过允许大小，请缩小后重试。")
		if auth.calls != 0 {
			t.Fatalf("authentication calls = %d", auth.calls)
		}
	})
}

func assertLibraryProblem(t *testing.T, response *httptest.ResponseRecorder, status int, code, detail string) {
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

func TestRoutesPropagateAuthenticationFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &libraryAPIStub{calls: make(map[string]int)}
	auth := &libraryAuthStub{err: apperror.Unauthorized(apperror.CodeAuthenticationRequired, "Authentication is required")}
	engine := gin.New()
	routes, _ := NewRoutes(api, auth)
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/library/favorites?sort=TITLE_ASC", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("status/header = %d/%q, body = %s", response.Code, response.Header().Get("WWW-Authenticate"), response.Body.String())
	}
	if api.calls["listFavorites"] != 0 {
		t.Fatalf("service calls = %d", api.calls["listFavorites"])
	}
}

type libraryAuthStub struct {
	userID            string
	err               error
	calls             int
	lastAuthorization string
}

func (auth *libraryAuthStub) Authenticate(_ context.Context, authorization string) (string, error) {
	auth.calls++
	auth.lastAuthorization = authorization
	return auth.userID, auth.err
}

type libraryAPIStub struct {
	calls map[string]int

	userID         string
	trackID        string
	idempotencyKey string
	favoriteInput  ListFavoritesInput
	historyInput   ListHistoryInput
	playbackInput  RecordPlaybackInput
}

func (api *libraryAPIStub) ListFavorites(_ context.Context, userID string, input ListFavoritesInput) (FavoritePageDTO, error) {
	api.calls["listFavorites"]++
	api.userID = userID
	api.favoriteInput = input
	return FavoritePageDTO{Items: []FavoriteItemDTO{}}, nil
}

func (api *libraryAPIStub) AddFavorite(_ context.Context, userID, trackID string) (FavoriteItemDTO, error) {
	api.calls["addFavorite"]++
	api.userID = userID
	api.trackID = trackID
	return FavoriteItemDTO{Track: catalog.TrackSummaryDTO{ID: trackID, Artists: []catalog.ArtistReferenceDTO{}}}, nil
}

func (api *libraryAPIStub) RemoveFavorite(_ context.Context, userID, trackID string) error {
	api.calls["removeFavorite"]++
	api.userID = userID
	api.trackID = trackID
	return nil
}

func (api *libraryAPIStub) ListHistory(_ context.Context, userID string, input ListHistoryInput) (HistoryPageDTO, error) {
	api.calls["listHistory"]++
	api.userID = userID
	api.historyInput = input
	return HistoryPageDTO{Items: []HistoryItemDTO{}}, nil
}

func (api *libraryAPIStub) RecordPlayback(
	_ context.Context,
	userID, trackID, key string,
	input RecordPlaybackInput,
) (MutationResult[HistoryItemDTO], error) {
	api.calls["recordPlayback"]++
	api.userID = userID
	api.trackID = trackID
	api.idempotencyKey = key
	api.playbackInput = input
	return MutationResult[HistoryItemDTO]{
		Body:     HistoryItemDTO{Track: catalog.TrackSummaryDTO{ID: trackID, Artists: []catalog.ArtistReferenceDTO{}}},
		Replayed: true,
	}, nil
}
