package adminmutation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestPermanentDeleteBatchWorkerProcessesClaimedItemsInOrder(t *testing.T) {
	now := time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{claims: []*ClaimedPermanentDeleteItem{
		permanentDeleteClaim("batch-1", "item-1", "track-1", "attempt-1", 0),
		permanentDeleteClaim("batch-1", "item-2", "track-2", "attempt-2", 1),
	}}
	deleter := &permanentDeleteTrackDeleterStub{delete: func(_ context.Context, trackID string, _ int, libraryDirectory string) (DeleteResult, error) {
		if libraryDirectory != "music" {
			t.Fatalf("library directory=%q", libraryDirectory)
		}
		return DeleteResult{DeletedFiles: 1, ScheduledObjects: 2}, nil
	}}
	worker := newPermanentDeleteWorkerForTest(t, store, deleter, now)
	for index := 0; index < 2; index++ {
		worked, err := worker.RunNext(context.Background())
		if err != nil || !worked {
			t.Fatalf("run %d worked/error=%v/%v", index, worked, err)
		}
	}
	worked, err := worker.RunNext(context.Background())
	if err != nil || worked {
		t.Fatalf("empty run worked/error=%v/%v", worked, err)
	}
	if got := deleter.trackIDs(); len(got) != 2 || got[0] != "track-1" || got[1] != "track-2" {
		t.Fatalf("delete order=%v", got)
	}
	if len(store.successes) != 2 || len(store.failures) != 0 || len(store.retries) != 0 {
		t.Fatalf("success/failure/retry=%d/%d/%d", len(store.successes), len(store.failures), len(store.retries))
	}
}

func TestPermanentDeleteBatchWorkerRetriesOnlyMetadataWritebackConflicts(t *testing.T) {
	now := time.Date(2026, time.July, 18, 13, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{claims: []*ClaimedPermanentDeleteItem{
		permanentDeleteClaim("batch-1", "item-1", "track-1", "attempt-1", 0),
	}}
	deleter := &permanentDeleteTrackDeleterStub{delete: func(context.Context, string, int, string) (DeleteResult, error) {
		return DeleteResult{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Tag writeback cancellation is pending",
			map[string]any{"conflictResourceType": "metadata_writeback_job", "conflictResourceId": "writeback-1"},
		)
	}}
	worker := newPermanentDeleteWorkerForTest(t, store, deleter, now)
	worker.retryBackoff = func(int) time.Duration { return 7 * time.Second }
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked/error=%v/%v", worked, err)
	}
	if len(store.retries) != 1 || len(store.failures) != 0 || len(store.successes) != 0 {
		t.Fatalf("retry/failure/success=%d/%d/%d", len(store.retries), len(store.failures), len(store.successes))
	}
	retry := store.retries[0]
	if retry.errorCode != string(apperror.CodeResourceConflict) || retry.message != "Tag writeback cancellation is pending" || !retry.nextAttemptAt.Equal(now.Add(7*time.Second)) {
		t.Fatalf("retry=%+v", retry)
	}
}

func TestPermanentDeleteBatchWorkerRecordsOrdinaryConflictAsFailure(t *testing.T) {
	now := time.Date(2026, time.July, 18, 14, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{claims: []*ClaimedPermanentDeleteItem{
		permanentDeleteClaim("batch-1", "item-1", "track-1", "attempt-1", 0),
	}}
	deleter := &permanentDeleteTrackDeleterStub{delete: func(context.Context, string, int, string) (DeleteResult, error) {
		return DeleteResult{}, apperror.Conflict(apperror.CodeVersionConflict, "Track version changed", nil)
	}}
	worker := newPermanentDeleteWorkerForTest(t, store, deleter, now)
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked/error=%v/%v", worked, err)
	}
	if len(store.failures) != 1 || len(store.retries) != 0 {
		t.Fatalf("failure/retry=%d/%d", len(store.failures), len(store.retries))
	}
	if store.failures[0].errorCode != string(apperror.CodeVersionConflict) || store.failures[0].message != "Track version changed" {
		t.Fatalf("failure=%+v", store.failures[0])
	}
}

func TestPermanentDeleteBatchWorkerConvergesNotFoundAfterCrashAsSuccess(t *testing.T) {
	now := time.Date(2026, time.July, 18, 15, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{claims: []*ClaimedPermanentDeleteItem{
		permanentDeleteClaim("batch-1", "item-1", "track-1", "attempt-1", 0),
	}}
	deleter := &permanentDeleteTrackDeleterStub{delete: func(context.Context, string, int, string) (DeleteResult, error) {
		return DeleteResult{}, apperror.NotFound("Track was not found")
	}}
	worker := newPermanentDeleteWorkerForTest(t, store, deleter, now)
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked/error=%v/%v", worked, err)
	}
	if len(store.successes) != 1 || store.successes[0].result != (DeleteResult{}) || store.successes[0].message == nil {
		t.Fatalf("success=%+v", store.successes)
	}
}

func TestPermanentDeleteBatchWorkerReleasesInterruptedClaim(t *testing.T) {
	now := time.Date(2026, time.July, 18, 16, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{claims: []*ClaimedPermanentDeleteItem{
		permanentDeleteClaim("batch-1", "item-1", "track-1", "attempt-1", 0),
	}}
	started := make(chan struct{})
	deleter := &permanentDeleteTrackDeleterStub{delete: func(ctx context.Context, _ string, _ int, _ string) (DeleteResult, error) {
		close(started)
		<-ctx.Done()
		return DeleteResult{}, ctx.Err()
	}}
	worker, err := NewPermanentDeleteBatchWorker(PermanentDeleteBatchWorkerDependencies{
		Store: store, Deleter: deleter, LibraryDirectory: "music", WorkerID: "worker-1",
		Clock: func() time.Time { return now }, Lease: 2 * time.Hour, Heartbeat: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, runErr := worker.RunNext(ctx)
		result <- runErr
	}()
	<-started
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if len(store.releases) != 1 || len(store.successes)+len(store.failures)+len(store.retries) != 0 {
		t.Fatalf("releases/success/failure/retry=%d/%d/%d/%d", len(store.releases), len(store.successes), len(store.failures), len(store.retries))
	}
}

func TestPermanentDeleteBatchWorkerStopsWhenLeaseIsLost(t *testing.T) {
	now := time.Date(2026, time.July, 18, 17, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{
		claims: []*ClaimedPermanentDeleteItem{permanentDeleteClaim("batch-1", "item-1", "track-1", "attempt-1", 0)},
		renew:  func(context.Context, string, string, string, time.Time, time.Time) (bool, error) { return false, nil },
	}
	deleter := &permanentDeleteTrackDeleterStub{delete: func(ctx context.Context, _ string, _ int, _ string) (DeleteResult, error) {
		<-ctx.Done()
		return DeleteResult{}, ctx.Err()
	}}
	worker, err := NewPermanentDeleteBatchWorker(PermanentDeleteBatchWorkerDependencies{
		Store: store, Deleter: deleter, LibraryDirectory: "music", WorkerID: "worker-1",
		Clock: func() time.Time { return now }, Lease: 20 * time.Millisecond, Heartbeat: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunNext(context.Background())
	if !worked || !errors.Is(err, ErrPermanentDeleteLeaseLost) {
		t.Fatalf("worked/error=%v/%v", worked, err)
	}
	if len(store.releases)+len(store.successes)+len(store.failures)+len(store.retries) != 0 {
		t.Fatal("lease-lost worker mutated terminal state")
	}
}

func TestPermanentDeleteBatchWorkerInitializeRecoversExpiredLeases(t *testing.T) {
	now := time.Date(2026, time.July, 18, 18, 0, 0, 0, time.UTC)
	store := &permanentDeleteWorkerStoreStub{}
	worker := newPermanentDeleteWorkerForTest(t, store, &permanentDeleteTrackDeleterStub{}, now)
	if err := worker.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.initializedAt) != 1 || !store.initializedAt[0].Equal(now) {
		t.Fatalf("initializedAt=%v", store.initializedAt)
	}
}

func newPermanentDeleteWorkerForTest(
	t *testing.T,
	store *permanentDeleteWorkerStoreStub,
	deleter *permanentDeleteTrackDeleterStub,
	now time.Time,
) *PermanentDeleteBatchWorker {
	t.Helper()
	worker, err := NewPermanentDeleteBatchWorker(PermanentDeleteBatchWorkerDependencies{
		Store: store, Deleter: deleter, LibraryDirectory: "music",
		WorkerID: "worker-1", Clock: func() time.Time { return now },
		Lease: 2 * time.Hour, Heartbeat: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func permanentDeleteClaim(batchID, itemID, trackID, attemptID string, position int) *ClaimedPermanentDeleteItem {
	requestedBy := "admin-1"
	return &ClaimedPermanentDeleteItem{
		Job: PermanentDeleteBatchRecord{ID: batchID, RequestedBy: &requestedBy, TraceID: "trace-1", Total: 2},
		Item: PermanentDeleteBatchItemRecord{
			ID: itemID, JobID: batchID, TrackID: trackID, ExpectedVersion: 3,
			Position: position, Status: DeleteBatchItemRunning, Attempts: 1, AttemptID: &attemptID,
		},
	}
}

type permanentDeleteRetryRecord struct {
	itemID, attemptID, workerID, errorCode, message string
	nextAttemptAt, now                              time.Time
}

type permanentDeleteFailureRecord struct {
	claim               ClaimedPermanentDeleteItem
	workerID, errorCode string
	message             string
	now                 time.Time
}

type permanentDeleteSuccessRecord struct {
	claim    ClaimedPermanentDeleteItem
	workerID string
	result   DeleteResult
	message  *string
	now      time.Time
}

type permanentDeleteReleaseRecord struct {
	itemID, attemptID, workerID string
	now                         time.Time
}

type permanentDeleteWorkerStoreStub struct {
	mu            sync.Mutex
	claims        []*ClaimedPermanentDeleteItem
	renew         func(context.Context, string, string, string, time.Time, time.Time) (bool, error)
	initializedAt []time.Time
	retries       []permanentDeleteRetryRecord
	releases      []permanentDeleteReleaseRecord
	successes     []permanentDeleteSuccessRecord
	failures      []permanentDeleteFailureRecord
}

func (store *permanentDeleteWorkerStoreStub) InitializePermanentDeleteBatches(_ context.Context, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.initializedAt = append(store.initializedAt, now)
	return nil
}

func (store *permanentDeleteWorkerStoreStub) ClaimPermanentDeleteBatchItem(context.Context, string, time.Time, time.Duration) (*ClaimedPermanentDeleteItem, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.claims) == 0 {
		return nil, nil
	}
	claim := store.claims[0]
	store.claims = store.claims[1:]
	return claim, nil
}

func (store *permanentDeleteWorkerStoreStub) RenewPermanentDeleteBatchItem(ctx context.Context, itemID, attemptID, workerID string, heartbeatAt, lockedUntil time.Time) (bool, error) {
	if store.renew != nil {
		return store.renew(ctx, itemID, attemptID, workerID, heartbeatAt, lockedUntil)
	}
	return true, nil
}

func (store *permanentDeleteWorkerStoreStub) RetryPermanentDeleteBatchItem(_ context.Context, itemID, attemptID, workerID, errorCode, message string, nextAttemptAt, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.retries = append(store.retries, permanentDeleteRetryRecord{
		itemID: itemID, attemptID: attemptID, workerID: workerID,
		errorCode: errorCode, message: message, nextAttemptAt: nextAttemptAt, now: now,
	})
	return nil
}

func (store *permanentDeleteWorkerStoreStub) ReleasePermanentDeleteBatchItem(_ context.Context, itemID, attemptID, workerID string, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.releases = append(store.releases, permanentDeleteReleaseRecord{itemID: itemID, attemptID: attemptID, workerID: workerID, now: now})
	return nil
}

func (store *permanentDeleteWorkerStoreStub) CompletePermanentDeleteBatchItemSuccess(_ context.Context, claim ClaimedPermanentDeleteItem, workerID string, result DeleteResult, message *string, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.successes = append(store.successes, permanentDeleteSuccessRecord{claim: claim, workerID: workerID, result: result, message: message, now: now})
	return nil
}

func (store *permanentDeleteWorkerStoreStub) CompletePermanentDeleteBatchItemFailure(_ context.Context, claim ClaimedPermanentDeleteItem, workerID, errorCode, message string, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failures = append(store.failures, permanentDeleteFailureRecord{claim: claim, workerID: workerID, errorCode: errorCode, message: message, now: now})
	return nil
}

type permanentDeleteTrackDeleterStub struct {
	mu      sync.Mutex
	delete  func(context.Context, string, int, string) (DeleteResult, error)
	deleted []string
}

func (deleter *permanentDeleteTrackDeleterStub) DeleteTrackPermanently(ctx context.Context, trackID string, expectedVersion int, libraryDirectory string) (DeleteResult, error) {
	deleter.mu.Lock()
	deleter.deleted = append(deleter.deleted, trackID)
	operation := deleter.delete
	deleter.mu.Unlock()
	if operation == nil {
		return DeleteResult{}, nil
	}
	return operation(ctx, trackID, expectedVersion, libraryDirectory)
}

func (deleter *permanentDeleteTrackDeleterStub) trackIDs() []string {
	deleter.mu.Lock()
	defer deleter.mu.Unlock()
	return append([]string(nil), deleter.deleted...)
}
