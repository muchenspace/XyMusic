package sse

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestBroadcasterSharesPollerAndReplaysLatestFrame(t *testing.T) {
	broadcaster := MustNew(Options{MaxTopics: 1})
	defer broadcaster.Close()
	var loads atomic.Int32
	options := TopicOptions{
		Load: func(context.Context) (any, error) {
			loads.Add(1)
			return map[string]any{"active": 2}, nil
		},
		Fingerprint:  func(any) string { return "state-1" },
		Payload:      func(value any) any { return value },
		PollInterval: time.Second,
	}
	first, err := broadcaster.Subscribe(context.Background(), "jobs", options)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	firstFrame := receiveFrame(t, first.Frames())
	if string(firstFrame) != "data: {\"active\":2}\n\n" {
		t.Fatalf("first frame=%q", firstFrame)
	}
	second, err := broadcaster.Subscribe(context.Background(), "jobs", options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondFrame := receiveFrame(t, second.Frames())
	if string(secondFrame) != string(firstFrame) {
		t.Fatalf("replayed frame=%q", secondFrame)
	}
	if loads.Load() != 1 {
		t.Fatalf("loads=%d", loads.Load())
	}
}

func TestBroadcasterTurnsPollingFailureIntoSSEErrorFrame(t *testing.T) {
	broadcaster := MustNew(Options{MaxTopics: 1})
	defer broadcaster.Close()
	subscription, err := broadcaster.Subscribe(context.Background(), "jobs", TopicOptions{
		Load:         func(context.Context) (any, error) { return nil, errors.New("database unavailable") },
		Fingerprint:  func(any) string { return "" },
		Payload:      func(any) any { return nil },
		PollInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	frame := string(receiveFrame(t, subscription.Frames()))
	if !strings.HasPrefix(frame, "event: error\ndata: ") || !strings.Contains(frame, `"retrying":true`) {
		t.Fatalf("error frame=%q", frame)
	}
}

func TestBroadcasterCapacityReturnsRetryableDependencyError(t *testing.T) {
	broadcaster := MustNew(Options{MaxSubscribers: 1, MaxTopics: 1})
	defer broadcaster.Close()
	options := TopicOptions{
		Load: func(context.Context) (any, error) {
			return 1, nil
		},
		Fingerprint:  func(any) string { return "one" },
		Payload:      func(value any) any { return value },
		PollInterval: time.Second,
	}
	first, err := broadcaster.Subscribe(context.Background(), "jobs", options)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	_, err = broadcaster.Subscribe(context.Background(), "jobs", options)
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeDependencyUnavailable ||
		applicationError.Metadata["retryAfterSeconds"] != 5 {
		t.Fatalf("capacity error=%#v", err)
	}
}

func receiveFrame(t *testing.T, frames <-chan []byte) []byte {
	t.Helper()
	select {
	case frame, open := <-frames:
		if !open {
			t.Fatal("SSE subscription closed before a frame was delivered")
		}
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE frame")
		return nil
	}
}
