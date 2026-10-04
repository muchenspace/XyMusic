package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

func (repository *Repository) ClaimBatchItem(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (ClaimResult, error) {
	result, err := repository.claimBatchItems(ctx, workerID, now, lease, 1)
	if err != nil {
		return ClaimResult{}, err
	}
	if result.FinishJobID != "" {
		return ClaimResult{FinishJobID: result.FinishJobID}, nil
	}
	if len(result.Items) == 0 {
		return ClaimResult{}, nil
	}
	item := result.Items[0]
	return ClaimResult{Item: &item}, nil
}

func (repository *Repository) ClaimBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (BatchClaimResult, error) {
	return repository.claimBatchItems(ctx, workerID, now, lease, limit)
}

func (repository *Repository) claimBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (BatchClaimResult, error) {
	if limit <= 0 {
		return BatchClaimResult{}, nil
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("begin tag scraping claim: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := scanBatchJob(tx.QueryRow(ctx, batchJobSelect+`
		WHERE status IN ('PENDING','RUNNING')
		  AND (
			tag_scraping_jobs.cancel_requested
			OR
			EXISTS (
				SELECT 1 FROM tag_scraping_job_items claimable
				WHERE claimable.job_id = tag_scraping_jobs.id
				  AND claimable.status = 'PENDING'
				  AND claimable.next_attempt_at <= GREATEST($1::timestamptz, clock_timestamp())
				  AND claimable.attempts < claimable.max_attempts
			) OR EXISTS (
				SELECT 1 FROM tag_scraping_job_items recoverable
				WHERE recoverable.job_id = tag_scraping_jobs.id
				  AND (
					recoverable.status = 'PENDING' AND recoverable.attempts >= recoverable.max_attempts
					OR recoverable.status = 'RUNNING'
					   AND (recoverable.locked_until IS NULL OR recoverable.locked_until < GREATEST($1::timestamptz, clock_timestamp()))
				  )
			) OR NOT EXISTS (
				SELECT 1 FROM tag_scraping_job_items active
				WHERE active.job_id = tag_scraping_jobs.id
				  AND active.status IN ('PENDING','RUNNING')
			)
		  )
		ORDER BY created_at, id FOR UPDATE SKIP LOCKED LIMIT 1`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return BatchClaimResult{}, fmt.Errorf("commit empty tag scraping claim: %w", commitErr)
		}
		return BatchClaimResult{}, nil
	}
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("claim tag scraping batch: %w", err)
	}
	if err := recoverExpiredBatchItems(ctx, tx, now, &job.ID); err != nil {
		return BatchClaimResult{}, err
	}
	if job.CancelRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE tag_scraping_job_items SET
				status = 'SKIPPED', attempt_id = NULL, locked_by = NULL, locked_until = NULL,
				message = 'The batch was cancelled', completed_at = $2, updated_at = $2
			WHERE job_id = $1 AND (
				status = 'PENDING' OR (status = 'RUNNING' AND (locked_until IS NULL OR locked_until < $2))
			)`, job.ID, now); err != nil {
			return BatchClaimResult{}, fmt.Errorf("skip cancelled tag scraping items: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return BatchClaimResult{}, fmt.Errorf("commit cancelled tag scraping claim: %w", err)
		}
		return BatchClaimResult{FinishJobID: job.ID}, nil
	}
	rows, err := tx.Query(ctx, claimedBatchItemSelect+`
		WHERE item.job_id = $1 AND item.status = 'PENDING'
		  AND item.next_attempt_at <= GREATEST($2::timestamptz, clock_timestamp())
		  AND item.attempts < item.max_attempts
		ORDER BY item.position FOR UPDATE OF item SKIP LOCKED LIMIT $3`, job.ID, now, limit)
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("query tag scraping items: %w", err)
	}
	type pendingClaim struct {
		item     BatchItemRecord
		metadata *TrackMetadata
	}
	claims := make([]pendingClaim, 0, limit)
	for rows.Next() {
		item, metadata, scanErr := scanClaimedBatchItem(rows)
		if scanErr != nil {
			rows.Close()
			return BatchClaimResult{}, fmt.Errorf("scan tag scraping item: %w", scanErr)
		}
		claims = append(claims, pendingClaim{item: item, metadata: metadata})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BatchClaimResult{}, fmt.Errorf("iterate tag scraping items: %w", err)
	}
	rows.Close()
	if len(claims) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return BatchClaimResult{}, fmt.Errorf("commit empty tag scraping claim: %w", err)
		}
		return BatchClaimResult{FinishJobID: job.ID}, nil
	}
	itemIDs := make([]string, len(claims))
	for index := range claims {
		itemIDs[index] = claims[index].item.ID
	}
	attemptRows, err := tx.Query(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'RUNNING', attempts = attempts + 1, attempt_id = gen_random_uuid(),
			locked_by = $3, locked_until = $4, started_at = $5, completed_at = NULL,
			updated_at = $5
		WHERE id = ANY($6::uuid[]) AND job_id = $1
		  AND status = 'PENDING' AND next_attempt_at <= GREATEST($2::timestamptz, clock_timestamp())
		  AND attempts < max_attempts
		RETURNING id::text, attempt_id::text`, job.ID, now, workerID, now.Add(lease), now, itemIDs)
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("own tag scraping items: %w", err)
	}
	attemptIDs := make(map[string]string, len(claims))
	for attemptRows.Next() {
		var itemID, attemptID string
		if err := attemptRows.Scan(&itemID, &attemptID); err != nil {
			attemptRows.Close()
			return BatchClaimResult{}, fmt.Errorf("scan tag scraping attempt: %w", err)
		}
		attemptIDs[itemID] = attemptID
	}
	if err := attemptRows.Err(); err != nil {
		attemptRows.Close()
		return BatchClaimResult{}, fmt.Errorf("iterate tag scraping attempts: %w", err)
	}
	attemptRows.Close()
	if len(attemptIDs) != len(claims) {
		return BatchClaimResult{}, errors.New("claimed tag scraping items disappeared")
	}
	if job.Status == JobPending {
		if _, err := tx.Exec(ctx, `
			UPDATE tag_scraping_jobs SET status = 'RUNNING', started_at = COALESCE(started_at, $2), updated_at = $2
			WHERE id = $1`, job.ID, now); err != nil {
			return BatchClaimResult{}, fmt.Errorf("start tag scraping batch: %w", err)
		}
		job.Status = JobRunning
		if job.StartedAt == nil {
			started := now
			job.StartedAt = &started
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchClaimResult{}, fmt.Errorf("commit tag scraping claim: %w", err)
	}
	lockedUntil := now.Add(lease)
	result := BatchClaimResult{Items: make([]ClaimedBatchItem, len(claims))}
	for index, claim := range claims {
		attemptID := attemptIDs[claim.item.ID]
		claim.item.Status = ItemRunning
		claim.item.Attempts++
		claim.item.AttemptID = &attemptID
		claim.item.LockedBy = &workerID
		claim.item.LockedUntil = &lockedUntil
		claim.item.StartedAt = &now
		result.Items[index] = ClaimedBatchItem{
			Job: job, Item: claim.item, AttemptID: attemptID, Metadata: claim.metadata,
		}
	}
	return result, nil
}

func (repository *Repository) RenewBatchItemLease(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	lockedUntil time.Time,
) (BatchLeaseControl, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("begin tag scraping lease renewal: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested FROM tag_scraping_jobs
		WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, nil
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping lease job: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return BatchLeaseControl{}, nil
	}
	var itemStatus string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, itemID, jobID).Scan(&itemStatus, &currentAttempt, &currentWorker, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, nil
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping lease item: %w", err)
	}
	if itemStatus != string(ItemRunning) || currentAttempt == nil || *currentAttempt != attemptID ||
		currentWorker == nil || *currentWorker != workerID || !leaseActive {
		return BatchLeaseControl{}, nil
	}
	if !cancelRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE tag_scraping_job_items SET locked_until = $2, updated_at = now()
			WHERE id = $1`, itemID, lockedUntil); err != nil {
			return BatchLeaseControl{}, fmt.Errorf("renew tag scraping item lease: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchLeaseControl{}, fmt.Errorf("commit tag scraping lease renewal: %w", err)
	}
	return BatchLeaseControl{Owned: true, CancelRequested: cancelRequested}, nil
}

func (repository *Repository) BatchCancelRequested(ctx context.Context, jobID string) (bool, error) {
	var requested bool
	err := repository.pool.QueryRow(ctx, "SELECT cancel_requested FROM tag_scraping_jobs WHERE id = $1", jobID).Scan(&requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read tag scraping cancellation: %w", err)
	}
	return requested, nil
}

func (repository *Repository) RetryBatchItem(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	candidate *Candidate,
	message string,
	nextAttemptAt time.Time,
	now time.Time,
) (BatchLeaseControl, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("begin tag scraping item retry: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested
		FROM tag_scraping_jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping item retry job: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	var itemStatus string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, itemID, jobID).Scan(&itemStatus, &currentAttempt, &currentWorker, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping item retry item: %w", err)
	}
	if itemStatus != string(ItemRunning) || currentAttempt == nil || *currentAttempt != attemptID ||
		currentWorker == nil || *currentWorker != workerID || !leaseActive {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if cancelRequested {
		if err := tx.Commit(ctx); err != nil {
			return BatchLeaseControl{}, fmt.Errorf("commit cancelled tag scraping retry check: %w", err)
		}
		return BatchLeaseControl{Owned: true, CancelRequested: true}, nil
	}
	var candidateJSON []byte
	var source *string
	if candidate != nil {
		candidateJSON, err = json.Marshal(candidate)
		if err != nil {
			return BatchLeaseControl{}, fmt.Errorf("encode tag scraping retry candidate: %w", err)
		}
		value := string(candidate.Source)
		source = &value
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	command, err := tx.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'PENDING', next_attempt_at = $5,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = $6::jsonb, source = $7, message = $8,
			started_at = NULL, completed_at = NULL, updated_at = $9
		WHERE id = $1 AND job_id = $2 AND attempt_id = $3 AND locked_by = $4
		  AND status = 'RUNNING' AND attempts < max_attempts`,
		itemID, jobID, attemptID, workerID, nextAttemptAt,
		nullableJSON(candidateJSON), source, message, now,
	)
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("requeue tag scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchLeaseControl{}, fmt.Errorf("commit tag scraping item retry: %w", err)
	}
	return BatchLeaseControl{Owned: true}, nil
}

func (repository *Repository) CompleteBatchItem(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	status ItemStatus,
	candidate *Candidate,
	message string,
	now time.Time,
) (bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin tag scraping item completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var candidateJSON []byte
	var source *string
	if candidate != nil {
		candidateJSON, _ = json.Marshal(candidate)
		value := string(candidate.Source)
		source = &value
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `SELECT status::text, cancel_requested FROM tag_scraping_jobs
		WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrBatchLeaseLost
	}
	if err != nil {
		return false, fmt.Errorf("lock tag scraping item completion: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return false, ErrBatchLeaseLost
	}
	var itemStatus string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, itemID, jobID).Scan(&itemStatus, &currentAttempt, &currentWorker, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrBatchLeaseLost
	}
	if err != nil {
		return false, fmt.Errorf("lock tag scraping completion item: %w", err)
	}
	if itemStatus != string(ItemRunning) || currentAttempt == nil || *currentAttempt != attemptID ||
		currentWorker == nil || *currentWorker != workerID || !leaseActive {
		return false, ErrBatchLeaseLost
	}
	finalStatus := status
	if cancelRequested {
		finalStatus, candidateJSON, source, message = ItemSkipped, nil, nil, "The batch was cancelled"
	}
	command, err := tx.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = $4, attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = $5::jsonb, source = $6, message = $7,
			completed_at = $8, updated_at = $8
		WHERE id = $1 AND job_id = $2 AND attempt_id = $3`, itemID, jobID, attemptID,
		string(finalStatus), nullableJSON(candidateJSON), source, message, now)
	if err != nil {
		return false, fmt.Errorf("complete tag scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return false, ErrBatchLeaseLost
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tag_scraping_jobs SET
			processed = processed + 1,
			succeeded = succeeded + $2,
			failed = failed + $3,
			updated_at = $4
		WHERE id = $1`, jobID, boolInt(finalStatus == ItemSucceeded), boolInt(finalStatus == ItemFailed), now); err != nil {
		return false, fmt.Errorf("update tag scraping batch counts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tag scraping item completion: %w", err)
	}
	return true, nil
}

type batchCompletionPayload struct {
	ItemID    string     `json:"item_id"`
	AttemptID string     `json:"attempt_id"`
	Status    ItemStatus `json:"status"`
	Candidate *Candidate `json:"candidate,omitempty"`
	Source    *string    `json:"source,omitempty"`
	Message   string     `json:"message"`
}

// CompleteBatchItems keeps the attempt and lease checks in the UPDATE
// predicate, so a stale result can be omitted without affecting newer work.
// The job row is locked once for the whole completion window and counts are
// advanced only for rows that passed that predicate.
func (repository *Repository) CompleteBatchItems(
	ctx context.Context,
	jobID string,
	workerID string,
	completions []BatchItemCompletion,
	now time.Time,
) ([]string, error) {
	if len(completions) == 0 {
		return nil, nil
	}
	payload := make([]batchCompletionPayload, len(completions))
	seenItems := make(map[string]struct{}, len(completions))
	for index, completion := range completions {
		if completion.ItemID == "" || completion.AttemptID == "" {
			return nil, errors.New("tag scraping batch completion is missing an item or attempt")
		}
		if _, exists := seenItems[completion.ItemID]; exists {
			return nil, fmt.Errorf("tag scraping batch completion contains duplicate item %q", completion.ItemID)
		}
		seenItems[completion.ItemID] = struct{}{}
		var source *string
		if completion.Candidate != nil {
			value := string(completion.Candidate.Source)
			source = &value
		}
		payload[index] = batchCompletionPayload{
			ItemID: completion.ItemID, AttemptID: completion.AttemptID, Status: completion.Status,
			Candidate: completion.Candidate, Source: source, Message: completion.Message,
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode tag scraping batch completion: %w", err)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tag scraping batch completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `SELECT status::text,cancel_requested
		FROM tag_scraping_jobs WHERE id=$1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBatchLeaseLost
	}
	if err != nil {
		return nil, fmt.Errorf("lock tag scraping batch completion: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return nil, ErrBatchLeaseLost
	}
	rows, err := tx.Query(ctx, `
		WITH input AS (
			SELECT item_id,attempt_id,status,candidate,source,message
			FROM jsonb_to_recordset($3::jsonb) AS input(
				item_id uuid,attempt_id uuid,status text,candidate jsonb,source text,message text
			)
		)
		UPDATE tag_scraping_job_items item SET
			status = CASE WHEN $4 THEN 'SKIPPED'::tag_scraping_item_status
				ELSE input.status::tag_scraping_item_status END,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = CASE WHEN $4 THEN NULL::jsonb ELSE input.candidate END,
			source = CASE WHEN $4 THEN NULL::varchar ELSE input.source END,
			message = CASE WHEN $4 THEN 'The batch was cancelled' ELSE LEFT(input.message, 4000) END,
			completed_at = $5, updated_at = $5
		FROM input
		WHERE item.id = input.item_id AND item.job_id = $1
			AND item.status = 'RUNNING'
			AND item.attempt_id = input.attempt_id
			AND item.locked_by = $2
			AND COALESCE(item.locked_until > clock_timestamp(), false)
		RETURNING item.id::text,item.status::text`, jobID, workerID, encoded, cancelRequested, now)
	if err != nil {
		return nil, fmt.Errorf("complete tag scraping batch items: %w", err)
	}
	completedIDs := make([]string, 0, len(completions))
	succeeded, failed := 0, 0
	for rows.Next() {
		var itemID, status string
		if err := rows.Scan(&itemID, &status); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan tag scraping batch completion: %w", err)
		}
		completedIDs = append(completedIDs, itemID)
		switch ItemStatus(status) {
		case ItemSucceeded:
			succeeded++
		case ItemFailed:
			failed++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate tag scraping batch completion: %w", err)
	}
	rows.Close()
	if len(completedIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE tag_scraping_jobs SET
			processed=processed+$2,succeeded=succeeded+$3,failed=failed+$4,updated_at=$5
			WHERE id=$1`, jobID, len(completedIDs), succeeded, failed, now); err != nil {
			return nil, fmt.Errorf("update tag scraping batch counts: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tag scraping batch completion: %w", err)
	}
	return completedIDs, nil
}

func (repository *Repository) ReleaseBatchItem(
	ctx context.Context,
	itemID string,
	attemptID string,
	workerID string,
	now time.Time,
) error {
	command, err := repository.pool.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'PENDING', attempts = GREATEST(attempts - 1, 0), next_attempt_at = $4,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			started_at = NULL, updated_at = $4
		WHERE id = $1 AND status = 'RUNNING' AND attempt_id = $2 AND locked_by = $3`,
		itemID, attemptID, workerID, now)
	if err != nil {
		return fmt.Errorf("release tag scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrBatchLeaseLost
	}
	return nil
}

func (repository *Repository) FinishBatch(ctx context.Context, jobID string, now time.Time) (bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin tag scraping finish: %w", err)
	}
	defer tx.Rollback(ctx)
	var cancelRequested bool
	err = tx.QueryRow(ctx, "SELECT cancel_requested FROM tag_scraping_jobs WHERE id = $1 FOR UPDATE", jobID).Scan(&cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, apperror.NotFound("Tag scraping batch was not found")
	}
	if err != nil {
		return false, fmt.Errorf("lock tag scraping finish: %w", err)
	}
	var total, active, succeeded, failed int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE status IN ('PENDING','RUNNING'))::int,
		       count(*) FILTER (WHERE status = 'SUCCEEDED')::int,
		       count(*) FILTER (WHERE status = 'FAILED')::int
		FROM tag_scraping_job_items WHERE job_id = $1`, jobID).Scan(&total, &active, &succeeded, &failed); err != nil {
		return false, fmt.Errorf("count tag scraping items: %w", err)
	}
	if active > 0 {
		return false, nil
	}
	status := JobCompleted
	if cancelRequested {
		status = JobCancelled
	} else if failed > 0 {
		status = JobFailed
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tag_scraping_jobs SET
			status = $2, processed = $3, succeeded = $4, failed = $5,
			completed_at = $6, updated_at = $6
		WHERE id = $1 AND status IN ('PENDING','RUNNING')`,
		jobID, string(status), total, succeeded, failed, now); err != nil {
		return false, fmt.Errorf("finish tag scraping batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tag scraping finish: %w", err)
	}
	return true, nil
}
