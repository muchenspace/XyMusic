package adminsources

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/platform/mediafile"
)

func (synchronizer *ProductionSynchronizer) probeFile(
	ctx context.Context,
	path string,
) (mediafile.ProbedMetadataFile, error) {
	if synchronizer.probeBudget != nil {
		if err := synchronizer.probeBudget.Acquire(ctx, 1); err != nil {
			return mediafile.ProbedMetadataFile{}, err
		}
		defer synchronizer.probeBudget.Release(1)
	}
	if synchronizer.probeGate != nil {
		select {
		case <-ctx.Done():
			return mediafile.ProbedMetadataFile{}, ctx.Err()
		case synchronizer.probeGate <- struct{}{}:
			defer func() { <-synchronizer.probeGate }()
		}
	}
	return synchronizer.probe.Probe(ctx, path)
}

func (synchronizer *ProductionSynchronizer) rootPath(ctx context.Context, rootID string) (string, error) {
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil && snapshot.rootPath != "" {
		return snapshot.rootPath, nil
	}
	var path string
	err := synchronizer.database.QueryRow(ctx, `
		SELECT path FROM library_roots WHERE id = $1`, rootID).Scan(&path)
	if err != nil {
		return "", fmt.Errorf("resolve music root path: %w", err)
	}
	return path, nil
}

func (synchronizer *ProductionSynchronizer) findSource(
	ctx context.Context,
	rootID string,
	normalizedPath string,
) (localSourceRecord, bool, error) {
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		source, found := snapshot.findSource(normalizedPath)
		return source, found, nil
	}
	row := synchronizer.database.QueryRow(ctx, `
		SELECT `+localSourceColumnsWithTrack+`
		FROM local_music_sources source
		LEFT JOIN local_music_source_tracks track_link ON track_link.source_id = source.id
		WHERE source.root_id = $1 AND source.normalized_source_path = $2`, rootID, normalizedPath)
	source, err := scanLocalSourceWithTrack(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return localSourceRecord{}, false, nil
	}
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("find local music source: %w", err)
	}
	return source, true, nil
}

func (synchronizer *ProductionSynchronizer) findRenameCandidates(
	ctx context.Context,
	rootID string,
	checksum string,
	seenAt time.Time,
) ([]localSourceRecord, error) {
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		candidates := snapshot.renameCandidates[checksum]
		valid := make([]localSourceRecord, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate == nil {
				continue
			}
			if !candidate.LastSeenAt.Equal(seenAt) && candidate.Status == SourceFileReady {
				valid = append(valid, *candidate)
			}
		}
		return valid, nil
	}
	rows, err := synchronizer.database.Query(ctx, `
		SELECT `+localSourceColumnsWithTrack+`
		FROM local_music_sources source
		LEFT JOIN local_music_source_tracks track_link ON track_link.source_id = source.id
		WHERE source.root_id = $1 AND source.checksum_sha256 = $2 AND source.last_seen_at <> $3
		  AND source.status = 'READY'`, rootID, checksum, seenAt)
	if err != nil {
		return nil, fmt.Errorf("find rename candidate sources: %w", err)
	}
	defer rows.Close()
	var candidates []localSourceRecord
	for rows.Next() {
		s, err := scanLocalSourceWithTrack(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, s)
	}
	return candidates, rows.Err()
}

func (synchronizer *ProductionSynchronizer) sourceHasExternalLyrics(
	ctx context.Context,
	sourceID string,
) (bool, error) {
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		_, exists := snapshot.externalLyricsByID[sourceID]
		return exists, nil
	}
	var hasExternal bool
	err := synchronizer.database.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM lyrics lyric
			JOIN local_music_source_tracks mapping ON mapping.track_id = lyric.track_id
			WHERE mapping.source_id = $1 AND lyric.origin = 'EXTERNAL'
		)`, sourceID).Scan(&hasExternal)
	if err != nil {
		return false, fmt.Errorf("check external lyrics: %w", err)
	}
	return hasExternal, nil
}

func sourceFailurePath(file DiscoveredFile) string {
	path := strings.TrimSpace(file.RelativePath)
	if path == "" {
		path = filepath.Base(strings.TrimSpace(file.AudioPath))
	}
	return normalizePlatformPath(path)
}

func (synchronizer *ProductionSynchronizer) markSourceFailed(
	ctx context.Context,
	rootID string,
	normalizedPath string,
	failure error,
	seenAt time.Time,
) error {
	now := time.Now().UTC()
	if synchronizer.now != nil {
		now = synchronizer.now().UTC()
	}
	errorMessage := "source synchronization failed"
	if failure != nil {
		errorMessage = failure.Error()
	}
	// Keep the failing source path in the persisted error. Per-file scan
	// failures are intentionally isolated so the overall scan can continue,
	// which means the generic scan error alone is otherwise not actionable in
	// the admin UI.
	displayPath := strings.TrimSpace(strings.ReplaceAll(normalizedPath, "\\", "/"))
	if displayPath != "" && !strings.Contains(errorMessage, displayPath) {
		errorMessage = fmt.Sprintf("源文件 %q 分析失败：%s", displayPath, errorMessage)
	}
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failed local library source synchronization: %w", err)
	}
	defer transaction.Rollback(ctx)
	var previousSourceUpdatedAt *time.Time
	if lookupErr := transaction.QueryRow(ctx, `
		SELECT updated_at FROM local_music_sources
		WHERE root_id IS NOT DISTINCT FROM $1 AND normalized_source_path = $2`, rootID, normalizedPath).Scan(&previousSourceUpdatedAt); lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return fmt.Errorf("inspect failed local library source: %w", lookupErr)
	}
	var sourceID string
	err = transaction.QueryRow(ctx, `
		INSERT INTO local_music_sources (
			root_id, source_path, normalized_source_path, checksum_sha256, size_bytes,
			modified_at, status, last_error, last_seen_at, updated_at
		) VALUES ($1, $2, $2, '', 0, $3, 'FAILED', $4, $5, $3)
		ON CONFLICT (root_id, normalized_source_path) DO UPDATE SET
			status = 'FAILED', last_error = EXCLUDED.last_error,
			last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at
		RETURNING id::text`, rootID, normalizedPath, now, errorMessage, seenAt).Scan(&sourceID)
	if err != nil {
		return fmt.Errorf("record failed local library source: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE tracks
		SET status = 'ERROR', version = version + 1, updated_at = $3
		WHERE id IN (
			SELECT track_id FROM local_music_source_tracks WHERE source_id = $1
		) AND (
			status <> 'ARCHIVED'
			OR (status = 'ARCHIVED' AND NOT archived_manually
				AND $2::timestamptz IS NOT NULL AND updated_at = $2::timestamptz)
		)`, sourceID, previousSourceUpdatedAt, now); err != nil {
		return fmt.Errorf("mark failed local library tracks: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed local library source synchronization: %w", err)
	}
	return nil
}
