package adminsettings

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/runtimemetrics"
	"xymusic/server/internal/platform/workerstatus"
)

func TestProductionSettingsReadAndDependencyProbes(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production settings probes")
	}
	absolute, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolute).Load()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := config.ResolveRuntime(cfg, filepath.Dir(absolute))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, resolved.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	monitor, err := workerstatus.New(workerstatus.Options{Path: absolute + ".worker-status"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &productionSettingsRuntime{
		config: cfg,
		status: setup.RuntimeSnapshot{Phase: setup.RuntimePhaseReady, Source: setup.RuntimeSourceManaged, Generation: 1},
	}
	metrics, err := runtimemetrics.New(runtimemetrics.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.Close()
	service, err := NewService(ServiceDependencies{
		Database: pool, Runtime: runtime, Store: productionNoopStore{},
		Storage: ProductionStorageFactory{}, MediaTool: setup.CommandMediaTool{}, Worker: monitor,
		Metrics:       metrics,
		RootDirectory: filepath.Dir(absolute), ConfigurationPath: absolute,
		Listener: ListenerDTO{
			IPv4: ListenerAddressDTO{Host: cfg.HTTP.IPv4Host, Port: cfg.HTTP.IPv4Port},
			IPv6: ListenerAddressDTO{Host: cfg.HTTP.IPv6Host, Port: cfg.HTTP.IPv6Port},
		}, ApplicationVersion: "integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings, err := service.Settings(); err != nil || settings.Version != 1 || !settings.Database.PasswordConfigured {
		t.Fatalf("Settings() = %#v, %v", settings, err)
	}
	if result, err := service.TestDatabase(ctx, DatabaseInput{}); err != nil || !result.OK {
		t.Fatalf("TestDatabase() = %#v, %v", result, err)
	}
	if result, err := service.TestStorage(ctx, StorageInput{}); err != nil || !result.OK || !result.BucketExists {
		t.Fatalf("TestStorage() = %#v, %v", result, err)
	}
	if result, err := service.TestMediaTools(ctx, MediaToolsInput{}); err != nil || !result.OK || len(result.Details) != 2 {
		t.Fatalf("TestMediaTools() = %#v, %v", result, err)
	}
	if result, err := service.TestLocalLibrary(ctx, nil); err != nil || !result.OK {
		t.Fatalf("TestLocalLibrary() = %#v, %v", result, err)
	}
	if result, err := service.SystemInformation(ctx); err != nil || result.DatabaseVersion == "" ||
		result.Queues.Total < 0 || result.Metrics.CollectedSince == "" || result.Metrics.Memory.RSSBytes == 0 {
		t.Fatalf("SystemInformation() = %#v, %v", result, err)
	}
}

type productionSettingsRuntime struct {
	config config.Config
	status setup.RuntimeSnapshot
}

func (runtime *productionSettingsRuntime) Status() setup.RuntimeSnapshot { return runtime.status }
func (runtime *productionSettingsRuntime) ActiveConfig() (config.Config, bool) {
	return runtime.config, true
}
func (runtime *productionSettingsRuntime) Initialize(_ context.Context, candidate config.Config, source string) error {
	runtime.config = candidate
	runtime.status.Source = source
	runtime.status.Generation++
	return nil
}

type productionNoopStore struct{}

func (productionNoopStore) Save(config.Config) error { return nil }
