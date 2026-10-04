package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"xymusic/server/internal/platform/mediafile"
)

func (synchronizer *ProductionSynchronizer) FlushScan(
	ctx context.Context,
	rootID string,
	seenAt time.Time,
) error {
	snapshot := sourceScanSnapshotFromContext(ctx)
	if snapshot == nil {
		return nil
	}
	snapshot.seenSourcesMu.Lock()
	seenIDs := make([]string, 0, len(snapshot.seenSourceIDs))
	for id := range snapshot.seenSourceIDs {
		seenIDs = append(seenIDs, id)
	}
	snapshot.seenSourcesMu.Unlock()

	if len(seenIDs) > 0 {
		now := synchronizer.now()
		const batchSize = 10_000
		for i := 0; i < len(seenIDs); i += batchSize {
			end := min(i+batchSize, len(seenIDs))
			chunk := seenIDs[i:end]
			if _, err := synchronizer.database.Exec(ctx, `
				UPDATE local_music_sources
				SET last_seen_at = $2, updated_at = $3
				WHERE id = ANY($1::uuid[]) AND last_seen_at < $2`, chunk, seenAt, now); err != nil {
				return fmt.Errorf("flush local library seen sources: %w", err)
			}
		}
	}
	return nil
}

// DeleteMissing removes database records for files that were not seen in a
// successfully completed scan. It intentionally does not touch any source file.
func (synchronizer *ProductionSynchronizer) DeleteMissing(
	ctx context.Context,
	rootID string,
	seenCutoff time.Time,
	deletedAt time.Time,
) (int, error) {
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin delete missing sources: %w", err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))

	// Never materialize every missing source from a large root. Deleting in
	// bounded chunks keeps both the Go heap and PostgreSQL array parameters
	// predictable when a library was moved or a mount temporarily disappeared.
	const batchSize = 5_000
	deletedTracks := 0
	for {
		rows, err := transaction.Query(ctx, `
			SELECT id::text
			FROM local_music_sources
			WHERE root_id = $1 AND last_seen_at < $2
			ORDER BY id
			LIMIT $3
			FOR UPDATE SKIP LOCKED`, rootID, seenCutoff, batchSize)
		if err != nil {
			return 0, fmt.Errorf("list missing sources for deletion: %w", err)
		}
		sourceIDs := make([]string, 0, batchSize)
		for rows.Next() {
			var sourceID string
			if err := rows.Scan(&sourceID); err != nil {
				rows.Close()
				return 0, fmt.Errorf("scan missing source for deletion: %w", err)
			}
			sourceIDs = append(sourceIDs, sourceID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, fmt.Errorf("iterate missing sources for deletion: %w", err)
		}
		rows.Close()
		if len(sourceIDs) == 0 {
			break
		}

		trackIDs, err := querySourceTrackIDs(ctx, transaction, sourceIDs)
		if err != nil {
			return 0, err
		}
		if _, err := transaction.Exec(ctx, `DELETE FROM local_music_sources WHERE id = ANY($1::uuid[])`, sourceIDs); err != nil {
			return 0, fmt.Errorf("delete missing source records: %w", err)
		}
		deleted, err := deleteOrphanedTracks(ctx, transaction, trackIDs)
		if err != nil {
			return 0, err
		}
		deletedTracks += deleted
	}

	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit missing source deletion: %w", err)
	}
	_ = deletedAt // retained in the method contract for timestamp-compatible callers
	return deletedTracks, nil
}

// ArchiveMissing is kept as a source-compatible adapter for older scanner
// implementations. Missing records are no longer archived; they are deleted
// from the database by DeleteMissing.
func (synchronizer *ProductionSynchronizer) ArchiveMissing(
	ctx context.Context,
	rootID string,
	seenCutoff time.Time,
	deletedAt time.Time,
) (int, error) {
	return synchronizer.DeleteMissing(ctx, rootID, seenCutoff, deletedAt)
}

func (synchronizer *ProductionSynchronizer) ProcessFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
) error {
	if file.ScanError != nil {
		recordErr := synchronizer.markSourceFailed(ctx, rootID, sourceFailurePath(file), file.ScanError, seenAt)
		if recordErr != nil {
			return errors.Join(file.ScanError, recordErr)
		}
		// The database write is only bookkeeping; callers must still observe the
		// per-file analysis failure so scans and direct callers count it as a
		// failure rather than silently treating it as processed.
		return file.ScanError
	}
	_, err := synchronizer.syncStandardFile(ctx, rootID, scanRunID, file, seenAt)
	if err != nil {
		_ = synchronizer.markSourceFailed(ctx, rootID, sourceFailurePath(file), err, seenAt)
		return err
	}
	return nil
}

func (synchronizer *ProductionSynchronizer) syncStandardFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
) (localSourceRecord, error) {
	return synchronizer.syncStandardFileWithOptions(ctx, rootID, scanRunID, file, seenAt, standardSyncOptions{})
}

type standardSyncOptions struct {
	Metadata      os.FileInfo
	Probed        *mediafile.ProbedMetadataFile
	Checksum      string
	Sidecars      []scannedLyric
	SidecarsReady bool
}

func (synchronizer *ProductionSynchronizer) syncStandardFileWithOptions(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
	options standardSyncOptions,
) (localSourceRecord, error) {
	snapshot := sourceScanSnapshotFromContext(ctx)
	var renameClaimedSourceID string
	renameCommitted := false
	defer func() {
		if snapshot != nil && renameClaimedSourceID != "" && !renameCommitted {
			snapshot.releaseRenameCandidate(renameClaimedSourceID)
		}
	}()
	metadata := options.Metadata
	if metadata == nil {
		var err error
		metadata, err = os.Stat(file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
	}
	normalizedPath := normalizePlatformPath(file.RelativePath)
	existing, found, err := synchronizer.findSource(ctx, rootID, normalizedPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	unchanged := found && existing.SizeBytes == metadata.Size() &&
		existing.ModifiedAt.UnixMilli() == metadata.ModTime().UnixMilli()
	needsArtwork, err := synchronizer.needsArtworkForTrack(ctx, existing.TrackID)
	if err != nil {
		return localSourceRecord{}, err
	}
	if unchanged && existing.Status == SourceFileReady && !needsArtwork {
		if snapshot != nil {
			snapshot.markSourceSeen(existing.ID)
		}
		externalLyrics, err := synchronizer.sourceHasExternalLyrics(ctx, existing.ID)
		if err != nil {
			return localSourceRecord{}, err
		}
		sidecars, err := readSidecarLyricsCached(snapshot, file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
		if len(sidecars) > 0 || externalLyrics {
			if err := synchronizer.syncUnchangedSidecars(ctx, existing, sidecars, seenAt); err != nil {
				return localSourceRecord{}, err
			}
		}
		existing.LastSeenAt = seenAt
		return existing, nil
	}

	checksum := options.Checksum
	if checksum == "" {
		checksum, err = fileSHA256(file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
	}
	if !found {
		candidates, err := synchronizer.findRenameCandidates(ctx, rootID, checksum, seenAt)
		if err != nil {
			return localSourceRecord{}, err
		}
		if len(candidates) == 1 && (snapshot == nil || snapshot.claimRenameCandidate(candidates[0].ID)) {
			existing, found = candidates[0], true
			renameClaimedSourceID = existing.ID
		}
	}
	if found && existing.Checksum == checksum && existing.Status == SourceFileReady && !needsArtwork {
		transaction, err := synchronizer.database.Begin(ctx)
		if err != nil {
			return localSourceRecord{}, fmt.Errorf("begin unchanged local library rename: %w", err)
		}
		defer transaction.Rollback(ctx)
		locked, err := scanLocalSource(transaction.QueryRow(ctx, `SELECT `+localSourceColumns+`
			FROM local_music_sources WHERE id=$1 FOR UPDATE`, existing.ID))
		if err != nil {
			return localSourceRecord{}, fmt.Errorf("lock unchanged local library rename: %w", err)
		}
		if locked.Checksum != checksum || locked.Status != SourceFileReady {
			return localSourceRecord{}, fmt.Errorf("local library source changed during rename detection")
		}
		pathChanging := locked.RootID != rootID || locked.NormalizedPath != normalizedPath
		if pathChanging {
			var blocked bool
			if err := transaction.QueryRow(ctx, `SELECT EXISTS(
				SELECT 1 FROM metadata_writeback_jobs
				WHERE source_id=$1 AND status IN ('PENDING','PROCESSING')
			)`, locked.ID).Scan(&blocked); err != nil {
				return localSourceRecord{}, fmt.Errorf("check Tag writeback path freeze: %w", err)
			}
			if blocked {
				return localSourceRecord{}, fmt.Errorf("Tag writeback keeps the local source path frozen")
			}
		}
		now := synchronizer.now()
		_, err = transaction.Exec(ctx, `UPDATE local_music_sources SET
			source_path=$2,normalized_source_path=$3,size_bytes=$4,modified_at=$5,
			last_seen_at=$6,updated_at=$7 WHERE id=$1`,
			locked.ID, file.RelativePath, normalizedPath, metadata.Size(), metadata.ModTime(), seenAt, now)
		if err != nil {
			return localSourceRecord{}, fmt.Errorf("rename unchanged local library file: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return localSourceRecord{}, fmt.Errorf("commit unchanged local library rename: %w", err)
		}
		renameCommitted = true
		locked.SourcePath, locked.NormalizedPath = file.RelativePath, normalizedPath
		locked.SizeBytes, locked.ModifiedAt, locked.LastSeenAt = metadata.Size(), metadata.ModTime(), seenAt
		return locked, nil
	}

	probed := options.Probed
	if probed == nil {
		value, err := synchronizer.probeFile(ctx, file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
		probed = &value
	}
	sidecars := options.Sidecars
	if !options.SidecarsReady {
		var err error
		sidecars, err = readSidecarLyricsCached(snapshot, file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
	}

	artwork, err := synchronizer.stageArtwork(ctx, file.AudioPath, probed.Metadata.HasArtwork, checksum)
	if err != nil {
		return localSourceRecord{}, err
	}
	catalogCache := newScanCatalogCache()
	source, artworkUsed, err := synchronizer.storeStandardFile(ctx, standardFileMutation{
		RootID: rootID, ScanRunID: scanRunID, File: file, Metadata: metadata,
		Raw: probed.Metadata, Probed: probed, Checksum: checksum,
		Existing: existing, ExistingFound: found,
		Lyrics:  mergeLyrics(sidecars, probed.Metadata.Lyrics),
		Artwork: artwork, CatalogCache: catalogCache,
		SeenAt: seenAt,
	})
	if err != nil {
		synchronizer.cleanupUnreferencedArtwork(ctx, artwork)
		return localSourceRecord{}, err
	}
	if artwork != nil && !artworkUsed {
		synchronizer.cleanupUnreferencedArtwork(ctx, artwork)
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		if batchCache := scanBatchCatalogFromContext(ctx); batchCache != nil {
			batchCache.merge(catalogCache)
		} else {
			// storeStandardFile commits before returning in the normal path; the
			// batch path publishes this cache only after its outer transaction
			// commits.
			snapshot.rememberCatalog(catalogCache)
		}
	}
	return source, nil
}

func (synchronizer *ProductionSynchronizer) syncUnchangedSidecars(
	ctx context.Context,
	source localSourceRecord,
	sidecars []scannedLyric,
	seenAt time.Time,
) error {
	if source.TrackID == nil {
		return nil
	}
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sidecar synchronization: %w", err)
	}
	defer transaction.Rollback(ctx)

	var overridesLyrics bool
	err = transaction.QueryRow(ctx, `
		SELECT (overrides ? 'lyrics')
		FROM track_metadata WHERE track_id = $1 FOR UPDATE`, *source.TrackID).Scan(&overridesLyrics)
	if err != nil {
		return fmt.Errorf("inspect track lyric overrides: %w", err)
	}
	if !overridesLyrics {
		if err := syncScannedLyrics(ctx, transaction, *source.TrackID, sidecars); err != nil {
			return err
		}
	}
	now := synchronizer.now()
	_, err = transaction.Exec(ctx, `
		UPDATE local_music_sources
		SET last_seen_at = $2, updated_at = $3
		WHERE id = $1`, source.ID, seenAt, now)
	if err != nil {
		return fmt.Errorf("touch local music source seen time: %w", err)
	}
	return transaction.Commit(ctx)
}
