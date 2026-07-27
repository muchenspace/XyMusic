package main

import (
	"context"
	"log/slog"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/control"
	"xymusic/server/internal/modules/setup"
)

const (
	managedRuntimeInitializationTimeout = 45 * time.Second
	managedRuntimeRetryInitialDelay     = 5 * time.Second
	managedRuntimeRetryMaxDelay         = 30 * time.Second
)

type managedRuntimeRetryPolicy struct {
	initialDelay   time.Duration
	maxDelay       time.Duration
	attemptTimeout time.Duration
}

func retryManagedRuntime(
	ctx context.Context,
	manager *control.Manager,
	raw config.Config,
	logger *slog.Logger,
) {
	retryManagedRuntimeWithPolicy(ctx, manager, raw, logger, managedRuntimeRetryPolicy{
		initialDelay:   managedRuntimeRetryInitialDelay,
		maxDelay:       managedRuntimeRetryMaxDelay,
		attemptTimeout: managedRuntimeInitializationTimeout,
	})
}

func retryManagedRuntimeWithPolicy(
	ctx context.Context,
	manager *control.Manager,
	raw config.Config,
	logger *slog.Logger,
	policy managedRuntimeRetryPolicy,
) {
	if ctx == nil || manager == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	policy = normalizeManagedRuntimeRetryPolicy(policy)

	delay := policy.initialDelay
	for attempt := 1; ; attempt++ {
		if !waitForManagedRuntimeRetry(ctx, delay) {
			return
		}
		if manager.Status().Phase == control.RuntimePhaseReady {
			return
		}

		attemptContext, cancel := context.WithTimeout(ctx, policy.attemptTimeout)
		err := manager.Initialize(attemptContext, raw, setup.RuntimeSourceManaged)
		cancel()
		if err == nil {
			logger.Info("runtime initialization recovered", "attempt", attempt)
			return
		}
		if ctx.Err() != nil {
			return
		}

		nextDelay := nextManagedRuntimeRetryDelay(delay, policy.maxDelay)
		logger.Warn("runtime initialization retry failed", "attempt", attempt, "retryIn", nextDelay, "error", err)
		delay = nextDelay
	}
}

func normalizeManagedRuntimeRetryPolicy(policy managedRuntimeRetryPolicy) managedRuntimeRetryPolicy {
	if policy.initialDelay <= 0 {
		policy.initialDelay = managedRuntimeRetryInitialDelay
	}
	if policy.maxDelay < policy.initialDelay {
		policy.maxDelay = policy.initialDelay
	}
	if policy.attemptTimeout <= 0 {
		policy.attemptTimeout = managedRuntimeInitializationTimeout
	}
	return policy
}

func waitForManagedRuntimeRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextManagedRuntimeRetryDelay(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}
