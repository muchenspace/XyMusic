package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/config"
	"xymusic/server/internal/control"
	"xymusic/server/internal/modules/adminweb"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/workerstatus"
)

// ControlRuntimeOptions carries the already-resolved process inputs needed to
// assemble the listener-lifetime control plane.
type ControlRuntimeOptions struct {
	RootDirectory      string
	ConfigurationPath  string
	Configured         bool
	AdminWebDirectory  string
	Listeners          config.HTTP
	CORS               httpserver.CORSConfig
	TrustedProxies     []string
	AllowedHosts       []string
	ApplicationVersion string
	ProcessStartedAt   time.Time
	Logger             *slog.Logger
}

// ControlRuntime bundles the assembled control manager and HTTP handler.
type ControlRuntime struct {
	Manager *control.Manager
	Handler *gin.Engine
}

// ControlRuntimeError names the assembly step that failed so the caller can
// preserve the original per-step log messages.
type ControlRuntimeError struct {
	Operation string
	Err       error
}

func (failure *ControlRuntimeError) Error() string {
	return failure.Operation + ": " + failure.Err.Error()
}
func (failure *ControlRuntimeError) Unwrap() error { return failure.Err }

// NewControlRuntime assembles the control plane exactly as the previous inline
// implementation did, preserving order, options, and error operations.
func NewControlRuntime(options ControlRuntimeOptions) (*ControlRuntime, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// Resolve the configuration path exactly as the setup service used to do
	// before it became an injected dependency: an empty root means the
	// executable directory, an empty path means "<root>/.env", and a relative
	// path is joined to the resolved root.
	rootDirectory := strings.TrimSpace(options.RootDirectory)
	if rootDirectory == "" {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return nil, &ControlRuntimeError{Operation: "locate executable root", Err: executableErr}
		}
		rootDirectory = filepath.Dir(executable)
	}
	absoluteRoot, err := filepath.Abs(rootDirectory)
	if err != nil {
		return nil, &ControlRuntimeError{Operation: "resolve control runtime root", Err: err}
	}
	configurationPath := strings.TrimSpace(options.ConfigurationPath)
	if configurationPath == "" {
		configurationPath = filepath.Join(absoluteRoot, ".env")
	} else if !filepath.IsAbs(configurationPath) {
		configurationPath = filepath.Join(absoluteRoot, configurationPath)
	}
	workerMonitor, err := workerstatus.New(workerstatus.Options{Path: configurationPath + ".worker-status"})
	if err != nil {
		return nil, &ControlRuntimeError{Operation: "configure worker status monitor", Err: err}
	}
	settingsStore := config.NewStore(configurationPath)
	var manager *control.Manager
	factory := control.RuntimeFactoryFunc(func(buildContext context.Context, candidate config.Config) (control.ManagedRuntime, error) {
		runtime, err := Bootstrap(buildContext, candidate, Options{
			RootDirectory: options.RootDirectory, StartBackground: false, Logger: logger,
			Administration: &AdministrationOptions{
				Runtime: manager, Store: settingsStore, Worker: workerMonitor,
				ConfigurationPath: configurationPath,
				IPv4ListenerHost:  options.Listeners.IPv4Host, IPv4ListenerPort: options.Listeners.IPv4Port,
				IPv6ListenerHost: options.Listeners.IPv6Host, IPv6ListenerPort: options.Listeners.IPv6Port,
				ApplicationVersion: options.ApplicationVersion, StartedAt: options.ProcessStartedAt,
			},
		})
		if err != nil {
			return nil, err
		}
		return control.RuntimeAdapter{
			Handler:   runtime.Handler,
			ReadyFunc: runtime.Ready,
			CloseFunc: runtime.CloseContext,
		}, nil
	})
	source := setup.RuntimeSourceSetup
	if options.Configured {
		source = setup.RuntimeSourceManaged
	}
	manager, err = control.NewManager(control.ManagerOptions{Source: source, Factory: factory})
	if err != nil {
		return nil, &ControlRuntimeError{Operation: "create runtime manager", Err: err}
	}
	assemblyFailed := true
	defer func() {
		if assemblyFailed {
			closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := manager.Close(closeContext); err != nil {
				logger.Error("close application runtime", "error", err)
			}
		}
	}()
	setupService, err := setup.NewService(setup.Options{
		RootDirectory: options.RootDirectory,
		ActualListener: setup.ActualListener{
			IPv4: setup.ListenerAddress{Host: options.Listeners.IPv4Host, Port: options.Listeners.IPv4Port},
			IPv6: setup.ListenerAddress{Host: options.Listeners.IPv6Host, Port: options.Listeners.IPv6Port},
		},
		ConfiguredAtStartup: &options.Configured,
		Runtime:             manager,
		Store:               setup.NewFileConfigurationRepository(configurationPath),
		Databases:           setup.ProductionDatabaseFactory{},
		MediaStorage:        setup.ProductionMediaStorageFactory{},
		MediaTool:           setup.CommandMediaTool{},
		ListenerProbe:       setup.NetworkListenerProbe{},
		SourceValidator:     setup.OSSourceValidator{},
		Passwords:           setup.SecurityPasswordHasher{},
		SecretGenerator:     setup.ProductionSecretGenerator,
		Executables:         setup.ProductionExecutableLocator{},
		Files:               setup.ProductionFileProbe{},
		MigrationProbe:      setup.ProductionMigrationProbe{},
	})
	if err != nil {
		return nil, &ControlRuntimeError{Operation: "create setup service", Err: err}
	}
	adminAssets, err := adminweb.New(options.AdminWebDirectory)
	if err != nil {
		return nil, &ControlRuntimeError{Operation: "configure admin web assets", Err: err}
	}
	handler, err := control.NewHandler(control.HandlerOptions{
		Manager: manager, Setup: setupService, RegisterAdminRoutes: adminAssets.Register,
		CORS: options.CORS, RequestLimits: httpserver.DefaultRequestLimits(), TrustedProxies: options.TrustedProxies,
		AllowedHosts: options.AllowedHosts, WorkerStatus: workerMonitor,
	})
	if err != nil {
		return nil, &ControlRuntimeError{Operation: "create control HTTP application", Err: err}
	}
	assemblyFailed = false
	return &ControlRuntime{Manager: manager, Handler: handler}, nil
}
