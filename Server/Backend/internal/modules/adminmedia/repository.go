package adminmedia

import (
	"context"
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

func (repository *Repository) CreateUpload(ctx context.Context, upload UploadReservation) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin admin upload reservation: %w", err)
	}
	defer tx.Rollback(ctx)

	var targetExists bool
	switch upload.Purpose {
	case PurposeTrackSource:
		err = tx.QueryRow(ctx, `select exists(select 1 from tracks where id = $1)`, upload.TargetID).Scan(&targetExists)
	case PurposeArtistArtwork:
		err = tx.QueryRow(ctx, `select exists(select 1 from artists where id = $1)`, upload.TargetID).Scan(&targetExists)
	case PurposeAlbumArtwork:
		err = tx.QueryRow(ctx, `select exists(select 1 from albums where id = $1)`, upload.TargetID).Scan(&targetExists)
	case PurposeUserAvatar:
		err = tx.QueryRow(ctx, `select exists(select 1 from users where id = $1)`, upload.TargetID).Scan(&targetExists)
	default:
		return apperror.Validation("Unsupported upload purpose")
	}
	if err != nil {
		return fmt.Errorf("verify upload target: %w", err)
	}
	if !targetExists {
		return apperror.NotFound("Upload target was not found")
	}

	_, err = tx.Exec(ctx, `
		insert into media_uploads (
			id, purpose, target_id, track_id, uploader_id, storage_path,
			expected_size, expected_checksum_sha256, expected_mime_type,
			original_file_name, status, expires_at, created_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'CREATED', $11, $12)`,
		upload.ID,
		upload.Purpose,
		upload.TargetID,
		upload.TrackID,
		upload.UploaderID,
		upload.StoragePath,
		upload.ExpectedSize,
		upload.ExpectedChecksumSHA256,
		upload.ExpectedMIMEType,
		upload.OriginalFileName,
		upload.ExpiresAt,
		upload.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert admin upload: %w", err)
	}

	return tx.Commit(ctx)
}

func (repository *Repository) FindUpload(ctx context.Context, uploadID string) (UploadReservation, error) {
	upload, err := scanUploadReservation(repository.pool.QueryRow(ctx, `
		select `+uploadReservationColumns+`
		from media_uploads
		where id = $1`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return UploadReservation{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return UploadReservation{}, fmt.Errorf("find admin upload: %w", err)
	}
	return upload, nil
}

func (repository *Repository) ClaimUploadCompletion(
	ctx context.Context,
	uploadID string,
	completionToken string,
	now time.Time,
	lease time.Duration,
) (CompletionClaim, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return CompletionClaim{}, fmt.Errorf("begin admin completion claim: %w", err)
	}
	defer tx.Rollback(ctx)

	upload, err := scanUploadReservation(tx.QueryRow(ctx, `
		select `+uploadReservationColumns+`
		from media_uploads
		where id = $1
		for update`, uploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionClaim{}, apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return CompletionClaim{}, fmt.Errorf("lock upload completion: %w", err)
	}
	if upload.Status == UploadStatusCompleted && upload.AssetID != nil {
		if err := tx.Commit(ctx); err != nil {
			return CompletionClaim{}, err
		}
		return CompletionClaim{Outcome: CompletionFinished, Upload: upload}, nil
	}
	if upload.Status == UploadStatusCompleting {
		stale := upload.CompletionStartedAt == nil || !upload.CompletionStartedAt.After(now.Add(-lease))
		if !stale {
			if err := tx.Commit(ctx); err != nil {
				return CompletionClaim{}, err
			}
			return CompletionClaim{Outcome: CompletionInProgress, Upload: upload}, nil
		}
	}
	if !upload.ExpiresAt.After(now) {
		_, _ = tx.Exec(ctx, `
			update media_uploads
			set status = 'EXPIRED', completion_token = null, completion_started_at = null
			where id = $1`, upload.ID)
		_ = tx.Commit(ctx)
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
		return CompletionClaim{}, fmt.Errorf("claim upload completion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletionClaim{}, fmt.Errorf("commit upload completion claim: %w", err)
	}
	upload.Status = UploadStatusCompleting
	upload.CompletionToken = &completionToken
	upload.CompletionStartedAt = &now
	upload.ExpiresAt = completionExpiresAt
	return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: completionToken}, nil
}

func (repository *Repository) UploadCompletionStatus(ctx context.Context, uploadID string) (string, error) {
	var status string
	err := repository.pool.QueryRow(ctx, `select status::text from media_uploads where id = $1`, uploadID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return "", fmt.Errorf("find upload completion status: %w", err)
	}
	return status, nil
}

func (repository *Repository) FinalizeUpload(ctx context.Context, input FinalizeUploadParams) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin finalize upload: %w", err)
	}
	defer tx.Rollback(ctx)

	upload, err := scanUploadReservation(tx.QueryRow(ctx, `
		select `+uploadReservationColumns+`
		from media_uploads where id = $1 for update`, input.UploadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Media upload was not found")
	}
	if err != nil {
		return fmt.Errorf("lock completing upload: %w", err)
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

	if input.CompletionFence != nil {
		if err := input.CompletionFence.Lock(ctx, tx); err != nil {
			return err
		}
	}

	var assetKind string
	switch upload.Purpose {
	case PurposeTrackSource:
		assetKind = "AUDIO_SOURCE"
	case PurposeArtistArtwork, PurposeAlbumArtwork, PurposeUserAvatar:
		assetKind = "ARTWORK"
	default:
		return errors.New("unsupported asset purpose")
	}

	if _, err := tx.Exec(ctx, `
		insert into media_assets (
			id, uploader_id, storage_path, kind, mime_type, size_bytes,
			checksum_sha256, width, height, status, created_at, updated_at
		) values ($1, $2, $3, $4::asset_kind, $5, $6, $7, $8, $9, 'READY', $10, $10)`,
		input.AssetID,
		upload.UploaderID,
		input.Inspected.StoragePath,
		assetKind,
		input.Inspected.MIMEType,
		input.Inspected.SizeBytes,
		input.Inspected.ChecksumSHA256,
		input.Inspected.Width,
		input.Inspected.Height,
		input.Now,
	); err != nil {
		return fmt.Errorf("insert media asset: %w", err)
	}

	// Update corresponding target table
	switch upload.Purpose {
	case PurposeTrackSource:
		duration := int64(0)
		if input.Inspected.DurationMs != nil {
			duration = *input.Inspected.DurationMs
		}
		if _, err := tx.Exec(ctx, `
			update tracks
			set source_asset_id = $2, duration_ms = $3, status = CASE WHEN status = 'ARCHIVED' THEN status ELSE 'READY' END,
				published_at = CASE WHEN status = 'ARCHIVED' THEN published_at ELSE COALESCE(published_at, $4) END, updated_at = $4
			where id = $1`, upload.TargetID, input.AssetID, duration, input.Now); err != nil {
			return fmt.Errorf("update track source asset: %w", err)
		}
	case PurposeArtistArtwork:
		command, err := tx.Exec(ctx, `
			update artists
			set artwork_asset_id = $2, version = version + 1, updated_at = $3
			where id = $1`, upload.TargetID, input.AssetID, input.Now)
		if err != nil {
			return fmt.Errorf("update artist artwork: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apperror.NotFound("Artist was not found")
		}
	case PurposeAlbumArtwork:
		command, err := tx.Exec(ctx, `
			update albums
			set cover_asset_id = $2, version = version + 1, updated_at = $3
			where id = $1`, upload.TargetID, input.AssetID, input.Now)
		if err != nil {
			return fmt.Errorf("update album cover: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apperror.NotFound("Album was not found")
		}
	case PurposeUserAvatar:
		var previousAssetID *string
		err = tx.QueryRow(ctx, `
			select avatar_asset_id
			from user_profiles
			where user_id = $1
			for update`, upload.TargetID).Scan(&previousAssetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NotFound("User profile was not found")
		}
		if err != nil {
			return fmt.Errorf("lock avatar profile: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update user_profiles
			set avatar_asset_id = $2, updated_at = $3
			where user_id = $1`, upload.TargetID, input.AssetID, input.Now); err != nil {
			return fmt.Errorf("attach avatar asset: %w", err)
		}
		command, err := tx.Exec(ctx, `
			update users
			set version = version + 1, updated_at = $2
			where id = $1`, upload.TargetID, input.Now)
		if err != nil {
			return fmt.Errorf("advance avatar user version: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apperror.NotFound("User no longer exists")
		}
		if previousAssetID != nil && *previousAssetID != input.AssetID {
			_, _ = tx.Exec(ctx, `
				update media_assets
				set status = 'DELETE_PENDING', updated_at = $2
				where id = $1
				  and not exists (select 1 from artists where artwork_asset_id = media_assets.id)
				  and not exists (select 1 from albums where cover_asset_id = media_assets.id)
				  and not exists (select 1 from playlists where cover_asset_id = media_assets.id)
				  and not exists (select 1 from user_profiles where avatar_asset_id = media_assets.id)`,
				*previousAssetID, input.Now)
		}
	}

	command, err := tx.Exec(ctx, `
		update media_uploads
		set status = 'COMPLETED', asset_id = $3, completed_at = $4,
			completion_token = null, completion_started_at = null
		where id = $1 and completion_token = $2`,
		input.UploadID, input.CompletionToken, input.AssetID, input.Now,
	)
	if err != nil {
		return fmt.Errorf("complete media upload record: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"Media upload completion ownership was lost",
			nil,
		)
	}

	return tx.Commit(ctx)
}

func (repository *Repository) FailUploadCompletion(
	ctx context.Context,
	uploadID string,
	completionToken string,
	retryable bool,
	reason string,
	now time.Time,
) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin fail upload completion: %w", err)
	}
	defer tx.Rollback(ctx)
	status := UploadStatusFailed
	if retryable {
		status = UploadStatusCreated
	}
	_, err = tx.Exec(ctx, `
		update media_uploads
		set status = $3, completion_token = null, completion_started_at = null,
			updated_at = $4
		where id = $1 and status = 'COMPLETING' and completion_token = $2`,
		uploadID, completionToken, status, now,
	)
	if err != nil {
		return fmt.Errorf("fail upload completion: %w", err)
	}
	return tx.Commit(ctx)
}

func (repository *Repository) AbandonUpload(ctx context.Context, actorID string, uploadID string) error {
	_, err := repository.pool.Exec(ctx, `
		update media_uploads
		set status = 'FAILED', updated_at = now()
		where id = $1 and uploader_id = $2 and status not in ('COMPLETED', 'FAILED')`,
		uploadID, actorID)
	return err
}

const uploadReservationColumns = `
	id, purpose::text, target_id, track_id, uploader_id, storage_path,
	expected_size, expected_checksum_sha256, expected_mime_type,
	original_file_name, status::text, asset_id, expires_at, created_at,
	completed_at, completion_token, completion_started_at`

type rowScanner interface {
	Scan(...any) error
}

func scanUploadReservation(row rowScanner) (UploadReservation, error) {
	var upload UploadReservation
	var trackID *string
	err := row.Scan(
		&upload.ID,
		&upload.Purpose,
		&upload.TargetID,
		&trackID,
		&upload.UploaderID,
		&upload.StoragePath,
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
	upload.TrackID = trackID
	return upload, err
}
