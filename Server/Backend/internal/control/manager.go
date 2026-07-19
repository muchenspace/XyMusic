// Package control owns the process-lifetime control plane. It keeps the HTTP
// listener alive while application runtimes are built, replaced, or absent.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

const (
	RuntimePhaseSetupRequired = "SETUP_REQUIRED"
	RuntimePhaseStarting      = "STARTING"
	RuntimePhaseReady         = setup.RuntimePhaseReady
	RuntimePhaseFailed        = "FAILED"
	RuntimePhaseStopped       = "STOPPED"

	defaultDrainTimeout = 30 * time.Second
	defaultCloseTimeout = 30 * time.Second
	maximumErrorRunes   = 2_000
)

var (
	// ErrRuntimeUnavailable means that no validated runtime can currently serve
	// application traffic.
	ErrRuntimeUnavailable = errors.New("application runtime is unavailable")
	// ErrReadinessCheckRequired prevents adapters from accidentally declaring a
	// runtime ready without checking its real dependencies.
	ErrReadinessCheckRequired = errors.New("runtime readiness check is required")
)

// ManagedRuntime is a fully built candidate runtime. Ready must verify the
// dependencies needed to serve traffic; it is called both before activation
// and by the control-plane readiness endpoint.
type ManagedRuntime interface {
	HTTPHandler() http.Handler
	Ready(context.Context) error
	Activate(context.Context) error
	Deactivate(context.Context) error
	Close(context.Context) error
}

// RuntimeFactory builds an isolated candidate. A candidate is not visible to
// requests until Manager has validated and activated it.
type RuntimeFactory interface {
	Build(context.Context, config.Config) (ManagedRuntime, error)
}

// RuntimeFactoryFunc adapts a function to RuntimeFactory.
type RuntimeFactoryFunc func(context.Context, config.Config) (ManagedRuntime, error)

// Build implements RuntimeFactory.
func (factory RuntimeFactoryFunc) Build(ctx context.Context, candidate config.Config) (ManagedRuntime, error) {
	if factory == nil {
		return nil, errors.New("runtime factory function is nil")
	}
	return factory(ctx, candidate)
}

// RuntimeAdapter is a convenience adapter for application composition. A
// ReadyFunc is deliberately mandatory: a missing function returns
// ErrReadinessCheckRequired instead of producing a false-positive readiness
// result. Activation hooks may be nil for runtimes without separately managed
// background work.
type RuntimeAdapter struct {
	Handler        http.Handler
	ReadyFunc      func(context.Context) error
	ActivateFunc   func(context.Context) error
	DeactivateFunc func(context.Context) error
	CloseFunc      func(context.Context) error
}

func (runtime RuntimeAdapter) HTTPHandler() http.Handler { return runtime.Handler }

func (runtime RuntimeAdapter) Ready(ctx context.Context) error {
	if runtime.ReadyFunc == nil {
		return ErrReadinessCheckRequired
	}
	return runtime.ReadyFunc(ctx)
}

func (runtime RuntimeAdapter) Activate(ctx context.Context) error {
	if runtime.ActivateFunc == nil {
		return nil
	}
	return runtime.ActivateFunc(ctx)
}

func (runtime RuntimeAdapter) Deactivate(ctx context.Context) error {
	if runtime.DeactivateFunc == nil {
		return nil
	}
	return runtime.DeactivateFunc(ctx)
}

func (runtime RuntimeAdapter) Close(ctx context.Context) error {
	if runtime.CloseFunc == nil {
		return nil
	}
	return runtime.CloseFunc(ctx)
}

// ManagerOptions configures runtime lifecycle behavior.
type ManagerOptions struct {
	Source       string
	Factory      RuntimeFactory
	DrainTimeout time.Duration
	CloseTimeout time.Duration
	Now          func() time.Time
}

// Manager implements setup.RuntimeController and httpserver.ReadinessChecker.
// Lifecycle transitions are serialized while request acquisition remains
// concurrent and lock-free for the duration of downstream handling.
type Manager struct {
	factory      RuntimeFactory
	drainTimeout time.Duration
	closeTimeout time.Duration
	now          func() time.Time

	transition sync.Mutex
	mu         sync.Mutex
	active     *runtimeReference
	config     config.Config
	hasConfig  bool
	state      setup.RuntimeSnapshot

	retirements      map[*runtimeReference]*retirement
	retirementErrors []error
}

type runtimeReference struct {
	runtime ManagedRuntime
	handler http.Handler

	inFlight      int
	retiring      bool
	drained       chan struct{}
	drainedClosed bool
}

type retirement struct {
	done       chan struct{}
	deactivate bool
}

var (
	_ setup.RuntimeController     = (*Manager)(nil)
	_ httpserver.ReadinessChecker = (*Manager)(nil)
	_ http.Handler                = (*Manager)(nil)
	_ RuntimeFactory              = RuntimeFactoryFunc(nil)
	_ ManagedRuntime              = RuntimeAdapter{}
)

// NewManager constructs a manager that can start without a configured
// runtime. An empty source means first-run setup mode.
func NewManager(options ManagerOptions) (*Manager, error) {
	if options.Factory == nil {
		return nil, errors.New("runtime factory is required")
	}
	source := strings.TrimSpace(options.Source)
	if source == "" {
		source = setup.RuntimeSourceSetup
	}
	if err := validateSource(source); err != nil {
		return nil, err
	}
	if options.DrainTimeout < 0 {
		return nil, errors.New("runtime drain timeout cannot be negative")
	}
	if options.CloseTimeout < 0 {
		return nil, errors.New("runtime close timeout cannot be negative")
	}
	drainTimeout := options.DrainTimeout
	if drainTimeout == 0 {
		drainTimeout = defaultDrainTimeout
	}
	closeTimeout := options.CloseTimeout
	if closeTimeout == 0 {
		closeTimeout = defaultCloseTimeout
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	phase := RuntimePhaseStarting
	if source == setup.RuntimeSourceSetup {
		phase = RuntimePhaseSetupRequired
	}
	return &Manager{
		factory:      options.Factory,
		drainTimeout: drainTimeout,
		closeTimeout: closeTimeout,
		now:          now,
		state: setup.RuntimeSnapshot{
			Phase:  phase,
			Source: source,
		},
		retirements: make(map[*runtimeReference]*retirement),
	}, nil
}

// Status returns a detached lifecycle snapshot safe for concurrent callers.
func (manager *Manager) Status() setup.RuntimeSnapshot {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return cloneSnapshot(manager.state)
}

// ActiveConfig returns a detached copy of the active runtime configuration.
func (manager *Manager) ActiveConfig() (config.Config, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.hasConfig || manager.active == nil {
		return config.Config{}, false
	}
	return cloneConfig(manager.config), true
}

// Initialize builds, validates, activates, and atomically publishes a
// candidate runtime. If activation fails after the previous runtime was
// deactivated, Manager attempts to reactivate it before restoring READY.
func (manager *Manager) Initialize(ctx context.Context, raw config.Config, source string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.transition.Lock()
	defer manager.transition.Unlock()

	source = strings.TrimSpace(source)
	if source == "" {
		source = manager.Status().Source
	}
	if err := validateSource(source); err != nil {
		return err
	}

	manager.mu.Lock()
	previous := manager.active
	previousState := cloneSnapshot(manager.state)
	manager.state.Phase = RuntimePhaseStarting
	manager.state.Source = source
	manager.state.LastError = nil
	manager.mu.Unlock()

	candidateConfig := cloneConfig(raw)
	candidate, err := manager.factory.Build(ctx, cloneConfig(candidateConfig))
	if err != nil {
		return manager.rollbackInitialization(ctx, previous, previousState, nil, false, fmt.Errorf("build runtime candidate: %w", err))
	}
	if isNilInterface(candidate) {
		return manager.rollbackInitialization(ctx, previous, previousState, nil, false, errors.New("runtime factory returned a nil candidate"))
	}
	handler := candidate.HTTPHandler()
	if isNilInterface(handler) {
		return manager.rollbackInitialization(ctx, previous, previousState, candidate, false, errors.New("runtime candidate returned a nil HTTP handler"))
	}
	if err := candidate.Ready(ctx); err != nil {
		return manager.rollbackInitialization(ctx, previous, previousState, candidate, false, fmt.Errorf("validate runtime candidate readiness: %w", err))
	}

	previousDeactivationAttempted := false
	if previous != nil {
		previousDeactivationAttempted = true
		if err := previous.runtime.Deactivate(ctx); err != nil {
			return manager.rollbackInitialization(ctx, previous, previousState, candidate, previousDeactivationAttempted, fmt.Errorf("deactivate previous runtime: %w", err))
		}
	}
	if err := candidate.Activate(ctx); err != nil {
		return manager.rollbackInitialization(ctx, previous, previousState, candidate, previousDeactivationAttempted, fmt.Errorf("activate runtime candidate: %w", err))
	}

	startedAt := manager.now().UTC().Format(time.RFC3339Nano)
	next := &runtimeReference{
		runtime: candidate,
		handler: handler,
		drained: make(chan struct{}),
	}
	var retired *retirement
	manager.mu.Lock()
	manager.active = next
	manager.config = cloneConfig(candidateConfig)
	manager.hasConfig = true
	manager.state = setup.RuntimeSnapshot{
		Phase:      RuntimePhaseReady,
		Source:     source,
		Generation: previousState.Generation + 1,
		StartedAt:  &startedAt,
	}
	if previous != nil {
		retired = manager.startRetirementLocked(previous, false)
	}
	manager.mu.Unlock()
	manager.runRetirement(previous, retired)
	return nil
}

func (manager *Manager) rollbackInitialization(
	ctx context.Context,
	previous *runtimeReference,
	previousState setup.RuntimeSnapshot,
	candidate ManagedRuntime,
	previousDeactivationAttempted bool,
	primary error,
) error {
	var cleanupError error
	if candidate != nil {
		cleanupContext, cancel := manager.detachedLifecycleContext(ctx)
		cleanupError = candidate.Close(cleanupContext)
		cancel()
	}

	var rollbackError error
	if previous != nil && previousDeactivationAttempted {
		rollbackContext, cancel := manager.detachedLifecycleContext(ctx)
		rollbackError = previous.runtime.Activate(rollbackContext)
		cancel()
	}

	combined := primary
	if cleanupError != nil {
		combined = errors.Join(combined, fmt.Errorf("close failed runtime candidate: %w", cleanupError))
	}
	if rollbackError != nil {
		combined = errors.Join(combined, fmt.Errorf("reactivate previous runtime: %w", rollbackError))
	}
	message := safeError(combined)

	var retired *retirement
	manager.mu.Lock()
	previousRemainsUsable := previous != nil && manager.active == previous && rollbackError == nil
	if previousRemainsUsable {
		manager.state = cloneSnapshot(previousState)
		manager.state.Phase = RuntimePhaseReady
		manager.state.LastError = stringPointer(message)
	} else {
		if previous != nil && manager.active == previous {
			manager.active = nil
			manager.config = config.Config{}
			manager.hasConfig = false
			retired = manager.startRetirementLocked(previous, false)
		}
		manager.state = setup.RuntimeSnapshot{
			Phase:      RuntimePhaseFailed,
			Source:     manager.state.Source,
			Generation: previousState.Generation,
			LastError:  stringPointer(message),
		}
	}
	manager.mu.Unlock()
	manager.runRetirement(previous, retired)
	return combined
}

// Check performs a live readiness check against the active runtime. It never
// treats a lifecycle phase alone as proof that dependencies are healthy.
func (manager *Manager) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.mu.Lock()
	if manager.active == nil || manager.state.Phase != RuntimePhaseReady {
		phase := manager.state.Phase
		manager.mu.Unlock()
		return fmt.Errorf("%w: phase %s", ErrRuntimeUnavailable, phase)
	}
	reference := manager.active
	reference.inFlight++
	manager.mu.Unlock()
	defer manager.release(reference)
	if err := reference.runtime.Ready(ctx); err != nil {
		return fmt.Errorf("runtime dependency readiness failed: %w", err)
	}
	return nil
}

// ServeHTTP forwards to one stable runtime generation. The reference remains
// held until the downstream handler has completely returned.
func (manager *Manager) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	reference := manager.acquire()
	if reference == nil {
		manager.writeUnavailable(writer, request)
		return
	}
	defer manager.release(reference)
	reference.handler.ServeHTTP(writer, request)
}

// Close stops accepting new runtime requests, drains all active and retired
// generations, and closes their resources. The manager may be initialized
// again after Close, which is required by setup compensation and retry flows.
func (manager *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.transition.Lock()
	defer manager.transition.Unlock()

	manager.mu.Lock()
	active := manager.active
	manager.active = nil
	manager.config = config.Config{}
	manager.hasConfig = false
	manager.state.Phase = RuntimePhaseStopped
	manager.state.StartedAt = nil
	var retired *retirement
	if active != nil {
		retired = manager.startRetirementLocked(active, true)
	}
	manager.mu.Unlock()
	manager.runRetirement(active, retired)

	return manager.waitForRetirements(ctx)
}

func (manager *Manager) acquire() *runtimeReference {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	reference := manager.active
	if reference == nil || reference.retiring {
		return nil
	}
	reference.inFlight++
	return reference
}

func (manager *Manager) release(reference *runtimeReference) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if reference.inFlight > 0 {
		reference.inFlight--
	}
	if reference.retiring && reference.inFlight == 0 {
		closeDrained(reference)
	}
}

func (manager *Manager) startRetirementLocked(reference *runtimeReference, deactivate bool) *retirement {
	if reference == nil {
		return nil
	}
	if existing := manager.retirements[reference]; existing != nil {
		return nil
	}
	reference.retiring = true
	if reference.inFlight == 0 {
		closeDrained(reference)
	}
	retired := &retirement{done: make(chan struct{}), deactivate: deactivate}
	manager.retirements[reference] = retired
	return retired
}

func (manager *Manager) runRetirement(reference *runtimeReference, retired *retirement) {
	if reference == nil || retired == nil {
		return
	}
	go func() {
		var failures []error
		if err := manager.waitForDrain(reference); err != nil {
			failures = append(failures, err)
		}
		if retired.deactivate {
			lifecycleContext, cancel := context.WithTimeout(context.Background(), manager.closeTimeout)
			if err := reference.runtime.Deactivate(lifecycleContext); err != nil {
				failures = append(failures, fmt.Errorf("deactivate retiring runtime: %w", err))
			}
			cancel()
		}
		closeContext, cancel := context.WithTimeout(context.Background(), manager.closeTimeout)
		if err := reference.runtime.Close(closeContext); err != nil {
			failures = append(failures, fmt.Errorf("close retiring runtime: %w", err))
		}
		cancel()

		manager.mu.Lock()
		if failure := errors.Join(failures...); failure != nil {
			manager.retirementErrors = append(manager.retirementErrors, failure)
		}
		delete(manager.retirements, reference)
		close(retired.done)
		manager.mu.Unlock()
	}()
}

func (manager *Manager) waitForDrain(reference *runtimeReference) error {
	timer := time.NewTimer(manager.drainTimeout)
	defer timer.Stop()
	select {
	case <-reference.drained:
		return nil
	case <-timer.C:
		return fmt.Errorf("runtime request drain exceeded %s", manager.drainTimeout)
	}
}

func (manager *Manager) waitForRetirements(ctx context.Context) error {
	for {
		manager.mu.Lock()
		if len(manager.retirements) == 0 {
			failures := append([]error(nil), manager.retirementErrors...)
			manager.retirementErrors = nil
			manager.mu.Unlock()
			return errors.Join(failures...)
		}
		var done <-chan struct{}
		for _, retired := range manager.retirements {
			done = retired.done
			break
		}
		manager.mu.Unlock()

		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("wait for runtime retirement: %w", ctx.Err())
		}
	}
}

func (manager *Manager) detachedLifecycleContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), manager.closeTimeout)
}

func (manager *Manager) writeUnavailable(writer http.ResponseWriter, request *http.Request) {
	status := manager.Status()
	detail := "当前运行时暂时不可用，请稍后重试"
	if status.Phase == RuntimePhaseSetupRequired {
		detail = "首次配置尚未完成，请先完成系统配置"
	}
	traceID := ""
	instance := ""
	method := ""
	if request != nil {
		traceID = httpserver.TraceIDFromContext(request.Context())
		method = request.Method
		if request.URL != nil {
			instance = request.URL.Path
		}
	}
	problem := httpserver.ProblemFromError(apperror.DependencyUnavailable(detail), traceID, instance)
	payload, _ := json.Marshal(problem)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", httpserver.ProblemMediaType)
	writer.Header().Set("Retry-After", "5")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if traceID != "" {
		writer.Header().Set(httpserver.TraceIDHeader, traceID)
	}
	writer.WriteHeader(problem.Status)
	if method != http.MethodHead {
		_, _ = writer.Write(payload)
	}
}

func validateSource(source string) error {
	if source != setup.RuntimeSourceSetup && source != setup.RuntimeSourceManaged {
		return fmt.Errorf("unsupported runtime configuration source %q", source)
	}
	return nil
}

func closeDrained(reference *runtimeReference) {
	if reference.drainedClosed {
		return
	}
	reference.drainedClosed = true
	close(reference.drained)
}

func cloneSnapshot(snapshot setup.RuntimeSnapshot) setup.RuntimeSnapshot {
	result := snapshot
	if snapshot.StartedAt != nil {
		result.StartedAt = stringPointer(*snapshot.StartedAt)
	}
	if snapshot.LastError != nil {
		result.LastError = stringPointer(*snapshot.LastError)
	}
	return result
}

func cloneConfig(input config.Config) config.Config {
	result := input
	result.HTTP.TrustedProxyAddresses = append([]string(nil), input.HTTP.TrustedProxyAddresses...)
	result.LocalLibrary.IncludePatterns = append([]string(nil), input.LocalLibrary.IncludePatterns...)
	result.LocalLibrary.ExcludePatterns = append([]string(nil), input.LocalLibrary.ExcludePatterns...)
	if input.LocalLibrary.ScanIntervalMinutes != nil {
		value := *input.LocalLibrary.ScanIntervalMinutes
		result.LocalLibrary.ScanIntervalMinutes = &value
	}
	return result
}

func safeError(err error) string {
	message := strings.TrimSpace(err.Error())
	runes := []rune(message)
	if len(runes) > maximumErrorRunes {
		message = string(runes[:maximumErrorRunes])
	}
	return message
}

func stringPointer(value string) *string { return &value }

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
