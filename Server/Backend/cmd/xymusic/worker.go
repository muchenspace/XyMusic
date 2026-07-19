package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/platform/workerstatus"
)

const permanentWorkerFailureExitCode = 78

func runWorkerProcess(
	ctx context.Context,
	logger *slog.Logger,
	root string,
	configurationPath string,
) int {
	store := config.NewStore(configurationPath)
	statusPath := configurationPath + ".worker-status"
	var runtime *app.Runtime
	var fingerprint string
	defer func() {
		if runtime == nil {
			_ = writeWorkerDocument(context.Background(), statusPath, "STOPPED", "")
			return
		}
		_ = writeWorkerDocument(context.Background(), statusPath, "STOPPING", fingerprint)
		closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := runtime.CloseContext(closeContext); err != nil {
			logger.Error("worker runtime close failed", "error", err)
		}
		cancel()
		_ = writeWorkerDocument(context.Background(), statusPath, "STOPPED", fingerprint)
	}()

	configurationTicker := time.NewTicker(2 * time.Second)
	defer configurationTicker.Stop()
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()
	waitingLogged := false
	for {
		if ctx.Err() != nil {
			return 0
		}
		candidate, err := store.Load()
		if errors.Is(err, config.ErrNotConfigured) {
			if runtime == nil {
				if !waitingLogged {
					logger.Info("worker waiting for configuration", "path", configurationPath)
					waitingLogged = true
				}
				_ = writeWorkerDocument(ctx, statusPath, "WAITING_FOR_CONFIGURATION", "")
			}
		} else if err != nil {
			if runtime == nil {
				logger.Error("worker configuration load failed", "error", err)
				_ = writeWorkerDocument(ctx, statusPath, "CONFIGURATION_ERROR", "")
				return 1
			}
			logger.Warn("worker configuration reload failed", "error", err)
		} else {
			waitingLogged = false
			candidateFingerprint := workerstatus.ConfigurationFingerprint(candidate)
			if runtime == nil || candidateFingerprint != fingerprint {
				replacement, buildErr := buildWorkerRuntime(ctx, candidate, root, logger)
				if buildErr != nil {
					if runtime == nil {
						logger.Error("worker runtime initialization failed", "error", buildErr)
						_ = writeWorkerDocument(ctx, statusPath, "CONFIGURATION_ERROR", "")
						if database.IsPermanentMigrationError(buildErr) {
							return permanentWorkerFailureExitCode
						}
						return 1
					}
					logger.Warn("worker runtime candidate rejected", "error", buildErr)
				} else {
					previous := runtime
					runtime = replacement
					fingerprint = candidateFingerprint
					if previous != nil {
						closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						if closeErr := previous.CloseContext(closeContext); closeErr != nil {
							logger.Error("previous worker runtime close failed", "error", closeErr)
						}
						cancel()
						logger.Info("worker configuration reloaded", "fingerprint", fingerprint[:12])
					} else {
						logger.Info("worker started", "fingerprint", fingerprint[:12])
					}
					_ = writeWorkerDocument(ctx, statusPath, "RUNNING", fingerprint)
				}
			}
		}

		select {
		case <-ctx.Done():
			return 0
		case <-heartbeatTicker.C:
			if runtime != nil {
				if err := writeWorkerDocument(ctx, statusPath, "RUNNING", fingerprint); err != nil {
					logger.Warn("worker status write failed", "error", err)
				}
			}
		case <-configurationTicker.C:
		}
	}
}

func buildWorkerRuntime(
	ctx context.Context,
	candidate config.Config,
	root string,
	logger *slog.Logger,
) (*app.Runtime, error) {
	buildContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return app.Bootstrap(buildContext, candidate, workerRuntimeOptions(root, logger))
}

func workerRuntimeOptions(root string, logger *slog.Logger) app.Options {
	return app.Options{
		RootDirectory:   root,
		StartBackground: true,
		Logger:          logger,
	}
}

func writeWorkerDocument(ctx context.Context, path, state, fingerprint string) error {
	return workerstatus.WriteDocument(ctx, path, workerstatus.Document{
		PID: os.Getpid(), State: state, Fingerprint: fingerprint,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}
