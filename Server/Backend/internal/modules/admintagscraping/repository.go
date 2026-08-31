package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
	"xymusic/server/internal/shared/tagwriteback"
)

type Repository struct {
	pool *pgxpool.Pool
}

type metadataDatabase interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

var _ Store = (*Repository)(nil)
var _ BatchClaimStore = (*Repository)(nil)
var _ BatchCompleteStore = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) FingerprintSource(ctx context.Context, trackID string) (FingerprintSource, error) {
	var trackStatus string
	var sourcePath, rootPath *string
	var startMS, endMS *int
	err := repository.pool.QueryRow(ctx, `
		SELECT track.status::text, source.source_path, source.root_path, source.start_ms, source.end_ms
		FROM tracks track
		LEFT JOIN LATERAL (
			SELECT local_source.source_path, COALESCE(root.path, '') AS root_path,
			       mapping.start_ms, mapping.end_ms
			FROM local_music_source_tracks mapping
			JOIN local_music_sources local_source ON local_source.id = mapping.source_id
			LEFT JOIN library_roots root ON root.id = local_source.root_id
			WHERE mapping.track_id = track.id
			ORDER BY CASE local_source.status WHEN 'READY' THEN 0 WHEN 'PROCESSING' THEN 1 ELSE 2 END,
			         local_source.updated_at DESC, local_source.id
			LIMIT 1
		) source ON true
		WHERE track.id = $1`, trackID).Scan(&trackStatus, &sourcePath, &rootPath, &startMS, &endMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return FingerprintSource{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return FingerprintSource{}, fmt.Errorf("find fingerprint source: %w", err)
	}
	if trackIsArchived(trackStatus) {
		return FingerprintSource{}, archivedTrackError(trackID)
	}
	if sourcePath == nil || startMS == nil {
		return FingerprintSource{}, apperror.NotFound("The track has no local source available for fingerprinting")
	}
	return FingerprintSource{
		SourcePath: *sourcePath,
		RootPath:   pointerValue(rootPath),
		StartMS:    *startMS,
		EndMS:      endMS,
	}, nil
}

func (repository *Repository) Metadata(ctx context.Context, trackID string) (TrackMetadata, error) {
	fence := batchMutationFenceFromContext(ctx)
	if fence == nil {
		if err := repository.ensureMetadata(ctx, trackID); err != nil {
			return TrackMetadata{}, err
		}
		return repository.loadMetadata(ctx, trackID)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("begin fenced metadata load: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fence.Lock(ctx, tx); err != nil {
		return TrackMetadata{}, err
	}
	if err := repository.ensureMetadataWith(ctx, tx, trackID); err != nil {
		return TrackMetadata{}, err
	}
	result, err := repository.loadMetadataWith(ctx, tx, trackID)
	if err != nil {
		return TrackMetadata{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TrackMetadata{}, fmt.Errorf("commit fenced metadata load: %w", err)
	}
	return result, nil
}

func (repository *Repository) UpdateMetadata(
	ctx context.Context,
	actorID string,
	trackID string,
	expectedVersion int,
	patch MetadataPatch,
	reason string,
) (TrackMetadata, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("begin metadata update: %w", err)
	}
	defer tx.Rollback(ctx)
	if fence := batchMutationFenceFromContext(ctx); fence != nil {
		if err := fence.Lock(ctx, tx); err != nil {
			return TrackMetadata{}, err
		}
	}
	var trackStatus string
	err = tx.QueryRow(ctx, "SELECT status::text FROM tracks WHERE id = $1 FOR UPDATE", trackID).Scan(&trackStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackMetadata{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("lock track for metadata update: %w", err)
	}
	if trackIsArchived(trackStatus) {
		return TrackMetadata{}, archivedTrackError(trackID)
	}
	if err := repository.ensureMetadataWith(ctx, tx, trackID); err != nil {
		return TrackMetadata{}, err
	}
	var rawJSON, overridesJSON []byte
	var version int
	err = tx.QueryRow(ctx, `
		SELECT raw_tags, overrides, version
		FROM track_metadata WHERE track_id = $1 FOR UPDATE`, trackID).Scan(&rawJSON, &overridesJSON, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackMetadata{}, apperror.NotFound("Track metadata was not found")
	}
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("lock track metadata: %w", err)
	}
	if version != expectedVersion {
		return TrackMetadata{}, apperror.Conflict(apperror.CodeVersionConflict, "Track metadata version is stale", map[string]any{
			"expectedVersion": expectedVersion,
			"currentVersion":  version,
		})
	}
	raw, overrides, err := decodeMetadataDocuments(rawJSON, overridesJSON)
	if err != nil {
		return TrackMetadata{}, err
	}
	nextOverrides := cloneMap(overrides)
	for field, value := range patch {
		if !editableMetadataField(field) {
			return TrackMetadata{}, apperror.Validation("The metadata patch contains an unknown field")
		}
		nextOverrides[field] = value
	}
	if reflect.DeepEqual(normalizeComparable(overrides), normalizeComparable(nextOverrides)) {
		return TrackMetadata{}, apperror.Conflict(apperror.CodeResourceConflict, "The metadata edit does not change any field", nil)
	}
	previousEffective, err := applyOverrides(raw, overrides)
	if err != nil {
		return TrackMetadata{}, err
	}
	nextEffective, err := applyOverrides(raw, nextOverrides)
	if err != nil {
		return TrackMetadata{}, err
	}
	nextOverridesJSON, _ := json.Marshal(nextOverrides)
	nextVersion := version + 1
	command, err := tx.Exec(ctx, `
		UPDATE track_metadata
		SET overrides = $1::jsonb, updated_by = $2, version = $3, updated_at = now()
		WHERE track_id = $4 AND version = $5`, nextOverridesJSON, actorID, nextVersion, trackID, version)
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("update track metadata: %w", err)
	}
	if command.RowsAffected() != 1 {
		return TrackMetadata{}, apperror.Conflict(apperror.CodeVersionConflict, "Track metadata version is stale", map[string]any{
			"expectedVersion": expectedVersion,
			"currentVersion":  version,
		})
	}
	if err := repository.projectMetadata(ctx, tx, trackID, nextEffective, previousEffective); err != nil {
		return TrackMetadata{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TrackMetadata{}, fmt.Errorf("commit metadata update: %w", err)
	}
	return repository.loadMetadata(ctx, trackID)
}

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
		if err := fence.Lock(ctx, tx); err != nil {
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
	var cue bool
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int, COALESCE(bool_or(cue_path IS NOT NULL), false)
		FROM local_music_source_tracks WHERE source_id = $1`, sourceID).Scan(&mappingCount, &cue); err != nil {
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
		SourcePath: sourcePath, MappingCount: mappingCount, Cue: cue,
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
			SELECT mapping.source_id, count(*)::int AS mapping_count,
			       COALESCE(bool_or(mapping.cue_path IS NOT NULL), false) AS cue
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
		       COALESCE(source_stats.mapping_count, 0), COALESCE(source_stats.cue, false)
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
		var cue *bool
		if err := rows.Scan(
			&trackID, &trackStatus, &sourceID, &sourcePath, &sourceStatus,
			&rootMode, &rootEnabled, &scanActive, &mappingCount, &cue,
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
			Cue: boolPointerValue(cue),
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

func (repository *Repository) ClaimBatchItem(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (ClaimResult, error) {
	result, err := repository.claimBatchItems(ctx, workerID, now, lease, 1)
	if err != nil {
		return ClaimResult{}, err
	}
	if result.FinishJobID != "" {
		return ClaimResult{FinishJobID: result.FinishJobID}, nil
	}
	if len(result.Items) == 0 {
		return ClaimResult{}, nil
	}
	item := result.Items[0]
	return ClaimResult{Item: &item}, nil
}

func (repository *Repository) ClaimBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (BatchClaimResult, error) {
	return repository.claimBatchItems(ctx, workerID, now, lease, limit)
}

func (repository *Repository) claimBatchItems(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
	limit int,
) (BatchClaimResult, error) {
	if limit <= 0 {
		return BatchClaimResult{}, nil
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("begin tag scraping claim: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := scanBatchJob(tx.QueryRow(ctx, batchJobSelect+`
		WHERE status IN ('PENDING','RUNNING')
		  AND (
			tag_scraping_jobs.cancel_requested
			OR
			EXISTS (
				SELECT 1 FROM tag_scraping_job_items claimable
				WHERE claimable.job_id = tag_scraping_jobs.id
				  AND claimable.status = 'PENDING'
				  AND claimable.next_attempt_at <= GREATEST($1::timestamptz, clock_timestamp())
				  AND claimable.attempts < claimable.max_attempts
			) OR EXISTS (
				SELECT 1 FROM tag_scraping_job_items recoverable
				WHERE recoverable.job_id = tag_scraping_jobs.id
				  AND (
					recoverable.status = 'PENDING' AND recoverable.attempts >= recoverable.max_attempts
					OR recoverable.status = 'RUNNING'
					   AND (recoverable.locked_until IS NULL OR recoverable.locked_until < GREATEST($1::timestamptz, clock_timestamp()))
				  )
			) OR NOT EXISTS (
				SELECT 1 FROM tag_scraping_job_items active
				WHERE active.job_id = tag_scraping_jobs.id
				  AND active.status IN ('PENDING','RUNNING')
			)
		  )
		ORDER BY created_at, id FOR UPDATE SKIP LOCKED LIMIT 1`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return BatchClaimResult{}, fmt.Errorf("commit empty tag scraping claim: %w", commitErr)
		}
		return BatchClaimResult{}, nil
	}
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("claim tag scraping batch: %w", err)
	}
	if err := recoverExpiredBatchItems(ctx, tx, now, &job.ID); err != nil {
		return BatchClaimResult{}, err
	}
	if job.CancelRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE tag_scraping_job_items SET
				status = 'SKIPPED', attempt_id = NULL, locked_by = NULL, locked_until = NULL,
				message = 'The batch was cancelled', completed_at = $2, updated_at = $2
			WHERE job_id = $1 AND (
				status = 'PENDING' OR (status = 'RUNNING' AND (locked_until IS NULL OR locked_until < $2))
			)`, job.ID, now); err != nil {
			return BatchClaimResult{}, fmt.Errorf("skip cancelled tag scraping items: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return BatchClaimResult{}, fmt.Errorf("commit cancelled tag scraping claim: %w", err)
		}
		return BatchClaimResult{FinishJobID: job.ID}, nil
	}
	rows, err := tx.Query(ctx, claimedBatchItemSelect+`
		WHERE item.job_id = $1 AND item.status = 'PENDING'
		  AND item.next_attempt_at <= GREATEST($2::timestamptz, clock_timestamp())
		  AND item.attempts < item.max_attempts
		ORDER BY item.position FOR UPDATE OF item SKIP LOCKED LIMIT $3`, job.ID, now, limit)
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("query tag scraping items: %w", err)
	}
	type pendingClaim struct {
		item     BatchItemRecord
		metadata *TrackMetadata
	}
	claims := make([]pendingClaim, 0, limit)
	for rows.Next() {
		item, metadata, scanErr := scanClaimedBatchItem(rows)
		if scanErr != nil {
			rows.Close()
			return BatchClaimResult{}, fmt.Errorf("scan tag scraping item: %w", scanErr)
		}
		claims = append(claims, pendingClaim{item: item, metadata: metadata})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BatchClaimResult{}, fmt.Errorf("iterate tag scraping items: %w", err)
	}
	rows.Close()
	if len(claims) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return BatchClaimResult{}, fmt.Errorf("commit empty tag scraping claim: %w", err)
		}
		return BatchClaimResult{FinishJobID: job.ID}, nil
	}
	itemIDs := make([]string, len(claims))
	for index := range claims {
		itemIDs[index] = claims[index].item.ID
	}
	attemptRows, err := tx.Query(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'RUNNING', attempts = attempts + 1, attempt_id = gen_random_uuid(),
			locked_by = $3, locked_until = $4, started_at = $5, completed_at = NULL,
			updated_at = $5
		WHERE id = ANY($6::uuid[]) AND job_id = $1
		  AND status = 'PENDING' AND next_attempt_at <= GREATEST($2::timestamptz, clock_timestamp())
		  AND attempts < max_attempts
		RETURNING id::text, attempt_id::text`, job.ID, now, workerID, now.Add(lease), now, itemIDs)
	if err != nil {
		return BatchClaimResult{}, fmt.Errorf("own tag scraping items: %w", err)
	}
	attemptIDs := make(map[string]string, len(claims))
	for attemptRows.Next() {
		var itemID, attemptID string
		if err := attemptRows.Scan(&itemID, &attemptID); err != nil {
			attemptRows.Close()
			return BatchClaimResult{}, fmt.Errorf("scan tag scraping attempt: %w", err)
		}
		attemptIDs[itemID] = attemptID
	}
	if err := attemptRows.Err(); err != nil {
		attemptRows.Close()
		return BatchClaimResult{}, fmt.Errorf("iterate tag scraping attempts: %w", err)
	}
	attemptRows.Close()
	if len(attemptIDs) != len(claims) {
		return BatchClaimResult{}, errors.New("claimed tag scraping items disappeared")
	}
	if job.Status == JobPending {
		if _, err := tx.Exec(ctx, `
			UPDATE tag_scraping_jobs SET status = 'RUNNING', started_at = COALESCE(started_at, $2), updated_at = $2
			WHERE id = $1`, job.ID, now); err != nil {
			return BatchClaimResult{}, fmt.Errorf("start tag scraping batch: %w", err)
		}
		job.Status = JobRunning
		if job.StartedAt == nil {
			started := now
			job.StartedAt = &started
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchClaimResult{}, fmt.Errorf("commit tag scraping claim: %w", err)
	}
	lockedUntil := now.Add(lease)
	result := BatchClaimResult{Items: make([]ClaimedBatchItem, len(claims))}
	for index, claim := range claims {
		attemptID := attemptIDs[claim.item.ID]
		claim.item.Status = ItemRunning
		claim.item.Attempts++
		claim.item.AttemptID = &attemptID
		claim.item.LockedBy = &workerID
		claim.item.LockedUntil = &lockedUntil
		claim.item.StartedAt = &now
		result.Items[index] = ClaimedBatchItem{
			Job: job, Item: claim.item, AttemptID: attemptID, Metadata: claim.metadata,
		}
	}
	return result, nil
}

func (repository *Repository) RenewBatchItemLease(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	lockedUntil time.Time,
) (BatchLeaseControl, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("begin tag scraping lease renewal: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested FROM tag_scraping_jobs
		WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, nil
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping lease job: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return BatchLeaseControl{}, nil
	}
	var itemStatus string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, itemID, jobID).Scan(&itemStatus, &currentAttempt, &currentWorker, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, nil
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping lease item: %w", err)
	}
	if itemStatus != string(ItemRunning) || currentAttempt == nil || *currentAttempt != attemptID ||
		currentWorker == nil || *currentWorker != workerID || !leaseActive {
		return BatchLeaseControl{}, nil
	}
	if !cancelRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE tag_scraping_job_items SET locked_until = $2, updated_at = now()
			WHERE id = $1`, itemID, lockedUntil); err != nil {
			return BatchLeaseControl{}, fmt.Errorf("renew tag scraping item lease: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchLeaseControl{}, fmt.Errorf("commit tag scraping lease renewal: %w", err)
	}
	return BatchLeaseControl{Owned: true, CancelRequested: cancelRequested}, nil
}

func (repository *Repository) BatchCancelRequested(ctx context.Context, jobID string) (bool, error) {
	var requested bool
	err := repository.pool.QueryRow(ctx, "SELECT cancel_requested FROM tag_scraping_jobs WHERE id = $1", jobID).Scan(&requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read tag scraping cancellation: %w", err)
	}
	return requested, nil
}

func (repository *Repository) RetryBatchItem(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	candidate *Candidate,
	message string,
	nextAttemptAt time.Time,
	now time.Time,
) (BatchLeaseControl, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("begin tag scraping item retry: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, cancel_requested
		FROM tag_scraping_jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping item retry job: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	var itemStatus string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, itemID, jobID).Scan(&itemStatus, &currentAttempt, &currentWorker, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("lock tag scraping item retry item: %w", err)
	}
	if itemStatus != string(ItemRunning) || currentAttempt == nil || *currentAttempt != attemptID ||
		currentWorker == nil || *currentWorker != workerID || !leaseActive {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if cancelRequested {
		if err := tx.Commit(ctx); err != nil {
			return BatchLeaseControl{}, fmt.Errorf("commit cancelled tag scraping retry check: %w", err)
		}
		return BatchLeaseControl{Owned: true, CancelRequested: true}, nil
	}
	var candidateJSON []byte
	var source *string
	if candidate != nil {
		candidateJSON, err = json.Marshal(candidate)
		if err != nil {
			return BatchLeaseControl{}, fmt.Errorf("encode tag scraping retry candidate: %w", err)
		}
		value := string(candidate.Source)
		source = &value
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	command, err := tx.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'PENDING', next_attempt_at = $5,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = $6::jsonb, source = $7, message = $8,
			started_at = NULL, completed_at = NULL, updated_at = $9
		WHERE id = $1 AND job_id = $2 AND attempt_id = $3 AND locked_by = $4
		  AND status = 'RUNNING' AND attempts < max_attempts`,
		itemID, jobID, attemptID, workerID, nextAttemptAt,
		nullableJSON(candidateJSON), source, message, now,
	)
	if err != nil {
		return BatchLeaseControl{}, fmt.Errorf("requeue tag scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return BatchLeaseControl{}, ErrBatchLeaseLost
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchLeaseControl{}, fmt.Errorf("commit tag scraping item retry: %w", err)
	}
	return BatchLeaseControl{Owned: true}, nil
}

func (repository *Repository) CompleteBatchItem(
	ctx context.Context,
	jobID string,
	itemID string,
	attemptID string,
	workerID string,
	status ItemStatus,
	candidate *Candidate,
	message string,
	now time.Time,
) (bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin tag scraping item completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var candidateJSON []byte
	var source *string
	if candidate != nil {
		candidateJSON, _ = json.Marshal(candidate)
		value := string(candidate.Source)
		source = &value
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `SELECT status::text, cancel_requested FROM tag_scraping_jobs
		WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrBatchLeaseLost
	}
	if err != nil {
		return false, fmt.Errorf("lock tag scraping item completion: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return false, ErrBatchLeaseLost
	}
	var itemStatus string
	var currentAttempt, currentWorker *string
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT status::text, attempt_id::text, locked_by,
		       COALESCE(locked_until > clock_timestamp(), false)
		FROM tag_scraping_job_items
		WHERE id = $1 AND job_id = $2
		FOR UPDATE`, itemID, jobID).Scan(&itemStatus, &currentAttempt, &currentWorker, &leaseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrBatchLeaseLost
	}
	if err != nil {
		return false, fmt.Errorf("lock tag scraping completion item: %w", err)
	}
	if itemStatus != string(ItemRunning) || currentAttempt == nil || *currentAttempt != attemptID ||
		currentWorker == nil || *currentWorker != workerID || !leaseActive {
		return false, ErrBatchLeaseLost
	}
	finalStatus := status
	if cancelRequested {
		finalStatus, candidateJSON, source, message = ItemSkipped, nil, nil, "The batch was cancelled"
	}
	command, err := tx.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = $4, attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = $5::jsonb, source = $6, message = $7,
			completed_at = $8, updated_at = $8
		WHERE id = $1 AND job_id = $2 AND attempt_id = $3`, itemID, jobID, attemptID,
		string(finalStatus), nullableJSON(candidateJSON), source, message, now)
	if err != nil {
		return false, fmt.Errorf("complete tag scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return false, ErrBatchLeaseLost
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tag_scraping_jobs SET
			processed = processed + 1,
			succeeded = succeeded + $2,
			failed = failed + $3,
			updated_at = $4
		WHERE id = $1`, jobID, boolInt(finalStatus == ItemSucceeded), boolInt(finalStatus == ItemFailed), now); err != nil {
		return false, fmt.Errorf("update tag scraping batch counts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tag scraping item completion: %w", err)
	}
	return true, nil
}

type batchCompletionPayload struct {
	ItemID    string     `json:"item_id"`
	AttemptID string     `json:"attempt_id"`
	Status    ItemStatus `json:"status"`
	Candidate *Candidate `json:"candidate,omitempty"`
	Source    *string    `json:"source,omitempty"`
	Message   string     `json:"message"`
}

// CompleteBatchItems keeps the attempt and lease checks in the UPDATE
// predicate, so a stale result can be omitted without affecting newer work.
// The job row is locked once for the whole completion window and counts are
// advanced only for rows that passed that predicate.
func (repository *Repository) CompleteBatchItems(
	ctx context.Context,
	jobID string,
	workerID string,
	completions []BatchItemCompletion,
	now time.Time,
) ([]string, error) {
	if len(completions) == 0 {
		return nil, nil
	}
	payload := make([]batchCompletionPayload, len(completions))
	seenItems := make(map[string]struct{}, len(completions))
	for index, completion := range completions {
		if completion.ItemID == "" || completion.AttemptID == "" {
			return nil, errors.New("tag scraping batch completion is missing an item or attempt")
		}
		if _, exists := seenItems[completion.ItemID]; exists {
			return nil, fmt.Errorf("tag scraping batch completion contains duplicate item %q", completion.ItemID)
		}
		seenItems[completion.ItemID] = struct{}{}
		var source *string
		if completion.Candidate != nil {
			value := string(completion.Candidate.Source)
			source = &value
		}
		payload[index] = batchCompletionPayload{
			ItemID: completion.ItemID, AttemptID: completion.AttemptID, Status: completion.Status,
			Candidate: completion.Candidate, Source: source, Message: completion.Message,
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode tag scraping batch completion: %w", err)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tag scraping batch completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobStatus string
	var cancelRequested bool
	err = tx.QueryRow(ctx, `SELECT status::text,cancel_requested
		FROM tag_scraping_jobs WHERE id=$1 FOR UPDATE`, jobID).Scan(&jobStatus, &cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBatchLeaseLost
	}
	if err != nil {
		return nil, fmt.Errorf("lock tag scraping batch completion: %w", err)
	}
	if jobStatus != string(JobPending) && jobStatus != string(JobRunning) {
		return nil, ErrBatchLeaseLost
	}
	rows, err := tx.Query(ctx, `
		WITH input AS (
			SELECT item_id,attempt_id,status,candidate,source,message
			FROM jsonb_to_recordset($3::jsonb) AS input(
				item_id uuid,attempt_id uuid,status text,candidate jsonb,source text,message text
			)
		)
		UPDATE tag_scraping_job_items item SET
			status = CASE WHEN $4 THEN 'SKIPPED'::tag_scraping_item_status
				ELSE input.status::tag_scraping_item_status END,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			candidate = CASE WHEN $4 THEN NULL::jsonb ELSE input.candidate END,
			source = CASE WHEN $4 THEN NULL::varchar ELSE input.source END,
			message = CASE WHEN $4 THEN 'The batch was cancelled' ELSE LEFT(input.message, 4000) END,
			completed_at = $5, updated_at = $5
		FROM input
		WHERE item.id = input.item_id AND item.job_id = $1
			AND item.status = 'RUNNING'
			AND item.attempt_id = input.attempt_id
			AND item.locked_by = $2
			AND COALESCE(item.locked_until > clock_timestamp(), false)
		RETURNING item.id::text,item.status::text`, jobID, workerID, encoded, cancelRequested, now)
	if err != nil {
		return nil, fmt.Errorf("complete tag scraping batch items: %w", err)
	}
	completedIDs := make([]string, 0, len(completions))
	succeeded, failed := 0, 0
	for rows.Next() {
		var itemID, status string
		if err := rows.Scan(&itemID, &status); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan tag scraping batch completion: %w", err)
		}
		completedIDs = append(completedIDs, itemID)
		switch ItemStatus(status) {
		case ItemSucceeded:
			succeeded++
		case ItemFailed:
			failed++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate tag scraping batch completion: %w", err)
	}
	rows.Close()
	if len(completedIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE tag_scraping_jobs SET
			processed=processed+$2,succeeded=succeeded+$3,failed=failed+$4,updated_at=$5
			WHERE id=$1`, jobID, len(completedIDs), succeeded, failed, now); err != nil {
			return nil, fmt.Errorf("update tag scraping batch counts: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tag scraping batch completion: %w", err)
	}
	return completedIDs, nil
}

func (repository *Repository) ReleaseBatchItem(
	ctx context.Context,
	itemID string,
	attemptID string,
	workerID string,
	now time.Time,
) error {
	command, err := repository.pool.Exec(ctx, `
		UPDATE tag_scraping_job_items SET
			status = 'PENDING', attempts = GREATEST(attempts - 1, 0), next_attempt_at = $4,
			attempt_id = NULL, locked_by = NULL, locked_until = NULL,
			started_at = NULL, updated_at = $4
		WHERE id = $1 AND status = 'RUNNING' AND attempt_id = $2 AND locked_by = $3`,
		itemID, attemptID, workerID, now)
	if err != nil {
		return fmt.Errorf("release tag scraping item: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrBatchLeaseLost
	}
	return nil
}

func (repository *Repository) FinishBatch(ctx context.Context, jobID string, now time.Time) (bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin tag scraping finish: %w", err)
	}
	defer tx.Rollback(ctx)
	var cancelRequested bool
	err = tx.QueryRow(ctx, "SELECT cancel_requested FROM tag_scraping_jobs WHERE id = $1 FOR UPDATE", jobID).Scan(&cancelRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, apperror.NotFound("Tag scraping batch was not found")
	}
	if err != nil {
		return false, fmt.Errorf("lock tag scraping finish: %w", err)
	}
	var total, active, succeeded, failed int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE status IN ('PENDING','RUNNING'))::int,
		       count(*) FILTER (WHERE status = 'SUCCEEDED')::int,
		       count(*) FILTER (WHERE status = 'FAILED')::int
		FROM tag_scraping_job_items WHERE job_id = $1`, jobID).Scan(&total, &active, &succeeded, &failed); err != nil {
		return false, fmt.Errorf("count tag scraping items: %w", err)
	}
	if active > 0 {
		return false, nil
	}
	status := JobCompleted
	if cancelRequested {
		status = JobCancelled
	} else if failed > 0 {
		status = JobFailed
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tag_scraping_jobs SET
			status = $2, processed = $3, succeeded = $4, failed = $5,
			completed_at = $6, updated_at = $6
		WHERE id = $1 AND status IN ('PENDING','RUNNING')`,
		jobID, string(status), total, succeeded, failed, now); err != nil {
		return false, fmt.Errorf("finish tag scraping batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tag scraping finish: %w", err)
	}
	return true, nil
}

func (repository *Repository) ensureMetadata(ctx context.Context, trackID string) error {
	return repository.ensureMetadataWith(ctx, repository.pool, trackID)
}

func (repository *Repository) ensureMetadataWith(ctx context.Context, database metadataDatabase, trackID string) error {
	command, err := database.Exec(ctx, `
		INSERT INTO track_metadata (track_id, source_id, raw_tags, raw_checksum_sha256, last_scanned_at)
		SELECT track.id, source.id,
			jsonb_build_object(
				'title', track.title,
				'credits', COALESCE((
					SELECT jsonb_agg(jsonb_build_object('name', artist.name, 'role', credit.role)
					                 ORDER BY credit.sort_order, artist.name)
					FROM track_artists credit JOIN artists artist ON artist.id = credit.artist_id
					WHERE credit.track_id = track.id
				), '[{"name":"Unknown Artist","role":"PRIMARY"}]'::jsonb),
				'albumArtists', COALESCE((
					SELECT jsonb_agg(artist.name ORDER BY credit.sort_order, artist.name)
					FROM album_artists credit JOIN artists artist ON artist.id = credit.artist_id
					WHERE credit.album_id = track.album_id AND credit.role = 'PRIMARY'
				), (
					SELECT jsonb_agg(artist.name ORDER BY credit.sort_order, artist.name)
					FROM track_artists credit JOIN artists artist ON artist.id = credit.artist_id
					WHERE credit.track_id = track.id AND credit.role = 'PRIMARY'
				), '["Unknown Artist"]'::jsonb),
				'album', album.title, 'releaseDate', album.release_date,
				'trackNumber', track.track_number, 'trackTotal', NULL,
				'discNumber', track.disc_number, 'discTotal', NULL,
				'genres', '[]'::jsonb, 'bpm', NULL, 'isrc', NULL,
				'comment', NULL, 'copyright', NULL,
					'lyrics', (
						SELECT jsonb_build_object('content', lyric.content, 'format', lyric.format, 'language', lyric.language, 'timing', lyric.timing)
					FROM lyrics lyric WHERE lyric.track_id = track.id AND lyric.content IS NOT NULL
					ORDER BY lyric.is_default DESC, lyric.created_at, lyric.id LIMIT 1
				),
				'hasArtwork', album.cover_asset_id IS NOT NULL
			), source.checksum_sha256, source.updated_at
		FROM tracks track
		LEFT JOIN albums album ON album.id = track.album_id
		LEFT JOIN LATERAL (
			SELECT local_source.id, local_source.checksum_sha256, local_source.updated_at
			FROM local_music_source_tracks mapping
			JOIN local_music_sources local_source ON local_source.id = mapping.source_id
			WHERE mapping.track_id = track.id
			ORDER BY CASE local_source.status WHEN 'READY' THEN 0 WHEN 'PROCESSING' THEN 1 ELSE 2 END,
			         local_source.updated_at DESC, local_source.id LIMIT 1
		) source ON true
		WHERE track.id = $1
		ON CONFLICT (track_id) DO NOTHING`, trackID)
	if err != nil {
		return fmt.Errorf("ensure track metadata: %w", err)
	}
	if command.RowsAffected() == 0 {
		var exists bool
		if err := database.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM track_metadata WHERE track_id = $1)", trackID).Scan(&exists); err != nil {
			return fmt.Errorf("check track metadata: %w", err)
		}
		if !exists {
			return apperror.NotFound("Track was not found")
		}
	}
	return nil
}

func (repository *Repository) loadMetadata(ctx context.Context, trackID string) (TrackMetadata, error) {
	return repository.loadMetadataWith(ctx, repository.pool, trackID)
}

func (repository *Repository) loadMetadataWith(ctx context.Context, database metadataDatabase, trackID string) (TrackMetadata, error) {
	return scanMetadataRow(database.QueryRow(ctx, `
		SELECT metadata.track_id, metadata.raw_tags, metadata.overrides, metadata.version,
		       metadata.last_scanned_at, metadata.updated_by, metadata.created_at, metadata.updated_at,
		       source.id, source.root_id, source.source_path, source.status, source.checksum_sha256,
		       root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
			       ), track.status::text,
		       COALESCE(mapping_stats.mapping_count, 0), COALESCE(mapping_stats.cue, false)
		FROM track_metadata metadata
		LEFT JOIN local_music_sources source ON source.id = metadata.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN tracks track ON track.id = metadata.track_id
		LEFT JOIN LATERAL (
			SELECT count(*)::int AS mapping_count,
			       COALESCE(bool_or(mapping.cue_path IS NOT NULL), false) AS cue
			FROM local_music_source_tracks mapping WHERE mapping.source_id = source.id
		) mapping_stats ON true
		WHERE metadata.track_id = $1`, trackID))
}

type metadataRowScanner interface {
	Scan(...any) error
}

func scanMetadataRow(row metadataRowScanner) (TrackMetadata, error) {
	var values trackMetadataRowValues
	err := row.Scan(values.scanTargets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackMetadata{}, apperror.NotFound("Track metadata was not found")
	}
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("load track metadata: %w", err)
	}
	result, err := values.build()
	if err != nil {
		return TrackMetadata{}, err
	}
	return result, nil
}

func (repository *Repository) MetadataBatch(
	ctx context.Context,
	trackIDs []string,
) (map[string]TrackMetadata, error) {
	result := make(map[string]TrackMetadata, len(trackIDs))
	if len(trackIDs) == 0 {
		return result, nil
	}
	identifiers := make([]uuid.UUID, len(trackIDs))
	for index, trackID := range trackIDs {
		identifier, parseErr := uuid.Parse(trackID)
		if parseErr != nil {
			return nil, fmt.Errorf("parse track metadata batch id: %w", parseErr)
		}
		identifiers[index] = identifier
	}
	rows, err := repository.pool.Query(ctx, `
		WITH source_stats AS (
			SELECT mapping.source_id, count(*)::int AS mapping_count,
			       COALESCE(bool_or(mapping.cue_path IS NOT NULL), false) AS cue
			FROM local_music_source_tracks mapping
			WHERE mapping.source_id IN (
				SELECT source_id FROM track_metadata
				WHERE track_id = ANY($1::uuid[]) AND source_id IS NOT NULL
			)
			GROUP BY mapping.source_id
		)
		SELECT metadata.track_id, metadata.raw_tags, metadata.overrides, metadata.version,
		       metadata.last_scanned_at, metadata.updated_by, metadata.created_at, metadata.updated_at,
		       source.id, source.root_id, source.source_path, source.status, source.checksum_sha256,
		       root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
		       ), track.status::text,
		       COALESCE(source_stats.mapping_count, 0), COALESCE(source_stats.cue, false)
		FROM track_metadata metadata
		LEFT JOIN local_music_sources source ON source.id = metadata.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN tracks track ON track.id = metadata.track_id
		LEFT JOIN source_stats ON source_stats.source_id = source.id
		WHERE metadata.track_id = ANY($1::uuid[])`, identifiers)

	if err != nil {
		return nil, fmt.Errorf("query track metadata batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		metadata, scanErr := scanMetadataRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result[metadata.TrackID] = metadata
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate track metadata batch: %w", err)
	}
	return result, nil
}

func (repository *Repository) projectMetadata(
	ctx context.Context,
	tx pgx.Tx,
	trackID string,
	metadata MetadataSnapshot,
	previous MetadataSnapshot,
) error {
	var currentAlbumID, currentCoverID *string
	err := tx.QueryRow(ctx, `
		SELECT track.album_id, album.cover_asset_id
		FROM tracks track LEFT JOIN albums album ON album.id = track.album_id
		WHERE track.id = $1`, trackID).Scan(&currentAlbumID, &currentCoverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Track was not found")
	}
	if err != nil {
		return fmt.Errorf("load metadata projection context: %w", err)
	}
	artistIDs := make(map[string]string)
	names := make([]string, 0, len(metadata.Credits)+len(metadata.AlbumArtists))
	for _, credit := range metadata.Credits {
		names = append(names, credit.Name)
	}
	names = append(names, metadata.AlbumArtists...)
	for _, name := range names {
		normalized := normalizeLookup(name)
		if _, exists := artistIDs[normalized]; exists {
			continue
		}
		var artistID string
		err := tx.QueryRow(ctx, `
			SELECT id FROM artists WHERE normalized_name = $1 ORDER BY id LIMIT 1`, normalized).Scan(&artistID)
		if errors.Is(err, pgx.ErrNoRows) {
			artistID = uuid.NewString()
			if _, err := tx.Exec(ctx, `
				INSERT INTO artists (id, name, normalized_name) VALUES ($1, $2, $3)`, artistID, name, normalized); err != nil {
				return fmt.Errorf("create projected artist: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("find projected artist: %w", err)
		}
		artistIDs[normalized] = artistID
	}

	var albumID *string
	if metadata.Album != nil && strings.TrimSpace(*metadata.Album) != "" {
		normalizedTitle := normalizeLookup(*metadata.Album)
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", normalizedTitle); err != nil {
			return fmt.Errorf("lock projected album identity: %w", err)
		}
		type albumCandidate struct {
			id, releaseDate string
			coverID         *string
		}
		rows, err := tx.Query(ctx, `
			SELECT id, COALESCE(release_date::text, ''), cover_asset_id
			FROM albums WHERE normalized_title = $1 ORDER BY id FOR UPDATE`, normalizedTitle)
		if err != nil {
			return fmt.Errorf("query projected albums: %w", err)
		}
		candidates := make([]albumCandidate, 0)
		for rows.Next() {
			var candidate albumCandidate
			if err := rows.Scan(&candidate.id, &candidate.releaseDate, &candidate.coverID); err != nil {
				rows.Close()
				return fmt.Errorf("scan projected album: %w", err)
			}
			candidates = append(candidates, candidate)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate projected albums: %w", err)
		}
		desiredArtists := make([]string, 0, len(metadata.AlbumArtists))
		for _, name := range metadata.AlbumArtists {
			desiredArtists = append(desiredArtists, artistIDs[normalizeLookup(name)])
		}
		credits := make(map[string][]string)
		if len(candidates) > 0 {
			ids := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				ids = append(ids, candidate.id)
			}
			creditRows, err := tx.Query(ctx, `
				SELECT album_id, artist_id FROM album_artists
				WHERE album_id = ANY($1::uuid[]) AND role = 'PRIMARY'
				ORDER BY album_id, sort_order`, ids)
			if err != nil {
				return fmt.Errorf("query projected album credits: %w", err)
			}
			for creditRows.Next() {
				var candidateID, artistID string
				if err := creditRows.Scan(&candidateID, &artistID); err != nil {
					creditRows.Close()
					return fmt.Errorf("scan projected album credit: %w", err)
				}
				credits[candidateID] = append(credits[candidateID], artistID)
			}
			creditRows.Close()
		}
		selected := ""
		for _, candidate := range candidates {
			if stringSlicesEqual(credits[candidate.id], desiredArtists) {
				if currentAlbumID != nil && candidate.id == *currentAlbumID {
					selected = candidate.id
					break
				}
				if selected == "" {
					selected = candidate.id
				}
			}
		}
		releaseDate := catalogReleaseDate(metadata.ReleaseDate)
		if selected != "" {
			albumID = &selected
			var selectedCandidate albumCandidate
			for _, candidate := range candidates {
				if candidate.id == selected {
					selectedCandidate = candidate
					break
				}
			}
			if selectedCandidate.releaseDate != pointerValue(releaseDate) {
				if _, err := tx.Exec(ctx, `UPDATE albums SET release_date = $2, version = version + 1, updated_at = now() WHERE id = $1`, selected, releaseDate); err != nil {
					return fmt.Errorf("update projected album release date: %w", err)
				}
			}
			if metadata.HasArtwork && currentCoverID != nil && selectedCandidate.coverID == nil {
				if _, err := tx.Exec(ctx, `UPDATE albums SET cover_asset_id = $2, version = version + 1, updated_at = now() WHERE id = $1`, selected, currentCoverID); err != nil {
					return fmt.Errorf("copy projected album artwork: %w", err)
				}
			}
		} else {
			created := uuid.NewString()
			var coverID *string
			if metadata.HasArtwork {
				coverID = currentCoverID
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO albums (id, title, normalized_title, release_date, cover_asset_id)
				VALUES ($1, $2, $3, $4, $5)`, created, *metadata.Album, normalizedTitle, releaseDate, coverID); err != nil {
				return fmt.Errorf("create projected album: %w", err)
			}
			for position, artistID := range desiredArtists {
				if _, err := tx.Exec(ctx, `
					INSERT INTO album_artists (album_id, artist_id, role, sort_order)
					VALUES ($1, $2, 'PRIMARY', $3)`, created, artistID, position); err != nil {
					return fmt.Errorf("create projected album credit: %w", err)
				}
			}
			albumID = &created
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tracks SET title = $2, normalized_title = $3, album_id = $4,
		                  track_number = $5, disc_number = $6,
		                  version = version + 1, updated_at = now()
		WHERE id = $1`, trackID, metadata.Title, normalizeLookup(metadata.Title), albumID, metadata.TrackNumber, metadata.DiscNumber); err != nil {
		return fmt.Errorf("project track metadata: %w", err)
	}
	if currentAlbumID != nil && (albumID == nil || *currentAlbumID != *albumID) {
		if err := repository.deleteEmptyAlbum(ctx, tx, *currentAlbumID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM track_artists WHERE track_id = $1", trackID); err != nil {
		return fmt.Errorf("replace projected track credits: %w", err)
	}
	for position, credit := range metadata.Credits {
		if _, err := tx.Exec(ctx, `
			INSERT INTO track_artists (track_id, artist_id, role, sort_order)
			VALUES ($1, $2, $3, $4)`, trackID, artistIDs[normalizeLookup(credit.Name)], credit.Role, position); err != nil {
			return fmt.Errorf("insert projected track credit: %w", err)
		}
	}
	if previous.Lyrics != nil && (metadata.Lyrics == nil || metadata.Lyrics.Language != previous.Lyrics.Language) {
		if _, err := tx.Exec(ctx, `
			DELETE FROM lyrics WHERE track_id = $1 AND language = $2 AND format = $3
			  AND timing = $4 AND content = $5 AND asset_id IS NULL`,
			trackID, previous.Lyrics.Language, previous.Lyrics.Format, previous.Lyrics.Timing, previous.Lyrics.Content); err != nil {
			return fmt.Errorf("remove previous projected lyrics: %w", err)
		}
	}
	if metadata.Lyrics != nil {
		if _, err := tx.Exec(ctx, "UPDATE lyrics SET is_default = false, updated_at = now() WHERE track_id = $1", trackID); err != nil {
			return fmt.Errorf("clear projected default lyrics: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO lyrics (id, track_id, format, timing, language, origin, content, is_default)
			VALUES ($1, $2, $3, $4, $5, 'SCRAPED', $6, true)
			ON CONFLICT (track_id, language) DO UPDATE SET
				format = EXCLUDED.format, timing = EXCLUDED.timing, content = EXCLUDED.content, origin = 'SCRAPED',
				asset_id = NULL, is_default = true, version = lyrics.version + 1, updated_at = now()`,
			uuid.NewString(), trackID, metadata.Lyrics.Format, metadata.Lyrics.Timing, metadata.Lyrics.Language, metadata.Lyrics.Content); err != nil {
			return fmt.Errorf("upsert projected lyrics: %w", err)
		}
	}
	return nil
}

func (repository *Repository) deleteEmptyAlbum(ctx context.Context, tx pgx.Tx, albumID string) error {
	var coverID *string
	err := tx.QueryRow(ctx, `
		DELETE FROM albums album WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM tracks WHERE album_id = album.id)
		RETURNING cover_asset_id`, albumID).Scan(&coverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete empty projected album: %w", err)
	}
	if coverID == nil {
		return nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE media_assets asset SET status = 'DELETE_PENDING', updated_at = now()
		WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM artists WHERE artwork_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM albums WHERE cover_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM playlists WHERE cover_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM user_profiles WHERE avatar_asset_id = asset.id)`, *coverID)
	if err != nil {
		return fmt.Errorf("detach projected album artwork: %w", err)
	}
	return nil
}

func scanBatchJob(row pgx.Row) (BatchJobRecord, error) {
	var result BatchJobRecord
	var optionsJSON []byte
	var status string
	err := row.Scan(
		&result.ID, &result.RequestedBy, &optionsJSON, &status, &result.Total,
		&result.Processed, &result.Succeeded, &result.Failed, &result.CancelRequested,
		&result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return BatchJobRecord{}, err
	}
	if err := json.Unmarshal(optionsJSON, &result.Options); err != nil {
		return BatchJobRecord{}, fmt.Errorf("decode tag scraping options: %w", err)
	}
	result.Status = JobStatus(status)
	return result, nil
}

type rowScanner interface{ Scan(...any) error }

type trackMetadataRowValues struct {
	trackID       string
	rawJSON       []byte
	overridesJSON []byte
	version       int
	lastScannedAt *time.Time
	updatedBy     *string
	createdAt     time.Time
	updatedAt     time.Time
	sourceID      *string
	rootID        *string
	sourcePath    *string
	sourceStatus  *string
	checksum      *string
	rootMode      *string
	rootEnabled   *bool
	scanActive    bool
	trackStatus   *string
	mappingCount  int
	cue           bool
}

func (values *trackMetadataRowValues) scanTargets() []any {
	return []any{
		&values.trackID, &values.rawJSON, &values.overridesJSON, &values.version,
		&values.lastScannedAt, &values.updatedBy, &values.createdAt, &values.updatedAt,
		&values.sourceID, &values.rootID, &values.sourcePath, &values.sourceStatus, &values.checksum,
		&values.rootMode, &values.rootEnabled, &values.scanActive, &values.trackStatus,
		&values.mappingCount, &values.cue,
	}
}

func (values *trackMetadataRowValues) build() (TrackMetadata, error) {
	raw, overrides, err := decodeMetadataDocuments(values.rawJSON, values.overridesJSON)
	if err != nil {
		return TrackMetadata{}, err
	}
	effective, err := applyOverrides(raw, overrides)
	if err != nil {
		return TrackMetadata{}, err
	}
	result := TrackMetadata{
		TrackID: values.trackID, Raw: raw, Overrides: overrides, Effective: effective,
		OverriddenFields: sortedMapKeys(overrides), TrackStatus: pointerValue(values.trackStatus),
		LastScannedAt: optionalTimestamp(values.lastScannedAt),
		CreatedAt:     formatTimestamp(values.createdAt), UpdatedAt: formatTimestamp(values.updatedAt),
	}
	if values.updatedBy != nil {
		updatedBy := *values.updatedBy
		result.UpdatedBy = &updatedBy
	}
	if values.sourceID != nil {
		mode, trackState := "", ""
		enabled := false
		if values.rootMode != nil {
			mode = *values.rootMode
		}
		if values.rootEnabled != nil {
			enabled = *values.rootEnabled
		}
		if values.trackStatus != nil {
			trackState = *values.trackStatus
		}
		eligibility := tagwriteback.Evaluate(tagwriteback.SourceContext{
			HasSource: true, TrackStatus: trackState, RootMode: mode, RootEnabled: enabled,
			ScanActive: values.scanActive, SourceStatus: pointerValue(values.sourceStatus),
			SourcePath: pointerValue(values.sourcePath), MappingCount: values.mappingCount, Cue: values.cue,
		})
		result.Source = &MetadataSource{
			ID: *values.sourceID, RootID: values.rootID, RelativePath: pointerValue(values.sourcePath),
			Status: pointerValue(values.sourceStatus), ChecksumSHA256: pointerValue(values.checksum),
			Mode: values.rootMode, CanWriteBack: eligibility.CanWriteBack,
			WritebackBlockReason: eligibility.MessagePointer(),
		}
	}
	result.Version = values.version
	return result, nil
}

func scanBatchItem(row rowScanner) (BatchItemRecord, error) {
	var result BatchItemRecord
	var status string
	var candidateJSON []byte
	var source *string
	err := row.Scan(
		&result.ID, &result.JobID, &result.TrackID, &result.ExpectedVersion, &result.Position,
		&status, &result.Attempts, &result.MaxAttempts, &result.NextAttemptAt,
		&result.AttemptID, &result.LockedBy, &result.LockedUntil, &candidateJSON,
		&source, &result.Message, &result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return BatchItemRecord{}, err
	}
	result.Status = ItemStatus(status)
	if len(candidateJSON) > 0 {
		var candidate Candidate
		if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
			return BatchItemRecord{}, fmt.Errorf("decode tag scraping candidate: %w", err)
		}
		result.Candidate = &candidate
	}
	if source != nil {
		value := Source(*source)
		result.Source = &value
	}
	return result, nil
}

func scanClaimedBatchItem(row rowScanner) (BatchItemRecord, *TrackMetadata, error) {
	var result BatchItemRecord
	var status string
	var candidateJSON []byte
	var source *string
	var metadataValues trackMetadataRowValues
	targets := []any{
		&result.ID, &result.JobID, &result.TrackID, &result.ExpectedVersion, &result.Position,
		&status, &result.Attempts, &result.MaxAttempts, &result.NextAttemptAt,
		&result.AttemptID, &result.LockedBy, &result.LockedUntil, &candidateJSON,
		&source, &result.Message, &result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	}
	targets = append(targets, metadataValues.scanTargets()...)
	if err := row.Scan(targets...); err != nil {
		return BatchItemRecord{}, nil, err
	}
	result.Status = ItemStatus(status)
	if len(candidateJSON) > 0 {
		var candidate Candidate
		if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
			return BatchItemRecord{}, nil, fmt.Errorf("decode tag scraping candidate: %w", err)
		}
		result.Candidate = &candidate
	}
	if source != nil {
		value := Source(*source)
		result.Source = &value
	}
	if metadataValues.version == 0 {
		return result, nil, nil
	}
	metadata, err := metadataValues.build()
	if err != nil {
		return BatchItemRecord{}, nil, err
	}
	return result, &metadata, nil
}

func scanWritebackJob(row pgx.Row) (WritebackJob, error) {
	var result WritebackJob
	var nextAttemptAt, startedAt, completedAt, createdAt, updatedAt *time.Time
	err := row.Scan(
		&result.ID, &result.TrackID, &result.SourceID, &result.Status,
		&result.Stage, &result.Attempts, &result.MaxAttempts, &result.CancelRequested,
		&result.MetadataVersion, &result.Reason,
		&result.OutputChecksumSHA256, &result.LastErrorCode, &result.LastError, &result.Version,
		&nextAttemptAt, &startedAt, &completedAt, &createdAt, &updatedAt,
	)
	if err != nil {
		return WritebackJob{}, err
	}
	result.StartedAt = optionalTimestamp(startedAt)
	result.CompletedAt = optionalTimestamp(completedAt)
	if nextAttemptAt != nil {
		result.NextAttemptAt = formatTimestamp(*nextAttemptAt)
	}
	if createdAt != nil {
		result.CreatedAt = formatTimestamp(*createdAt)
	}
	if updatedAt != nil {
		result.UpdatedAt = formatTimestamp(*updatedAt)
	}
	return result, nil
}

func decodeMetadataDocuments(rawJSON, overridesJSON []byte) (MetadataSnapshot, map[string]any, error) {
	var raw MetadataSnapshot
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return MetadataSnapshot{}, nil, fmt.Errorf("decode raw track metadata: %w", err)
	}
	normalizeSnapshot(&raw)
	if err := validateStoredMetadataLyrics(raw); err != nil {
		return MetadataSnapshot{}, nil, err
	}
	overrides := make(map[string]any)
	if len(overridesJSON) > 0 {
		if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
			return MetadataSnapshot{}, nil, fmt.Errorf("decode track metadata overrides: %w", err)
		}
	}
	return raw, overrides, nil
}

func applyOverrides(raw MetadataSnapshot, overrides map[string]any) (MetadataSnapshot, error) {
	document, _ := json.Marshal(raw)
	var combined map[string]any
	if err := json.Unmarshal(document, &combined); err != nil {
		return MetadataSnapshot{}, err
	}
	for field, value := range overrides {
		combined[field] = value
	}
	combined["hasArtwork"] = raw.HasArtwork
	document, _ = json.Marshal(combined)
	var result MetadataSnapshot
	if err := json.Unmarshal(document, &result); err != nil {
		return MetadataSnapshot{}, apperror.Validation("Stored metadata overrides are invalid")
	}
	normalizeSnapshot(&result)
	if err := validateStoredMetadataLyrics(result); err != nil {
		return MetadataSnapshot{}, err
	}
	return result, nil
}

func validateStoredMetadataLyrics(snapshot MetadataSnapshot) error {
	if snapshot.Lyrics == nil {
		return nil
	}
	if err := sharedlyrics.ValidateDocument(
		snapshot.Lyrics.Format,
		snapshot.Lyrics.Timing,
		snapshot.Lyrics.Content,
	); err != nil {
		return apperror.Internal("Stored metadata lyrics violate the timing contract", err)
	}
	return nil
}

func normalizeSnapshot(snapshot *MetadataSnapshot) {
	if snapshot.Credits == nil {
		snapshot.Credits = []MetadataCredit{}
	}
	if snapshot.AlbumArtists == nil {
		snapshot.AlbumArtists = []string{}
	}
	if snapshot.Genres == nil {
		snapshot.Genres = []string{}
	}
}

func changedMetadataFields(previous, next MetadataSnapshot) []string {
	previousDocument, _ := json.Marshal(previous)
	nextDocument, _ := json.Marshal(next)
	var left, right map[string]any
	_ = json.Unmarshal(previousDocument, &left)
	_ = json.Unmarshal(nextDocument, &right)
	fields := make([]string, 0)
	for _, field := range metadataFields {
		if !reflect.DeepEqual(normalizeComparable(left[field]), normalizeComparable(right[field])) {
			fields = append(fields, field)
		}
	}
	return fields
}

func editableMetadataField(field string) bool {
	for _, candidate := range metadataFields {
		if field == candidate {
			return true
		}
	}
	return false
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func sortedMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeLookup(value string) string { return strings.ToLower(normalizeText(value)) }

func catalogReleaseDate(value *string) *string {
	if value == nil || len(*value) == 10 {
		return value
	}
	result := *value
	if len(result) == 4 {
		result += "-01-01"
	} else if len(result) == 7 {
		result += "-01"
	}
	return &result
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolPointerValue(value *bool) bool {
	return value != nil && *value
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var metadataFields = []string{
	"title", "credits", "albumArtists", "album", "releaseDate", "trackNumber", "trackTotal",
	"discNumber", "discTotal", "genres", "bpm", "isrc", "comment", "copyright", "lyrics",
}

const batchJobSelect = `
	SELECT id, requested_by, options, status::text, total, processed, succeeded, failed,
	       cancel_requested, started_at, completed_at, created_at, updated_at
	FROM tag_scraping_jobs`

const batchItemSelect = `
	SELECT id, job_id, track_id, expected_version, position, status::text,
	       attempts, max_attempts, next_attempt_at, attempt_id, locked_by, locked_until, candidate, source, message,
	       started_at, completed_at, created_at, updated_at
	FROM tag_scraping_job_items`

const claimedBatchItemSelect = `
	SELECT item.id, item.job_id, item.track_id, item.expected_version, item.position, item.status::text,
	       item.attempts, item.max_attempts, item.next_attempt_at,
	       item.attempt_id, item.locked_by, item.locked_until, item.candidate, item.source, item.message,
	       item.started_at, item.completed_at, item.created_at, item.updated_at,
	       item.track_id, COALESCE(metadata.raw_tags, '{}'::jsonb), COALESCE(metadata.overrides, '{}'::jsonb),
	       COALESCE(metadata.version, 0), metadata.last_scanned_at, metadata.updated_by,
	       COALESCE(metadata.created_at, item.created_at), COALESCE(metadata.updated_at, item.updated_at),
	       source.id, source.root_id, source.source_path, source.status, source.checksum_sha256,
	       root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
			       ), track.status::text,
	       COALESCE(mapping_stats.mapping_count, 0), COALESCE(mapping_stats.cue, false)
	FROM tag_scraping_job_items item
	JOIN tracks track ON track.id = item.track_id
	LEFT JOIN track_metadata metadata ON metadata.track_id = item.track_id
	LEFT JOIN local_music_sources source ON source.id = metadata.source_id
	LEFT JOIN library_roots root ON root.id = source.root_id
	LEFT JOIN LATERAL (
		SELECT count(*)::int AS mapping_count,
		       COALESCE(bool_or(mapping.cue_path IS NOT NULL), false) AS cue
		FROM local_music_source_tracks mapping WHERE mapping.source_id = source.id
	) mapping_stats ON true`
