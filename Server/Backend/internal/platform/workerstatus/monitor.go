package workerstatus

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultFreshness = 45 * time.Second
	DefaultCacheTTL  = 500 * time.Millisecond
)

type Snapshot struct {
	Mode         string  `json:"mode"`
	State        string  `json:"state"`
	Responsive   bool    `json:"responsive"`
	Synchronized bool    `json:"synchronized"`
	Available    bool    `json:"available"`
	UpdatedAt    *string `json:"updatedAt"`
}

type Document struct {
	PID         int    `json:"pid,omitempty"`
	State       string `json:"state,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type Options struct {
	Path         string
	Freshness    time.Duration
	CacheTTL     time.Duration
	Now          func() time.Time
	ReadFile     func(string) ([]byte, error)
	ProcessAlive func(int) bool
}

type Monitor struct {
	path         string
	freshness    time.Duration
	cacheTTL     time.Duration
	now          func() time.Time
	readFile     func(string) ([]byte, error)
	processAlive func(int) bool

	mu      sync.Mutex
	cached  *cachedSnapshot
	pending *pendingSnapshot
}

type cachedSnapshot struct {
	fingerprint string
	expiresAt   time.Time
	value       Snapshot
}

type pendingSnapshot struct {
	fingerprint string
	done        chan struct{}
	value       Snapshot
}

func New(options Options) (*Monitor, error) {
	path := strings.TrimSpace(options.Path)
	if path == "" {
		return nil, errors.New("worker status path is required")
	}
	freshness := options.Freshness
	if freshness == 0 {
		freshness = DefaultFreshness
	}
	if freshness < time.Second {
		return nil, errors.New("worker status freshness must be at least one second")
	}
	cacheTTL := options.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = DefaultCacheTTL
	}
	if cacheTTL < 0 || cacheTTL > 5*time.Second {
		return nil, errors.New("worker status cache must be between zero and five seconds")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	readFile := options.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	processAlive := options.ProcessAlive
	if processAlive == nil {
		processAlive = processIsAlive
	}
	return &Monitor{
		path: path, freshness: freshness, cacheTTL: cacheTTL, now: now,
		readFile: readFile, processAlive: processAlive,
	}, nil
}

func (monitor *Monitor) Status(ctx context.Context, expectedFingerprint string) Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	now := monitor.now()
	monitor.mu.Lock()
	if cached := monitor.cached; cached != nil && cached.fingerprint == expectedFingerprint && now.Before(cached.expiresAt) {
		value := cached.value
		monitor.mu.Unlock()
		return value
	}
	if pending := monitor.pending; pending != nil && pending.fingerprint == expectedFingerprint {
		done := pending.done
		monitor.mu.Unlock()
		select {
		case <-done:
			return pending.value
		case <-ctx.Done():
			return Unavailable()
		}
	}
	pending := &pendingSnapshot{fingerprint: expectedFingerprint, done: make(chan struct{})}
	monitor.pending = pending
	monitor.mu.Unlock()

	value := monitor.read(expectedFingerprint)
	monitor.mu.Lock()
	pending.value = value
	monitor.cached = &cachedSnapshot{
		fingerprint: expectedFingerprint,
		expiresAt:   monitor.now().Add(monitor.cacheTTL),
		value:       value,
	}
	if monitor.pending == pending {
		monitor.pending = nil
	}
	close(pending.done)
	monitor.mu.Unlock()
	return value
}

func (monitor *Monitor) read(expectedFingerprint string) Snapshot {
	content, err := monitor.readFile(monitor.path)
	if err != nil {
		return Unavailable()
	}
	var document Document
	if err := json.Unmarshal(content, &document); err != nil {
		return Unavailable()
	}
	return Evaluate(document, expectedFingerprint, monitor.now(), monitor.freshness, monitor.processAlive)
}

func Evaluate(document Document, expectedFingerprint string, now time.Time, freshness time.Duration, alive func(int) bool) Snapshot {
	state := strings.TrimSpace(document.State)
	if state == "" {
		state = "UNKNOWN"
	}
	updated, err := time.Parse(time.RFC3339Nano, document.UpdatedAt)
	validTimestamp := err == nil
	responsive := false
	if validTimestamp && document.PID > 0 && state == "RUNNING" && alive != nil && alive(document.PID) {
		age := now.Sub(updated)
		responsive = age >= 0 && age <= freshness
	}
	synchronized := responsive && document.Fingerprint == expectedFingerprint
	var updatedAt *string
	if validTimestamp {
		value := updated.UTC().Format(time.RFC3339Nano)
		updatedAt = &value
	}
	return Snapshot{
		Mode: "external", State: state, Responsive: responsive,
		Synchronized: synchronized, Available: responsive && synchronized,
		UpdatedAt: updatedAt,
	}
}

func Unavailable() Snapshot {
	return Snapshot{
		Mode: "external", State: "UNAVAILABLE", Responsive: false,
		Synchronized: false, Available: false, UpdatedAt: nil,
	}
}
