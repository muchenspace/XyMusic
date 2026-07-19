package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/control"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/workerstatus"
)

const expectedLegacyAPIProbeCount = 135

type fullAPIManifest struct {
	CountingRules struct {
		ExpectedAPIEndpoints int `json:"expectedAPIEndpoints"`
	} `json:"countingRules"`
	APIs []fullAPIContract `json:"apis"`
}

type fullAPIContract struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Auth        string `json:"auth"`
	Idempotency string `json:"idempotency"`
	BodyKind    string `json:"bodyKind"`
}

type fullAPIProbe struct {
	method      string
	path        string
	body        []byte
	contentType string
}

type fullAPIResponse struct {
	status int
	header http.Header
	body   []byte
}

func TestEveryLegacyAPIHasOneReadOnlyProbe(t *testing.T) {
	root := fullAPIProjectRoot(t)
	manifest := loadFullAPIManifest(t, filepath.Join(root, "contracts", "legacy-api.json"))
	expected := validateFullAPIManifest(t, manifest)
	probed := make(map[string]int, len(manifest.APIs))
	concrete := make(map[string]string, len(manifest.APIs))
	for _, contract := range manifest.APIs {
		key := fullAPIRouteKey(contract.Method, contract.Path)
		probe, err := buildReadOnlyFullAPIProbe(contract)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if strings.Contains(probe.path, ":") {
			t.Fatalf("%s produced non-concrete path %q", key, probe.path)
		}
		concreteKey := fullAPIRouteKey(probe.method, probe.path)
		if previous, duplicate := concrete[concreteKey]; duplicate {
			t.Fatalf("safe probes %s and %s collide at %s", previous, key, concreteKey)
		}
		concrete[concreteKey] = key
		probed[key]++
	}
	if differences := verifyFullAPIProbeCoverage(expected, probed); len(differences) != 0 {
		t.Fatalf("safe probe coverage differs:\n%s", strings.Join(differences, "\n"))
	}
	if len(concrete) != expectedLegacyAPIProbeCount {
		t.Fatalf("safe probes contain %d concrete method/path pairs, want %d", len(concrete), expectedLegacyAPIProbeCount)
	}
}

// TestLegacyAndGoEveryAPIReadOnlyParity probes every manifest endpoint without
// credentials. Bodies are either invalid or rejected before authentication,
// so no request can enter a database, object-storage, or filesystem mutation.
func TestLegacyAndGoEveryAPIReadOnlyParity(t *testing.T) {
	environmentPath := strings.TrimSpace(os.Getenv("XYMUSIC_INTEGRATION_ENV"))
	legacyBase := strings.TrimRight(strings.TrimSpace(os.Getenv("XYMUSIC_LEGACY_BASE_URL")), "/")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run the 135-endpoint read-only differential contract test")
	}
	parsedLegacyBase, err := url.ParseRequestURI(legacyBase)
	if err != nil || parsedLegacyBase.Scheme == "" || parsedLegacyBase.Host == "" {
		t.Fatalf("invalid XYMUSIC_LEGACY_BASE_URL %q", legacyBase)
	}

	root := fullAPIProjectRoot(t)
	manifest := loadFullAPIManifest(t, filepath.Join(root, "contracts", "legacy-api.json"))
	expected := validateFullAPIManifest(t, manifest)

	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Dir(absoluteEnvironmentPath)
	resolved, err := config.ResolveRuntime(raw, runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := workerstatus.New(workerstatus.Options{Path: absoluteEnvironmentPath + ".worker-status"})
	if err != nil {
		t.Fatal(err)
	}

	var manager *control.Manager
	factory := control.RuntimeFactoryFunc(func(ctx context.Context, candidate config.Config) (control.ManagedRuntime, error) {
		runtime, err := app.Bootstrap(ctx, candidate, app.Options{
			RootDirectory: runtimeRoot,
			Administration: &app.AdministrationOptions{
				Runtime: manager, Store: config.NewStore(absoluteEnvironmentPath), Worker: monitor,
				ConfigurationPath:  absoluteEnvironmentPath,
				IPv4ListenerHost:   resolved.HTTP.IPv4Host,
				IPv4ListenerPort:   resolved.HTTP.IPv4Port,
				IPv6ListenerHost:   resolved.HTTP.IPv6Host,
				IPv6ListenerPort:   resolved.HTTP.IPv6Port,
				ApplicationVersion: "read-only-parity-test",
				StartedAt:          time.Now(),
			},
			StartBackground: false,
		})
		if err != nil {
			return nil, err
		}
		return control.RuntimeAdapter{
			Handler: runtime.Handler, ReadyFunc: runtime.Ready, CloseFunc: runtime.CloseContext,
		}, nil
	})
	manager, err = control.NewManager(control.ManagerOptions{Source: setup.RuntimeSourceManaged, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close Go control runtime: %v", err)
		}
	}()

	configured := true
	setupService, err := setup.NewService(setup.Options{
		RootDirectory:     runtimeRoot,
		ConfigurationPath: absoluteEnvironmentPath,
		ActualListener: setup.ActualListener{
			IPv4: setup.ListenerAddress{Host: resolved.HTTP.IPv4Host, Port: resolved.HTTP.IPv4Port},
			IPv6: setup.ListenerAddress{Host: resolved.HTTP.IPv6Host, Port: resolved.HTTP.IPv6Port},
		},
		ConfiguredAtStartup: &configured,
		Runtime:             manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := control.NewHandler(control.HandlerOptions{
		Manager: manager, Setup: setupService,
		CORS: httpserver.DefaultCORSConfig(), RequestLimits: httpserver.DefaultRequestLimits(),
		TrustedProxies: append([]string(nil), resolved.HTTP.TrustedProxyAddresses...),
		WorkerStatus:   monitor,
	})
	if err != nil {
		t.Fatal(err)
	}
	initializeContext, cancelInitialize := context.WithTimeout(context.Background(), 45*time.Second)
	err = manager.Initialize(initializeContext, raw, setup.RuntimeSourceManaged)
	cancelInitialize()
	if err != nil {
		t.Fatalf("initialize Go runtime: %v", err)
	}
	goServer := httptest.NewServer(handler)
	defer goServer.Close()

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	probed := make(map[string]int, len(manifest.APIs))
	var differences []string
	for index, contract := range manifest.APIs {
		key := fullAPIRouteKey(contract.Method, contract.Path)
		probed[key]++
		probe, err := buildReadOnlyFullAPIProbe(contract)
		if err != nil {
			differences = append(differences, fmt.Sprintf("%s: build probe: %v", key, err))
			continue
		}
		traceID := fmt.Sprintf("all-api-parity-%03d", index+1)
		if isModernOnlyFullAPIEndpoint(key) {
			modern, requestErr := executeFullAPIProbe(client, goServer.URL, traceID, probe)
			if requestErr != nil {
				differences = append(differences, fmt.Sprintf("%s: Go request failed: %v", key, requestErr))
				continue
			}
			if modern.status == http.StatusNotFound {
				differences = append(differences, fmt.Sprintf("%s: modern-only route returned 404", key))
			}
			continue
		}
		legacy, err := executeFullAPIProbe(client, legacyBase, traceID, probe)
		if err != nil {
			differences = append(differences, fmt.Sprintf("%s: legacy request failed: %v", key, err))
			continue
		}
		modern, err := executeFullAPIProbe(client, goServer.URL, traceID, probe)
		if err != nil {
			differences = append(differences, fmt.Sprintf("%s: Go request failed: %v", key, err))
			continue
		}
		differences = append(differences, compareFullAPIResponses(key, legacy, modern)...)
	}
	differences = append(differences, verifyFullAPIProbeCoverage(expected, probed)...)
	t.Logf("read-only probe coverage: %d/%d unique endpoints; shared endpoints compared against legacy and Go", len(probed), expectedLegacyAPIProbeCount)
	if len(differences) != 0 {
		sort.Strings(differences)
		t.Fatalf("read-only 135-endpoint differential contract found %d difference(s):\n%s", len(differences), strings.Join(differences, "\n"))
	}
}

func loadFullAPIManifest(t *testing.T, path string) fullAPIManifest {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest fullAPIManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func validateFullAPIManifest(t *testing.T, manifest fullAPIManifest) map[string]struct{} {
	t.Helper()
	if manifest.CountingRules.ExpectedAPIEndpoints != expectedLegacyAPIProbeCount {
		t.Fatalf("manifest declares %d endpoints, want %d", manifest.CountingRules.ExpectedAPIEndpoints, expectedLegacyAPIProbeCount)
	}
	if len(manifest.APIs) != expectedLegacyAPIProbeCount {
		t.Fatalf("manifest contains %d endpoints, want %d", len(manifest.APIs), expectedLegacyAPIProbeCount)
	}
	expected := make(map[string]struct{}, len(manifest.APIs))
	for index, contract := range manifest.APIs {
		key := fullAPIRouteKey(contract.Method, contract.Path)
		if _, duplicate := expected[key]; duplicate {
			t.Fatalf("manifest endpoint %s is duplicated at index %d", key, index)
		}
		expected[key] = struct{}{}
	}
	if len(expected) != expectedLegacyAPIProbeCount {
		t.Fatalf("manifest unique endpoint set contains %d endpoints, want %d", len(expected), expectedLegacyAPIProbeCount)
	}
	return expected
}

func buildReadOnlyFullAPIProbe(contract fullAPIContract) (fullAPIProbe, error) {
	path, err := concreteFullAPIPath(contract.Path)
	if err != nil {
		return fullAPIProbe{}, err
	}
	probe := fullAPIProbe{method: contract.Method, path: path}
	switch contract.BodyKind {
	case "none":
	case "json", "json-optional":
		probe.body = []byte(`{}`)
		probe.contentType = "application/json"
	case "binary":
		probe.body = []byte{}
		probe.contentType = "application/octet-stream"
	default:
		return fullAPIProbe{}, fmt.Errorf("unsupported body kind %q", contract.BodyKind)
	}
	if contract.Auth == "none" && contract.Path != "/api/setup/status" && strings.HasPrefix(contract.Path, "/api/setup/") {
		// Syntax failure is guaranteed to precede RequireSetup, even when a test
		// accidentally targets a first-run legacy process.
		probe.body = []byte(`{`)
		probe.contentType = "application/json"
	}
	if contract.Method == http.MethodPost && contract.Path == "/api/v1/admin/auth/refresh" {
		// This legacy handler consumes a database-backed rate-limit bucket before
		// checking its cookie. An oversize declared body is rejected by the shared
		// request-size middleware first, preserving the read-only guarantee.
		probe.body = bytes.Repeat([]byte{'x'}, int(httpserver.MaxStructuredRequestBodyBytes)+1)
		probe.contentType = "application/json"
	}
	return probe, nil
}

func concreteFullAPIPath(template string) (string, error) {
	values := map[string]string{
		"id":         "00000000-0000-4000-8000-000000000001",
		"sessionId":  "00000000-0000-4000-8000-000000000002",
		"entryId":    "00000000-0000-4000-8000-000000000003",
		"revisionId": "00000000-0000-4000-8000-000000000004",
		"scanId":     "00000000-0000-4000-8000-000000000005",
		"trackId":    "00000000-0000-4000-8000-000000000006",
		"jobId":      "00000000-0000-4000-8000-000000000007",
	}
	segments := strings.Split(template, "/")
	for index, segment := range segments {
		if !strings.HasPrefix(segment, ":") {
			continue
		}
		value, ok := values[strings.TrimPrefix(segment, ":")]
		if !ok {
			return "", fmt.Errorf("no safe concrete value for path parameter %q", segment)
		}
		segments[index] = value
	}
	return strings.Join(segments, "/"), nil
}

func isModernOnlyFullAPIEndpoint(endpoint string) bool {
	switch endpoint {
	case "POST /api/v1/admin/tracks/batch/restore",
		"POST /api/v1/admin/tracks/batch/delete-permanently",
		"GET /api/v1/admin/tracks/batch/delete-permanently/:jobId":
		return true
	default:
		return false
	}
}

func executeFullAPIProbe(client *http.Client, baseURL, traceID string, probe fullAPIProbe) (fullAPIResponse, error) {
	requestContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, probe.method, baseURL+probe.path, bytes.NewReader(probe.body))
	if err != nil {
		return fullAPIResponse{}, err
	}
	request.Header.Set("X-Trace-Id", traceID)
	if probe.contentType != "" {
		request.Header.Set("Content-Type", probe.contentType)
	}
	for _, forbidden := range []string{"Authorization", "Cookie", "Idempotency-Key", "X-CSRF-Token"} {
		if request.Header.Get(forbidden) != "" {
			return fullAPIResponse{}, fmt.Errorf("read-only probe unexpectedly contains %s", forbidden)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return fullAPIResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return fullAPIResponse{}, err
	}
	return fullAPIResponse{status: response.StatusCode, header: response.Header.Clone(), body: body}, nil
}

func compareFullAPIResponses(key string, legacy, modern fullAPIResponse) []string {
	var differences []string
	if legacy.status != modern.status {
		differences = append(differences, fmt.Sprintf("%s: status legacy=%d go=%d", key, legacy.status, modern.status))
	}
	headers := []string{"WWW-Authenticate", "Content-Type", "Allow"}
	for _, header := range headers {
		legacyValue := canonicalFullAPIHeader(header, legacy.header.Values(header))
		modernValue := canonicalFullAPIHeader(header, modern.header.Values(header))
		if legacyValue != modernValue {
			differences = append(differences, fmt.Sprintf("%s: %s legacy=%q go=%q", key, header, legacyValue, modernValue))
		}
	}
	if isProblemContentType(legacy.header.Get("Content-Type")) || isProblemContentType(modern.header.Get("Content-Type")) {
		var legacyProblem any
		var modernProblem any
		legacyErr := json.Unmarshal(legacy.body, &legacyProblem)
		modernErr := json.Unmarshal(modern.body, &modernProblem)
		switch {
		case legacyErr != nil || modernErr != nil:
			differences = append(differences, fmt.Sprintf(
				"%s: problem JSON decode legacyErr=%v goErr=%v legacy=%q go=%q",
				key, legacyErr, modernErr, abbreviatedFullAPIBody(legacy.body), abbreviatedFullAPIBody(modern.body),
			))
		case !reflect.DeepEqual(legacyProblem, modernProblem):
			differences = append(differences, fmt.Sprintf(
				"%s: problem JSON differs legacy=%s go=%s",
				key, abbreviatedFullAPIBody(legacy.body), abbreviatedFullAPIBody(modern.body),
			))
		}
	}
	return differences
}

func canonicalFullAPIHeader(name string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	switch name {
	case "Content-Type":
		mediaType, parameters, err := mime.ParseMediaType(values[0])
		if err != nil {
			return strings.ToLower(strings.TrimSpace(values[0]))
		}
		return mime.FormatMediaType(strings.ToLower(mediaType), parameters)
	case "Allow":
		var methods []string
		for _, value := range values {
			for _, method := range strings.Split(value, ",") {
				if method = strings.TrimSpace(method); method != "" {
					methods = append(methods, strings.ToUpper(method))
				}
			}
		}
		sort.Strings(methods)
		return strings.Join(methods, ", ")
	case "WWW-Authenticate":
		return strings.ToLower(strings.Join(strings.Fields(strings.Join(values, " ")), " "))
	default:
		return strings.TrimSpace(strings.Join(values, ", "))
	}
}

func isProblemContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, httpserver.ProblemMediaType)
}

func verifyFullAPIProbeCoverage(expected map[string]struct{}, probed map[string]int) []string {
	var differences []string
	if len(probed) != expectedLegacyAPIProbeCount {
		differences = append(differences, fmt.Sprintf("coverage: probed %d unique endpoints, want %d", len(probed), expectedLegacyAPIProbeCount))
	}
	for key := range expected {
		switch probed[key] {
		case 0:
			differences = append(differences, "coverage: missing "+key)
		case 1:
		default:
			differences = append(differences, fmt.Sprintf("coverage: %s probed %d times", key, probed[key]))
		}
	}
	for key := range probed {
		if _, ok := expected[key]; !ok {
			differences = append(differences, "coverage: unexpected "+key)
		}
	}
	return differences
}

func abbreviatedFullAPIBody(body []byte) string {
	const maximum = 512
	if len(body) <= maximum {
		return string(body)
	}
	return string(body[:maximum]) + "..."
}

func fullAPIProjectRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not locate BackendGo root containing go.mod")
		}
		directory = parent
	}
}

func fullAPIRouteKey(method, path string) string {
	return method + " " + path
}
