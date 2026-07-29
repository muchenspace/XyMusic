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

type PipelineSnapshot struct {
	Total            uint64  `json:"total"`
	Errors           uint64  `json:"errors"`
	ErrorRate        float64 `json:"errorRate"`
	AverageLatencyMS float64 `json:"averageLatencyMs"`
	MaximumLatencyMS float64 `json:"maximumLatencyMs"`
	LastLatencyMS    float64 `json:"lastLatencyMs"`
}

type CacheSnapshot struct {
	Hits    uint64  `json:"hits"`
	Misses  uint64  `json:"misses"`
	HitRate float64 `json:"hitRate"`
}

type PlatformSnapshot struct {
	Requests  uint64  `json:"requests"`
	Errors    uint64  `json:"errors"`
	ErrorRate float64 `json:"errorRate"`
}

type Snapshot struct {
	CollectedSince string                      `json:"collectedSince"`
	Requests       RequestSnapshot             `json:"requests"`
	EventLoop      EventLoopSnapshot           `json:"eventLoop"`
	Memory         MemorySnapshot              `json:"memory"`
	Pipelines      map[string]PipelineSnapshot `json:"pipelines,omitempty"`
	Caches         map[string]CacheSnapshot    `json:"caches,omitempty"`
	Platforms      map[string]PlatformSnapshot `json:"platforms,omitempty"`
}

type pipelineMetric struct {
	total, errors              uint64
	durationTotalMS, maximumMS float64
	lastMS                     float64
}

type cacheMetric struct{ hits, misses uint64 }

type platformMetric struct{ requests, errors uint64 }

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
	pipelines          map[string]*pipelineMetric
	caches             map[string]*cacheMetric
	platforms          map[string]*platformMetric

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

func (collector *Collector) ObservePipeline(stage string, duration time.Duration, failed bool) {
	if collector == nil || stage == "" {
		return
	}
	durationMS := max(0, float64(duration)/float64(time.Millisecond))
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if collector.pipelines == nil {
		collector.pipelines = make(map[string]*pipelineMetric)
	}
	metric := collector.pipelines[stage]
	if metric == nil {
		metric = &pipelineMetric{}
		collector.pipelines[stage] = metric
	}
	metric.total++
	if failed {
		metric.errors++
	}
	metric.durationTotalMS += durationMS
	metric.maximumMS = max(metric.maximumMS, durationMS)
	metric.lastMS = durationMS
}

func (collector *Collector) ObserveCache(name string, hit bool) {
	if collector == nil || name == "" {
		return
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if collector.caches == nil {
		collector.caches = make(map[string]*cacheMetric)
	}
	metric := collector.caches[name]
	if metric == nil {
		metric = &cacheMetric{}
		collector.caches[name] = metric
	}
	if hit {
		metric.hits++
	} else {
		metric.misses++
	}
}

func (collector *Collector) ObservePlatform(source string, failed bool) {
	if collector == nil || source == "" {
		return
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if collector.platforms == nil {
		collector.platforms = make(map[string]*platformMetric)
	}
	metric := collector.platforms[source]
	if metric == nil {
		metric = &platformMetric{}
		collector.platforms[source] = metric
	}
	metric.requests++
	if failed {
		metric.errors++
	}
}

func (collector *Collector) Snapshot() Snapshot {
	if collector == nil {
		return Snapshot{}
	}
	collector.mu.Lock()
	durations := append([]float64(nil), collector.durations[:collector.durationCount]...)
	var pipelines map[string]PipelineSnapshot
	if len(collector.pipelines) > 0 {
		pipelines = make(map[string]PipelineSnapshot, len(collector.pipelines))
		for name, metric := range collector.pipelines {
			pipelines[name] = PipelineSnapshot{
				Total: metric.total, Errors: metric.errors,
				ErrorRate:        ratio(float64(metric.errors), float64(metric.total)),
				AverageLatencyMS: rounded(ratio(metric.durationTotalMS, float64(metric.total))),
				MaximumLatencyMS: rounded(metric.maximumMS), LastLatencyMS: rounded(metric.lastMS),
			}
		}
	}
	var caches map[string]CacheSnapshot
	if len(collector.caches) > 0 {
		caches = make(map[string]CacheSnapshot, len(collector.caches))
		for name, metric := range collector.caches {
			total := metric.hits + metric.misses
			caches[name] = CacheSnapshot{Hits: metric.hits, Misses: metric.misses,
				HitRate: ratio(float64(metric.hits), float64(total))}
		}
	}
	var platforms map[string]PlatformSnapshot
	if len(collector.platforms) > 0 {
		platforms = make(map[string]PlatformSnapshot, len(collector.platforms))
		for name, metric := range collector.platforms {
			platforms[name] = PlatformSnapshot{Requests: metric.requests, Errors: metric.errors,
				ErrorRate: ratio(float64(metric.errors), float64(metric.requests))}
		}
	}
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
		Pipelines: pipelines, Caches: caches, Platforms: platforms,
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
