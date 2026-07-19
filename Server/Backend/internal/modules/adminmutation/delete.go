package adminmutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

type stagedSourceFile struct {
	sourcePath, stagedPath string
}

type writebackDeletionRecord struct {
	id, status string
	workerHeld bool
}

func (repository *Repository) DeleteTrackPermanently(
	ctx context.Context,
	id string,
	expectedVersion int,
	defaultLibraryDirectory string,
) (DeleteResult, error) {
	var albumID *string
	var version int
	var status string
	err := repository.pool.QueryRow(ctx, `SELECT album_id,version,status::text FROM tracks WHERE id=$1`, id).Scan(&albumID, &version, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeleteResult{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return DeleteResult{}, fmt.Errorf("query permanently deleted track: %w", err)
	}
	if version != expectedVersion {
		return DeleteResult{}, versionConflict("Track", expectedVersion, version, nil)
	}
	if status != "ARCHIVED" {
		return DeleteResult{}, apperror.Conflict(apperror.CodeInvalidStateTransition, "Track must be in the recycle bin before permanent deletion", nil)
	}

	type sourceRecord struct {
		id               string
		rootID, rootPath *string
		sourcePath       string
		trackCount       int
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT source.id,source.root_id,root.path,source.source_path,
		       (SELECT count(*)::int FROM local_music_source_tracks all_mappings WHERE all_mappings.source_id=source.id)
		FROM local_music_source_tracks mapped
		JOIN local_music_sources source ON source.id=mapped.source_id
		LEFT JOIN library_roots root ON root.id=source.root_id
		WHERE mapped.track_id=$1
	`, id)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("query permanent deletion sources: %w", err)
	}
	sources := []sourceRecord{}
	rootIDs := []string{}
	seenRoots := map[string]struct{}{}
	for rows.Next() {
		var source sourceRecord
		if err := rows.Scan(&source.id, &source.rootID, &source.rootPath, &source.sourcePath, &source.trackCount); err != nil {
			rows.Close()
			return DeleteResult{}, err
		}
		sources = append(sources, source)
		if source.rootID != nil {
			if _, seen := seenRoots[*source.rootID]; !seen {
				seenRoots[*source.rootID] = struct{}{}
				rootIDs = append(rootIDs, *source.rootID)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return DeleteResult{}, err
	}
	if len(rootIDs) > 0 {
		var active bool
		if err := repository.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM library_scan_runs WHERE root_id=ANY($1::uuid[]) AND status IN('PENDING','RUNNING'))`, rootIDs).Scan(&active); err != nil {
			return DeleteResult{}, err
		}
		if active {
			return DeleteResult{}, apperror.Conflict(
				apperror.CodeResourceConflict,
				"Wait for the source scan to finish before permanent deletion",
				map[string]any{"conflictResourceType": "library_scan"},
			)
		}
	}
	var mediaActive bool
	if err := repository.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM media_jobs
		WHERE track_id=$1 AND status IN('PENDING','PROCESSING'))`, id).Scan(&mediaActive); err != nil {
		return DeleteResult{}, err
	}
	if mediaActive {
		return DeleteResult{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Wait for media processing to stop before permanent deletion",
			map[string]any{"conflictResourceType": "media_job"},
		)
	}
	sourceFiles := []string{}
	for _, source := range sources {
		if source.trackCount > 1 {
			continue
		}
		root := defaultLibraryDirectory
		if source.rootPath != nil {
			root = *source.rootPath
		}
		path, err := secureSourcePath(root, source.sourcePath)
		if err != nil {
			return DeleteResult{}, err
		}
		sourceFiles = append(sourceFiles, path)
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DeleteResult{}, fmt.Errorf("begin permanent track deletion: %w", err)
	}
	staged := []stagedSourceFile{}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	var lockedAlbum *string
	err = tx.QueryRow(ctx, `SELECT album_id FROM tracks WHERE id=$1 AND version=$2 AND status='ARCHIVED' FOR UPDATE`, id, expectedVersion).Scan(&lockedAlbum)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return DeleteResult{}, repository.versionFailure(ctx, "Track", "tracks", id, expectedVersion)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return DeleteResult{}, err
	}
	writebacks, err := lockTrackWritebacksForDeletion(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		return DeleteResult{}, err
	}
	conflictJobID := ""
	for _, job := range writebacks {
		if job.status == "PROCESSING" || (job.status == "PENDING" && job.workerHeld) {
			conflictJobID = job.id
			break
		}
	}
	if conflictJobID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE metadata_writeback_jobs SET
				cancel_requested = true,
				status = CASE
					WHEN status = 'PENDING'
						AND NOT (locked_by IS NOT NULL AND locked_until > now())
						THEN 'CANCELLED'::metadata_writeback_status
					ELSE status
				END,
				completed_at = CASE
					WHEN status = 'PENDING'
						AND NOT (locked_by IS NOT NULL AND locked_until > now()) THEN now()
					ELSE completed_at
				END,
				locked_by = CASE
					WHEN status = 'PENDING'
						AND NOT (locked_by IS NOT NULL AND locked_until > now()) THEN NULL
					ELSE locked_by
				END,
				locked_until = CASE
					WHEN status = 'PENDING'
						AND NOT (locked_by IS NOT NULL AND locked_until > now()) THEN NULL
					ELSE locked_until
				END,
				version = version + 1,
				updated_at = now()
			WHERE track_id = $1 AND status IN ('PENDING', 'PROCESSING')`, id); err != nil {
			_ = tx.Rollback(ctx)
			return DeleteResult{}, fmt.Errorf("request metadata writeback cancellation before permanent deletion: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return DeleteResult{}, fmt.Errorf("commit metadata writeback cancellation before permanent deletion: %w", err)
		}
		committed = true
		return DeleteResult{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Tag writeback cancellation is pending; retry permanent deletion after the worker stops",
			map[string]any{
				"conflictResourceType":  "metadata_writeback_job",
				"conflictResourceId":    conflictJobID,
				"cancellationRequested": true,
			},
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_writeback_jobs SET
			cancel_requested = true,
			status = 'CANCELLED',
			completed_at = now(),
			locked_by = NULL,
			locked_until = NULL,
			last_error_code = NULL,
			last_error = NULL,
			version = version + 1,
			updated_at = now()
		WHERE track_id = $1 AND status = 'PENDING'`, id); err != nil {
		_ = tx.Rollback(ctx)
		return DeleteResult{}, fmt.Errorf("cancel pending metadata writebacks before permanent deletion: %w", err)
	}
	staged, err = stageSourceFilesForDeletion(sourceFiles, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		return DeleteResult{}, err
	}
	restoreOnError := func(cause error) (DeleteResult, error) {
		_ = tx.Rollback(ctx)
		failures := restoreStagedSourceFiles(staged)
		staged = nil
		if len(failures) > 0 {
			return DeleteResult{}, apperror.Unprocessable(apperror.CodeSourceFileRestoreFailed, "Permanent deletion was rolled back, but staged track files could not be restored", map[string]any{"restoreFailureCount": len(failures)})
		}
		return DeleteResult{}, cause
	}
	assetIDs, err := queryTrackAssetIDs(ctx, tx, id)
	if err != nil {
		return restoreOnError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM playlist_tracks WHERE track_id=$1`, id); err != nil {
		return restoreOnError(err)
	}
	primaryRows, err := tx.Query(ctx, `SELECT id FROM local_music_sources WHERE track_id=$1 FOR UPDATE`, id)
	if err != nil {
		return restoreOnError(err)
	}
	primaryIDs := []string{}
	for primaryRows.Next() {
		var sourceID string
		if err := primaryRows.Scan(&sourceID); err != nil {
			primaryRows.Close()
			return restoreOnError(err)
		}
		primaryIDs = append(primaryIDs, sourceID)
	}
	err = primaryRows.Err()
	primaryRows.Close()
	if err != nil {
		return restoreOnError(err)
	}
	for _, sourceID := range primaryIDs {
		var replacementTrack string
		var replacementJob *string
		err := tx.QueryRow(ctx, `SELECT track_id,media_job_id FROM local_music_source_tracks WHERE source_id=$1 AND track_id<>$2 ORDER BY segment_index LIMIT 1`, sourceID, id).Scan(&replacementTrack, &replacementJob)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return restoreOnError(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE local_music_sources SET track_id=$2,media_job_id=$3,updated_at=now() WHERE id=$1`, sourceID, replacementTrack, replacementJob); err != nil {
			return restoreOnError(err)
		}
	}
	command, err := tx.Exec(ctx, `DELETE FROM tracks WHERE id=$1 AND version=$2 AND status='ARCHIVED'`, id, expectedVersion)
	if err != nil {
		return restoreOnError(err)
	}
	if command.RowsAffected() != 1 {
		return restoreOnError(apperror.Conflict(apperror.CodeVersionConflict, "Track changed during permanent deletion", nil))
	}
	if _, err := deleteAlbumIfEmpty(ctx, tx, stringValue(lockedAlbum), "EMPTY_ALBUM_AFTER_TRACK_DELETE"); err != nil {
		return restoreOnError(err)
	}
	scheduled := 0
	if len(assetIDs) > 0 {
		assetRows, err := tx.Query(ctx, `UPDATE media_assets asset SET status='DELETE_PENDING',updated_at=now() WHERE id=ANY($1::uuid[]) AND NOT EXISTS(SELECT 1 FROM track_variants WHERE asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM media_jobs WHERE source_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM local_music_sources WHERE source_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM lyrics WHERE asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM media_uploads WHERE asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM artists WHERE artwork_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM albums WHERE cover_asset_id=asset.id) AND NOT EXISTS(SELECT 1 FROM user_profiles WHERE avatar_asset_id=asset.id) RETURNING object_key`, assetIDs)
		if err != nil {
			return restoreOnError(err)
		}
		objectKeys := []string{}
		for assetRows.Next() {
			var key string
			if err := assetRows.Scan(&key); err != nil {
				assetRows.Close()
				return restoreOnError(err)
			}
			objectKeys = append(objectKeys, key)
		}
		err = assetRows.Err()
		assetRows.Close()
		if err != nil {
			return restoreOnError(err)
		}
		for _, key := range objectKeys {
			if _, err := tx.Exec(ctx, `INSERT INTO object_cleanup_jobs(object_key,reason) VALUES($1,'TRACK_PERMANENT_DELETE') ON CONFLICT(object_key) DO NOTHING`, key); err != nil {
				return restoreOnError(err)
			}
		}
		scheduled = len(objectKeys)
	}
	if err := tx.Commit(ctx); err != nil {
		return restoreOnError(err)
	}
	committed = true
	cleanupFailures := finalizeStagedSourceFiles(staged)
	deletedFiles, quarantinedFiles := reportedDeletionCounts(staged, cleanupFailures)
	return DeleteResult{DeletedFiles: deletedFiles, QuarantinedFiles: quarantinedFiles, ScheduledObjects: scheduled}, nil
}

func lockTrackWritebacksForDeletion(
	ctx context.Context,
	tx pgx.Tx,
	trackID string,
) ([]writebackDeletionRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, status::text,
		       locked_by IS NOT NULL AND locked_until > now()
		FROM metadata_writeback_jobs
		WHERE track_id = $1
		ORDER BY created_at, id
		FOR UPDATE`, trackID)
	if err != nil {
		return nil, fmt.Errorf("lock metadata writebacks before permanent deletion: %w", err)
	}
	defer rows.Close()
	records := make([]writebackDeletionRecord, 0)
	for rows.Next() {
		var record writebackDeletionRecord
		if err := rows.Scan(
			&record.id, &record.status, &record.workerHeld,
		); err != nil {
			return nil, fmt.Errorf("scan metadata writeback before permanent deletion: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metadata writebacks before permanent deletion: %w", err)
	}
	return records, nil
}

func queryTrackAssetIDs(ctx context.Context, tx pgx.Tx, trackID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT asset_id FROM track_variants WHERE track_id=$1 UNION SELECT source_asset_id FROM media_jobs WHERE track_id=$1 UNION SELECT source.source_asset_id FROM local_music_source_tracks mapped JOIN local_music_sources source ON source.id=mapped.source_id WHERE mapped.track_id=$1 UNION SELECT asset_id FROM lyrics WHERE track_id=$1 UNION SELECT asset_id FROM media_uploads WHERE track_id=$1`, trackID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		if id != nil {
			ids = append(ids, *id)
		}
	}
	err = rows.Err()
	rows.Close()
	return ids, err
}

func secureSourcePath(rootPath, sourcePath string) (string, error) {
	root, err := filepath.Abs(rootPath)
	if err != nil {
		return "", err
	}
	candidate := sourcePath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !pathWithin(root, candidate, false) {
		return "", apperror.Forbidden("Source file path is outside the configured music directory")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	allowRoot := false
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		resolved, err = nearestExistingPath(filepath.Dir(candidate))
		allowRoot = true
		if err != nil {
			return "", err
		}
	}
	if !pathWithin(realRoot, resolved, allowRoot) {
		return "", apperror.Forbidden("Source file path is outside the configured music directory")
	}
	return candidate, nil
}
func nearestExistingPath(path string) (string, error) {
	candidate := path
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		candidate = parent
	}
}
func pathWithin(root, candidate string, allowRoot bool) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return (allowRoot || relative != ".") && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func stageSourceFilesForDeletion(paths []string, trackID string) ([]stagedSourceFile, error) {
	return stageDeletionFiles(paths, trackID)
}

func stageDeletionFiles(paths []string, trackID string) ([]stagedSourceFile, error) {
	staged := []stagedSourceFile{}
	seen := map[string]struct{}{}
	for _, source := range paths {
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		entry, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			failures := restoreStagedSourceFiles(staged)
			return nil, apperror.Unprocessable(apperror.CodeSourceFileDeleteFailed, "A track file could not be inspected before deletion", map[string]any{"restoreFailureCount": len(failures)})
		}
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
			failures := restoreStagedSourceFiles(staged)
			return nil, apperror.Unprocessable(apperror.CodeSourceFileDeleteFailed, "Only regular track files can be staged for deletion", map[string]any{"restoreFailureCount": len(failures)})
		}
		target := filepath.Join(filepath.Dir(source), ".xymusic-delete-"+trackID+"-"+uuid.NewString())
		if err := os.Rename(source, target); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			failures := restoreStagedSourceFiles(staged)
			return nil, apperror.Unprocessable(apperror.CodeSourceFileDeleteFailed, "A track file could not be staged for deletion", map[string]any{"restoreFailureCount": len(failures)})
		}
		staged = append(staged, stagedSourceFile{sourcePath: source, stagedPath: target})
		stagedEntry, err := os.Lstat(target)
		if err != nil || stagedEntry.Mode()&os.ModeSymlink != 0 || !stagedEntry.Mode().IsRegular() {
			failures := restoreStagedSourceFiles(staged)
			return nil, apperror.Unprocessable(apperror.CodeSourceFileDeleteFailed, "A staged track file failed safety validation", map[string]any{"restoreFailureCount": len(failures)})
		}
	}
	return staged, nil
}
func restoreStagedSourceFiles(staged []stagedSourceFile) []string {
	failures := []string{}
	for index := len(staged) - 1; index >= 0; index-- {
		file := staged[index]
		if err := os.Rename(file.stagedPath, file.sourcePath); err != nil {
			failures = append(failures, file.sourcePath)
		}
	}
	return failures
}
func finalizeStagedSourceFiles(staged []stagedSourceFile) []string {
	failures := []string{}
	for _, file := range staged {
		if err := os.Remove(file.stagedPath); err != nil && !os.IsNotExist(err) {
			failures = append(failures, file.stagedPath)
		}
	}
	return failures
}

func reportedDeletionCounts(staged []stagedSourceFile, cleanupFailures []string) (int, int) {
	failed := make(map[string]struct{}, len(cleanupFailures))
	for _, path := range cleanupFailures {
		failed[path] = struct{}{}
	}
	deleted := 0
	for _, file := range staged {
		if _, exists := failed[file.stagedPath]; exists {
			continue
		}
		deleted++
	}
	return deleted, len(cleanupFailures)
}
