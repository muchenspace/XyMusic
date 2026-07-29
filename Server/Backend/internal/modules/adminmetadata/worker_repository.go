package adminmetadata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

type expiredWritebackAudit struct {
	ID       string
	TrackID  string
	ActorID  *string
	Attempts int
}

func (repository *Repository) ClaimWriteback(
	ctx context.Context,
	workerID string,
	lease time.Duration,
) (*WritebackJob, error) {
	if lease <= 0 {
		return nil, errors.New("metadata writeback lease must be positive")
	}
	leaseMicroseconds := lease.Microseconds()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin metadata writeback claim: %w", err)
	}
	defer tx.Rollback(ctx)

	cancelled, err := tx.Query(ctx, `
		update metadata_writeback_jobs set
			status = 'CANCELLED', locked_by = null, locked_until = null,
			completed_at = now(), last_error_code = null, last_error = null,
			stage = 'QUEUED', backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = null,
			version = version + 1, updated_at = now()
		where cancel_requested = true and (
		  status = 'PENDING' or (
		    status = 'PROCESSING' and (locked_until is null or locked_until <= now())
		    and not (
		      stage in ('PREPARED','FILE_REPLACED','COMMITTED') and attempt_id is not null
		      and output_checksum_sha256 is not null
		    )
		  )
		)
		returning id::text, track_id::text, requested_by::text, attempts`)
	if err != nil {
		return nil, fmt.Errorf("cancel exhausted metadata writebacks: %w", err)
	}
	cancelledAudits := make([]expiredWritebackAudit, 0)
	for cancelled.Next() {
		var id, trackID string
		var actorID *string
		var attempts int
		if err := cancelled.Scan(&id, &trackID, &actorID, &attempts); err != nil {
			cancelled.Close()
			return nil, fmt.Errorf("scan cancelled exhausted metadata writeback: %w", err)
		}
		cancelledAudits = append(cancelledAudits, expiredWritebackAudit{
			ID: id, TrackID: trackID, ActorID: actorID, Attempts: attempts,
		})
	}
	if err := cancelled.Err(); err != nil {
		cancelled.Close()
		return nil, fmt.Errorf("iterate cancelled exhausted metadata writebacks: %w", err)
	}
	cancelled.Close()
	for _, item := range cancelledAudits {
		if err := insertAudit(ctx, tx, auditWrite{
			ActorID: item.ActorID, Action: "TRACK_METADATA_WRITEBACK_CANCELLED",
			TargetType: "metadata_writeback_job", TargetID: &item.ID, Result: "SUCCESS",
			TraceID: "worker:" + item.ID,
			Details: map[string]any{"trackId": item.TrackID, "attempts": item.Attempts},
		}); err != nil {
			return nil, err
		}
	}

	exhausted, err := tx.Query(ctx, `
		update metadata_writeback_jobs set
			status = 'FAILED', locked_by = null, locked_until = null,
			completed_at = now(), last_error_code = 'WORKER_LEASE_EXPIRED',
			last_error = 'The final worker lease expired before completion',
			stage = 'QUEUED', backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = null,
			version = version + 1, updated_at = now()
		where cancel_requested = false and attempts >= max_attempts and (
		  status = 'PENDING' or (
		    status = 'PROCESSING' and (locked_until is null or locked_until <= now())
		    and not (
		      stage in ('PREPARED','FILE_REPLACED','COMMITTED') and attempt_id is not null
		      and output_checksum_sha256 is not null
		    )
		  )
		)
		returning id::text, track_id::text, requested_by::text, attempts`)
	if err != nil {
		return nil, fmt.Errorf("fail exhausted metadata writebacks: %w", err)
	}
	exhaustedAudits := make([]expiredWritebackAudit, 0)
	for exhausted.Next() {
		var id, trackID string
		var actorID *string
		var attempts int
		if err := exhausted.Scan(&id, &trackID, &actorID, &attempts); err != nil {
			exhausted.Close()
			return nil, fmt.Errorf("scan exhausted metadata writeback: %w", err)
		}
		exhaustedAudits = append(exhaustedAudits, expiredWritebackAudit{
			ID: id, TrackID: trackID, ActorID: actorID, Attempts: attempts,
		})
	}
	if err := exhausted.Err(); err != nil {
		exhausted.Close()
		return nil, fmt.Errorf("iterate exhausted metadata writebacks: %w", err)
	}
	exhausted.Close()
	for _, item := range exhaustedAudits {
		if err := insertAudit(ctx, tx, auditWrite{
			ActorID: item.ActorID, Action: "TRACK_METADATA_WRITEBACK_FAILED",
			TargetType: "metadata_writeback_job", TargetID: &item.ID, Result: "FAILURE",
			TraceID: "worker:" + item.ID,
			Details: map[string]any{
				"trackId": item.TrackID, "code": "WORKER_LEASE_EXPIRED", "attempts": item.Attempts,
			},
		}); err != nil {
			return nil, err
		}
	}

	candidate, err := scanWriteback(tx.QueryRow(ctx, `
		select `+writebackColumns+`
		from metadata_writeback_jobs
		where next_attempt_at <= now() and (
			(status = 'PENDING' and attempts < max_attempts) or (
				status = 'PROCESSING'
				and (locked_until is null or locked_until <= now())
				and (
				  attempts < max_attempts or (
				    stage in ('PREPARED','FILE_REPLACED','COMMITTED') and attempt_id is not null
				    and output_checksum_sha256 is not null
				  )
				)
			)
		)
		order by next_attempt_at, created_at
		for update skip locked limit 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty metadata writeback claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select metadata writeback candidate: %w", err)
	}
	recovering := writebackNeedsReconciliation(candidate)
	if candidate.CancelRequested && !recovering {
		job, err := scanWriteback(tx.QueryRow(ctx, `
			update metadata_writeback_jobs set
				status = 'CANCELLED', completed_at = now(), locked_by = null,
				locked_until = null, stage = 'QUEUED', backup_path = null,
				backup_expires_at = null, output_checksum_sha256 = null,
				version = version + 1, updated_at = now()
			where id = $1 returning `+writebackColumns, candidate.ID))
		if err != nil {
			return nil, fmt.Errorf("cancel claimed metadata writeback: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit cancelled metadata writeback claim: %w", err)
		}
		return &job, nil
	}
	if recovering {
		job, err := scanWriteback(tx.QueryRow(ctx, `
			update metadata_writeback_jobs set locked_by = $2,
				locked_until = now() + ($3::double precision * interval '1 microsecond'),
				backup_path = null, backup_expires_at = null,
				version = version + 1, updated_at = now()
			where id = $1 and version = $4 returning `+writebackColumns,
			candidate.ID, workerID, leaseMicroseconds, candidate.Version))
		if err != nil {
			return nil, fmt.Errorf("claim transient writeback rollback recovery: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit transient rollback recovery claim: %w", err)
		}
		return &job, nil
	}
	attemptID := uuid.NewString()
	job, err := scanWriteback(tx.QueryRow(ctx, `
		update metadata_writeback_jobs set
			status = 'PROCESSING',
			attempts = attempts + 1,
			attempt_id = $2,
			stage = 'PREPARING', backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = null,
			locked_by = $3,
			locked_until = now() + ($4::double precision * interval '1 microsecond'),
			started_at = coalesce(started_at, now()), completed_at = null,
			version = version + 1, updated_at = now()
		where id = $1 and version = $5
		returning `+writebackColumns,
		candidate.ID, attemptID, workerID, leaseMicroseconds, candidate.Version))
	if err != nil {
		return nil, fmt.Errorf("claim metadata writeback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit metadata writeback claim: %w", err)
	}
	return &job, nil
}

func (repository *Repository) LoadWritebackContext(
	ctx context.Context,
	jobID, workerID, attemptID string,
) (WritebackContext, error) {
	return loadWritebackContext(ctx, repository.pool, jobID, workerID, attemptID, false)
}

func (repository *Repository) RenewWritebackLease(
	ctx context.Context,
	jobID, workerID, attemptID string,
	lease time.Duration,
) error {
	if lease <= 0 {
		return errors.New("metadata writeback lease must be positive")
	}
	command, err := repository.pool.Exec(ctx, `
		update metadata_writeback_jobs set
			locked_until = now() + ($4::double precision * interval '1 microsecond'),
			updated_at = now()
		where id = $1 and status = 'PROCESSING'
		  and locked_by = $2 and attempt_id = $3`,
		jobID, workerID, attemptID, lease.Microseconds())
	if err != nil {
		return fmt.Errorf("renew metadata writeback lease: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback lease was lost during heartbeat")
	}
	return nil
}

func (repository *Repository) WritebackCancellationRequested(
	ctx context.Context,
	jobID, workerID, attemptID string,
) (bool, error) {
	var cancelled bool
	err := repository.pool.QueryRow(ctx, `
		select cancel_requested from metadata_writeback_jobs
		where id = $1 and status = 'PROCESSING' and locked_by = $2 and attempt_id = $3`,
		jobID, workerID, attemptID).Scan(&cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback lease was lost")
	}
	if err != nil {
		return false, fmt.Errorf("read metadata writeback cancellation: %w", err)
	}
	return cancelled, nil
}

func (repository *Repository) MarkWritebackPrepared(
	ctx context.Context,
	jobID, workerID, attemptID, outputChecksum string,
) error {
	command, err := repository.pool.Exec(ctx, `
		update metadata_writeback_jobs set stage = 'PREPARED',
			backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = $4, updated_at = now()
		where id = $1 and status = 'PROCESSING' and locked_by = $2
		  and attempt_id = $3`,
		jobID, workerID, attemptID, outputChecksum)
	if err != nil {
		return fmt.Errorf("mark metadata writeback prepared: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback lease was lost before file replacement")
	}
	return nil
}

func (repository *Repository) MarkWritebackFileReplaced(
	ctx context.Context,
	jobID, workerID, attemptID, outputChecksum string,
) error {
	command, err := repository.pool.Exec(ctx, `
		update metadata_writeback_jobs set stage = 'FILE_REPLACED',
			output_checksum_sha256 = $4, updated_at = now()
		where id = $1 and status = 'PROCESSING' and locked_by = $2 and attempt_id = $3
		  and output_checksum_sha256 = $4`,
		jobID, workerID, attemptID, outputChecksum)
	if err != nil {
		return fmt.Errorf("mark metadata writeback file replaced: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback lease was lost after file replacement")
	}
	return nil
}

func (repository *Repository) CompleteTransientRollback(
	ctx context.Context,
	jobID, workerID, attemptID string,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transient writeback rollback completion: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := lockedWriteback(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if job.Status != WritebackProcessing || job.LockedBy == nil || *job.LockedBy != workerID ||
		job.AttemptID == nil || *job.AttemptID != attemptID || !writebackNeedsTransientRecovery(job) {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Transient rollback recovery ownership was lost")
	}
	nextStatus := WritebackPending
	code, message := "WORKER_LEASE_EXPIRED", "The previous worker lease expired; the original source was restored"
	if job.CancelRequested {
		nextStatus = WritebackCancelled
		code, message = "WRITEBACK_CANCELLED", "Metadata writeback was cancelled"
	} else if job.Attempts >= job.MaxAttempts {
		nextStatus = WritebackFailed
	}
	command, err := tx.Exec(ctx, `
		update metadata_writeback_jobs set status = $4::metadata_writeback_status,
			attempt_id = null, stage = 'QUEUED', locked_by = null, locked_until = null,
			next_attempt_at = now(), completed_at = case when $4 = 'PENDING' then null else now() end,
			backup_path = null, backup_expires_at = null, output_checksum_sha256 = null,
			last_error_code = $5, last_error = $6,
			version = version + 1, updated_at = now()
		where id = $1 and version = $2 and attempt_id = $3`,
		job.ID, job.Version, attemptID, string(nextStatus), code, message)
	if err != nil {
		return fmt.Errorf("complete transient writeback rollback: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Transient rollback recovery ownership was lost")
	}
	if nextStatus != WritebackPending {
		action, result := "TRACK_METADATA_WRITEBACK_FAILED", "FAILURE"
		if nextStatus == WritebackCancelled {
			action, result = "TRACK_METADATA_WRITEBACK_CANCELLED", "SUCCESS"
		}
		if err := insertAudit(ctx, tx, auditWrite{
			ActorID: job.RequestedBy, Action: action, TargetType: "metadata_writeback_job",
			TargetID: &job.ID, Result: result, TraceID: "worker:" + job.ID,
			Details: map[string]any{"trackId": job.TrackID, "code": code, "attempts": job.Attempts},
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transient writeback rollback completion: %w", err)
	}
	return nil
}

func (repository *Repository) ReleaseTransientRollback(
	ctx context.Context,
	jobID, workerID, attemptID string,
	processError error,
	retryAfter time.Duration,
) error {
	if retryAfter <= 0 {
		return errors.New("transient rollback retry delay must be positive")
	}
	code, message := writebackErrorCode(processError), safeWritebackError(processError)
	if slowTransientRollbackRetry(code) {
		retryAfter = time.Hour
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	command, err := repository.pool.Exec(ctx, `
		update metadata_writeback_jobs set locked_by = null, locked_until = null,
			next_attempt_at = now() + ($4::double precision * interval '1 microsecond'),
			backup_path = null, backup_expires_at = null,
			last_error_code = $5, last_error = $6,
			version = version + 1, updated_at = now()
		where id = $1 and status = 'PROCESSING' and locked_by = $2 and attempt_id = $3
		  and stage in ('PREPARED','FILE_REPLACED') and output_checksum_sha256 is not null`,
		jobID, workerID, attemptID, retryAfter.Microseconds(), code, message)
	if err != nil {
		return fmt.Errorf("release transient writeback rollback recovery: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Transient rollback recovery ownership was lost")
	}
	return nil
}

func slowTransientRollbackRetry(code string) bool {
	switch code {
	case "ROLLBACK_FAILED", "SOURCE_CHANGED", "SOURCE_PATH_CHANGED", "UNSAFE_SOURCE_PATH":
		return true
	default:
		return false
	}
}

func (repository *Repository) CommitWriteback(ctx context.Context, input WritebackCommit) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin metadata writeback commit: %w", err)
	}
	defer tx.Rollback(ctx)
	contextRecord, err := loadWritebackContext(ctx, tx, input.JobID, input.WorkerID, input.AttemptID, true)
	if err != nil {
		return err
	}
	if contextRecord.Job.Stage != StageFileReplaced {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback replacement stage changed unexpectedly")
	}
	if contextRecord.Job.OutputChecksumSHA256 == nil ||
		*contextRecord.Job.OutputChecksumSHA256 != input.OutputSHA256 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback output checksum changed unexpectedly")
	}
	if contextRecord.Job.CancelRequested {
		return NewWritebackError("WRITEBACK_CANCELLED", "Metadata writeback was cancelled")
	}
	if err := assertWritableSource(
		contextRecord.RootMode,
		contextRecord.Enabled,
		contextRecord.Status,
		contextRecord.Source.Status,
	); err != nil {
		return err
	}
	if err := assertWritebackContextUnchanged(contextRecord, input.OriginalSHA256, input.Metadata); err != nil {
		return err
	}
	sourceCommand, err := tx.Exec(ctx, `
		update local_music_sources set checksum_sha256 = $2, size_bytes = $3,
			modified_at = $4, status = 'READY', last_error = null,
			last_seen_at = now(), updated_at = now()
		where id = $1 and checksum_sha256 = $5`, contextRecord.Source.ID,
		input.OutputSHA256, input.OutputSize, input.OutputModified, input.OriginalSHA256)
	if err != nil {
		return fmt.Errorf("commit metadata writeback source: %w", err)
	}
	if sourceCommand.RowsAffected() != 1 {
		return NewWritebackError("SOURCE_CHANGED", "The source record changed before writeback completion")
	}
	metadataJSON, err := encodeJSON(input.Metadata)
	if err != nil {
		return err
	}
	nextVersion := contextRecord.Metadata.Version + 1
	metadataCommand, err := tx.Exec(ctx, `
		update track_metadata set raw_tags = $3::jsonb, raw_checksum_sha256 = $4,
			last_scanned_at = now(), updated_by = $5, version = $6, updated_at = now()
		where track_id = $1 and version = $2`, contextRecord.Metadata.TrackID,
		contextRecord.Metadata.Version, string(metadataJSON), input.OutputSHA256,
		contextRecord.Job.RequestedBy, nextVersion)
	if err != nil {
		return fmt.Errorf("commit metadata writeback snapshot: %w", err)
	}
	if metadataCommand.RowsAffected() != 1 {
		return NewWritebackError("METADATA_CHANGED", "Track metadata changed before writeback completion")
	}
	if _, err := tx.Exec(ctx, `
		insert into track_metadata_revisions (
			track_id, metadata_version, action, raw_tags, overrides,
			effective_tags, actor_id, reason
		) values ($1, $2, 'WRITEBACK', $3::jsonb, $4::jsonb, $3::jsonb, $5, $6)`,
		contextRecord.Metadata.TrackID, nextVersion, string(metadataJSON),
		string(contextRecord.Metadata.Overrides), contextRecord.Job.RequestedBy,
		contextRecord.Job.Reason); err != nil {
		return fmt.Errorf("record metadata writeback revision: %w", err)
	}
	command, err := tx.Exec(ctx, `
		update metadata_writeback_jobs set stage = 'COMMITTED',
			locked_until = greatest(coalesce(locked_until, now()), now() + interval '2 minutes'),
			next_attempt_at = now(), completed_at = null,
			backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = $4, last_error_code = null, last_error = null,
			version = version + 1, updated_at = now()
		where id = $1 and version = $2 and attempt_id = $3`, contextRecord.Job.ID,
		contextRecord.Job.Version, input.AttemptID, input.OutputSHA256)
	if err != nil {
		return fmt.Errorf("complete metadata writeback job: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback lease was lost before completion")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit metadata writeback transaction: %w", err)
	}
	return nil
}

func (repository *Repository) CompleteCommittedRollback(
	ctx context.Context,
	jobID, workerID, attemptID string,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin committed writeback rollback cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := lockedWriteback(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if job.Status != WritebackProcessing || job.Stage != StageCommitted ||
		job.LockedBy == nil || *job.LockedBy != workerID ||
		job.AttemptID == nil || *job.AttemptID != attemptID {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Committed rollback cleanup ownership was lost")
	}
	command, err := tx.Exec(ctx, `
		update metadata_writeback_jobs set status = 'READY', attempt_id = null,
			locked_by = null, locked_until = null,
			cancel_requested = false, completed_at = now(),
			backup_path = null, backup_expires_at = null,
			last_error_code = null, last_error = null,
			version = version + 1, updated_at = now()
		where id = $1 and version = $2 and attempt_id = $3`, job.ID, job.Version, attemptID)
	if err != nil {
		return fmt.Errorf("complete committed writeback rollback cleanup: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Committed rollback cleanup ownership was lost")
	}
	if err := insertAudit(ctx, tx, auditWrite{
		ActorID: job.RequestedBy,
		Action:  "TRACK_METADATA_WRITEBACK_COMPLETED", TargetType: "metadata_writeback_job",
		TargetID: &job.ID, Result: "SUCCESS", TraceID: "worker:" + job.ID,
		Details: map[string]any{"trackId": job.TrackID, "sourceId": job.SourceID},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit completed writeback rollback cleanup: %w", err)
	}
	return nil
}

func (repository *Repository) ReleaseCommittedRollback(
	ctx context.Context,
	jobID, workerID, attemptID string,
	processError error,
	retryAfter time.Duration,
) error {
	if retryAfter <= 0 {
		return errors.New("committed rollback cleanup retry delay must be positive")
	}
	code, message := writebackErrorCode(processError), safeWritebackError(processError)
	if slowTransientRollbackRetry(code) {
		retryAfter = time.Hour
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	command, err := repository.pool.Exec(ctx, `
		update metadata_writeback_jobs set locked_by = null, locked_until = null,
			next_attempt_at = now() + ($4::double precision * interval '1 microsecond'),
			backup_path = null, backup_expires_at = null,
			last_error_code = $5, last_error = $6,
			version = version + 1, updated_at = now()
		where id = $1 and status = 'PROCESSING' and stage = 'COMMITTED'
		  and locked_by = $2 and attempt_id = $3`,
		jobID, workerID, attemptID, retryAfter.Microseconds(), code, message)
	if err != nil {
		return fmt.Errorf("release committed writeback rollback cleanup: %w", err)
	}
	if command.RowsAffected() != 1 {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Committed rollback cleanup ownership was lost")
	}
	return nil
}

type writebackFailureDecision struct {
	Code       string
	Message    string
	Cancelled  bool
	RetryDelay time.Duration
}

func resolveWritebackFailure(job WritebackJob, processError error) writebackFailureDecision {
	code := writebackErrorCode(processError)
	message := safeWritebackError(processError)
	cancelled := code == "WRITEBACK_CANCELLED" || (job.CancelRequested && code != "ROLLBACK_FAILED")
	if job.CancelRequested && code != "ROLLBACK_FAILED" {
		code = "WRITEBACK_CANCELLED"
		message = "Metadata writeback was cancelled"
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	retryDelay := 5 * time.Second * time.Duration(1<<max(0, job.Attempts-1))
	if retryDelay > 5*time.Minute {
		retryDelay = 5 * time.Minute
	}
	return writebackFailureDecision{
		Code: code, Message: message, Cancelled: cancelled, RetryDelay: retryDelay,
	}
}

func (repository *Repository) FailWriteback(
	ctx context.Context,
	jobID, workerID, attemptID string,
	processError error,
	_ time.Time,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failed metadata writeback: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := lockedWriteback(ctx, tx, jobID)
	if err != nil {
		if apperror.IsCode(err, apperror.CodeResourceNotFound) {
			return nil
		}
		return err
	}
	if job.Status != WritebackProcessing || job.LockedBy == nil || *job.LockedBy != workerID ||
		job.AttemptID == nil || *job.AttemptID != attemptID {
		return nil
	}
	decision := resolveWritebackFailure(job, processError)
	if writebackNeedsReconciliation(job) {
		retryDelay := decision.RetryDelay
		if slowTransientRollbackRetry(decision.Code) {
			retryDelay = time.Hour
		}
		command, err := tx.Exec(ctx, `
			update metadata_writeback_jobs set locked_by = null, locked_until = null,
				next_attempt_at = now() + ($4::double precision * interval '1 microsecond'),
				backup_path = null, backup_expires_at = null,
				last_error_code = $5, last_error = $6,
				version = version + 1, updated_at = now()
			where id = $1 and version = $2 and attempt_id = $3`,
			job.ID, job.Version, attemptID, retryDelay.Microseconds(),
			decision.Code, decision.Message)
		if err != nil {
			return fmt.Errorf("preserve transient rollback recovery state: %w", err)
		}
		if command.RowsAffected() != 1 {
			return NewWritebackError("WRITEBACK_LEASE_LOST", "Transient rollback recovery ownership was lost")
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit transient rollback recovery state: %w", err)
		}
		return nil
	}
	code := decision.Code
	cancelled := decision.Cancelled
	terminal := cancelled || terminalWritebackCodes[code] || job.Attempts >= job.MaxAttempts
	nextStatus := WritebackPending
	if cancelled {
		nextStatus = WritebackCancelled
	} else if terminal {
		nextStatus = WritebackFailed
	}
	message := decision.Message
	command, err := tx.Exec(ctx, `
		update metadata_writeback_jobs set status = $4::metadata_writeback_status,
			attempt_id = case when $4 = 'PENDING' then null else attempt_id end,
			stage = 'QUEUED',
			locked_by = null, locked_until = null,
			next_attempt_at = case when $4 = 'PENDING'
				then now() + ($5::double precision * interval '1 microsecond') else now() end,
			completed_at = case when $4 = 'PENDING' then null else now() end,
			backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = null,
			last_error_code = $6, last_error = $7,
			version = version + 1, updated_at = now()
		where id = $1 and version = $2 and attempt_id = $3`,
		job.ID, job.Version, attemptID, string(nextStatus),
		decision.RetryDelay.Microseconds(), code, message)
	if err != nil {
		return fmt.Errorf("persist failed metadata writeback: %w", err)
	}
	if command.RowsAffected() == 1 && nextStatus != WritebackPending {
		action, result := "TRACK_METADATA_WRITEBACK_FAILED", "FAILURE"
		if cancelled {
			action, result = "TRACK_METADATA_WRITEBACK_CANCELLED", "SUCCESS"
		}
		if err := insertAudit(ctx, tx, auditWrite{
			ActorID: job.RequestedBy, Action: action, TargetType: "metadata_writeback_job",
			TargetID: &job.ID, Result: result, TraceID: "worker:" + job.ID,
			Details: map[string]any{"trackId": job.TrackID, "code": code, "attempts": job.Attempts},
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed metadata writeback: %w", err)
	}
	return nil
}

func loadWritebackContext(
	ctx context.Context,
	database queryer,
	jobID, workerID, attemptID string,
	locked bool,
) (WritebackContext, error) {
	lockClause := ""
	if locked {
		lockClause = " for update of job, metadata, source, root"
	}
	row := database.QueryRow(ctx, `
		select `+qualifiedWritebackColumns+`,
			`+metadataColumns+`,
			source.id::text, source.root_id::text, source.source_path,
			source.status, source.checksum_sha256,
			root.path, root.mode::text, root.enabled, root.status::text,
			track.status::text,
			artwork.object_key, artwork.mime_type
		from metadata_writeback_jobs job
		join track_metadata metadata on metadata.track_id = job.track_id
		join local_music_sources source on source.id = job.source_id
		join library_roots root on root.id = source.root_id
		join tracks track on track.id = job.track_id
		left join albums album on album.id = track.album_id
		left join media_assets artwork on artwork.id = album.cover_asset_id and artwork.status = 'READY'
		where job.id = $1 and job.status = 'PROCESSING'
		  and job.locked_by = $2 and job.attempt_id = $3`+lockClause,
		jobID, workerID, attemptID)
	result, err := scanWritebackContext(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackContext{}, NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback lease was lost")
	}
	if err != nil {
		return WritebackContext{}, fmt.Errorf("load metadata writeback context: %w", err)
	}
	return result, nil
}

func scanWritebackContext(row scanRow) (WritebackContext, error) {
	var result WritebackContext
	var sourceRootID *string
	var artworkObjectKey, artworkMIMEType *string
	err := row.Scan(
		&result.Job.ID, &result.Job.TrackID, &result.Job.SourceID,
		&result.Job.RevisionID, &result.Job.RequestedBy, &result.Job.Reason,
		&result.Job.MetadataSnapshot, &result.Job.MetadataVersion,
		&result.Job.ExpectedSourceChecksum, &result.Job.RootPathSnapshot,
		&result.Job.SourcePathSnapshot, &result.Job.Status,
		&result.Job.Attempts, &result.Job.MaxAttempts, &result.Job.Version,
		&result.Job.CancelRequested, &result.Job.AttemptID, &result.Job.Stage,
		&result.Job.LockedBy, &result.Job.LockedUntil, &result.Job.NextAttemptAt,
		&result.Job.StartedAt, &result.Job.CompletedAt, &result.Job.BackupPath,
		&result.Job.BackupExpiresAt, &result.Job.OutputChecksumSHA256,
		&result.Job.LastErrorCode, &result.Job.LastError,
		&result.Job.CreatedAt, &result.Job.UpdatedAt,
		&result.Metadata.TrackID, &result.Metadata.SourceID, &result.Metadata.Raw,
		&result.Metadata.Overrides, &result.Metadata.RawChecksum,
		&result.Metadata.LastScannedAt, &result.Metadata.UpdatedBy,
		&result.Metadata.Version, &result.Metadata.CreatedAt, &result.Metadata.UpdatedAt,
		&result.Source.ID, &sourceRootID, &result.Source.SourcePath,
		&result.Source.Status, &result.Source.ChecksumSHA256,
		&result.RootPath, &result.RootMode, &result.Enabled, &result.Status,
		&result.TrackStatus,
		&artworkObjectKey, &artworkMIMEType,
	)
	result.Source.RootID = sourceRootID
	if artworkObjectKey != nil && artworkMIMEType != nil {
		result.Artwork = &ArtworkReference{ObjectKey: *artworkObjectKey, MIMEType: *artworkMIMEType}
	}
	return result, err
}

func assertWritebackContextUnchanged(
	contextRecord WritebackContext,
	originalChecksum string,
	expected MetadataSnapshot,
) error {
	if err := assertWritebackPathSnapshot(contextRecord); err != nil {
		return err
	}
	if err := assertWritebackTrackActive(contextRecord); err != nil {
		return err
	}
	if contextRecord.Source.ChecksumSHA256 != originalChecksum ||
		contextRecord.Job.ExpectedSourceChecksum != originalChecksum {
		return NewWritebackError("SOURCE_CHANGED", "The source record changed while metadata was being written")
	}
	if contextRecord.Metadata.Version != contextRecord.Job.MetadataVersion {
		return NewWritebackError("METADATA_CHANGED", "Track metadata changed after the writeback was queued")
	}
	if contextRecord.Metadata.SourceID == nil || *contextRecord.Metadata.SourceID != contextRecord.Source.ID {
		return NewWritebackError("METADATA_CHANGED", "The track is now linked to a different source file")
	}
	raw, err := decodeSnapshot(contextRecord.Metadata.Raw)
	if err != nil {
		return err
	}
	overrides, err := decodeOverrides(contextRecord.Metadata.Overrides)
	if err != nil {
		return err
	}
	effective, err := ApplyMetadataOverrides(raw, overrides)
	if err != nil {
		return err
	}
	// hasArtwork reflects the physical source after the worker has injected a
	// ready album cover; it is not an administrator-editable metadata field.
	expected.HasArtwork = effective.HasArtwork
	if !MetadataSnapshotsEqual(effective, expected) {
		return NewWritebackError("METADATA_CHANGED", "Track metadata changed after the writeback was queued")
	}
	return nil
}

var terminalWritebackCodes = map[string]bool{
	"WRITEBACK_CANCELLED": true, "WRITEBACK_LEASE_LOST": true,
	"SOURCE_CHANGED": true, "SOURCE_PATH_CHANGED": true,
	"SOURCE_NOT_FOUND": true, "SOURCE_NOT_FILE": true,
	"ROOT_NOT_FOUND": true, "UNSAFE_SOURCE_PATH": true,
	"ROLLBACK_FAILED":  true,
	"METADATA_CHANGED": true, "METADATA_NOT_FOUND": true, "FORBIDDEN": true,
	"INVALID_STATE_TRANSITION": true, "VALIDATION_ERROR": true,
	"RESOURCE_NOT_FOUND": true, "NO_AUDIO_STREAM": true,
	"UNSUPPORTED_CONTAINER": true, "METADATA_VERIFICATION_FAILED": true,
	"STREAM_VERIFICATION_FAILED": true, "DURATION_VERIFICATION_FAILED": true,
	"ARTWORK_VERIFICATION_FAILED":   true,
	"ARTWORK_WRITEBACK_UNSUPPORTED": true, "ARTWORK_FORMAT_UNSUPPORTED": true,
}

const qualifiedWritebackColumns = `
	job.id::text, job.track_id::text, job.source_id::text, job.revision_id::text,
	job.requested_by::text, job.reason, job.metadata_snapshot, job.metadata_version,
	job.expected_source_checksum, job.root_path_snapshot, job.source_path_snapshot,
	job.status::text, job.attempts, job.max_attempts,
	job.version, job.cancel_requested, job.attempt_id::text, job.stage, job.locked_by,
	job.locked_until, job.next_attempt_at, job.started_at, job.completed_at,
	job.backup_path, job.backup_expires_at, job.output_checksum_sha256,
	job.last_error_code, job.last_error, job.created_at, job.updated_at`
