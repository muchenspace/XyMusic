package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/platform/workerstatus"
)

func TestWorkerWaitsForConfigurationAndPublishesStoppedState(t *testing.T) {
	root := t.TempDir()
	configurationPath := filepath.Join(root, ".env")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		result <- runWorkerProcess(ctx, logger, root, configurationPath)
	}()

	statusPath := configurationPath + ".worker-status"
	waitForWorkerState(t, statusPath, "WAITING_FOR_CONFIGURATION")
	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("exit code=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	waitForWorkerState(t, statusPath, "STOPPED")
}

func TestWorkerRuntimeOptionsWireProcessLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	options := workerRuntimeOptions("worker-root", logger)
	if options.RootDirectory != "worker-root" || !options.StartBackground || options.Logger != logger {
		t.Fatalf("worker runtime options = %+v", options)
	}
}

func waitForWorkerState(t *testing.T, path, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil {
			var document workerstatus.Document
			if json.Unmarshal(content, &document) == nil && document.State == state {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker state %q was not published", state)
}
