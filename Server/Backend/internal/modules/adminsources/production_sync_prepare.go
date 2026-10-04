package adminsources

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"xymusic/server/internal/platform/mediafile"
)

func (synchronizer *ProductionSynchronizer) PrepareFile(
	ctx context.Context,
	rootID string,
	_ string,
	file DiscoveredFile,
	seenAt time.Time,
) (any, bool, error) {
	if file.ScanError != nil {
		return nil, false, nil
	}
	metadata := file.FileInfo
	var err error
	if metadata == nil {
		metadata, err = os.Stat(file.AudioPath)
		if err != nil {
			return nil, true, err
		}
	}
	normalizedPath := normalizePlatformPath(file.RelativePath)
	existing, found, err := synchronizer.findSource(ctx, rootID, normalizedPath)
	if err != nil {
		return nil, true, err
	}
	unchanged := found && existing.SizeBytes == metadata.Size() &&
		existing.ModifiedAt.UnixMilli() == metadata.ModTime().UnixMilli()
	needsArtwork, err := synchronizer.needsArtworkForTrack(ctx, existing.TrackID)
	if err != nil {
		return nil, true, err
	}
	if unchanged && existing.Status == SourceFileReady && !needsArtwork {
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			snapshot.markSourceSeen(existing.ID)
		}
		sidecars, err := readSidecarLyricsCached(sourceScanSnapshotFromContext(ctx), file.AudioPath)
		if err != nil {
			return nil, true, err
		}
		externalLyrics, err := synchronizer.sourceHasExternalLyrics(ctx, existing.ID)
		if err != nil {
			return nil, true, err
		}
		return &preparedStandardFile{
			Metadata: metadata, Sidecars: sidecars, SidecarsReady: true,
			Existing: existing, ExistingFound: true, UnchangedReady: true,
			NeedsSidecarSync: len(sidecars) > 0 || externalLyrics,
		}, true, nil
	}
	checksum, err := fileSHA256(file.AudioPath)
	if err != nil {
		return nil, true, err
	}
	if !found {
		candidates, err := synchronizer.findRenameCandidates(ctx, rootID, checksum, seenAt)
		if err != nil {
			return nil, true, err
		}
		if len(candidates) == 1 && candidates[0].Checksum == checksum && candidates[0].Status == SourceFileReady {
			return &preparedStandardFile{Metadata: metadata, Checksum: checksum}, true, nil
		}
	}
	if found && existing.Checksum == checksum && existing.Status == SourceFileReady && !needsArtwork {
		return &preparedStandardFile{Metadata: metadata, Checksum: checksum}, true, nil
	}
	probed, sidecars, err := synchronizer.prepareMetadataReads(ctx, file.AudioPath)
	if err != nil {
		return nil, true, err
	}
	return &preparedStandardFile{
		Metadata: metadata, Checksum: checksum, Probed: &probed,
		Sidecars: sidecars, SidecarsReady: true,
	}, true, nil
}

func (synchronizer *ProductionSynchronizer) prepareMetadataReads(
	ctx context.Context,
	path string,
) (mediafile.ProbedMetadataFile, []scannedLyric, error) {
	readContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var group sync.WaitGroup
	var resultMu sync.Mutex
	var firstErr error
	var probed mediafile.ProbedMetadataFile
	var sidecars []scannedLyric
	recordError := func(err error) {
		if err == nil {
			return
		}
		resultMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		resultMu.Unlock()
	}
	group.Add(2)
	go func() {
		defer group.Done()
		value, err := synchronizer.probeFile(readContext, path)
		if err != nil {
			recordError(err)
			return
		}
		resultMu.Lock()
		probed = value
		resultMu.Unlock()
	}()
	go func() {
		defer group.Done()
		value, err := readSidecarLyricsCached(sourceScanSnapshotFromContext(readContext), path)
		if err != nil {
			recordError(err)
			return
		}
		resultMu.Lock()
		sidecars = value
		resultMu.Unlock()
	}()
	group.Wait()
	resultMu.Lock()
	err := firstErr
	resultMu.Unlock()
	if err != nil {
		return mediafile.ProbedMetadataFile{}, nil, err
	}
	return probed, sidecars, nil
}

func (synchronizer *ProductionSynchronizer) ProcessPreparedFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
	prepared any,
) error {
	value, ok := prepared.(*preparedStandardFile)
	if !ok || value == nil {
		return errors.New("local library prepared file has an invalid type")
	}
	stable := true
	var err error
	if !preparedStabilityWasChecked(ctx) {
		stable, err = synchronizer.preparedFileStillStable(file, value)
		if err != nil {
			if scanTransactionFromContext(ctx) != nil {
				return err
			}
			return synchronizer.finishPreparedFileError(ctx, rootID, file, seenAt, err)
		}
	}
	if !stable {
		return synchronizer.ProcessFile(ctx, rootID, scanRunID, file, seenAt)
	}
	if value.UnchangedReady {
		if !value.NeedsSidecarSync {
			return nil
		}
		if !value.ExistingFound {
			return errors.New("local library reusable source is missing its source record")
		}
		sidecarErr := synchronizer.syncUnchangedSidecars(ctx, value.Existing, value.Sidecars, seenAt)
		if scanTransactionFromContext(ctx) != nil {
			return sidecarErr
		}
		return synchronizer.finishPreparedFileError(ctx, rootID, file, seenAt, sidecarErr)
	}
	if scanTransactionFromContext(ctx) != nil {
		_, err := synchronizer.syncStandardFileWithOptions(ctx, rootID, scanRunID, file, seenAt, standardSyncOptions{
			Metadata: value.Metadata, Probed: value.Probed, Checksum: value.Checksum,
			Sidecars: value.Sidecars, SidecarsReady: value.SidecarsReady,
		})
		return err
	}
	return synchronizer.processStablePreparedFile(ctx, rootID, scanRunID, file, seenAt, value)
}

func (synchronizer *ProductionSynchronizer) HandlePreparedFileFailure(
	ctx context.Context,
	rootID string,
	_ string,
	file DiscoveredFile,
	seenAt time.Time,
	failure error,
) error {
	return synchronizer.finishPreparedFileError(ctx, rootID, file, seenAt, failure)
}

func (synchronizer *ProductionSynchronizer) preparedFileStillStable(
	file DiscoveredFile,
	prepared *preparedStandardFile,
) (bool, error) {
	if prepared == nil || prepared.Metadata == nil {
		return true, nil
	}
	current, err := os.Stat(file.AudioPath)
	if err != nil {
		return false, err
	}
	return current.Size() == prepared.Metadata.Size() &&
		current.ModTime().UnixMilli() == prepared.Metadata.ModTime().UnixMilli(), nil
}

func (synchronizer *ProductionSynchronizer) processStablePreparedFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
	prepared *preparedStandardFile,
) error {
	_, err := synchronizer.syncStandardFileWithOptions(ctx, rootID, scanRunID, file, seenAt, standardSyncOptions{
		Metadata: prepared.Metadata, Probed: prepared.Probed, Checksum: prepared.Checksum,
		Sidecars: prepared.Sidecars, SidecarsReady: prepared.SidecarsReady,
	})
	return synchronizer.finishPreparedFileError(ctx, rootID, file, seenAt, err)
}

func (synchronizer *ProductionSynchronizer) finishPreparedFileError(
	ctx context.Context,
	rootID string,
	file DiscoveredFile,
	seenAt time.Time,
	err error,
) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, ErrScanCancelled) {
		return err
	}
	_ = synchronizer.markSourceFailed(ctx, rootID, sourceFailurePath(file), err, seenAt)
	return err
}
