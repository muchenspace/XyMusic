package admintagscraping

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
)

func TestArtistArtworkRouteContractsRejectMalformedInputsBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{
		UserID: "admin", Role: identity.RoleAdmin,
	}}
	routes, _ := NewRoutes(
		&scrapingAPIStub{}, &batchAPIStub{}, &artistArtworkBatchAPIStub{}, identityService, &scrapingIdempotencyStub{},
	)
	engine := gin.New()
	routes.Register(engine)
	artistID := "00000000-0000-0000-0000-000000000001"
	validCandidate := `{"source":"qmusic","id":"artist","name":"Artist","imageUrl":"https://y.qq.com/music/photo_new/T001R500x500M000artist.jpg","aliases":[],"score":2}`
	tests := []struct {
		path string
		body string
	}{
		{"/api/v1/admin/tag-scraping/artists/search", `{"source":"qmusic","query":"Artist","sources":["qmusic"]}`},
		{"/api/v1/admin/tag-scraping/artists/search", `{"source":"smart","query":"Artist","sources":null}`},
		{"/api/v1/admin/tag-scraping/artists/search", `{"source":"kugou","query":"Artist"}`},
		{"/api/v1/admin/tag-scraping/artists/search", `{"source":"smart","query":"   "}`},
		{"/api/v1/admin/tag-scraping/artists/" + artistID + "/apply", `{"expectedVersion":0,"candidate":` + validCandidate + `,"overwrite":false,"reason":"operator"}`},
		{"/api/v1/admin/tag-scraping/artists/" + artistID + "/apply", `{"expectedVersion":1,"candidate":{"source":"qmusic","id":"artist","name":"Artist","imageUrl":"https://music.126.net/artist.jpg","aliases":[],"score":2},"overwrite":false,"reason":"operator"}`},
		{"/api/v1/admin/tag-scraping/artists/" + artistID + "/apply", `{"expectedVersion":1,"candidate":{"source":"qmusic","id":"artist","name":"Artist","imageUrl":"https://y.qq.com/a.jpg","aliases":null,"score":2},"overwrite":false,"reason":"operator"}`},
		{"/api/v1/admin/tag-scraping/artists/" + artistID + "/apply", `{"expectedVersion":1,"candidate":` + validCandidate + `,"overwrite":false,"reason":"x"}`},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
	if identityService.calls != 0 {
		t.Fatalf("malformed artist artwork requests authenticated %d times", identityService.calls)
	}
}

func TestArtistArtworkApplyStripsUnknownFieldsFromIdempotencyPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	identityService := &scrapingIdentityStub{actor: identity.AuthenticatedActor{
		UserID: "admin", Role: identity.RoleAdmin,
	}}
	idempotency := &scrapingIdempotencyStub{}
	scraping := &scrapingAPIStub{}
	routes, _ := NewRoutes(scraping, &batchAPIStub{}, &artistArtworkBatchAPIStub{}, identityService, idempotency)
	engine := gin.New()
	routes.Register(engine)
	artistID := "00000000-0000-0000-0000-000000000001"
	body := `{"expectedVersion":1,"candidate":{"source":"qmusic","id":"artist","name":"Artist","imageUrl":"https://y.qq.com/music/photo_new/T001R500x500M000artist.jpg","aliases":[],"score":2,"ignoredCandidate":true},"overwrite":false,"reason":"operator scrape","ignoredTop":true}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/tag-scraping/artists/"+artistID+"/apply", bytes.NewBufferString(body),
	)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "artist-artwork-key-123")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || scraping.artistApplyCalls != 1 || len(idempotency.payloads) != 1 {
		t.Fatalf("status/calls/payloads = %d/%d/%#v body=%s", response.Code, scraping.artistApplyCalls, idempotency.payloads, response.Body.String())
	}
	encoded, err := json.Marshal(idempotency.payloads[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("ignored")) {
		t.Fatalf("unknown artist artwork fields reached idempotency payload: %s", encoded)
	}
}
