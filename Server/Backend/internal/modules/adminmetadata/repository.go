package adminmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/tagwriteback"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ Store = (*Repository)(nil)
var _ WorkerStore = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) EnsureMetadata(ctx context.Context, trackIDs []string) error {
	trackIDs = uniqueNonEmpty(trackIDs)
	if len(trackIDs) == 0 {
		return nil
	}
	rows, err := repository.pool.Query(ctx, `
		select track_id::text from track_metadata where track_id = any($1::uuid[])`, trackIDs)
	if err != nil {
		return fmt.Errorf("find existing track metadata: %w", err)
	}
	existing := make(map[string]struct{}, len(trackIDs))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan existing track metadata: %w", err)
		}
		existing[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate existing track metadata: %w", err)
	}
	rows.Close()

	for _, trackID := range trackIDs {
		if _, found := existing[trackID]; found {
			continue
		}
		snapshot, err := catalogSnapshot(ctx, repository.pool, trackID)
		if err != nil {
			return err
		}
		raw, err := encodeJSON(snapshot)
		if err != nil {
			return err
		}
		var sourceID *string
		var checksum *string
		var scannedAt *time.Time
		err = repository.pool.QueryRow(ctx, `
			select source.id::text, source.checksum_sha256, source.updated_at
			from local_music_source_tracks mapping
			join local_music_sources source on source.id = mapping.source_id
			where mapping.track_id = $1
			order by case source.status when 'READY' then 0 when 'PROCESSING' then 1 else 2 end,
				source.updated_at desc, source.id
			limit 1`, trackID).Scan(&sourceID, &checksum, &scannedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			sourceID, checksum, scannedAt = nil, nil, nil
		} else if err != nil {
			return fmt.Errorf("find metadata baseline source: %w", err)
		}
		if _, err := repository.pool.Exec(ctx, `
			insert into track_metadata (
				track_id, source_id, raw_tags, overrides, raw_checksum_sha256, last_scanned_at
			) values ($1, $2, $3::jsonb, '{}'::jsonb, $4, $5)
			on conflict (track_id) do nothing`, trackID, sourceID, string(raw), checksum, scannedAt); err != nil {
			return fmt.Errorf("create track metadata baseline: %w", err)
		}
	}

	if _, err := repository.pool.Exec(ctx, `
		insert into track_metadata_revisions (
			track_id, metadata_version, action, raw_tags, overrides, effective_tags, reason
		)
		select metadata.track_id, metadata.version, 'BASELINE', metadata.raw_tags,
			metadata.overrides, metadata.raw_tags || metadata.overrides, 'Initial metadata snapshot'
		from track_metadata metadata
		where metadata.track_id = any($1::uuid[])
		  and not exists (
			select 1 from track_metadata_revisions revision
			where revision.track_id = metadata.track_id
		  )
		on conflict (track_id, metadata_version) do nothing`, trackIDs); err != nil {
		return fmt.Errorf("create metadata baseline revision: %w", err)
	}
	return nil
}

func (repository *Repository) FindMetadata(ctx context.Context, trackID string) (MetadataRecord, error) {
	record, err := scanMetadata(repository.pool.QueryRow(ctx, `
		select `+metadataColumns+`,
			source.id::text, source.root_id::text, source.source_path, source.status,
			source.checksum_sha256, root.path, root.mode::text, root.enabled, root.status::text,
			track.status::text, coalesce(mapping_stats.mapping_count, 0),
			coalesce(mapping_stats.cue, false)
		from track_metadata metadata
		left join local_music_sources source on source.id = metadata.source_id
		left join library_roots root on root.id = source.root_id
		left join tracks track on track.id = metadata.track_id
		left join lateral (
			select count(*)::int as mapping_count,
				coalesce(bool_or(mapping.cue_path is not null), false) as cue
			from local_music_source_tracks mapping where mapping.source_id = source.id
		) mapping_stats on true
		where metadata.track_id = $1`, trackID), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return MetadataRecord{}, apperror.NotFound("Track metadata was not found")
	}
	if err != nil {
		return MetadataRecord{}, fmt.Errorf("find track metadata: %w", err)
	}
	return record, nil
}

func (repository *Repository) UpdateMetadata(
	ctx context.Context,
	actorID, traceID, trackID string,
	input MetadataMutationInput,
) (MetadataRecord, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return MetadataRecord{}, fmt.Errorf("begin track metadata update: %w", err)
	}
	defer tx.Rollback(ctx)

	row, err := lockedMetadata(ctx, tx, trackID)
	if err != nil {
		return MetadataRecord{}, err
	}
	if row.Version != input.ExpectedVersion {
		return MetadataRecord{}, metadataVersionConflict(input.ExpectedVersion, row.Version, "")
	}
	raw, currentOverrides, previousEffective, nextOverrides, nextEffective, err := mutationSnapshots(row, input)
	if err != nil {
		return MetadataRecord{}, err
	}
	if stableEqual(currentOverrides, nextOverrides) {
		return MetadataRecord{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The metadata edit does not change any field",
			nil,
		)
	}
	nextVersion := row.Version + 1
	if err := persistMetadataMutation(ctx, tx, mutationWrite{
		TrackID: trackID, ActorID: actorID, TraceID: traceID, Action: "EDIT",
		AuditAction: "TRACK_METADATA_UPDATED", Reason: input.Reason,
		PreviousVersion: row.Version, NextVersion: nextVersion, Raw: raw,
		Overrides: nextOverrides, PreviousEffective: previousEffective, Effective: nextEffective,
		ResetFields: input.ResetFields,
	}); err != nil {
		return MetadataRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MetadataRecord{}, fmt.Errorf("commit track metadata update: %w", err)
	}
	return repository.FindMetadata(ctx, trackID)
}

func (repository *Repository) BatchUpdateMetadata(
	ctx context.Context,
	actorID, traceID string,
	input BatchMetadataMutationInput,
) ([]BatchUpdateRecord, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin batch metadata update: %w", err)
	}
	defer tx.Rollback(ctx)

	items := append([]BatchMutationItem(nil), input.Items...)
	sort.Slice(items, func(left, right int) bool { return items[left].TrackID < items[right].TrackID })
	type preparedMutation struct {
		row               MetadataRecord
		raw               MetadataSnapshot
		overrides         MetadataOverrides
		previousEffective MetadataSnapshot
		effective         MetadataSnapshot
		changedFields     []string
	}
	prepared := make([]preparedMutation, 0, len(items))
	for _, item := range items {
		row, err := lockedMetadata(ctx, tx, item.TrackID)
		if err != nil {
			return nil, err
		}
		if row.Version != item.ExpectedVersion {
			return nil, metadataVersionConflict(item.ExpectedVersion, row.Version, item.TrackID)
		}
		raw, currentOverrides, previousEffective, nextOverrides, nextEffective, err := mutationSnapshots(row, MetadataMutationInput{
			ExpectedVersion: item.ExpectedVersion,
			Patch:           input.Patch, ResetFields: input.ResetFields, Reason: input.Reason,
		})
		if err != nil {
			return nil, err
		}
		if stableEqual(currentOverrides, nextOverrides) {
			return nil, apperror.Conflict(
				apperror.CodeResourceConflict,
				"Metadata for track "+item.TrackID+" would not change",
				nil,
			)
		}
		prepared = append(prepared, preparedMutation{
			row: row, raw: raw, overrides: nextOverrides, previousEffective: previousEffective,
			effective: nextEffective, changedFields: MetadataChangedFields(previousEffective, nextEffective),
		})
	}

	result := make([]BatchUpdateRecord, 0, len(prepared))
	for _, change := range prepared {
		nextVersion := change.row.Version + 1
		if err := persistMetadataMutation(ctx, tx, mutationWrite{
			TrackID: change.row.TrackID, ActorID: actorID, TraceID: traceID, Action: "EDIT",
			AuditAction: "TRACK_METADATA_BATCH_UPDATED", Reason: input.Reason,
			PreviousVersion: change.row.Version, NextVersion: nextVersion, Raw: change.raw,
			Overrides: change.overrides, PreviousEffective: change.previousEffective,
			Effective: change.effective, BatchSize: len(prepared),
		}); err != nil {
			return nil, err
		}
		result = append(result, BatchUpdateRecord{
			TrackID: change.row.TrackID, Version: nextVersion, ChangedFields: change.changedFields,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit batch metadata update: %w", err)
	}
	return result, nil
}

func (repository *Repository) ListRevisions(
	ctx context.Context,
	trackID string,
	limit, offset int,
) ([]RevisionRecord, int, error) {
	rows, err := repository.pool.Query(ctx, `
		select `+revisionColumns+`
		from track_metadata_revisions
		where track_id = $1
		order by metadata_version desc
		limit $2 offset $3`, trackID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list metadata revisions: %w", err)
	}
	defer rows.Close()
	items := make([]RevisionRecord, 0, limit)
	for rows.Next() {
		item, err := scanRevision(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan metadata revision: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate metadata revisions: %w", err)
	}
	var total int
	if err := repository.pool.QueryRow(ctx, `
		select count(*)::int from track_metadata_revisions where track_id = $1`, trackID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count metadata revisions: %w", err)
	}
	return items, total, nil
}

func (repository *Repository) FindRevision(
	ctx context.Context,
	trackID, revisionID string,
) (RevisionRecord, error) {
	revision, err := scanRevision(repository.pool.QueryRow(ctx, `
		select `+revisionColumns+`
		from track_metadata_revisions
		where id = $1 and track_id = $2`, revisionID, trackID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RevisionRecord{}, apperror.NotFound("Metadata revision was not found")
	}
	if err != nil {
		return RevisionRecord{}, fmt.Errorf("find metadata revision: %w", err)
	}
	return revision, nil
}

func (repository *Repository) RestoreMetadata(
	ctx context.Context,
	actorID, traceID, trackID, revisionID string,
	input VersionReasonInput,
) (MetadataRecord, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return MetadataRecord{}, fmt.Errorf("begin metadata restore: %w", err)
	}
	defer tx.Rollback(ctx)
	row, err := lockedMetadata(ctx, tx, trackID)
	if err != nil {
		return MetadataRecord{}, err
	}
	if row.Version != input.ExpectedVersion {
		return MetadataRecord{}, metadataVersionConflict(input.ExpectedVersion, row.Version, "")
	}
	revision, err := scanRevision(tx.QueryRow(ctx, `
		select `+revisionColumns+` from track_metadata_revisions
		where id = $1 and track_id = $2`, revisionID, trackID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MetadataRecord{}, apperror.NotFound("Metadata revision was not found")
	}
	if err != nil {
		return MetadataRecord{}, fmt.Errorf("find metadata restore revision: %w", err)
	}
	raw, err := decodeSnapshot(row.Raw)
	if err != nil {
		return MetadataRecord{}, err
	}
	currentOverrides, err := decodeOverrides(row.Overrides)
	if err != nil {
		return MetadataRecord{}, err
	}
	previousEffective, err := ApplyMetadataOverrides(raw, currentOverrides)
	if err != nil {
		return MetadataRecord{}, err
	}
	revisionEffective, err := decodeSnapshot(revision.Effective)
	if err != nil {
		return MetadataRecord{}, err
	}
	revisionEffective.HasArtwork = raw.HasArtwork
	restoredOverrides, err := MetadataOverridesForTarget(raw, revisionEffective)
	if err != nil {
		return MetadataRecord{}, err
	}
	if stableEqual(currentOverrides, restoredOverrides) {
		return MetadataRecord{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The selected revision is already active",
			nil,
		)
	}
	nextEffective, err := ApplyMetadataOverrides(raw, restoredOverrides)
	if err != nil {
		return MetadataRecord{}, err
	}
	if err := persistMetadataMutation(ctx, tx, mutationWrite{
		TrackID: trackID, ActorID: actorID, TraceID: traceID, Action: "RESTORE",
		AuditAction: "TRACK_METADATA_RESTORED", Reason: input.Reason,
		PreviousVersion: row.Version, NextVersion: row.Version + 1, Raw: raw,
		Overrides: restoredOverrides, PreviousEffective: previousEffective, Effective: nextEffective,
		RestoredRevisionID: revisionID, RestoredMetadataVersion: revision.MetadataVersion,
	}); err != nil {
		return MetadataRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MetadataRecord{}, fmt.Errorf("commit metadata restore: %w", err)
	}
	return repository.FindMetadata(ctx, trackID)
}

func (repository *Repository) EnqueueWriteback(
	ctx context.Context,
	actorID, traceID, trackID string,
	input VersionReasonInput,
) (WritebackJob, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return WritebackJob{}, fmt.Errorf("begin metadata writeback enqueue: %w", err)
	}
	defer tx.Rollback(ctx)
	var initialSourceID, rootID string
	err = tx.QueryRow(ctx, `
		select metadata.source_id::text, source.root_id::text
		from track_metadata metadata
		join local_music_sources source on source.id = metadata.source_id
		where metadata.track_id = $1`, trackID).Scan(&initialSourceID, &rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("A writable local source for this track was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("find metadata writeback lock order: %w", err)
	}
	var lockedRootID, lockedTrackID string
	if err := tx.QueryRow(ctx, `select id::text from library_roots where id = $1 for update`, rootID).Scan(&lockedRootID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("The music source for this track no longer exists")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback root: %w", err)
	}
	if err := tx.QueryRow(ctx, `select id::text from tracks where id = $1 for update`, trackID).Scan(&lockedTrackID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("Track was not found")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback track: %w", err)
	}

	var metadata MetadataRecord
	var source MetadataSourceRecord
	var rootPath, rootMode, rootStatus, trackStatus string
	var rootEnabled bool
	err = tx.QueryRow(ctx, `
		select `+metadataColumns+`, source.id::text, source.root_id::text,
			source.source_path, source.status, source.checksum_sha256,
			root.path, root.mode::text, root.enabled, root.status::text,
			track.status::text
		from track_metadata metadata
		join tracks track on track.id = metadata.track_id
		join local_music_sources source on source.id = metadata.source_id
		join library_roots root on root.id = source.root_id
		where metadata.track_id = $1
		for update of metadata, source`, trackID).Scan(
		&metadata.TrackID, &metadata.SourceID, &metadata.Raw, &metadata.Overrides,
		&metadata.RawChecksum, &metadata.LastScannedAt, &metadata.UpdatedBy,
		&metadata.Version, &metadata.CreatedAt, &metadata.UpdatedAt,
		&source.ID, &source.RootID, &source.SourcePath, &source.Status,
		&source.ChecksumSHA256, &rootPath, &rootMode, &rootEnabled, &rootStatus,
		&trackStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("A writable local source for this track was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback context: %w", err)
	}
	if source.ID != initialSourceID || source.RootID == nil || *source.RootID != rootID {
		return WritebackJob{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The local source changed while Tag writeback was being queued",
			nil,
		)
	}
	var mappingCount int
	var cue bool
	if err := tx.QueryRow(ctx, `
		select count(*)::int, coalesce(bool_or(cue_path is not null), false)
		from local_music_source_tracks where source_id = $1`, source.ID).Scan(&mappingCount, &cue); err != nil {
		return WritebackJob{}, fmt.Errorf("inspect metadata source mappings: %w", err)
	}
	if metadata.Version != input.ExpectedVersion {
		return WritebackJob{}, metadataVersionConflict(input.ExpectedVersion, metadata.Version, "")
	}
	if err := tagwriteback.Evaluate(tagwriteback.SourceContext{
		HasSource: true, TrackStatus: trackStatus, RootMode: rootMode,
		RootEnabled: rootEnabled, RootStatus: rootStatus, SourceStatus: source.Status,
		SourcePath: source.SourcePath, MappingCount: mappingCount, Cue: cue,
	}).Error(trackID); err != nil {
		return WritebackJob{}, err
	}
	var conflictingWritebackID string
	err = tx.QueryRow(ctx, `
		select id::text from metadata_writeback_jobs
		where source_id = $1 and status in ('PENDING','PROCESSING')
		order by created_at desc limit 1`, source.ID).Scan(&conflictingWritebackID)
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
	raw, err := decodeSnapshot(metadata.Raw)
	if err != nil {
		return WritebackJob{}, err
	}
	overrides, err := decodeOverrides(metadata.Overrides)
	if err != nil {
		return WritebackJob{}, err
	}
	effective, err := ApplyMetadataOverrides(raw, overrides)
	if err != nil {
		return WritebackJob{}, err
	}
	snapshotJSON, err := encodeJSON(effective)
	if err != nil {
		return WritebackJob{}, err
	}
	var revisionID *string
	err = tx.QueryRow(ctx, `
		select id::text from track_metadata_revisions
		where track_id = $1 and metadata_version = $2 limit 1`, trackID, metadata.Version).Scan(&revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		revisionID = nil
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("find queued metadata revision: %w", err)
	}
	job, err := scanWriteback(tx.QueryRow(ctx, `
		insert into metadata_writeback_jobs (
			track_id, source_id, revision_id, requested_by, reason,
			metadata_snapshot, metadata_version, expected_source_checksum,
			root_path_snapshot, source_path_snapshot
		) values ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)
		returning `+writebackColumns,
		trackID, source.ID, revisionID, actorID, input.Reason,
		string(snapshotJSON), metadata.Version, source.ChecksumSHA256,
		rootPath, source.SourcePath,
	))
	if err != nil {
		if uniqueViolation(err) {
			return WritebackJob{}, apperror.Conflict(
				apperror.CodeResourceConflict,
				"A metadata writeback is already active for this source",
				nil,
			)
		}
		return WritebackJob{}, fmt.Errorf("enqueue metadata writeback: %w", err)
	}
	if err := insertAudit(ctx, tx, auditWrite{
		ActorID: &actorID, Action: "TRACK_METADATA_WRITEBACK_QUEUED",
		TargetType: "metadata_writeback_job", TargetID: &job.ID, Result: "SUCCESS",
		TraceID: traceID, Details: map[string]any{
			"trackId": trackID, "sourceId": source.ID,
			"metadataVersion": metadata.Version, "reason": input.Reason,
		},
	}); err != nil {
		return WritebackJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WritebackJob{}, fmt.Errorf("commit metadata writeback enqueue: %w", err)
	}
	return job, nil
}

func (repository *Repository) ListWritebacks(
	ctx context.Context,
	query WritebackListQuery,
) ([]WritebackJob, int, error) {
	where := "where true"
	arguments := []any{}
	if query.Status != "" {
		arguments = append(arguments, string(query.Status))
		where += fmt.Sprintf(" and status = $%d::metadata_writeback_status", len(arguments))
	}
	if query.TrackID != "" {
		arguments = append(arguments, query.TrackID)
		where += fmt.Sprintf(" and track_id = $%d", len(arguments))
	}
	arguments = append(arguments, query.Limit, query.Offset)
	rows, err := repository.pool.Query(ctx, `
		select `+writebackColumns+` from metadata_writeback_jobs `+where+`
		order by created_at desc, id desc
		limit $`+fmt.Sprint(len(arguments)-1)+` offset $`+fmt.Sprint(len(arguments)), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("list metadata writeback jobs: %w", err)
	}
	items := make([]WritebackJob, 0, query.Limit)
	for rows.Next() {
		job, err := scanWriteback(rows)
		if err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan metadata writeback job: %w", err)
		}
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("iterate metadata writeback jobs: %w", err)
	}
	rows.Close()
	countArguments := arguments[:len(arguments)-2]
	var total int
	if err := repository.pool.QueryRow(ctx,
		`select count(*)::int from metadata_writeback_jobs `+where,
		countArguments...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count metadata writeback jobs: %w", err)
	}
	return items, total, nil
}

func (repository *Repository) FindWriteback(ctx context.Context, jobID string) (WritebackJob, error) {
	job, err := scanWriteback(repository.pool.QueryRow(ctx, `
		select `+writebackColumns+` from metadata_writeback_jobs where id = $1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("Metadata writeback job was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("find metadata writeback job: %w", err)
	}
	return job, nil
}

func (repository *Repository) CancelWriteback(
	ctx context.Context,
	actorID, traceID, jobID string,
	input VersionReasonInput,
) (WritebackJob, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return WritebackJob{}, fmt.Errorf("begin metadata writeback cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := lockedWriteback(ctx, tx, jobID)
	if err != nil {
		return WritebackJob{}, err
	}
	if job.Version != input.ExpectedVersion {
		return WritebackJob{}, writebackVersionConflict(input.ExpectedVersion, job.Version)
	}
	if err := validateWritebackCancellation(job); err != nil {
		return WritebackJob{}, err
	}
	immediate := job.Status == WritebackPending
	if _, err := tx.Exec(ctx, `
		update metadata_writeback_jobs set
			cancel_requested = true,
			status = case when $3 then 'CANCELLED'::metadata_writeback_status else status end,
			completed_at = case when $3 then now() else completed_at end,
			locked_by = case when $3 then null else locked_by end,
			locked_until = case when $3 then null else locked_until end,
			stage = case when $3 then 'QUEUED' else stage end,
			backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = case when $3 then null else output_checksum_sha256 end,
			version = version + 1, updated_at = now()
		where id = $1 and version = $2`, jobID, job.Version, immediate); err != nil {
		return WritebackJob{}, fmt.Errorf("cancel metadata writeback job: %w", err)
	}
	if err := insertAudit(ctx, tx, auditWrite{
		ActorID: &actorID, Action: "TRACK_METADATA_WRITEBACK_CANCELLED",
		TargetType: "metadata_writeback_job", TargetID: &jobID, Result: "SUCCESS",
		TraceID: traceID, Details: map[string]any{
			"trackId": job.TrackID, "reason": input.Reason, "cancellationPending": !immediate,
		},
	}); err != nil {
		return WritebackJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WritebackJob{}, fmt.Errorf("commit metadata writeback cancellation: %w", err)
	}
	return repository.FindWriteback(ctx, jobID)
}

func (repository *Repository) RetryWriteback(
	ctx context.Context,
	actorID, traceID, jobID string,
	input VersionReasonInput,
) (WritebackJob, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return WritebackJob{}, fmt.Errorf("begin metadata writeback retry: %w", err)
	}
	defer tx.Rollback(ctx)
	candidate, err := scanWriteback(tx.QueryRow(ctx, `
		select `+writebackColumns+` from metadata_writeback_jobs where id = $1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("Metadata writeback job was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("find metadata writeback retry context: %w", err)
	}
	var rootID string
	if err := tx.QueryRow(ctx, `
		select root_id::text from local_music_sources where id = $1`, candidate.SourceID,
	).Scan(&rootID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("The local source for this writeback no longer exists")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("find metadata writeback retry root: %w", err)
	}
	// Lock in root -> track -> job order. Root deletion already owns the root
	// first, while permanent track deletion owns the track before the job; this
	// ordering waits without holding the row needed by either operation.
	var lockedRootID string
	if err := tx.QueryRow(ctx, `select id::text from library_roots where id = $1 for update`, rootID).Scan(&lockedRootID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("The music source for this writeback no longer exists")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback retry root: %w", err)
	}
	var lockedTrackID string
	if err := tx.QueryRow(ctx, `select id::text from tracks where id = $1 for update`, candidate.TrackID).Scan(&lockedTrackID); errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("The track for this writeback no longer exists")
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback retry track: %w", err)
	}
	job, err := lockedWriteback(ctx, tx, jobID)
	if err != nil {
		return WritebackJob{}, err
	}
	if job.Version != input.ExpectedVersion {
		return WritebackJob{}, writebackVersionConflict(input.ExpectedVersion, job.Version)
	}
	if job.Status != WritebackFailed && job.Status != WritebackCancelled {
		return WritebackJob{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Only a failed or cancelled writeback can be retried",
			nil,
		)
	}
	var metadata MetadataRecord
	var source MetadataSourceRecord
	var rootPath, rootMode, rootStatus, trackStatus string
	var rootEnabled bool
	err = tx.QueryRow(ctx, `
		select `+metadataColumns+`, source.id::text, source.root_id::text,
			source.source_path, source.status, source.checksum_sha256,
			root.path, root.mode::text, root.enabled, root.status::text,
			track.status::text
		from track_metadata metadata
		join tracks track on track.id = metadata.track_id
		join local_music_sources source on source.id = metadata.source_id
		join library_roots root on root.id = source.root_id
		where metadata.track_id = $1
		for update of metadata, source`, job.TrackID).Scan(
		&metadata.TrackID, &metadata.SourceID, &metadata.Raw, &metadata.Overrides,
		&metadata.RawChecksum, &metadata.LastScannedAt, &metadata.UpdatedBy,
		&metadata.Version, &metadata.CreatedAt, &metadata.UpdatedAt,
		&source.ID, &source.RootID, &source.SourcePath, &source.Status,
		&source.ChecksumSHA256, &rootPath, &rootMode, &rootEnabled, &rootStatus,
		&trackStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("The local source for this writeback no longer exists")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback retry context: %w", err)
	}
	if source.RootID == nil || *source.RootID != rootID {
		return WritebackJob{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"The writeback source moved to a different music root",
			nil,
		)
	}
	var mappingCount int
	var cue bool
	if err := tx.QueryRow(ctx, `
		select count(*)::int, coalesce(bool_or(cue_path is not null), false)
		from local_music_source_tracks where source_id = $1`, source.ID).Scan(&mappingCount, &cue); err != nil {
		return WritebackJob{}, fmt.Errorf("inspect retried metadata source mappings: %w", err)
	}
	if err := tagwriteback.Evaluate(tagwriteback.SourceContext{
		HasSource: true, TrackStatus: trackStatus, RootMode: rootMode,
		RootEnabled: rootEnabled, RootStatus: rootStatus, SourceStatus: source.Status,
		SourcePath: source.SourcePath, MappingCount: mappingCount, Cue: cue,
	}).Error(job.TrackID); err != nil {
		return WritebackJob{}, err
	}
	raw, err := decodeSnapshot(metadata.Raw)
	if err != nil {
		return WritebackJob{}, err
	}
	overrides, err := decodeOverrides(metadata.Overrides)
	if err != nil {
		return WritebackJob{}, err
	}
	effective, err := ApplyMetadataOverrides(raw, overrides)
	if err != nil {
		return WritebackJob{}, err
	}
	snapshotJSON, err := encodeJSON(effective)
	if err != nil {
		return WritebackJob{}, err
	}
	var revisionID *string
	err = tx.QueryRow(ctx, `select id::text from track_metadata_revisions
		where track_id = $1 and metadata_version = $2 limit 1`, job.TrackID, metadata.Version).Scan(&revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		revisionID = nil
	} else if err != nil {
		return WritebackJob{}, fmt.Errorf("find retried metadata revision: %w", err)
	}
	command, err := tx.Exec(ctx, `
		update metadata_writeback_jobs set
			source_id = $3, revision_id = $4, requested_by = $5, reason = $6,
			metadata_snapshot = $7::jsonb, metadata_version = $8,
			expected_source_checksum = $9,
			root_path_snapshot = $10, source_path_snapshot = $11,
			status = 'PENDING', attempts = 0,
			cancel_requested = false, attempt_id = null,
			stage = 'QUEUED',
			locked_by = null, locked_until = null, next_attempt_at = now(),
			started_at = null, completed_at = null,
			backup_path = null, backup_expires_at = null,
			output_checksum_sha256 = null,
			last_error_code = null, last_error = null,
			version = version + 1, updated_at = now()
		where id = $1 and version = $2`, job.ID, job.Version, source.ID, revisionID,
		actorID, input.Reason, string(snapshotJSON), metadata.Version,
		source.ChecksumSHA256, rootPath, source.SourcePath)
	if err != nil {
		if uniqueViolation(err) {
			return WritebackJob{}, apperror.Conflict(
				apperror.CodeResourceConflict,
				"A metadata writeback is already active for this source",
				nil,
			)
		}
		return WritebackJob{}, fmt.Errorf("retry metadata writeback job: %w", err)
	}
	if command.RowsAffected() != 1 {
		return WritebackJob{}, writebackVersionConflict(input.ExpectedVersion, job.Version)
	}
	if err := insertAudit(ctx, tx, auditWrite{
		ActorID: &actorID, Action: "TRACK_METADATA_WRITEBACK_RETRIED",
		TargetType: "metadata_writeback_job", TargetID: &jobID, Result: "SUCCESS",
		TraceID: traceID, Details: map[string]any{
			"trackId": job.TrackID, "metadataVersion": metadata.Version, "reason": input.Reason,
		},
	}); err != nil {
		return WritebackJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WritebackJob{}, fmt.Errorf("commit metadata writeback retry: %w", err)
	}
	return repository.FindWriteback(ctx, jobID)
}

type mutationWrite struct {
	TrackID                 string
	ActorID                 string
	TraceID                 string
	Action                  string
	AuditAction             string
	Reason                  string
	PreviousVersion         int
	NextVersion             int
	Raw                     MetadataSnapshot
	Overrides               MetadataOverrides
	PreviousEffective       MetadataSnapshot
	Effective               MetadataSnapshot
	ResetFields             []string
	BatchSize               int
	RestoredRevisionID      string
	RestoredMetadataVersion int
}

func persistMetadataMutation(ctx context.Context, tx pgx.Tx, input mutationWrite) error {
	rawJSON, err := encodeJSON(input.Raw)
	if err != nil {
		return err
	}
	overridesJSON, err := encodeJSON(input.Overrides)
	if err != nil {
		return err
	}
	effectiveJSON, err := encodeJSON(input.Effective)
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		update track_metadata set overrides = $3::jsonb, updated_by = $4,
			version = $5, updated_at = now()
		where track_id = $1 and version = $2`, input.TrackID, input.PreviousVersion,
		string(overridesJSON), input.ActorID, input.NextVersion)
	if err != nil {
		return fmt.Errorf("update track metadata: %w", err)
	}
	if command.RowsAffected() != 1 {
		return metadataVersionConflict(input.PreviousVersion, input.PreviousVersion+1, "")
	}
	if _, err := tx.Exec(ctx, `
		insert into track_metadata_revisions (
			track_id, metadata_version, action, raw_tags, overrides,
			effective_tags, actor_id, reason
		) values ($1, $2, $3::metadata_revision_action, $4::jsonb, $5::jsonb,
			$6::jsonb, $7, $8)`, input.TrackID, input.NextVersion, input.Action,
		string(rawJSON), string(overridesJSON), string(effectiveJSON), input.ActorID, input.Reason); err != nil {
		return fmt.Errorf("insert track metadata revision: %w", err)
	}
	if err := projectMetadata(ctx, tx, input.TrackID, input.Effective, input.PreviousEffective, "MANUAL"); err != nil {
		return err
	}
	details := map[string]any{
		"metadataVersion": input.NextVersion,
		"changedFields":   MetadataChangedFields(input.PreviousEffective, input.Effective),
		"reason":          input.Reason,
	}
	if input.ResetFields != nil {
		details["resetFields"] = input.ResetFields
	}
	if input.BatchSize > 0 {
		details["batchSize"] = input.BatchSize
	}
	if input.RestoredRevisionID != "" {
		details["restoredRevisionId"] = input.RestoredRevisionID
		details["restoredMetadataVersion"] = input.RestoredMetadataVersion
	}
	targetID := input.TrackID
	if err := insertAudit(ctx, tx, auditWrite{
		ActorID: &input.ActorID, Action: input.AuditAction, TargetType: "track_metadata",
		TargetID: &targetID, Result: "SUCCESS", TraceID: input.TraceID, Details: details,
	}); err != nil {
		return err
	}
	return nil
}

func mutationSnapshots(
	row MetadataRecord,
	input MetadataMutationInput,
) (MetadataSnapshot, MetadataOverrides, MetadataSnapshot, MetadataOverrides, MetadataSnapshot, error) {
	raw, err := decodeSnapshot(row.Raw)
	if err != nil {
		return MetadataSnapshot{}, nil, MetadataSnapshot{}, nil, MetadataSnapshot{}, err
	}
	currentOverrides, err := decodeOverrides(row.Overrides)
	if err != nil {
		return MetadataSnapshot{}, nil, MetadataSnapshot{}, nil, MetadataSnapshot{}, err
	}
	nextOverrides, err := UpdateMetadataOverrides(currentOverrides, input.Patch, input.ResetFields)
	if err != nil {
		return MetadataSnapshot{}, nil, MetadataSnapshot{}, nil, MetadataSnapshot{}, err
	}
	previousEffective, err := ApplyMetadataOverrides(raw, currentOverrides)
	if err != nil {
		return MetadataSnapshot{}, nil, MetadataSnapshot{}, nil, MetadataSnapshot{}, err
	}
	nextEffective, err := ApplyMetadataOverrides(raw, nextOverrides)
	if err != nil {
		return MetadataSnapshot{}, nil, MetadataSnapshot{}, nil, MetadataSnapshot{}, err
	}
	return raw, currentOverrides, previousEffective, nextOverrides, nextEffective, nil
}

func lockedMetadata(ctx context.Context, tx pgx.Tx, trackID string) (MetadataRecord, error) {
	var trackStatus string
	if err := tx.QueryRow(ctx, `
		select status::text from tracks where id = $1 for update`, trackID).Scan(&trackStatus); errors.Is(err, pgx.ErrNoRows) {
		return MetadataRecord{}, apperror.NotFound("Track was not found")
	} else if err != nil {
		return MetadataRecord{}, fmt.Errorf("lock track for metadata mutation: %w", err)
	}
	if trackStatus == "ARCHIVED" {
		return MetadataRecord{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Archived tracks are read-only; restore the track before editing metadata",
			map[string]any{"trackId": trackID, "trackStatus": trackStatus},
		)
	}
	record, err := scanMetadata(tx.QueryRow(ctx, `
		select `+metadataColumns+` from track_metadata metadata
		where metadata.track_id = $1 for update`, trackID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return MetadataRecord{}, apperror.NotFound("Track metadata was not found")
	}
	if err != nil {
		return MetadataRecord{}, fmt.Errorf("lock track metadata: %w", err)
	}
	return record, nil
}

func lockedWriteback(ctx context.Context, tx pgx.Tx, jobID string) (WritebackJob, error) {
	job, err := scanWriteback(tx.QueryRow(ctx, `
		select `+writebackColumns+` from metadata_writeback_jobs where id = $1 for update`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WritebackJob{}, apperror.NotFound("Metadata writeback job was not found")
	}
	if err != nil {
		return WritebackJob{}, fmt.Errorf("lock metadata writeback job: %w", err)
	}
	return job, nil
}

func catalogSnapshot(ctx context.Context, database queryer, trackID string) (MetadataSnapshot, error) {
	var title string
	var albumID, albumTitle, releaseDate *string
	var trackNumber, discNumber *int
	var coverAssetID *string
	err := database.QueryRow(ctx, `
		select track.title, track.album_id::text, track.track_number, track.disc_number,
			album.title, album.release_date::text, album.cover_asset_id::text
		from tracks track
		left join albums album on album.id = track.album_id
		where track.id = $1`, trackID).Scan(
		&title, &albumID, &trackNumber, &discNumber,
		&albumTitle, &releaseDate, &coverAssetID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MetadataSnapshot{}, apperror.NotFound("One or more tracks were not found")
	}
	if err != nil {
		return MetadataSnapshot{}, fmt.Errorf("load catalog metadata baseline: %w", err)
	}
	creditRows, err := database.Query(ctx, `
		select artist.name, credit.role::text
		from track_artists credit join artists artist on artist.id = credit.artist_id
		where credit.track_id = $1
		order by credit.sort_order, artist.name`, trackID)
	if err != nil {
		return MetadataSnapshot{}, fmt.Errorf("load catalog metadata credits: %w", err)
	}
	credits := make([]MetadataCredit, 0)
	for creditRows.Next() {
		var credit MetadataCredit
		if err := creditRows.Scan(&credit.Name, &credit.Role); err != nil {
			creditRows.Close()
			return MetadataSnapshot{}, fmt.Errorf("scan catalog metadata credit: %w", err)
		}
		credits = append(credits, credit)
	}
	creditRows.Close()
	albumArtists := make([]string, 0)
	if albumID != nil {
		rows, err := database.Query(ctx, `
			select artist.name
			from album_artists credit join artists artist on artist.id = credit.artist_id
			where credit.album_id = $1 and credit.role = 'PRIMARY'
			order by credit.sort_order, artist.name`, *albumID)
		if err != nil {
			return MetadataSnapshot{}, fmt.Errorf("load catalog album artists: %w", err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return MetadataSnapshot{}, fmt.Errorf("scan catalog album artist: %w", err)
			}
			albumArtists = append(albumArtists, name)
		}
		rows.Close()
	}
	var lyrics *MetadataLyrics
	var lyric MetadataLyrics
	err = database.QueryRow(ctx, `
		select content, format::text, language from lyrics
		where track_id = $1 and content is not null
		order by is_default desc, created_at, id limit 1`, trackID).Scan(
		&lyric.Content, &lyric.Format, &lyric.Language)
	if err == nil {
		lyrics = &lyric
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return MetadataSnapshot{}, fmt.Errorf("load catalog lyrics: %w", err)
	}
	value := map[string]any{
		"title": title, "credits": credits, "albumArtists": albumArtists,
		"album": albumTitle, "releaseDate": releaseDate,
		"trackNumber": trackNumber, "trackTotal": nil,
		"discNumber": discNumber, "discTotal": nil, "genres": []string{},
		"bpm": nil, "isrc": nil, "comment": nil, "copyright": nil,
		"lyrics": lyrics, "hasArtwork": coverAssetID != nil,
	}
	return NormalizeMetadataSnapshot(value)
}

func projectMetadata(
	ctx context.Context,
	tx pgx.Tx,
	trackID string,
	metadata MetadataSnapshot,
	previous MetadataSnapshot,
	lyricOrigin string,
) error {
	var currentAlbumID, currentCoverID *string
	err := tx.QueryRow(ctx, `
		select track.album_id::text, album.cover_asset_id::text
		from tracks track left join albums album on album.id = track.album_id
		where track.id = $1`, trackID).Scan(&currentAlbumID, &currentCoverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Track was not found")
	}
	if err != nil {
		return fmt.Errorf("load metadata projection track: %w", err)
	}

	artistIDs := make(map[string]string)
	for _, credit := range metadata.Credits {
		if err := resolveArtist(ctx, tx, artistIDs, credit.Name); err != nil {
			return err
		}
	}
	for _, name := range metadata.AlbumArtists {
		if err := resolveArtist(ctx, tx, artistIDs, name); err != nil {
			return err
		}
	}

	var nextAlbumID *string
	if metadata.Album != nil {
		normalizedTitle := normalizeLookup(*metadata.Album)
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, normalizedTitle); err != nil {
			return fmt.Errorf("lock projected album identity: %w", err)
		}
		desiredArtists := make([]string, 0, len(metadata.AlbumArtists))
		for _, name := range metadata.AlbumArtists {
			desiredArtists = append(desiredArtists, artistIDs[normalizeLookup(name)])
		}
		rows, err := tx.Query(ctx, `
			select id::text, release_date::text, cover_asset_id::text
			from albums where normalized_title = $1 order by id for update`, normalizedTitle)
		if err != nil {
			return fmt.Errorf("load projected album candidates: %w", err)
		}
		type albumCandidate struct {
			id                   string
			releaseDate, coverID *string
		}
		candidates := make([]albumCandidate, 0)
		for rows.Next() {
			var candidate albumCandidate
			if err := rows.Scan(&candidate.id, &candidate.releaseDate, &candidate.coverID); err != nil {
				rows.Close()
				return fmt.Errorf("scan projected album candidate: %w", err)
			}
			candidates = append(candidates, candidate)
		}
		rows.Close()
		var selected *albumCandidate
		for index := range candidates {
			artistRows, err := tx.Query(ctx, `
				select artist_id::text from album_artists
				where album_id = $1 and role = 'PRIMARY' order by sort_order`, candidates[index].id)
			if err != nil {
				return fmt.Errorf("load projected album credits: %w", err)
			}
			actual := make([]string, 0)
			for artistRows.Next() {
				var id string
				if err := artistRows.Scan(&id); err != nil {
					artistRows.Close()
					return fmt.Errorf("scan projected album credit: %w", err)
				}
				actual = append(actual, id)
			}
			artistRows.Close()
			if stringSlicesEqual(actual, desiredArtists) &&
				(selected == nil || (currentAlbumID != nil && candidates[index].id == *currentAlbumID)) {
				selected = &candidates[index]
				if currentAlbumID != nil && candidates[index].id == *currentAlbumID {
					break
				}
			}
		}
		releaseDate := catalogReleaseDate(metadata.ReleaseDate)
		if selected != nil {
			nextAlbumID = &selected.id
			if !pointerStringEqual(selected.releaseDate, releaseDate) {
				if _, err := tx.Exec(ctx, `update albums set release_date = $2::date,
					version = version + 1, updated_at = now() where id = $1`, selected.id, releaseDate); err != nil {
					return fmt.Errorf("update projected album release date: %w", err)
				}
			}
			if metadata.HasArtwork && currentCoverID != nil && selected.coverID == nil {
				if _, err := tx.Exec(ctx, `update albums set cover_asset_id = $2,
					version = version + 1, updated_at = now() where id = $1`, selected.id, *currentCoverID); err != nil {
					return fmt.Errorf("preserve projected album artwork: %w", err)
				}
			}
		} else {
			var createdID string
			var coverID *string
			if metadata.HasArtwork {
				coverID = currentCoverID
			}
			if err := tx.QueryRow(ctx, `
				insert into albums (title, normalized_title, release_date, cover_asset_id)
				values ($1, $2, $3::date, $4) returning id::text`,
				*metadata.Album, normalizedTitle, releaseDate, coverID).Scan(&createdID); err != nil {
				return fmt.Errorf("create projected album: %w", err)
			}
			nextAlbumID = &createdID
			for order, artistID := range desiredArtists {
				if _, err := tx.Exec(ctx, `
					insert into album_artists (album_id, artist_id, role, sort_order)
					values ($1, $2, 'PRIMARY', $3)`, createdID, artistID, order); err != nil {
					return fmt.Errorf("create projected album artist: %w", err)
				}
			}
		}
	}
	if _, err := tx.Exec(ctx, `
		update tracks set title = $2, normalized_title = $3, album_id = $4,
			track_number = $5, disc_number = $6, version = version + 1, updated_at = now()
		where id = $1`, trackID, metadata.Title, normalizeLookup(metadata.Title), nextAlbumID,
		metadata.TrackNumber, metadata.DiscNumber); err != nil {
		return fmt.Errorf("project metadata onto track: %w", err)
	}
	if currentAlbumID != nil && (nextAlbumID == nil || *nextAlbumID != *currentAlbumID) {
		if err := deleteAlbumIfEmpty(ctx, tx, *currentAlbumID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `delete from track_artists where track_id = $1`, trackID); err != nil {
		return fmt.Errorf("replace projected track artists: %w", err)
	}
	for order, credit := range metadata.Credits {
		if _, err := tx.Exec(ctx, `
			insert into track_artists (track_id, artist_id, role, sort_order)
			values ($1, $2, $3::artist_credit_role, $4)`,
			trackID, artistIDs[normalizeLookup(credit.Name)], string(credit.Role), order); err != nil {
			return fmt.Errorf("create projected track artist: %w", err)
		}
	}
	if err := projectLyrics(ctx, tx, trackID, metadata.Lyrics, previous.Lyrics, lyricOrigin); err != nil {
		return err
	}
	return nil
}

func resolveArtist(ctx context.Context, tx pgx.Tx, cache map[string]string, name string) error {
	lookup := normalizeLookup(name)
	if _, found := cache[lookup]; found {
		return nil
	}
	var id string
	err := tx.QueryRow(ctx, `
		select id::text from artists where normalized_name = $1 order by id limit 1`, lookup).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			insert into artists (name, normalized_name) values ($1, $2) returning id::text`, name, lookup).Scan(&id)
	}
	if err != nil {
		return fmt.Errorf("resolve metadata projection artist: %w", err)
	}
	cache[lookup] = id
	return nil
}

func projectLyrics(
	ctx context.Context,
	tx pgx.Tx,
	trackID string,
	next, previous *MetadataLyrics,
	origin string,
) error {
	if previous != nil && (next == nil || next.Language != previous.Language) {
		if _, err := tx.Exec(ctx, `
			delete from lyrics where track_id = $1 and language = $2 and format = $3::lyrics_format
				and content = $4 and asset_id is null`, trackID, previous.Language, previous.Format, previous.Content); err != nil {
			return fmt.Errorf("remove previous projected lyrics: %w", err)
		}
	}
	if next != nil {
		if _, err := tx.Exec(ctx, `update lyrics set is_default = false, updated_at = now()
			where track_id = $1`, trackID); err != nil {
			return fmt.Errorf("clear projected default lyrics: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into lyrics (track_id, format, language, origin, content, is_default)
			values ($1, $2::lyrics_format, $3, $4::lyrics_origin, $5, true)
			on conflict (track_id, language) do update set
				format = excluded.format, content = excluded.content, origin = excluded.origin,
				asset_id = null, is_default = true, version = lyrics.version + 1, updated_at = now()`,
			trackID, next.Format, next.Language, origin, next.Content); err != nil {
			return fmt.Errorf("upsert projected lyrics: %w", err)
		}
		return nil
	}
	if previous == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `update lyrics set is_default = false, updated_at = now()
		where track_id = $1`, trackID); err != nil {
		return fmt.Errorf("clear removed projected lyrics: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update lyrics set is_default = true, updated_at = now()
		where id = (
			select id from lyrics where track_id = $1 order by created_at, id limit 1
		)`, trackID); err != nil {
		return fmt.Errorf("select fallback projected lyrics: %w", err)
	}
	return nil
}

func deleteAlbumIfEmpty(ctx context.Context, tx pgx.Tx, albumID string) error {
	var coverID *string
	err := tx.QueryRow(ctx, `
		delete from albums album where album.id = $1
		  and not exists (select 1 from tracks where album_id = album.id)
		returning cover_asset_id::text`, albumID).Scan(&coverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete empty metadata album: %w", err)
	}
	if coverID == nil {
		return nil
	}
	var objectKey string
	err = tx.QueryRow(ctx, `
		update media_assets asset set status = 'DELETE_PENDING', updated_at = now()
		where asset.id = $1
		  and not exists (select 1 from artists where artwork_asset_id = asset.id)
		  and not exists (select 1 from albums where cover_asset_id = asset.id)
		  and not exists (select 1 from playlists where cover_asset_id = asset.id)
		  and not exists (select 1 from user_profiles where avatar_asset_id = asset.id)
		returning object_key`, *coverID).Scan(&objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("detach empty album artwork: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into object_cleanup_jobs (object_key, reason)
		values ($1, 'EMPTY_ALBUM_AFTER_METADATA_UPDATE')
		on conflict (object_key) do update set
			reason = excluded.reason, status = 'PENDING', attempts = 0, attempt_id = null,
			locked_by = null, locked_until = null, next_attempt_at = now(),
			last_error = null, updated_at = now()`, objectKey); err != nil {
		return fmt.Errorf("queue empty album artwork cleanup: %w", err)
	}
	return nil
}

type auditWrite struct {
	ActorID    *string
	Action     string
	TargetType string
	TargetID   *string
	Result     string
	TraceID    string
	Details    map[string]any
}

func insertAudit(ctx context.Context, database executor, input auditWrite) error {
	details, err := json.Marshal(input.Details)
	if err != nil {
		return fmt.Errorf("encode metadata audit details: %w", err)
	}
	if _, err := database.Exec(ctx, `
		insert into audit_logs (actor_id, action, target_type, target_id, result, trace_id, details)
		values ($1, $2, $3, $4, $5::audit_result, $6, $7::jsonb)`,
		input.ActorID, input.Action, input.TargetType, input.TargetID,
		input.Result, input.TraceID, string(details)); err != nil {
		return fmt.Errorf("write metadata audit log: %w", err)
	}
	return nil
}

func assertWritableSource(rootMode string, enabled bool, rootStatus, sourceStatus string) error {
	return tagwriteback.Evaluate(tagwriteback.SourceContext{
		HasSource: true, RootMode: rootMode, RootEnabled: enabled,
		RootStatus: rootStatus, SourceStatus: sourceStatus,
	}).Error("")
}

func metadataVersionConflict(expected, current int, trackID string) error {
	metadata := map[string]any{"expectedVersion": expected, "currentVersion": current}
	if trackID != "" {
		metadata["trackId"] = trackID
	}
	return apperror.Conflict(
		apperror.CodeVersionConflict,
		"Track metadata version is stale",
		metadata,
	)
}

func writebackVersionConflict(expected, current int) error {
	return apperror.Conflict(
		apperror.CodeVersionConflict,
		"Metadata writeback job version is stale",
		map[string]any{"expectedVersion": expected, "currentVersion": current},
	)
}

func validateWritebackCancellation(job WritebackJob) error {
	if job.Status != WritebackPending && job.Status != WritebackProcessing {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Only an active writeback can be cancelled",
			nil,
		)
	}
	if job.Stage == StageCommitted {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Tag writeback has already been committed and is completing temporary-file cleanup",
			nil,
		)
	}
	return nil
}

func SupportsMetadataWriteback(path string) bool {
	return tagwriteback.Supports(path)
}

func catalogReleaseDate(value *string) *string {
	if value == nil || len(*value) == 10 {
		return value
	}
	converted := *value + "-01"
	if len(*value) == 4 {
		converted += "-01"
	}
	return &converted
}

func uniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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

func pointerStringEqual(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

type scanRow interface{ Scan(...any) error }

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func scanMetadata(row scanRow, withSource bool) (MetadataRecord, error) {
	var record MetadataRecord
	destinations := []any{
		&record.TrackID, &record.SourceID, &record.Raw, &record.Overrides,
		&record.RawChecksum, &record.LastScannedAt, &record.UpdatedBy,
		&record.Version, &record.CreatedAt, &record.UpdatedAt,
	}
	var sourceID, rootID, sourcePath, sourceStatus, checksum *string
	var rootPath, rootMode, rootStatus, trackStatus *string
	var rootEnabled *bool
	var mappingCount int
	var cue bool
	if withSource {
		destinations = append(destinations,
			&sourceID, &rootID, &sourcePath, &sourceStatus, &checksum,
			&rootPath, &rootMode, &rootEnabled, &rootStatus, &trackStatus,
			&mappingCount, &cue,
		)
	}
	if err := row.Scan(destinations...); err != nil {
		return MetadataRecord{}, err
	}
	if withSource && sourceID != nil && sourcePath != nil && sourceStatus != nil && checksum != nil {
		record.Source = &MetadataSourceRecord{
			ID: *sourceID, RootID: rootID, SourcePath: *sourcePath, Status: *sourceStatus,
			ChecksumSHA256: *checksum, RootPath: rootPath, RootMode: rootMode,
			RootEnabled: rootEnabled, RootStatus: rootStatus, TrackStatus: trackStatus,
			MappingCount: mappingCount, Cue: cue,
		}
	}
	return record, nil
}

func scanRevision(row scanRow) (RevisionRecord, error) {
	var revision RevisionRecord
	err := row.Scan(
		&revision.ID, &revision.TrackID, &revision.MetadataVersion, &revision.Action,
		&revision.Raw, &revision.Overrides, &revision.Effective,
		&revision.ActorID, &revision.Reason, &revision.CreatedAt,
	)
	return revision, err
}

func scanWriteback(row scanRow) (WritebackJob, error) {
	var job WritebackJob
	err := row.Scan(
		&job.ID, &job.TrackID, &job.SourceID, &job.RevisionID, &job.RequestedBy,
		&job.Reason, &job.MetadataSnapshot, &job.MetadataVersion,
		&job.ExpectedSourceChecksum, &job.RootPathSnapshot, &job.SourcePathSnapshot,
		&job.Status, &job.Attempts, &job.MaxAttempts,
		&job.Version, &job.CancelRequested, &job.AttemptID, &job.Stage,
		&job.LockedBy, &job.LockedUntil, &job.NextAttemptAt, &job.StartedAt,
		&job.CompletedAt, &job.BackupPath, &job.BackupExpiresAt,
		&job.OutputChecksumSHA256, &job.LastErrorCode, &job.LastError,
		&job.CreatedAt, &job.UpdatedAt,
	)
	return job, err
}

const metadataColumns = `
	metadata.track_id::text, metadata.source_id::text, metadata.raw_tags,
	metadata.overrides, metadata.raw_checksum_sha256, metadata.last_scanned_at,
	metadata.updated_by::text, metadata.version, metadata.created_at, metadata.updated_at`

const revisionColumns = `
	id::text, track_id::text, metadata_version, action::text, raw_tags,
	overrides, effective_tags, actor_id::text, reason, created_at`

const writebackColumns = `
	id::text, track_id::text, source_id::text, revision_id::text, requested_by::text,
	reason, metadata_snapshot, metadata_version, expected_source_checksum,
	root_path_snapshot, source_path_snapshot,
	status::text, attempts, max_attempts, version, cancel_requested, attempt_id::text,
	stage, locked_by, locked_until, next_attempt_at, started_at, completed_at,
	backup_path, backup_expires_at, output_checksum_sha256, last_error_code,
	last_error, created_at, updated_at`
