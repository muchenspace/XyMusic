package media

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresStore struct {
	database postgresDatabase
}

func NewPostgresStore(database *pgxpool.Pool) (*PostgresStore, error) {
	if database == nil {
		return nil, errors.New("media worker database is required")
	}
	return &PostgresStore{database: database}, nil
}

func (store *PostgresStore) ClaimMediaJob(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (_ *MediaJob, resultErr error) {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin media job claim: %w", err)
	}
	defer func() { resultErr = finishTransaction(ctx, transaction, resultErr) }()
	if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET
		status='FAILED',locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		last_error_code='RETRY_EXHAUSTED',
		last_error='Media job lease expired after all retry attempts were used',
		updated_at=$1,version=version+1
		WHERE status='PROCESSING' AND locked_until<$1 AND attempts>=max_attempts`, now); err != nil {
		return nil, fmt.Errorf("expire exhausted media jobs: %w", err)
	}
	candidate, err := scanMediaJob(transaction.QueryRow(ctx, `SELECT `+mediaJobColumns+`
		FROM media_jobs WHERE cancel_requested=false AND attempts<max_attempts
		AND next_attempt_at<=$1 AND (
			status='PENDING' OR (status='PROCESSING' AND locked_until<$1)
		) ORDER BY next_attempt_at,created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select media job claim candidate: %w", err)
	}
	attemptID := uuid.NewString()
	claimed, err := scanMediaJob(transaction.QueryRow(ctx, `UPDATE media_jobs SET
		status='PROCESSING',attempts=attempts+1,attempt_id=$2,version=version+1,
		locked_by=$3,locked_until=$4,heartbeat_at=$1,last_error=NULL,last_error_code=NULL,updated_at=$1
		WHERE id=$5 AND version=$6 RETURNING `+mediaJobColumns,
		now, attemptID, workerID, now.Add(lease), candidate.ID, candidate.Version))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim media job: %w", err)
	}
	return claimed, nil
}

func (store *PostgresStore) RenewMediaLease(
	ctx context.Context,
	jobID, attemptID, workerID string,
	heartbeatAt, lockedUntil time.Time,
) (bool, error) {
	result, err := store.database.Exec(ctx, `UPDATE media_jobs SET heartbeat_at=$4,locked_until=$5
		WHERE id=$1 AND status='PROCESSING' AND locked_by=$3 AND attempt_id=$2
		AND cancel_requested=false`, jobID, attemptID, workerID, heartbeatAt, lockedUntil)
	if err != nil {
		return false, fmt.Errorf("renew media job lease: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

func (store *PostgresStore) MediaJobControl(
	ctx context.Context,
	jobID, attemptID, workerID string,
) (JobControl, error) {
	var status string
	var lockedBy *string
	var cancelRequested bool
	err := store.database.QueryRow(ctx, `SELECT status::text,locked_by,cancel_requested
		FROM media_jobs WHERE id=$1 AND attempt_id=$2`, jobID, attemptID).Scan(
		&status, &lockedBy, &cancelRequested,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobControl{}, nil
	}
	if err != nil {
		return JobControl{}, fmt.Errorf("read media job control: %w", err)
	}
	return JobControl{
		Owned:           status == "PROCESSING" && lockedBy != nil && *lockedBy == workerID,
		CancelRequested: cancelRequested,
	}, nil
}

func (store *PostgresStore) FindReadySourceAsset(ctx context.Context, assetID string) (*SourceAsset, error) {
	var asset SourceAsset
	err := store.database.QueryRow(ctx, `SELECT id,object_key,size_bytes,checksum_sha256
		FROM media_assets WHERE id=$1 AND status='READY'`, assetID).Scan(
		&asset.ID, &asset.ObjectKey, &asset.SizeBytes, &asset.ChecksumSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find media source asset: %w", err)
	}
	return &asset, nil
}

func (store *PostgresStore) CommitMediaJob(
	ctx context.Context,
	input CommitMediaJob,
) (_ []string, resultErr error) {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin media job completion: %w", err)
	}
	defer func() { resultErr = finishTransaction(ctx, transaction, resultErr) }()
	var status string
	var lockedBy, attemptID *string
	var cancelRequested bool
	err = transaction.QueryRow(ctx, `SELECT status::text,locked_by,attempt_id,cancel_requested
		FROM media_jobs WHERE id=$1 FOR UPDATE`, input.Job.ID).Scan(
		&status, &lockedBy, &attemptID, &cancelRequested,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, newInterruptedError("JOB_LEASE_LOST", "media job is no longer owned by this attempt")
	}
	if err != nil {
		return nil, fmt.Errorf("lock media job for completion: %w", err)
	}
	if status != "PROCESSING" || lockedBy == nil || *lockedBy != input.WorkerID ||
		attemptID == nil || input.Job.AttemptID == nil || *attemptID != *input.Job.AttemptID ||
		cancelRequested {
		return nil, newInterruptedError("JOB_LEASE_LOST", "media job is no longer owned by this attempt")
	}
	var mediaGeneration int
	var trackStatus string
	err = transaction.QueryRow(ctx, `SELECT media_generation,status::text FROM tracks
		WHERE id=$1 FOR UPDATE`, input.Job.TrackID).Scan(&mediaGeneration, &trackStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, newInterruptedError("JOB_SUPERSEDED", "a newer media generation owns this track")
	}
	if err != nil {
		return nil, fmt.Errorf("lock media job track: %w", err)
	}
	if mediaGeneration != input.Job.Generation || trackStatus == "ARCHIVED" {
		return nil, newInterruptedError("JOB_SUPERSEDED", "a newer media generation owns this track")
	}
	rows, err := transaction.Query(ctx, `SELECT asset_id FROM track_variants WHERE track_id=$1`, input.Job.TrackID)
	if err != nil {
		return nil, fmt.Errorf("list replaced media variants: %w", err)
	}
	previousAssetIDs := make([]string, 0, len(input.Generated))
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan replaced media variant: %w", err)
		}
		previousAssetIDs = append(previousAssetIDs, assetID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate replaced media variants: %w", err)
	}
	rows.Close()
	hasLossless := slices.ContainsFunc(input.Generated, func(output GeneratedVariant) bool {
		return output.Profile.Quality == "LOSSLESS"
	})
	if !hasLossless {
		if _, err := transaction.Exec(ctx, `DELETE FROM track_variants
			WHERE track_id=$1 AND quality='LOSSLESS'`, input.Job.TrackID); err != nil {
			return nil, fmt.Errorf("remove obsolete lossless variant: %w", err)
		}
	}
	for _, output := range input.Generated {
		var assetID string
		err := transaction.QueryRow(ctx, `INSERT INTO media_assets(
			object_key,kind,mime_type,size_bytes,checksum_sha256,status
		) VALUES($1,'AUDIO_VARIANT',$2,$3,$4,'READY') RETURNING id`,
			output.ObjectKey, output.Profile.MIMEType, output.SizeBytes, output.ChecksumSHA256,
		).Scan(&assetID)
		if err != nil {
			return nil, fmt.Errorf("create media variant asset: %w", err)
		}
		bitrate := EstimatedBitrate(output.SizeBytes, input.DurationMS)
		_, err = transaction.Exec(ctx, `INSERT INTO track_variants(
			track_id,asset_id,quality,mime_type,codec,container,bitrate,sample_rate,status
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'READY')
		ON CONFLICT(track_id,quality) DO UPDATE SET
			asset_id=excluded.asset_id,mime_type=excluded.mime_type,codec=excluded.codec,
			container=excluded.container,bitrate=excluded.bitrate,sample_rate=excluded.sample_rate,
			status='READY',updated_at=$9`, input.Job.TrackID, assetID, output.Profile.Quality,
			output.Profile.MIMEType, output.Profile.Codec, output.Profile.Container, bitrate,
			input.SampleRate, input.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("upsert media track variant: %w", err)
		}
	}
	updatedTrack, err := transaction.Exec(ctx, `UPDATE tracks SET duration_ms=$2,status='READY',
		published_at=CASE WHEN $3 THEN $4 ELSE published_at END,version=version+1,updated_at=$4
		WHERE id=$1 AND media_generation=$5 AND status<>'ARCHIVED'`, input.Job.TrackID,
		input.DurationMS, input.Job.PublishOnReady, input.CompletedAt, input.Job.Generation)
	if err != nil {
		return nil, fmt.Errorf("complete media job track: %w", err)
	}
	if updatedTrack.RowsAffected() != 1 {
		return nil, newInterruptedError("JOB_SUPERSEDED", "track generation changed during completion")
	}
	completedJob, err := transaction.Exec(ctx, `UPDATE media_jobs SET status='READY',locked_by=NULL,
		locked_until=NULL,heartbeat_at=NULL,last_error=NULL,last_error_code=NULL,
		version=version+1,updated_at=$4
		WHERE id=$1 AND locked_by=$2 AND attempt_id=$3 AND status='PROCESSING'
		AND cancel_requested=false`, input.Job.ID, input.WorkerID, *input.Job.AttemptID,
		input.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("complete media job: %w", err)
	}
	if completedJob.RowsAffected() != 1 {
		return nil, newInterruptedError("JOB_LEASE_LOST", "media job changed during completion")
	}
	directStatus := "PROCESSING"
	if input.Job.PublishOnReady {
		directStatus = "READY"
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status=$2,last_error=NULL,updated_at=$3
		WHERE media_job_id=$1`, input.Job.ID, directStatus, input.CompletedAt); err != nil {
		return nil, fmt.Errorf("complete direct local media source: %w", err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status='READY',last_error=NULL,updated_at=$1
		WHERE id IN(
			SELECT mapped.source_id FROM local_music_source_tracks mapped
			INNER JOIN media_jobs linked_job ON linked_job.id=mapped.media_job_id
			GROUP BY mapped.source_id HAVING bool_and(linked_job.status='READY')
		)`, input.CompletedAt); err != nil {
		return nil, fmt.Errorf("complete mapped local media sources: %w", err)
	}
	return previousAssetIDs, nil
}

func (store *PostgresStore) FailMediaJob(
	ctx context.Context,
	job MediaJob,
	workerID string,
	processErr error,
	now time.Time,
) (resultErr error) {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin media job failure: %w", err)
	}
	defer func() { resultErr = finishTransaction(ctx, transaction, resultErr) }()
	var status string
	var lockedBy, attemptID *string
	var cancelRequested bool
	var attempts, maxAttempts int
	err = transaction.QueryRow(ctx, `SELECT status::text,locked_by,attempt_id,cancel_requested,attempts,max_attempts
		FROM media_jobs WHERE id=$1 FOR UPDATE`, job.ID).Scan(
		&status, &lockedBy, &attemptID, &cancelRequested, &attempts, &maxAttempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock failed media job: %w", err)
	}
	if status != "PROCESSING" || lockedBy == nil || *lockedBy != workerID ||
		attemptID == nil || job.AttemptID == nil || *attemptID != *job.AttemptID {
		return nil
	}
	terminal := attempts >= maxAttempts
	nextStatus := "PENDING"
	if cancelRequested {
		nextStatus = "CANCELLED"
	} else if terminal {
		nextStatus = "FAILED"
	}
	delay := retryDelay(attempts)
	if isInterrupted(processErr) {
		delay = 0
	}
	message := safeWorkerError(processErr)
	code := workerErrorCode(processErr)
	if cancelRequested {
		message = "Cancelled by an administrator"
		code = "CANCELLED"
	}
	updated, err := transaction.Exec(ctx, `UPDATE media_jobs SET status=$4,locked_by=NULL,
		locked_until=NULL,heartbeat_at=NULL,next_attempt_at=$5,last_error_code=$6,last_error=$7,
		version=version+1,updated_at=$8 WHERE id=$1 AND locked_by=$2 AND attempt_id=$3`,
		job.ID, workerID, *job.AttemptID, nextStatus, now.Add(delay), code, message, now)
	if err != nil {
		return fmt.Errorf("finalize failed media job: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return nil
	}
	sourceStatus := "PROCESSING"
	if nextStatus == "FAILED" || nextStatus == "CANCELLED" {
		sourceStatus = "FAILED"
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status=$2,last_error=$3,updated_at=$4
		WHERE media_job_id=$1`, job.ID, sourceStatus, message, now); err != nil {
		return fmt.Errorf("fail direct local media source: %w", err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status=$2,last_error=$3,updated_at=$4
		WHERE id IN(SELECT source_id FROM local_music_source_tracks WHERE media_job_id=$1)`,
		job.ID, sourceStatus, message, now); err != nil {
		return fmt.Errorf("fail mapped local media sources: %w", err)
	}
	if nextStatus == "FAILED" {
		if _, err := transaction.Exec(ctx, `UPDATE tracks SET status='ERROR',version=version+1,updated_at=$3
			WHERE id=$1 AND media_generation=$2 AND status<>'ARCHIVED'`,
			job.TrackID, job.Generation, now); err != nil {
			return fmt.Errorf("fail media job track: %w", err)
		}
	}
	return nil
}

func (store *PostgresStore) ScheduleReplacedAssetCleanup(
	ctx context.Context,
	assetIDs []string,
	now time.Time,
) (resultErr error) {
	assetIDs = compactStrings(assetIDs)
	if len(assetIDs) == 0 {
		return nil
	}
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin replaced media cleanup: %w", err)
	}
	defer func() { resultErr = finishTransaction(ctx, transaction, resultErr) }()
	rows, err := transaction.Query(ctx, `UPDATE media_assets asset SET status='DELETE_PENDING',updated_at=$2
		WHERE id=ANY($1::uuid[]) AND NOT EXISTS(
			SELECT 1 FROM track_variants variant WHERE variant.asset_id=asset.id
		) RETURNING object_key`, assetIDs, now)
	if err != nil {
		return fmt.Errorf("mark replaced media assets for deletion: %w", err)
	}
	keys := make([]string, 0, len(assetIDs))
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return fmt.Errorf("scan replaced media cleanup key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate replaced media cleanup keys: %w", err)
	}
	rows.Close()
	for _, key := range keys {
		if _, err := transaction.Exec(ctx, `INSERT INTO object_cleanup_jobs(
			object_key,reason,next_attempt_at,created_at,updated_at
		) VALUES($1,'REPLACED_MEDIA_VARIANT',$2,$2,$2) ON CONFLICT(object_key) DO NOTHING`, key, now); err != nil {
			return fmt.Errorf("enqueue replaced media cleanup: %w", err)
		}
	}
	return nil
}

func (store *PostgresStore) EnqueueObjectCleanup(
	ctx context.Context,
	objectKey, reason string,
	now time.Time,
) error {
	_, err := store.database.Exec(ctx, `INSERT INTO object_cleanup_jobs(
		object_key,reason,next_attempt_at,created_at,updated_at
	) VALUES($1,$2,$3,$3,$3) ON CONFLICT(object_key) DO NOTHING`, objectKey, reason, now)
	if err != nil {
		return fmt.Errorf("enqueue object cleanup: %w", err)
	}
	return nil
}

func (store *PostgresStore) ClaimObjectCleanup(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (_ *CleanupJob, resultErr error) {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin object cleanup claim: %w", err)
	}
	defer func() { resultErr = finishTransaction(ctx, transaction, resultErr) }()
	if _, err := transaction.Exec(ctx, `UPDATE object_cleanup_jobs SET status='FAILED',locked_by=NULL,
		locked_until=NULL,last_error='Object cleanup lease expired after all retry attempts were used',updated_at=$1
		WHERE status='PROCESSING' AND locked_until<$1 AND attempts>=max_attempts`, now); err != nil {
		return nil, fmt.Errorf("expire exhausted object cleanup jobs: %w", err)
	}
	candidate, err := scanCleanupJob(transaction.QueryRow(ctx, `SELECT `+cleanupJobColumns+`
		FROM object_cleanup_jobs WHERE attempts<max_attempts AND next_attempt_at<=$1
		AND (status='PENDING' OR (status='PROCESSING' AND locked_until<$1))
		ORDER BY next_attempt_at,created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select object cleanup candidate: %w", err)
	}
	attemptID := uuid.NewString()
	claimed, err := scanCleanupJob(transaction.QueryRow(ctx, `UPDATE object_cleanup_jobs SET
		status='PROCESSING',attempts=attempts+1,attempt_id=$2,locked_by=$3,
		locked_until=$4,last_error=NULL,updated_at=$1 WHERE id=$5 RETURNING `+cleanupJobColumns,
		now, attemptID, workerID, now.Add(lease), candidate.ID))
	if err != nil {
		return nil, fmt.Errorf("claim object cleanup job: %w", err)
	}
	return claimed, nil
}

func (store *PostgresStore) ReadyAssetReferencesObject(ctx context.Context, objectKey string) (bool, error) {
	var found bool
	err := store.database.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM media_assets WHERE object_key=$1 AND status='READY'
	)`, objectKey).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("inspect cleanup object references: %w", err)
	}
	return found, nil
}

func (store *PostgresStore) CompleteObjectCleanup(
	ctx context.Context,
	cleanup CleanupJob,
	workerID string,
	referenced bool,
	now time.Time,
) (_ bool, resultErr error) {
	if cleanup.AttemptID == nil {
		return false, nil
	}
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin object cleanup completion: %w", err)
	}
	defer func() { resultErr = finishTransaction(ctx, transaction, resultErr) }()
	updated, err := transaction.Exec(ctx, `UPDATE object_cleanup_jobs SET status='COMPLETED',
		locked_by=NULL,locked_until=NULL,last_error=NULL,updated_at=$4
		WHERE id=$1 AND locked_by=$2 AND attempt_id=$3`, cleanup.ID, workerID, *cleanup.AttemptID, now)
	if err != nil {
		return false, fmt.Errorf("complete object cleanup job: %w", err)
	}
	owned := updated.RowsAffected() == 1
	if owned && !referenced {
		if _, err := transaction.Exec(ctx, `UPDATE media_assets SET status='DELETED',updated_at=$2
			WHERE object_key=$1 AND status='DELETE_PENDING'`, cleanup.ObjectKey, now); err != nil {
			return false, fmt.Errorf("complete deleted media asset: %w", err)
		}
	}
	return owned, nil
}

func (store *PostgresStore) FailObjectCleanup(
	ctx context.Context,
	cleanup CleanupJob,
	workerID string,
	cleanupErr error,
	now time.Time,
) error {
	if cleanup.AttemptID == nil {
		return nil
	}
	status := "PENDING"
	if cleanup.Attempts >= cleanup.MaxAttempts {
		status = "FAILED"
	}
	_, err := store.database.Exec(ctx, `UPDATE object_cleanup_jobs SET status=$4,locked_by=NULL,
		locked_until=NULL,next_attempt_at=$5,last_error=$6,updated_at=$7
		WHERE id=$1 AND locked_by=$2 AND attempt_id=$3`, cleanup.ID, workerID,
		*cleanup.AttemptID, status, now.Add(retryDelay(cleanup.Attempts)),
		safeWorkerError(cleanupErr), now)
	if err != nil {
		return fmt.Errorf("fail object cleanup job: %w", err)
	}
	return nil
}

func scanMediaJob(row pgx.Row) (*MediaJob, error) {
	var job MediaJob
	err := row.Scan(
		&job.ID, &job.Type, &job.SourceAssetID, &job.TrackID, &job.Status,
		&job.Attempts, &job.MaxAttempts, &job.Version, &job.Generation, &job.AttemptID,
		&job.CancelRequested, &job.PublishOnReady, &job.Payload, &job.LockedBy,
		&job.LockedUntil, &job.HeartbeatAt, &job.NextAttemptAt, &job.LastError,
		&job.LastErrorCode, &job.CreatedAt, &job.UpdatedAt,
	)
	return &job, err
}

func scanCleanupJob(row pgx.Row) (*CleanupJob, error) {
	var job CleanupJob
	err := row.Scan(
		&job.ID, &job.ObjectKey, &job.Reason, &job.Status, &job.Attempts,
		&job.MaxAttempts, &job.AttemptID, &job.LockedBy, &job.LockedUntil,
		&job.NextAttemptAt, &job.LastError, &job.CreatedAt, &job.UpdatedAt,
	)
	return &job, err
}

func finishTransaction(ctx context.Context, transaction pgx.Tx, resultErr error) error {
	if resultErr != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return resultErr
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit media worker transaction: %w", err)
	}
	return nil
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

const mediaJobColumns = `id,type::text,source_asset_id,track_id,status::text,attempts,max_attempts,
	version,generation,attempt_id,cancel_requested,publish_on_ready,payload,locked_by,locked_until,
	heartbeat_at,next_attempt_at,last_error,last_error_code,created_at,updated_at`

const cleanupJobColumns = `id,object_key,reason,status::text,attempts,max_attempts,attempt_id,
	locked_by,locked_until,next_attempt_at,last_error,created_at,updated_at`
