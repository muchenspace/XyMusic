package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

func (repository *Repository) CreateArtistArtworkBatch(
	ctx context.Context,
	actorID string,
	input CreateArtistArtworkBatchInput,
	maxAttempts int,
) (string, int, int, error) {
	if input.Options.Overwrite {
		return "", 0, 0, apperror.Validation("Artist artwork batches cannot overwrite existing artwork")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", 0, 0, fmt.Errorf("begin artist artwork scraping batch: %w", err)
	}
	defer tx.Rollback(ctx)

	artistIDs := make([]string, 0, len(input.Items))
	for _, item := range input.Items {
		artistIDs = append(artistIDs, item.ArtistID)
	}
	rows, err := tx.Query(ctx, `
		SELECT artist.id, artist.name, artist.normalized_name, artist.version,
		       artist.artwork_asset_id IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM track_artists credit
		         WHERE credit.artist_id = artist.id AND credit.role IN ('PRIMARY', 'FEATURED')
		       ) OR EXISTS (
		         SELECT 1 FROM album_artists credit
		         WHERE credit.artist_id = artist.id AND credit.role IN ('PRIMARY', 'FEATURED')
		       )
		FROM artists artist
		WHERE artist.id = ANY($1::uuid[])
		ORDER BY artist.id
		FOR UPDATE OF artist`, artistIDs)
	if err != nil {
		return "", 0, 0, fmt.Errorf("lock artist artwork batch candidates: %w", err)
	}
	targets := make(map[string]ArtistArtworkBatchTarget, len(input.Items))
	for rows.Next() {
		var target ArtistArtworkBatchTarget
		if err := rows.Scan(
			&target.ID, &target.Name, &target.NormalizedName, &target.Version,
			&target.HasArtwork, &target.PerformerRole,
		); err != nil {
			rows.Close()
			return "", 0, 0, fmt.Errorf("scan artist artwork batch candidate: %w", err)
		}
		targets[target.ID] = target
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", 0, 0, fmt.Errorf("iterate artist artwork batch candidates: %w", err)
	}
	rows.Close()

	selected := make([]ArtistArtworkBatchItemInput, 0, len(input.Items))
	excluded := 0
	for _, item := range input.Items {
		target, exists := targets[item.ArtistID]
		if !exists {
			return "", 0, 0, apperror.NotFound("Artist was not found")
		}
		if !target.PerformerRole || !artistArtworkScrapeNameEligible(target) ||
			(!input.Options.Overwrite && target.HasArtwork) {
			excluded++
			continue
		}
		if target.Version != item.ExpectedVersion {
			return "", 0, 0, apperror.Conflict(
				apperror.CodeVersionConflict,
				"Artist version changed; refresh and try again",
				map[string]any{
					"artistId": item.ArtistID, "expectedVersion": item.ExpectedVersion,
					"currentVersion": target.Version,
				},
			)
		}
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		return "", 0, excluded, nil
	}

	jobID := uuid.NewString()
	optionsJSON, err := json.Marshal(input.Options)
	if err != nil {
		return "", 0, 0, fmt.Errorf("encode artist artwork batch options: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO artist_artwork_scraping_jobs (id, requested_by, options, total)
		VALUES ($1, $2, $3::jsonb, $4)`, jobID, actorID, optionsJSON, len(selected)); err != nil {
		return "", 0, 0, fmt.Errorf("insert artist artwork scraping batch: %w", err)
	}
	batch := &pgx.Batch{}
	for position, item := range selected {
		batch.Queue(`
			INSERT INTO artist_artwork_scraping_job_items (
				id, job_id, artist_id, expected_version, position, max_attempts
			) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.NewString(), jobID, item.ArtistID, item.ExpectedVersion, position, maxAttempts,
		)
	}
	results := tx.SendBatch(ctx, batch)
	for range selected {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return "", 0, 0, fmt.Errorf("insert artist artwork scraping batch item: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return "", 0, 0, fmt.Errorf("close artist artwork batch item insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, 0, fmt.Errorf("commit artist artwork scraping batch: %w", err)
	}
	return jobID, len(selected), excluded, nil
}

func (repository *Repository) ArtistArtworkBatch(
	ctx context.Context,
	jobID string,
	updatedAfter *time.Time,
) (ArtistArtworkBatchJobRecord, []ArtistArtworkBatchItemRecord, error) {
	job, err := scanArtistArtworkBatchJob(repository.pool.QueryRow(
		ctx, artistArtworkBatchJobSelect+" WHERE id = $1", jobID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ArtistArtworkBatchJobRecord{}, nil, apperror.NotFound("Artist artwork scraping batch was not found")
	}
	if err != nil {
		return ArtistArtworkBatchJobRecord{}, nil, fmt.Errorf("find artist artwork scraping batch: %w", err)
	}
	query := artistArtworkBatchItemSelect + " WHERE job_id = $1"
	arguments := []any{jobID}
	if updatedAfter != nil {
		query += " AND updated_at > $2"
		arguments = append(arguments, *updatedAfter)
	}
	query += " ORDER BY position"
	rows, err := repository.pool.Query(ctx, query, arguments...)
	if err != nil {
		return ArtistArtworkBatchJobRecord{}, nil, fmt.Errorf("query artist artwork scraping batch items: %w", err)
	}
	defer rows.Close()
	items := make([]ArtistArtworkBatchItemRecord, 0)
	for rows.Next() {
		item, scanErr := scanArtistArtworkBatchItem(rows)
		if scanErr != nil {
			return ArtistArtworkBatchJobRecord{}, nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ArtistArtworkBatchJobRecord{}, nil, fmt.Errorf("iterate artist artwork scraping batch items: %w", err)
	}
	return job, items, nil
}

func (repository *Repository) RequestArtistArtworkBatchCancel(ctx context.Context, jobID string) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin artist artwork batch cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status::text FROM artist_artwork_scraping_jobs
		WHERE id = $1 FOR UPDATE`, jobID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Artist artwork scraping batch was not found")
	}
	if err != nil {
		return fmt.Errorf("lock artist artwork batch cancellation: %w", err)
	}
	if status != string(JobPending) && status != string(JobRunning) {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"The artist artwork batch has already finished and cannot be cancelled",
			nil,
		)
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_jobs
		SET cancel_requested = true, updated_at = $2
		WHERE id = $1`, jobID, now); err != nil {
		return fmt.Errorf("cancel artist artwork scraping batch: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = 'SKIPPED', attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			message = 'The batch was cancelled', completed_at = $2, updated_at = $2
		WHERE job_id = $1 AND status = 'PENDING'`, jobID, now); err != nil {
		return fmt.Errorf("skip pending cancelled artist artwork items: %w", err)
	}
	if err := recountArtistArtworkBatch(ctx, tx, jobID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit artist artwork batch cancellation: %w", err)
	}
	return nil
}

func (repository *Repository) RetryArtistArtworkBatch(ctx context.Context, jobID string) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin artist artwork batch retry: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status::text FROM artist_artwork_scraping_jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Artist artwork scraping batch was not found")
	}
	if err != nil {
		return fmt.Errorf("lock artist artwork scraping batch: %w", err)
	}
	if status != string(JobFailed) && status != string(JobCompleted) {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Only finished artist artwork batches can retry failed items",
			nil,
		)
	}
	command, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = 'PENDING', attempts = 0, next_attempt_at = now(),
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = NULL, source = NULL, message = NULL,
			started_at = NULL, completed_at = NULL, updated_at = now()
		WHERE job_id = $1 AND status = 'FAILED'`, jobID)
	if err != nil {
		return fmt.Errorf("reset failed artist artwork scraping items: %w", err)
	}
	if command.RowsAffected() == 0 {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"The artist artwork batch has no failed items to retry",
			nil,
		)
	}
	if err := recountArtistArtworkBatch(ctx, tx, jobID, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_jobs SET
			status = 'PENDING', cancel_requested = false,
			completed_at = NULL, updated_at = now()
		WHERE id = $1`, jobID); err != nil {
		return fmt.Errorf("reopen artist artwork scraping batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit artist artwork batch retry: %w", err)
	}
	return nil
}

func (repository *Repository) RecoverExpiredArtistArtworkBatchItems(ctx context.Context, now time.Time) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin artist artwork batch recovery: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := recoverExpiredArtistArtworkBatchItems(ctx, tx, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit artist artwork batch recovery: %w", err)
	}
	return nil
}

func (repository *Repository) ClaimArtistArtworkBatchItem(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (ArtistArtworkBatchClaimResult, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("begin artist artwork batch claim: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := recoverExpiredArtistArtworkBatchItems(ctx, tx, now); err != nil {
		return ArtistArtworkBatchClaimResult{}, err
	}
	job, err := scanArtistArtworkBatchJob(tx.QueryRow(ctx, artistArtworkBatchJobSelect+`
		WHERE status IN ('PENDING', 'RUNNING')
		  AND (
		    EXISTS (
		      SELECT 1 FROM artist_artwork_scraping_job_items claimable
		      WHERE claimable.job_id = artist_artwork_scraping_jobs.id
		        AND claimable.status = 'PENDING'
		        AND claimable.next_attempt_at <= $1
		        AND claimable.attempts < claimable.max_attempts
		    ) OR NOT EXISTS (
		      SELECT 1 FROM artist_artwork_scraping_job_items active
		      WHERE active.job_id = artist_artwork_scraping_jobs.id
		        AND active.status IN ('PENDING', 'RUNNING')
		    )
		  )
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED LIMIT 1`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("commit empty artist artwork claim: %w", commitErr)
		}
		return ArtistArtworkBatchClaimResult{}, nil
	}
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("claim artist artwork scraping batch: %w", err)
	}
	if job.CancelRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE artist_artwork_scraping_job_items SET
				status = 'SKIPPED', attempt_id = NULL, locked_by = NULL, locked_until = NULL,
				message = 'The batch was cancelled', completed_at = $2, updated_at = $2
			WHERE job_id = $1 AND status = 'PENDING'`, job.ID, now); err != nil {
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("skip cancelled artist artwork batch items: %w", err)
		}
		if err := recountArtistArtworkBatch(ctx, tx, job.ID, now); err != nil {
			return ArtistArtworkBatchClaimResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("commit cancelled artist artwork claim: %w", err)
		}
		return ArtistArtworkBatchClaimResult{FinishJobID: job.ID}, nil
	}
	item, err := scanArtistArtworkBatchItem(tx.QueryRow(ctx, artistArtworkBatchItemSelect+`
		WHERE job_id = $1 AND status = 'PENDING'
		  AND next_attempt_at <= $2 AND attempts < max_attempts
		ORDER BY position FOR UPDATE SKIP LOCKED LIMIT 1`, job.ID, now))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("commit empty artist artwork item claim: %w", err)
		}
		return ArtistArtworkBatchClaimResult{FinishJobID: job.ID}, nil
	}
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("claim artist artwork scraping item: %w", err)
	}
	attemptID := uuid.NewString()
	lockedUntil := now.Add(lease)
	command, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = 'RUNNING', attempts = attempts + 1,
			attempt_id = $2, locked_by = $3, locked_until = $4,
			started_at = $5, completed_at = NULL, updated_at = $5
		WHERE id = $1 AND status = 'PENDING' AND attempts < max_attempts`,
		item.ID, attemptID, workerID, lockedUntil, now,
	)
	if err != nil || command.RowsAffected() != 1 {
		if err == nil {
			err = errors.New("claimed artist artwork item disappeared")
		}
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("own artist artwork scraping item: %w", err)
	}
	target, err := artistArtworkBatchTarget(ctx, tx, item.ArtistID)
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, err
	}
	if job.Status == JobPending {
		if _, err := tx.Exec(ctx, `
			UPDATE artist_artwork_scraping_jobs SET
				status = 'RUNNING', started_at = COALESCE(started_at, $2), updated_at = $2
			WHERE id = $1`, job.ID, now); err != nil {
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("start artist artwork scraping batch: %w", err)
		}
		job.Status = JobRunning
		if job.StartedAt == nil {
			started := now
			job.StartedAt = &started
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("commit artist artwork scraping claim: %w", err)
	}
	item.Status = ItemRunning
	item.Attempts++
	item.AttemptID = &attemptID
	item.LockedBy = &workerID
	item.LockedUntil = &lockedUntil
	item.StartedAt = &now
	return ArtistArtworkBatchClaimResult{Item: &ClaimedArtistArtworkBatchItem{
		Job: job, Item: item, Target: target, AttemptID: attemptID,
	}}, nil
}

func (repository *Repository) RenewArtistArtworkBatchItemLease(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	lockedUntil time.Time,
) (BatchLeaseControl, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("begin artist artwork lease renewal: %w", err)
	}
	defer tx.Rollback(ctx)
	jobStatus, cancelRequested, err := lockArtistArtworkBatchJob(ctx, tx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, nil
	}
	if err != nil {
		return BatchLeaseControl{}, err
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return BatchLeaseControl{}, nil
	}
	owned, err := lockArtistArtworkBatchItemOwnership(ctx, tx, jobID, itemID, attemptID, workerID)
	if err != nil {
		return BatchLeaseControl{}, err
	}
	if !owned {
		return BatchLeaseControl{}, nil
	}
	if !cancelRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE artist_artwork_scraping_job_items
			SET locked_until = $2, updated_at = now()
			WHERE id = $1`, itemID, lockedUntil); err != nil {
			return BatchLeaseControl{}, fmt.Errorf("renew artist artwork item lease: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchLeaseControl{}, fmt.Errorf("commit artist artwork lease renewal: %w", err)
	}
	return BatchLeaseControl{Owned: true, CancelRequested: cancelRequested}, nil
}

func (repository *Repository) ArtistArtworkBatchCancelRequested(ctx context.Context, jobID string) (bool, error) {
	var requested bool
	err := repository.pool.QueryRow(ctx, `
		SELECT cancel_requested FROM artist_artwork_scraping_jobs WHERE id = $1`, jobID).Scan(&requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read artist artwork batch cancellation: %w", err)
	}
	return requested, nil
}

func (repository *Repository) RetryArtistArtworkBatchItem(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	candidate *ArtistCandidate,
	message string,
	nextAttemptAt time.Time,
	now time.Time,
) (BatchLeaseControl, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("begin artist artwork item retry: %w", err)
	}
	defer tx.Rollback(ctx)
	jobStatus, cancelRequested, err := lockArtistArtworkBatchJob(ctx, tx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, ErrArtistArtworkBatchLeaseLost
	}
	if err != nil {
		return BatchLeaseControl{}, err
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return BatchLeaseControl{}, ErrArtistArtworkBatchLeaseLost
	}
	owned, err := lockArtistArtworkBatchItemOwnership(ctx, tx, jobID, itemID, attemptID, workerID)
	if err != nil {
		return BatchLeaseControl{}, err
	}
	if !owned {
		return BatchLeaseControl{}, ErrArtistArtworkBatchLeaseLost
	}
	if cancelRequested {
		if err := tx.Commit(ctx); err != nil {
			return BatchLeaseControl{}, fmt.Errorf("commit cancelled artist artwork retry check: %w", err)
		}
		return BatchLeaseControl{Owned: true, CancelRequested: true}, nil
	}
	candidateJSON, source, err := encodeArtistArtworkCandidate(candidate)
	if err != nil {
		return BatchLeaseControl{}, err
	}
	message = truncateArtistArtworkBatchMessage(message)
	command, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
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
		return BatchLeaseControl{}, fmt.Errorf("requeue artist artwork scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return BatchLeaseControl{}, ErrArtistArtworkBatchLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchLeaseControl{}, fmt.Errorf("commit artist artwork item retry: %w", err)
	}
	return BatchLeaseControl{Owned: true}, nil
}

func (repository *Repository) CompleteArtistArtworkBatchItem(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	status ItemStatus,
	candidate *ArtistCandidate,
	message string,
	now time.Time,
) (bool, error) {
	if status != ItemSucceeded && status != ItemFailed && status != ItemSkipped {
		return false, errors.New("artist artwork batch item completion status is invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin artist artwork item completion: %w", err)
	}
	defer tx.Rollback(ctx)
	jobStatus, cancelRequested, err := lockArtistArtworkBatchJob(ctx, tx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrArtistArtworkBatchLeaseLost
	}
	if err != nil {
		return false, err
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return false, ErrArtistArtworkBatchLeaseLost
	}
	owned, err := lockArtistArtworkBatchItemOwnership(ctx, tx, jobID, itemID, attemptID, workerID)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, ErrArtistArtworkBatchLeaseLost
	}
	finalStatus := status
	if cancelRequested && status != ItemSucceeded {
		finalStatus, candidate, message = ItemSkipped, nil, "The batch was cancelled"
	}
	candidateJSON, source, err := encodeArtistArtworkCandidate(candidate)
	if err != nil {
		return false, err
	}
	message = truncateArtistArtworkBatchMessage(message)
	command, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = $5, attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = $6::jsonb, source = $7, message = $8,
			completed_at = $9, updated_at = $9
		WHERE id = $1 AND job_id = $2 AND attempt_id = $3 AND locked_by = $4`,
		itemID, jobID, attemptID, workerID, string(finalStatus),
		nullableJSON(candidateJSON), source, message, now,
	)
	if err != nil {
		return false, fmt.Errorf("complete artist artwork scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return false, ErrArtistArtworkBatchLeaseLost
	}
	if _, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_jobs SET
			processed = processed + 1,
			succeeded = succeeded + $2,
			failed = failed + $3,
			updated_at = $4
		WHERE id = $1`, jobID, boolInt(finalStatus == ItemSucceeded), boolInt(finalStatus == ItemFailed), now); err != nil {
		return false, fmt.Errorf("update artist artwork batch counts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit artist artwork item completion: %w", err)
	}
	return true, nil
}

func (repository *Repository) ReleaseArtistArtworkBatchItem(
	ctx context.Context,
	itemID string,
	attemptID string,
	workerID string,
	now time.Time,
) error {
	command, err := repository.pool.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = 'PENDING', attempts = GREATEST(attempts - 1, 0), next_attempt_at = $4,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			started_at = NULL, updated_at = $4
		WHERE id = $1 AND status = 'RUNNING' AND attempt_id = $2 AND locked_by = $3`,
		itemID, attemptID, workerID, now)
	if err != nil {
		return fmt.Errorf("release artist artwork scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrArtistArtworkBatchLeaseLost
	}
	return nil
}

func (repository *Repository) FinishArtistArtworkBatch(
	ctx context.Context,
	jobID string,
	now time.Time,
) (bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin artist artwork batch finish: %w", err)
	}
	defer tx.Rollback(ctx)
	var cancelRequested bool
	err = tx.QueryRow(ctx, `
		SELECT cancel_requested FROM artist_artwork_scraping_jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, apperror.NotFound("Artist artwork scraping batch was not found")
	}
	if err != nil {
		return false, fmt.Errorf("lock artist artwork batch finish: %w", err)
	}
	var total, active, succeeded, failed int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE status IN ('PENDING', 'RUNNING'))::int,
		       count(*) FILTER (WHERE status = 'SUCCEEDED')::int,
		       count(*) FILTER (WHERE status = 'FAILED')::int
		FROM artist_artwork_scraping_job_items WHERE job_id = $1`, jobID).Scan(
		&total, &active, &succeeded, &failed,
	); err != nil {
		return false, fmt.Errorf("count artist artwork scraping batch items: %w", err)
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
		UPDATE artist_artwork_scraping_jobs SET
			status = $2, processed = $3, succeeded = $4, failed = $5,
			completed_at = $6, updated_at = $6
		WHERE id = $1 AND status IN ('PENDING', 'RUNNING')`,
		jobID, string(status), total, succeeded, failed, now,
	); err != nil {
		return false, fmt.Errorf("finish artist artwork scraping batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit artist artwork batch finish: %w", err)
	}
	return true, nil
}

func recoverExpiredArtistArtworkBatchItems(ctx context.Context, tx pgx.Tx, now time.Time) error {
	rows, err := tx.Query(ctx, `
		UPDATE artist_artwork_scraping_job_items item SET
			status = CASE WHEN job.cancel_requested THEN 'SKIPPED'::tag_scraping_item_status
			              ELSE 'FAILED'::tag_scraping_item_status END,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			message = CASE WHEN job.cancel_requested THEN 'The batch was cancelled'
			               ELSE 'Worker lease expired after the final attempt' END,
			completed_at = $1, updated_at = $1
		FROM artist_artwork_scraping_jobs job
		WHERE item.job_id = job.id AND item.status = 'RUNNING'
		  AND (item.locked_until IS NULL OR item.locked_until < $1)
		  AND item.attempts >= item.max_attempts
		RETURNING item.job_id`, now)
	if err != nil {
		return fmt.Errorf("fail exhausted artist artwork scraping leases: %w", err)
	}
	affected := make(map[string]struct{})
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			rows.Close()
			return fmt.Errorf("scan recovered artist artwork batch: %w", err)
		}
		affected[jobID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate recovered artist artwork batches: %w", err)
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_job_items SET
			status = 'PENDING', next_attempt_at = $1,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			started_at = NULL, updated_at = $1
		WHERE status = 'RUNNING' AND (locked_until IS NULL OR locked_until < $1)
		  AND attempts < max_attempts`, now); err != nil {
		return fmt.Errorf("recover expired artist artwork scraping items: %w", err)
	}
	for jobID := range affected {
		if err := recountArtistArtworkBatch(ctx, tx, jobID, now); err != nil {
			return err
		}
	}
	return nil
}

func recountArtistArtworkBatch(ctx context.Context, tx pgx.Tx, jobID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE artist_artwork_scraping_jobs job SET
			processed = counts.processed,
			succeeded = counts.succeeded,
			failed = counts.failed,
			updated_at = $2
		FROM (
			SELECT count(*) FILTER (WHERE status NOT IN ('PENDING', 'RUNNING'))::int AS processed,
			       count(*) FILTER (WHERE status = 'SUCCEEDED')::int AS succeeded,
			       count(*) FILTER (WHERE status = 'FAILED')::int AS failed
			FROM artist_artwork_scraping_job_items WHERE job_id = $1
		) counts WHERE job.id = $1`, jobID, now); err != nil {
		return fmt.Errorf("recount artist artwork scraping batch: %w", err)
	}
	return nil
}

func artistArtworkBatchTarget(ctx context.Context, tx pgx.Tx, artistID string) (ArtistArtworkBatchTarget, error) {
	var target ArtistArtworkBatchTarget
	err := tx.QueryRow(ctx, `
		SELECT artist.id, artist.name, artist.normalized_name, artist.version,
		       artist.artwork_asset_id IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM track_artists credit
		         WHERE credit.artist_id = artist.id AND credit.role IN ('PRIMARY', 'FEATURED')
		       ) OR EXISTS (
		         SELECT 1 FROM album_artists credit
		         WHERE credit.artist_id = artist.id AND credit.role IN ('PRIMARY', 'FEATURED')
		       )
		FROM artists artist WHERE artist.id = $1`, artistID).Scan(
		&target.ID, &target.Name, &target.NormalizedName, &target.Version,
		&target.HasArtwork, &target.PerformerRole,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ArtistArtworkBatchTarget{}, apperror.NotFound("Artist was not found")
	}
	if err != nil {
		return ArtistArtworkBatchTarget{}, fmt.Errorf("load artist artwork scraping target: %w", err)
	}
	return target, nil
}

func lockArtistArtworkBatchJob(
	ctx context.Context,
	tx pgx.Tx,
	jobID string,
) (string, bool, error) {
	var status string
	var cancelRequested bool
	err := tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested
		FROM artist_artwork_scraping_jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&status, &cancelRequested)
	if err != nil {
		return "", false, err
	}
	return status, cancelRequested, nil
}

func lockArtistArtworkBatchItemOwnership(
	ctx context.Context,
	tx pgx.Tx,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
) (bool, error) {
	var status string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err := tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM artist_artwork_scraping_job_items
		WHERE id = $1 AND job_id = $2 FOR UPDATE`, itemID, jobID).Scan(
		&status, &currentAttempt, &currentWorker, &leaseActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock artist artwork batch item ownership: %w", err)
	}
	return status == string(ItemRunning) && currentAttempt != nil && *currentAttempt == attemptID &&
		currentWorker != nil && *currentWorker == workerID && leaseActive, nil
}

func encodeArtistArtworkCandidate(candidate *ArtistCandidate) ([]byte, *string, error) {
	if candidate == nil {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return nil, nil, fmt.Errorf("encode artist artwork candidate: %w", err)
	}
	source := string(candidate.Source)
	return encoded, &source, nil
}

func truncateArtistArtworkBatchMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= 4_000 {
		return message
	}
	return string(runes[:4_000])
}

func artistArtworkScrapeNameEligible(target ArtistArtworkBatchTarget) bool {
	normalized := normalizeForTagMatch(target.NormalizedName)
	if normalized == "" {
		return false
	}
	switch normalized {
	case "", "unknown", "unknownartist", "未知", "未知艺术家":
		return false
	default:
		return true
	}
}

func scanArtistArtworkBatchJob(row rowScanner) (ArtistArtworkBatchJobRecord, error) {
	var result ArtistArtworkBatchJobRecord
	var optionsJSON []byte
	var status string
	if err := row.Scan(
		&result.ID, &result.RequestedBy, &optionsJSON, &status, &result.Total,
		&result.Processed, &result.Succeeded, &result.Failed, &result.CancelRequested,
		&result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		return ArtistArtworkBatchJobRecord{}, err
	}
	if err := json.Unmarshal(optionsJSON, &result.Options); err != nil {
		return ArtistArtworkBatchJobRecord{}, fmt.Errorf("decode artist artwork batch options: %w", err)
	}
	result.Status = JobStatus(status)
	return result, nil
}

func scanArtistArtworkBatchItem(row rowScanner) (ArtistArtworkBatchItemRecord, error) {
	var result ArtistArtworkBatchItemRecord
	var status string
	var candidateJSON []byte
	var source *string
	if err := row.Scan(
		&result.ID, &result.JobID, &result.ArtistID, &result.ExpectedVersion, &result.Position,
		&status, &result.Attempts, &result.MaxAttempts, &result.NextAttemptAt,
		&result.AttemptID, &result.LockedBy, &result.LockedUntil,
		&candidateJSON, &source, &result.Message, &result.StartedAt, &result.CompletedAt,
		&result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		return ArtistArtworkBatchItemRecord{}, err
	}
	result.Status = ItemStatus(status)
	if len(candidateJSON) > 0 {
		var candidate ArtistCandidate
		if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
			return ArtistArtworkBatchItemRecord{}, fmt.Errorf("decode artist artwork batch candidate: %w", err)
		}
		result.Candidate = &candidate
	}
	if source != nil {
		value := Source(*source)
		result.Source = &value
	}
	return result, nil
}

const artistArtworkBatchJobSelect = `
	SELECT id, requested_by, options, status::text, total, processed, succeeded, failed,
	       cancel_requested, started_at, completed_at, created_at, updated_at
	FROM artist_artwork_scraping_jobs`

const artistArtworkBatchItemSelect = `
	SELECT id, job_id, artist_id, expected_version, position, status::text,
	       attempts, max_attempts, next_attempt_at, attempt_id, locked_by, locked_until,
	       candidate, source, message, started_at, completed_at, created_at, updated_at
	FROM artist_artwork_scraping_job_items`
