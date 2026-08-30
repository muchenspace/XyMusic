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
	counts.LibraryScans, err = drain(ctx, executor, libraryScansStatement, cutoffs.OperationalJobs)
	if err != nil {
		return counts, policyError("library scans", err)
	}
	counts.Writebacks, err = drain(ctx, executor, writebacksStatement, cutoffs.OperationalJobs)
	if err != nil {
		return counts, policyError("metadata writebacks", err)
	}
	counts.TrackDeleteBatches, err = drain(
		ctx, executor, trackDeleteBatchesStatement, cutoffs.OperationalJobs,
	)
	if err != nil {
		return counts, policyError("permanent track deletion batches", err)
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
RETURNING target.key_hash`

const refreshTokensStatement = `
WITH candidates AS (
  SELECT id FROM refresh_tokens
  WHERE expires_at <= $1::timestamptz
     OR (revoked_at IS NOT NULL AND revoked_at <= $1::timestamptz)
  ORDER BY LEAST(expires_at, COALESCE(revoked_at, expires_at)), id
  LIMIT 500
)
DELETE FROM refresh_tokens target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const sessionsRevokedStatement = `
WITH idle_candidates AS (
  SELECT id FROM auth_sessions
  WHERE revoked_at IS NULL
    AND last_seen_at <= $1::timestamptz
    AND NOT EXISTS (
      SELECT 1 FROM refresh_tokens token
      WHERE token.session_id = auth_sessions.id
        AND token.revoked_at IS NULL
        AND token.expires_at > $1::timestamptz
    )
  ORDER BY last_seen_at, id
  LIMIT 500
), revoked_sessions AS (
  UPDATE auth_sessions session
  SET revoked_at = $2::timestamptz
  FROM idle_candidates
  WHERE session.id = idle_candidates.id
  RETURNING session.id
), revoked_tokens AS (
  UPDATE refresh_tokens token
  SET revoked_at = $2::timestamptz
  FROM idle_candidates
  WHERE token.session_id = idle_candidates.id
    AND token.revoked_at IS NULL
)
SELECT id FROM revoked_sessions`

const sessionsDeletedStatement = `
WITH candidates AS (
  SELECT id FROM auth_sessions
  WHERE revoked_at IS NOT NULL AND revoked_at <= $1::timestamptz
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
)
UPDATE media_uploads upload
SET status = 'EXPIRED',
    completion_token = NULL,
    completion_started_at = NULL
FROM candidates
WHERE upload.id = candidates.id
RETURNING upload.id`

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
    AND completed_at IS NOT NULL AND completed_at <= $1::timestamptz
  ORDER BY completed_at, id
  LIMIT 500
)
DELETE FROM metadata_writeback_jobs target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`

const trackDeleteBatchesStatement = `
WITH candidates AS (
  SELECT id FROM track_delete_batches
  WHERE status = 'COMPLETED' AND completed_at <= $1::timestamptz
  ORDER BY completed_at, id
  LIMIT 500
)
DELETE FROM track_delete_batches target
USING candidates
WHERE target.id = candidates.id
RETURNING target.id`
