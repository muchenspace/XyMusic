package admintagscraping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/shared/apperror"
)

const (
	defaultBatchLease     = 120 * time.Second
	defaultBatchHeartbeat = 30 * time.Second
	defaultBatchIdlePoll  = 5 * time.Second
	defaultBatchRetryBase = 5 * time.Second
	defaultBatchRetryMax  = 5 * time.Minute
	// A successful claim window should flow directly into the next window.
	// The idle poll still backs off when no work is available; sleeping after
	// every successful window adds fixed latency to large batches.
	defaultBatchWorkPoll         = 0
	batchCancellationCacheWindow = 500 * time.Millisecond
)

type BatchProcessor interface {
	TrackMetadata(context.Context, string) (TrackMetadata, error)
	Search(context.Context, SearchInput) ([]Candidate, error)
	Apply(context.Context, string, string, ApplyInput) (ApplyResult, error)
}

type BatchMetadataProcessor interface {
	TrackMetadataBatch(context.Context, []string) (map[string]TrackMetadata, error)
}

type batchMetadataApplyProcessor interface {
	applyWithMetadata(context.Context, string, string, ApplyInput, TrackMetadata) (ApplyResult, error)
}

type BatchServiceDependencies struct {
	Store       Store
	Processor   BatchProcessor
	Logger      Logger
	WorkerID    string
	Workers     int
	ClaimWindow int
	Clock       func() time.Time
	Lease       time.Duration
	Heartbeat   time.Duration
	IdlePoll    time.Duration
	WorkingPoll time.Duration
	RetryBase   time.Duration
	RetryMax    time.Duration
}

type BatchService struct {
	store       Store
	processor   BatchProcessor
	logger      Logger
	workerID    string
	workers     int
	claimWindow int
	now         func() time.Time
	lease       time.Duration
	heartbeat   time.Duration
	idlePoll    time.Duration
	workingPoll time.Duration
	retryBase   time.Duration
	retryMax    time.Duration
	wake        chan struct{}

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	cancel      context.CancelFunc
	done        chan struct{}

	activeMu sync.Mutex
	active   map[string]map[string]context.CancelFunc
}

var _ BatchAPI = (*BatchService)(nil)

type NoopLogger struct{}

func (NoopLogger) Info(string, map[string]any)  {}
func (NoopLogger) Warn(string, map[string]any)  {}
func (NoopLogger) Error(string, map[string]any) {}

type batchActivityChecker struct {
	service   *BatchService
	jobID     string
	mu        sync.Mutex
	checkedAt time.Time
	requested bool
}

func (checker *batchActivityChecker) Invalidate() {
	checker.mu.Lock()
	checker.checkedAt = time.Time{}
	checker.mu.Unlock()
}

func (checker *batchActivityChecker) CheckFresh(ctx context.Context) error {
	checker.Invalidate()
	return checker.Check(ctx)
}

func (checker *batchActivityChecker) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := checker.service.now().UTC()
	checker.mu.Lock()
	if !checker.checkedAt.IsZero() && now.Sub(checker.checkedAt) < batchCancellationCacheWindow {
		requested := checker.requested
		checker.mu.Unlock()
		if requested {
			return errBatchCancellationRequested
		}
		return nil
	}
	checker.mu.Unlock()

	requested, err := checker.service.store.BatchCancelRequested(ctx, checker.jobID)
	if err != nil {
		return err
	}
	checker.mu.Lock()
	checker.checkedAt, checker.requested = now, requested
	checker.mu.Unlock()
	if requested {
		return errBatchCancellationRequested
	}
	return nil
}

func NewBatchService(dependencies BatchServiceDependencies) (*BatchService, error) {
	if dependencies.Store == nil {
		return nil, errors.New("admin tag scraping batch store is required")
	}
	if dependencies.Processor == nil {
		return nil, errors.New("admin tag scraping batch processor is required")
	}
	if dependencies.Logger == nil {
		dependencies.Logger = NoopLogger{}
	}
	if dependencies.WorkerID == "" {
		dependencies.WorkerID = "tag-batch-" + uuid.NewString()
	}
	if dependencies.Workers <= 0 {
		dependencies.Workers = 1
	}
	if dependencies.Workers > 64 {
		return nil, errors.New("admin tag scraping batch worker count must not exceed 64")
	}
	if dependencies.ClaimWindow <= 0 {
		dependencies.ClaimWindow = min(64, dependencies.Workers*4)
	}
	if dependencies.ClaimWindow < dependencies.Workers || dependencies.ClaimWindow > 256 {
		return nil, errors.New("admin tag scraping batch claim window must be between worker count and 256")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	if dependencies.Lease <= 0 {
		dependencies.Lease = defaultBatchLease
	}
	if dependencies.Heartbeat <= 0 {
		dependencies.Heartbeat = defaultBatchHeartbeat
	}
	if dependencies.Heartbeat >= dependencies.Lease {
		return nil, errors.New("admin tag scraping heartbeat must be shorter than the lease")
	}
	if dependencies.IdlePoll <= 0 {
		dependencies.IdlePoll = defaultBatchIdlePoll
	}
	if dependencies.WorkingPoll < 0 {
		dependencies.WorkingPoll = defaultBatchWorkPoll
	}
	if dependencies.RetryBase <= 0 {
		dependencies.RetryBase = defaultBatchRetryBase
	}
	if dependencies.RetryMax <= 0 {
		dependencies.RetryMax = defaultBatchRetryMax
	}
	if dependencies.RetryMax < dependencies.RetryBase {
		return nil, errors.New("tag scraping retry maximum must not be shorter than its base delay")
	}
	return &BatchService{
		store: dependencies.Store, processor: dependencies.Processor, logger: dependencies.Logger,
		workerID: dependencies.WorkerID, workers: dependencies.Workers, claimWindow: dependencies.ClaimWindow,
		now: dependencies.Clock, lease: dependencies.Lease, heartbeat: dependencies.Heartbeat,
		idlePoll: dependencies.IdlePoll, workingPoll: dependencies.WorkingPoll,
		retryBase: dependencies.RetryBase, retryMax: dependencies.RetryMax,
		wake: make(chan struct{}, dependencies.Workers), active: make(map[string]map[string]context.CancelFunc),
	}, nil
}

// Start recovers expired leases and starts the durable batch worker. The
// caller owns lifecycle cancellation and must also call Close during shutdown.
func (service *BatchService) Start(ctx context.Context) error {
	service.lifecycleMu.Lock()
	defer service.lifecycleMu.Unlock()
	if service.closed {
		return errors.New("admin tag scraping batch service is closed")
	}
	if service.started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := service.store.RecoverExpiredBatchItems(ctx, service.now().UTC()); err != nil {
		return err
	}
	workerContext, cancel := context.WithCancel(ctx)
	service.cancel = cancel
	service.done = make(chan struct{})
	service.started = true
	go service.runWorkers(workerContext, service.done)
	service.signal()
	return nil
}

func (service *BatchService) Close(ctx context.Context) error {
	service.lifecycleMu.Lock()
	if service.closed {
		done := service.done
		service.lifecycleMu.Unlock()
		if done == nil {
			return nil
		}
		return waitForDone(ctx, done)
	}
	service.closed = true
	cancel := service.cancel
	done := service.done
	service.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	service.cancelActive()
	if done == nil {
		return nil
	}
	return waitForDone(ctx, done)
}

func (service *BatchService) Create(ctx context.Context, actorID string, input CreateBatchInput) (BatchJobDTO, error) {
	if err := validateCreateBatch(input); err != nil {
		return BatchJobDTO{}, err
	}
	filterMissingFields := len(input.Options.MissingFields) > 0
	eligible := make([]BatchItemInput, 0, len(input.Items))
	metadataByTrack := make(map[string]TrackMetadata, len(input.Items))
	if batchProcessor, ok := service.processor.(BatchMetadataProcessor); ok {
		trackIDs := make([]string, 0, len(input.Items))
		for _, item := range input.Items {
			trackIDs = append(trackIDs, item.TrackID)
		}
		var err error
		metadataByTrack, err = batchProcessor.TrackMetadataBatch(ctx, trackIDs)
		if err != nil {
			return BatchJobDTO{}, err
		}
	}
	for _, item := range input.Items {
		metadata, exists := metadataByTrack[item.TrackID]
		var err error
		if !exists {
			metadata, err = service.processor.TrackMetadata(ctx, item.TrackID)
		}
		if err != nil {
			return BatchJobDTO{}, err
		}
		if trackIsArchived(metadata.TrackStatus) {
			return BatchJobDTO{}, archivedTrackError(item.TrackID)
		}
		if metadata.Version != item.ExpectedVersion {
			return BatchJobDTO{}, apperror.Conflict(
				apperror.CodeVersionConflict,
				"曲目 Tag 版本已变化，请刷新后重试",
				map[string]any{
					"trackId": item.TrackID, "expectedVersion": item.ExpectedVersion,
					"currentVersion": metadata.Version,
				},
			)
		}
		if !filterMissingFields || matchesMissingFields(metadata.Effective, input.Options.MissingFields) {
			eligible = append(eligible, item)
		}
	}
	if filterMissingFields {
		if len(eligible) == 0 {
			return BatchJobDTO{}, apperror.Validation("所选曲目均已包含指定字段，无需刮削")
		}
		input.Items = eligible
	}
	if input.Options.WriteBack {
		if err := service.store.ValidateBatchWriteback(ctx, input.Items); err != nil {
			return BatchJobDTO{}, err
		}
	}
	jobID, err := service.store.CreateBatch(ctx, actorID, input)
	if err != nil {
		return BatchJobDTO{}, err
	}
	service.signal()
	return service.Job(ctx, jobID, nil)
}

func (service *BatchService) Job(ctx context.Context, jobID string, updatedAfter *time.Time) (BatchJobDTO, error) {
	job, items, err := service.store.Batch(ctx, jobID, updatedAfter)
	if err != nil {
		return BatchJobDTO{}, err
	}
	return presentBatch(job, items, updatedAfter != nil), nil
}

func (service *BatchService) Cancel(ctx context.Context, jobID string) (BatchJobDTO, error) {
	if err := service.store.RequestBatchCancel(ctx, jobID); err != nil {
		return BatchJobDTO{}, err
	}
	service.cancelJob(jobID)
	service.signal()
	return service.Job(ctx, jobID, nil)
}

func (service *BatchService) Retry(ctx context.Context, jobID string) (BatchJobDTO, error) {
	if err := service.store.RetryBatch(ctx, jobID); err != nil {
		return BatchJobDTO{}, err
	}
	service.signal()
	return service.Job(ctx, jobID, nil)
}

func (service *BatchService) runWorkers(ctx context.Context, done chan struct{}) {
	if batchStore, ok := service.store.(BatchClaimStore); ok {
		service.runBatchWorkers(ctx, batchStore)
		close(done)
		return
	}
	var group sync.WaitGroup
	for index := 0; index < service.workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			service.run(ctx)
		}()
	}
	group.Wait()
	close(done)
}

func (service *BatchService) runBatchWorkers(ctx context.Context, store BatchClaimStore) {
	delay := time.Duration(0)
	for {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-service.wake:
				timer.Stop()
			case <-timer.C:
			}
		} else {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		worked, err := service.processBatchNext(ctx, store)
		if err != nil {
			service.logger.Error("tag_scraping.batch.poll_failed", map[string]any{
				"workerId": service.workerID,
				"message":  messageOf(err),
			})
			delay = service.idlePoll
			continue
		}
		if worked {
			delay = service.workingPoll
		} else {
			delay = service.idlePoll
		}
	}
}

func (service *BatchService) run(ctx context.Context, done ...chan struct{}) {
	if len(done) > 0 && done[0] != nil {
		defer close(done[0])
	}
	delay := time.Duration(0)
	for {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-service.wake:
				timer.Stop()
			case <-timer.C:
			}
		} else {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		worked, err := service.processNext(ctx)
		if err != nil {
			service.logger.Error("tag_scraping.batch.poll_failed", map[string]any{
				"workerId": service.workerID,
				"message":  messageOf(err),
			})
			delay = service.idlePoll
			continue
		}
		if worked {
			delay = service.workingPoll
		} else {
			delay = service.idlePoll
		}
	}
}

func (service *BatchService) processNext(ctx context.Context) (bool, error) {
	now := service.now().UTC()
	claim, err := service.store.ClaimBatchItem(ctx, service.workerID, now, service.lease)
	if err != nil {
		return false, err
	}
	if claim.FinishJobID != "" {
		finished, err := service.store.FinishBatch(ctx, claim.FinishJobID, service.now().UTC())
		return finished, err
	}
	if claim.Item == nil {
		return false, nil
	}
	service.processItem(ctx, *claim.Item)
	return true, nil
}

func (service *BatchService) processBatchNext(ctx context.Context, store BatchClaimStore) (bool, error) {
	now := service.now().UTC()
	result, err := store.ClaimBatchItems(ctx, service.workerID, now, service.lease, service.claimWindow)
	if err != nil {
		return false, err
	}
	if result.FinishJobID != "" {
		finished, err := service.store.FinishBatch(ctx, result.FinishJobID, service.now().UTC())
		return finished, err
	}
	if len(result.Items) == 0 {
		return false, nil
	}
	results := make(chan batchItemExecution, len(result.Items))
	claims := make(chan ClaimedBatchItem)
	var group sync.WaitGroup
	workerCount := min(service.workers, len(result.Items))
	group.Add(workerCount)
	for index := 0; index < workerCount; index++ {
		go func() {
			defer group.Done()
			for claim := range claims {
				results <- service.executeClaim(ctx, claim)
			}
		}()
	}
	for index := range result.Items {
		claims <- result.Items[index]
	}
	close(claims)
	group.Wait()
	close(results)
	ready := make([]batchItemExecution, 0, len(result.Items))
	completionsByJob := make(map[string][]BatchItemCompletion)
	for execution := range results {
		if execution.shouldRelease {
			service.releaseItem(ctx, execution.claim)
			continue
		}
		if execution.shouldRetry {
			if service.retryItem(ctx, &execution) {
				continue
			}
		}
		if !execution.shouldComplete {
			continue
		}
		ready = append(ready, execution)
		jobID := execution.claim.Job.ID
		completionsByJob[jobID] = append(completionsByJob[jobID], execution.completion)
	}
	if batchStore, ok := service.store.(BatchCompleteStore); ok {
		for jobID, completions := range completionsByJob {
			completedIDs, completeErr := batchStore.CompleteBatchItems(
				context.WithoutCancel(ctx), jobID, service.workerID, completions, service.now().UTC(),
			)
			if completeErr != nil {
				for _, completion := range completions {
					execution := findBatchExecution(ready, jobID, completion.ItemID)
					service.logBatchCompletionFailure(jobID, completion, completeErr)
					service.completeItem(context.WithoutCancel(ctx), execution)
				}
				continue
			}
			completed := make(map[string]struct{}, len(completedIDs))
			for _, itemID := range completedIDs {
				completed[itemID] = struct{}{}
			}
			if len(completed) < len(completions) {
				service.logger.Warn("tag_scraping.batch.complete_partial", map[string]any{
					"jobId": jobID, "workerId": service.workerID,
					"expected": len(completions), "completed": len(completed),
				})
			}
			for _, completion := range completions {
				execution := findBatchExecution(ready, jobID, completion.ItemID)
				if _, accepted := completed[completion.ItemID]; !accepted {
					service.logBatchCompletionRejected(execution.claim)
					service.completeItem(context.WithoutCancel(ctx), execution)
					continue
				}
				service.logBatchCompletion(execution.claim, completion.Status)
			}
		}
		return true, nil
	}
	for _, execution := range ready {
		service.completeItem(ctx, execution)
	}
	return true, nil
}

func (service *BatchService) processItem(workerContext context.Context, claim ClaimedBatchItem) {
	execution := service.executeClaim(workerContext, claim)
	if execution.shouldRelease {
		service.releaseItem(workerContext, claim)
		return
	}
	if execution.shouldRetry && service.retryItem(workerContext, &execution) {
		return
	}
	if execution.shouldComplete {
		service.completeItem(workerContext, execution)
	}
}

type batchItemExecution struct {
	claim          ClaimedBatchItem
	completion     BatchItemCompletion
	shouldComplete bool
	shouldRelease  bool
	shouldRetry    bool
	retryAfter     time.Duration
}

type batchRetryHint struct {
	retryAfter time.Duration
	retry      bool
}

func (service *BatchService) executeClaim(
	workerContext context.Context,
	claim ClaimedBatchItem,
) batchItemExecution {
	itemContext, cancel := context.WithCancel(workerContext)
	service.setActiveItem(claim.Job.ID, claim.Item.ID, cancel)
	defer func() {
		cancel()
		service.clearActiveItem(claim.Job.ID, claim.Item.ID)
	}()
	var ownershipLost atomic.Bool
	var cancelRequested atomic.Bool
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(service.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-itemContext.Done():
				return
			case <-ticker.C:
				control, err := service.store.RenewBatchItemLease(
					itemContext, claim.Job.ID, claim.Item.ID, claim.AttemptID, service.workerID,
					service.now().UTC().Add(service.lease),
				)
				if itemContext.Err() != nil {
					return
				}
				if err != nil {
					service.logger.Warn("tag_scraping.batch.renew_failed", map[string]any{
						"jobId": claim.Job.ID, "itemId": claim.Item.ID,
						"attemptId": claim.AttemptID, "workerId": service.workerID,
						"message": messageOf(err),
					})
					ownershipLost.Store(true)
					cancel()
					return
				}
				if !control.Owned {
					service.logger.Warn("tag_scraping.batch.lease_lost", map[string]any{
						"jobId": claim.Job.ID, "itemId": claim.Item.ID,
						"attemptId": claim.AttemptID, "workerId": service.workerID,
					})
					ownershipLost.Store(true)
					cancel()
					return
				}
				if control.CancelRequested {
					cancelRequested.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	retryHint := batchRetryHint{}
	status, candidate, message := service.executeItem(itemContext, claim, &ownershipLost, &retryHint)
	cancel()
	<-heartbeatDone
	if ownershipLost.Load() {
		return batchItemExecution{claim: claim}
	}
	if workerContext.Err() != nil {
		return batchItemExecution{claim: claim, shouldRelease: true}
	}
	if cancelRequested.Load() {
		status, candidate, message = ItemSkipped, nil, "The batch was cancelled"
	}
	shouldRetry := retryHint.retry && status == ItemFailed && claim.Item.Attempts < claim.Item.MaxAttempts
	if retryHint.retry && !shouldRetry && status == ItemFailed {
		message = "All retry attempts failed: " + message
	}
	return batchItemExecution{
		claim: claim,
		completion: BatchItemCompletion{
			ItemID: claim.Item.ID, AttemptID: claim.AttemptID,
			Status: status, Candidate: candidate, Message: message,
		},
		shouldComplete: !shouldRetry,
		shouldRetry:    shouldRetry,
		retryAfter:     retryHint.retryAfter,
	}
}

// retryItem changes only the durable queue state. A retry is deliberately
// separate from completion so transient failures do not increment processed
// counts or become terminal FAILED items.
func (service *BatchService) retryItem(
	workerContext context.Context,
	execution *batchItemExecution,
) bool {
	if execution == nil || !execution.shouldRetry {
		return false
	}
	now := service.now().UTC()
	delay := service.retryDelay(execution.claim.Item.Attempts, execution.retryAfter)
	control, err := service.store.RetryBatchItem(
		context.WithoutCancel(workerContext), execution.claim.Job.ID, execution.claim.Item.ID,
		execution.claim.AttemptID, service.workerID, execution.completion.Candidate,
		execution.completion.Message, now.Add(delay), now,
	)
	if err != nil {
		if !errors.Is(err, ErrBatchLeaseLost) {
			service.logger.Warn("tag_scraping.batch.retry_failed", map[string]any{
				"jobId": execution.claim.Job.ID, "itemId": execution.claim.Item.ID,
				"attemptId": execution.claim.AttemptID, "workerId": service.workerID,
				"message": messageOf(err),
			})
		}
		// Keep the item RUNNING when the retry transaction failed. Lease
		// recovery will return it to the queue without losing the attempt.
		return true
	}
	if control.CancelRequested {
		execution.shouldRetry = false
		execution.shouldComplete = true
		execution.completion.Status = ItemSkipped
		execution.completion.Candidate = nil
		execution.completion.Message = "The batch was cancelled"
		return false
	}
	if !control.Owned {
		return true
	}
	service.logger.Info("tag_scraping.batch.item_requeued", map[string]any{
		"jobId": execution.claim.Job.ID, "itemId": execution.claim.Item.ID,
		"attemptId": execution.claim.AttemptID, "workerId": service.workerID,
		"attempts": execution.claim.Item.Attempts, "retryAfterMs": delay.Milliseconds(),
	})
	service.signal()
	return true
}

func (service *BatchService) retryDelay(attempt int, hint time.Duration) time.Duration {
	delay := service.retryBase
	for current := 1; current < attempt && delay < service.retryMax; current++ {
		if delay > service.retryMax/2 {
			delay = service.retryMax
			break
		}
		delay *= 2
	}
	if hint > delay {
		delay = hint
	}
	if delay > service.retryMax {
		return service.retryMax
	}
	return delay
}

func (service *BatchService) releaseItem(workerContext context.Context, claim ClaimedBatchItem) {
	if err := service.store.ReleaseBatchItem(
		context.WithoutCancel(workerContext), claim.Item.ID, claim.AttemptID,
		service.workerID, service.now().UTC(),
	); err != nil {
		service.logger.Warn("tag_scraping.batch.release_failed", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID,
			"attemptId": claim.AttemptID, "workerId": service.workerID,
			"message": messageOf(err),
		})
	}
}

func (service *BatchService) completeItem(workerContext context.Context, execution batchItemExecution) {
	claim := execution.claim
	completion := execution.completion
	completed, err := service.store.CompleteBatchItem(
		context.WithoutCancel(workerContext), claim.Job.ID, completion.ItemID, completion.AttemptID,
		service.workerID, completion.Status, completion.Candidate, completion.Message, service.now().UTC(),
	)
	if err != nil {
		service.logBatchCompletionFailure(claim.Job.ID, completion, err)
		return
	}
	if !completed {
		service.logBatchCompletionRejected(claim)
		return
	}
	service.logBatchCompletion(claim, completion.Status)
}

func (service *BatchService) logBatchCompletionFailure(
	jobID string,
	completion BatchItemCompletion,
	err error,
) {
	service.logger.Warn("tag_scraping.batch.complete_failed", map[string]any{
		"jobId": jobID, "itemId": completion.ItemID,
		"attemptId": completion.AttemptID, "workerId": service.workerID,
		"message": messageOf(err),
	})
}

func (service *BatchService) logBatchCompletionRejected(claim ClaimedBatchItem) {
	service.logger.Warn("tag_scraping.batch.complete_rejected", map[string]any{
		"jobId": claim.Job.ID, "itemId": claim.Item.ID,
		"attemptId": claim.AttemptID, "workerId": service.workerID,
	})
}

func (service *BatchService) logBatchCompletion(claim ClaimedBatchItem, status ItemStatus) {
	service.logger.Info("tag_scraping.batch.item_completed", map[string]any{
		"jobId": claim.Job.ID, "itemId": claim.Item.ID,
		"attemptId": claim.AttemptID, "workerId": service.workerID,
		"status": string(status),
	})
}

func findBatchExecution(
	executions []batchItemExecution,
	jobID string,
	itemID string,
) batchItemExecution {
	for _, execution := range executions {
		if execution.claim.Job.ID == jobID && execution.claim.Item.ID == itemID {
			return execution
		}
	}
	return batchItemExecution{claim: ClaimedBatchItem{
		Job: BatchJobRecord{ID: jobID}, Item: BatchItemRecord{ID: itemID},
	}}
}

func (service *BatchService) executeItem(
	ctx context.Context,
	claim ClaimedBatchItem,
	ownershipLost *atomic.Bool,
	retryOutput ...*batchRetryHint,
) (ItemStatus, *Candidate, string) {
	markTransient := func(err error) {
		if retry, hint := transientBatchItemError(err); retry && len(retryOutput) > 0 && retryOutput[0] != nil {
			retryOutput[0].retry = true
			if hint > retryOutput[0].retryAfter {
				retryOutput[0].retryAfter = hint
			}
		}
	}
	ctx = withBatchMutationFence(ctx, &BatchMutationFence{
		JobID: claim.Job.ID, ItemID: claim.Item.ID,
		AttemptID: claim.AttemptID, WorkerID: service.workerID,
	})
	if claim.Job.RequestedBy == nil {
		return ItemFailed, nil, "The administrator who created the job no longer exists"
	}
	activity := &batchActivityChecker{service: service, jobID: claim.Job.ID}
	ensureActive := activity.Check
	if err := ensureActive(ctx); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
	}
	var metadata TrackMetadata
	var err error
	if claim.Metadata != nil {
		metadata = *claim.Metadata
	} else {
		metadata, err = service.processor.TrackMetadata(ctx, claim.Item.TrackID)
		if err != nil {
			return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
		}
	}
	if trackIsArchived(metadata.TrackStatus) {
		return ItemSkipped, nil, archivedBatchItemMessage
	}
	if err := ensureActive(ctx); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
	}
	if !matchesMissingFields(metadata.Effective, claim.Job.Options.MissingFields) {
		return ItemSkipped, nil, "The track does not match the configured missing-field conditions"
	}
	query := SearchInput{Title: &metadata.Effective.Title, Verbatim: claim.Job.Options.Verbatim}
	artistNames := make([]string, 0, len(metadata.Effective.Credits))
	for _, credit := range metadata.Effective.Credits {
		artistNames = append(artistNames, credit.Name)
	}
	artist := strings.Join(artistNames, ",")
	query.Artist = &artist
	if metadata.Effective.Album != nil {
		query.Album = metadata.Effective.Album
	} else {
		empty := ""
		query.Album = &empty
	}
	type sourceSearchResult struct {
		source  Source
		matches []Candidate
		err     error
	}
	var selected *Candidate
	sourceErrors := make([]string, 0)
	successfulSources := 0
	recordSearch := func(source Source, matches []Candidate, searchErr error) {
		if searchErr != nil {
			markTransient(searchErr)
			sourceErrors = append(sourceErrors, string(source)+": "+messageOf(searchErr))
			return
		}
		successfulSources++
		if selected != nil {
			return
		}
		for index := range matches {
			if reliableTagMatch(matches[index], claim.Job.Options.MatchMode) {
				match := matches[index]
				selected = &match
				return
			}
		}
	}
	for sourceIndex := 0; sourceIndex < len(claim.Job.Options.Sources); sourceIndex++ {
		if ctx.Err() != nil || ownershipLost.Load() {
			return ItemSkipped, nil, "The batch was cancelled"
		}
		if err := ensureActive(ctx); err != nil {
			return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
		}
		remaining := claim.Job.Options.Sources[sourceIndex:]
		if sourceIndex > 0 && len(remaining) > 1 {
			results := make([]sourceSearchResult, len(remaining))
			var searchGroup sync.WaitGroup
			for resultIndex, source := range remaining {
				resultIndex, source := resultIndex, source
				searchGroup.Add(1)
				go func() {
					defer searchGroup.Done()
					searchQuery := query
					searchQuery.Source = source
					matches, searchErr := service.processor.Search(ctx, searchQuery)
					results[resultIndex] = sourceSearchResult{
						source: source, matches: matches, err: searchErr,
					}
				}()
			}
			searchGroup.Wait()
			for _, result := range results {
				recordSearch(result.source, result.matches, result.err)
			}
			break
		}
		source := claim.Job.Options.Sources[sourceIndex]
		query.Source = source
		matches, searchErr := service.processor.Search(ctx, query)
		recordSearch(query.Source, matches, searchErr)
		if selected != nil {
			break
		}
	}
	// Refresh after the complete read-only search phase so cancellation cannot
	// pass through to Apply, without querying once for every source.
	if err := activity.CheckFresh(ctx); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
	}
	if selected == nil {
		if len(retryOutput) > 0 && retryOutput[0] != nil && retryOutput[0].retry {
			message := "Scraping sources were temporarily unavailable"
			if len(sourceErrors) > 0 {
				message += ": " + strings.Join(sourceErrors, "; ")
			}
			return ItemFailed, nil, message
		}
		if successfulSources == 0 && len(sourceErrors) == len(claim.Job.Options.Sources) {
			return ItemFailed, nil, "All scraping sources failed: " + strings.Join(sourceErrors, "; ")
		}
		message := "No reliable match was found"
		if len(sourceErrors) > 0 {
			message += "; some sources failed: " + strings.Join(sourceErrors, "; ")
		}
		return ItemSkipped, nil, message
	}
	if err := ensureActive(ctx); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
	}
	applyInput := ApplyInput{
		ExpectedVersion: claim.Item.ExpectedVersion,
		Candidate:       *selected,
		Verbatim:        claim.Job.Options.Verbatim,
		Fields:          claim.Job.Options.Fields,
		WriteBack:       claim.Job.Options.WriteBack,
		Reason:          claim.Job.Options.Reason,
		cancellationCheck: func(checkContext context.Context) error {
			// The fresh check immediately before Apply protects the read-to-write
			// boundary. Apply's repository mutation fence checks the durable
			// cancellation flag again inside the write transaction, so forcing a
			// separate query before every optional Apply phase only adds latency.
			return activity.Check(checkContext)
		},
	}
	var result ApplyResult
	if batchProcessor, ok := service.processor.(batchMetadataApplyProcessor); ok && claim.Metadata != nil {
		result, err = batchProcessor.applyWithMetadata(
			ctx, *claim.Job.RequestedBy, claim.Item.TrackID, applyInput, *claim.Metadata,
		)
	} else {
		result, err = service.processor.Apply(
			ctx, *claim.Job.RequestedBy, claim.Item.TrackID, applyInput,
		)
	}
	if err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost, retryOutput...)
	}
	message := strings.Join(result.Warnings, "; ")
	if message == "" {
		message = "Scraping completed"
	}
	return ItemSucceeded, selected, message
}

func (service *BatchService) itemErrorStatus(
	ctx context.Context,
	err error,
	ownershipLost *atomic.Bool,
	retryOutput ...*batchRetryHint,
) (ItemStatus, *Candidate, string) {
	if errors.Is(err, ErrBatchLeaseLost) {
		if ownershipLost != nil {
			ownershipLost.Store(true)
		}
		return ItemSkipped, nil, "The batch item lease was lost"
	}
	if ctx.Err() != nil || errors.Is(err, errBatchCancellationRequested) {
		return ItemSkipped, nil, "The batch was cancelled"
	}
	if isArchivedTrackError(err) {
		return ItemSkipped, nil, archivedBatchItemMessage
	}
	if retry, hint := transientBatchItemError(err); retry && len(retryOutput) > 0 && retryOutput[0] != nil {
		retryOutput[0].retry = true
		if hint > retryOutput[0].retryAfter {
			retryOutput[0].retryAfter = hint
		}
	}
	return ItemFailed, nil, messageOf(err)
}

func transientBatchItemError(err error) (bool, time.Duration) {
	if err == nil || errors.Is(err, context.Canceled) {
		return false, 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true, 0
	}
	applicationError, ok := apperror.As(err)
	if !ok {
		return false, 0
	}
	switch applicationError.Code {
	case apperror.CodeDependencyUnavailable, apperror.CodeRateLimited:
		return true, retryAfterFromMetadata(applicationError.Metadata)
	default:
		return false, 0
	}
}

func (service *BatchService) signal() {
	for index := 0; index < service.workers; index++ {
		select {
		case service.wake <- struct{}{}:
		default:
			return
		}
	}
}

func (service *BatchService) setActive(jobID string, cancel context.CancelFunc) {
	service.setActiveItem(jobID, "", cancel)
}

func (service *BatchService) setActiveItem(jobID, itemID string, cancel context.CancelFunc) {
	service.activeMu.Lock()
	items := service.active[jobID]
	if items == nil {
		items = make(map[string]context.CancelFunc)
		service.active[jobID] = items
	}
	items[itemID] = cancel
	service.activeMu.Unlock()
}

func (service *BatchService) clearActive(jobID string) {
	service.activeMu.Lock()
	delete(service.active, jobID)
	service.activeMu.Unlock()
}

func (service *BatchService) clearActiveItem(jobID, itemID string) {
	service.activeMu.Lock()
	items := service.active[jobID]
	delete(items, itemID)
	if len(items) == 0 {
		delete(service.active, jobID)
	}
	service.activeMu.Unlock()
}

func (service *BatchService) cancelJob(jobID string) {
	service.activeMu.Lock()
	items := service.active[jobID]
	cancellations := make([]context.CancelFunc, 0, len(items))
	for _, cancel := range items {
		cancellations = append(cancellations, cancel)
	}
	service.activeMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (service *BatchService) cancelActive() {
	service.activeMu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(service.active))
	for _, items := range service.active {
		for _, cancel := range items {
			cancellations = append(cancellations, cancel)
		}
	}
	service.activeMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func validateCreateBatch(input CreateBatchInput) error {
	if len(input.Items) < 1 || len(input.Items) > maxTagScrapingBatchItems {
		return apperror.Validation("A tag scraping batch must contain 1 to 5000 tracks")
	}
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if item.TrackID == "" || item.ExpectedVersion < 1 {
			return apperror.Validation("A batch item is invalid")
		}
		if _, duplicate := seen[item.TrackID]; duplicate {
			return apperror.Validation("Batch track IDs must be unique")
		}
		seen[item.TrackID] = struct{}{}
	}
	if len(input.Options.Sources) < 1 || len(input.Options.Sources) > 5 || !uniqueSources(input.Options.Sources) {
		return apperror.Validation("Batch scraping sources are invalid")
	}
	if input.Options.Verbatim && !onlyQQMusicSource(input.Options.Sources) {
		return unsupportedVerbatimSourceError()
	}
	if input.Options.MatchMode != MatchStrict && input.Options.MatchMode != MatchSimple {
		return apperror.Validation("Batch match mode is invalid")
	}
	if len(input.Options.MissingFields) > 6 || !uniqueMissingFields(input.Options.MissingFields) {
		return apperror.Validation("Batch missing-field conditions are invalid")
	}
	if reason := normalizeText(input.Options.Reason); javascriptLength(reason) < 2 || javascriptLength(reason) > 500 {
		return apperror.Validation("Batch reason is invalid")
	}
	return nil
}

func onlyQQMusicSource(sources []Source) bool {
	for _, source := range sources {
		if source != SourceQMusic {
			return false
		}
	}
	return len(sources) > 0
}

func presentBatch(job BatchJobRecord, items []BatchItemRecord, partial bool) BatchJobDTO {
	skipped := max(0, job.Processed-job.Succeeded-job.Failed)
	result := BatchJobDTO{
		ID: job.ID, RequestedBy: job.RequestedBy, Options: job.Options, Status: job.Status,
		Total: job.Total, Processed: job.Processed, Succeeded: job.Succeeded, Failed: job.Failed,
		Skipped:         skipped,
		CancelRequested: job.CancelRequested, StartedAt: optionalTimestamp(job.StartedAt),
		CompletedAt: optionalTimestamp(job.CompletedAt), CreatedAt: formatTimestamp(job.CreatedAt),
		UpdatedAt: formatTimestamp(job.UpdatedAt), Unsuccessful: max(0, job.Processed-job.Succeeded),
		PartialItems: partial, Items: make([]BatchItemDTO, 0, len(items)),
	}
	for _, item := range items {
		result.Items = append(result.Items, BatchItemDTO{
			ID: item.ID, JobID: item.JobID, TrackID: item.TrackID, ExpectedVersion: item.ExpectedVersion,
			Position: item.Position, Status: item.Status, Attempts: item.Attempts, MaxAttempts: item.MaxAttempts,
			NextAttemptAt: formatTimestamp(item.NextAttemptAt), Candidate: item.Candidate, Source: item.Source,
			Message: item.Message, CreatedAt: formatTimestamp(item.CreatedAt), UpdatedAt: formatTimestamp(item.UpdatedAt),
			StartedAt: optionalTimestamp(item.StartedAt), CompletedAt: optionalTimestamp(item.CompletedAt),
		})
	}
	return result
}

func formatTimestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func optionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTimestamp(*value)
	return &formatted
}

func waitForDone(ctx context.Context, done <-chan struct{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("close admin tag scraping batch worker: %w", ctx.Err())
	}
}
