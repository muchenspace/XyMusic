package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ Store = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) UpdateProfile(
	ctx context.Context,
	userID string,
	expectedVersion int,
	changes ProfileChanges,
	now time.Time,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin profile update: %w", err)
	}
	defer tx.Rollback(ctx)

	var version int
	err = tx.QueryRow(ctx, `
		update users
		set version = version + 1, updated_at = $3
		where id = $1 and version = $2
		returning version`, userID, expectedVersion, now).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		var currentVersion int
		lookupErr := tx.QueryRow(ctx, `select version from users where id = $1`, userID).Scan(&currentVersion)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return apperror.NotFound("User no longer exists")
		}
		if lookupErr != nil {
			return fmt.Errorf("find current user version: %w", lookupErr)
		}
		return apperror.Conflict(
			apperror.CodeVersionConflict,
			"User profile was modified by another client",
			map[string]any{
				"expectedVersion":      expectedVersion,
				"currentVersion":       currentVersion,
				"conflictResourceType": "USER",
				"conflictResourceId":   userID,
			},
		)
	}
	if err != nil {
		return fmt.Errorf("advance user profile version: %w", err)
	}
	command, err := tx.Exec(ctx, `
		update user_profiles
		set display_name = case when $2 then $3 else display_name end,
			bio = case when $4 then $5 else bio end,
			updated_at = $6
		where user_id = $1`,
		userID,
		changes.DisplayNameSet,
		changes.DisplayName,
		changes.BioSet,
		changes.Bio,
		now,
	)
	if err != nil {
		return fmt.Errorf("update user profile: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.NotFound("User profile was not found")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit profile update: %w", err)
	}
	return nil
}

func (repository *Repository) CreateAvatarUpload(
	ctx context.Context,
	input CreateUploadParams,
) (AvatarUpload, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return AvatarUpload{}, fmt.Errorf("begin avatar upload reservation: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`select pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"media-upload-quota:"+input.ActorID,
	); err != nil {
		return AvatarUpload{}, fmt.Errorf("lock avatar upload quota: %w", err)
	}
	if err := expireActorUploads(ctx, tx, input.ActorID, input.Now); err != nil {
		return AvatarUpload{}, err
	}
	var targetExists bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1
			from users u
			join user_profiles p on p.user_id = u.id
			where u.id = $1
		)`, input.ActorID).Scan(&targetExists); err != nil {
		return AvatarUpload{}, fmt.Errorf("verify avatar upload target: %w", err)
	}
	if !targetExists {
		return AvatarUpload{}, apperror.NotFound("User profile was not found")
	}
	var activeCount int
	var activeBytes int64
	if err := tx.QueryRow(ctx, `
		select count(*)::int, coalesce(sum(expected_size), 0)::bigint
		from media_uploads
		where uploader_id = $1
		  and status in ('CREATED', 'COMPLETING')
		  and expires_at > $2`, input.ActorID, input.Now).Scan(&activeCount, &activeBytes); err != nil {
		return AvatarUpload{}, fmt.Errorf("measure avatar upload quota: %w", err)
	}
	if activeCount >= maximumActiveAvatarUploads || activeBytes+input.SizeBytes > avatarUploadByteBudget {
		retryAfter := int(input.ExpiresAt.Sub(input.Now) / time.Second)
		if retryAfter < 1 {
			retryAfter = 1
		}
		return AvatarUpload{}, apperror.RateLimited(retryAfter)
	}

	upload, err := scanAvatarUpload(tx.QueryRow(ctx, `
		insert into media_uploads (
			id, purpose, target_id, track_id, uploader_id, object_key,
			expected_size, expected_checksum_sha256, expected_mime_type,
			original_file_name, status, expires_at, created_at
		) values ($1, 'USER_AVATAR', $2, null, $2, $3, $4, $5, $6, $7, 'CREATED', $8, $9)
		returning `+avatarUploadColumns,
		input.ID,
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
		return AvatarUpload{}, fmt.Errorf("insert avatar upload: %w", err)
	}
	details, _ := json.Marshal(map[string]any{
		"purpose":   AvatarUploadPurpose,
		"targetId":  input.ActorID,
		"sizeBytes": input.SizeBytes,
	})
	if _, err := tx.Exec(ctx, `
		insert into audit_logs (actor_id, action, target_type, target_id, result, trace_id, details)
		values ($1, 'media.upload.create', 'media_upload', $2, 'SUCCESS', $3, $4::jsonb)`,
		input.ActorID, input.ID, input.TraceID, string(details),
	); err != nil {
		return AvatarUpload{}, fmt.Errorf("audit avatar upload reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AvatarUpload{}, fmt.Errorf("commit avatar upload reservation: %w", err)
	}
	return upload, nil
}

func (repository *Repository) MarkAvatarUploadFailed(ctx context.Context, actorID, uploadID string) error {
	_, err := repository.pool.Exec(ctx, `
		update media_uploads
		set status = 'FAILED', completion_token = null, completion_started_at = null
		where id = $1 and uploader_id = $2 and target_id = $2 and status = 'CREATED'`,
		uploadID, actorID,
	)
	if err != nil {
		return fmt.Errorf("mark avatar reservation failed: %w", err)
	}
	return nil
}

func (repository *Repository) ClaimAvatarCompletion(
	ctx context.Context,
	actorID string,
	uploadID string,
	completionToken string,
	now time.Time,
	lease time.Duration,
) (CompletionClaim, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return CompletionClaim{}, fmt.Errorf("begin avatar completion claim: %w", err)
	}
	defer tx.Rollback(ctx)

	upload, err := scanAvatarUpload(tx.QueryRow(ctx, `
		select `+avatarUploadColumns+`
		from media_uploads
		where id = $1
		for update`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionClaim{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return CompletionClaim{}, fmt.Errorf("lock avatar upload completion: %w", err)
	}
	if upload.UploaderID != actorID || upload.TargetID != actorID || upload.Purpose != AvatarUploadPurpose {
		return CompletionClaim{}, apperror.Forbidden("Users can only complete their own avatar upload")
	}
	if upload.Status == UploadStatusCompleted && upload.AssetID != nil {
		if err := tx.Commit(ctx); err != nil {
			return CompletionClaim{}, fmt.Errorf("commit completed avatar lookup: %w", err)
		}
		return CompletionClaim{Outcome: CompletionFinished, Upload: upload}, nil
	}
	if upload.Status == UploadStatusCompleting {
		stale := upload.CompletionStartedAt == nil || !upload.CompletionStartedAt.After(now.Add(-lease))
		if !stale {
			if err := tx.Commit(ctx); err != nil {
				return CompletionClaim{}, fmt.Errorf("commit active avatar completion lookup: %w", err)
			}
			return CompletionClaim{Outcome: CompletionInProgress, Upload: upload}, nil
		}
	}
	if !upload.ExpiresAt.After(now) {
		if _, err := tx.Exec(ctx, `
			update media_uploads
			set status = 'EXPIRED', completion_token = null, completion_started_at = null
			where id = $1`, upload.ID); err != nil {
			return CompletionClaim{}, fmt.Errorf("expire avatar upload: %w", err)
		}
		if err := queueObjectCleanup(ctx, tx, upload.ObjectKey, "EXPIRED_UPLOAD", now); err != nil {
			return CompletionClaim{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CompletionClaim{}, fmt.Errorf("commit avatar upload expiry: %w", err)
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
		return CompletionClaim{}, fmt.Errorf("claim avatar upload completion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletionClaim{}, fmt.Errorf("commit avatar completion claim: %w", err)
	}
	upload.Status = UploadStatusCompleting
	upload.CompletionToken = &completionToken
	upload.CompletionStartedAt = &now
	upload.ExpiresAt = completionExpiresAt
	return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: completionToken}, nil
}

func (repository *Repository) AvatarCompletionStatus(
	ctx context.Context,
	actorID string,
	uploadID string,
) (string, error) {
	upload, err := scanAvatarUpload(repository.pool.QueryRow(ctx, `
		select `+avatarUploadColumns+` from media_uploads where id = $1`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return "", fmt.Errorf("find avatar completion status: %w", err)
	}
	if upload.UploaderID != actorID || upload.TargetID != actorID || upload.Purpose != AvatarUploadPurpose {
		return "", apperror.Forbidden("Users can only complete their own avatar upload")
	}
	return upload.Status, nil
}

func (repository *Repository) FinalizeAvatarCompletion(
	ctx context.Context,
	input FinalizeAvatarParams,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin avatar completion: %w", err)
	}
	defer tx.Rollback(ctx)

	upload, err := scanAvatarUpload(tx.QueryRow(ctx, `
		select `+avatarUploadColumns+`
		from media_uploads where id = $1 for update`, input.UploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return fmt.Errorf("lock completing avatar upload: %w", err)
	}
	if upload.UploaderID != input.ActorID || upload.TargetID != input.ActorID || upload.Purpose != AvatarUploadPurpose {
		return apperror.Forbidden("Users can only complete their own avatar upload")
	}
	if upload.Status == UploadStatusCompleted && upload.AssetID != nil {
		return tx.Commit(ctx)
	}
	if upload.Status != UploadStatusCompleting || upload.CompletionToken == nil || *upload.CompletionToken != input.CompletionToken {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"Media upload completion ownership was lost",
			nil,
		)
	}
	if _, err := tx.Exec(ctx, `
		insert into media_assets (
			id, uploader_id, object_key, kind, mime_type, size_bytes,
			checksum_sha256, width, height, status, created_at, updated_at
		) values ($1, $2, $3, 'ARTWORK', $4, $5, $6, $7, $8, 'READY', $9, $9)`,
		input.AssetID,
		input.ActorID,
		input.Inspected.ObjectKey,
		input.Inspected.MIMEType,
		input.Inspected.SizeBytes,
		input.Inspected.ChecksumSHA256,
		input.Inspected.Width,
		input.Inspected.Height,
		input.Now,
	); err != nil {
		return fmt.Errorf("insert avatar asset: %w", err)
	}
	var previousAssetID *string
	err = tx.QueryRow(ctx, `
		select avatar_asset_id
		from user_profiles
		where user_id = $1
		for update`, input.ActorID).Scan(&previousAssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("User profile was not found")
	}
	if err != nil {
		return fmt.Errorf("lock avatar profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update user_profiles
		set avatar_asset_id = $2, updated_at = $3
		where user_id = $1`, input.ActorID, input.AssetID, input.Now); err != nil {
		return fmt.Errorf("attach avatar asset: %w", err)
	}
	command, err := tx.Exec(ctx, `
		update users
		set version = version + 1, updated_at = $2
		where id = $1`, input.ActorID, input.Now)
	if err != nil {
		return fmt.Errorf("advance avatar user version: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.NotFound("User no longer exists")
	}
	if previousAssetID != nil && *previousAssetID != input.AssetID {
		var detachedKey string
		detachErr := tx.QueryRow(ctx, `
			update media_assets
			set status = 'DELETE_PENDING', updated_at = $2
			where id = $1
			  and not exists (select 1 from artists where artwork_asset_id = media_assets.id)
			  and not exists (select 1 from albums where cover_asset_id = media_assets.id)
			  and not exists (select 1 from user_profiles where avatar_asset_id = media_assets.id)
			returning object_key`, *previousAssetID, input.Now).Scan(&detachedKey)
		if detachErr != nil && !errors.Is(detachErr, pgx.ErrNoRows) {
			return fmt.Errorf("detach previous avatar: %w", detachErr)
		}
		if detachErr == nil {
			if err := queueObjectCleanup(ctx, tx, detachedKey, "REPLACED_ARTWORK", input.Now); err != nil {
				return err
			}
		}
	}
	command, err = tx.Exec(ctx, `
		update media_uploads
		set status = 'COMPLETED', asset_id = $3, completed_at = $4,
			completion_token = null, completion_started_at = null
		where id = $1 and completion_token = $2`,
		input.UploadID, input.CompletionToken, input.AssetID, input.Now,
	)
	if err != nil {
		return fmt.Errorf("complete avatar upload record: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"Media upload completion ownership was lost",
			nil,
		)
	}
	queued := make(map[string]struct{})
	for _, objectKey := range input.Inspected.CleanupKeys {
		if objectKey == "" || objectKey == input.Inspected.ObjectKey {
			continue
		}
		if _, exists := queued[objectKey]; exists {
			continue
		}
		queued[objectKey] = struct{}{}
		if err := queueObjectCleanup(ctx, tx, objectKey, "NORMALIZED_UPLOAD_SOURCE", input.Now); err != nil {
			return err
		}
	}
	details, _ := json.Marshal(map[string]any{
		"assetId":  input.AssetID,
		"jobId":    nil,
		"purpose":  AvatarUploadPurpose,
		"targetId": input.ActorID,
	})
	if _, err := tx.Exec(ctx, `
		insert into audit_logs (actor_id, action, target_type, target_id, result, trace_id, details)
		values ($1, 'media.upload.complete', 'media_upload', $2, 'SUCCESS', $3, $4::jsonb)`,
		input.ActorID, input.UploadID, input.TraceID, string(details),
	); err != nil {
		return fmt.Errorf("audit avatar completion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit avatar completion: %w", err)
	}
	return nil
}

func (repository *Repository) FailAvatarCompletion(
	ctx context.Context,
	uploadID string,
	completionToken string,
	retryable bool,
	cleanupKeys []string,
	reason string,
	now time.Time,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failed avatar completion cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	status := UploadStatusFailed
	if retryable {
		status = UploadStatusCreated
	}
	command, err := tx.Exec(ctx, `
		update media_uploads
		set status = $3, completion_token = null, completion_started_at = null
		where id = $1 and status = 'COMPLETING' and completion_token = $2`,
		uploadID, completionToken, status,
	)
	if err != nil {
		return fmt.Errorf("release failed avatar completion: %w", err)
	}
	if command.RowsAffected() == 1 && !retryable {
		queued := make(map[string]struct{})
		for _, objectKey := range cleanupKeys {
			if objectKey == "" {
				continue
			}
			if _, exists := queued[objectKey]; exists {
				continue
			}
			queued[objectKey] = struct{}{}
			if err := queueObjectCleanup(ctx, tx, objectKey, reason, now); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed avatar completion cleanup: %w", err)
	}
	return nil
}

const avatarUploadColumns = `
	id, purpose::text, target_id, uploader_id, object_key,
	expected_size, expected_checksum_sha256, expected_mime_type,
	original_file_name, status::text, asset_id, expires_at, created_at,
	completed_at, completion_token, completion_started_at`

type rowScanner interface {
	Scan(...any) error
}

func scanAvatarUpload(row rowScanner) (AvatarUpload, error) {
	var upload AvatarUpload
	err := row.Scan(
		&upload.ID,
		&upload.Purpose,
		&upload.TargetID,
		&upload.UploaderID,
		&upload.ObjectKey,
		&upload.ExpectedSize,
		&upload.ExpectedChecksumSHA256,
		&upload.ExpectedMIMEType,
		&upload.OriginalFileName,
		&upload.Status,
		&upload.AssetID,
		&upload.ExpiresAt,
		&upload.CreatedAt,
		&upload.CompletedAt,
		&upload.CompletionToken,
		&upload.CompletionStartedAt,
	)
	return upload, err
}

func expireActorUploads(ctx context.Context, tx pgx.Tx, actorID string, now time.Time) error {
	rows, err := tx.Query(ctx, `
		update media_uploads
		set status = 'EXPIRED', completion_token = null, completion_started_at = null
		where uploader_id = $1
		  and (
			(status = 'CREATED' and expires_at <= $2)
			or (status = 'COMPLETING' and expires_at <= $2
				and (completion_started_at is null or completion_started_at <= $3))
		  )
		returning object_key`, actorID, now, now.Add(-completionLease))
	if err != nil {
		return fmt.Errorf("expire stale avatar uploads: %w", err)
	}
	defer rows.Close()
	var objectKeys []string
	for rows.Next() {
		var objectKey string
		if err := rows.Scan(&objectKey); err != nil {
			return fmt.Errorf("read expired avatar upload: %w", err)
		}
		objectKeys = append(objectKeys, objectKey)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate expired avatar uploads: %w", err)
	}
	for _, objectKey := range objectKeys {
		if err := queueObjectCleanup(ctx, tx, objectKey, "EXPIRED_UPLOAD", now); err != nil {
			return err
		}
	}
	return nil
}

func queueObjectCleanup(ctx context.Context, tx pgx.Tx, objectKey, reason string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		insert into object_cleanup_jobs (object_key, reason, next_attempt_at, updated_at)
		values ($1, $2, $3, $3)
		on conflict (object_key) do update set
			reason = excluded.reason,
			status = 'PENDING',
			attempts = 0,
			attempt_id = null,
			locked_by = null,
			locked_until = null,
			next_attempt_at = excluded.next_attempt_at,
			last_error = null,
			updated_at = excluded.updated_at`, objectKey, reason, now)
	if err != nil {
		return fmt.Errorf("queue object cleanup: %w", err)
	}
	return nil
}
