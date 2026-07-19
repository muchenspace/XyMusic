package admintagscraping

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	errBatchCancellationRequested = errors.New("tag scraping batch cancellation was requested")
	ErrBatchLeaseLost             = errors.New("tag scraping batch item lease was lost")
)

type BatchMutationFence struct {
	JobID     string
	ItemID    string
	AttemptID string
	WorkerID  string
}

type batchMutationContextKey struct{}

func withBatchMutationFence(ctx context.Context, fence *BatchMutationFence) context.Context {
	if fence == nil {
		return ctx
	}
	return context.WithValue(ctx, batchMutationContextKey{}, fence)
}

func batchMutationFenceFromContext(ctx context.Context) *BatchMutationFence {
	if ctx == nil {
		return nil
	}
	fence, _ := ctx.Value(batchMutationContextKey{}).(*BatchMutationFence)
	return fence
}

// Lock serializes a batch mutation with cancellation, lease renewal, reclaim,
// and completion. Every caller locks the job before the item to keep the
// transaction order stable across workers.
func (fence *BatchMutationFence) Lock(ctx context.Context, tx pgx.Tx) error {
	if fence == nil {
		return nil
	}
	if fence.JobID == "" || fence.ItemID == "" || fence.AttemptID == "" || fence.WorkerID == "" {
		return ErrBatchLeaseLost
	}
	var jobStatus string
	var cancelRequested bool
	err := tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested
		FROM tag_scraping_jobs
		WHERE id = $1
		FOR UPDATE`, fence.JobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBatchLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock tag scraping batch mutation job: %w", err)
	}
	if cancelRequested {
		return errBatchCancellationRequested
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return ErrBatchLeaseLost
	}

	var itemStatus string
	var attemptID, workerID *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, fence.ItemID, fence.JobID).Scan(
		&itemStatus, &attemptID, &workerID, &leaseActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBatchLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock tag scraping batch mutation item: %w", err)
	}
	if itemStatus != string(ItemRunning) || attemptID == nil || *attemptID != fence.AttemptID ||
		workerID == nil || *workerID != fence.WorkerID || !leaseActive {
		return ErrBatchLeaseLost
	}
	return nil
}
