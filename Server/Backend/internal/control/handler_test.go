package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/runtimemetrics"
	"xymusic/server/internal/platform/workerstatus"
)

func TestControlHandlerStaysAvailableWithoutRuntimeAndForwardsAfterActivation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var dependencyReady atomic.Bool
	dependencyReady.Store(true)
	runtime := newFakeRuntime("configured runtime")
	runtime.readyFunc = func(context.Context) error {
		if !dependencyReady.Load() {
			return errors.New("database unavailable")
		}
		return nil
	}
	manager := mustManager(t, RuntimeFactoryFunc(func(context.Context, config.Config) (ManagedRuntime, error) {
		return runtime, nil
	}))
	handler, err := NewHandler(HandlerOptions{
		Manager: manager,
		Setup:   fakeSetupAPI{},
		TraceIDGenerator: func() string {
			return "trace-control-test"
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSetupRouteContract(t, handler)
	assertResponse(t, handler, http.MethodGet, "/health/live", http.StatusOK, "ok")
	assertResponse(t, handler, http.MethodGet, "/health/ready", http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
	assertResponse(t, handler, http.MethodGet, "/api/setup/status", http.StatusOK, "setupRequired")
	applicationUnavailable := performControlRequest(handler, http.MethodGet, "/api/v1/tracks")
	if applicationUnavailable.Code != http.StatusServiceUnavailable || applicationUnavailable.Header().Get("Retry-After") != "5" {
		t.Fatalf("unconfigured application response = %d headers=%v body=%q", applicationUnavailable.Code, applicationUnavailable.Header(), applicationUnavailable.Body.String())
	}
	if contentType := applicationUnavailable.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/problem+json") {
		t.Fatalf("unconfigured content type = %q", contentType)
	}
	adminRedirect := performControlRequest(handler, http.MethodGet, "/admin")
	if adminRedirect.Code != http.StatusFound || adminRedirect.Header().Get("Location") != "/admin/" {
		t.Fatalf("admin redirect = %d location=%q", adminRedirect.Code, adminRedirect.Header().Get("Location"))
	}
	assertResponse(t, handler, http.MethodGet, "/admin/app.js", http.StatusServiceUnavailable, "ADMIN_WEB_UNAVAILABLE")

	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, handler, http.MethodGet, "/api/v1/tracks", http.StatusOK, "configured runtime")
	assertResponse(t, handler, http.MethodGet, "/health/ready", http.StatusOK, "ready")

	dependencyReady.Store(false)
	assertResponse(t, handler, http.MethodGet, "/health/ready", http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
	if snapshot := manager.Status(); snapshot.Phase != RuntimePhaseReady {
		t.Fatalf("transient dependency failure changed lifecycle phase: %#v", snapshot)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestControlHandlerUsesRegisteredAdminRoutesWithoutRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := mustManager(t, RuntimeFactoryFunc(func(context.Context, config.Config) (ManagedRuntime, error) {
		return nil, errors.New("factory must not be called")
	}))
	handler, err := NewHandler(HandlerOptions{
		Manager: manager,
		Setup:   fakeSetupAPI{},
		RegisterAdminRoutes: func(engine *gin.Engine) {
			engine.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/admin/") })
			engine.GET("/admin/*path", func(c *gin.Context) { c.String(http.StatusOK, "admin asset") })
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResponse(t, handler, http.MethodGet, "/admin/index.html", http.StatusOK, "admin asset")
}

func TestControlForwardingCountsManagedRuntimeRequestOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics, err := runtimemetrics.New(runtimemetrics.Options{SampleLimit: 32, SampleInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.Close()
	inner, err := httpserver.New(httpserver.Options{
		Metrics: metrics,
		RegisterRoutes: func(engine *gin.Engine) {
			engine.GET("/api/v1/measured", func(c *gin.Context) { c.String(http.StatusOK, "measured") })
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakeRuntime("unused")
	runtime.handler = inner
	manager := mustManager(t, RuntimeFactoryFunc(func(context.Context, config.Config) (ManagedRuntime, error) {
		return runtime, nil
	}))
	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	outer, err := NewHandler(HandlerOptions{Manager: manager, Setup: fakeSetupAPI{}})
	if err != nil {
		t.Fatal(err)
	}
	response := performControlRequest(outer, http.MethodGet, "/api/v1/measured")
	if response.Code != http.StatusOK || response.Body.String() != "measured" {
		t.Fatalf("forwarded response = %d %q", response.Code, response.Body.String())
	}
	snapshot := metrics.Snapshot().Requests
	if snapshot.Total != 1 || snapshot.InFlight != 0 || snapshot.Sampled != 1 {
		t.Fatalf("forwarded request metrics = %+v", snapshot)
	}
}

func TestControlReadinessRequiresSynchronizedWorker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime := newFakeRuntime("configured runtime")
	manager := mustManager(t, RuntimeFactoryFunc(func(context.Context, config.Config) (ManagedRuntime, error) {
		return runtime, nil
	}))
	cfg := runtimeConfig(1)
	if err := manager.Initialize(context.Background(), cfg, setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	monitor := &fakeWorkerMonitor{snapshot: workerstatus.Snapshot{
		Mode: "external", State: "RUNNING", Responsive: true,
		Synchronized: true, Available: true,
	}}
	handler, err := NewHandler(HandlerOptions{Manager: manager, WorkerStatus: monitor})
	if err != nil {
		t.Fatal(err)
	}
	ready := performControlRequest(handler, http.MethodGet, "/health/ready")
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"worker":{"mode":"external"`) || !strings.Contains(ready.Body.String(), `"reason":null`) {
		t.Fatalf("ready response = %d %q", ready.Code, ready.Body.String())
	}
	if monitor.fingerprint != workerstatus.ConfigurationFingerprint(cfg) {
		t.Fatalf("worker fingerprint = %q", monitor.fingerprint)
	}

	monitor.snapshot = workerstatus.Unavailable()
	unavailable := performControlRequest(handler, http.MethodGet, "/health/ready")
	if unavailable.Code != http.StatusServiceUnavailable || !strings.Contains(unavailable.Body.String(), `"reason":"worker_unavailable"`) {
		t.Fatalf("unavailable response = %d %q", unavailable.Code, unavailable.Body.String())
	}
}

func TestControlHandlerRegistersRealSetupServiceWithoutEnvironmentFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := mustManager(t, RuntimeFactoryFunc(func(context.Context, config.Config) (ManagedRuntime, error) {
		return nil, errors.New("factory must not be called")
	}))
	configured := false
	service, err := setup.NewService(setup.Options{
		RootDirectory:       t.TempDir(),
		ConfigurationPath:   "missing.env",
		ConfiguredAtStartup: &configured,
		Runtime:             manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(HandlerOptions{Manager: manager, Setup: service})
	if err != nil {
		t.Fatal(err)
	}
	response := performControlRequest(handler, http.MethodGet, "/api/setup/status")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"setupRequired":true`) || !strings.Contains(response.Body.String(), `"phase":"SETUP_REQUIRED"`) {
		t.Fatalf("actual setup service status = %d %q", response.Code, response.Body.String())
	}
	assertResponse(t, handler, http.MethodGet, "/api/v1/tracks", http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
}

func assertSetupRouteContract(t *testing.T, engine *gin.Engine) {
	t.Helper()
	expected := map[string]struct{}{
		"GET /api/setup/status":              {},
		"POST /api/setup/http/test":          {},
		"POST /api/setup/paths/test":         {},
		"POST /api/setup/database/test":      {},
		"POST /api/setup/storage/test":       {},
		"POST /api/setup/media/test":         {},
		"POST /api/setup/source/test":        {},
		"POST /api/setup/administrator/test": {},
		"POST /api/setup/complete":           {},
	}
	actual := make(map[string]struct{})
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/setup/") {
			actual[route.Method+" "+route.Path] = struct{}{}
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("setup route count = %d, want %d: %#v", len(actual), len(expected), actual)
	}
	for route := range expected {
		if _, ok := actual[route]; !ok {
			t.Errorf("missing setup route %s", route)
		}
	}
}

func assertResponse(t *testing.T, handler http.Handler, method, path string, status int, bodyContains string) {
	t.Helper()
	response := performControlRequest(handler, method, path)
	if response.Code != status || !strings.Contains(response.Body.String(), bodyContains) {
		t.Fatalf("%s %s = %d %q, want %d containing %q", method, path, response.Code, response.Body.String(), status, bodyContains)
	}
}

func performControlRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

type fakeSetupAPI struct{}

type fakeWorkerMonitor struct {
	snapshot    workerstatus.Snapshot
	fingerprint string
}

func (monitor *fakeWorkerMonitor) Status(_ context.Context, fingerprint string) workerstatus.Snapshot {
	monitor.fingerprint = fingerprint
	return monitor.snapshot
}

func (fakeSetupAPI) Status() setup.StatusResponse {
	return setup.StatusResponse{
		SetupRequired:       true,
		ConfigurationSource: setup.RuntimeSourceSetup,
		Runtime: setup.RuntimeStatusResponse{
			Phase:  RuntimePhaseSetupRequired,
			Source: setup.RuntimeSourceSetup,
		},
	}
}

func (fakeSetupAPI) RequireSetup() error { return nil }

func (fakeSetupAPI) TestHTTP(context.Context, setup.HTTPInput) (setup.OKResponse, error) {
	return setup.OKResponse{OK: true}, nil
}

func (fakeSetupAPI) TestPaths(context.Context, setup.PathsInput) (setup.PathsTestResponse, error) {
	return setup.PathsTestResponse{OK: true}, nil
}

func (fakeSetupAPI) TestDatabase(context.Context, setup.DatabaseTestInput) (setup.DatabaseTestResponse, error) {
	return setup.DatabaseTestResponse{OK: true}, nil
}

func (fakeSetupAPI) TestStorage(context.Context, setup.StorageInput) (setup.StorageTestResponse, error) {
	return setup.StorageTestResponse{OK: true}, nil
}

func (fakeSetupAPI) TestMedia(context.Context, setup.MediaInput) (setup.MediaTestResponse, error) {
	return setup.MediaTestResponse{OK: true}, nil
}

func (fakeSetupAPI) TestSource(context.Context, setup.SourceInput) (setup.SourceTestResponse, error) {
	return setup.SourceTestResponse{OK: true}, nil
}

func (fakeSetupAPI) TestAdministrator(context.Context, setup.AdministratorInput) (setup.OKResponse, error) {
	return setup.OKResponse{OK: true}, nil
}

func (fakeSetupAPI) Complete(context.Context, setup.SetupInput, string) (setup.CompletionResponse, error) {
	return setup.CompletionResponse{Configured: true}, nil
}
