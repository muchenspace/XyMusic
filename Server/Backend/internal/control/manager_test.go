package control

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/setup"
)

func TestManagerRequestCanInitiateRuntimeReplacementWithoutDeadlock(t *testing.T) {
	secondConfig := runtimeConfig(2)
	var manager *Manager
	first := newFakeRuntime("first")
	second := newFakeRuntime("second")
	first.handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := manager.Initialize(context.Background(), secondConfig, setup.RuntimeSourceManaged); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = writer.Write([]byte("switched"))
	})
	created := map[int]*fakeRuntime{1: first, 2: second}
	var err error
	manager, err = NewManager(ManagerOptions{
		Source: setup.RuntimeSourceManaged,
		Factory: RuntimeFactoryFunc(func(_ context.Context, candidate config.Config) (ManagedRuntime, error) {
			return created[candidate.HTTP.Port], nil
		}),
		DrainTimeout: time.Second,
		CloseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		manager.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings", nil))
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request deadlocked while replacing its own runtime")
	}
	if response.Code != http.StatusOK || response.Body.String() != "switched" {
		t.Fatalf("unexpected response: %d %q", response.Code, response.Body.String())
	}
	if snapshot := manager.Status(); snapshot.Phase != RuntimePhaseReady || snapshot.Generation != 2 {
		t.Fatalf("unexpected status after replacement: %#v", snapshot)
	}
	eventually(t, time.Second, func() bool { return first.closeCalls.Load() == 1 })
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.closeCalls.Load() != 1 {
		t.Fatalf("second runtime close calls = %d", second.closeCalls.Load())
	}
}

func TestManagerDrainsRetiredRuntimeBeforeClosingIt(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	first := newFakeRuntime("first")
	first.handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		_, _ = writer.Write([]byte("old response"))
	})
	second := newFakeRuntime("second")
	created := map[int]*fakeRuntime{1: first, 2: second}
	manager := mustManager(t, RuntimeFactoryFunc(func(_ context.Context, candidate config.Config) (ManagedRuntime, error) {
		return created[candidate.HTTP.Port], nil
	}))
	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}

	requestFinished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		manager.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil))
		requestFinished <- response
	}()
	<-requestStarted
	if err := manager.Initialize(context.Background(), runtimeConfig(2), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.closed:
		t.Fatal("retired runtime closed before its request drained")
	case <-time.After(30 * time.Millisecond):
	}

	close(releaseRequest)
	response := <-requestFinished
	if response.Body.String() != "old response" {
		t.Fatalf("unexpected old response: %q", response.Body.String())
	}
	select {
	case <-first.closed:
	case <-time.After(time.Second):
		t.Fatal("retired runtime did not close after request drain")
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerRollsBackFailedCandidateAndPreservesGeneration(t *testing.T) {
	first := newFakeRuntime("first")
	second := newFakeRuntime("second")
	second.activateFunc = func(context.Context) error { return errors.New("candidate activation failed") }
	created := map[int]*fakeRuntime{1: first, 2: second}
	manager := mustManager(t, RuntimeFactoryFunc(func(_ context.Context, candidate config.Config) (ManagedRuntime, error) {
		return created[candidate.HTTP.Port], nil
	}))
	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	startedAt := manager.Status().StartedAt

	err := manager.Initialize(context.Background(), runtimeConfig(2), setup.RuntimeSourceManaged)
	if err == nil || !strings.Contains(err.Error(), "candidate activation failed") {
		t.Fatalf("unexpected initialize error: %v", err)
	}
	snapshot := manager.Status()
	if snapshot.Phase != RuntimePhaseReady || snapshot.Source != setup.RuntimeSourceManaged || snapshot.Generation != 1 {
		t.Fatalf("previous runtime was not restored: %#v", snapshot)
	}
	if snapshot.StartedAt == nil || startedAt == nil || *snapshot.StartedAt != *startedAt {
		t.Fatalf("rollback changed the previous start time: before=%v after=%v", startedAt, snapshot.StartedAt)
	}
	if snapshot.LastError == nil || !strings.Contains(*snapshot.LastError, "candidate activation failed") {
		t.Fatalf("rollback failure was not recorded safely: %#v", snapshot)
	}
	if first.deactivateCalls.Load() != 1 || first.activateCalls.Load() != 2 {
		t.Fatalf("previous lifecycle calls activate=%d deactivate=%d", first.activateCalls.Load(), first.deactivateCalls.Load())
	}
	if second.closeCalls.Load() != 1 {
		t.Fatalf("failed candidate close calls = %d", second.closeCalls.Load())
	}
	response := httptest.NewRecorder()
	manager.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if response.Code != http.StatusOK || response.Body.String() != "first" {
		t.Fatalf("request was not routed to restored runtime: %d %q", response.Code, response.Body.String())
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerRejectsUnreadyCandidateBeforeDeactivatingPreviousRuntime(t *testing.T) {
	first := newFakeRuntime("first")
	second := newFakeRuntime("second")
	second.readyFunc = func(context.Context) error { return errors.New("storage probe failed") }
	created := map[int]*fakeRuntime{1: first, 2: second}
	manager := mustManager(t, RuntimeFactoryFunc(func(_ context.Context, candidate config.Config) (ManagedRuntime, error) {
		return created[candidate.HTTP.Port], nil
	}))
	firstConfig := runtimeConfig(1)
	firstConfig.HTTP.TrustedProxyAddresses = []string{"192.0.2.10"}
	if err := manager.Initialize(context.Background(), firstConfig, setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	firstConfig.HTTP.TrustedProxyAddresses[0] = "192.0.2.11"

	err := manager.Initialize(context.Background(), runtimeConfig(2), setup.RuntimeSourceManaged)
	if err == nil || !strings.Contains(err.Error(), "storage probe failed") {
		t.Fatalf("unexpected readiness failure: %v", err)
	}
	if first.deactivateCalls.Load() != 0 || first.activateCalls.Load() != 1 {
		t.Fatalf("unready candidate disturbed previous runtime: activate=%d deactivate=%d", first.activateCalls.Load(), first.deactivateCalls.Load())
	}
	if second.activateCalls.Load() != 0 || second.closeCalls.Load() != 1 {
		t.Fatalf("unready candidate lifecycle activate=%d close=%d", second.activateCalls.Load(), second.closeCalls.Load())
	}
	activeConfig, active := manager.ActiveConfig()
	if !active || len(activeConfig.HTTP.TrustedProxyAddresses) != 1 || activeConfig.HTTP.TrustedProxyAddresses[0] != "192.0.2.10" {
		t.Fatalf("active configuration was not isolated from caller mutation: %#v", activeConfig)
	}
	activeConfig.HTTP.TrustedProxyAddresses[0] = "192.0.2.12"
	again, _ := manager.ActiveConfig()
	if again.HTTP.TrustedProxyAddresses[0] != "192.0.2.10" {
		t.Fatal("ActiveConfig exposed manager-owned slices")
	}
	if snapshot := manager.Status(); snapshot.Phase != RuntimePhaseReady || snapshot.Generation != 1 || snapshot.Source != setup.RuntimeSourceManaged {
		t.Fatalf("unexpected rollback snapshot: %#v", snapshot)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerStopsRoutingWhenRollbackActivationFails(t *testing.T) {
	first := newFakeRuntime("first")
	first.activateFunc = func(context.Context) error {
		if first.activateCalls.Load() > 1 {
			return errors.New("rollback activation failed")
		}
		return nil
	}
	second := newFakeRuntime("second")
	second.activateFunc = func(context.Context) error { return errors.New("candidate activation failed") }
	created := map[int]*fakeRuntime{1: first, 2: second}
	manager := mustManager(t, RuntimeFactoryFunc(func(_ context.Context, candidate config.Config) (ManagedRuntime, error) {
		return created[candidate.HTTP.Port], nil
	}))
	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(context.Background(), runtimeConfig(2), setup.RuntimeSourceManaged); err == nil {
		t.Fatal("expected candidate activation failure")
	}

	snapshot := manager.Status()
	if snapshot.Phase != RuntimePhaseFailed || snapshot.Generation != 1 || snapshot.LastError == nil || !strings.Contains(*snapshot.LastError, "rollback activation failed") {
		t.Fatalf("unexpected failed rollback status: %#v", snapshot)
	}
	if _, active := manager.ActiveConfig(); active {
		t.Fatal("failed previous runtime remained active")
	}
	response := httptest.NewRecorder()
	manager.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after rollback failure returned %d", response.Code)
	}
	eventually(t, time.Second, func() bool { return first.closeCalls.Load() == 1 })
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerConcurrentTrafficAndRepeatedAtomicSwitches(t *testing.T) {
	var runtimeID atomic.Int64
	var closedWhileHandling atomic.Bool
	factory := RuntimeFactoryFunc(func(_ context.Context, _ config.Config) (ManagedRuntime, error) {
		id := runtimeID.Add(1)
		runtime := newFakeRuntime(strconv.FormatInt(id, 10))
		runtime.handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			if runtime.closedFlag.Load() {
				closedWhileHandling.Store(true)
			}
			time.Sleep(100 * time.Microsecond)
			if runtime.closedFlag.Load() {
				closedWhileHandling.Store(true)
			}
			_, _ = fmt.Fprint(writer, runtime.name)
		})
		return runtime, nil
	})
	manager := mustManager(t, factory)
	if err := manager.Initialize(context.Background(), runtimeConfig(1), setup.RuntimeSourceManaged); err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 100 {
				response := httptest.NewRecorder()
				manager.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
				if response.Code != http.StatusOK || response.Body.Len() == 0 {
					t.Errorf("concurrent request returned %d %q", response.Code, response.Body.String())
					return
				}
			}
		}()
	}
	close(start)
	for generation := 2; generation <= 25; generation++ {
		if err := manager.Initialize(context.Background(), runtimeConfig(generation), setup.RuntimeSourceManaged); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	if snapshot := manager.Status(); snapshot.Generation != 25 || snapshot.Phase != RuntimePhaseReady {
		t.Fatalf("unexpected final status: %#v", snapshot)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closedWhileHandling.Load() {
		t.Fatal("a runtime was closed while one of its handlers was still executing")
	}
}

func TestRuntimeAdapterRequiresRealReadinessCheck(t *testing.T) {
	runtime := RuntimeAdapter{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	if !errors.Is(runtime.Ready(context.Background()), ErrReadinessCheckRequired) {
		t.Fatal("adapter without ReadyFunc reported ready")
	}
}

type fakeRuntime struct {
	name           string
	handler        http.Handler
	readyFunc      func(context.Context) error
	activateFunc   func(context.Context) error
	deactivateFunc func(context.Context) error
	closeFunc      func(context.Context) error

	readyCalls      atomic.Int64
	activateCalls   atomic.Int64
	deactivateCalls atomic.Int64
	closeCalls      atomic.Int64
	closedFlag      atomic.Bool
	closed          chan struct{}
	closeOnce       sync.Once
}

func newFakeRuntime(name string) *fakeRuntime {
	runtime := &fakeRuntime{name: name, closed: make(chan struct{})}
	runtime.handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(name))
	})
	return runtime
}

func (runtime *fakeRuntime) HTTPHandler() http.Handler { return runtime.handler }

func (runtime *fakeRuntime) Ready(ctx context.Context) error {
	runtime.readyCalls.Add(1)
	if runtime.readyFunc != nil {
		return runtime.readyFunc(ctx)
	}
	return nil
}

func (runtime *fakeRuntime) Activate(ctx context.Context) error {
	runtime.activateCalls.Add(1)
	if runtime.activateFunc != nil {
		return runtime.activateFunc(ctx)
	}
	return nil
}

func (runtime *fakeRuntime) Deactivate(ctx context.Context) error {
	runtime.deactivateCalls.Add(1)
	if runtime.deactivateFunc != nil {
		return runtime.deactivateFunc(ctx)
	}
	return nil
}

func (runtime *fakeRuntime) Close(ctx context.Context) error {
	runtime.closeCalls.Add(1)
	runtime.closedFlag.Store(true)
	runtime.closeOnce.Do(func() { close(runtime.closed) })
	if runtime.closeFunc != nil {
		return runtime.closeFunc(ctx)
	}
	return nil
}

func mustManager(t *testing.T, factory RuntimeFactory) *Manager {
	t.Helper()
	manager, err := NewManager(ManagerOptions{
		Source:       setup.RuntimeSourceSetup,
		Factory:      factory,
		DrainTimeout: time.Second,
		CloseTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func runtimeConfig(port int) config.Config {
	return config.Config{HTTP: config.HTTP{Port: port}}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not satisfied before timeout")
		}
		time.Sleep(time.Millisecond)
	}
}
