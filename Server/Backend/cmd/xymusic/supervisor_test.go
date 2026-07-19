package main

import (
	"testing"
	"time"
)

func TestWorkerRestartDelayUsesBoundedExponentialBackoff(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for index, expected := range want {
		if actual := workerRestartDelay(index + 1); actual != expected {
			t.Fatalf("attempt %d delay=%s want=%s", index+1, actual, expected)
		}
	}
}

func TestNormalizedExitCodeMapsSignalsToFailure(t *testing.T) {
	if normalizedExitCode(-1) != 1 || normalizedExitCode(78) != 78 {
		t.Fatal("exit code normalization changed")
	}
}
