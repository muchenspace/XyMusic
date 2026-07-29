package admintagscraping

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/shared/apperror"
)

const (
	defaultBatchLease            = 120 * time.Second
	defaultBatchHeartbeat        = 30 * time.Second
	defaultBatchIdlePoll         = 5 * time.Second
	defaultBatchWorkPoll         = 250 * time.Millisecond
	defaultBatchWorkers          = 64
	maximumBatchRecoveryAttempts = 3
)

var defaultBatchChannelConcurrency = map[Source]int{
	SourceQMusic:  64,
	SourceNetease: 64,
	SourceKugou:   64,
}

var maximumBatchChannelConcurrency = map[Source]int{
	SourceQMusic:  64,
	SourceNetease: 64,
	SourceKugou:   64,
}

type BatchProcessor interface {
	TrackMetadata(context.Context, string) (TrackMetadata, error)
	Search(context.Context, SearchInput) ([]Candidate, error)
	Apply(context.Context, string, string, string, ApplyInput) (ApplyResult, error)
}

type BatchServiceDependencies struct {
	Store       Store
	Processor   BatchProcessor
	Logger      Logger
	WorkerID    string
	Clock       func() time.Time
	Lease       time.Duration
	Heartbeat   time.Duration
	IdlePoll    time.Duration
	WorkingPoll time.Duration
	Workers     int
	Metrics     PerformanceMetrics
}

type BatchService struct {
	store       Store
	processor   BatchProcessor
	logger      Logger
	workerID    string
	now         func() time.Time
	lease       time.Duration
	heartbeat   time.Duration
	idlePoll    time.Duration
	workingPoll time.Duration
	workers     int
	metrics     PerformanceMetrics
	wake        chan struct{}

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	cancel      context.CancelFunc
	done        chan struct{}

	activeMu sync.Mutex
	active   map[string][]*activeBatchItem

	channelMu     sync.Mutex
	channelGates  map[string]map[Source]*batchChannelGate
	channelGlobal map[Source]*batchChannelGate
	channelActive map[Source]int
}

type activeBatchItem struct {
	cancel     context.CancelFunc
	cancelling func()
}

type batchProgress struct {
	mu           sync.Mutex
	stage        ItemStage
	message      string
	retryCount   int
	retryAfterAt *time.Time
}

func newBatchProgress() *batchProgress {
	return &batchProgress{stage: StageSearchCandidates}
}

func (progress *batchProgress) set(stage ItemStage, message string, retryCount int, retryAfterAt *time.Time) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if retryCount == 0 && progress.retryCount > 0 {
		retryCount = progress.retryCount
	}
	progress.stage = stage
	progress.message = message
	progress.retryCount = max(0, retryCount)
	progress.retryAfterAt = retryAfterAt
}

func (progress *batchProgress) snapshot() (ItemStage, string, int, *time.Time) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	var retryAfterAt *time.Time
	if progress.retryAfterAt != nil {
		value := *progress.retryAfterAt
		retryAfterAt = &value
	}
	return progress.stage, progress.message, progress.retryCount, retryAfterAt
}

var _ BatchAPI = (*BatchService)(nil)

type NoopLogger struct{}

func (NoopLogger) Info(string, map[string]any)  {}
func (NoopLogger) Warn(string, map[string]any)  {}
func (NoopLogger) Error(string, map[string]any) {}

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
	if dependencies.WorkingPoll <= 0 {
		dependencies.WorkingPoll = defaultBatchWorkPoll
	}
	if dependencies.Workers == 0 {
		dependencies.Workers = defaultBatchWorkers
	}
	if dependencies.Workers < 1 || dependencies.Workers > 64 {
		return nil, errors.New("admin tag scraping workers must be from 1 to 64")
	}
	return &BatchService{
		store: dependencies.Store, processor: dependencies.Processor, logger: dependencies.Logger,
		workerID: dependencies.WorkerID,
		now:      dependencies.Clock, lease: dependencies.Lease, heartbeat: dependencies.Heartbeat,
		idlePoll: dependencies.IdlePoll, workingPoll: dependencies.WorkingPoll,
		workers: dependencies.Workers, wake: make(chan struct{}, 1), active: make(map[string][]*activeBatchItem),
		metrics:       dependencies.Metrics,
		channelGates:  make(map[string]map[Source]*batchChannelGate),
		channelGlobal: make(map[Source]*batchChannelGate), channelActive: make(map[Source]int),
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
	go service.run(workerContext, service.done)
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
	input.Options = normalizeBatchOptions(input.Options)
	if err := validateCreateBatch(input); err != nil {
		return BatchJobDTO{}, err
	}
	filterMissingFields := len(input.Options.MissingFields) > 0
	eligible := make([]BatchItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		metadata, err := service.processor.TrackMetadata(ctx, item.TrackID)
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
	result := presentBatch(job, items, updatedAfter != nil && !isTerminalJobStatus(job.Status))
	result.ChannelStatus = service.channelStatus(job.ID, job.Options)
	result.Concurrency = service.concurrencyStatus(job)
	if isTerminalJobStatus(job.Status) {
		service.deleteChannelGates(job.ID)
	}
	return result, nil
}

func (service *BatchService) Cancel(ctx context.Context, jobID string) (BatchJobDTO, error) {
	if err := service.store.RequestBatchCancel(ctx, jobID); err != nil {
		return BatchJobDTO{}, err
	}
	service.cancelJob(jobID)
	service.signal()
	result, err := service.Job(ctx, jobID, nil)
	if err == nil && isTerminalJobStatus(result.Status) {
		service.deleteChannelGates(jobID)
	}
	return result, err
}

func (service *BatchService) Retry(ctx context.Context, jobID string) (BatchJobDTO, error) {
	if err := service.store.RetryBatch(ctx, jobID); err != nil {
		return BatchJobDTO{}, err
	}
	service.signal()
	return service.Job(ctx, jobID, nil)
}

func (service *BatchService) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	var group sync.WaitGroup
	for index := 0; index < service.workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			service.runWorker(ctx)
		}()
	}
	group.Wait()
}

func (service *BatchService) runWorker(ctx context.Context) {
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
		} else if ctx.Err() != nil {
			return
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
	claim, err := service.store.ClaimBatchItem(ctx, service.workerID, service.lease)
	if err != nil {
		return false, err
	}
	for _, jobID := range claim.FinishedJobIDs {
		service.deleteChannelGates(jobID)
	}
	if claim.FinishJobID != "" {
		service.deleteChannelGates(claim.FinishJobID)
		return true, nil
	}
	if claim.Item == nil {
		return false, nil
	}
	service.processItem(ctx, *claim.Item)
	return true, nil
}

func (service *BatchService) updateBatchProgress(
	ctx context.Context,
	claim ClaimedBatchItem,
	progress *batchProgress,
	stage ItemStage,
	message string,
	retryCount int,
	retryAfterAt *time.Time,
) {
	progress.set(stage, message, retryCount, retryAfterAt)
	updater, ok := service.store.(interface {
		UpdateBatchItemProgress(context.Context, string, string, string, ItemStage, string, int, *time.Time, time.Time) error
	})
	if !ok {
		return
	}
	if err := updater.UpdateBatchItemProgress(
		ctx, claim.Item.ID, claim.AttemptID, service.workerID,
		stage, message, retryCount, retryAfterAt, service.now().UTC(),
	); err != nil && !errors.Is(err, ErrBatchLeaseLost) {
		service.logger.Warn("tag_scraping.batch.progress_update_failed", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID, "stage": stage,
			"message": messageOf(err),
		})
	}
}

func (service *BatchService) processItem(workerContext context.Context, claim ClaimedBatchItem) {
	startedAt := time.Now()
	itemContext, cancel := context.WithCancel(workerContext)
	progress := newBatchProgress()
	var ownershipLost atomic.Bool
	var completionAttempted atomic.Bool
	itemContext = withBatchProgressReporter(itemContext, func(stage ItemStage, message string, retryCount int, retryAfterAt *time.Time) {
		service.updateBatchProgress(itemContext, claim, progress, stage, message, retryCount, retryAfterAt)
	})
	activeItem := service.setActiveWithCancellation(claim.Job.ID, cancel, func() {
		_, _, retryCount, _ := progress.snapshot()
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(workerContext), 2*time.Second)
		defer cleanupCancel()
		service.updateBatchProgress(cleanupContext, claim, progress, StageCancelling,
			"正在取消，等待当前操作和封面上传清理完成", retryCount, nil)
	})
	defer func() {
		cancel()
		service.clearActive(claim.Job.ID, activeItem)
		if recovered := recover(); recovered != nil {
			stack := string(debug.Stack())
			service.logger.Error("tag_scraping.batch.processor_panic", map[string]any{
				"jobId": claim.Job.ID, "itemId": claim.Item.ID, "attemptId": claim.AttemptID,
				"workerId": service.workerID, "stack": stack,
			})
			if !completionAttempted.Load() && !ownershipLost.Load() {
				service.completePanickedItem(workerContext, claim, stack)
			}
		}
	}()
	var cancelRequested atomic.Bool
	heartbeatDone := make(chan struct{})
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				ownershipLost.Store(true)
				cancel()
				service.logger.Error("tag_scraping.batch.heartbeat_panic", map[string]any{
					"jobId": claim.Job.ID, "itemId": claim.Item.ID, "attemptId": claim.AttemptID,
					"workerId": service.workerID, "stack": string(debug.Stack()),
				})
			}
			close(heartbeatDone)
		}()
		ticker := time.NewTicker(service.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-itemContext.Done():
				return
			case <-ticker.C:
				control, err := service.store.RenewBatchItemLease(
					itemContext, claim.Job.ID, claim.Item.ID, claim.AttemptID, service.workerID,
					service.lease,
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

	status, candidate, message := service.executeItem(itemContext, claim, &ownershipLost)
	if service.metrics != nil {
		service.metrics.ObservePipeline("scraping", time.Since(startedAt), status == ItemFailed)
	}
	cancel()
	<-heartbeatDone
	if ownershipLost.Load() {
		return
	}
	if workerContext.Err() != nil {
		releaseContext, releaseCancel := context.WithTimeout(context.WithoutCancel(workerContext), 10*time.Second)
		defer releaseCancel()
		if err := service.store.ReleaseBatchItem(
			releaseContext, claim.Item.ID, claim.AttemptID,
			service.workerID, service.now().UTC(),
		); err != nil {
			service.logger.Warn("tag_scraping.batch.release_failed", map[string]any{
				"jobId": claim.Job.ID, "itemId": claim.Item.ID,
				"attemptId": claim.AttemptID, "workerId": service.workerID,
				"message": messageOf(err),
			})
		}
		return
	}
	if cancelRequested.Load() && status != ItemSucceeded {
		status, candidate, message = ItemCancelled, nil, "The batch was cancelled"
	}
	cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(workerContext), 10*time.Second)
	defer cleanupCancel()
	if requested, err := service.store.BatchCancelRequested(cleanupContext, claim.Job.ID); err == nil && requested && status != ItemSucceeded {
		status, candidate, message = ItemCancelled, nil, "The batch was cancelled"
		cancelRequested.Store(true)
	}
	completionAttempted.Store(true)
	completion, err := service.store.CompleteBatchItem(
		cleanupContext, claim.Job.ID, claim.Item.ID, claim.AttemptID,
		service.workerID, status, candidate, message, service.now().UTC(),
	)
	if err != nil {
		service.logger.Warn("tag_scraping.batch.complete_failed", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID,
			"attemptId": claim.AttemptID, "workerId": service.workerID,
			"message": messageOf(err),
		})
		return
	}
	if !completion.ItemCompleted {
		service.logger.Warn("tag_scraping.batch.complete_rejected", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID,
			"attemptId": claim.AttemptID, "workerId": service.workerID,
		})
		return
	}
	if completion.JobFinished {
		service.deleteChannelGates(claim.Job.ID)
	}
	service.logger.Info("tag_scraping.batch.item_completed", map[string]any{
		"jobId": claim.Job.ID, "itemId": claim.Item.ID,
		"attemptId": claim.AttemptID, "workerId": service.workerID,
		"status": string(status),
	})
}

func (service *BatchService) completePanickedItem(parent context.Context, claim ClaimedBatchItem, stack string) {
	cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cleanupCancel()
	completion, err := service.store.CompleteBatchItem(
		cleanupContext, claim.Job.ID, claim.Item.ID, claim.AttemptID, service.workerID,
		ItemFailed, nil, "Worker processor panicked: "+stack, service.now().UTC(),
	)
	if err != nil {
		service.logger.Error("tag_scraping.batch.panic_completion_failed", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID, "attemptId": claim.AttemptID,
			"workerId": service.workerID, "message": messageOf(err),
		})
		return
	}
	if completion.JobFinished {
		service.deleteChannelGates(claim.Job.ID)
	}
}

func (service *BatchService) executeItem(
	ctx context.Context,
	claim ClaimedBatchItem,
	ownershipLost *atomic.Bool,
) (ItemStatus, *Candidate, string) {
	ctx = withBatchMutationFence(ctx, &BatchMutationFence{
		JobID: claim.Job.ID, ItemID: claim.Item.ID,
		AttemptID: claim.AttemptID, WorkerID: service.workerID,
	})
	if claim.Job.RequestedBy == nil {
		return ItemFailed, nil, "The administrator who created the job no longer exists"
	}
	if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost)
	}
	reportBatchProgress(ctx, StageSearchCandidates, "正在读取曲目并准备联合搜索", claim.Item.RetryCount, nil)
	metadata, err := service.processor.TrackMetadata(ctx, claim.Item.TrackID)
	if err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost)
	}
	if trackIsArchived(metadata.TrackStatus) {
		return ItemSkipped, nil, archivedBatchItemMessage
	}
	if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost)
	}
	if !matchesMissingFields(metadata.Effective, claim.Job.Options.MissingFields) {
		return ItemSkipped, nil, "The track does not match the configured missing-field conditions"
	}
	query := SearchInput{Title: &metadata.Effective.Title, Verbatim: claim.Job.Options.Verbatim}
	artistNames := make([]string, 0, len(metadata.Effective.Credits))
	for _, credit := range metadata.Effective.Credits {
		if credit.Role == "PRIMARY" {
			artistNames = append(artistNames, credit.Name)
		}
	}
	if len(artistNames) == 0 {
		for _, credit := range metadata.Effective.Credits {
			artistNames = append(artistNames, credit.Name)
		}
	}
	artist := ""
	if len(artistNames) > 0 {
		artist = artistNames[0]
	}
	query.Artist = &artist
	if metadata.Effective.Album != nil {
		query.Album = metadata.Effective.Album
	} else {
		empty := ""
		query.Album = &empty
	}
	var selected *Candidate
	sourceErrors := make([]string, 0)
	successfulSources := 0
	retryCount := claim.Item.RetryCount
	for _, source := range claim.Job.Options.Sources {
		if ctx.Err() != nil || ownershipLost.Load() {
			return ItemCancelled, nil, "The batch was cancelled"
		}
		if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
			return service.itemErrorStatus(ctx, err, ownershipLost)
		}
		query.Source = source
		reportBatchProgress(ctx, StageWaitingRateLimit, fmt.Sprintf("%s 音乐限流，等待平台许可", source), retryCount, nil)
		releaseChannel, acquireErr := service.acquireChannel(ctx, claim.Job.ID, source, claim.Job.Options.ChannelConcurrency[source])
		if acquireErr != nil {
			return service.itemErrorStatus(ctx, acquireErr, ownershipLost)
		}
		reportBatchProgress(ctx, StageSearchCandidates, fmt.Sprintf("正在搜索 %s 候选", source), retryCount, nil)
		matches, searchErr := func() ([]Candidate, error) {
			defer releaseChannel()
			return service.processor.Search(ctx, query)
		}()
		if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
			return service.itemErrorStatus(ctx, err, ownershipLost)
		}
		if searchErr != nil {
			if retryable, retryAfter := transientArtistArtworkBatchError(searchErr); retryable {
				if retryAfter <= 0 {
					retryAfter = time.Second
				}
				retryCount++
				recoveryAt := service.now().UTC().Add(retryAfter)
				reportBatchProgress(ctx, StageRetryWaiting, fmt.Sprintf("%s 请求失败，%s 后重试或切换来源", source, retryAfter.Round(time.Second)), retryCount, &recoveryAt)
				if sleepErr := sleepContext(ctx, retryAfter); sleepErr != nil {
					return service.itemErrorStatus(ctx, sleepErr, ownershipLost)
				}
			}
			sourceErrors = append(sourceErrors, string(source)+": "+messageOf(searchErr))
			continue
		}
		successfulSources++
		for index := range matches {
			if reliableTagMatch(query, matches[index], claim.Job.Options.MatchMode) {
				match := matches[index]
				selected = &match
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		if successfulSources == 0 && len(sourceErrors) == len(claim.Job.Options.Sources) {
			return ItemFailed, nil, "All scraping sources failed: " + strings.Join(sourceErrors, "; ")
		}
		message := "No reliable match was found"
		if len(sourceErrors) > 0 {
			message += "; some sources failed: " + strings.Join(sourceErrors, "; ")
		}
		return ItemSkipped, nil, message
	}
	if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost)
	}
	reportBatchProgress(ctx, StageWaitingRateLimit, fmt.Sprintf("%s 音乐限流，等待平台许可", selected.Source), retryCount, nil)
	releaseChannel, acquireErr := service.acquireChannel(
		ctx, claim.Job.ID, selected.Source, claim.Job.Options.ChannelConcurrency[selected.Source],
	)
	if acquireErr != nil {
		return service.itemErrorStatus(ctx, acquireErr, ownershipLost)
	}
	reportBatchProgress(ctx, StageUpdateMetadata, "正在更新元数据", retryCount, nil)
	applyContext := withBatchChannelLease(ctx, selected.Source, func(channelContext context.Context, source Source) (func(), error) {
		return service.acquireChannel(channelContext, claim.Job.ID, source, claim.Job.Options.ChannelConcurrency[source])
	})
	result, err := func() (ApplyResult, error) {
		defer releaseChannel()
		return service.processor.Apply(applyContext, *claim.Job.RequestedBy, uuid.NewString(), claim.Item.TrackID, ApplyInput{
			ExpectedVersion: claim.Item.ExpectedVersion,
			Candidate:       *selected,
			Verbatim:        claim.Job.Options.Verbatim,
			Fields:          claim.Job.Options.Fields,
			WriteBack:       claim.Job.Options.WriteBack,
			Reason:          claim.Job.Options.Reason,
			cancellationCheck: func(checkContext context.Context) error {
				return service.ensureBatchActive(checkContext, claim.Job.ID)
			},
		})
	}()
	if err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost)
	}
	message := strings.Join(result.Warnings, "; ")
	if message == "" {
		message = "Scraping completed"
	}
	if applyResultHasDependencyFailure(result) {
		return ItemFailed, nil, message
	}
	return ItemSucceeded, selected, message
}

func applyResultHasDependencyFailure(result ApplyResult) bool {
	for _, warning := range result.Warnings {
		if strings.HasPrefix(warning, "Lyrics retrieval failed:") || strings.HasPrefix(warning, "Cover application failed:") {
			return true
		}
	}
	return false
}

func (service *BatchService) itemErrorStatus(
	ctx context.Context,
	err error,
	ownershipLost *atomic.Bool,
) (ItemStatus, *Candidate, string) {
	if errors.Is(err, ErrBatchLeaseLost) {
		if ownershipLost != nil {
			ownershipLost.Store(true)
		}
		return ItemSkipped, nil, "The batch item lease was lost"
	}
	if ctx.Err() != nil || errors.Is(err, errBatchCancellationRequested) {
		return ItemCancelled, nil, "The batch was cancelled"
	}
	if isArchivedTrackError(err) {
		return ItemSkipped, nil, archivedBatchItemMessage
	}
	return ItemFailed, nil, messageOf(err)
}

func (service *BatchService) ensureBatchActive(ctx context.Context, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	requested, err := service.store.BatchCancelRequested(ctx, jobID)
	if err != nil {
		return err
	}
	if requested {
		return errBatchCancellationRequested
	}
	return nil
}

func (service *BatchService) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *BatchService) setActive(jobID string, cancel context.CancelFunc) *activeBatchItem {
	return service.setActiveWithCancellation(jobID, cancel, nil)
}

func (service *BatchService) setActiveWithCancellation(jobID string, cancel context.CancelFunc, cancelling func()) *activeBatchItem {
	item := &activeBatchItem{cancel: cancel, cancelling: cancelling}
	service.activeMu.Lock()
	service.active[jobID] = append(service.active[jobID], item)
	service.activeMu.Unlock()
	return item
}

func (service *BatchService) clearActive(jobID string, item *activeBatchItem) {
	service.activeMu.Lock()
	defer service.activeMu.Unlock()
	active := service.active[jobID]
	for index, current := range active {
		if current != item {
			continue
		}
		active = append(active[:index], active[index+1:]...)
		if len(active) == 0 {
			delete(service.active, jobID)
		} else {
			service.active[jobID] = active
		}
		return
	}
}

func (service *BatchService) cancelJob(jobID string) {
	service.activeMu.Lock()
	active := append([]*activeBatchItem(nil), service.active[jobID]...)
	service.activeMu.Unlock()
	for _, item := range active {
		item.cancel()
		if item.cancelling != nil {
			go item.cancelling()
		}
	}
}

func (service *BatchService) cancelActive() {
	service.activeMu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(service.active))
	for _, active := range service.active {
		for _, item := range active {
			cancellations = append(cancellations, item.cancel)
		}
	}
	service.activeMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func validateCreateBatch(input CreateBatchInput) error {
	if len(input.Items) < 1 || len(input.Items) > 200 {
		return apperror.Validation("A tag scraping batch must contain 1 to 200 tracks")
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
	for source, limit := range input.Options.ChannelConcurrency {
		maximum, known := maximumBatchChannelConcurrency[source]
		if !known || limit < 1 || limit > maximum {
			return apperror.Validation("Batch channel concurrency is invalid")
		}
	}
	for _, source := range input.Options.Sources {
		limit := input.Options.ChannelConcurrency[source]
		maximum := maximumBatchChannelConcurrency[source]
		if limit < 1 || maximum < 1 || limit > maximum {
			return apperror.Validation("Batch channel concurrency is invalid")
		}
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

func normalizeBatchOptions(options BatchOptions) BatchOptions {
	options.ChannelConcurrency = cloneChannelConcurrency(options.ChannelConcurrency)
	if options.ChannelConcurrency == nil {
		options.ChannelConcurrency = make(map[Source]int, len(options.Sources))
	}
	for _, source := range options.Sources {
		if _, exists := options.ChannelConcurrency[source]; !exists {
			options.ChannelConcurrency[source] = defaultBatchChannelConcurrency[source]
		}
	}
	return options
}

func cloneChannelConcurrency(values map[Source]int) map[Source]int {
	if values == nil {
		return nil
	}
	result := make(map[Source]int, len(values))
	for source, value := range values {
		result[source] = value
	}
	return result
}

type batchChannelGate struct {
	semaphore chan struct{}
	mu        sync.Mutex
	waiting   int
}

func newBatchChannelGate(limit int) *batchChannelGate {
	return &batchChannelGate{semaphore: make(chan struct{}, limit)}
}

func (gate *batchChannelGate) acquire(ctx context.Context) error {
	select {
	case gate.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	gate.mu.Lock()
	gate.waiting++
	gate.mu.Unlock()
	defer func() {
		gate.mu.Lock()
		gate.waiting--
		gate.mu.Unlock()
	}()
	select {
	case gate.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (gate *batchChannelGate) release() { <-gate.semaphore }

func gateWaiting(gate *batchChannelGate) int {
	if gate == nil {
		return 0
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.waiting
}

func (service *BatchService) acquireChannel(
	ctx context.Context,
	jobID string,
	source Source,
	target int,
) (func(), error) {
	if target < 1 {
		target = defaultBatchChannelConcurrency[source]
	}
	if target < 1 {
		target = 1
	}
	maximum := maximumBatchChannelConcurrency[source]
	if maximum < 1 {
		maximum = 64
	}
	if target > maximum {
		target = maximum
	}
	service.channelMu.Lock()
	perJob := service.channelGates[jobID]
	if perJob == nil {
		perJob = make(map[Source]*batchChannelGate)
		service.channelGates[jobID] = perJob
	}
	batchGate := perJob[source]
	if batchGate == nil {
		batchGate = newBatchChannelGate(target)
		perJob[source] = batchGate
	}
	globalGate := service.channelGlobal[source]
	if globalGate == nil {
		globalGate = newBatchChannelGate(maximum)
		service.channelGlobal[source] = globalGate
	}
	service.channelMu.Unlock()
	if err := batchGate.acquire(ctx); err != nil {
		return nil, err
	}
	if err := globalGate.acquire(ctx); err != nil {
		batchGate.release()
		return nil, err
	}
	service.channelMu.Lock()
	service.channelActive[source]++
	service.channelMu.Unlock()
	return func() {
		globalGate.release()
		batchGate.release()
		service.channelMu.Lock()
		if service.channelActive[source] > 0 {
			service.channelActive[source]--
		}
		service.channelMu.Unlock()
	}, nil
}

func (service *BatchService) deleteChannelGates(jobID string) {
	service.channelMu.Lock()
	delete(service.channelGates, jobID)
	service.channelMu.Unlock()
}

func (service *BatchService) channelStatus(jobID string, options BatchOptions) map[Source]BatchChannelStatus {
	options = normalizeBatchOptions(options)
	healthProvider, _ := service.processor.(interface {
		ChannelHealth(Source) ChannelHealth
	})
	service.channelMu.Lock()
	defer service.channelMu.Unlock()
	result := make(map[Source]BatchChannelStatus, len(options.Sources))
	perJob := service.channelGates[jobID]
	for _, source := range options.Sources {
		target := options.ChannelConcurrency[source]
		actual := 0
		if gate := perJob[source]; gate != nil {
			actual = len(gate.semaphore)
		}
		state := "READY"
		if actual > 0 {
			state = "ACTIVE"
		}
		status := BatchChannelStatus{Target: target, Actual: actual, State: state}
		status.Limit = maximumBatchChannelConcurrency[source]
		status.Waiting = gateWaiting(perJob[source])
		if healthProvider != nil {
			health := healthProvider.ChannelHealth(source)
			if health.State != "" && health.State != "READY" {
				status.State = health.State
			}
			status.RetryAfterSeconds = health.RetryAfterSeconds
			status.ConsecutiveFailures = health.ConsecutiveFailures
			status.ErrorRate = health.ErrorRate
		}
		result[source] = status
	}
	return result
}

func (service *BatchService) concurrencyStatus(job BatchJobRecord) BatchConcurrencyStatus {
	service.activeMu.Lock()
	actual := len(service.active[job.ID])
	service.activeMu.Unlock()
	waiting := max(0, job.Total-job.Processed-actual)
	result := BatchConcurrencyStatus{
		TotalActual: actual, TotalWaiting: waiting, TotalLimit: service.workers,
	}
	if provider, ok := service.processor.(interface{ ArtworkHealth() ArtworkHealth }); ok {
		health := provider.ArtworkHealth()
		result.CoverActual = health.Actual
		result.CoverWaiting = health.Waiting
		result.CoverLimit = health.Limit
	}
	return result
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
	skipped := 0
	cancelled := job.Cancelled
	if partial {
		skipped = max(0, job.Processed-job.Succeeded-job.Failed-job.Cancelled)
	} else {
		cancelled = 0
		for _, item := range items {
			switch item.Status {
			case ItemSkipped:
				skipped++
			case ItemCancelled:
				cancelled++
			}
		}
	}
	result := BatchJobDTO{
		ID: job.ID, RequestedBy: job.RequestedBy, Options: job.Options, Status: job.Status,
		Total: job.Total, Processed: job.Processed, Succeeded: job.Succeeded, Failed: job.Failed,
		Skipped: skipped, Cancelled: cancelled,
		CancelRequested: job.CancelRequested, StartedAt: optionalTimestamp(job.StartedAt),
		CompletedAt: optionalTimestamp(job.CompletedAt), CreatedAt: formatTimestamp(job.CreatedAt),
		UpdatedAt: formatTimestamp(job.UpdatedAt), Unsuccessful: max(0, job.Processed-job.Succeeded),
		PartialItems: partial, Items: make([]BatchItemDTO, 0, len(items)),
	}
	for _, item := range items {
		result.Items = append(result.Items, BatchItemDTO{
			ID: item.ID, JobID: item.JobID, TrackID: item.TrackID, ExpectedVersion: item.ExpectedVersion,
			Position: item.Position, Status: item.Status, Stage: item.Stage,
			HeartbeatAt: optionalTimestamp(item.HeartbeatAt), RetryCount: item.RetryCount,
			RetryAfterAt: optionalTimestamp(item.RetryAfterAt), RecoveryCount: item.RecoveryCount,
			Candidate: item.Candidate, Source: item.Source,
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
