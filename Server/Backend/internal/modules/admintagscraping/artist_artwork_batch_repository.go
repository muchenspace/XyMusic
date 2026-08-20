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
	itemIDs := make([]string, len(selected))
	selectedArtistIDs := make([]string, len(selected))
	expectedVersions := make([]int, len(selected))
	positions := make([]int, len(selected))
	maxAttemptsByItem := make([]int, len(selected))
	for position, item := range selected {
		itemIDs[position] = uuid.NewString()
		selectedArtistIDs[position] = item.ArtistID
		expectedVersions[position] = item.ExpectedVersion
		positions[position] = position
		maxAttemptsByItem[position] = maxAttempts
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO artist_artwork_scraping_job_items (
			id, job_id, artist_id, expected_version, position, max_attempts
		)
		SELECT input.item_id, $6, input.artist_id, input.expected_version,
		       input.position, input.max_attempts
		FROM unnest($1::uuid[], $2::uuid[], $3::int[], $4::int[], $5::int[])
			AS input(item_id, artist_id, expected_version, position, max_attempts)`,
		itemIDs, selectedArtistIDs, expectedVersions, positions, maxAttemptsByItem, jobID); err != nil {
		return "", 0, 0, fmt.Errorf("insert artist artwork scraping batch items: %w", err)
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
	result, err := repository.ClaimArtistArtworkBatchItems(ctx, workerID, now, lease, 1)
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, err
	}
	if result.FinishJobID != "" || len(result.Items) == 0 {
		return ArtistArtworkBatchClaimResult{FinishJobID: result.FinishJobID}, nil
	}
	item := result.Items[0]
	return ArtistArtworkBatchClaimResult{Item: &item}, nil
}

func (repository *Repository) ClaimArtistArtworkBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (ArtistArtworkBatchClaimResult, error) {
	return repository.claimArtistArtworkBatchItems(ctx, workerID, now, lease, limit)
}

func (repository *Repository) claimArtistArtworkBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (ArtistArtworkBatchClaimResult, error) {
	if limit <= 0 {
		return ArtistArtworkBatchClaimResult{}, nil
	}
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
	rows, err := tx.Query(ctx, artistArtworkBatchItemSelect+`
		WHERE job_id = $1 AND status = 'PENDING'
		  AND next_attempt_at <= $2 AND attempts < max_attempts
		ORDER BY position FOR UPDATE SKIP LOCKED LIMIT $3`, job.ID, now, limit)
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("query artist artwork scraping items: %w", err)
	}
	items := make([]ArtistArtworkBatchItemRecord, 0, limit)
	for rows.Next() {
		item, scanErr := scanArtistArtworkBatchItem(rows)
		if scanErr != nil {
			rows.Close()
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("scan artist artwork scraping item: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("iterate artist artwork scraping items: %w", err)
	}
	rows.Close()
	if len(items) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("commit empty artist artwork item claim: %w", err)
		}
		return ArtistArtworkBatchClaimResult{FinishJobID: job.ID}, nil
	}
	itemIDs := make([]string, len(items))
	attemptIDs := make([]string, len(items))
	for index := range items {
		itemIDs[index] = items[index].ID
		attemptIDs[index] = uuid.NewString()
	}
	lockedUntil := now.Add(lease)
	attemptRows, err := tx.Query(ctx, `
		WITH requested AS (
			SELECT item_id, attempt_id
			FROM unnest($2::uuid[], $3::uuid[]) AS input(item_id, attempt_id)
		)
		UPDATE artist_artwork_scraping_job_items AS item SET
			status = 'RUNNING', attempts = item.attempts + 1,
			attempt_id = requested.attempt_id, locked_by = $4, locked_until = $5,
			started_at = $6, completed_at = NULL, updated_at = $6
		FROM requested
		WHERE item.id = requested.item_id AND item.job_id = $1
		  AND item.status = 'PENDING' AND item.attempts < item.max_attempts
		RETURNING item.id::text, item.attempt_id::text`,
		job.ID, itemIDs, attemptIDs, workerID, lockedUntil, now,
	)
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("own artist artwork scraping items: %w", err)
	}
	claimedAttempts := make(map[string]string, len(items))
	for attemptRows.Next() {
		var itemID, attemptID string
		if err := attemptRows.Scan(&itemID, &attemptID); err != nil {
			attemptRows.Close()
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("scan artist artwork scraping attempt: %w", err)
		}
		claimedAttempts[itemID] = attemptID
	}
	if err := attemptRows.Err(); err != nil {
		attemptRows.Close()
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("iterate artist artwork scraping attempts: %w", err)
	}
	attemptRows.Close()
	if len(claimedAttempts) != len(items) {
		return ArtistArtworkBatchClaimResult{}, errors.New("claimed artist artwork items disappeared")
	}
	artistIDs := make([]string, len(items))
	for index := range items {
		artistIDs[index] = items[index].ArtistID
	}
	targetRows, err := tx.Query(ctx, `
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
		ORDER BY artist.id`, artistIDs)
	if err != nil {
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("load artist artwork scraping targets: %w", err)
	}
	targets := make(map[string]ArtistArtworkBatchTarget, len(items))
	for targetRows.Next() {
		var target ArtistArtworkBatchTarget
		if err := targetRows.Scan(
			&target.ID, &target.Name, &target.NormalizedName, &target.Version,
			&target.HasArtwork, &target.PerformerRole,
		); err != nil {
			targetRows.Close()
			return ArtistArtworkBatchClaimResult{}, fmt.Errorf("scan artist artwork scraping target: %w", err)
		}
		targets[target.ID] = target
	}
	if err := targetRows.Err(); err != nil {
		targetRows.Close()
		return ArtistArtworkBatchClaimResult{}, fmt.Errorf("iterate artist artwork scraping targets: %w", err)
	}
	targetRows.Close()
	if len(targets) != len(items) {
		return ArtistArtworkBatchClaimResult{}, errors.New("artist artwork scraping target disappeared")
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
	claims := make([]ClaimedArtistArtworkBatchItem, len(items))
	for index := range items {
		item := items[index]
		attemptID := claimedAttempts[item.ID]
		itemLockedUntil := lockedUntil
		itemStartedAt := now
		item.Status = ItemRunning
		item.Attempts++
		item.AttemptID = &attemptID
		item.LockedBy = &workerID
		item.LockedUntil = &itemLockedUntil
		item.StartedAt = &itemStartedAt
		claims[index] = ClaimedArtistArtworkBatchItem{
			Job: job, Item: item, Target: targets[item.ArtistID], AttemptID: attemptID,
		}
	}
	return ArtistArtworkBatchClaimResult{Items: claims}, nil
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

type artistArtworkBatchCompletionPayload struct {
	ItemID    string          `json:"item_id"`
	AttemptID string          `json:"attempt_id"`
	Status    ItemStatus      `json:"status"`
	Candidate json.RawMessage `json:"candidate"`
	Source    *string         `json:"source"`
	Message   string          `json:"message"`
}

// CompleteArtistArtworkBatchItems keeps one job lock and one transaction for
// a completion window. Lease and attempt predicates still isolate stale item
// results, so a lost item does not invalidate the rest of the window.
func (repository *Repository) CompleteArtistArtworkBatchItems(
	ctx context.Context,
	jobID string,
	workerID string,
	completions []ArtistArtworkBatchItemCompletion,
	now time.Time,
) ([]string, error) {
	if len(completions) == 0 {
		return nil, nil
	}
	payload := make([]artistArtworkBatchCompletionPayload, len(completions))
	seenItems := make(map[string]struct{}, len(completions))
	for index, completion := range completions {
		if completion.ItemID == "" || completion.AttemptID == "" {
			return nil, errors.New("artist artwork batch completion is missing an item or attempt")
		}
		if _, exists := seenItems[completion.ItemID]; exists {
			return nil, fmt.Errorf("artist artwork batch completion contains duplicate item %q", completion.ItemID)
		}
		seenItems[completion.ItemID] = struct{}{}
		if completion.Status != ItemSucceeded && completion.Status != ItemFailed && completion.Status != ItemSkipped {
			return nil, errors.New("artist artwork batch item completion status is invalid")
		}
		candidateJSON, source, err := encodeArtistArtworkCandidate(completion.Candidate)
		if err != nil {
			return nil, err
		}
		payload[index] = artistArtworkBatchCompletionPayload{
			ItemID: completion.ItemID, AttemptID: completion.AttemptID, Status: completion.Status,
			Candidate: json.RawMessage(candidateJSON), Source: source,
			Message: truncateArtistArtworkBatchMessage(completion.Message),
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode artist artwork batch completion: %w", err)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin artist artwork batch completion: %w", err)
	}
	defer tx.Rollback(ctx)
	jobStatus, cancelRequested, err := lockArtistArtworkBatchJob(ctx, tx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrArtistArtworkBatchLeaseLost
	}
	if err != nil {
		return nil, err
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return nil, ErrArtistArtworkBatchLeaseLost
	}
	rows, err := tx.Query(ctx, `
		WITH input AS (
			SELECT item_id,attempt_id,status,candidate,source,message
			FROM jsonb_to_recordset($3::jsonb) AS input(
				item_id uuid,attempt_id uuid,status text,candidate jsonb,source text,message text
			)
		)
		UPDATE artist_artwork_scraping_job_items item SET
			status = CASE WHEN $4 AND input.status <> 'SUCCEEDED'
				THEN 'SKIPPED'::tag_scraping_item_status
				ELSE input.status::tag_scraping_item_status END,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = CASE WHEN $4 AND input.status <> 'SUCCEEDED' THEN NULL::jsonb ELSE input.candidate END,
			source = CASE WHEN $4 AND input.status <> 'SUCCEEDED' THEN NULL::varchar ELSE input.source END,
			message = CASE WHEN $4 AND input.status <> 'SUCCEEDED' THEN 'The batch was cancelled' ELSE LEFT(input.message, 4000) END,
			completed_at = $5, updated_at = $5
		FROM input
		WHERE item.id = input.item_id AND item.job_id = $1
			AND item.status = 'RUNNING'
			AND item.attempt_id = input.attempt_id
			AND item.locked_by = $2
			AND COALESCE(item.locked_until > clock_timestamp(), false)
		RETURNING item.id::text,item.status::text`, jobID, workerID, encoded, cancelRequested, now)
	if err != nil {
		return nil, fmt.Errorf("complete artist artwork batch items: %w", err)
	}
	completedIDs := make([]string, 0, len(completions))
	succeeded, failed := 0, 0
	for rows.Next() {
		var itemID, status string
		if err := rows.Scan(&itemID, &status); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan artist artwork batch completion: %w", err)
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
		return nil, fmt.Errorf("iterate artist artwork batch completion: %w", err)
	}
	rows.Close()
	if len(completedIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE artist_artwork_scraping_jobs SET
				processed = processed + $2,
				succeeded = succeeded + $3,
				failed = failed + $4,
				updated_at = $5
			WHERE id = $1`, jobID, len(completedIDs), succeeded, failed, now); err != nil {
			return nil, fmt.Errorf("update artist artwork batch counts: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit artist artwork batch completion: %w", err)
	}
	return completedIDs, nil
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
