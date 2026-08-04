package admintagscraping

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkBatchServiceLoad(b *testing.B) {
	for _, itemCount := range []int{1_000, 5_000} {
		for _, workers := range []int{1, 8} {
			b.Run(batchLoadBenchmarkName(itemCount, workers), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				benchmarkStarted := time.Now()
				for iteration := 0; iteration < b.N; iteration++ {
					store := newBatchLoadStore(itemCount)
					processor := &batchLoadProcessor{}
					service, err := NewBatchService(BatchServiceDependencies{
						Store: store, Processor: processor, WorkerID: "benchmark-worker",
						Workers: workers, Heartbeat: time.Hour, Lease: 2 * time.Hour,
					})
					if err != nil {
						b.Fatal(err)
					}
					ctx := context.Background()
					if err := store.RecoverExpiredBatchItems(ctx, time.Now().UTC()); err != nil {
						b.Fatal(err)
					}
					if err := runBatchLoadWorkers(ctx, service, store, workers); err != nil {
						b.Fatal(err)
					}
					if completed := store.completed.Load(); completed != int64(itemCount) {
						b.Fatalf("completed=%d want=%d", completed, itemCount)
					}
					if recovered := store.recoveredItems.Load(); recovered != 1 {
						b.Fatalf("recovered_items=%d want=1", recovered)
					}
					b.ReportMetric(float64(store.completed.Load()), "items")
					b.ReportMetric(float64(processor.searchCalls.Load()), "upstream_requests")
					b.ReportMetric(float64(store.dbCalls.Load()), "store_calls")
					b.ReportMetric(float64(store.claimCalls.Load()), "claim_calls")
					b.ReportMetric(float64(store.completionCalls.Load()), "completion_calls")
					b.ReportMetric(float64(store.dbCalls.Load())/float64(itemCount), "store_calls/item")
					b.ReportMetric(float64(store.queueWaitNs.Load())/float64(itemCount), "claim_wait_ns/item")
					b.ReportMetric(float64(store.recoveryCalls.Load()), "lease_recoveries")
				}
				b.ReportMetric(float64(itemCount*b.N)/time.Since(benchmarkStarted).Seconds(), "items/sec")
			})
		}
	}
}

func batchLoadBenchmarkName(itemCount, workers int) string {
	return "items-" + formatBenchmarkInt(itemCount) + "/workers-" + formatBenchmarkInt(workers)
}

func formatBenchmarkInt(value int) string {
	return strconv.Itoa(value)
}

func runBatchLoadWorkers(
	ctx context.Context,
	service *BatchService,
	store *batchLoadStore,
	workers int,
) error {
	var group sync.WaitGroup
	errorsFound := make(chan error, workers)
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer group.Done()
			for {
				var worked bool
				var err error
				if batchStore, ok := any(store).(BatchClaimStore); ok {
					worked, err = service.processBatchNext(ctx, batchStore)
				} else {
					worked, err = service.processNext(ctx)
				}
				if err != nil {
					errorsFound <- err
					return
				}
				if worked {
					continue
				}
				if store.completed.Load() >= int64(store.total) {
					return
				}
				select {
				case <-store.completedSignal:
				case <-ctx.Done():
					return
				default:
					runtime.Gosched()
				}
			}
		}()
	}
	group.Wait()
	select {
	case err := <-errorsFound:
		return err
	default:
		return nil
	}
}

type batchLoadStore struct {
	*storeStub
	total           int
	next            int
	mu              sync.Mutex
	completed       atomic.Int64
	completionCalls atomic.Int64
	recoveredItems  atomic.Int64
	dbCalls         atomic.Int64
	claimCalls      atomic.Int64
	queueWaitNs     atomic.Int64
	recoveryCalls   atomic.Int64
	completedSignal chan struct{}
	expiredLease    bool
	recoveryPending bool
}

func newBatchLoadStore(total int) *batchLoadStore {
	return &batchLoadStore{
		storeStub:       &storeStub{},
		total:           total,
		next:            1,
		expiredLease:    true,
		completedSignal: make(chan struct{}, total),
	}
}

func (store *batchLoadStore) RecoverExpiredBatchItems(context.Context, time.Time) error {
	store.mu.Lock()
	// Model one RUNNING item whose lease expired before this worker start. The
	// recovery pass moves it back to the pending queue before normal claims.
	if store.expiredLease {
		store.expiredLease = false
		store.recoveryPending = true
	}
	store.mu.Unlock()
	store.dbCalls.Add(1)
	store.recoveryCalls.Add(1)
	return nil
}

func (store *batchLoadStore) ClaimBatchItem(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (ClaimResult, error) {
	if err := ctx.Err(); err != nil {
		return ClaimResult{}, err
	}
	started := time.Now()
	store.mu.Lock()
	store.queueWaitNs.Add(time.Since(started).Nanoseconds())
	store.dbCalls.Add(1)
	store.claimCalls.Add(1)
	if store.recoveryPending {
		store.recoveryPending = false
		store.mu.Unlock()
		store.recoveredItems.Add(1)
		jobActor := "benchmark-admin"
		return ClaimResult{Item: &ClaimedBatchItem{
			Job: BatchJobRecord{ID: "benchmark-job", RequestedBy: &jobActor, Status: JobRunning,
				Options: BatchOptions{Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
					Fields: ApplyFields{Title: true}, Reason: "benchmark"}},
			Item: BatchItemRecord{ID: "item-recovered", JobID: "benchmark-job",
				TrackID: "track-recovered", ExpectedVersion: 1,
				Position: 0, Status: ItemRunning, CreatedAt: now, UpdatedAt: now},
			AttemptID: "recovered-attempt",
		}}, nil
	}
	if store.next >= store.total {
		store.mu.Unlock()
		return ClaimResult{}, nil
	}
	position := store.next
	store.next++
	store.mu.Unlock()

	jobActor := "benchmark-admin"
	attempt := "attempt-" + formatBenchmarkInt(position)
	return ClaimResult{Item: &ClaimedBatchItem{
		Job: BatchJobRecord{ID: "benchmark-job", RequestedBy: &jobActor, Status: JobRunning,
			Options: BatchOptions{Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
				Fields: ApplyFields{Title: true}, Reason: "benchmark"}},
		Item: BatchItemRecord{ID: "item-" + formatBenchmarkInt(position), JobID: "benchmark-job",
			TrackID: "track-" + formatBenchmarkInt(position), ExpectedVersion: 1,
			Position: position, Status: ItemRunning, CreatedAt: now, UpdatedAt: now},
		AttemptID: attempt,
	}}, nil
}

func (store *batchLoadStore) ClaimBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (BatchClaimResult, error) {
	if err := ctx.Err(); err != nil {
		return BatchClaimResult{}, err
	}
	if limit <= 0 {
		return BatchClaimResult{}, nil
	}
	started := time.Now()
	store.mu.Lock()
	store.queueWaitNs.Add(time.Since(started).Nanoseconds())
	store.dbCalls.Add(1)
	store.claimCalls.Add(1)
	claims := make([]ClaimedBatchItem, 0, limit)
	if store.recoveryPending {
		store.recoveryPending = false
		store.recoveredItems.Add(1)
		claims = append(claims, benchmarkBatchClaim("item-recovered", "track-recovered", 0, "recovered-attempt", now))
	}
	for len(claims) < limit && store.next < store.total {
		position := store.next
		store.next++
		claims = append(claims, benchmarkBatchClaim(
			"item-"+formatBenchmarkInt(position), "track-"+formatBenchmarkInt(position),
			position, "attempt-"+formatBenchmarkInt(position), now,
		))
	}
	store.mu.Unlock()
	_ = workerID
	_ = lease
	return BatchClaimResult{Items: claims}, nil
}

func benchmarkBatchClaim(itemID, trackID string, position int, attemptID string, now time.Time) ClaimedBatchItem {
	jobActor := "benchmark-admin"
	metadata := TrackMetadata{TrackID: trackID, Version: 1, Effective: MetadataSnapshot{
		Title: "Song", Credits: []MetadataCredit{{Name: "Artist", Role: "PRIMARY"}},
	}}
	return ClaimedBatchItem{
		Job: BatchJobRecord{ID: "benchmark-job", RequestedBy: &jobActor, Status: JobRunning,
			Options: BatchOptions{Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
				Fields: ApplyFields{Title: true}, Reason: "benchmark"}},
		Item: BatchItemRecord{ID: itemID, JobID: "benchmark-job", TrackID: trackID,
			ExpectedVersion: 1, Position: position, Status: ItemRunning, CreatedAt: now, UpdatedAt: now},
		AttemptID: attemptID, Metadata: &metadata,
	}
}

func (store *batchLoadStore) BatchCancelRequested(context.Context, string) (bool, error) {
	store.dbCalls.Add(1)
	return false, nil
}

func (store *batchLoadStore) RenewBatchItemLease(
	context.Context, string, string, string, string, time.Time,
) (BatchLeaseControl, error) {
	store.dbCalls.Add(1)
	return BatchLeaseControl{Owned: true}, nil
}

func (store *batchLoadStore) CompleteBatchItem(
	ctx context.Context,
	_, _, _, _ string,
	status ItemStatus,
	_ *Candidate,
	_ string,
	_ time.Time,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	store.dbCalls.Add(1)
	if status != ItemSucceeded {
		return false, errors.New("benchmark item did not succeed")
	}
	store.completed.Add(1)
	select {
	case store.completedSignal <- struct{}{}:
	default:
	}
	return true, nil
}

func (store *batchLoadStore) CompleteBatchItems(
	ctx context.Context,
	_, _ string,
	completions []BatchItemCompletion,
	_ time.Time,
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.dbCalls.Add(1)
	store.completionCalls.Add(1)
	completedIDs := make([]string, 0, len(completions))
	for _, completion := range completions {
		if completion.Status != ItemSucceeded {
			return nil, errors.New("benchmark item did not succeed")
		}
		completedIDs = append(completedIDs, completion.ItemID)
	}
	store.completed.Add(int64(len(completions)))
	for range completions {
		select {
		case store.completedSignal <- struct{}{}:
		default:
		}
	}
	return completedIDs, nil
}

type batchLoadProcessor struct {
	searchCalls atomic.Int64
}

func (*batchLoadProcessor) TrackMetadata(context.Context, string) (TrackMetadata, error) {
	return TrackMetadata{Version: 1, Effective: MetadataSnapshot{
		Title: "Song", Credits: []MetadataCredit{{Name: "Artist", Role: "PRIMARY"}},
	}}, nil
}

func (processor *batchLoadProcessor) Search(ctx context.Context, input SearchInput) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	processor.searchCalls.Add(1)
	return []Candidate{
		{ID: "candidate", Name: "Song", Artist: "Artist", Source: input.Source, Score: benchmarkFloatPointer(4)},
	}, nil
}

func (*batchLoadProcessor) Apply(
	ctx context.Context, _, _, _ string, _ ApplyInput,
) (ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{}, nil
}

func benchmarkFloatPointer(value float64) *float64 { return &value }
