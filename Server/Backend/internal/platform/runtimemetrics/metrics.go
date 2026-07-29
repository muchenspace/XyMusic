package runtimemetrics

import (
	"errors"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

const (
	defaultSampleLimit    = 2_048
	defaultSampleInterval = time.Second
	minimumSampleLimit    = 32
	maximumSampleLimit    = 100_000
)

type Options struct {
	SampleLimit    int
	SampleInterval time.Duration
}

type RequestSnapshot struct {
	Total            uint64  `json:"total"`
	InFlight         int64   `json:"inFlight"`
	Errors           uint64  `json:"errors"`
	ErrorRate        float64 `json:"errorRate"`
	Slow             uint64  `json:"slow"`
	AverageLatencyMS float64 `json:"averageLatencyMs"`
	P95LatencyMS     float64 `json:"p95LatencyMs"`
	MaximumLatencyMS float64 `json:"maximumLatencyMs"`
	Sampled          int     `json:"sampled"`
}

type EventLoopSnapshot struct {
	LagMS        float64 `json:"lagMs"`
	MaximumLagMS float64 `json:"maximumLagMs"`
}

type MemorySnapshot struct {
	RSSBytes       uint64 `json:"rssBytes"`
	HeapUsedBytes  uint64 `json:"heapUsedBytes"`
	HeapTotalBytes uint64 `json:"heapTotalBytes"`
	ExternalBytes  uint64 `json:"externalBytes"`
}

type Snapshot struct {
	CollectedSince string            `json:"collectedSince"`
	Requests       RequestSnapshot   `json:"requests"`
	EventLoop      EventLoopSnapshot `json:"eventLoop"`
	Memory         MemorySnapshot    `json:"memory"`
}

type Collector struct {
	mu sync.Mutex

	now                func() time.Time
	startedAt          time.Time
	durations          []float64
	durationWriteIndex int
	durationCount      int
	requestCount       uint64
	errorCount         uint64
	slowRequestCount   uint64
	inFlightRequests   int64
	durationTotalMS    float64
	maximumDurationMS  float64
	eventLoopLagMS     float64
	maximumLoopLagMS   float64

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func New(options Options) (*Collector, error) {
	if options.SampleLimit == 0 {
		options.SampleLimit = defaultSampleLimit
	}
	if options.SampleLimit < minimumSampleLimit || options.SampleLimit > maximumSampleLimit {
		return nil, errors.New("runtime metric sample limit must be from 32 to 100000")
	}
	if options.SampleInterval == 0 {
		options.SampleInterval = defaultSampleInterval
	}
	if options.SampleInterval <= 0 {
		return nil, errors.New("runtime metric sample interval must be positive")
	}
	now := time.Now
	collector := &Collector{
		now:       now,
		startedAt: now().UTC(),
		durations: make([]float64, options.SampleLimit),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go collector.sampleSchedulerDelay(options.SampleInterval)
	return collector, nil
}

func (collector *Collector) RequestStarted() {
	if collector == nil {
		return
	}
	collector.mu.Lock()
	collector.inFlightRequests++
	collector.mu.Unlock()
}

func (collector *Collector) RequestFinished(status int, duration time.Duration) {
	if collector == nil {
		return
	}
	durationMS := float64(duration) / float64(time.Millisecond)
	if durationMS < 0 || math.IsNaN(durationMS) || math.IsInf(durationMS, 0) {
		durationMS = 0
	}
	collector.mu.Lock()
	if collector.inFlightRequests > 0 {
		collector.inFlightRequests--
	}
	collector.requestCount++
	collector.durationTotalMS += durationMS
	collector.maximumDurationMS = max(collector.maximumDurationMS, durationMS)
	if status >= 500 {
		collector.errorCount++
	}
	if durationMS >= 1_000 {
		collector.slowRequestCount++
	}
	collector.durations[collector.durationWriteIndex] = durationMS
	collector.durationWriteIndex = (collector.durationWriteIndex + 1) % len(collector.durations)
	collector.durationCount = min(len(collector.durations), collector.durationCount+1)
	collector.mu.Unlock()
}

func (collector *Collector) Snapshot() Snapshot {
	if collector == nil {
		return Snapshot{}
	}
	collector.mu.Lock()
	durations := append([]float64(nil), collector.durations[:collector.durationCount]...)
	requests := RequestSnapshot{
		Total: collector.requestCount, InFlight: collector.inFlightRequests,
		Errors: collector.errorCount, Slow: collector.slowRequestCount,
		ErrorRate:        ratio(float64(collector.errorCount), float64(collector.requestCount)),
		AverageLatencyMS: ratio(collector.durationTotalMS, float64(collector.requestCount)),
		MaximumLatencyMS: collector.maximumDurationMS, Sampled: collector.durationCount,
	}
	eventLoop := EventLoopSnapshot{
		LagMS: collector.eventLoopLagMS, MaximumLagMS: collector.maximumLoopLagMS,
	}
	startedAt := collector.startedAt
	collector.mu.Unlock()

	sort.Float64s(durations)
	requests.P95LatencyMS = percentile(durations, 0.95)
	requests.AverageLatencyMS = rounded(requests.AverageLatencyMS)
	requests.P95LatencyMS = rounded(requests.P95LatencyMS)
	requests.MaximumLatencyMS = rounded(requests.MaximumLatencyMS)
	eventLoop.LagMS = rounded(eventLoop.LagMS)
	eventLoop.MaximumLagMS = rounded(eventLoop.MaximumLagMS)

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	external := uint64(0)
	if memory.Sys > memory.HeapSys {
		external = memory.Sys - memory.HeapSys
	}
	return Snapshot{
		CollectedSince: startedAt.Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z"),
		Requests:       requests,
		EventLoop:      eventLoop,
		Memory: MemorySnapshot{
			RSSBytes: memory.Sys, HeapUsedBytes: memory.HeapAlloc,
			HeapTotalBytes: memory.HeapSys, ExternalBytes: external,
		},
	}
}

func (collector *Collector) Close() {
	if collector == nil {
		return
	}
	collector.closeOnce.Do(func() { close(collector.stop) })
	<-collector.done
}

func (collector *Collector) sampleSchedulerDelay(interval time.Duration) {
	defer close(collector.done)
	expected := collector.now().Add(interval)
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-collector.stop:
			return
		case <-timer.C:
			observed := collector.observeSchedulerWake(expected)
			expected = observed.Add(interval)
			timer.Reset(interval)
		}
	}
}

func (collector *Collector) observeSchedulerWake(expected time.Time) time.Time {
	observed := collector.now()
	collector.recordEventLoopLag(observed.Sub(expected))
	return observed
}

func (collector *Collector) recordEventLoopLag(delay time.Duration) {
	delayMS := float64(delay) / float64(time.Millisecond)
	if delayMS < 0 {
		delayMS = 0
	}
	collector.mu.Lock()
	collector.eventLoopLagMS = delayMS
	collector.maximumLoopLagMS = max(collector.maximumLoopLagMS, delayMS)
	collector.mu.Unlock()
}

func percentile(sorted []float64, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := min(len(sorted)-1, int(math.Ceil(float64(len(sorted))*fraction))-1)
	return sorted[index]
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func rounded(value float64) float64 {
	return math.Round(value*100) / 100
}
