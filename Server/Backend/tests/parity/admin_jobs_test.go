package parity

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/adminjobs"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/security"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
	"xymusic/server/internal/shared/sse"
)

func TestLegacyAndGoAdminJobsAuthenticationAndValidationParity(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	legacyBase := os.Getenv("XYMUSIC_LEGACY_BASE_URL")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run admin jobs parity")
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
	runtime, err := app.Bootstrap(ctx, cfg, app.Options{RootDirectory: filepath.Dir(absolute)})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	service, err := adminjobs.NewService(adminjobs.NewRepository(runtime.DB.Pool), parityMetadataMutator{})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := security.NewPayloadCipher(cfg.Security.IdempotencyEncryptionSecret)
	if err != nil {
		t.Fatal(err)
	}
	broadcaster := sse.MustNew(sse.Options{MaxTopics: 1})
	defer broadcaster.Close()
	routes, err := adminjobs.NewRoutes(
		service,
		runtime.Identity,
		adminjobs.NewPersistentIdempotency(sharedidempotency.New(runtime.DB.Pool, cipher)),
		broadcaster,
	)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: func(engine *gin.Engine) {
		routes.Register(engine)
	}})
	if err != nil {
		t.Fatal(err)
	}
	goServer := httptest.NewServer(engine)
	defer goServer.Close()
	client := &http.Client{Timeout: 20 * time.Second}
	legacyBase = strings.TrimRight(legacyBase, "/")
	id := "00000000-0000-4000-8000-000000000001"
	requests := []mediaParityRequest{
		{http.MethodGet, "/api/v1/admin/jobs?unknown=true", nil, ""},
		{http.MethodGet, "/api/v1/admin/jobs/events", nil, ""},
		{http.MethodGet, "/api/v1/admin/jobs/" + id, nil, ""},
		{http.MethodPost, "/api/v1/admin/jobs/" + id + "/retry", []byte(`{"unknown":true}`), "application/json"},
		{http.MethodPost, "/api/v1/admin/jobs/" + id + "/cancel", []byte(`{}`), "application/json"},
	}
	for _, request := range requests {
		legacy := fetchMediaParity(t, client, legacyBase+request.path, request)
		modern := fetchMediaParity(t, client, goServer.URL+request.path, request)
		if legacy.status != modern.status || !semanticJSONEqual(legacy.body, modern.body) {
			t.Fatalf("unauthorized %s %s differs:\nlegacy=%d %s\ngo=%d %s", request.method, request.path, legacy.status, legacy.body, modern.status, modern.body)
		}
	}
	invalid := []mediaParityRequest{
		{http.MethodGet, "/api/v1/admin/jobs?page=0", nil, ""},
		{http.MethodGet, "/api/v1/admin/jobs/not-a-uuid", nil, ""},
		{http.MethodPost, "/api/v1/admin/jobs/not-a-uuid/retry", []byte(`{}`), "application/json"},
		{http.MethodPost, "/api/v1/admin/jobs/" + id + "/retry", []byte(`{"reason":null}`), "application/json"},
		{http.MethodPost, "/api/v1/admin/jobs/not-a-uuid/cancel", []byte(`{}`), "application/json"},
	}
	for _, request := range invalid {
		legacy := fetchMediaParity(t, client, legacyBase+request.path, request)
		modern := fetchMediaParity(t, client, goServer.URL+request.path, request)
		if legacy.status != modern.status || !semanticJSONEqual(legacy.body, modern.body) {
			t.Fatalf("invalid %s %s differs:\nlegacy=%d %s\ngo=%d %s", request.method, request.path, legacy.status, legacy.body, modern.status, modern.body)
		}
	}
	accessToken := existingAdministratorAccessToken(t, ctx, runtime, cfg)
	listPath := "/api/v1/admin/jobs?page=1&pageSize=25&sort=createdAt&order=desc"
	legacyList := fetchAdminSettings(t, client, http.MethodGet, legacyBase+listPath, accessToken, nil)
	modernList := fetchAdminSettings(t, client, http.MethodGet, goServer.URL+listPath, accessToken, nil)
	assertParityJSON(t, "GET "+listPath, legacyList, modernList, nil)
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(legacyList.body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) > 0 {
		detailPath := "/api/v1/admin/jobs/" + page.Items[0].ID
		legacyDetail := fetchAdminSettings(t, client, http.MethodGet, legacyBase+detailPath, accessToken, nil)
		modernDetail := fetchAdminSettings(t, client, http.MethodGet, goServer.URL+detailPath, accessToken, nil)
		assertParityJSON(t, "GET "+detailPath, legacyDetail, modernDetail, nil)
	}
	legacyEvents := fetchFirstSSEEvent(t, client, legacyBase+"/api/v1/admin/jobs/events", accessToken)
	modernEvents := fetchFirstSSEEvent(t, client, goServer.URL+"/api/v1/admin/jobs/events", accessToken)
	if legacyEvents.status != modernEvents.status || legacyEvents.retry != modernEvents.retry || !deepJSONEqual(legacyEvents.data, modernEvents.data) {
		t.Fatalf("GET /api/v1/admin/jobs/events differs:\nlegacy=%#v\ngo=%#v", legacyEvents, modernEvents)
	}
}

type parityMetadataMutator struct{}

type sseParitySnapshot struct {
	status int
	retry  string
	data   any
}

func fetchFirstSSEEvent(t *testing.T, client *http.Client, target, accessToken string) sseParitySnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-Trace-Id", "admin-jobs-sse-parity")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result := sseParitySnapshot{status: response.StatusCode}
	if response.StatusCode != http.StatusOK {
		return result
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%s content type = %q", target, response.Header.Get("Content-Type"))
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "retry:"):
			result.retry = strings.TrimSpace(strings.TrimPrefix(line, "retry:"))
		case strings.HasPrefix(line, "data:"):
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &result.data); err != nil {
				t.Fatal(err)
			}
			return result
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	t.Fatalf("%s did not emit an SSE data frame", target)
	return result
}

func (parityMetadataMutator) Retry(context.Context, string, string, string, adminjobs.MetadataMutationInput) error {
	return nil
}
func (parityMetadataMutator) Cancel(context.Context, string, string, string, adminjobs.MetadataMutationInput) error {
	return nil
}
