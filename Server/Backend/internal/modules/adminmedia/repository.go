package adminmedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

const (
	maximumActiveUploads = 3
	avatarByteBudget     = int64(15 * 1024 * 1024)
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ Store = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) CreateUpload(
	ctx context.Context,
	input CreateUploadParams,
) (MediaUpload, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return MediaUpload{}, fmt.Errorf("begin media upload reservation: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := expireStaleUploads(ctx, tx, input.Now); err != nil {
		return MediaUpload{}, err
	}
	if err := requireUploadTarget(ctx, tx, input.Purpose, input.TargetID); err != nil {
		return MediaUpload{}, err
	}
	if _, err := tx.Exec(ctx,
		`select pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"media-upload-quota:"+input.ActorID,
	); err != nil {
		return MediaUpload{}, fmt.Errorf("lock media upload quota: %w", err)
	}
	var activeCount int
	var activeBytes int64
	if err := tx.QueryRow(ctx, `
		select count(*)::int, coalesce(sum(expected_size), 0)::bigint
		from media_uploads
		where uploader_id = $1
		  and status in ('CREATED', 'COMPLETING')
		  and expires_at > $2`, input.ActorID, input.Now).Scan(&activeCount, &activeBytes); err != nil {
		return MediaUpload{}, fmt.Errorf("measure media upload quota: %w", err)
	}
	byteBudget := input.MaximumBytes * 2
	if input.Purpose == PurposeUserAvatar {
		byteBudget = avatarByteBudget
	}
	if activeCount >= maximumActiveUploads || activeBytes+input.SizeBytes > byteBudget {
		retryAfter := int(input.ExpiresAt.Sub(input.Now) / time.Second)
		if retryAfter < 1 {
			retryAfter = 1
		}
		return MediaUpload{}, apperror.RateLimited(retryAfter)
	}

	var trackID *string
	if input.Purpose == PurposeTrackSource {
		trackID = &input.TargetID
	}
	upload, err := scanUpload(tx.QueryRow(ctx, `
		insert into media_uploads (
			id, purpose, target_id, track_id, uploader_id, object_key,
			expected_size, expected_checksum_sha256, expected_mime_type,
			original_file_name, status, expires_at, created_at
		) values ($1, $2::media_upload_purpose, $3, $4, $5, $6, $7, $8, $9, $10, 'CREATED', $11, $12)
		returning `+mediaUploadColumns,
		input.ID,
		string(input.Purpose),
		input.TargetID,
		trackID,
		input.ActorID,
		input.ObjectKey,
		input.SizeBytes,
		input.ChecksumSHA256,
		input.ContentType,
		input.FileName,
		input.ExpiresAt,
		input.Now,
	))
	if err != nil {
		return MediaUpload{}, fmt.Errorf("insert media upload reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MediaUpload{}, fmt.Errorf("commit media upload reservation: %w", err)
	}
	return upload, nil
}

func (repository *Repository) MarkUploadFailed(ctx context.Context, actorID, uploadID string) error {
	_, err := repository.pool.Exec(ctx, `
		update media_uploads
		set status = 'FAILED', completion_token = null, completion_started_at = null
		where id = $1 and uploader_id = $2 and status = 'CREATED'`, uploadID, actorID)
	if err != nil {
		return fmt.Errorf("mark media upload reservation failed: %w", err)
	}
	return nil
}

func (repository *Repository) AbandonUpload(
	ctx context.Context,
	actorID string,
	uploadID string,
	now time.Time,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin abandoned media upload cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	upload, err := scanUpload(tx.QueryRow(ctx, `
		select `+mediaUploadColumns+` from media_uploads where id = $1 for update`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return fmt.Errorf("lock abandoned media upload: %w", err)
	}
	if upload.UploaderID != actorID {
		return apperror.Forbidden("Only the upload creator can abandon it")
	}
	if upload.Status != UploadStatusCompleted {
		if _, err := tx.Exec(ctx, `
			update media_uploads
			set status = 'FAILED', completion_token = null, completion_started_at = null
			where id = $1 and status <> 'COMPLETED'`, upload.ID); err != nil {
			return fmt.Errorf("abandon media upload reservation: %w", err)
		}
		if err := queueObjectCleanup(ctx, tx, upload.ObjectKey, "ABANDONED_UPLOAD", now); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit abandoned media upload cleanup: %w", err)
	}
	return nil
}

func (repository *Repository) FindUploadForContent(
	ctx context.Context,
	actorID string,
	uploadID string,
) (MediaUpload, error) {
	upload, err := scanUpload(repository.pool.QueryRow(ctx, `
		select `+mediaUploadColumns+` from media_uploads where id = $1`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaUpload{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return MediaUpload{}, fmt.Errorf("find media upload content reservation: %w", err)
	}
	if upload.UploaderID != actorID {
		return MediaUpload{}, apperror.Forbidden("Only the upload creator can send its content")
	}
	return upload, nil
}

func (repository *Repository) ClaimCompletion(
	ctx context.Context,
	actorID string,
	uploadID string,
	completionToken string,
	now time.Time,
	lease time.Duration,
) (CompletionClaim, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return CompletionClaim{}, fmt.Errorf("begin media completion claim: %w", err)
	}
	defer tx.Rollback(ctx)

	upload, err := scanUpload(tx.QueryRow(ctx, `
		select `+mediaUploadColumns+` from media_uploads where id = $1 for update`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionClaim{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return CompletionClaim{}, fmt.Errorf("lock media upload completion: %w", err)
	}
	if upload.UploaderID != actorID {
		return CompletionClaim{}, apperror.Forbidden("Only the upload creator can complete it")
	}
	if upload.Status == UploadStatusCompleted && upload.AssetID != nil {
		if err := tx.Commit(ctx); err != nil {
			return CompletionClaim{}, fmt.Errorf("commit completed media upload lookup: %w", err)
		}
		return CompletionClaim{Outcome: CompletionFinished, Upload: upload}, nil
	}
	if upload.Status == UploadStatusCompleting {
		stale := upload.CompletionStartedAt == nil || !upload.CompletionStartedAt.After(now.Add(-lease))
		if !stale {
			if err := tx.Commit(ctx); err != nil {
				return CompletionClaim{}, fmt.Errorf("commit active media completion lookup: %w", err)
			}
			return CompletionClaim{Outcome: CompletionInProgress, Upload: upload}, nil
		}
	}
	if !upload.ExpiresAt.After(now) {
		if _, err := tx.Exec(ctx, `
			update media_uploads
			set status = 'EXPIRED', completion_token = null, completion_started_at = null
			where id = $1`, upload.ID); err != nil {
			return CompletionClaim{}, fmt.Errorf("expire media upload: %w", err)
		}
		if err := queueObjectCleanup(ctx, tx, upload.ObjectKey, "EXPIRED_UPLOAD", now); err != nil {
			return CompletionClaim{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CompletionClaim{}, fmt.Errorf("commit media upload expiry: %w", err)
		}
		return CompletionClaim{Outcome: CompletionExpired, Upload: upload}, nil
	}
	if upload.Status != UploadStatusCreated && upload.Status != UploadStatusCompleting {
		return CompletionClaim{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			fmt.Sprintf("Upload cannot be completed from %s", upload.Status),
			nil,
		)
	}
	completionExpiresAt := now.Add(lease)
	if _, err := tx.Exec(ctx, `
		update media_uploads
		set status = 'COMPLETING', completion_token = $2,
			completion_started_at = $3, expires_at = $4
		where id = $1`, upload.ID, completionToken, now, completionExpiresAt); err != nil {
		return CompletionClaim{}, fmt.Errorf("claim media upload completion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletionClaim{}, fmt.Errorf("commit media completion claim: %w", err)
	}
	upload.Status = UploadStatusCompleting
	upload.CompletionToken = &completionToken
	upload.CompletionStartedAt = &now
	upload.ExpiresAt = completionExpiresAt
	return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: completionToken}, nil
}

func (repository *Repository) CompletionStatus(
	ctx context.Context,
	actorID string,
	uploadID string,
) (MediaUpload, error) {
	upload, err := scanUpload(repository.pool.QueryRow(ctx, `
		select `+mediaUploadColumns+` from media_uploads where id = $1`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaUpload{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return MediaUpload{}, fmt.Errorf("find media upload completion status: %w", err)
	}
	if upload.UploaderID != actorID {
		return MediaUpload{}, apperror.Forbidden("Only the upload creator can complete it")
	}
	return upload, nil
}

func (repository *Repository) FinalizeCompletion(
	ctx context.Context,
	input FinalizeCompletionParams,
) (CompletedUpload, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return CompletedUpload{}, fmt.Errorf("begin media upload completion: %w", err)
	}
	defer tx.Rollback(ctx)
	if input.CompletionFence != nil {
		if err := input.CompletionFence.Lock(ctx, tx); err != nil {
			return CompletedUpload{}, err
		}
	}

	upload, err := scanUpload(tx.QueryRow(ctx, `
		select `+mediaUploadColumns+` from media_uploads where id = $1 for update`, input.UploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletedUpload{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return CompletedUpload{}, fmt.Errorf("lock completing media upload: %w", err)
	}
	if upload.UploaderID != input.ActorID {
		return CompletedUpload{}, apperror.Forbidden("Only the upload creator can complete it")
	}
	if upload.Status == UploadStatusCompleted && upload.AssetID != nil {
		completed := completedUpload(upload)
		if err := tx.Commit(ctx); err != nil {
			return CompletedUpload{}, fmt.Errorf("commit completed upload lookup: %w", err)
		}
		return completed, nil
	}
	if upload.Status != UploadStatusCompleting || upload.CompletionToken == nil ||
		*upload.CompletionToken != input.CompletionToken {
		return CompletedUpload{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Media upload completion ownership was lost",
			nil,
		)
	}
	kind := "ARTWORK"
	if upload.Purpose == PurposeTrackSource {
		kind = "AUDIO_SOURCE"
	}
	if _, err := tx.Exec(ctx, `
		insert into media_assets (
			id, uploader_id, object_key, kind, mime_type, size_bytes,
			checksum_sha256, width, height, status, created_at, updated_at
		) values ($1, $2, $3, $4::asset_kind, $5, $6, $7, $8, $9, 'READY', $10, $10)`,
		input.AssetID,
		input.ActorID,
		input.Inspected.ObjectKey,
		kind,
		input.Inspected.MIMEType,
		input.Inspected.SizeBytes,
		input.Inspected.ChecksumSHA256,
		input.Inspected.Width,
		input.Inspected.Height,
		input.Now,
	); err != nil {
		return CompletedUpload{}, fmt.Errorf("insert completed media asset: %w", err)
	}

	var jobID *string
	switch upload.Purpose {
	case PurposeTrackSource:
		createdJobID, err := finalizeTrackUpload(ctx, tx, upload, input, input.Now)
		if err != nil {
			return CompletedUpload{}, err
		}
		jobID = &createdJobID
	case PurposeArtistArtwork:
		if err := attachArtistArtwork(ctx, tx, upload.TargetID, input.AssetID, input.Now); err != nil {
			return CompletedUpload{}, err
		}
	case PurposeAlbumArtwork:
		if err := attachAlbumArtwork(ctx, tx, upload.TargetID, input.AssetID, input.Now); err != nil {
			return CompletedUpload{}, err
		}
	case PurposeUserAvatar:
		if err := attachUserAvatar(ctx, tx, upload.TargetID, input.AssetID, input.Now); err != nil {
			return CompletedUpload{}, err
		}
	default:
		return CompletedUpload{}, errors.New("media upload has an unsupported purpose")
	}

	command, err := tx.Exec(ctx, `
		update media_uploads
		set status = 'COMPLETED', asset_id = $3, job_id = $4, completed_at = $5,
			completion_token = null, completion_started_at = null
		where id = $1 and completion_token = $2`,
		input.UploadID,
		input.CompletionToken,
		input.AssetID,
		jobID,
		input.Now,
	)
	if err != nil {
		return CompletedUpload{}, fmt.Errorf("complete media upload record: %w", err)
	}
	if command.RowsAffected() != 1 {
		return CompletedUpload{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Media upload completion ownership was lost",
			nil,
		)
	}
	for _, objectKey := range uniqueStrings(input.Inspected.CleanupKeys) {
		if objectKey != "" && objectKey != input.Inspected.ObjectKey {
			if err := queueObjectCleanup(ctx, tx, objectKey, "NORMALIZED_UPLOAD_SOURCE", input.Now); err != nil {
				return CompletedUpload{}, err
			}
		}
	}
	if err := insertAudit(ctx, tx, AuditWrite{
		ActorID:    input.ActorID,
		Action:     "media.upload.complete",
		TargetType: "media_upload",
		TargetID:   upload.ID,
		TraceID:    input.TraceID,
		Details: map[string]any{
			"assetId":  input.AssetID,
			"jobId":    jobID,
			"purpose":  upload.Purpose,
			"targetId": upload.TargetID,
		},
	}); err != nil {
		return CompletedUpload{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletedUpload{}, fmt.Errorf("commit media upload completion: %w", err)
	}
	return CompletedUpload{UploadID: upload.ID, AssetID: input.AssetID, JobID: jobID}, nil
}

func (repository *Repository) FailCompletion(
	ctx context.Context,
	uploadID string,
	completionToken string,
	retryable bool,
	objectKeys []string,
	reason string,
	now time.Time,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failed media completion cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	status := UploadStatusFailed
	if retryable {
		status = UploadStatusCreated
	}
	command, err := tx.Exec(ctx, `
		update media_uploads
		set status = $3::media_upload_status, completion_token = null, completion_started_at = null
		where id = $1 and status = 'COMPLETING' and completion_token = $2`,
		uploadID, completionToken, status,
	)
	if err != nil {
		return fmt.Errorf("release failed media completion: %w", err)
	}
	if command.RowsAffected() == 1 && !retryable {
		for _, objectKey := range uniqueStrings(objectKeys) {
			if objectKey != "" {
				if err := queueObjectCleanup(ctx, tx, objectKey, reason, now); err != nil {
					return err
				}
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed media completion cleanup: %w", err)
	}
	return nil
}

func (repository *Repository) FindJob(ctx context.Context, jobID string) (MediaJob, error) {
	job, err := scanJob(repository.pool.QueryRow(ctx, `
		select `+mediaJobColumns+` from media_jobs where id = $1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaJob{}, apperror.NotFound("Media job was not found")
	}
	if err != nil {
		return MediaJob{}, fmt.Errorf("find media job: %w", err)
	}
	return job, nil
}

func (repository *Repository) RetryJob(
	ctx context.Context,
	input RetryJobParams,
) (MediaJob, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return MediaJob{}, fmt.Errorf("begin media job retry: %w", err)
	}
	defer tx.Rollback(ctx)
	current, err := scanJob(tx.QueryRow(ctx, `
		select `+mediaJobColumns+` from media_jobs where id = $1 for update`, input.JobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaJob{}, apperror.NotFound("Media job was not found")
	}
	if err != nil {
		return MediaJob{}, fmt.Errorf("lock media job retry: %w", err)
	}
	if current.Version != input.ExpectedVersion {
		return MediaJob{}, apperror.Conflict(
			apperror.CodeVersionConflict,
			"Media job version is stale",
			map[string]any{
				"expectedVersion": input.ExpectedVersion,
				"currentVersion":  current.Version,
			},
		)
	}
	if current.Status != JobStatusFailed && current.Status != JobStatusCancelled {
		return MediaJob{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Only failed or cancelled media jobs can be retried",
			nil,
		)
	}
	var trackStatus string
	var trackGeneration int
	var trackVersion int
	err = tx.QueryRow(ctx, `
		select status::text, media_generation, version
		from tracks where id = $1 for update`, current.TrackID).Scan(
		&trackStatus, &trackGeneration, &trackVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) || trackStatus == "ARCHIVED" {
		return MediaJob{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Archived tracks cannot retry media processing",
			nil,
		)
	}
	if err != nil {
		return MediaJob{}, fmt.Errorf("lock media job track: %w", err)
	}
	var active bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from media_jobs
			where track_id = $1 and id <> $2 and status in ('PENDING', 'PROCESSING')
		)`, current.TrackID, current.ID).Scan(&active); err != nil {
		return MediaJob{}, fmt.Errorf("check newer active media job: %w", err)
	}
	if active {
		return MediaJob{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"A newer media job is already active",
			nil,
		)
	}
	generation := trackGeneration + 1
	if _, err := tx.Exec(ctx, `
		update tracks set media_generation = $2, version = $3, updated_at = $4 where id = $1`,
		current.TrackID, generation, trackVersion+1, input.Now,
	); err != nil {
		return MediaJob{}, fmt.Errorf("advance media job track generation: %w", err)
	}
	updated, err := scanJob(tx.QueryRow(ctx, `
		update media_jobs set
			status = 'PENDING', generation = $3, attempts = 0, attempt_id = null,
			cancel_requested = false, next_attempt_at = $4, locked_by = null,
			locked_until = null, heartbeat_at = null, last_error = null,
			last_error_code = null, version = version + 1, updated_at = $4
		where id = $1 and version = $2
		returning `+mediaJobColumns,
		input.JobID, input.ExpectedVersion, generation, input.Now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaJob{}, apperror.Conflict(
			apperror.CodeVersionConflict,
			"Media job changed while retrying",
			nil,
		)
	}
	if err != nil {
		return MediaJob{}, fmt.Errorf("retry media job: %w", err)
	}
	if err := insertAudit(ctx, tx, AuditWrite{
		ActorID:    input.ActorID,
		Action:     "media.job.retry",
		TargetType: "media_job",
		TargetID:   current.ID,
		TraceID:    input.TraceID,
		Details:    map[string]any{"reason": input.Reason, "generation": generation},
	}); err != nil {
		return MediaJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MediaJob{}, fmt.Errorf("commit media job retry: %w", err)
	}
	return updated, nil
}

func (repository *Repository) WriteAudit(ctx context.Context, input AuditWrite) error {
	if err := insertAudit(ctx, repository.pool, input); err != nil {
		return err
	}
	return nil
}

func requireUploadTarget(
	ctx context.Context,
	tx pgx.Tx,
	purpose UploadPurpose,
	targetID string,
) error {
	switch purpose {
	case PurposeTrackSource:
		var status string
		err := tx.QueryRow(ctx, `select status::text from tracks where id = $1`, targetID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NotFound("Track was not found")
		}
		if err != nil {
			return fmt.Errorf("verify track upload target: %w", err)
		}
		if status == "ARCHIVED" {
			return apperror.Conflict(
				apperror.CodeInvalidStateTransition,
				"Archived tracks cannot receive media",
				nil,
			)
		}
	case PurposeArtistArtwork:
		return requireTargetExists(ctx, tx, "artists", "Artist", targetID)
	case PurposeAlbumArtwork:
		return requireTargetExists(ctx, tx, "albums", "Album", targetID)
	case PurposeUserAvatar:
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from users u join user_profiles p on p.user_id = u.id where u.id = $1
			)`, targetID).Scan(&exists); err != nil {
			return fmt.Errorf("verify user avatar upload target: %w", err)
		}
		if !exists {
			return apperror.NotFound("User profile was not found")
		}
	default:
		return apperror.Validation("purpose is invalid")
	}
	return nil
}

func requireTargetExists(ctx context.Context, tx pgx.Tx, table, label, targetID string) error {
	query := "select exists (select 1 from " + table + " where id = $1)"
	var exists bool
	if err := tx.QueryRow(ctx, query, targetID).Scan(&exists); err != nil {
		return fmt.Errorf("verify %s upload target: %w", label, err)
	}
	if !exists {
		return apperror.NotFound(label + " was not found")
	}
	return nil
}

func expireStaleUploads(ctx context.Context, tx pgx.Tx, now time.Time) error {
	rows, err := tx.Query(ctx, `
		select id, object_key
		from media_uploads
		where (
			status = 'CREATED' and expires_at < $1
		) or (
			status = 'COMPLETING' and expires_at < $1
			and (completion_started_at is null or completion_started_at < $2)
		)
		order by expires_at, id
		limit 100
		for update skip locked`, now, now.Add(-completionLease))
	if err != nil {
		return fmt.Errorf("find stale media uploads: %w", err)
	}
	defer rows.Close()
	type staleUpload struct{ id, objectKey string }
	var uploads []staleUpload
	for rows.Next() {
		var upload staleUpload
		if err := rows.Scan(&upload.id, &upload.objectKey); err != nil {
			return fmt.Errorf("scan stale media upload: %w", err)
		}
		uploads = append(uploads, upload)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stale media uploads: %w", err)
	}
	for _, upload := range uploads {
		if _, err := tx.Exec(ctx, `
			update media_uploads
			set status = 'EXPIRED', completion_token = null, completion_started_at = null
			where id = $1`, upload.id); err != nil {
			return fmt.Errorf("expire stale media upload: %w", err)
		}
		if err := queueObjectCleanup(ctx, tx, upload.objectKey, "EXPIRED_UPLOAD", now); err != nil {
			return err
		}
	}
	return nil
}

func finalizeTrackUpload(
	ctx context.Context,
	tx pgx.Tx,
	upload MediaUpload,
	input FinalizeCompletionParams,
	now time.Time,
) (string, error) {
	if upload.TrackID == nil {
		return "", errors.New("track source upload has no track id")
	}
	var status string
	var generation int
	var version int
	err := tx.QueryRow(ctx, `
		select status::text, media_generation, version
		from tracks where id = $1 for update`, *upload.TrackID).Scan(&status, &generation, &version)
	if errors.Is(err, pgx.ErrNoRows) || status == "ARCHIVED" {
		return "", apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Archived tracks cannot receive media",
			nil,
		)
	}
	if err != nil {
		return "", fmt.Errorf("lock track media generation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update media_jobs set
			status = 'CANCELLED', cancel_requested = true, locked_by = null,
			locked_until = null, heartbeat_at = null, last_error_code = 'SUPERSEDED',
			last_error = 'A newer upload superseded this media job',
			version = version + 1, updated_at = $2
		where track_id = $1 and status in ('PENDING', 'PROCESSING')`, *upload.TrackID, now); err != nil {
		return "", fmt.Errorf("cancel superseded media jobs: %w", err)
	}
	generation++
	if _, err := tx.Exec(ctx, `
		update tracks set media_generation = $2, version = $3, updated_at = $4 where id = $1`,
		*upload.TrackID, generation, version+1, now,
	); err != nil {
		return "", fmt.Errorf("advance track media generation: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"uploadId":         upload.ID,
		"originalFileName": upload.OriginalFileName,
	})
	if err != nil {
		return "", fmt.Errorf("encode media job payload: %w", err)
	}
	jobID := input.JobID
	if jobID == "" {
		return "", errors.New("media job id is required")
	}
	if _, err := tx.Exec(ctx, `
		insert into media_jobs (
			id, type, source_asset_id, track_id, generation, idempotency_key,
			payload, created_at, updated_at, next_attempt_at
		) values ($1, 'INGEST_TRACK', $2, $3, $4, $5, $6::jsonb, $7, $7, $7)`,
		jobID,
		input.AssetID,
		*upload.TrackID,
		generation,
		"ingest-track:"+upload.ID,
		string(payload),
		now,
	); err != nil {
		return "", fmt.Errorf("enqueue track media job: %w", err)
	}
	return jobID, nil
}

func attachArtistArtwork(ctx context.Context, tx pgx.Tx, artistID, assetID string, now time.Time) error {
	var previousAssetID *string
	err := tx.QueryRow(ctx, `select artwork_asset_id from artists where id = $1 for update`, artistID).Scan(&previousAssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Artist was not found")
	}
	if err != nil {
		return fmt.Errorf("lock artist artwork target: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update artists set artwork_asset_id = $2, version = version + 1, updated_at = $3 where id = $1`,
		artistID, assetID, now,
	); err != nil {
		return fmt.Errorf("attach artist artwork: %w", err)
	}
	return detachArtwork(ctx, tx, previousAssetID, now)
}

func attachAlbumArtwork(ctx context.Context, tx pgx.Tx, albumID, assetID string, now time.Time) error {
	var previousAssetID *string
	err := tx.QueryRow(ctx, `select cover_asset_id from albums where id = $1 for update`, albumID).Scan(&previousAssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Album was not found")
	}
	if err != nil {
		return fmt.Errorf("lock album artwork target: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update albums set cover_asset_id = $2, version = version + 1, updated_at = $3 where id = $1`,
		albumID, assetID, now,
	); err != nil {
		return fmt.Errorf("attach album artwork: %w", err)
	}
	return detachArtwork(ctx, tx, previousAssetID, now)
}

func attachUserAvatar(ctx context.Context, tx pgx.Tx, userID, assetID string, now time.Time) error {
	var previousAssetID *string
	err := tx.QueryRow(ctx, `select avatar_asset_id from user_profiles where user_id = $1 for update`, userID).Scan(&previousAssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("User profile was not found")
	}
	if err != nil {
		return fmt.Errorf("lock user avatar target: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update user_profiles set avatar_asset_id = $2, updated_at = $3 where user_id = $1`,
		userID, assetID, now,
	); err != nil {
		return fmt.Errorf("attach user avatar: %w", err)
	}
	command, err := tx.Exec(ctx, `
		update users set version = version + 1, updated_at = $2 where id = $1`, userID, now)
	if err != nil {
		return fmt.Errorf("advance avatar user version: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.NotFound("User was not found")
	}
	return detachArtwork(ctx, tx, previousAssetID, now)
}

func detachArtwork(ctx context.Context, tx pgx.Tx, assetID *string, now time.Time) error {
	if assetID == nil {
		return nil
	}
	var objectKey string
	err := tx.QueryRow(ctx, `
		update media_assets set status = 'DELETE_PENDING', updated_at = $2
		where id = $1
		  and not exists (select 1 from artists where artwork_asset_id = media_assets.id)
		  and not exists (select 1 from albums where cover_asset_id = media_assets.id)
		  and not exists (select 1 from user_profiles where avatar_asset_id = media_assets.id)
		returning object_key`, *assetID, now).Scan(&objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("detach replaced artwork: %w", err)
	}
	return queueObjectCleanup(ctx, tx, objectKey, "REPLACED_ARTWORK", now)
}

func queueObjectCleanup(
	ctx context.Context,
	database interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	objectKey string,
	reason string,
	now time.Time,
) error {
	_, err := database.Exec(ctx, `
		insert into object_cleanup_jobs (object_key, reason, next_attempt_at, created_at, updated_at)
		values ($1, $2, $3, $3, $3)
		on conflict (object_key) do update set
			reason = excluded.reason, status = 'PENDING', attempts = 0, attempt_id = null,
			locked_by = null, locked_until = null, next_attempt_at = excluded.next_attempt_at,
			last_error = null, updated_at = excluded.updated_at`, objectKey, reason, now)
	if err != nil {
		return fmt.Errorf("queue object cleanup: %w", err)
	}
	return nil
}

func insertAudit(
	ctx context.Context,
	database interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	input AuditWrite,
) error {
	details, err := json.Marshal(input.Details)
	if err != nil {
		return fmt.Errorf("encode media audit details: %w", err)
	}
	if _, err := database.Exec(ctx, `
		insert into audit_logs (actor_id, action, target_type, target_id, result, trace_id, details)
		values ($1, $2, $3, $4, 'SUCCESS', $5, $6::jsonb)`,
		input.ActorID,
		input.Action,
		input.TargetType,
		input.TargetID,
		input.TraceID,
		string(details),
	); err != nil {
		return fmt.Errorf("write media audit log: %w", err)
	}
	return nil
}

type scanRow interface {
	Scan(...any) error
}

func scanUpload(row scanRow) (MediaUpload, error) {
	var upload MediaUpload
	var purpose string
	err := row.Scan(
		&upload.ID,
		&purpose,
		&upload.TargetID,
		&upload.TrackID,
		&upload.UploaderID,
		&upload.ObjectKey,
		&upload.ExpectedSize,
		&upload.ExpectedChecksumSHA256,
		&upload.ExpectedMIMEType,
		&upload.OriginalFileName,
		&upload.Status,
		&upload.CompletionToken,
		&upload.CompletionStartedAt,
		&upload.AssetID,
		&upload.JobID,
		&upload.ExpiresAt,
		&upload.CreatedAt,
		&upload.CompletedAt,
	)
	upload.Purpose = UploadPurpose(purpose)
	return upload, err
}

func scanJob(row scanRow) (MediaJob, error) {
	var job MediaJob
	err := row.Scan(
		&job.ID,
		&job.Type,
		&job.Status,
		&job.TrackID,
		&job.Generation,
		&job.Attempts,
		&job.MaxAttempts,
		&job.CancelRequested,
		&job.LastError,
		&job.LastErrorCode,
		&job.NextAttemptAt,
		&job.Version,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	return job, err
}

func completedUpload(upload MediaUpload) CompletedUpload {
	if upload.AssetID == nil {
		return CompletedUpload{}
	}
	return CompletedUpload{UploadID: upload.ID, AssetID: *upload.AssetID, JobID: upload.JobID}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

const mediaUploadColumns = `
	id, purpose::text, target_id, track_id, uploader_id, object_key,
	expected_size, expected_checksum_sha256, expected_mime_type,
	original_file_name, status::text, completion_token, completion_started_at,
	asset_id, job_id, expires_at, created_at, completed_at`

const mediaJobColumns = `
	id, type::text, status::text, track_id, generation, attempts, max_attempts,
	cancel_requested, last_error, last_error_code, next_attempt_at, version,
	created_at, updated_at`
