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
	"xymusic/server/internal/shared/tagwriteback"
)

func (repository *Repository) ValidateBatchWriteback(ctx context.Context, items []BatchItemInput) error {
	trackIDs := make([]string, 0, len(items))
	for _, item := range items {
		trackIDs = append(trackIDs, item.TrackID)
	}
	rows, err := repository.pool.Query(ctx, `
		WITH requested AS (
			SELECT track_id, position
			FROM unnest($1::uuid[]) WITH ORDINALITY input(track_id, position)
		), source_stats AS (
			SELECT mapping.source_id, count(*)::int AS mapping_count
			FROM local_music_source_tracks mapping
			WHERE mapping.source_id IN (
				SELECT metadata.source_id
				FROM track_metadata metadata
				JOIN requested ON requested.track_id = metadata.track_id
				WHERE metadata.source_id IS NOT NULL
			)
			GROUP BY mapping.source_id
		)
		SELECT requested.track_id::text, track.status::text,
		       source.id::text, source.source_path, source.status,
		       root.mode::text, root.enabled, EXISTS (
		         SELECT 1 FROM library_scan_runs active_scan
		         WHERE active_scan.root_id = root.id
		           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
		       ),
		       COALESCE(source_stats.mapping_count, 0)
		FROM requested
		LEFT JOIN tracks track ON track.id = requested.track_id
		LEFT JOIN track_metadata metadata ON metadata.track_id = requested.track_id
		LEFT JOIN local_music_sources source ON source.id = metadata.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN source_stats ON source_stats.source_id = source.id
		ORDER BY requested.position`, trackIDs)

	if err != nil {
		return fmt.Errorf("validate batch Tag writeback sources: %w", err)
	}
	defer rows.Close()
	var firstIneligible error
	writableCount := 0
	for rows.Next() {
		var trackID string
		var trackStatus, sourceID, sourcePath, sourceStatus, rootMode *string
		var rootEnabled *bool
		var scanActive bool
		var mappingCount *int
		if err := rows.Scan(
			&trackID, &trackStatus, &sourceID, &sourcePath, &sourceStatus,
			&rootMode, &rootEnabled, &scanActive, &mappingCount,
		); err != nil {
			return fmt.Errorf("scan batch Tag writeback source: %w", err)
		}
		if trackStatus == nil {
			return apperror.New(
				apperror.CodeResourceNotFound,
				"A selected track was not found",
				apperror.WithMetadata(map[string]any{"trackId": trackID}),
			)
		}
		eligibility := tagwriteback.Evaluate(tagwriteback.SourceContext{
			HasSource: sourceID != nil, TrackStatus: pointerValue(trackStatus),
			RootMode: pointerValue(rootMode), RootEnabled: boolPointerValue(rootEnabled),
			ScanActive: scanActive, SourceStatus: pointerValue(sourceStatus),
			SourcePath: pointerValue(sourcePath), MappingCount: intPointerValue(mappingCount),
		})
		if eligibility.CanWriteBack {
			writableCount++
			continue
		}
		if firstIneligible == nil {
			firstIneligible = eligibility.Error(trackID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate batch Tag writeback sources: %w", err)
	}
	if writableCount == 0 && firstIneligible != nil {
		return firstIneligible
	}
	return nil
}

func (repository *Repository) CreateBatch(ctx context.Context, actorID string, input CreateBatchInput) (string, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin tag scraping batch: %w", err)
	}
	defer tx.Rollback(ctx)
	jobID := uuid.NewString()
	optionsJSON, _ := json.Marshal(input.Options)
	if _, err := tx.Exec(ctx, `
		INSERT INTO tag_scraping_jobs (id, requested_by, options, total)
		VALUES ($1, $2, $3::jsonb, $4)`, jobID, actorID, optionsJSON, len(input.Items)); err != nil {
		return "", fmt.Errorf("insert tag scraping batch: %w", err)
	}
	itemIDs := make([]string, len(input.Items))
	trackIDs := make([]string, len(input.Items))
	expectedVersions := make([]int, len(input.Items))
	positions := make([]int, len(input.Items))
	for position, item := range input.Items {
		itemIDs[position] = uuid.NewString()
		trackIDs[position] = item.TrackID
		expectedVersions[position] = item.ExpectedVersion
		positions[position] = position
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tag_scraping_job_items (id, job_id, track_id, expected_version, position)
		SELECT input.item_id, $5, input.track_id, input.expected_version, input.position
		FROM unnest($1::uuid[], $2::uuid[], $3::int[], $4::int[])
			AS input(item_id, track_id, expected_version, position)`,
		itemIDs, trackIDs, expectedVersions, positions, jobID); err != nil {
		return "", fmt.Errorf("insert tag scraping batch items: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit tag scraping batch: %w", err)
	}
	return jobID, nil
}

func (repository *Repository) Batch(ctx context.Context, jobID string, updatedAfter *time.Time) (BatchJobRecord, []BatchItemRecord, error) {
	job, err := scanBatchJob(repository.pool.QueryRow(ctx, batchJobSelect+" WHERE id = $1", jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchJobRecord{}, nil, apperror.NotFound("Tag scraping batch was not found")
	}
	if err != nil {
		return BatchJobRecord{}, nil, fmt.Errorf("find tag scraping batch: %w", err)
	}
	query := batchItemSelect + " WHERE job_id = $1"
	arguments := []any{jobID}
	if updatedAfter != nil {
		query += " AND updated_at > $2"
		arguments = append(arguments, *updatedAfter)
	}
	query += " ORDER BY position"
	rows, err := repository.pool.Query(ctx, query, arguments...)
	if err != nil {
		return BatchJobRecord{}, nil, fmt.Errorf("query tag scraping batch items: %w", err)
	}
	defer rows.Close()
	items := make([]BatchItemRecord, 0)
	for rows.Next() {
		item, scanErr := scanBatchItem(rows)
		if scanErr != nil {
			return BatchJobRecord{}, nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return BatchJobRecord{}, nil, fmt.Errorf("iterate tag scraping batch items: %w", err)
	}
	return job, items, nil
}

func (repository *Repository) RequestBatchCancel(ctx context.Context, jobID string) error {
	var updated string
	err := repository.pool.QueryRow(ctx, `
		UPDATE tag_scraping_jobs SET cancel_requested = true, updated_at = now()
		WHERE id = $1 AND status IN ('PENDING', 'RUNNING') RETURNING id`, jobID).Scan(&updated)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("cancel tag scraping batch: %w", err)
	}
	var exists bool
	if lookupErr := repository.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tag_scraping_jobs WHERE id = $1)", jobID).Scan(&exists); lookupErr != nil {
		return fmt.Errorf("check tag scraping batch: %w", lookupErr)
	}
	if !exists {
		return apperror.NotFound("Tag scraping batch was not found")
	}
	return apperror.Conflict(apperror.CodeInvalidStateTransition, "The batch has already finished and cannot be cancelled", nil)
}

func (repository *Repository) RetryBatch(ctx context.Context, jobID string) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tag scraping retry: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, "SELECT status::text FROM tag_scraping_jobs WHERE id = $1 FOR UPDATE", jobID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Tag scraping batch was not found")
	}
	if err != nil {
		return fmt.Errorf("lock tag scraping batch: %w", err)
	}
	if status != string(JobFailed) && status != string(JobCompleted) {
		return apperror.Conflict(apperror.CodeInvalidStateTransition, "Only finished batches can retry failed items", nil)
	}
	command, err := tx.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'PENDING', attempts = 0, next_attempt_at = now(),
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = NULL, source = NULL, message = NULL, started_at = NULL,
			completed_at = NULL, updated_at = now()
		WHERE job_id = $1 AND status = 'FAILED'`, jobID)
	if err != nil {
		return fmt.Errorf("reset failed tag scraping items: %w", err)
	}
	if command.RowsAffected() == 0 {
		return apperror.Conflict(apperror.CodeResourceConflict, "The batch has no failed items to retry", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tag_scraping_jobs job SET
			status = 'PENDING', cancel_requested = false,
			processed = counts.processed, succeeded = counts.succeeded, failed = counts.failed,
			completed_at = NULL, updated_at = now()
		FROM (
			SELECT count(*) FILTER (WHERE status NOT IN ('PENDING','RUNNING'))::int AS processed,
			       count(*) FILTER (WHERE status = 'SUCCEEDED')::int AS succeeded,
			       count(*) FILTER (WHERE status = 'FAILED')::int AS failed
			FROM tag_scraping_job_items WHERE job_id = $1
		) counts WHERE job.id = $1`, jobID); err != nil {
		return fmt.Errorf("recount retried tag scraping batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tag scraping retry: %w", err)
	}
	return nil
}

func (repository *Repository) RecoverExpiredBatchItems(ctx context.Context, now time.Time) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tag scraping recovery: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := recoverExpiredBatchItems(ctx, tx, now, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tag scraping recovery: %w", err)
	}
	return nil
}

func recoverExpiredBatchItems(ctx context.Context, tx pgx.Tx, now time.Time, onlyJobID *string) error {
	query := `
		UPDATE tag_scraping_job_items item SET
			status = CASE
				WHEN job.cancel_requested THEN 'SKIPPED'::tag_scraping_item_status
				WHEN item.attempts >= item.max_attempts THEN 'FAILED'::tag_scraping_item_status
				ELSE 'PENDING'::tag_scraping_item_status
			END,
			next_attempt_at = $1,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			started_at = NULL,
			completed_at = CASE
				WHEN job.cancel_requested OR item.attempts >= item.max_attempts THEN $1
				ELSE NULL
			END,
			message = CASE
				WHEN job.cancel_requested THEN 'The batch was cancelled'
				WHEN item.attempts >= item.max_attempts THEN 'Tag scraping attempts exhausted'
				ELSE item.message
			END,
			updated_at = $1
		FROM tag_scraping_jobs job
		WHERE item.job_id = job.id
		  AND job.status IN ('PENDING', 'RUNNING')
		  AND (
			(item.status = 'RUNNING' AND (item.locked_until IS NULL OR item.locked_until < $1))
			OR (item.status = 'PENDING' AND item.attempts >= item.max_attempts)
		  )
	`
	arguments := []any{now}
	if onlyJobID != nil {
		query += " AND item.job_id = $2"
		arguments = append(arguments, *onlyJobID)
	}
	query += " RETURNING item.job_id::text"
	rows, err := tx.Query(ctx, query, arguments...)
	if err != nil {
		return fmt.Errorf("recover expired tag scraping items: %w", err)
	}
	affectedJobs := make(map[string]struct{})
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			rows.Close()
			return fmt.Errorf("scan recovered tag scraping job: %w", err)
		}
		affectedJobs[jobID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate recovered tag scraping jobs: %w", err)
	}
	rows.Close()
	for jobID := range affectedJobs {
		if err := recountBatch(ctx, tx, jobID, now); err != nil {
			return err
		}
	}
	return nil
}

func recountBatch(ctx context.Context, tx pgx.Tx, jobID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE tag_scraping_jobs job SET
			processed = counts.processed,
			succeeded = counts.succeeded,
			failed = counts.failed,
			updated_at = $2
		FROM (
			SELECT count(*) FILTER (WHERE status NOT IN ('PENDING', 'RUNNING'))::int AS processed,
			       count(*) FILTER (WHERE status = 'SUCCEEDED')::int AS succeeded,
			       count(*) FILTER (WHERE status = 'FAILED')::int AS failed
			FROM tag_scraping_job_items WHERE job_id = $1
		) counts
		WHERE job.id = $1`, jobID, now); err != nil {
		return fmt.Errorf("recount recovered tag scraping batch: %w", err)
	}
	return nil
}
