package admintagscraping

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	errArtistArtworkBatchCancellationRequested = errors.New("artist artwork batch cancellation was requested")
	ErrArtistArtworkBatchLeaseLost             = errors.New("artist artwork batch item lease was lost")
)

type ArtistArtworkBatchMutationFence struct {
	JobID     string
	ItemID    string
	AttemptID string
	WorkerID  string
}

// Lock is called inside the final AdminMedia transaction. It prevents a
// cancelled, expired, or reclaimed batch attempt from committing artwork.
func (fence *ArtistArtworkBatchMutationFence) Lock(ctx context.Context, tx pgx.Tx) error {
	if fence == nil {
		return nil
	}
	if fence.JobID == "" || fence.ItemID == "" || fence.AttemptID == "" || fence.WorkerID == "" {
		return ErrArtistArtworkBatchLeaseLost
	}
	var jobStatus string
	var cancelRequested bool
	err := tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested
		FROM artist_artwork_scraping_jobs
		WHERE id = $1
		FOR UPDATE`, fence.JobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrArtistArtworkBatchLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock artist artwork batch mutation job: %w", err)
	}
	if cancelRequested {
		return errArtistArtworkBatchCancellationRequested
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return ErrArtistArtworkBatchLeaseLost
	}

	var itemStatus string
	var attemptID, workerID *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM artist_artwork_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, fence.ItemID, fence.JobID).Scan(
		&itemStatus, &attemptID, &workerID, &leaseActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrArtistArtworkBatchLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock artist artwork batch mutation item: %w", err)
	}
	if itemStatus != string(ItemRunning) || attemptID == nil || *attemptID != fence.AttemptID ||
		workerID == nil || *workerID != fence.WorkerID || !leaseActive {
		return ErrArtistArtworkBatchLeaseLost
	}
	return nil
}

// CommitSuccess records the item result before the surrounding AdminMedia
// transaction commits. Any later media or artist failure rolls this update
// back together with the artwork attachment.
func (fence *ArtistArtworkBatchMutationFence) CommitSuccess(
	ctx context.Context,
	tx pgx.Tx,
	candidate ArtistCandidate,
) error {
	if fence == nil || fence.JobID == "" || fence.ItemID == "" ||
		fence.AttemptID == "" || fence.WorkerID == "" {
		return ErrArtistArtworkBatchLeaseLost
	}
	candidateJSON, source, err := encodeArtistArtworkCandidate(&candidate)
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = 'SUCCEEDED', attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = $5::jsonb, source = $6,
			message = 'Artist artwork scraping completed',
			completed_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE id = $1 AND job_id = $2 AND attempt_id = $3 AND locked_by = $4
		  AND status = 'RUNNING'`,
		fence.ItemID, fence.JobID, fence.AttemptID, fence.WorkerID,
		nullableJSON(candidateJSON), source,
	)
	if err != nil {
		return fmt.Errorf("atomically complete artist artwork scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrArtistArtworkBatchLeaseLost
	}
	command, err = tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_jobs SET
			processed = processed + 1,
			succeeded = succeeded + 1,
			updated_at = clock_timestamp()
		WHERE id = $1 AND status IN ('PENDING', 'RUNNING')`, fence.JobID)
	if err != nil {
		return fmt.Errorf("atomically update artist artwork batch counts: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrArtistArtworkBatchLeaseLost
	}
	return nil
}

var _ artistArtworkSuccessFence = (*ArtistArtworkBatchMutationFence)(nil)
