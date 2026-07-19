package parity

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
)

func TestLegacyAndGoAdminMediaAuthenticationAndValidationParity(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	legacyBase := os.Getenv("XYMUSIC_LEGACY_BASE_URL")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run admin media parity")
	}
	absolute, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolute).Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	goRuntime, err := app.Bootstrap(ctx, cfg, app.Options{RootDirectory: filepath.Dir(absolute)})
	if err != nil {
		t.Fatal(err)
	}
	defer goRuntime.Close()
	goServer := httptest.NewServer(goRuntime.Handler)
	defer goServer.Close()
	client := &http.Client{Timeout: 20 * time.Second}
	legacyBase = strings.TrimRight(legacyBase, "/")
	id := "00000000-0000-4000-8000-000000000001"
	checksum := strings.Repeat("a", 64)

	requests := []mediaParityRequest{
		{http.MethodPost, "/api/v1/admin/media/uploads", []byte(`{"purpose":"TRACK_SOURCE","targetId":"` + id + `","fileName":"source.flac","contentType":"audio/flac","sizeBytes":4,"checksumSha256":"` + checksum + `"}`), "application/json"},
		{http.MethodPut, "/api/v1/admin/media/uploads/" + id + "/content", []byte("FLAC"), "audio/flac"},
		{http.MethodPost, "/api/v1/admin/media/uploads/" + id + "/complete", []byte(`{}`), "application/json"},
		{http.MethodGet, "/api/v1/admin/media/jobs/" + id, nil, ""},
		{http.MethodPost, "/api/v1/admin/media/jobs/" + id + "/retry", []byte(`{"expectedVersion":1}`), "application/json"},
	}
	for _, request := range requests {
		legacy := fetchMediaParity(t, client, legacyBase+request.path, request)
		modern := fetchMediaParity(t, client, goServer.URL+request.path, request)
		if legacy.status != modern.status || !semanticJSONEqual(legacy.body, modern.body) {
			t.Fatalf("unauthorized %s %s differs:\nlegacy=%d %s\ngo=%d %s", request.method, request.path, legacy.status, legacy.body, modern.status, modern.body)
		}
	}

	invalid := []mediaParityRequest{
		{http.MethodPost, "/api/v1/admin/media/uploads", []byte(`{"purpose":"TRACK_SOURCE","targetId":"` + id + `","fileName":"source.flac","contentType":"audio/flac","sizeBytes":4,"checksumSha256":"` + checksum + `","unknown":true}`), "application/json"},
		{http.MethodPut, "/api/v1/admin/media/uploads/not-a-uuid/content", []byte("FLAC"), "audio/flac"},
		{http.MethodPost, "/api/v1/admin/media/uploads/not-a-uuid/complete", []byte(`{}`), "application/json"},
		{http.MethodGet, "/api/v1/admin/media/jobs/not-a-uuid", nil, ""},
		{http.MethodPost, "/api/v1/admin/media/jobs/" + id + "/retry", []byte(`{"expectedVersion":0}`), "application/json"},
	}
	for _, request := range invalid {
		legacy := fetchMediaParity(t, client, legacyBase+request.path, request)
		modern := fetchMediaParity(t, client, goServer.URL+request.path, request)
		if legacy.status != modern.status || !semanticJSONEqual(legacy.body, modern.body) {
			t.Fatalf("invalid %s %s differs:\nlegacy=%d %s\ngo=%d %s", request.method, request.path, legacy.status, legacy.body, modern.status, modern.body)
		}
	}
	accessToken := existingAdministratorAccessToken(t, ctx, goRuntime, cfg)
	var jobID string
	err = goRuntime.DB.QueryRow(ctx, "select id from media_jobs order by created_at desc limit 1").Scan(&jobID)
	if err == nil {
		path := "/api/v1/admin/media/jobs/" + jobID
		legacy := fetchAdminSettings(t, client, http.MethodGet, legacyBase+path, accessToken, nil)
		modern := fetchAdminSettings(t, client, http.MethodGet, goServer.URL+path, accessToken, nil)
		assertParityJSON(t, "GET "+path, legacy, modern, nil)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}

type mediaParityRequest struct {
	method      string
	path        string
	body        []byte
	contentType string
}

func fetchMediaParity(t *testing.T, client *http.Client, target string, input mediaParityRequest) response {
	t.Helper()
	request, err := http.NewRequest(input.method, target, bytes.NewReader(input.body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Trace-Id", "admin-media-parity")
	if input.contentType != "" {
		request.Header.Set("Content-Type", input.contentType)
	}
	return executeParityRequest(t, client, request)
}

func executeParityRequest(t *testing.T, client *http.Client, request *http.Request) response {
	t.Helper()
	result, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Body, 8*1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	return response{status: result.StatusCode, body: body}
}
