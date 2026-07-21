package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/workerstatus"
)

func TestGinRoutesCoverEveryLegacyAPI(t *testing.T) {
	if os.Getenv("XYMUSIC_REQUIRE_FULL_API_PARITY") != "1" {
		t.Skip("set XYMUSIC_REQUIRE_FULL_API_PARITY=1 for the final 126-endpoint gate")
	}
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Fatal("XYMUSIC_INTEGRATION_ENV is required by the full API parity gate")
	}
	root := projectRoot(t)
	manifestBytes, err := os.ReadFile(filepath.Join(root, "contracts", "legacy-api.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		APIs []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"apis"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	expected := make(map[string]struct{}, len(manifest.APIs))
	for _, route := range manifest.APIs {
		expected[route.Method+" "+route.Path] = struct{}{}
	}

	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	coverageRuntime := &coverageRuntimeController{
		config: cfg,
		status: setup.RuntimeSnapshot{Phase: setup.RuntimePhaseReady, Source: setup.RuntimeSourceManaged, Generation: 1},
	}
	runtime, err := app.Bootstrap(ctx, cfg, app.Options{
		RootDirectory: filepath.Dir(absoluteEnvironmentPath),
		Administration: &app.AdministrationOptions{
			Runtime: coverageRuntime, Store: coverageConfigurationStore{}, Worker: coverageWorkerMonitor{},
			ConfigurationPath: absoluteEnvironmentPath,
			IPv4ListenerHost:  cfg.HTTP.IPv4Host, IPv4ListenerPort: cfg.HTTP.IPv4Port,
			IPv6ListenerHost: cfg.HTTP.IPv6Host, IPv6ListenerPort: cfg.HTTP.IPv6Port,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	actual := make(map[string]struct{})
	for _, route := range runtime.Handler.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/oss/") {
			continue
		}
		if strings.HasPrefix(route.Path, "/api/") || strings.HasPrefix(route.Path, "/health/") {
			actual[route.Method+" "+route.Path] = struct{}{}
		}
	}
	// Setup routes are owned by the listener-lifetime control application so
	// they remain available when the managed business runtime is absent. The
	// public contract is the union of both registered Gin route sets.
	setupEngine := gin.New()
	setup.RegisterRoutes(setupEngine, coverageSetupAPI{})
	for _, route := range setupEngine.Routes() {
		actual[route.Method+" "+route.Path] = struct{}{}
	}
	var differences []string
	for route := range expected {
		if _, ok := actual[route]; !ok {
			differences = append(differences, "missing: "+route)
		}
	}
	for route := range actual {
		if _, ok := expected[route]; !ok {
			differences = append(differences, "unexpected: "+route)
		}
	}
	if len(differences) != 0 {
		sort.Strings(differences)
		t.Fatalf("Gin API coverage differs from the 126-endpoint manifest (%d registered, %d expected):\n%s", len(actual), len(expected), strings.Join(differences, "\n"))
	}
}

type coverageSetupAPI struct{}

type coverageRuntimeController struct {
	config config.Config
	status setup.RuntimeSnapshot
}

func (runtime *coverageRuntimeController) Status() setup.RuntimeSnapshot { return runtime.status }
func (runtime *coverageRuntimeController) ActiveConfig() (config.Config, bool) {
	return runtime.config, true
}
func (runtime *coverageRuntimeController) Initialize(_ context.Context, candidate config.Config, source string) error {
	runtime.config = candidate
	runtime.status.Source = source
	runtime.status.Generation++
	return nil
}

type coverageConfigurationStore struct{}

func (coverageConfigurationStore) Save(config.Config) error { return nil }

type coverageWorkerMonitor struct{}

func (coverageWorkerMonitor) Status(context.Context, string) workerstatus.Snapshot {
	return workerstatus.Snapshot{
		Mode: "external", State: "RUNNING", Responsive: true, Synchronized: true, Available: true,
	}
}

func (coverageSetupAPI) Status() setup.StatusResponse { return setup.StatusResponse{} }
func (coverageSetupAPI) RequireSetup() error          { return nil }
func (coverageSetupAPI) TestHTTP(context.Context, setup.HTTPInput) (setup.OKResponse, error) {
	return setup.OKResponse{}, nil
}
func (coverageSetupAPI) TestPaths(context.Context, setup.PathsInput) (setup.PathsTestResponse, error) {
	return setup.PathsTestResponse{}, nil
}
func (coverageSetupAPI) TestDatabase(context.Context, setup.DatabaseTestInput) (setup.DatabaseTestResponse, error) {
	return setup.DatabaseTestResponse{}, nil
}
func (coverageSetupAPI) TestStorage(context.Context, setup.StorageInput) (setup.StorageTestResponse, error) {
	return setup.StorageTestResponse{}, nil
}
func (coverageSetupAPI) TestMedia(context.Context, setup.MediaInput) (setup.MediaTestResponse, error) {
	return setup.MediaTestResponse{}, nil
}
func (coverageSetupAPI) TestSource(context.Context, setup.SourceInput) (setup.SourceTestResponse, error) {
	return setup.SourceTestResponse{}, nil
}
func (coverageSetupAPI) TestAdministrator(context.Context, setup.AdministratorInput) (setup.OKResponse, error) {
	return setup.OKResponse{}, nil
}
func (coverageSetupAPI) Complete(context.Context, setup.SetupInput, string) (setup.CompletionResponse, error) {
	return setup.CompletionResponse{}, nil
}
