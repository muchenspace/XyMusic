package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/platform/workerstatus"
)

const (
	forbiddenPrimaryID   = "00000000-0000-4000-8000-000000000001"
	forbiddenSecondaryID = "00000000-0000-4000-8000-000000000002"
)

type adminForbiddenContractAPI struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Auth        string `json:"auth"`
	Idempotency string `json:"idempotency"`
	BodyKind    string `json:"bodyKind"`
}

type adminForbiddenFixture struct {
	path        string
	body        []byte
	contentType string
}

type adminForbiddenResponse struct {
	status      int
	contentType string
	body        []byte
}

type activeUserCredential struct {
	token       string
	userID      string
	sessionID   string
	authVersion int
}

type adminForbiddenSideEffectSnapshot struct {
	idempotencyRecords int64
	auditLogs          int64
}

func TestLegacyAndGoRejectActiveUsersFromEveryAdminSessionAPI(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	legacyBase := os.Getenv("XYMUSIC_LEGACY_BASE_URL")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run administrator authorization parity")
	}
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	contractAPIs := loadAdminSessionContractAPIs(t)
	if len(contractAPIs) != 91 {
		t.Fatalf("admin-session contract endpoint count = %d, want 91", len(contractAPIs))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	goRuntime, err := app.Bootstrap(ctx, cfg, app.Options{
		RootDirectory: filepath.Dir(absoluteEnvironmentPath),
		Administration: &app.AdministrationOptions{
			Runtime: &adminForbiddenRuntimeController{
				config: cfg,
				status: setup.RuntimeSnapshot{
					Phase: setup.RuntimePhaseReady, Source: setup.RuntimeSourceManaged, Generation: 1,
				},
			},
			Store:              adminForbiddenConfigurationStore{},
			Worker:             adminForbiddenWorkerMonitor{},
			ConfigurationPath:  absoluteEnvironmentPath,
			IPv4ListenerHost:   cfg.HTTP.IPv4Host,
			IPv4ListenerPort:   cfg.HTTP.IPv4Port,
			IPv6ListenerHost:   cfg.HTTP.IPv6Host,
			IPv6ListenerPort:   cfg.HTTP.IPv6Port,
			ApplicationVersion: "admin-forbidden-parity",
			StartedAt:          time.Now().Add(-time.Minute),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer goRuntime.Close()
	credential := existingActiveUserCredential(t, ctx, goRuntime, cfg)
	before := snapshotAdminForbiddenSideEffects(t, ctx, goRuntime, credential.userID)

	goServer := httptest.NewServer(goRuntime.Handler)
	defer goServer.Close()
	client := &http.Client{Timeout: 15 * time.Second}
	legacyBase = strings.TrimRight(legacyBase, "/")
	covered := 0
	seen := make(map[string]struct{}, len(contractAPIs))
	for index, api := range contractAPIs {
		key := api.Method + " " + api.Path
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate admin-session contract endpoint %s", key)
		}
		seen[key] = struct{}{}
		fixture, fixtureErr := validAdminForbiddenFixture(api)
		if fixtureErr != nil {
			t.Fatalf("fixture for %s: %v", key, fixtureErr)
		}
		api := api
		fixtureIndex := index
		t.Run(fmt.Sprintf("%03d_%s_%s", index+1, api.Method, strings.ReplaceAll(strings.TrimPrefix(api.Path, "/"), "/", "_")), func(t *testing.T) {
			traceID := fmt.Sprintf("admin-user-forbidden-%03d", fixtureIndex+1)
			modern := fetchAdminForbidden(
				t, ctx, client, goServer.URL+fixture.path, credential.token, traceID, api, fixture,
			)
			if isModernOnlyAdminForbiddenEndpoint(key) {
				assertModernAdminForbidden(t, key, modern)
				covered++
				return
			}
			legacy := fetchAdminForbidden(
				t, ctx, client, legacyBase+fixture.path, credential.token, traceID, api, fixture,
			)
			assertAdminForbiddenParity(t, key, legacy, modern)
			covered++
		})
	}
	if covered != len(contractAPIs) {
		t.Errorf("verified %d/%d admin-session endpoints", covered, len(contractAPIs))
	}
	assertActiveUserCredentialUnchanged(t, ctx, goRuntime, credential)
	after := snapshotAdminForbiddenSideEffects(t, ctx, goRuntime, credential.userID)
	if after != before {
		t.Errorf("forbidden requests created persistent side effects: before=%+v after=%+v", before, after)
	}
	t.Logf("verified active USER rejection on %d/%d admin-session endpoints", covered, len(contractAPIs))
}

func loadAdminSessionContractAPIs(t *testing.T) []adminForbiddenContractAPI {
	t.Helper()
	manifestPath := filepath.Join("..", "..", "contracts", "legacy-api.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	var manifest struct {
		APIs []adminForbiddenContractAPI `json:"apis"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	result := make([]adminForbiddenContractAPI, 0, 91)
	for _, api := range manifest.APIs {
		if api.Auth == "admin-session" {
			result = append(result, api)
		}
	}
	return result
}

func validAdminForbiddenFixture(api adminForbiddenContractAPI) (adminForbiddenFixture, error) {
	key := api.Method + " " + api.Path
	path := strings.NewReplacer(
		":revisionId", forbiddenSecondaryID,
		":sessionId", forbiddenSecondaryID,
		":scanId", forbiddenSecondaryID,
		":jobId", forbiddenSecondaryID,
		":id", forbiddenPrimaryID,
	).Replace(api.Path)
	switch key {
	case "GET /api/v1/admin/tag-scraping/artwork":
		path += "?url=" + url.QueryEscape("https://y.qq.com/cover.jpg")
	case "GET /api/v1/admin/sources/browse":
		path += "?path=music"
	}
	if strings.Contains(path, ":") {
		return adminForbiddenFixture{}, fmt.Errorf("unresolved path parameter in %q", path)
	}
	fixture := adminForbiddenFixture{path: path}
	switch api.BodyKind {
	case "none":
		return fixture, nil
	case "json", "json-optional":
		body, found := adminForbiddenJSONBodies[key]
		if !found && api.BodyKind == "json-optional" {
			body, found = `{}`, true
		}
		if !found {
			return adminForbiddenFixture{}, errors.New("valid JSON body is not defined")
		}
		if !json.Valid([]byte(body)) {
			return adminForbiddenFixture{}, errors.New("configured request body is not valid JSON")
		}
		fixture.body = []byte(body)
		fixture.contentType = "application/json"
		return fixture, nil
	case "binary":
		fixture.body = []byte("FLAC")
		fixture.contentType = "audio/flac"
		return fixture, nil
	default:
		return adminForbiddenFixture{}, fmt.Errorf("unsupported body kind %q", api.BodyKind)
	}
}

var adminForbiddenJSONBodies = map[string]string{
	"POST /api/v1/admin/users": `{
		"username":"Alice_1","password":"secret1","displayName":"Alice","role":"USER"
	}`,
	"PATCH /api/v1/admin/users/:id": `{
		"expectedVersion":1,"displayName":"Alice","reason":"operator update"
	}`,
	"POST /api/v1/admin/users/:id/password": `{
		"expectedVersion":1,"password":"secret2","reason":"operator reset"
	}`,
	"POST /api/v1/admin/users/:id/sessions/:sessionId/revoke": `{
		"reason":"operator revoke"
	}`,
	"DELETE /api/v1/admin/users/:id": `{
		"expectedVersion":1,"reason":"operator delete"
	}`,
	"POST /api/v1/admin/users/:id/restore": `{
		"expectedVersion":2,"reason":"operator restore"
	}`,
	"PATCH /api/v1/admin/tracks/:id/metadata": `{
		"expectedVersion":1,"patch":{"title":"New title"},"reason":"operator edit"
	}`,
	"POST /api/v1/admin/metadata/batch": `{
		"items":[{"trackId":"` + forbiddenPrimaryID + `","expectedVersion":2}],
		"patch":{"genres":["Rock"]},"reason":"operator batch"
	}`,
	"POST /api/v1/admin/tracks/:id/metadata/revisions/:revisionId/restore": `{
		"expectedVersion":3,"reason":"operator restore"
	}`,
	"POST /api/v1/admin/tracks/:id/metadata/writeback": `{
		"expectedVersion":4,"reason":"operator writeback"
	}`,
	"POST /api/v1/admin/metadata/writeback-jobs/:id/retry": `{
		"expectedVersion":2,"reason":"operator retry"
	}`,
	"POST /api/v1/admin/metadata/writeback-jobs/:id/cancel": `{
		"expectedVersion":3,"reason":"operator cancel"
	}`,
	"POST /api/v1/admin/tag-scraping/search": `{
		"source":"smart","title":"Song","artist":"Artist"
	}`,
	"POST /api/v1/admin/tag-scraping/candidates/details": `{
		"candidate":{"id":"song","name":"Song","artist":"Artist","artistId":"artist","album":"Album","albumId":"album","albumImg":"https://y.qq.com/cover.jpg","year":"2020","track":"1","disc":"1","genre":"Rock","source":"qmusic"}
	}`,
	"POST /api/v1/admin/tag-scraping/tracks/:id/apply": `{
		"expectedVersion":1,
		"candidate":{"id":"song","name":"Song","artist":"Artist","artistId":"artist","album":"Album","albumId":"album","albumImg":"https://y.qq.com/cover.jpg","year":"2020","track":"1","disc":"1","genre":"Rock","source":"qmusic"},
		"fields":{"title":true,"artist":true,"album":true,"year":true,"genre":true,"lyrics":true,"cover":true,"overwrite":false},
		"writeBack":false,"reason":"operator apply"
	}`,
	"POST /api/v1/admin/tag-scraping/batches": `{
		"items":[{"trackId":"` + forbiddenPrimaryID + `","expectedVersion":1}],
		"options":{"sources":["qmusic"],"matchMode":"strict","missingFields":["lyrics"],
		"fields":{"title":true,"artist":true,"album":true,"year":true,"genre":true,"lyrics":true,"cover":true,"overwrite":false},
		"writeBack":false,"reason":"batch apply"}
	}`,
	"PATCH /api/v1/admin/settings": `{
		"expectedVersion":1,"registration":{"enabled":true}
	}`,
	"POST /api/v1/admin/settings/test/database":      `{}`,
	"POST /api/v1/admin/settings/test/storage":       `{}`,
	"POST /api/v1/admin/settings/test/media-tools":   `{}`,
	"POST /api/v1/admin/settings/test/local-library": `{}`,
	"POST /api/v1/admin/artists": `{
		"name":"Artist","description":"Biography"
	}`,
	"PATCH /api/v1/admin/artists/:id": `{
		"expectedVersion":2,"name":"Renamed Artist","description":null
	}`,
	"POST /api/v1/admin/albums": `{
		"title":"Album","artistCredits":[{"artistId":"` + forbiddenSecondaryID + `","role":"PRIMARY","sortOrder":0}],
		"releaseDate":"2026-07-16","description":null
	}`,
	"PATCH /api/v1/admin/albums/:id": `{
		"expectedVersion":2,"title":"Updated Album","releaseDate":null
	}`,
	"POST /api/v1/admin/albums/merge": `{
		"target":{"albumId":"` + forbiddenPrimaryID + `","expectedVersion":2},
		"sources":[{"albumId":"` + forbiddenSecondaryID + `","expectedVersion":3}],
		"fieldSources":{"title":"` + forbiddenPrimaryID + `","cover":null,"artistCredits":"` + forbiddenSecondaryID + `","releaseDate":null,"description":null}
	}`,
	"POST /api/v1/admin/tracks": `{
		"title":"Track","albumId":"` + forbiddenPrimaryID + `",
		"artistCredits":[{"artistId":"` + forbiddenSecondaryID + `","role":"PRIMARY","sortOrder":0}],"trackNumber":7
	}`,
	"POST /api/v1/admin/tracks/batch/restore": `{
		"items":[{"trackId":"` + forbiddenPrimaryID + `","expectedVersion":2}]
	}`,
	"POST /api/v1/admin/tracks/batch/delete-permanently": `{
		"items":[{"trackId":"` + forbiddenPrimaryID + `","expectedVersion":2}]
	}`,
	"PATCH /api/v1/admin/tracks/:id": `{
		"expectedVersion":2,"title":"Updated Track","albumId":null,"trackNumber":null,"discNumber":2
	}`,
	"POST /api/v1/admin/tracks/:id/publish": `{
		"expectedVersion":2
	}`,
	"POST /api/v1/admin/tracks/:id/archive": `{
		"expectedVersion":2
	}`,
	"POST /api/v1/admin/tracks/:id/restore": `{
		"expectedVersion":2
	}`,
	"DELETE /api/v1/admin/tracks/:id": `{
		"expectedVersion":2
	}`,
	"PUT /api/v1/admin/tracks/:id/lyrics": `{
		"expectedVersion":2,"language":"zh-CN","format":"LRC","content":"[00:00]Line","isDefault":true
	}`,
	"PATCH /api/v1/admin/users/:id/status": `{
		"expectedVersion":2,"status":"SUSPENDED","reason":"maintenance"
	}`,
	"POST /api/v1/admin/sources": `{
		"name":"Music","path":"music","mode":"READ_ONLY","enabled":true,"scanOnStartup":false,
		"scanIntervalMinutes":5,"includePatterns":[],"excludePatterns":[]
	}`,
	"PATCH /api/v1/admin/sources/:id": `{
		"expectedVersion":1,"name":"Updated"
	}`,
	"DELETE /api/v1/admin/sources/:id": `{
		"expectedVersion":1,"archiveCatalog":false
	}`,
	"POST /api/v1/admin/media/uploads": `{
		"purpose":"TRACK_SOURCE","targetId":"` + forbiddenPrimaryID + `","fileName":"source.flac",
		"contentType":"audio/flac","sizeBytes":4,"checksumSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}`,
	"POST /api/v1/admin/media/uploads/:id/complete": `{}`,
	"POST /api/v1/admin/media/jobs/:id/retry": `{
		"expectedVersion":2,"reason":"operator retry"
	}`,
}

func existingActiveUserCredential(t *testing.T, ctx context.Context, runtime *app.Runtime, cfg config.Config) activeUserCredential {
	t.Helper()
	var userID, sessionID, role string
	var authVersion int
	err := runtime.DB.QueryRow(ctx, `
		select u.id, s.id, u.auth_version, u.role
		from users u
		join auth_sessions s on s.user_id = u.id
		where u.role = 'USER' and u.status = 'ACTIVE' and s.revoked_at is null
		order by s.last_seen_at desc, s.created_at desc
		limit 1
	`).Scan(&userID, &sessionID, &authVersion, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no ACTIVE USER with an unrevoked session is available for administrator authorization parity")
	}
	if err != nil {
		t.Fatal(err)
	}
	tokens := security.NewAccessTokenService(
		cfg.Security.AccessTokenSecret,
		time.Duration(cfg.Security.AccessTokenTTLSeconds)*time.Second,
	)
	token, _, err := tokens.Issue(security.Principal{
		UserID: userID, SessionID: sessionID, AuthVersion: authVersion, Role: role,
	})
	if err != nil {
		t.Fatal(err)
	}
	return activeUserCredential{
		token: token, userID: userID, sessionID: sessionID, authVersion: authVersion,
	}
}

func assertActiveUserCredentialUnchanged(
	t *testing.T,
	ctx context.Context,
	runtime *app.Runtime,
	credential activeUserCredential,
) {
	t.Helper()
	var role, status string
	var authVersion int
	var sessionActive bool
	err := runtime.DB.QueryRow(ctx, `
		select u.role, u.status, u.auth_version, s.revoked_at is null
		from users u
		join auth_sessions s on s.user_id = u.id
		where u.id = $1 and s.id = $2
	`, credential.userID, credential.sessionID).Scan(&role, &status, &authVersion, &sessionActive)
	if err != nil {
		t.Fatalf("reload active USER credential after forbidden requests: %v", err)
	}
	if role != "USER" || status != "ACTIVE" || authVersion != credential.authVersion || !sessionActive {
		t.Errorf(
			"active USER credential changed after forbidden requests: role=%s status=%s authVersion=%d sessionActive=%t",
			role, status, authVersion, sessionActive,
		)
	}
}

func snapshotAdminForbiddenSideEffects(
	t *testing.T,
	ctx context.Context,
	runtime *app.Runtime,
	userID string,
) adminForbiddenSideEffectSnapshot {
	t.Helper()
	var snapshot adminForbiddenSideEffectSnapshot
	err := runtime.DB.QueryRow(ctx, `
		select
			(select count(*) from idempotency_records
			 where actor_id = $1 and key like 'active-user-forbidden-admin-user-forbidden-%'),
			(select count(*) from audit_logs
			 where actor_id = $1 and trace_id like 'admin-user-forbidden-%')
	`, userID).Scan(&snapshot.idempotencyRecords, &snapshot.auditLogs)
	if err != nil {
		t.Fatalf("snapshot forbidden-request side effects: %v", err)
	}
	return snapshot
}

func fetchAdminForbidden(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	target, accessToken, traceID string,
	api adminForbiddenContractAPI,
	fixture adminForbiddenFixture,
) adminForbiddenResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, api.Method, target, bytes.NewReader(fixture.body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-Trace-Id", traceID)
	if fixture.contentType != "" {
		request.Header.Set("Content-Type", fixture.contentType)
	}
	if api.Idempotency == "required" {
		request.Header.Set("Idempotency-Key", "active-user-forbidden-"+traceID)
	}
	if strings.HasSuffix(api.Path, "/events") {
		request.Header.Set("Accept", "text/event-stream")
	}
	result, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", api.Method, target, err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Body, 1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	return adminForbiddenResponse{
		status: result.StatusCode, contentType: result.Header.Get("Content-Type"), body: body,
	}
}

func assertAdminForbiddenParity(t *testing.T, endpoint string, legacy, modern adminForbiddenResponse) {
	t.Helper()
	if legacy.status != http.StatusForbidden || modern.status != http.StatusForbidden {
		t.Fatalf("%s did not reject active USER as 403:\nlegacy=%d %s\ngo=%d %s", endpoint, legacy.status, legacy.body, modern.status, modern.body)
	}
	if !strings.HasPrefix(legacy.contentType, "application/problem+json") ||
		!strings.HasPrefix(modern.contentType, "application/problem+json") {
		t.Fatalf("%s problem content type differs: legacy=%q go=%q", endpoint, legacy.contentType, modern.contentType)
	}
	var legacyProblem map[string]any
	var modernProblem map[string]any
	if err := json.Unmarshal(legacy.body, &legacyProblem); err != nil {
		t.Fatalf("%s legacy forbidden response is not JSON: %v (%s)", endpoint, err, legacy.body)
	}
	if err := json.Unmarshal(modern.body, &modernProblem); err != nil {
		t.Fatalf("%s Go forbidden response is not JSON: %v (%s)", endpoint, err, modern.body)
	}
	if legacyProblem["code"] != "FORBIDDEN" || modernProblem["code"] != "FORBIDDEN" {
		t.Fatalf("%s forbidden code differs: legacy=%v go=%v", endpoint, legacyProblem["code"], modernProblem["code"])
	}
	if !deepJSONEqual(legacyProblem, modernProblem) {
		legacyJSON, _ := json.Marshal(legacyProblem)
		modernJSON, _ := json.Marshal(modernProblem)
		t.Fatalf("%s forbidden problem differs:\nlegacy=%s\ngo=%s", endpoint, legacyJSON, modernJSON)
	}
}

func isModernOnlyAdminForbiddenEndpoint(endpoint string) bool {
	switch endpoint {
	case "POST /api/v1/admin/tracks/batch/restore",
		"POST /api/v1/admin/tracks/batch/delete-permanently",
		"GET /api/v1/admin/tracks/batch/delete-permanently/:jobId",
		"POST /api/v1/admin/tag-scraping/candidates/details":
		return true
	default:
		return false
	}
}

func assertModernAdminForbidden(t *testing.T, endpoint string, response adminForbiddenResponse) {
	t.Helper()
	if response.status != http.StatusForbidden || !strings.HasPrefix(response.contentType, "application/problem+json") {
		t.Fatalf("%s did not reject active USER as 403: status=%d contentType=%q body=%s",
			endpoint, response.status, response.contentType, response.body)
	}
	var problem map[string]any
	if err := json.Unmarshal(response.body, &problem); err != nil {
		t.Fatalf("%s Go forbidden response is not JSON: %v (%s)", endpoint, err, response.body)
	}
	if problem["code"] != "FORBIDDEN" {
		t.Fatalf("%s Go forbidden code=%v body=%s", endpoint, problem["code"], response.body)
	}
}

type adminForbiddenRuntimeController struct {
	config config.Config
	status setup.RuntimeSnapshot
}

func (runtime *adminForbiddenRuntimeController) Status() setup.RuntimeSnapshot { return runtime.status }
func (runtime *adminForbiddenRuntimeController) ActiveConfig() (config.Config, bool) {
	return runtime.config, true
}
func (runtime *adminForbiddenRuntimeController) Initialize(_ context.Context, candidate config.Config, source string) error {
	runtime.config = candidate
	runtime.status.Source = source
	runtime.status.Generation++
	return nil
}

type adminForbiddenConfigurationStore struct{}

func (adminForbiddenConfigurationStore) Save(config.Config) error { return nil }

type adminForbiddenWorkerMonitor struct{}

func (adminForbiddenWorkerMonitor) Status(context.Context, string) workerstatus.Snapshot {
	return workerstatus.Snapshot{
		Mode: "external", State: "RUNNING", Responsive: true, Synchronized: true, Available: true,
	}
}
