package admintagscraping

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/shared/apperror"
)

const (
	defaultArtistArtworkBatchMaxAttempts = 3
	defaultArtistArtworkRetryBase        = 5 * time.Second
	defaultArtistArtworkRetryMaximum     = 5 * time.Minute
)

type ArtistArtworkBatchServiceDependencies struct {
	Store       ArtistArtworkBatchStore
	Processor   ArtistArtworkBatchProcessor
	Logger      Logger
	WorkerID    string
	Clock       func() time.Time
	Lease       time.Duration
	Heartbeat   time.Duration
	IdlePoll    time.Duration
	WorkingPoll time.Duration
	MaxAttempts int
	RetryBase   time.Duration
	RetryMax    time.Duration
}

type ArtistArtworkBatchService struct {
	store       ArtistArtworkBatchStore
	processor   ArtistArtworkBatchProcessor
	logger      Logger
	workerID    string
	now         func() time.Time
	lease       time.Duration
	heartbeat   time.Duration
	idlePoll    time.Duration
	workingPoll time.Duration
	maxAttempts int
	retryBase   time.Duration
	retryMax    time.Duration
	wake        chan struct{}

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	cancel      context.CancelFunc
	done        chan struct{}

	activeMu sync.Mutex
	active   map[string]context.CancelFunc
}

func NewArtistArtworkBatchService(
	dependencies ArtistArtworkBatchServiceDependencies,
) (*ArtistArtworkBatchService, error) {
	if dependencies.Store == nil {
		return nil, errors.New("artist artwork batch store is required")
	}
	if dependencies.Processor == nil {
		return nil, errors.New("artist artwork batch processor is required")
	}
	if dependencies.Logger == nil {
		dependencies.Logger = NoopLogger{}
	}
	if dependencies.WorkerID == "" {
		dependencies.WorkerID = "artist-artwork-batch-" + uuid.NewString()
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
		return nil, errors.New("artist artwork batch heartbeat must be shorter than the lease")
	}
	if dependencies.IdlePoll <= 0 {
		dependencies.IdlePoll = defaultBatchIdlePoll
	}
	if dependencies.WorkingPoll <= 0 {
		dependencies.WorkingPoll = defaultBatchWorkPoll
	}
	if dependencies.MaxAttempts <= 0 {
		dependencies.MaxAttempts = defaultArtistArtworkBatchMaxAttempts
	}
	if dependencies.MaxAttempts > 10 {
		return nil, errors.New("artist artwork batch max attempts must not exceed 10")
	}
	if dependencies.RetryBase <= 0 {
		dependencies.RetryBase = defaultArtistArtworkRetryBase
	}
	if dependencies.RetryMax <= 0 {
		dependencies.RetryMax = defaultArtistArtworkRetryMaximum
	}
	if dependencies.RetryMax < dependencies.RetryBase {
		return nil, errors.New("artist artwork retry maximum must not be shorter than its base delay")
	}
	return &ArtistArtworkBatchService{
		store: dependencies.Store, processor: dependencies.Processor, logger: dependencies.Logger,
		workerID: dependencies.WorkerID, now: dependencies.Clock,
		lease: dependencies.Lease, heartbeat: dependencies.Heartbeat,
		idlePoll: dependencies.IdlePoll, workingPoll: dependencies.WorkingPoll,
		maxAttempts: dependencies.MaxAttempts, retryBase: dependencies.RetryBase, retryMax: dependencies.RetryMax,
		wake: make(chan struct{}, 1), active: make(map[string]context.CancelFunc),
	}, nil
}

func (service *ArtistArtworkBatchService) Start(ctx context.Context) error {
	service.lifecycleMu.Lock()
	defer service.lifecycleMu.Unlock()
	if service.closed {
		return errors.New("artist artwork batch service is closed")
	}
	if service.started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := service.store.RecoverExpiredArtistArtworkBatchItems(ctx, service.now().UTC()); err != nil {
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

func (service *ArtistArtworkBatchService) Close(ctx context.Context) error {
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

func (service *ArtistArtworkBatchService) Create(
	ctx context.Context,
	actorID string,
	input CreateArtistArtworkBatchInput,
) (ArtistArtworkBatchCreateResult, error) {
	if err := validateCreateArtistArtworkBatch(input); err != nil {
		return ArtistArtworkBatchCreateResult{}, err
	}
	input.Options.Reason = normalizeText(input.Options.Reason)
	jobID, selected, excluded, err := service.store.CreateArtistArtworkBatch(
		ctx, actorID, input, service.maxAttempts,
	)
	if err != nil {
		return ArtistArtworkBatchCreateResult{}, err
	}
	result := ArtistArtworkBatchCreateResult{Selected: selected, ConditionExcluded: excluded}
	if jobID == "" {
		return result, nil
	}
	job, err := service.Job(ctx, jobID, nil)
	if err != nil {
		return ArtistArtworkBatchCreateResult{}, err
	}
	result.Job = &job
	service.signal()
	return result, nil
}

func (service *ArtistArtworkBatchService) Job(
	ctx context.Context,
	jobID string,
	updatedAfter *time.Time,
) (ArtistArtworkBatchJobDTO, error) {
	job, items, err := service.store.ArtistArtworkBatch(ctx, jobID, updatedAfter)
	if err != nil {
		return ArtistArtworkBatchJobDTO{}, err
	}
	return presentArtistArtworkBatch(job, items, updatedAfter != nil), nil
}

func (service *ArtistArtworkBatchService) Cancel(
	ctx context.Context,
	jobID string,
) (ArtistArtworkBatchJobDTO, error) {
	if err := service.store.RequestArtistArtworkBatchCancel(ctx, jobID); err != nil {
		return ArtistArtworkBatchJobDTO{}, err
	}
	service.cancelJob(jobID)
	service.signal()
	return service.Job(ctx, jobID, nil)
}

func (service *ArtistArtworkBatchService) Retry(
	ctx context.Context,
	jobID string,
) (ArtistArtworkBatchJobDTO, error) {
	if err := service.store.RetryArtistArtworkBatch(ctx, jobID); err != nil {
		return ArtistArtworkBatchJobDTO{}, err
	}
	service.signal()
	return service.Job(ctx, jobID, nil)
}

func (service *ArtistArtworkBatchService) run(ctx context.Context, done chan struct{}) {
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
			service.logger.Error("artist_artwork.batch.poll_failed", map[string]any{
				"workerId": service.workerID, "message": messageOf(err),
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

func (service *ArtistArtworkBatchService) processNext(ctx context.Context) (bool, error) {
	claim, err := service.store.ClaimArtistArtworkBatchItem(
		ctx, service.workerID, service.now().UTC(), service.lease,
	)
	if err != nil {
		return false, err
	}
	if claim.FinishJobID != "" {
		finished, err := service.store.FinishArtistArtworkBatch(
			ctx, claim.FinishJobID, service.now().UTC(),
		)
		return finished, err
	}
	if claim.Item == nil {
		return false, nil
	}
	service.processItem(ctx, *claim.Item)
	return true, nil
}

type artistArtworkBatchExecution struct {
	status     ItemStatus
	candidate  *ArtistCandidate
	message    string
	retry      bool
	retryAfter time.Duration
}

func (service *ArtistArtworkBatchService) processItem(
	workerContext context.Context,
	claim ClaimedArtistArtworkBatchItem,
) {
	itemContext, cancel := context.WithCancel(workerContext)
	service.setActive(claim.Job.ID, cancel)
	defer func() {
		cancel()
		service.clearActive(claim.Job.ID)
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
				control, err := service.store.RenewArtistArtworkBatchItemLease(
					itemContext, claim.Job.ID, claim.Item.ID, claim.AttemptID, service.workerID,
					service.now().UTC().Add(service.lease),
				)
				if itemContext.Err() != nil {
					return
				}
				if err != nil {
					service.logger.Warn("artist_artwork.batch.renew_failed", map[string]any{
						"jobId": claim.Job.ID, "itemId": claim.Item.ID,
						"attemptId": claim.AttemptID, "workerId": service.workerID,
						"message": messageOf(err),
					})
					ownershipLost.Store(true)
					cancel()
					return
				}
				if !control.Owned {
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

	execution := service.executeItem(itemContext, claim, &ownershipLost)
	cancel()
	<-heartbeatDone
	if ownershipLost.Load() && execution.status != ItemSucceeded {
		return
	}
	if workerContext.Err() != nil && execution.status != ItemSucceeded {
		if err := service.store.ReleaseArtistArtworkBatchItem(
			context.WithoutCancel(workerContext), claim.Item.ID, claim.AttemptID,
			service.workerID, service.now().UTC(),
		); err != nil && !errors.Is(err, ErrArtistArtworkBatchLeaseLost) {
			service.logger.Warn("artist_artwork.batch.release_failed", map[string]any{
				"jobId": claim.Job.ID, "itemId": claim.Item.ID, "message": messageOf(err),
			})
		}
		return
	}
	if cancelRequested.Load() && execution.status != ItemSucceeded {
		execution = artistArtworkBatchExecution{status: ItemSkipped, message: "The batch was cancelled"}
	}

	if execution.retry && claim.Item.Attempts < claim.Item.MaxAttempts {
		delay := service.retryDelay(claim.Item.Attempts, execution.retryAfter)
		control, err := service.store.RetryArtistArtworkBatchItem(
			context.WithoutCancel(itemContext), claim.Job.ID, claim.Item.ID, claim.AttemptID,
			service.workerID, execution.candidate, execution.message,
			service.now().UTC().Add(delay), service.now().UTC(),
		)
		if err != nil {
			if !errors.Is(err, ErrArtistArtworkBatchLeaseLost) {
				service.logger.Warn("artist_artwork.batch.retry_failed", map[string]any{
					"jobId": claim.Job.ID, "itemId": claim.Item.ID, "message": messageOf(err),
				})
			}
			return
		}
		if control.CancelRequested {
			execution = artistArtworkBatchExecution{status: ItemSkipped, message: "The batch was cancelled"}
		} else if control.Owned {
			service.logger.Info("artist_artwork.batch.item_requeued", map[string]any{
				"jobId": claim.Job.ID, "itemId": claim.Item.ID,
				"attempts": claim.Item.Attempts, "retryAfterMs": delay.Milliseconds(),
			})
			service.signal()
			return
		} else {
			return
		}
	}
	if execution.retry {
		execution.status = ItemFailed
		execution.message = "All retry attempts failed: " + execution.message
	}
	completed, err := service.store.CompleteArtistArtworkBatchItem(
		context.WithoutCancel(itemContext), claim.Job.ID, claim.Item.ID, claim.AttemptID,
		service.workerID, execution.status, execution.candidate, execution.message, service.now().UTC(),
	)
	if err != nil {
		if !errors.Is(err, ErrArtistArtworkBatchLeaseLost) {
			service.logger.Warn("artist_artwork.batch.complete_failed", map[string]any{
				"jobId": claim.Job.ID, "itemId": claim.Item.ID, "message": messageOf(err),
			})
		}
		return
	}
	if completed {
		service.logger.Info("artist_artwork.batch.item_completed", map[string]any{
			"jobId": claim.Job.ID, "itemId": claim.Item.ID, "status": string(execution.status),
		})
	}
}

func (service *ArtistArtworkBatchService) executeItem(
	ctx context.Context,
	claim ClaimedArtistArtworkBatchItem,
	ownershipLost *atomic.Bool,
) artistArtworkBatchExecution {
	ctx = withArtworkMutationFence(ctx, &ArtistArtworkBatchMutationFence{
		JobID: claim.Job.ID, ItemID: claim.Item.ID,
		AttemptID: claim.AttemptID, WorkerID: service.workerID,
	})
	if claim.Job.RequestedBy == nil {
		return artistArtworkBatchExecution{status: ItemFailed, message: "The administrator who created the job no longer exists"}
	}
	if err := service.ensureActive(ctx, claim.Job.ID); err != nil {
		return service.executionError(ctx, err, ownershipLost)
	}
	if claim.Target.Version != claim.Item.ExpectedVersion {
		return artistArtworkBatchExecution{status: ItemSkipped, message: "Artist version changed before artwork scraping"}
	}
	if !claim.Target.PerformerRole {
		return artistArtworkBatchExecution{status: ItemSkipped, message: "Artist no longer has a primary or featured performer role"}
	}
	if !artistArtworkScrapeNameEligible(claim.Target) {
		return artistArtworkBatchExecution{status: ItemSkipped, message: "Placeholder artists cannot receive scraped artwork"}
	}
	if !claim.Job.Options.Overwrite && claim.Target.HasArtwork {
		return artistArtworkBatchExecution{status: ItemSkipped, message: "Artist artwork already exists"}
	}

	var selected *ArtistCandidate
	sourceErrors := make([]string, 0)
	successfulSources := 0
	transientFailure := false
	retryAfter := time.Duration(0)
	for _, source := range claim.Job.Options.Sources {
		if err := service.ensureActive(ctx, claim.Job.ID); err != nil {
			return service.executionError(ctx, err, ownershipLost)
		}
		matches, searchErr := service.processor.SearchArtists(ctx, ArtistSearchInput{
			Source: source, Query: claim.Target.Name,
		})
		if searchErr != nil {
			sourceErrors = append(sourceErrors, string(source)+": "+messageOf(searchErr))
			if retry, hint := transientArtistArtworkBatchError(searchErr); retry {
				transientFailure = true
				if hint > retryAfter {
					retryAfter = hint
				}
			}
			continue
		}
		successfulSources++
		for index := range matches {
			if reliableArtistArtworkMatch(claim.Target.Name, matches[index]) {
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
		if transientFailure {
			return artistArtworkBatchExecution{
				status: ItemFailed, retry: true, retryAfter: retryAfter,
				message: "Artist search was incomplete because a source failed: " + strings.Join(sourceErrors, "; "),
			}
		}
		if successfulSources == 0 && len(sourceErrors) == len(claim.Job.Options.Sources) {
			return artistArtworkBatchExecution{
				status: ItemFailed, message: "Artist search failed: " + strings.Join(sourceErrors, "; "),
			}
		}
		message := "No trustworthy exact artist match with artwork was found"
		if len(sourceErrors) > 0 {
			message += "; some sources failed: " + strings.Join(sourceErrors, "; ")
		}
		return artistArtworkBatchExecution{status: ItemSkipped, message: message}
	}
	if err := service.ensureActive(ctx, claim.Job.ID); err != nil {
		return service.executionError(ctx, err, ownershipLost)
	}
	result, err := service.processor.ApplyArtistArtwork(
		ctx, *claim.Job.RequestedBy, uuid.NewString(), claim.Item.ArtistID,
		ArtistArtworkApplyInput{
			ExpectedVersion: claim.Item.ExpectedVersion,
			Candidate:       *selected, Overwrite: claim.Job.Options.Overwrite,
			Reason: claim.Job.Options.Reason,
		},
	)
	if err != nil {
		execution := service.executionError(ctx, err, ownershipLost)
		execution.candidate = selected
		return execution
	}
	if !result.Applied {
		return artistArtworkBatchExecution{
			status: ItemSkipped, candidate: selected,
			message: "Artist artwork no longer meets the configured update conditions",
		}
	}
	return artistArtworkBatchExecution{
		status: ItemSucceeded, candidate: selected, message: "Artist artwork scraping completed",
	}
}

func (service *ArtistArtworkBatchService) executionError(
	ctx context.Context,
	err error,
	ownershipLost *atomic.Bool,
) artistArtworkBatchExecution {
	if errors.Is(err, ErrArtistArtworkBatchLeaseLost) {
		if ownershipLost != nil {
			ownershipLost.Store(true)
		}
		return artistArtworkBatchExecution{status: ItemSkipped, message: "The artist artwork batch item lease was lost"}
	}
	if ctx.Err() != nil || errors.Is(err, errArtistArtworkBatchCancellationRequested) {
		return artistArtworkBatchExecution{status: ItemSkipped, message: "The batch was cancelled"}
	}
	if artistArtworkBatchConflict(err) {
		return artistArtworkBatchExecution{status: ItemSkipped, message: messageOf(err)}
	}
	if retry, hint := transientArtistArtworkBatchError(err); retry {
		return artistArtworkBatchExecution{
			status: ItemFailed, message: messageOf(err), retry: true, retryAfter: hint,
		}
	}
	return artistArtworkBatchExecution{status: ItemFailed, message: messageOf(err)}
}

func (service *ArtistArtworkBatchService) ensureActive(ctx context.Context, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	requested, err := service.store.ArtistArtworkBatchCancelRequested(ctx, jobID)
	if err != nil {
		return err
	}
	if requested {
		return errArtistArtworkBatchCancellationRequested
	}
	return nil
}

func (service *ArtistArtworkBatchService) retryDelay(attempt int, hint time.Duration) time.Duration {
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

func transientArtistArtworkBatchError(err error) (bool, time.Duration) {
	if err == nil || errors.Is(err, context.Canceled) {
		return false, 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true, 0
	}
	applicationError, ok := apperror.As(err)
	if !ok {
		return true, 0
	}
	if applicationError.Code != apperror.CodeDependencyUnavailable && applicationError.Code != apperror.CodeRateLimited {
		return false, 0
	}
	return true, retryAfterFromMetadata(applicationError.Metadata)
}

func retryAfterFromMetadata(metadata map[string]any) time.Duration {
	if metadata == nil {
		return 0
	}
	var seconds int64
	switch value := metadata["retryAfterSeconds"].(type) {
	case int:
		seconds = int64(value)
	case int32:
		seconds = int64(value)
	case int64:
		seconds = value
	case float64:
		seconds = int64(value)
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func artistArtworkBatchConflict(err error) bool {
	applicationError, ok := apperror.As(err)
	if !ok {
		return false
	}
	switch applicationError.Code {
	case apperror.CodeVersionConflict, apperror.CodeResourceConflict,
		apperror.CodeInvalidStateTransition, apperror.CodeResourceNotFound:
		return true
	default:
		return false
	}
}

func reliableArtistArtworkMatch(name string, candidate ArtistCandidate) bool {
	if strings.TrimSpace(candidate.ID) == "" || strings.TrimSpace(candidate.ImageURL) == "" ||
		!isArtistSearchableSource(candidate.Source) {
		return false
	}
	expected := normalizeForTagMatch(name)
	if expected == "" {
		return false
	}
	if normalizeForTagMatch(candidate.Name) == expected {
		return true
	}
	for _, alias := range candidate.Aliases {
		if normalizeForTagMatch(alias) == expected {
			return true
		}
	}
	return false
}

func validateCreateArtistArtworkBatch(input CreateArtistArtworkBatchInput) error {
	if len(input.Items) < 1 || len(input.Items) > 200 {
		return apperror.Validation("An artist artwork batch must contain 1 to 200 artists")
	}
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if _, err := uuid.Parse(item.ArtistID); err != nil || item.ExpectedVersion < 1 {
			return apperror.Validation("An artist artwork batch item is invalid")
		}
		if _, duplicate := seen[item.ArtistID]; duplicate {
			return apperror.Validation("Artist artwork batch IDs must be unique")
		}
		seen[item.ArtistID] = struct{}{}
	}
	if len(input.Options.Sources) < 1 || len(input.Options.Sources) > len(searchableArtistSources) ||
		!uniqueSources(input.Options.Sources) {
		return apperror.Validation("Artist artwork batch sources are invalid")
	}
	if input.Options.Overwrite {
		return apperror.Validation("Artist artwork batches cannot overwrite existing artwork")
	}
	for _, source := range input.Options.Sources {
		if !isArtistSearchableSource(source) {
			return apperror.Validation("Artist artwork batch sources are invalid")
		}
	}
	reason := normalizeText(input.Options.Reason)
	if javascriptLength(reason) < 2 || javascriptLength(reason) > 500 {
		return apperror.Validation("Artist artwork batch reason is invalid")
	}
	return nil
}

func presentArtistArtworkBatch(
	job ArtistArtworkBatchJobRecord,
	items []ArtistArtworkBatchItemRecord,
	partial bool,
) ArtistArtworkBatchJobDTO {
	result := ArtistArtworkBatchJobDTO{
		ID: job.ID, RequestedBy: job.RequestedBy, Options: job.Options, Status: job.Status,
		Total: job.Total, Processed: job.Processed, Succeeded: job.Succeeded, Failed: job.Failed,
		Skipped: max(0, job.Processed-job.Succeeded-job.Failed), CancelRequested: job.CancelRequested,
		StartedAt: optionalTimestamp(job.StartedAt), CompletedAt: optionalTimestamp(job.CompletedAt),
		CreatedAt: formatTimestamp(job.CreatedAt), UpdatedAt: formatTimestamp(job.UpdatedAt),
		PartialItems: partial, Items: make([]ArtistArtworkBatchItemDTO, 0, len(items)),
	}
	for _, item := range items {
		result.Items = append(result.Items, ArtistArtworkBatchItemDTO{
			ID: item.ID, JobID: item.JobID, ArtistID: item.ArtistID,
			ExpectedVersion: item.ExpectedVersion, Position: item.Position, Status: item.Status,
			Attempts: item.Attempts, MaxAttempts: item.MaxAttempts,
			NextAttemptAt: formatTimestamp(item.NextAttemptAt), Candidate: item.Candidate,
			Source: item.Source, Message: item.Message, StartedAt: optionalTimestamp(item.StartedAt),
			CompletedAt: optionalTimestamp(item.CompletedAt), CreatedAt: formatTimestamp(item.CreatedAt),
			UpdatedAt: formatTimestamp(item.UpdatedAt),
		})
	}
	return result
}

func (service *ArtistArtworkBatchService) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *ArtistArtworkBatchService) setActive(jobID string, cancel context.CancelFunc) {
	service.activeMu.Lock()
	service.active[jobID] = cancel
	service.activeMu.Unlock()
}

func (service *ArtistArtworkBatchService) clearActive(jobID string) {
	service.activeMu.Lock()
	delete(service.active, jobID)
	service.activeMu.Unlock()
}

func (service *ArtistArtworkBatchService) cancelJob(jobID string) {
	service.activeMu.Lock()
	cancel := service.active[jobID]
	service.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (service *ArtistArtworkBatchService) cancelActive() {
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
