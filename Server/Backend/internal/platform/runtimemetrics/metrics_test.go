package runtimemetrics

import (
	"testing"
	"time"
)

func TestCollectorPreservesLegacySnapshotFieldsAndStatistics(t *testing.T) {
	collector, err := New(Options{SampleLimit: 32, SampleInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()

	collector.RequestStarted()
	collector.RequestStarted()
	collector.RequestFinished(200, 100*time.Millisecond)
	collector.RequestFinished(503, 200*time.Millisecond)
	collector.RequestStarted()
	collector.RequestFinished(200, 1500*time.Millisecond)
	collector.RequestStarted()
	collector.RequestFinished(404, 400*time.Millisecond)
	collector.recordEventLoopLag(25_126 * time.Microsecond)
	collector.recordEventLoopLag(5_124 * time.Microsecond)

	snapshot := collector.Snapshot()
	if _, err := time.Parse(time.RFC3339Nano, snapshot.CollectedSince); err != nil {
		t.Fatalf("collectedSince = %q: %v", snapshot.CollectedSince, err)
	}
	want := RequestSnapshot{
		Total: 4, Errors: 1, ErrorRate: 0.25, Slow: 1,
		AverageLatencyMS: 550, P95LatencyMS: 1500, MaximumLatencyMS: 1500, Sampled: 4,
	}
	if snapshot.Requests != want {
		t.Fatalf("requests = %+v, want %+v", snapshot.Requests, want)
	}
	if snapshot.EventLoop.LagMS != 5.12 || snapshot.EventLoop.MaximumLagMS != 25.13 {
		t.Fatalf("eventLoop = %+v", snapshot.EventLoop)
	}
	if snapshot.Memory.RSSBytes == 0 || snapshot.Memory.HeapUsedBytes == 0 || snapshot.Memory.HeapTotalBytes == 0 {
		t.Fatalf("memory = %+v", snapshot.Memory)
	}
}

func TestCollectorCapsSamplesAndCloseIsIdempotent(t *testing.T) {
	collector, err := New(Options{SampleLimit: 32, SampleInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 40 {
		collector.RequestStarted()
		collector.RequestFinished(200, time.Duration(index)*time.Millisecond)
	}
	if snapshot := collector.Snapshot(); snapshot.Requests.Sampled != 32 || snapshot.Requests.Total != 40 {
		t.Fatalf("snapshot = %+v", snapshot.Requests)
	}
	collector.Close()
	collector.Close()
}

func TestErrorRatePreservesLegacyUnroundedRatio(t *testing.T) {
	collector, err := New(Options{SampleLimit: 32, SampleInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	for _, status := range []int{200, 503, 200} {
		collector.RequestStarted()
		collector.RequestFinished(status, time.Millisecond)
	}
	if got, want := collector.Snapshot().Requests.ErrorRate, 1.0/3.0; got != want {
		t.Fatalf("errorRate = %.17g, want %.17g", got, want)
	}
}

func TestSchedulerLagUsesWakeObservationTime(t *testing.T) {
	expected := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	observed := expected.Add(37_456 * time.Microsecond)
	collector := &Collector{now: func() time.Time { return observed }}
	if got := collector.observeSchedulerWake(expected); !got.Equal(observed) {
		t.Fatalf("observed wake = %s, want %s", got, observed)
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if rounded(collector.eventLoopLagMS) != 37.46 || rounded(collector.maximumLoopLagMS) != 37.46 {
		t.Fatalf("event-loop lag = %f/%f", collector.eventLoopLagMS, collector.maximumLoopLagMS)
	}
}

func TestCollectorValidatesOptions(t *testing.T) {
	if _, err := New(Options{SampleLimit: 31}); err == nil {
		t.Fatal("sample limit below legacy minimum was accepted")
	}
	if _, err := New(Options{SampleLimit: 32, SampleInterval: -time.Second}); err == nil {
		t.Fatal("negative sample interval was accepted")
	}
}
