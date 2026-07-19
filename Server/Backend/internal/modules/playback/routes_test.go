package playback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
)

func TestCreatePlaybackGrantRouteReturnsServiceGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	trackID := "00000000-0000-4000-8000-000000000001"
	api := &playbackAPIStub{result: GrantDTO{
		TrackID:         trackID,
		VariantID:       "variant-1",
		SelectedQuality: QualityHigh,
		URL:             "https://storage.example/playback",
		ExpiresAt:       "2026-07-16T09:00:00.000Z",
		MimeType:        "audio/flac",
		Codec:           "flac",
		Container:       "flac",
		Bitrate:         320000,
		ContentLength:   12345,
		CacheKey:        "variant-1:checksum",
	}}
	auth := &playbackAuthenticatorStub{}
	routes, err := NewRoutes(api, auth)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: func(engine *gin.Engine) {
		routes.Register(engine)
	}})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tracks/"+trackID+"/playback",
		strings.NewReader(`{"preferredQuality":"HIGH","acceptedCodecs":["FLAC","aac"],"unknown":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer listener-token")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if api.calls != 1 || api.trackID != trackID || api.input.PreferredQuality != QualityHigh ||
		len(api.input.AcceptedCodecs) != 2 || api.input.AcceptedCodecs[0] != "FLAC" || api.input.AcceptedCodecs[1] != "aac" {
		t.Fatalf("service calls/input=%d/%s/%+v", api.calls, api.trackID, api.input)
	}
	if auth.calls != 1 || auth.authorization != "Bearer listener-token" {
		t.Fatalf("auth calls/authorization=%d/%q", auth.calls, auth.authorization)
	}
	var body GrantDTO
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.VariantID != "variant-1" || body.URL != "https://storage.example/playback" || body.CacheKey != "variant-1:checksum" {
		t.Fatalf("body=%+v", body)
	}
}

type playbackAPIStub struct {
	result  GrantDTO
	calls   int
	trackID string
	input   Input
}

func (stub *playbackAPIStub) CreateGrant(_ context.Context, trackID string, input Input) (GrantDTO, error) {
	stub.calls++
	stub.trackID = trackID
	stub.input = input
	return stub.result, nil
}

type playbackAuthenticatorStub struct {
	calls         int
	authorization string
}

func (stub *playbackAuthenticatorStub) Authenticate(_ context.Context, authorization string) error {
	stub.calls++
	stub.authorization = authorization
	return nil
}
