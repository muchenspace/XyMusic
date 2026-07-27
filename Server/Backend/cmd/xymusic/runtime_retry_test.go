package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/control"
	"xymusic/server/internal/modules/setup"
)

func TestRetryManagedRuntimeRecoversAfterTransientInitializationFailure(t *testing.T) {
	var available atomic.Bool
	var builds atomic.Int32
	manager, err := control.NewManager(control.ManagerOptions{
		Source: setup.RuntimeSourceManaged,
		Factory: control.RuntimeFactoryFunc(func(context.Context, config.Config) (control.ManagedRuntime, error) {
			builds.Add(1)
			if !available.Load() {
				return nil, errors.New("dependencies unavailable")
			}
			return control.RuntimeAdapter{
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
				ReadyFunc: func(context.Context) error {
					return nil
				},
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Initialize(context.Background(), config.Config{}, setup.RuntimeSourceManaged); err == nil {
		t.Fatal("expected initial runtime initialization to fail")
	}
	if status := manager.Status(); status.Phase != control.RuntimePhaseFailed {
		t.Fatalf("failed initialization status = %#v", status)
	}

	available.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		retryManagedRuntimeWithPolicy(ctx, manager, config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), managedRuntimeRetryPolicy{
			initialDelay:   time.Millisecond,
			maxDelay:       4 * time.Millisecond,
			attemptTimeout: time.Second,
		})
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if manager.Status().Phase == control.RuntimePhaseReady {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if status := manager.Status(); status.Phase != control.RuntimePhaseReady {
		t.Fatalf("runtime did not recover: %#v", status)
	}
	if builds.Load() != 2 {
		t.Fatalf("runtime build count = %d, want initial failure plus one retry", builds.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime retry loop did not stop")
	}
}
