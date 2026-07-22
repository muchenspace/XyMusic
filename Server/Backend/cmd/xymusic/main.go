package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/control"
	"xymusic/server/internal/modules/adminweb"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/workerstatus"
)

const (
	applicationVersion     = "0.1.2"
	firstRunIPv4Host       = "0.0.0.0"
	firstRunIPv6Host       = "::"
	firstRunListenerPort   = 3000
	maximumRequestReadTime = 30 * time.Minute
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	root, err := defaultRootDirectory()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	flags := flag.NewFlagSet("xymusic", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(arguments); errors.Is(err, flag.ErrHelp) {
		return 0
	} else if err != nil {
		return 2
	}
	remaining := flags.Args()
	command := "supervise"
	if len(remaining) > 0 {
		command = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 || (command != "supervise" && command != "serve" && command != "worker" && command != "migrate") {
		fmt.Fprintln(os.Stderr, "usage: xymusic [supervise|serve|worker|migrate]")
		return 2
	}
	absRoot := root
	configurationPath := filepath.Join(absRoot, ".env")
	store := config.NewStore(configurationPath)
	if err := store.Recover(); err != nil {
		fmt.Fprintln(os.Stderr, "recover configuration:", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch command {
	case "supervise":
		return runSupervisorProcess(ctx, logger, configurationPath)
	case "worker":
		return runWorkerProcess(ctx, logger, absRoot, configurationPath)
	}
	cfg, err := store.Load()
	configured := err == nil
	if err != nil && !errors.Is(err, config.ErrNotConfigured) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if command == "migrate" {
		if !configured {
			fmt.Fprintln(os.Stderr, config.ErrNotConfigured)
			return 1
		}
		resolved, err := config.ResolveRuntime(cfg, absRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return runMigrate(ctx, logger, resolved)
	}
	return runServer(ctx, logger, cfg, configured, absRoot, configurationPath)
}

func runMigrate(ctx context.Context, logger *slog.Logger, cfg config.Config) int {
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		return 1
	}
	defer pool.Close()
	if err := database.RunMigrations(ctx, pool.Pool, cfg.Paths.MigrationsDirectory); err != nil {
		logger.Error("database migration failed", "error", err)
		return 1
	}
	logger.Info("database migrations completed")
	return 0
}

func runServer(
	ctx context.Context,
	logger *slog.Logger,
	raw config.Config,
	configured bool,
	root string,
	configurationPath string,
) int {
	processStartedAt := time.Now()
	listeners, cors, allowedHosts := firstRunHTTPDefaults()
	adminDirectory := filepath.Join(root, config.DefaultAdminWebDirectory)
	trustedProxies := []string(nil)
	environment := config.Development
	if configured {
		resolved, err := config.ResolveRuntime(raw, root)
		if err != nil {
			logger.Error("resolve runtime configuration", "error", err)
			return 1
		}
		listeners = resolved.HTTP
		adminDirectory = resolved.Paths.AdminWebDirectory
		trustedProxies = append([]string(nil), resolved.HTTP.TrustedProxyAddresses...)
		environment = resolved.Environment
		cors = httpserver.UnrestrictedCORSConfig()
		allowedHosts = nil
	}

	workerMonitor, err := workerstatus.New(workerstatus.Options{Path: configurationPath + ".worker-status"})
	if err != nil {
		logger.Error("configure worker status monitor", "error", err)
		return 1
	}
	settingsStore := config.NewStore(configurationPath)
	var manager *control.Manager
	factory := control.RuntimeFactoryFunc(func(buildContext context.Context, candidate config.Config) (control.ManagedRuntime, error) {
		runtime, err := app.Bootstrap(buildContext, candidate, app.Options{
			RootDirectory: root, StartBackground: false, Logger: logger,
			Administration: &app.AdministrationOptions{
				Runtime: manager, Store: settingsStore, Worker: workerMonitor,
				ConfigurationPath: configurationPath,
				IPv4ListenerHost:  listeners.IPv4Host, IPv4ListenerPort: listeners.IPv4Port,
				IPv6ListenerHost: listeners.IPv6Host, IPv6ListenerPort: listeners.IPv6Port,
				ApplicationVersion: applicationVersion, StartedAt: processStartedAt,
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
	if configured {
		source = setup.RuntimeSourceManaged
	}
	manager, err = control.NewManager(control.ManagerOptions{Source: source, Factory: factory})
	if err != nil {
		logger.Error("create runtime manager", "error", err)
		return 1
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := manager.Close(closeContext); err != nil {
			logger.Error("close application runtime", "error", err)
		}
	}()
	setupService, err := setup.NewService(setup.Options{
		RootDirectory:     root,
		ConfigurationPath: configurationPath,
		ActualListener: setup.ActualListener{
			IPv4: setup.ListenerAddress{Host: listeners.IPv4Host, Port: listeners.IPv4Port},
			IPv6: setup.ListenerAddress{Host: listeners.IPv6Host, Port: listeners.IPv6Port},
		},
		ConfiguredAtStartup: &configured,
		Runtime:             manager,
	})
	if err != nil {
		logger.Error("create setup service", "error", err)
		return 1
	}
	adminAssets, err := adminweb.New(adminDirectory)
	if err != nil {
		logger.Error("configure admin web assets", "error", err)
		return 1
	}
	handler, err := control.NewHandler(control.HandlerOptions{
		Manager: manager, Setup: setupService, RegisterAdminRoutes: adminAssets.Register,
		CORS: cors, RequestLimits: httpserver.DefaultRequestLimits(), TrustedProxies: trustedProxies,
		AllowedHosts: allowedHosts, WorkerStatus: workerMonitor,
	})
	if err != nil {
		logger.Error("create control HTTP application", "error", err)
		return 1
	}
	if configured {
		bootstrapContext, cancelBootstrap := context.WithTimeout(ctx, 45*time.Second)
		err := manager.Initialize(bootstrapContext, raw, setup.RuntimeSourceManaged)
		cancelBootstrap()
		if err != nil {
			logger.Error("runtime initialization failed; control listener remains available", "error", err)
		}
	}

	type listenerSpec struct {
		network string
		host    string
		port    int
	}
	specs := []listenerSpec{
		{network: "tcp4", host: listeners.IPv4Host, port: listeners.IPv4Port},
		{network: "tcp6", host: listeners.IPv6Host, port: listeners.IPv6Port},
	}
	servers := make([]*http.Server, 0, len(specs))
	networkListeners := make([]net.Listener, 0, len(specs))
	for _, spec := range specs {
		address := net.JoinHostPort(spec.host, strconv.Itoa(spec.port))
		listener, err := (&net.ListenConfig{}).Listen(ctx, spec.network, address)
		if err != nil {
			for _, opened := range networkListeners {
				_ = opened.Close()
			}
			logger.Error("open HTTP listener", "network", spec.network, "address", address, "error", err)
			return 1
		}
		networkListeners = append(networkListeners, listener)
		servers = append(servers, &http.Server{
			Addr:              address,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       maximumRequestReadTime,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    1 << 20,
		})
	}
	serveErrors := make(chan error, len(servers))
	for index, server := range servers {
		listener := networkListeners[index]
		go func(server *http.Server, listener net.Listener, network string) {
			logger.Info("server started", "network", network, "address", server.Addr, "environment", environment, "configured", configured)
			serveErrors <- server.Serve(listener)
		}(server, listener, specs[index].network)
	}

	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			for _, server := range servers {
				_ = server.Close()
			}
			return 1
		}
		return 0
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, server := range servers {
			if err := server.Shutdown(shutdownContext); err != nil {
				logger.Error("graceful shutdown failed", "address", server.Addr, "error", err)
				_ = server.Close()
				return 1
			}
		}
		logger.Info("server stopped")
		return 0
	}
}

func firstRunHTTPDefaults() (config.HTTP, httpserver.CORSConfig, []string) {
	return config.HTTP{
		IPv4Host: firstRunIPv4Host,
		IPv4Port: firstRunListenerPort,
		IPv6Host: firstRunIPv6Host,
		IPv6Port: firstRunListenerPort,
		Host:     firstRunIPv4Host,
		Port:     firstRunListenerPort,
	}, httpserver.UnrestrictedCORSConfig(), nil
}

func defaultRootDirectory() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	return filepath.Dir(executable), nil
}
