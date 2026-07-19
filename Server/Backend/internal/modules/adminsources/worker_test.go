package adminsources

import (
	"context"
	"errors"
	"testing"
	"time"

	"xymusic/server/internal/config"
)

func TestWorkerInitializesDefaultAndStartupSources(t *testing.T) {
	directory := t.TempDir()
	store := &workerStoreStub{startupIDs: []string{testRootID}}
	worker, err := NewWorker(WorkerOptions{
		Store: store, Scanner: scannerFunc(func(context.Context, ScanInput) (ScanResult, error) { return ScanResult{}, nil }),
		RootDirectory: directory,
		DefaultRoot: config.LocalLibrary{
			Name: "Local", Directory: directory, Mode: string(RootModeReadOnly), Enabled: true,
			SyncOnStartup: true, IncludePatterns: []string{}, ExcludePatterns: []string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.initializeCalls != 1 || store.defaultMutation.Path != directory || store.defaultMutation.Status != RootStatusUnknown {
		t.Fatalf("initialization=%d mutation=%+v", store.initializeCalls, store.defaultMutation)
	}
	if len(store.enqueued) != 1 || store.enqueued[0].RootID != testRootID || !store.enqueued[0].Deduplicate {
		t.Fatalf("enqueued=%+v", store.enqueued)
	}
}

func TestWorkerExecutesClaimWithProgressAndFencing(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	attemptID := "00000000-0000-4000-8000-000000000003"
	store := &workerStoreStub{claim: &ClaimedScan{
		Run:  ScanRun{ID: testRunID, RootID: testRootID, RootVersion: 4, Status: ScanStatusRunning, AttemptID: &attemptID},
		Root: Root{ID: testRootID, Path: t.TempDir(), Version: 4, Enabled: true},
	}}
	scanner := scannerFunc(func(ctx context.Context, input ScanInput) (ScanResult, error) {
		if input.ScanRunID != testRunID {
			return ScanResult{}, errors.New("scan run id was not forwarded")
		}
		cancelled, err := input.IsCancelled(ctx)
		if err != nil || cancelled {
			return ScanResult{}, errors.New("unexpected cancellation")
		}
		if err := input.OnProgress(ctx, ScanProgress{DiscoveredFiles: 3, ProcessedFiles: 2, FailedFiles: 1}); err != nil {
			return ScanResult{}, err
		}
		return ScanResult{DiscoveredFiles: 3, ProcessedFiles: 3, FailedFiles: 1, ArchivedFiles: 2}, nil
	})
	worker, err := NewWorker(WorkerOptions{
		Store: store, Scanner: scanner, RootDirectory: t.TempDir(),
		WorkerID: "worker-test", Lease: time.Second, Heartbeat: 100 * time.Millisecond,
		ProgressWrite: 0, Now: func() time.Time { return now },
		DefaultRoot: config.LocalLibrary{Directory: store.claim.Root.Path, Mode: string(RootModeReadOnly), IncludePatterns: []string{}, ExcludePatterns: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunNextScan(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.scheduleCalls != 1 || store.progress.ProcessedFiles != 2 || store.completed.ProcessedFiles != 3 {
		t.Fatalf("schedule=%d progress=%+v completed=%+v", store.scheduleCalls, store.progress, store.completed)
	}
	if store.finalStatus != "" {
		t.Fatalf("unexpected final status=%s", store.finalStatus)
	}
}

func TestWorkerFinalizesRequestedCancellation(t *testing.T) {
	attemptID := "00000000-0000-4000-8000-000000000003"
	store := &workerStoreStub{cancelled: true, claim: &ClaimedScan{
		Run:  ScanRun{ID: testRunID, RootID: testRootID, RootVersion: 1, Status: ScanStatusRunning, AttemptID: &attemptID},
		Root: Root{ID: testRootID, Path: t.TempDir(), Version: 1, Enabled: true},
	}}
	scanner := scannerFunc(func(ctx context.Context, input ScanInput) (ScanResult, error) {
		cancelled, err := input.IsCancelled(ctx)
		if err != nil || !cancelled {
			return ScanResult{}, errors.New("expected cancellation")
		}
		return ScanResult{}, ErrScanCancelled
	})
	worker, err := NewWorker(WorkerOptions{
		Store: store, Scanner: scanner, RootDirectory: t.TempDir(), WorkerID: "worker-test",
		Lease: time.Second, Heartbeat: 100 * time.Millisecond,
		DefaultRoot: config.LocalLibrary{Directory: store.claim.Root.Path, Mode: string(RootModeReadOnly), IncludePatterns: []string{}, ExcludePatterns: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunNextScan(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.finalStatus != ScanStatusCancelled || store.finalError != nil {
		t.Fatalf("final status=%s error=%v", store.finalStatus, store.finalError)
	}
}

type scannerFunc func(context.Context, ScanInput) (ScanResult, error)

func (function scannerFunc) Scan(ctx context.Context, input ScanInput) (ScanResult, error) {
	return function(ctx, input)
}

type workerStoreStub struct {
	initializeCalls int
	scheduleCalls   int
	defaultMutation RootMutation
	startupIDs      []string
	enqueued        []EnqueueScanCommand
	claim           *ClaimedScan
	cancelled       bool
	owned           bool
	progress        ScanProgress
	completed       ScanResult
	finalStatus     ScanStatus
	finalError      *string
}

func (stub *workerStoreStub) InitializeScans(context.Context, time.Time) error {
	stub.initializeCalls++
	return nil
}
func (stub *workerStoreStub) EnsureDefaultRoot(_ context.Context, mutation RootMutation) (Root, error) {
	stub.defaultMutation = mutation
	return Root{ID: testRootID}, nil
}
func (stub *workerStoreStub) StartupRootIDs(context.Context) ([]string, error) {
	return append([]string(nil), stub.startupIDs...), nil
}
func (stub *workerStoreStub) EnqueueScan(_ context.Context, command EnqueueScanCommand) (ScanRun, error) {
	stub.enqueued = append(stub.enqueued, command)
	return ScanRun{ID: testRunID, RootID: command.RootID}, nil
}
func (stub *workerStoreStub) EnqueueScheduledScans(context.Context, time.Time) error {
	stub.scheduleCalls++
	return nil
}
func (stub *workerStoreStub) ClaimNextScan(context.Context, string, time.Time, time.Duration) (*ClaimedScan, error) {
	claim := stub.claim
	stub.claim = nil
	if claim != nil {
		stub.owned = true
	}
	return claim, nil
}
func (stub *workerStoreStub) HeartbeatScan(context.Context, string, string, string, time.Time, time.Duration) (bool, error) {
	return stub.owned, nil
}
func (stub *workerStoreStub) ScanControl(context.Context, string, string, string) (bool, bool, error) {
	return stub.cancelled, stub.owned, nil
}
func (stub *workerStoreStub) UpdateScanProgress(_ context.Context, _, _, _ string, progress ScanProgress, _ time.Time) (bool, error) {
	stub.progress = progress
	return stub.owned, nil
}
func (stub *workerStoreStub) CompleteScan(_ context.Context, _ ClaimedScan, _, _ string, result ScanResult, _ time.Time) (bool, error) {
	stub.completed = result
	return stub.owned, nil
}
func (stub *workerStoreStub) FinalizeScanFailure(_ context.Context, _ ClaimedScan, _, _ string, status ScanStatus, lastError *string, _ time.Time) (bool, error) {
	stub.finalStatus, stub.finalError = status, lastError
	return stub.owned, nil
}
