package adminmutation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/shared/apperror"
)

const (
	defaultPermanentDeleteLease         = 120 * time.Second
	defaultPermanentDeleteHeartbeat     = 30 * time.Second
	permanentDeleteStateSaveTimeout     = 15 * time.Second
	maximumPermanentDeleteRetryDelay    = 30 * time.Second
	maximumPermanentDeletePublicMessage = 4_000
)

var errPermanentDeleteWorkerStopped = errors.New("permanent delete batch worker stopped")

type PermanentDeleteBatchLogger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
	Error(string, map[string]any)
}

type noopPermanentDeleteBatchLogger struct{}

func (noopPermanentDeleteBatchLogger) Info(string, map[string]any)  {}
func (noopPermanentDeleteBatchLogger) Warn(string, map[string]any)  {}
func (noopPermanentDeleteBatchLogger) Error(string, map[string]any) {}

type PermanentDeleteBatchWorkerDependencies struct {
	Store            PermanentDeleteBatchWorkerStore
	Deleter          PermanentTrackDeleter
	LibraryDirectory string
	Logger           PermanentDeleteBatchLogger
	WorkerID         string
	Clock            func() time.Time
	Lease            time.Duration
	Heartbeat        time.Duration
	RetryBackoff     func(int) time.Duration
}

type PermanentDeleteBatchWorker struct {
	store            PermanentDeleteBatchWorkerStore
	deleter          PermanentTrackDeleter
	libraryDirectory string
	logger           PermanentDeleteBatchLogger
	workerID         string
	now              func() time.Time
	lease            time.Duration
	heartbeat        time.Duration
	retryBackoff     func(int) time.Duration
}

func NewPermanentDeleteBatchWorker(
	dependencies PermanentDeleteBatchWorkerDependencies,
) (*PermanentDeleteBatchWorker, error) {
	if dependencies.Store == nil {
		return nil, errors.New("permanent delete batch worker store is required")
	}
	if dependencies.Deleter == nil {
		return nil, errors.New("permanent delete batch track deleter is required")
	}
	if dependencies.Logger == nil {
		dependencies.Logger = noopPermanentDeleteBatchLogger{}
	}
	if strings.TrimSpace(dependencies.WorkerID) == "" {
		dependencies.WorkerID = "track-delete-" + uuid.NewString()
	}
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	if dependencies.Lease <= 0 {
		dependencies.Lease = defaultPermanentDeleteLease
	}
	if dependencies.Heartbeat <= 0 {
		dependencies.Heartbeat = defaultPermanentDeleteHeartbeat
	}
	if dependencies.Heartbeat >= dependencies.Lease {
		return nil, errors.New("permanent delete batch heartbeat must be shorter than the lease")
	}
	if dependencies.RetryBackoff == nil {
		dependencies.RetryBackoff = permanentDeleteRetryBackoff
	}
	return &PermanentDeleteBatchWorker{
		store: dependencies.Store, deleter: dependencies.Deleter,
		libraryDirectory: dependencies.LibraryDirectory,
		logger:           dependencies.Logger, workerID: dependencies.WorkerID,
		now: dependencies.Clock, lease: dependencies.Lease,
		heartbeat: dependencies.Heartbeat, retryBackoff: dependencies.RetryBackoff,
	}, nil
}

func (worker *PermanentDeleteBatchWorker) Initialize(ctx context.Context) error {
	return worker.store.InitializePermanentDeleteBatches(ctx, worker.now().UTC())
}

func (worker *PermanentDeleteBatchWorker) RunNext(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, nil
	}
	claim, err := worker.store.ClaimPermanentDeleteBatchItem(
		ctx, worker.workerID, worker.now().UTC(), worker.lease,
	)
	if err != nil {
		return false, err
	}
	if claim == nil {
		return false, nil
	}
	if claim.Item.AttemptID == nil {
		return true, ErrPermanentDeleteLeaseLost
	}
	attemptID := *claim.Item.AttemptID
	processContext, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	processingDone := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- worker.maintainLease(
			processContext, ctx, cancel, processingDone, claim.Item.ID, attemptID,
		)
	}()
	result, processErr := worker.deleter.DeleteTrackPermanently(
		processContext,
		claim.Item.TrackID,
		claim.Item.ExpectedVersion,
		worker.libraryDirectory,
	)
	close(processingDone)
	cancel(nil)
	heartbeatErr := <-heartbeatDone

	if errors.Is(heartbeatErr, ErrPermanentDeleteLeaseLost) {
		return true, ErrPermanentDeleteLeaseLost
	}
	if ctx.Err() != nil || errors.Is(context.Cause(processContext), errPermanentDeleteWorkerStopped) {
		return true, worker.releaseInterruptedClaim(claim.Item.ID, attemptID)
	}
	if heartbeatErr != nil {
		if releaseErr := worker.releaseInterruptedClaim(claim.Item.ID, attemptID); releaseErr != nil {
			return true, errors.Join(heartbeatErr, releaseErr)
		}
		return true, heartbeatErr
	}

	completionContext, completionCancel := worker.stateSaveContext(ctx)
	defer completionCancel()
	if processErr == nil {
		if err := worker.store.CompletePermanentDeleteBatchItemSuccess(
			completionContext, *claim, worker.workerID, result, nil, worker.now().UTC(),
		); err != nil {
			return true, err
		}
		worker.logger.Info("admin.track_delete_batch.item_succeeded", worker.logFields(*claim, nil))
		return true, nil
	}
	if apperror.IsCode(processErr, apperror.CodeResourceNotFound) {
		message := "Track was already permanently deleted; the retried item was completed idempotently"
		if err := worker.store.CompletePermanentDeleteBatchItemSuccess(
			completionContext, *claim, worker.workerID, DeleteResult{}, &message, worker.now().UTC(),
		); err != nil {
			return true, err
		}
		worker.logger.Info("admin.track_delete_batch.item_idempotent_success", worker.logFields(*claim, nil))
		return true, nil
	}
	code, message := permanentDeleteError(processErr)
	if isMetadataWritebackConflict(processErr) {
		nextAttemptAt := worker.now().UTC().Add(worker.retryBackoff(claim.Item.Attempts))
		if err := worker.store.RetryPermanentDeleteBatchItem(
			completionContext, claim.Item.ID, attemptID, worker.workerID,
			code, message, nextAttemptAt, worker.now().UTC(),
		); err != nil {
			return true, err
		}
		worker.logger.Warn("admin.track_delete_batch.item_retry_scheduled", worker.logFields(*claim, processErr))
		return true, nil
	}
	if err := worker.store.CompletePermanentDeleteBatchItemFailure(
		completionContext, *claim, worker.workerID, code, message, worker.now().UTC(),
	); err != nil {
		return true, err
	}
	worker.logger.Warn("admin.track_delete_batch.item_failed", worker.logFields(*claim, processErr))
	return true, nil
}

func (worker *PermanentDeleteBatchWorker) maintainLease(
	ctx context.Context,
	parent context.Context,
	cancel context.CancelCauseFunc,
	done <-chan struct{},
	itemID string,
	attemptID string,
) error {
	ticker := time.NewTicker(worker.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-parent.Done():
			cancel(errPermanentDeleteWorkerStopped)
			return nil
		case <-ctx.Done():
			return nil
		case <-done:
			return nil
		case heartbeatAt := <-ticker.C:
			heartbeatAt = heartbeatAt.UTC()
			owned, err := worker.store.RenewPermanentDeleteBatchItem(
				ctx, itemID, attemptID, worker.workerID,
				heartbeatAt, heartbeatAt.Add(worker.lease),
			)
			if err != nil {
				cancel(err)
				return err
			}
			if !owned {
				cancel(ErrPermanentDeleteLeaseLost)
				return ErrPermanentDeleteLeaseLost
			}
		}
	}
}

func (worker *PermanentDeleteBatchWorker) releaseInterruptedClaim(itemID, attemptID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), permanentDeleteStateSaveTimeout)
	defer cancel()
	err := worker.store.ReleasePermanentDeleteBatchItem(
		ctx, itemID, attemptID, worker.workerID, worker.now().UTC(),
	)
	if errors.Is(err, ErrPermanentDeleteLeaseLost) {
		return nil
	}
	return err
}

func (worker *PermanentDeleteBatchWorker) stateSaveContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), permanentDeleteStateSaveTimeout)
}

func (worker *PermanentDeleteBatchWorker) logFields(
	claim ClaimedPermanentDeleteItem,
	err error,
) map[string]any {
	fields := map[string]any{
		"batchId": claim.Job.ID, "itemId": claim.Item.ID,
		"trackId": claim.Item.TrackID, "workerId": worker.workerID,
	}
	if claim.Item.AttemptID != nil {
		fields["attemptId"] = *claim.Item.AttemptID
	}
	if err != nil {
		fields["message"] = truncatePermanentDeleteMessage(err.Error())
	}
	return fields
}

func isMetadataWritebackConflict(err error) bool {
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeResourceConflict {
		return false
	}
	resourceType, _ := applicationError.Metadata["conflictResourceType"].(string)
	return resourceType == "metadata_writeback_job"
}

func permanentDeleteError(err error) (string, string) {
	if applicationError, ok := apperror.As(err); ok {
		return string(applicationError.Code), truncatePermanentDeleteMessage(applicationError.Detail)
	}
	return string(apperror.CodeInternalError), "Permanent deletion failed unexpectedly"
}

func truncatePermanentDeleteMessage(message string) string {
	if len(message) <= maximumPermanentDeletePublicMessage {
		return message
	}
	return message[:maximumPermanentDeletePublicMessage]
}

func permanentDeleteRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 2 * time.Second
	for index := 1; index < attempt && delay < maximumPermanentDeleteRetryDelay; index++ {
		delay *= 2
		if delay >= maximumPermanentDeleteRetryDelay {
			return maximumPermanentDeleteRetryDelay
		}
	}
	return delay
}
