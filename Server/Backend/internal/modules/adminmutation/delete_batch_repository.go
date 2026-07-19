package adminmutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

const deleteBatchJobColumns = `id,requested_by,trace_id,status::text,total,processed,succeeded,failed,
	started_at,completed_at,created_at,updated_at`

const deleteBatchItemColumns = `id,job_id,track_id,expected_version,position,status::text,attempts,
	next_attempt_at,attempt_id,locked_by,locked_until,heartbeat_at,deleted_files,quarantined_files,
	scheduled_objects,error_code,message,started_at,completed_at,created_at,updated_at`

func (repository *Repository) CreatePermanentDeleteBatch(
	ctx context.Context,
	actorID string,
	traceID string,
	input []BatchTrackItemInput,
) (PermanentDeleteBatchRecord, []PermanentDeleteBatchItemRecord, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("begin permanent delete batch creation: %w", err)
	}
	defer tx.Rollback(ctx)
	ordered := append([]BatchTrackItemInput(nil), input...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].TrackID < ordered[right].TrackID })
	for _, item := range ordered {
		state, err := lockTrackMutationState(ctx, tx, item.TrackID)
		if err != nil {
			return PermanentDeleteBatchRecord{}, nil, err
		}
		if state.Version != item.ExpectedVersion {
			return PermanentDeleteBatchRecord{}, nil, versionConflict("Track", item.ExpectedVersion, state.Version, map[string]any{"trackId": item.TrackID})
		}
		if state.Status != "ARCHIVED" {
			return PermanentDeleteBatchRecord{}, nil, apperror.New(
				apperror.CodeInvalidStateTransition,
				"Track must be in the recycle bin before permanent deletion",
				apperror.WithMetadata(map[string]any{"trackId": item.TrackID}),
			)
		}
	}
	jobID := uuid.NewString()
	job, err := scanPermanentDeleteBatch(tx.QueryRow(ctx, `INSERT INTO track_delete_batches(
		id,requested_by,trace_id,total
	) VALUES($1,$2,$3,$4) RETURNING `+deleteBatchJobColumns,
		jobID, actorID, traceID, len(input)))
	if err != nil {
		return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("create permanent delete batch: %w", err)
	}
	items := make([]PermanentDeleteBatchItemRecord, 0, len(input))
	for position, item := range input {
		stored, err := scanPermanentDeleteBatchItem(tx.QueryRow(ctx, `INSERT INTO track_delete_batch_items(
			job_id,track_id,expected_version,position
		) VALUES($1,$2,$3,$4) RETURNING `+deleteBatchItemColumns,
			jobID, item.TrackID, item.ExpectedVersion, position))
		if err != nil {
			return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("create permanent delete batch item: %w", err)
		}
		items = append(items, stored)
	}
	if err := tx.Commit(ctx); err != nil {
		return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("commit permanent delete batch creation: %w", err)
	}
	return job, items, nil
}

func (repository *Repository) FindPermanentDeleteBatch(
	ctx context.Context,
	jobID string,
) (PermanentDeleteBatchRecord, []PermanentDeleteBatchItemRecord, error) {
	job, err := scanPermanentDeleteBatch(repository.pool.QueryRow(ctx, `SELECT `+deleteBatchJobColumns+`
		FROM track_delete_batches WHERE id=$1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PermanentDeleteBatchRecord{}, nil, apperror.NotFound("Permanent delete batch was not found")
	}
	if err != nil {
		return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("find permanent delete batch: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `SELECT `+deleteBatchItemColumns+`
		FROM track_delete_batch_items WHERE job_id=$1 ORDER BY position,id`, jobID)
	if err != nil {
		return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("list permanent delete batch items: %w", err)
	}
	defer rows.Close()
	items := make([]PermanentDeleteBatchItemRecord, 0, job.Total)
	for rows.Next() {
		item, err := scanPermanentDeleteBatchItem(rows)
		if err != nil {
			return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("scan permanent delete batch item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return PermanentDeleteBatchRecord{}, nil, fmt.Errorf("iterate permanent delete batch items: %w", err)
	}
	return job, items, nil
}

func (repository *Repository) InitializePermanentDeleteBatches(ctx context.Context, now time.Time) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin permanent delete batch recovery: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE track_delete_batch_items SET
		status='PENDING',attempt_id=NULL,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		next_attempt_at=$1,message='The previous deletion attempt stopped before completion',updated_at=$1
		WHERE status='RUNNING' AND (locked_until IS NULL OR locked_until<$1)`, now); err != nil {
		return fmt.Errorf("recover permanent delete batch items: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit permanent delete batch recovery: %w", err)
	}
	return nil
}

func (repository *Repository) ClaimPermanentDeleteBatchItem(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (*ClaimedPermanentDeleteItem, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin permanent delete item claim: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE track_delete_batch_items SET
		status='PENDING',attempt_id=NULL,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		next_attempt_at=$1,message='The previous deletion attempt stopped before completion',updated_at=$1
		WHERE status='RUNNING' AND locked_until<$1`, now); err != nil {
		return nil, fmt.Errorf("recover expired permanent delete item leases: %w", err)
	}
	var jobID string
	err = tx.QueryRow(ctx, `SELECT job.id FROM track_delete_batches job
		WHERE job.status IN ('PENDING','RUNNING')
		AND NOT EXISTS(SELECT 1 FROM track_delete_batch_items active
			WHERE active.job_id=job.id AND active.status='RUNNING')
		AND (SELECT pending.next_attempt_at FROM track_delete_batch_items pending
			WHERE pending.job_id=job.id AND pending.status='PENDING'
			ORDER BY pending.position,pending.id LIMIT 1) <= $1
		ORDER BY job.created_at,job.id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty permanent delete item claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select permanent delete batch claim: %w", err)
	}
	var itemID string
	if err := tx.QueryRow(ctx, `SELECT id FROM track_delete_batch_items
		WHERE job_id=$1 AND status='PENDING' ORDER BY position,id LIMIT 1 FOR UPDATE`, jobID).Scan(&itemID); err != nil {
		return nil, fmt.Errorf("select permanent delete item claim: %w", err)
	}
	attemptID := uuid.NewString()
	item, err := scanPermanentDeleteBatchItem(tx.QueryRow(ctx, `UPDATE track_delete_batch_items SET
		status='RUNNING',attempts=attempts+1,attempt_id=$2,locked_by=$3,locked_until=$4,
		heartbeat_at=$1,started_at=COALESCE(started_at,$1),error_code=NULL,message=NULL,updated_at=$1
		WHERE id=$5 AND status='PENDING' RETURNING `+deleteBatchItemColumns,
		now, attemptID, workerID, now.Add(lease), itemID))
	if err != nil {
		return nil, fmt.Errorf("claim permanent delete item: %w", err)
	}
	job, err := scanPermanentDeleteBatch(tx.QueryRow(ctx, `UPDATE track_delete_batches SET
		status='RUNNING',started_at=COALESCE(started_at,$2),updated_at=$2
		WHERE id=$1 RETURNING `+deleteBatchJobColumns, jobID, now))
	if err != nil {
		return nil, fmt.Errorf("start permanent delete batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit permanent delete item claim: %w", err)
	}
	return &ClaimedPermanentDeleteItem{Job: job, Item: item}, nil
}

func (repository *Repository) RenewPermanentDeleteBatchItem(
	ctx context.Context,
	itemID string,
	attemptID string,
	workerID string,
	heartbeatAt time.Time,
	lockedUntil time.Time,
) (bool, error) {
	command, err := repository.pool.Exec(ctx, `UPDATE track_delete_batch_items SET
		heartbeat_at=$4,locked_until=$5,updated_at=$4
		WHERE id=$1 AND attempt_id=$2 AND locked_by=$3 AND status='RUNNING'`,
		itemID, attemptID, workerID, heartbeatAt, lockedUntil)
	if err != nil {
		return false, fmt.Errorf("renew permanent delete item lease: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (repository *Repository) RetryPermanentDeleteBatchItem(
	ctx context.Context,
	itemID string,
	attemptID string,
	workerID string,
	errorCode string,
	message string,
	nextAttemptAt time.Time,
	now time.Time,
) error {
	command, err := repository.pool.Exec(ctx, `UPDATE track_delete_batch_items SET
		status='PENDING',attempt_id=NULL,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		error_code=$4,message=$5,next_attempt_at=$6,updated_at=$7
		WHERE id=$1 AND attempt_id=$2 AND locked_by=$3 AND status='RUNNING'`,
		itemID, attemptID, workerID, errorCode, message, nextAttemptAt, now)
	if err != nil {
		return fmt.Errorf("retry permanent delete batch item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrPermanentDeleteLeaseLost
	}
	return nil
}

func (repository *Repository) ReleasePermanentDeleteBatchItem(
	ctx context.Context,
	itemID string,
	attemptID string,
	workerID string,
	now time.Time,
) error {
	command, err := repository.pool.Exec(ctx, `UPDATE track_delete_batch_items SET
		status='PENDING',attempt_id=NULL,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		next_attempt_at=$4,updated_at=$4
		WHERE id=$1 AND attempt_id=$2 AND locked_by=$3 AND status='RUNNING'`,
		itemID, attemptID, workerID, now)
	if err != nil {
		return fmt.Errorf("release permanent delete batch item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrPermanentDeleteLeaseLost
	}
	return nil
}

func (repository *Repository) CompletePermanentDeleteBatchItemSuccess(
	ctx context.Context,
	claim ClaimedPermanentDeleteItem,
	workerID string,
	result DeleteResult,
	message *string,
	now time.Time,
) error {
	return repository.completePermanentDeleteBatchItem(
		ctx, claim, workerID, DeleteBatchItemSucceeded, result, "", message, now,
	)
}

func (repository *Repository) CompletePermanentDeleteBatchItemFailure(
	ctx context.Context,
	claim ClaimedPermanentDeleteItem,
	workerID string,
	errorCode string,
	message string,
	now time.Time,
) error {
	return repository.completePermanentDeleteBatchItem(
		ctx, claim, workerID, DeleteBatchItemFailed, DeleteResult{}, errorCode, &message, now,
	)
}

func (repository *Repository) completePermanentDeleteBatchItem(
	ctx context.Context,
	claim ClaimedPermanentDeleteItem,
	workerID string,
	status DeleteBatchItemStatus,
	result DeleteResult,
	errorCode string,
	message *string,
	now time.Time,
) error {
	if claim.Item.AttemptID == nil {
		return ErrPermanentDeleteLeaseLost
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin permanent delete item completion: %w", err)
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `UPDATE track_delete_batch_items SET
		status=$4::track_delete_batch_item_status,attempt_id=NULL,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		deleted_files=$5,quarantined_files=$6,scheduled_objects=$7,error_code=$8,message=$9,
		completed_at=$10,updated_at=$10
		WHERE id=$1 AND attempt_id=$2 AND locked_by=$3 AND status='RUNNING'`,
		claim.Item.ID, *claim.Item.AttemptID, workerID, status,
		result.DeletedFiles, result.QuarantinedFiles, result.ScheduledObjects,
		nullableBatchText(errorCode), message, now)
	if err != nil {
		return fmt.Errorf("complete permanent delete batch item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrPermanentDeleteLeaseLost
	}
	succeededIncrement, failedIncrement := 0, 0
	auditResult := "SUCCESS"
	if status == DeleteBatchItemSucceeded {
		succeededIncrement = 1
	} else {
		failedIncrement = 1
		auditResult = "FAILURE"
	}
	if _, err := tx.Exec(ctx, `UPDATE track_delete_batches SET
		processed=processed+1,succeeded=succeeded+$2,failed=failed+$3,
		status=CASE WHEN processed+1=total THEN
			CASE WHEN failed+$3>0 THEN 'FAILED'::track_delete_batch_status
			ELSE 'COMPLETED'::track_delete_batch_status END
			ELSE 'RUNNING'::track_delete_batch_status END,
		completed_at=CASE WHEN processed+1=total THEN $4 ELSE completed_at END,updated_at=$4
		WHERE id=$1`, claim.Job.ID, succeededIncrement, failedIncrement, now); err != nil {
		return fmt.Errorf("update permanent delete batch counts: %w", err)
	}
	details := map[string]any{
		"batchId": claim.Job.ID, "batchSize": claim.Job.Total,
		"deletedFiles": result.DeletedFiles, "quarantinedFiles": result.QuarantinedFiles,
		"scheduledObjects": result.ScheduledObjects,
	}
	if errorCode != "" {
		details["errorCode"] = errorCode
	}
	if message != nil {
		details["message"] = *message
	}
	if err := insertPermanentDeleteBatchAudit(
		ctx, tx, claim.Job.RequestedBy, claim.Job.TraceID, claim.Item.TrackID, auditResult, details,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit permanent delete item completion: %w", err)
	}
	return nil
}

func insertPermanentDeleteBatchAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID *string,
	traceID string,
	trackID string,
	result string,
	details map[string]any,
) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode permanent delete batch audit: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs(
		actor_id,action,target_type,target_id,result,trace_id,details
	) VALUES($1,'admin.track.delete_permanently','track',$2,$3::audit_result,$4,$5::jsonb)`,
		actorID, trackID, result, traceID, encoded); err != nil {
		return fmt.Errorf("write permanent delete batch audit: %w", err)
	}
	return nil
}

func scanPermanentDeleteBatch(row pgx.Row) (PermanentDeleteBatchRecord, error) {
	var record PermanentDeleteBatchRecord
	err := row.Scan(
		&record.ID, &record.RequestedBy, &record.TraceID, &record.Status,
		&record.Total, &record.Processed, &record.Succeeded, &record.Failed,
		&record.StartedAt, &record.CompletedAt, &record.CreatedAt, &record.UpdatedAt,
	)
	return record, err
}

func scanPermanentDeleteBatchItem(row interface{ Scan(...any) error }) (PermanentDeleteBatchItemRecord, error) {
	var record PermanentDeleteBatchItemRecord
	err := row.Scan(
		&record.ID, &record.JobID, &record.TrackID, &record.ExpectedVersion, &record.Position,
		&record.Status, &record.Attempts, &record.NextAttemptAt, &record.AttemptID,
		&record.LockedBy, &record.LockedUntil, &record.HeartbeatAt,
		&record.DeletedFiles, &record.QuarantinedFiles, &record.ScheduledObjects,
		&record.ErrorCode, &record.Message, &record.StartedAt, &record.CompletedAt,
		&record.CreatedAt, &record.UpdatedAt,
	)
	return record, err
}

func nullableBatchText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
