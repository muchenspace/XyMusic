package retention

import (
	"context"
	"fmt"
	"time"
)

func apply(ctx context.Context, executor Executor, now time.Time) (Counts, error) {
	cutoffs := RetentionCutoffs(now)
	idleSession := now.Add(-time.Hour)
	staleUploadCompletion := now.Add(-10 * time.Minute)
	counts := Counts{}
	var err error

	counts.Idempotency, err = drain(ctx, executor, idempotencyStatement, now)
	if err != nil {
		return counts, policyError("idempotency", err)
	}
	counts.RateLimits, err = drain(ctx, executor, rateLimitsStatement, now)
	if err != nil {
		return counts, policyError("rate limits", err)
	}
	counts.RefreshTokens, err = drain(ctx, executor, refreshTokensStatement, cutoffs.RefreshTokens)
	if err != nil {
		return counts, policyError("refresh tokens", err)
	}
	counts.SessionsRevoked, err = drain(ctx, executor, sessionsRevokedStatement, idleSession, now)
	if err != nil {
		return counts, policyError("idle sessions", err)
	}
	counts.SessionsDeleted, err = drain(ctx, executor, sessionsDeletedStatement, cutoffs.RevokedSessions)
	if err != nil {
		return counts, policyError("revoked sessions", err)
	}
	counts.UploadsExpired, err = drain(
		ctx, executor, uploadsExpiredStatement, now, staleUploadCompletion,
	)
	if err != nil {
		return counts, policyError("expired uploads", err)
	}
	counts.UploadsDeleted, err = drain(ctx, executor, uploadsDeletedStatement, cutoffs.Uploads)
	if err != nil {
		return counts, policyError("terminal uploads", err)
	}
	counts.MediaJobs, err = drain(ctx, executor, mediaJobsStatement, cutoffs.OperationalJobs)
	if err != nil {
		return counts, policyError("media jobs", err)
	}
	counts.LibraryScans, err = drain(ctx, executor, libraryScansStatement, cutoffs.OperationalJobs)
	if err != nil {
		return counts, policyError("library scans", err)
	}
	counts.Writebacks, err = drain(ctx, executor, writebacksStatement, cutoffs.OperationalJobs)
	if err != nil {
		return counts, policyError("metadata writebacks", err)
	}
	counts.ObjectCleanupJobs, err = drain(
		ctx, executor, objectCleanupJobsStatement, cutoffs.OperationalJobs,
	)
	if err != nil {
		return counts, policyError("object cleanup jobs", err)
	}
	counts.TrackDeleteBatches, err = drain(
		ctx, executor, trackDeleteBatchesStatement, cutoffs.OperationalJobs,
	)
	if err != nil {
		return counts, policyError("permanent track deletion batches", err)
	}
	counts.Audit, err = drain(ctx, executor, auditStatement, cutoffs.Audit)
	if err != nil {
		return counts, policyError("audit logs", err)
	}
	return counts, nil
}

func drain(
	ctx context.Context,
	executor Executor,
	statement string,
	arguments ...any,
) (int64, error) {
	var affected int64
	for batch := 0; batch < MaxBatchesPerPolicy; batch++ {
		rows, err := executor.Execute(ctx, statement, arguments...)
		if err != nil {
			return affected, err
		}
		affected += rows
		if rows < BatchSize {
			break
		}
	}
	return affected, nil
}

func policyError(policy string, err error) error {
	return fmt.Errorf("apply %s retention policy: %w", policy, err)
}

const idempotencyStatement = `
WITH candidates AS (
  SELECT id FROM idempotency_records
  WHERE expires_at <= $1::timestamptz
  ORDER BY expires_at, id
  LIMIT 500
)
DELETE FROM idempotency_records target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const rateLimitsStatement = `
WITH candidates AS (
  SELECT key_hash FROM rate_limit_buckets
  WHERE reset_at <= $1::timestamptz
  ORDER BY reset_at, key_hash
  LIMIT 500
)
DELETE FROM rate_limit_buckets target
USING candidates
WHERE target.key_hash = candidates.key_hash
RETURNING target.key_hash AS "keyHash"`

const refreshTokensStatement = `
WITH candidates AS (
  SELECT id FROM refresh_tokens
  WHERE expires_at <= $1::timestamptz
     OR used_at <= $1::timestamptz
     OR revoked_at <= $1::timestamptz
  ORDER BY expires_at, id
  LIMIT 500
)
DELETE FROM refresh_tokens target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const sessionsRevokedStatement = `
WITH candidates AS (
  SELECT auth_session.id
  FROM auth_sessions auth_session
  WHERE auth_session.revoked_at IS NULL
    AND auth_session.last_seen_at < $1::timestamptz
    AND NOT EXISTS (
      SELECT 1 FROM refresh_tokens token
      WHERE token.session_id = auth_session.id
        AND token.expires_at > $2::timestamptz
        AND token.used_at IS NULL
        AND token.revoked_at IS NULL
    )
  ORDER BY auth_session.last_seen_at, auth_session.id
  LIMIT 500
)
UPDATE auth_sessions target
SET revoked_at = $2::timestamptz
FROM candidates
WHERE target.id = candidates.id AND target.revoked_at IS NULL
RETURNING target.id`

const sessionsDeletedStatement = `
WITH candidates AS (
  SELECT id FROM auth_sessions
  WHERE revoked_at <= $1::timestamptz
  ORDER BY revoked_at, id
  LIMIT 500
)
DELETE FROM auth_sessions target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const uploadsExpiredStatement = `
WITH candidates AS (
  SELECT id FROM media_uploads
  WHERE (status = 'CREATED' AND expires_at <= $1::timestamptz)
     OR (status = 'COMPLETING' AND expires_at <= $1::timestamptz
       AND (completion_started_at IS NULL OR completion_started_at <= $2::timestamptz))
  ORDER BY expires_at, id
  LIMIT 500
  FOR UPDATE SKIP LOCKED
), expired AS (
  UPDATE media_uploads target
  SET status = 'EXPIRED', completion_token = NULL, completion_started_at = NULL
  FROM candidates
  WHERE target.id = candidates.id
  RETURNING target.id, target.object_key
), queued AS (
  INSERT INTO object_cleanup_jobs (
    object_key, reason, status, attempts, max_attempts, next_attempt_at, created_at, updated_at
  )
  SELECT object_key, 'EXPIRED_UPLOAD', 'PENDING', 0, 20,
    $1::timestamptz, $1::timestamptz, $1::timestamptz
  FROM expired
  ON CONFLICT (object_key) DO UPDATE SET
    reason = 'EXPIRED_UPLOAD',
    status = (CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN 'PROCESSING' ELSE 'PENDING' END)::object_cleanup_status,
    attempts = CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN object_cleanup_jobs.attempts ELSE 0 END,
    attempt_id = CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN object_cleanup_jobs.attempt_id ELSE NULL END,
    locked_by = CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN object_cleanup_jobs.locked_by ELSE NULL END,
    locked_until = CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN object_cleanup_jobs.locked_until ELSE NULL END,
    next_attempt_at = CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN object_cleanup_jobs.next_attempt_at ELSE $1::timestamptz END,
    last_error = CASE WHEN object_cleanup_jobs.status = 'PROCESSING'
      THEN object_cleanup_jobs.last_error ELSE NULL END,
    updated_at = $1::timestamptz
  RETURNING object_key
)
SELECT expired.id FROM expired
INNER JOIN queued ON queued.object_key = expired.object_key`

const uploadsDeletedStatement = `
WITH candidates AS (
  SELECT id FROM media_uploads
  WHERE (status = 'COMPLETED' AND completed_at <= $1::timestamptz)
     OR (status IN ('EXPIRED', 'FAILED') AND expires_at <= $1::timestamptz)
  ORDER BY created_at, id
  LIMIT 500
)
DELETE FROM media_uploads target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const mediaJobsStatement = `
WITH candidates AS (
  SELECT id FROM media_jobs
  WHERE status IN ('READY', 'FAILED', 'CANCELLED') AND updated_at <= $1::timestamptz
  ORDER BY updated_at, id
  LIMIT 500
), cleared_uploads AS (
  UPDATE media_uploads upload SET job_id = NULL
  FROM candidates WHERE upload.job_id = candidates.id
)
DELETE FROM media_jobs target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const libraryScansStatement = `
WITH candidates AS (
  SELECT id FROM library_scan_runs
  WHERE status IN ('COMPLETED', 'FAILED', 'CANCELLED')
    AND completed_at <= $1::timestamptz
  ORDER BY completed_at, id
  LIMIT 500
)
DELETE FROM library_scan_runs target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const writebacksStatement = `
WITH candidates AS (
	SELECT id FROM metadata_writeback_jobs
	WHERE status IN ('READY', 'FAILED', 'CANCELLED')
	  AND completed_at <= $1::timestamptz
	ORDER BY completed_at, id
  LIMIT 500
)
DELETE FROM metadata_writeback_jobs target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const objectCleanupJobsStatement = `
WITH candidates AS (
  SELECT id FROM object_cleanup_jobs
  WHERE status IN ('COMPLETED', 'FAILED') AND updated_at <= $1::timestamptz
  ORDER BY updated_at, id
  LIMIT 500
)
DELETE FROM object_cleanup_jobs target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const trackDeleteBatchesStatement = `
WITH candidates AS (
  SELECT id FROM track_delete_batches
  WHERE status IN ('COMPLETED', 'FAILED')
    AND completed_at <= $1::timestamptz
  ORDER BY completed_at, id
  LIMIT 500
)
DELETE FROM track_delete_batches target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const auditStatement = `
WITH candidates AS (
  SELECT id FROM audit_logs
  WHERE created_at <= $1::timestamptz
  ORDER BY created_at, id
  LIMIT 500
)
DELETE FROM audit_logs target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`
