package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/tagwriteback"
)

func (repository *Repository) TrackAlbumID(ctx context.Context, trackID string) (*string, error) {
	var albumID *string
	err := repository.pool.QueryRow(ctx, "SELECT album_id FROM tracks WHERE id = $1", trackID).Scan(&albumID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return nil, fmt.Errorf("find track album: %w", err)
	}
	return albumID, nil
}

func (repository *Repository) EnqueueWriteback(
	ctx context.Context,
	actorID string,
	trackID string,
	expectedVersion int,
	reason string,
) (WritebackJob, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WritebackJob{}, fmt.Errorf("begin writeback enqueue: %w", err)
	}
	defer tx.Rollback(ctx)
	if fence := batchMutationFenceFromContext(ctx); fence != nil {
		if err := fence.Lock(ctx, mediaTxAdapter{tx: tx}); err != nil {
			return WritebackJob{}, err
		}
	}
	if err := repository.ensureMetadataWith(ctx, tx, trackID); err != nil {
		return WritebackJob{}, err
	}
	var initialSourceID, rootID string
	err = tx.QueryRow(ctx, `
		SELECT metadata.source_id::text, source.root_id::text
		FROM track_metadata metadata
		JOIN local_music_sources source ON source.id = metadata.source_id
		WHERE metadata.track_id = $1`, trackID).Scan(&initialSourceID, &rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("A writable local source for this track was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("find writeback lock order: %w", err)
	}
	var lockedRootID, lockedTrackID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM library_roots WHERE id = $1 FOR UPDATE`, rootID).Scan(&lockedRootID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("The music source for this track no longer exists")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("lock writeback root: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT id::text FROM tracks WHERE id = $1 FOR UPDATE`, trackID).Scan(&lockedTrackID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("Track was not found")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("lock writeback track: %w", err)
	}
	var rawJSON, overridesJSON []byte
	var sourceID, sourcePath, sourceStatus, checksum, currentRootID, rootPath, rootMode, trackStatus string
	var version int
	var rootEnabled, scanActive bool
	err = tx.QueryRow(ctx, `
		SELECT metadata.raw_tags, metadata.overrides, metadata.version,
		       source.id, source.source_path, source.status, source.checksum_sha256,
		       root.id::text, root.path, root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
			       ), track.status::text
		FROM track_metadata metadata
		JOIN tracks track ON track.id = metadata.track_id
		JOIN local_music_sources source ON source.id = metadata.source_id
		JOIN library_roots root ON root.id = source.root_id
		WHERE metadata.track_id = $1
		FOR UPDATE OF metadata, source`, trackID).Scan(
		&rawJSON, &overridesJSON, &version, &sourceID, &sourcePath, &sourceStatus,
		&checksum, &currentRootID, &rootPath, &rootMode, &rootEnabled, &scanActive, &trackStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("A writable local source for this track was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("lock writeback source: %w", err)
	}
	if sourceID != initialSourceID || currentRootID != rootID {
		return WritebackJob{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The local source changed while Tag writeback was being queued",
			nil,
		)
	}
	var mappingCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int
		FROM local_music_source_tracks WHERE source_id = $1`, sourceID).Scan(&mappingCount); err != nil {
		return WritebackJob{}, fmt.Errorf("inspect writeback source mappings: %w", err)
	}
	if version != expectedVersion {
		return WritebackJob{}, apperror.Conflict(apperror.CodeVersionConflict, "Track metadata version is stale", map[string]any{
			"expectedVersion": expectedVersion, "currentVersion": version,
		})
	}
	if err := tagwriteback.Evaluate(tagwriteback.SourceContext{
		HasSource: true, TrackStatus: trackStatus, RootMode: rootMode,
		RootEnabled: rootEnabled, ScanActive: scanActive, SourceStatus: sourceStatus,
		SourcePath: sourcePath, MappingCount: mappingCount,
	}).Error(trackID); err != nil {
		return WritebackJob{}, err
	}
	var conflictingWritebackID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM metadata_writeback_jobs
		WHERE source_id = $1 AND status IN ('PENDING','PROCESSING')
		ORDER BY created_at DESC LIMIT 1`, sourceID).Scan(&conflictingWritebackID)
	if err == nil {
		return WritebackJob{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Complete or cancel the existing Tag writeback before starting another",
			map[string]any{"writebackJobId": conflictingWritebackID},
		)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, fmt.Errorf("check conflicting metadata writeback: %w", err)
	}
	raw, overrides, err := decodeMetadataDocuments(rawJSON, overridesJSON)
	if err != nil {
		return WritebackJob{}, err
	}
	effective, err := applyOverrides(raw, overrides)
	if err != nil {
		return WritebackJob{}, err
	}
	snapshotJSON, _ := json.Marshal(effective)
	jobID := uuid.NewString()
	row := tx.QueryRow(ctx, `
		INSERT INTO metadata_writeback_jobs
			(id, track_id, root_id, source_id, target_path, original_checksum_sha256,
			 requested_by, reason, metadata_snapshot, metadata_version,
			 expected_source_checksum, root_path_snapshot, source_path_snapshot,
			 idempotency_key, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13,
			gen_random_uuid()::text, '{}'::jsonb)
		RETURNING id, track_id, source_id, status::text, stage, attempts,
		          max_attempts, cancel_requested, metadata_version, reason,
		          output_checksum_sha256, last_error_code, last_error,
		          version, next_attempt_at, started_at, completed_at, created_at, updated_at`,
		jobID, trackID, rootID, sourceID, sourcePath, checksum,
		actorID, reason, snapshotJSON, version, checksum, rootPath, sourcePath)
	job, err := scanWritebackJob(row)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return WritebackJob{}, apperror.Conflict(apperror.CodeResourceConflict, "A metadata writeback is already active for this source", nil)
		}
		return WritebackJob{}, fmt.Errorf("insert metadata writeback job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return WritebackJob{}, fmt.Errorf("commit metadata writeback enqueue: %w", err)
	}
	return job, nil
}
