package parity

import (
	"bytes"
	"context"
	"encoding/json"
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
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/platform/workerstatus"
)

func TestLegacyAndGoAdminSettingsReadOnlyParity(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	legacyBase := os.Getenv("XYMUSIC_LEGACY_BASE_URL")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run admin settings parity")
	}
	absolute, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolute).Load()
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := workerstatus.New(workerstatus.Options{Path: absolute + ".worker-status"})
	if err != nil {
		t.Fatal(err)
	}
	runtimeController := &settingsParityRuntime{
		config: cfg,
		status: setup.RuntimeSnapshot{Phase: setup.RuntimePhaseReady, Source: setup.RuntimeSourceManaged, Generation: 1},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	goRuntime, err := app.Bootstrap(ctx, cfg, app.Options{
		RootDirectory: filepath.Dir(absolute),
		Administration: &app.AdministrationOptions{
			Runtime: runtimeController, Store: settingsParityStore{}, Worker: monitor,
			ConfigurationPath: absolute,
			IPv4ListenerHost:  cfg.HTTP.IPv4Host, IPv4ListenerPort: cfg.HTTP.IPv4Port,
			IPv6ListenerHost: cfg.HTTP.IPv6Host, IPv6ListenerPort: cfg.HTTP.IPv6Port,
			ApplicationVersion: "0.1.0",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer goRuntime.Close()
	accessToken := existingAdministratorAccessToken(t, ctx, goRuntime, cfg)
	goServer := httptest.NewServer(goRuntime.Handler)
	defer goServer.Close()
	client := &http.Client{Timeout: 30 * time.Second}
	legacyBase = strings.TrimRight(legacyBase, "/")

	settingsLegacy := fetchAdminSettings(t, client, http.MethodGet, legacyBase+"/api/v1/admin/settings", accessToken, nil)
	settingsGo := fetchAdminSettings(t, client, http.MethodGet, goServer.URL+"/api/v1/admin/settings", accessToken, nil)
	assertParityJSON(t, "GET /api/v1/admin/settings", settingsLegacy, settingsGo, nil)

	tests := []struct {
		path      string
		normalize func(any)
	}{
		{"/api/v1/admin/settings/test/database", removeJSONFields("latencyMs")},
		{"/api/v1/admin/settings/test/storage", removeJSONFields("latencyMs")},
		{"/api/v1/admin/settings/test/media-tools", nil},
		{"/api/v1/admin/settings/test/local-library", nil},
	}
	for _, test := range tests {
		legacy := fetchAdminSettings(t, client, http.MethodPost, legacyBase+test.path, accessToken, []byte(`{}`))
		modern := fetchAdminSettings(t, client, http.MethodPost, goServer.URL+test.path, accessToken, []byte(`{}`))
		assertParityJSON(t, "POST "+test.path, legacy, modern, test.normalize)
	}

	systemLegacy := fetchAdminSettings(t, client, http.MethodGet, legacyBase+"/api/v1/admin/system", accessToken, nil)
	systemGo := fetchAdminSettings(t, client, http.MethodGet, goServer.URL+"/api/v1/admin/system", accessToken, nil)
	assertRuntimeMetricsContract(t, "legacy", systemLegacy)
	assertRuntimeMetricsContract(t, "Go", systemGo)
	assertParityJSON(t, "GET /api/v1/admin/system", systemLegacy, systemGo, removeJSONFields(
		"applicationVersion", "runtimeVersion", "platform", "architecture", "uptimeSeconds", "worker", "metrics",
	))
}

func assertRuntimeMetricsContract(t *testing.T, name string, response response) {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("%s system status=%d body=%s", name, response.status, response.body)
	}
	var document map[string]any
	if err := json.Unmarshal(response.body, &document); err != nil {
		t.Fatalf("%s system JSON: %v", name, err)
	}
	metrics, ok := document["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("%s metrics is null or not an object: %#v", name, document["metrics"])
	}
	assertExactMetricKeys(t, name+" metrics", metrics, "collectedSince", "requests", "eventLoop", "memory")
	collectedSince, ok := metrics["collectedSince"].(string)
	if !ok {
		t.Fatalf("%s collectedSince=%#v", name, metrics["collectedSince"])
	}
	if _, err := time.Parse(time.RFC3339Nano, collectedSince); err != nil {
		t.Fatalf("%s collectedSince=%q: %v", name, collectedSince, err)
	}
	requests, ok := metrics["requests"].(map[string]any)
	if !ok {
		t.Fatalf("%s requests=%#v", name, metrics["requests"])
	}
	assertExactMetricKeys(
		t, name+" requests", requests,
		"total", "inFlight", "errors", "errorRate", "slow",
		"averageLatencyMs", "p95LatencyMs", "maximumLatencyMs", "sampled",
	)
	total := metricNumber(t, name+" requests.total", requests["total"])
	sampled := metricNumber(t, name+" requests.sampled", requests["sampled"])
	if sampled < 0 || total < 0 || sampled > total {
		t.Fatalf("%s request totals sampled=%v total=%v", name, sampled, total)
	}
	eventLoop, ok := metrics["eventLoop"].(map[string]any)
	if !ok {
		t.Fatalf("%s eventLoop=%#v", name, metrics["eventLoop"])
	}
	assertExactMetricKeys(t, name+" eventLoop", eventLoop, "lagMs", "maximumLagMs")
	lag := metricNumber(t, name+" eventLoop.lagMs", eventLoop["lagMs"])
	maximumLag := metricNumber(t, name+" eventLoop.maximumLagMs", eventLoop["maximumLagMs"])
	if lag < 0 || maximumLag < lag {
		t.Fatalf("%s event-loop lag=%v maximum=%v", name, lag, maximumLag)
	}
	memory, ok := metrics["memory"].(map[string]any)
	if !ok {
		t.Fatalf("%s memory=%#v", name, metrics["memory"])
	}
	assertExactMetricKeys(t, name+" memory", memory, "rssBytes", "heapUsedBytes", "heapTotalBytes", "externalBytes")
	for _, key := range []string{"rssBytes", "heapUsedBytes", "heapTotalBytes", "externalBytes"} {
		if value := metricNumber(t, name+" memory."+key, memory[key]); value < 0 {
			t.Fatalf("%s memory.%s=%v", name, key, value)
		}
	}
}

func assertExactMetricKeys(t *testing.T, name string, object map[string]any, keys ...string) {
	t.Helper()
	if len(object) != len(keys) {
		t.Fatalf("%s keys=%v, want=%v", name, object, keys)
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Fatalf("%s missing key %q: %v", name, key, object)
		}
	}
}

func metricNumber(t *testing.T, name string, value any) float64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("%s=%#v is not a number", name, value)
	}
	return number
}

func existingAdministratorAccessToken(
	t *testing.T,
	ctx context.Context,
	runtime *app.Runtime,
	cfg config.Config,
) string {
	t.Helper()
	var userID, sessionID, role string
	var authVersion int
	err := runtime.DB.QueryRow(ctx, `
		select u.id, s.id, u.auth_version, u.role
		from users u
		join auth_sessions s on s.user_id=u.id
		where u.role='ADMIN' and u.status='ACTIVE' and s.revoked_at is null
		order by s.last_seen_at desc, s.created_at desc
		limit 1
	`).Scan(&userID, &sessionID, &authVersion, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no active administrator session is available for read-only parity")
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
	return token
}

func fetchAdminSettings(
	t *testing.T,
	client *http.Client,
	method, target, accessToken string,
	body []byte,
) response {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-Trace-Id", "admin-settings-parity")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	result, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	content, err := io.ReadAll(io.LimitReader(result.Body, 8*1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	return response{status: result.StatusCode, body: content}
}

func assertParityJSON(t *testing.T, name string, legacy, modern response, normalize func(any)) {
	t.Helper()
	if legacy.status != modern.status {
		t.Fatalf("%s status differs: legacy=%d go=%d\nlegacy=%s\ngo=%s", name, legacy.status, modern.status, legacy.body, modern.body)
	}
	var legacyValue any
	var modernValue any
	if err := json.Unmarshal(legacy.body, &legacyValue); err != nil {
		t.Fatalf("%s legacy JSON: %v (%s)", name, err, legacy.body)
	}
	if err := json.Unmarshal(modern.body, &modernValue); err != nil {
		t.Fatalf("%s Go JSON: %v (%s)", name, err, modern.body)
	}
	if normalize != nil {
		normalize(legacyValue)
		normalize(modernValue)
	}
	if !deepJSONEqual(legacyValue, modernValue) {
		legacyJSON, _ := json.Marshal(legacyValue)
		modernJSON, _ := json.Marshal(modernValue)
		t.Fatalf("%s body differs:\nlegacy=%s\ngo=%s", name, legacyJSON, modernJSON)
	}
}

func removeJSONFields(fields ...string) func(any) {
	return func(value any) {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		for _, field := range fields {
			delete(object, field)
		}
	}
}

type settingsParityRuntime struct {
	config config.Config
	status setup.RuntimeSnapshot
}

func (runtime *settingsParityRuntime) Status() setup.RuntimeSnapshot { return runtime.status }
func (runtime *settingsParityRuntime) ActiveConfig() (config.Config, bool) {
	return runtime.config, true
}
func (runtime *settingsParityRuntime) Initialize(_ context.Context, candidate config.Config, source string) error {
	runtime.config = candidate
	runtime.status.Source = source
	runtime.status.Generation++
	return nil
}

type settingsParityStore struct{}

func (settingsParityStore) Save(config.Config) error { return nil }
