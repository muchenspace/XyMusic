package admintagscraping

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

func TestRoutesExposeAllFourteenTagScrapingAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	scraping := &scrapingAPIStub{}
	batches := &batchAPIStub{}
	artistBatches := &artistArtworkBatchAPIStub{}
	identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	idempotency := &scrapingIdempotencyStub{replayed: true}
	routes, err := NewRoutes(scraping, batches, artistBatches, identityService, idempotency)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(httpserver.TraceIDMiddleware(func() string { return "trace-scrape-123" }))
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	candidate := `{"id":"song","name":"Song","artist":"Artist","artistId":"artist","album":"Album","albumId":"album","albumImg":"https://y.qq.com/cover.jpg","year":"2020","track":"1","disc":"1","genre":"Rock","source":"qmusic"}`
	artistCandidate := `{"source":"qmusic","id":"artist","name":"Artist","imageUrl":"https://y.qq.com/music/photo_new/T001R500x500M000artist.jpg","aliases":[],"score":2}`
	fields := `{"title":true,"artist":true,"album":true,"year":true,"genre":true,"lyrics":true,"cover":true,"overwrite":false}`
	requests := []struct {
		method, path, body string
		status             int
		idempotent         bool
	}{
		{http.MethodPost, "/api/v1/admin/tag-scraping/search", `{"source":"smart","title":"Song","artist":"Artist"}`, http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/search", `{"source":"smart","query":"Artist"}`, http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/" + id + "/apply", `{"expectedVersion":1,"candidate":` + artistCandidate + `,"overwrite":false,"reason":"operator apply"}`, http.StatusOK, true},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/batches", `{"items":[{"artistId":"` + id + `","expectedVersion":1}],"options":{"sources":["qmusic"],"overwrite":false,"reason":"batch avatar"}}`, http.StatusAccepted, true},
		{http.MethodGet, "/api/v1/admin/tag-scraping/artists/batches/" + id + "?updatedAfter=2026-07-16T01%3A02%3A03Z", "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/batches/" + id + "/cancel", "", http.StatusAccepted, true},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/batches/" + id + "/retry", "", http.StatusAccepted, true},
		{http.MethodPost, "/api/v1/admin/tag-scraping/tracks/" + id + "/fingerprint", "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tag-scraping/tracks/" + id + "/apply", `{"expectedVersion":1,"candidate":` + candidate + `,"fields":` + fields + `,"writeBack":false,"reason":"operator apply"}`, http.StatusOK, true},
		{http.MethodGet, "/api/v1/admin/tag-scraping/artwork?url=https%3A%2F%2Fy.qq.com%2Fcover.jpg", "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tag-scraping/batches", `{"items":[{"trackId":"` + id + `","expectedVersion":1}],"options":{"sources":["qmusic"],"matchMode":"strict","missingFields":["lyrics"],"fields":` + fields + `,"writeBack":false,"reason":"batch apply"}}`, http.StatusAccepted, true},
		{http.MethodGet, "/api/v1/admin/tag-scraping/batches/" + id + "?updatedAfter=2026-07-16T01%3A02%3A03Z", "", http.StatusOK, false},
		{http.MethodPost, "/api/v1/admin/tag-scraping/batches/" + id + "/cancel", "", http.StatusAccepted, true},
		{http.MethodPost, "/api/v1/admin/tag-scraping/batches/" + id + "/retry", "", http.StatusAccepted, true},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer admin")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if item.idempotent {
			request.Header.Set("Idempotency-Key", "request-key-123")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Trace-Id") != "trace-scrape-123" {
			t.Fatalf("trace header = %q", response.Header().Get("X-Trace-Id"))
		}
		if item.idempotent && response.Header().Get("X-Idempotent-Replay") != "true" {
			t.Fatalf("%s replay=%q", item.path, response.Header().Get("X-Idempotent-Replay"))
		}
	}
	if scraping.searchCalls != 1 || scraping.artistSearchCalls != 1 || scraping.fingerprintCalls != 1 ||
		scraping.applyCalls != 1 || scraping.artistApplyCalls != 1 || scraping.artworkCalls != 1 {
		t.Fatalf("scraping calls=%d/%d/%d/%d/%d/%d", scraping.searchCalls, scraping.artistSearchCalls,
			scraping.fingerprintCalls, scraping.applyCalls, scraping.artistApplyCalls, scraping.artworkCalls)
	}
	if batches.createCalls != 1 || batches.jobCalls != 1 || batches.cancelCalls != 1 || batches.retryCalls != 1 {
		t.Fatalf("batch calls=%d/%d/%d/%d", batches.createCalls, batches.jobCalls, batches.cancelCalls, batches.retryCalls)
	}
	if artistBatches.createCalls != 1 || artistBatches.jobCalls != 1 ||
		artistBatches.cancelCalls != 1 || artistBatches.retryCalls != 1 {
		t.Fatalf("artist batch calls=%d/%d/%d/%d", artistBatches.createCalls,
			artistBatches.jobCalls, artistBatches.cancelCalls, artistBatches.retryCalls)
	}
	expectedScopes := []string{
		"admin.tag-scraping.artist-artwork.apply:" + id,
		"admin.tag-scraping.artist-artwork.batch.create",
		"admin.tag-scraping.artist-artwork.batch.cancel:" + id,
		"admin.tag-scraping.artist-artwork.batch.retry:" + id,
		"admin.tag-scraping.apply:" + id,
		"admin.tag-scraping.batch.create",
		"admin.tag-scraping.batch.cancel:" + id,
		"admin.tag-scraping.batch.retry:" + id,
	}
	if !reflect.DeepEqual(idempotency.scopes, expectedScopes) {
		t.Fatalf("idempotency scopes=%#v", idempotency.scopes)
	}
	if identityService.calls != 14 {
		t.Fatalf("authentication calls=%d", identityService.calls)
	}
	if response := scraping.artworkResponse; string(response.Bytes) != "JPEG" {
		t.Fatalf("artwork response=%#v", response)
	}
	if batches.updatedAfter == nil || !batches.updatedAfter.Equal(time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("updatedAfter=%v", batches.updatedAfter)
	}
	if artistBatches.updatedAfter == nil || !artistBatches.updatedAfter.Equal(time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("artist updatedAfter=%v", artistBatches.updatedAfter)
	}
}

func TestRouteContractsRejectMalformedBodiesBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(&scrapingAPIStub{}, &batchAPIStub{}, &artistArtworkBatchAPIStub{}, identityService, &scrapingIdempotencyStub{})
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	invalid := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/admin/tag-scraping/search", `{"source":"invalid","title":"Song"}`},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/search", `{"source":"kugou","query":"Artist"}`},
		{http.MethodPost, "/api/v1/admin/tag-scraping/artists/" + id + "/apply", `{"expectedVersion":1,"candidate":{},"overwrite":false,"reason":"ok"}`},
		{http.MethodPost, "/api/v1/admin/tag-scraping/tracks/not-a-uuid/fingerprint", ""},
		{http.MethodPost, "/api/v1/admin/tag-scraping/tracks/" + id + "/apply", `{"expectedVersion":1,"candidate":{},"fields":{},"reason":"ok"}`},
		{http.MethodGet, "/api/v1/admin/tag-scraping/artwork?url=short", ""},
		{http.MethodPost, "/api/v1/admin/tag-scraping/batches", `{"items":[],"options":{}}`},
		{http.MethodGet, "/api/v1/admin/tag-scraping/batches/" + id + "?updatedAfter=invalid", ""},
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

func TestReadOnlyContractProbesReturnLegacyValidationDetailBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := "00000000-0000-4000-8000-000000000001"
	tests := []struct {
		name, method, path, body string
	}{
		{"search", http.MethodPost, "/api/v1/admin/tag-scraping/search", `{}`},
		{"artist search", http.MethodPost, "/api/v1/admin/tag-scraping/artists/search", `{}`},
		{"artist apply", http.MethodPost, "/api/v1/admin/tag-scraping/artists/" + id + "/apply", `{}`},
		{"apply", http.MethodPost, "/api/v1/admin/tag-scraping/tracks/" + id + "/apply", `{}`},
		{"artwork", http.MethodGet, "/api/v1/admin/tag-scraping/artwork", ""},
		{"create batch", http.MethodPost, "/api/v1/admin/tag-scraping/batches", `{}`},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			scraping := &scrapingAPIStub{}
			batches := &batchAPIStub{}
			identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
			idempotency := &scrapingIdempotencyStub{}
			routes, _ := NewRoutes(scraping, batches, &artistArtworkBatchAPIStub{}, identityService, idempotency)
			engine := gin.New()
			routes.Register(engine)
			request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
			if item.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assertTagScrapingProblem(t, response, http.StatusBadRequest, string(apperror.CodeValidationError), "请求参数不符合接口要求")
			if identityService.calls != 0 || len(idempotency.scopes) != 0 {
				t.Fatalf("authentication/idempotency calls = %d/%d", identityService.calls, len(idempotency.scopes))
			}
			if scraping.searchCalls != 0 || scraping.artistSearchCalls != 0 || scraping.applyCalls != 0 ||
				scraping.artistApplyCalls != 0 || scraping.artworkCalls != 0 || batches.createCalls != 0 {
				t.Fatalf("service calls = %d/%d/%d/%d/%d/%d", scraping.searchCalls, scraping.artistSearchCalls,
					scraping.applyCalls, scraping.artistApplyCalls, scraping.artworkCalls, batches.createCalls)
			}
		})
	}
}

func TestTagScrapingJSONContractPreservesSchemaParseAndPayloadErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newEngine := func(t *testing.T) (*gin.Engine, *scrapingIdentityStub) {
		t.Helper()
		identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
		routes, _ := NewRoutes(&scrapingAPIStub{}, &batchAPIStub{}, &artistArtworkBatchAPIStub{}, identityService, &scrapingIdempotencyStub{})
		engine := gin.New()
		limiter, err := httpserver.RequestSizeLimiter(httpserver.DefaultRequestLimits())
		if err != nil {
			t.Fatal(err)
		}
		engine.Use(limiter)
		routes.Register(engine)
		return engine, identityService
	}
	tests := []struct {
		name, body, detail string
	}{
		{"wrong JSON type is schema validation", `{"source":1}`, "请求参数不符合接口要求"},
		{"malformed JSON is parse validation", `{`, "请求内容无法解析"},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			engine, identityService := newEngine(t)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tag-scraping/search", bytes.NewBufferString(item.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assertTagScrapingProblem(t, response, http.StatusBadRequest, string(apperror.CodeValidationError), item.detail)
			if identityService.calls != 0 {
				t.Fatalf("authentication calls = %d", identityService.calls)
			}
		})
	}
	t.Run("chunked oversized JSON remains payload too large", func(t *testing.T) {
		engine, identityService := newEngine(t)
		body := bytes.Repeat([]byte{' '}, int(httpserver.MaxStructuredRequestBodyBytes)+1)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tag-scraping/search", bytes.NewReader(body))
		request.ContentLength = -1
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assertTagScrapingProblem(t, response, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge), "提交内容超过允许大小，请缩小后重试。")
		if identityService.calls != 0 {
			t.Fatalf("authentication calls = %d", identityService.calls)
		}
	})
}

func assertTagScrapingProblem(t *testing.T, response *httptest.ResponseRecorder, status int, code, detail string) {
	t.Helper()
	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	decodeJSON(t, response.Body.Bytes(), &problem)
	if response.Code != status || problem.Status != status || problem.Code != code || problem.Detail != detail {
		t.Fatalf("response/problem = %d/%#v", response.Code, problem)
	}
}

func TestRouteQueriesIgnoreUnknownFieldsAndUseLastRepeatedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	scraping := &scrapingAPIStub{}
	batches := &batchAPIStub{}
	identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(scraping, batches, &artistArtworkBatchAPIStub{}, identityService, &scrapingIdempotencyStub{})
	engine := gin.New()
	routes.Register(engine)

	artworkRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/tag-scraping/artwork?url=https%3A%2F%2Fignored.example%2Fold.jpg&unknown=true&url=https%3A%2F%2Fy.qq.com%2Fcover.jpg", nil)
	artworkRequest.Header.Set("Authorization", "Bearer admin")
	artworkResponse := httptest.NewRecorder()
	engine.ServeHTTP(artworkResponse, artworkRequest)
	if artworkResponse.Code != http.StatusOK {
		t.Fatalf("artwork status=%d body=%s", artworkResponse.Code, artworkResponse.Body.String())
	}
	if scraping.artworkURL != "https://y.qq.com/cover.jpg" {
		t.Fatalf("artwork url=%q", scraping.artworkURL)
	}

	id := "00000000-0000-0000-0000-000000000001"
	batchRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/tag-scraping/batches/"+id+"?updatedAfter=invalid&unknown=true&updatedAfter=2026-07-16T01%3A02%3A03Z", nil)
	batchRequest.Header.Set("Authorization", "Bearer admin")
	batchResponse := httptest.NewRecorder()
	engine.ServeHTTP(batchResponse, batchRequest)
	if batchResponse.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", batchResponse.Code, batchResponse.Body.String())
	}
	if batches.updatedAfter == nil || !batches.updatedAfter.Equal(time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("updatedAfter=%v", batches.updatedAfter)
	}
}

func TestUnknownJSONFieldsAreStrippedFromIdempotentPayloads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	idempotency := &scrapingIdempotencyStub{}
	routes, _ := NewRoutes(&scrapingAPIStub{}, &batchAPIStub{}, &artistArtworkBatchAPIStub{}, identityService, idempotency)
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	body := `{"expectedVersion":1,"candidate":{"id":"song","name":"Song","artist":"","artistId":"","album":"","albumId":"","albumImg":"","year":"","track":"","disc":"","genre":"","source":"qmusic","ignoredCandidate":true},"fields":{"title":true,"artist":false,"album":false,"year":false,"genre":false,"lyrics":false,"cover":false,"overwrite":false,"ignoredField":true},"writeBack":false,"reason":"operator apply","ignoredTop":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tag-scraping/tracks/"+id+"/apply", bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-key-123")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(idempotency.payloads) != 1 {
		t.Fatalf("payloads=%#v", idempotency.payloads)
	}
	encoded, err := json.Marshal(idempotency.payloads[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("ignored")) {
		t.Fatalf("unknown fields reached idempotency payload: %s", encoded)
	}
}

type scrapingAPIStub struct {
	searchCalls, artistSearchCalls, fingerprintCalls, applyCalls, artistApplyCalls, artworkCalls int
	artworkResponse                                                                              DownloadedArtwork
	artworkURL                                                                                   string
}

func (stub *scrapingAPIStub) Search(context.Context, SearchInput) ([]Candidate, error) {
	stub.searchCalls++
	return []Candidate{}, nil
}
func (stub *scrapingAPIStub) SearchArtists(context.Context, ArtistSearchInput) ([]ArtistCandidate, error) {
	stub.artistSearchCalls++
	return []ArtistCandidate{}, nil
}
func (stub *scrapingAPIStub) Fingerprint(context.Context, string) ([]Candidate, error) {
	stub.fingerprintCalls++
	return []Candidate{}, nil
}
func (stub *scrapingAPIStub) Apply(context.Context, string, string, string, ApplyInput) (ApplyResult, error) {
	stub.applyCalls++
	return ApplyResult{Warnings: []string{}}, nil
}
func (stub *scrapingAPIStub) ApplyArtistArtwork(
	context.Context,
	string,
	string,
	string,
	ArtistArtworkApplyInput,
) (ArtistArtworkApplyResult, error) {
	stub.artistApplyCalls++
	return ArtistArtworkApplyResult{Applied: true, Version: 2}, nil
}
func (stub *scrapingAPIStub) Artwork(_ context.Context, rawURL string) (DownloadedArtwork, error) {
	stub.artworkCalls++
	stub.artworkURL = rawURL
	stub.artworkResponse = DownloadedArtwork{Bytes: []byte("JPEG"), ContentType: "image/jpeg", Extension: "jpg"}
	return stub.artworkResponse, nil
}

type batchAPIStub struct {
	createCalls, jobCalls, cancelCalls, retryCalls int
	updatedAfter                                   *time.Time
}

func (stub *batchAPIStub) Create(context.Context, string, CreateBatchInput) (BatchJobDTO, error) {
	stub.createCalls++
	return BatchJobDTO{Items: []BatchItemDTO{}}, nil
}
func (stub *batchAPIStub) Job(_ context.Context, _ string, updatedAfter *time.Time) (BatchJobDTO, error) {
	stub.jobCalls++
	stub.updatedAfter = updatedAfter
	return BatchJobDTO{Items: []BatchItemDTO{}}, nil
}
func (stub *batchAPIStub) Cancel(context.Context, string) (BatchJobDTO, error) {
	stub.cancelCalls++
	return BatchJobDTO{Items: []BatchItemDTO{}}, nil
}
func (stub *batchAPIStub) Retry(context.Context, string) (BatchJobDTO, error) {
	stub.retryCalls++
	return BatchJobDTO{Items: []BatchItemDTO{}}, nil
}

type scrapingIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *scrapingIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*scrapingIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*scrapingIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*scrapingIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*scrapingIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}

type scrapingIdempotencyStub struct {
	scopes   []string
	payloads []any
	replayed bool
}

func (stub *scrapingIdempotencyStub) Execute(
	_ context.Context,
	input IdempotencyInput,
	operation func() (IdempotencyResponse, error),
) (IdempotencyResult, error) {
	stub.scopes = append(stub.scopes, input.Scope)
	stub.payloads = append(stub.payloads, input.Payload)
	response, err := operation()
	return IdempotencyResult{Status: response.Status, Body: response.Body, Replayed: stub.replayed}, err
}

func decodeJSON(t *testing.T, raw []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}
