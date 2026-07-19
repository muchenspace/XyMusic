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
	defaultBatchWorkPoll  = 250 * time.Millisecond
)

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
	wake        chan struct{}

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	cancel      context.CancelFunc
	done        chan struct{}

	activeMu sync.Mutex
	active   map[string]context.CancelFunc
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
	return &BatchService{
		store: dependencies.Store, processor: dependencies.Processor, logger: dependencies.Logger,
		workerID: dependencies.WorkerID,
		now:      dependencies.Clock, lease: dependencies.Lease, heartbeat: dependencies.Heartbeat,
		idlePoll: dependencies.IdlePoll, workingPoll: dependencies.WorkingPoll,
		wake: make(chan struct{}, 1), active: make(map[string]context.CancelFunc),
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

func (service *BatchService) run(ctx context.Context, done chan struct{}) {
	defer close(done)
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

func (service *BatchService) processItem(workerContext context.Context, claim ClaimedBatchItem) {
	itemContext, cancel := context.WithCancel(workerContext)
	service.setActive(claim.Job.ID, cancel)
	defer func() {
		cancel()
		service.clearActive(claim.Job.ID, cancel)
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

	status, candidate, message := service.executeItem(itemContext, claim, &ownershipLost)
	cancel()
	<-heartbeatDone
	if ownershipLost.Load() {
		return
	}
	if workerContext.Err() != nil {
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
		return
	}
	if cancelRequested.Load() {
		status, candidate, message = ItemSkipped, nil, "The batch was cancelled"
	}
	completed, err := service.store.CompleteBatchItem(
		context.WithoutCancel(itemContext), claim.Job.ID, claim.Item.ID, claim.AttemptID,
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
	if !completed {
		service.logger.Warn("tag_scraping.batch.complete_rejected", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID,
			"attemptId": claim.AttemptID, "workerId": service.workerID,
		})
		return
	}
	service.logger.Info("tag_scraping.batch.item_completed", map[string]any{
		"jobId": claim.Job.ID, "itemId": claim.Item.ID,
		"attemptId": claim.AttemptID, "workerId": service.workerID,
		"status": string(status),
	})
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
	query := SearchInput{Title: &metadata.Effective.Title}
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
	var selected *Candidate
	sourceErrors := make([]string, 0)
	successfulSources := 0
	for _, source := range claim.Job.Options.Sources {
		if ctx.Err() != nil || ownershipLost.Load() {
			return ItemSkipped, nil, "The batch was cancelled"
		}
		if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
			return service.itemErrorStatus(ctx, err, ownershipLost)
		}
		query.Source = source
		matches, searchErr := service.processor.Search(ctx, query)
		if err := service.ensureBatchActive(ctx, claim.Job.ID); err != nil {
			return service.itemErrorStatus(ctx, err, ownershipLost)
		}
		if searchErr != nil {
			sourceErrors = append(sourceErrors, string(source)+": "+messageOf(searchErr))
			continue
		}
		successfulSources++
		for index := range matches {
			if reliableTagMatch(matches[index], claim.Job.Options.MatchMode) {
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
	result, err := service.processor.Apply(ctx, *claim.Job.RequestedBy, uuid.NewString(), claim.Item.TrackID, ApplyInput{
		ExpectedVersion: claim.Item.ExpectedVersion,
		Candidate:       *selected,
		Fields:          claim.Job.Options.Fields,
		WriteBack:       claim.Job.Options.WriteBack,
		Reason:          claim.Job.Options.Reason,
		cancellationCheck: func(checkContext context.Context) error {
			return service.ensureBatchActive(checkContext, claim.Job.ID)
		},
	})
	if err != nil {
		return service.itemErrorStatus(ctx, err, ownershipLost)
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

func (service *BatchService) setActive(jobID string, cancel context.CancelFunc) {
	service.activeMu.Lock()
	service.active[jobID] = cancel
	service.activeMu.Unlock()
}

func (service *BatchService) clearActive(jobID string, _ context.CancelFunc) {
	service.activeMu.Lock()
	delete(service.active, jobID)
	service.activeMu.Unlock()
}

func (service *BatchService) cancelJob(jobID string) {
	service.activeMu.Lock()
	cancel := service.active[jobID]
	service.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (service *BatchService) cancelActive() {
	service.activeMu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(service.active))
	for _, cancel := range service.active {
		cancellations = append(cancellations, cancel)
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
			Position: item.Position, Status: item.Status, Candidate: item.Candidate, Source: item.Source,
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
