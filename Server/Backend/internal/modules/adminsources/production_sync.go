package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/modules/adminmetadata"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

type SourceObjectStorage interface {
	UploadFile(context.Context, string, string, string, string) (int64, error)
	StatObject(context.Context, string) (sizeBytes int64, checksumSHA256 string, exists bool, err error)
}

type SourceMetadataProbe interface {
	Probe(context.Context, string) (adminmetadata.ProbedMetadataFile, error)
}

// ResourceBudget is injected by the application layer when multiple
// background workers share the same machine resource. The synchronizer keeps
// its local gates as well, while the shared budget prevents independent pools
// from adding up beyond the process-wide limit.
type ResourceBudget interface {
	Acquire(context.Context, int) error
	Release(int)
}

type FFprobeMetadataProbe struct {
	executable string
	runner     adminmetadata.ProcessRunner
}

func NewFFprobeMetadataProbe(executable string, runner adminmetadata.ProcessRunner) (*FFprobeMetadataProbe, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("local library ffprobe path is required")
	}
	if runner == nil {
		runner = adminmetadata.OSProcessRunner{}
	}
	return &FFprobeMetadataProbe{executable: executable, runner: runner}, nil
}

func (probe *FFprobeMetadataProbe) Probe(ctx context.Context, path string) (adminmetadata.ProbedMetadataFile, error) {
	return adminmetadata.ProbeMetadataFile(ctx, path, probe.executable, probe.runner)
}

type ProductionSynchronizerOptions struct {
	Database                 *pgxpool.Pool
	Storage                  SourceObjectStorage
	Probe                    SourceMetadataProbe
	FFmpegPath               string
	Runner                   adminmetadata.ProcessRunner
	Now                      func() time.Time
	ProbeWorkers             int
	StorageWorkers           int
	FFmpegWorkers            int
	FFmpegThreads            int
	ReadySourceObjectStatTTL time.Duration
	ProbeBudget              ResourceBudget
	StorageBudget            ResourceBudget
	FFmpegBudget             ResourceBudget
}

type ProductionSynchronizer struct {
	database                 syncDatabase
	storage                  SourceObjectStorage
	probe                    SourceMetadataProbe
	ffmpegPath               string
	runner                   adminmetadata.ProcessRunner
	now                      func() time.Time
	probeGate                chan struct{}
	storageGate              chan struct{}
	ffmpegGate               chan struct{}
	probeBudget              ResourceBudget
	storageBudget            ResourceBudget
	ffmpegBudget             ResourceBudget
	ffmpegThreads            int
	readySourceObjectStatTTL time.Duration
	readyObjectStatsMu       sync.Mutex
	readyObjectStats         map[string]readySourceObjectStat
}

type readySourceObjectStat struct {
	sizeBytes int64
	checksum  string
	exists    bool
	checkedAt time.Time
}

type syncDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}

// scanBatchDatabase exposes one outer transaction as a normal synchronizer
// database. Begin creates a pgx savepoint, allowing the existing per-file
// transaction code to remain isolated inside a batch.
type scanBatchDatabase struct {
	transaction pgx.Tx
}

func (database *scanBatchDatabase) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	return database.transaction.Query(ctx, query, arguments...)
}

func (database *scanBatchDatabase) QueryRow(ctx context.Context, query string, arguments ...any) pgx.Row {
	return database.transaction.QueryRow(ctx, query, arguments...)
}

func (database *scanBatchDatabase) Exec(ctx context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	return database.transaction.Exec(ctx, query, arguments...)
}

func (database *scanBatchDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	return database.transaction.Begin(ctx)
}

var _ FileSynchronizer = (*ProductionSynchronizer)(nil)
var _ ScanPipeline = (*ProductionSynchronizer)(nil)

func NewProductionSynchronizer(options ProductionSynchronizerOptions) (*ProductionSynchronizer, error) {
	if options.Database == nil {
		return nil, errors.New("local library synchronizer database is required")
	}
	if options.Storage == nil {
		return nil, errors.New("local library synchronizer object storage is required")
	}
	if options.Probe == nil {
		return nil, errors.New("local library synchronizer metadata probe is required")
	}
	if options.Runner == nil {
		options.Runner = adminmetadata.OSProcessRunner{}
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.ReadySourceObjectStatTTL < 0 {
		return nil, errors.New("local library ready source object stat TTL is invalid")
	}
	if options.ProbeWorkers < 0 || options.ProbeWorkers > 64 {
		return nil, errors.New("local library probe worker count must be between 0 and 64")
	}
	if options.StorageWorkers < 0 || options.StorageWorkers > 64 {
		return nil, errors.New("local library storage worker count must be between 0 and 64")
	}
	if options.FFmpegWorkers < 0 || options.FFmpegWorkers > 64 {
		return nil, errors.New("local library ffmpeg worker count must be between 0 and 64")
	}
	if options.FFmpegThreads < 0 || options.FFmpegThreads > 64 {
		return nil, errors.New("local library ffmpeg thread count must be between 0 and 64")
	}
	probeWorkers := options.ProbeWorkers
	if probeWorkers == 0 {
		probeWorkers = max(1, min(4, runtime.GOMAXPROCS(0)))
	}
	storageWorkers := options.StorageWorkers
	if storageWorkers == 0 {
		storageWorkers = max(1, min(probeWorkers*2, 16))
	}
	ffmpegWorkers := options.FFmpegWorkers
	if ffmpegWorkers == 0 {
		ffmpegWorkers = max(1, min(4, runtime.GOMAXPROCS(0)))
	}
	ffmpegThreads := options.FFmpegThreads
	if ffmpegThreads == 0 {
		ffmpegThreads = max(1, runtime.GOMAXPROCS(0)/ffmpegWorkers)
	}
	return &ProductionSynchronizer{
		database: options.Database, storage: options.Storage, probe: options.Probe,
		ffmpegPath: strings.TrimSpace(options.FFmpegPath), runner: options.Runner, now: options.Now,
		probeGate:                make(chan struct{}, probeWorkers),
		storageGate:              make(chan struct{}, storageWorkers),
		ffmpegGate:               make(chan struct{}, ffmpegWorkers),
		probeBudget:              options.ProbeBudget,
		storageBudget:            options.StorageBudget,
		ffmpegBudget:             options.FFmpegBudget,
		ffmpegThreads:            ffmpegThreads,
		readySourceObjectStatTTL: options.ReadySourceObjectStatTTL,
		readyObjectStats:         make(map[string]readySourceObjectStat),
	}, nil
}

type preparedStandardFile struct {
	Metadata      os.FileInfo
	Checksum      string
	Probed        *adminmetadata.ProbedMetadataFile
	Sidecars      []scannedLyric
	SidecarsReady bool
	Existing      localSourceRecord
	ExistingFound bool
	// UnchangedReady is a read-only scan hit. FlushScan will persist its
	// last_seen_at, so the commit stage only needs to revalidate the file and
	// synchronize sidecars when their presence requires it.
	UnchangedReady   bool
	NeedsSidecarSync bool
}

// PrepareFile performs the read-heavy part of standard-file synchronization.
// It deliberately does not upload or mutate the database; those operations
// run in the scanner's smaller commit pool.
func (synchronizer *ProductionSynchronizer) PrepareFile(
	ctx context.Context,
	rootID string,
	_ string,
	file DiscoveredFile,
	seenAt time.Time,
) (any, bool, error) {
	if file.ScanError != nil || file.CuePath != "" {
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
	if found {
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			snapshot.markSourceSeen(existing.ID)
		}
	}
	unchanged := found && existing.SizeBytes == metadata.Size() &&
		existing.ModifiedAt.UnixMilli() == metadata.ModTime().UnixMilli()
	if unchanged && existing.Status == SourceFileReady {
		reusable, err := synchronizer.readySourceAssetReusable(ctx, existing)
		if err != nil {
			return nil, true, err
		}
		if reusable {
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
	}
	if unchanged && existing.Status == SourceFileProcessing && existing.MediaJobID != nil {
		return &preparedStandardFile{Metadata: metadata}, true, nil
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
			reusable, err := synchronizer.readySourceAssetReusable(ctx, candidates[0])
			if err != nil {
				return nil, true, err
			}
			if reusable {
				return &preparedStandardFile{Metadata: metadata, Checksum: checksum}, true, nil
			}
		}
	}
	if found && existing.Checksum == checksum && existing.Status == SourceFileReady {
		reusable, err := synchronizer.readySourceAssetReusable(ctx, existing)
		if err != nil {
			return nil, true, err
		}
		if reusable {
			return &preparedStandardFile{Metadata: metadata, Checksum: checksum}, true, nil
		}
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

// prepareMetadataReads overlaps the independent ffprobe process and sidecar
// directory read after checksum/reuse decisions have established that both
// are needed. The probe budget still bounds the external process count.
func (synchronizer *ProductionSynchronizer) prepareMetadataReads(
	ctx context.Context,
	path string,
) (adminmetadata.ProbedMetadataFile, []scannedLyric, error) {
	readContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var group sync.WaitGroup
	var resultMu sync.Mutex
	var firstErr error
	var probed adminmetadata.ProbedMetadataFile
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
		return adminmetadata.ProbedMetadataFile{}, nil, err
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
	stable, err := synchronizer.preparedFileStillStable(file, value)
	if err != nil {
		return synchronizer.finishPreparedFileError(ctx, rootID, file, seenAt, err)
	}
	if !stable {
		// The source changed while waiting for the commit stage. Re-run the
		// normal path so the new generation is hashed and probed together.
		return synchronizer.ProcessFile(ctx, rootID, scanRunID, file, seenAt)
	}
	if value.UnchangedReady {
		if !value.NeedsSidecarSync {
			return nil
		}
		if !value.ExistingFound {
			return errors.New("local library reusable source is missing its source record")
		}
		return synchronizer.finishPreparedFileError(
			ctx, rootID, file, seenAt,
			synchronizer.syncUnchangedSidecars(ctx, value.Existing, value.Sidecars, seenAt),
		)
	}
	return synchronizer.processStablePreparedFile(ctx, rootID, scanRunID, file, seenAt, value)
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
	_ = synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.RelativePath), err, seenAt)
	return err
}

// ProcessPreparedFileBatch keeps the per-file synchronization implementation
// and failure semantics while amortizing the outer BEGIN/COMMIT round trips.
// pgx nested transactions are savepoints, so a failed file rolls back only
// its own catalog/media changes and the remaining files can continue.
func (synchronizer *ProductionSynchronizer) ProcessPreparedFileBatch(
	ctx context.Context,
	rootID string,
	scanRunID string,
	files []PreparedScanBatchFile,
	seenAt time.Time,
) []error {
	results := make([]error, len(files))
	if len(files) == 0 {
		return results
	}

	// A prepared file with probe output represents a changed/new source. Its
	// commit path still performs source and artwork uploads before the catalog
	// transaction. Run those items through the normal synchronizer first. The
	// remaining files are revalidated here, before BEGIN, so the batch
	// transaction only contains database work.
	batchFiles := make([]PreparedScanBatchFile, 0, len(files))
	batchIndexes := make([]int, 0, len(files))
	for index, file := range files {
		prepared, isStandard := file.Prepared.(*preparedStandardFile)
		if !isStandard || prepared == nil {
			results[index] = synchronizer.ProcessPreparedFile(
				ctx, rootID, scanRunID, file.File, seenAt, file.Prepared,
			)
			continue
		}
		if prepared.UnchangedReady {
			results[index] = synchronizer.ProcessPreparedFile(
				ctx, rootID, scanRunID, file.File, seenAt, file.Prepared,
			)
			continue
		}
		if prepared.Probed != nil {
			results[index] = synchronizer.ProcessPreparedFile(
				ctx, rootID, scanRunID, file.File, seenAt, file.Prepared,
			)
			continue
		}
		stable, stableErr := synchronizer.preparedFileStillStable(file.File, prepared)
		if stableErr != nil {
			results[index] = synchronizer.finishPreparedFileError(
				ctx, rootID, file.File, seenAt, stableErr,
			)
			continue
		}
		if !stable {
			results[index] = synchronizer.ProcessFile(
				ctx, rootID, scanRunID, file.File, seenAt,
			)
			continue
		}
		batchFiles = append(batchFiles, file)
		batchIndexes = append(batchIndexes, index)
	}
	if len(batchFiles) == 0 {
		return results
	}
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		failure := fmt.Errorf("begin local library scan commit batch: %w", err)
		for index, file := range batchFiles {
			resultIndex := batchIndexes[index]
			results[resultIndex] = failure
			if ctx.Err() == nil {
				_ = synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.File.RelativePath), failure, seenAt)
			}
		}
		return results
	}
	defer transaction.Rollback(ctx)

	// Avoid copying ProductionSynchronizer because it owns mutexes protecting
	// the ready-object cache. The batch view has its own small cache while the
	// scan snapshot remains shared through the context.
	batchSynchronizer := &ProductionSynchronizer{
		database:                 &scanBatchDatabase{transaction: transaction},
		storage:                  synchronizer.storage,
		probe:                    synchronizer.probe,
		ffmpegPath:               synchronizer.ffmpegPath,
		runner:                   synchronizer.runner,
		now:                      synchronizer.now,
		probeGate:                synchronizer.probeGate,
		storageGate:              synchronizer.storageGate,
		ffmpegGate:               synchronizer.ffmpegGate,
		probeBudget:              synchronizer.probeBudget,
		storageBudget:            synchronizer.storageBudget,
		ffmpegBudget:             synchronizer.ffmpegBudget,
		ffmpegThreads:            synchronizer.ffmpegThreads,
		readySourceObjectStatTTL: synchronizer.readySourceObjectStatTTL,
		readyObjectStats:         make(map[string]readySourceObjectStat),
	}
	for index, file := range batchFiles {
		resultIndex := batchIndexes[index]
		prepared, _ := file.Prepared.(*preparedStandardFile)
		results[resultIndex] = batchSynchronizer.processStablePreparedFile(
			ctx, rootID, scanRunID, file.File, seenAt, prepared,
		)
	}
	if err := transaction.Commit(ctx); err != nil {
		commitError := fmt.Errorf("commit local library scan commit batch: %w", err)
		for index, file := range batchFiles {
			resultIndex := batchIndexes[index]
			if results[resultIndex] == nil {
				results[resultIndex] = commitError
			} else {
				results[resultIndex] = errors.Join(results[resultIndex], commitError)
			}
			if ctx.Err() == nil {
				_ = synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.File.RelativePath), commitError, seenAt)
			}
		}
	}
	return results
}

func (synchronizer *ProductionSynchronizer) HandlePreparedFileFailure(
	ctx context.Context,
	rootID string,
	_ string,
	file DiscoveredFile,
	seenAt time.Time,
	failure error,
) error {
	if failure == nil || errors.Is(failure, context.Canceled) || errors.Is(failure, ErrScanCancelled) {
		return nil
	}
	return synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.RelativePath), failure, seenAt)
}

func (synchronizer *ProductionSynchronizer) probeFile(
	ctx context.Context,
	path string,
) (adminmetadata.ProbedMetadataFile, error) {
	if synchronizer.probeGate != nil {
		if err := acquireSynchronizerGate(ctx, synchronizer.probeGate); err != nil {
			return adminmetadata.ProbedMetadataFile{}, err
		}
		defer func() { <-synchronizer.probeGate }()
	}
	if synchronizer.probeBudget != nil {
		if err := synchronizer.probeBudget.Acquire(ctx, 1); err != nil {
			return adminmetadata.ProbedMetadataFile{}, err
		}
		defer synchronizer.probeBudget.Release(1)
	}
	return synchronizer.probe.Probe(ctx, path)
}

func acquireSynchronizerGate(ctx context.Context, gate chan struct{}) error {
	if gate == nil {
		return nil
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (synchronizer *ProductionSynchronizer) uploadFile(
	ctx context.Context,
	objectKey, path, contentType, checksum string,
) (int64, error) {
	if synchronizer.storageGate != nil {
		if err := acquireSynchronizerGate(ctx, synchronizer.storageGate); err != nil {
			return 0, err
		}
		defer func() { <-synchronizer.storageGate }()
	}
	if synchronizer.storageBudget != nil {
		if err := synchronizer.storageBudget.Acquire(ctx, 1); err != nil {
			return 0, err
		}
		defer synchronizer.storageBudget.Release(1)
	}
	return synchronizer.storage.UploadFile(ctx, objectKey, path, contentType, checksum)
}

func (synchronizer *ProductionSynchronizer) statObject(
	ctx context.Context,
	objectKey string,
) (int64, string, bool, error) {
	if synchronizer.storageGate != nil {
		if err := acquireSynchronizerGate(ctx, synchronizer.storageGate); err != nil {
			return 0, "", false, err
		}
		defer func() { <-synchronizer.storageGate }()
	}
	if synchronizer.storageBudget != nil {
		if err := synchronizer.storageBudget.Acquire(ctx, 1); err != nil {
			return 0, "", false, err
		}
		defer synchronizer.storageBudget.Release(1)
	}
	return synchronizer.storage.StatObject(ctx, objectKey)
}

func (synchronizer *ProductionSynchronizer) runFFmpeg(
	ctx context.Context,
	arguments []string,
	timeout time.Duration,
) (adminmetadata.ProcessResult, error) {
	if synchronizer.ffmpegGate != nil {
		if err := acquireSynchronizerGate(ctx, synchronizer.ffmpegGate); err != nil {
			return adminmetadata.ProcessResult{}, err
		}
		defer func() { <-synchronizer.ffmpegGate }()
	}
	threads := max(1, synchronizer.ffmpegThreads)
	if synchronizer.ffmpegBudget != nil {
		if err := synchronizer.ffmpegBudget.Acquire(ctx, threads); err != nil {
			return adminmetadata.ProcessResult{}, err
		}
		defer synchronizer.ffmpegBudget.Release(threads)
	}
	arguments = append([]string{"-threads", fmt.Sprint(threads)}, arguments...)
	return synchronizer.runner.Run(ctx, synchronizer.ffmpegPath, arguments, timeout)
}

func (synchronizer *ProductionSynchronizer) ProcessFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
) error {
	normalizedPath := normalizePlatformPath(file.RelativePath)
	var processErr error
	if file.ScanError != nil {
		if err := synchronizer.touchDiscoveredSource(ctx, rootID, normalizedPath, seenAt); err != nil {
			return err
		}
		processErr = file.ScanError
	} else if file.CuePath != "" {
		processErr = synchronizer.syncCueFile(ctx, rootID, scanRunID, file, seenAt)
	} else {
		_, processErr = synchronizer.syncStandardFile(ctx, rootID, scanRunID, file, seenAt, false)
	}
	if processErr == nil || errors.Is(processErr, context.Canceled) || errors.Is(processErr, ErrScanCancelled) {
		return processErr
	}
	_ = synchronizer.markSourceFailed(ctx, rootID, normalizedPath, processErr, seenAt)
	return processErr
}

func (synchronizer *ProductionSynchronizer) touchDiscoveredSource(
	ctx context.Context,
	rootID, normalizedPath string,
	seenAt time.Time,
) error {
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin discovered local library source touch: %w", err)
	}
	defer transaction.Rollback(ctx)
	var sourceID string
	var status SourceFileStatus
	var sourceUpdatedAt time.Time
	err = transaction.QueryRow(ctx, `SELECT id,status,updated_at
		FROM local_music_sources
		WHERE root_id=$1 AND normalized_source_path=$2
		FOR UPDATE`, rootID, normalizedPath).Scan(&sourceID, &status, &sourceUpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock discovered local library source: %w", err)
	}
	now := synchronizer.now()
	if status == SourceFileMissing {
		if _, err := transaction.Exec(ctx, `UPDATE tracks track SET
			status=CASE WHEN track.published_at IS NOT NULL AND track.duration_ms>0 AND EXISTS(
				SELECT 1 FROM track_variants variant
				JOIN media_assets asset ON asset.id=variant.asset_id
				WHERE variant.track_id=track.id AND variant.status='READY' AND asset.status='READY'
			) THEN 'READY'::catalog_status ELSE 'ERROR'::catalog_status END,
			version=track.version+1,updated_at=$3
			WHERE track.status='ARCHIVED' AND track.updated_at=$2
			AND EXISTS(
				SELECT 1 FROM local_music_source_tracks mapping
				WHERE mapping.source_id=$1 AND mapping.track_id=track.id
			)
			AND NOT EXISTS(
				SELECT 1 FROM audit_logs audit
				WHERE audit.action='admin.track.archive' AND audit.target_type='track'
				AND audit.target_id=track.id AND audit.result='SUCCESS'
			)`, sourceID, sourceUpdatedAt, now); err != nil {
			return fmt.Errorf("restore incorrectly archived local library tracks: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		last_seen_at=$2,updated_at=$3 WHERE id=$1`, sourceID, seenAt, now); err != nil {
		return fmt.Errorf("touch discovered local library source: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit discovered local library source touch: %w", err)
	}
	return nil
}

func (synchronizer *ProductionSynchronizer) ArchiveMissing(
	ctx context.Context,
	rootID string,
	scanStartedAt, now time.Time,
) (int, error) {
	var archived int
	err := synchronizer.database.QueryRow(ctx, `WITH stale_sources AS MATERIALIZED(
		SELECT id FROM local_music_sources WHERE root_id=$1 AND last_seen_at<$2
	), missing_sources AS(
		UPDATE local_music_sources SET status='MISSING',updated_at=$3
		WHERE id IN(SELECT id FROM stale_sources) AND status<>'MISSING' RETURNING id
	), archived_tracks AS(
		UPDATE tracks track SET status='ARCHIVED',version=track.version+1,updated_at=$3
		WHERE track.status<>'ARCHIVED' AND EXISTS(
			SELECT 1 FROM local_music_source_tracks mapping
			JOIN stale_sources missing ON missing.id=mapping.source_id WHERE mapping.track_id=track.id
		) AND NOT EXISTS(
			SELECT 1 FROM local_music_source_tracks active_mapping
			JOIN local_music_sources active_source ON active_source.id=active_mapping.source_id
			WHERE active_mapping.track_id=track.id AND active_source.status<>'MISSING'
			AND NOT EXISTS(SELECT 1 FROM stale_sources stale WHERE stale.id=active_source.id)
		) RETURNING track.id
	) SELECT count(*)::int FROM missing_sources`, rootID, scanStartedAt, now).Scan(&archived)
	if err != nil {
		return 0, fmt.Errorf("archive missing local library files: %w", err)
	}
	return archived, nil
}

type localSourceRecord struct {
	ID             string
	RootID         string
	SourcePath     string
	NormalizedPath string
	Checksum       string
	SizeBytes      int64
	ModifiedAt     time.Time
	TrackID        string
	SourceAssetID  *string
	MediaJobID     *string
	Status         SourceFileStatus
	LastSeenAt     time.Time
	UpdatedAt      time.Time
}

func (synchronizer *ProductionSynchronizer) readySourceAssetReusable(
	ctx context.Context,
	source localSourceRecord,
) (bool, error) {
	if source.SourceAssetID == nil {
		return false, nil
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		asset, exists := snapshot.assetsByID[*source.SourceAssetID]
		if !exists || !asset.ready {
			return false, nil
		}
		if asset.sizeBytes != source.SizeBytes ||
			(asset.checksum != nil && !strings.EqualFold(*asset.checksum, source.Checksum)) {
			return false, nil
		}
		storedSize, storedChecksum, objectExists, err := synchronizer.statReadySourceObject(
			ctx, snapshot, asset.objectKey, asset.sizeBytes, asset.checksum,
		)
		if err != nil {
			return false, fmt.Errorf("inspect reusable local library source asset: %w", err)
		}
		if !objectExists || storedSize != asset.sizeBytes {
			return false, nil
		}
		if asset.checksum != nil && storedChecksum != "" &&
			!strings.EqualFold(storedChecksum, *asset.checksum) {
			return false, nil
		}
		return true, nil
	}
	var objectKey string
	var sizeBytes int64
	var checksum *string
	err := synchronizer.database.QueryRow(ctx, `SELECT object_key,size_bytes,checksum_sha256
		FROM media_assets WHERE id=$1 AND kind='AUDIO_SOURCE' AND status='READY'`, *source.SourceAssetID).Scan(
		&objectKey, &sizeBytes, &checksum,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find reusable local library source asset: %w", err)
	}
	if sizeBytes != source.SizeBytes || (checksum != nil && !strings.EqualFold(*checksum, source.Checksum)) {
		return false, nil
	}
	storedSize, storedChecksum, exists, err := synchronizer.statReadySourceObject(
		ctx, nil, objectKey, sizeBytes, checksum,
	)
	if err != nil {
		return false, fmt.Errorf("inspect reusable local library source asset: %w", err)
	}
	if !exists || storedSize != sizeBytes {
		return false, nil
	}
	if checksum != nil && storedChecksum != "" && !strings.EqualFold(storedChecksum, *checksum) {
		return false, nil
	}
	return true, nil
}

type stagedArtwork struct {
	ObjectKey string
	Path      string
	SizeBytes int64
	Checksum  string
}

type scannedLyric struct {
	Content   string
	Format    string
	Language  string
	Timing    sharedlyrics.Timing
	Origin    string
	IsDefault bool
}

func (synchronizer *ProductionSynchronizer) syncStandardFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
	preserveCueMappings bool,
) (localSourceRecord, error) {
	return synchronizer.syncStandardFileWithOptions(ctx, rootID, scanRunID, file, seenAt, standardSyncOptions{
		PreserveCueMappings: preserveCueMappings,
	})
}

type standardSyncOptions struct {
	PreserveCueMappings bool
	Metadata            os.FileInfo
	Probed              *adminmetadata.ProbedMetadataFile
	Checksum            string
	Sidecars            []scannedLyric
	SidecarsReady       bool
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
	if found {
		if snapshot != nil {
			snapshot.markSourceSeen(existing.ID)
		}
	}
	unchanged := found && existing.SizeBytes == metadata.Size() &&
		existing.ModifiedAt.UnixMilli() == metadata.ModTime().UnixMilli()
	assetChecked := false
	assetReusable := false
	checkReadyAsset := func() (bool, error) {
		if assetChecked {
			return assetReusable, nil
		}
		assetChecked = true
		var checkErr error
		assetReusable, checkErr = synchronizer.readySourceAssetReusable(ctx, existing)
		return assetReusable, checkErr
	}
	if unchanged && existing.Status == SourceFileReady {
		reusable, err := checkReadyAsset()
		if err != nil {
			return localSourceRecord{}, err
		}
		if reusable {
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
	}
	if unchanged && existing.Status == SourceFileProcessing && existing.MediaJobID != nil {
		var status string
		err := synchronizer.database.QueryRow(ctx, `SELECT status::text FROM media_jobs WHERE id=$1`, *existing.MediaJobID).Scan(&status)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return localSourceRecord{}, fmt.Errorf("find local library media job: %w", err)
		}
		if status == "PENDING" || status == "PROCESSING" {
			existing.LastSeenAt = seenAt
			return existing, nil
		}
		if status == "READY" {
			reusable, checkErr := checkReadyAsset()
			if checkErr != nil {
				return localSourceRecord{}, checkErr
			}
			if reusable {
				_, err := synchronizer.database.Exec(ctx, `UPDATE local_music_sources SET
					status='READY',last_error=NULL,updated_at=$2 WHERE id=$1`, existing.ID, synchronizer.now())
				existing.Status = SourceFileReady
				existing.LastSeenAt = seenAt
				return existing, err
			}
		}
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
	if found && existing.Checksum == checksum && existing.Status == SourceFileReady {
		reusable, err := checkReadyAsset()
		if err != nil {
			return localSourceRecord{}, err
		}
		if reusable {
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
	}
	var probed adminmetadata.ProbedMetadataFile
	if options.Probed != nil {
		probed = *options.Probed
	} else {
		var err error
		probed, err = synchronizer.probeFile(ctx, file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
	}
	raw := probed.Metadata
	sidecars := options.Sidecars
	if !options.SidecarsReady {
		sidecars, err = readSidecarLyricsCached(snapshot, file.AudioPath)
		if err != nil {
			return localSourceRecord{}, err
		}
	}
	lyrics := mergeLyrics(sidecars, raw.Lyrics)
	if len(lyrics) > 0 {
		defaultLyric := lyrics[0]
		for _, lyric := range lyrics {
			if lyric.IsDefault {
				defaultLyric = lyric
				break
			}
		}
		raw.Lyrics = &adminmetadata.MetadataLyrics{
			Content: defaultLyric.Content, Format: defaultLyric.Format, Language: defaultLyric.Language, Timing: defaultLyric.Timing,
		}
	}
	trackID := uuid.NewString()
	if found {
		trackID = existing.TrackID
	}
	objectKey := fmt.Sprintf("library/sources/%s/%s%s", trackID, checksum, strings.ToLower(filepath.Ext(file.AudioPath)))
	mimeType, err := sourceMediaType(file.AudioPath)
	if err != nil {
		return localSourceRecord{}, err
	}
	var uploadedSize int64
	var artwork *stagedArtwork
	queueArtworkCleanup := func(candidate *stagedArtwork, reason string) {
		if candidate == nil {
			return
		}
		if snapshot != nil {
			snapshot.deferArtworkCleanup(*candidate, reason)
			return
		}
		_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), candidate.ObjectKey, reason)
	}
	needArtwork := !options.PreserveCueMappings && raw.HasArtwork && synchronizer.ffmpegPath != ""
	if !needArtwork {
		uploadedSize, err = synchronizer.uploadFile(ctx, objectKey, file.AudioPath, mimeType, checksum)
		if err != nil {
			return localSourceRecord{}, err
		}
	} else {
		artifactContext, artifactCancel := context.WithCancel(ctx)
		var artifactGroup sync.WaitGroup
		var artifactMu sync.Mutex
		var artifactErr error
		recordArtifactError := func(err error) {
			if err == nil {
				return
			}
			artifactMu.Lock()
			if artifactErr == nil {
				artifactErr = err
				artifactCancel()
			}
			artifactMu.Unlock()
		}
		artifactGroup.Add(2)
		go func() {
			defer artifactGroup.Done()
			size, uploadErr := synchronizer.uploadFile(
				artifactContext, objectKey, file.AudioPath, mimeType, checksum,
			)
			if uploadErr != nil {
				recordArtifactError(uploadErr)
				return
			}
			uploadedSize = size
		}()
		go func() {
			defer artifactGroup.Done()
			staged, artworkErr := synchronizer.stageArtwork(
				artifactContext, file.AudioPath, raw.HasArtwork, checksum,
			)
			if artworkErr != nil {
				recordArtifactError(artworkErr)
				return
			}
			artwork = staged
		}()
		artifactGroup.Wait()
		artifactCancel()
		artifactMu.Lock()
		artifactFailure := artifactErr
		artifactMu.Unlock()
		if artifactFailure != nil {
			_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), objectKey, "ABANDONED_LIBRARY_SOURCE")
			queueArtworkCleanup(artwork, "ABANDONED_LIBRARY_ARTWORK")
			return localSourceRecord{}, artifactFailure
		}
	}
	source, artworkUsed, err := synchronizer.storeStandardFile(ctx, standardFileMutation{
		RootID: rootID, ScanRunID: scanRunID, File: file, SeenAt: seenAt, Metadata: metadata, Existing: existing,
		ExistingFound: found, Raw: raw, Lyrics: lyrics, Probed: options.Probed, TrackID: trackID, ObjectKey: objectKey,
		MimeType: mimeType, UploadedSize: uploadedSize, Checksum: checksum,
		PreserveCueMappings: options.PreserveCueMappings, Artwork: artwork,
	})
	if err != nil {
		_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), objectKey, "ABANDONED_LIBRARY_SOURCE")
		queueArtworkCleanup(artwork, "ABANDONED_LIBRARY_ARTWORK")
		return localSourceRecord{}, err
	}
	if artwork != nil && !artworkUsed {
		queueArtworkCleanup(artwork, "UNUSED_LIBRARY_ARTWORK")
	}
	if artwork != nil && artworkUsed {
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			snapshot.rememberArtwork(checksum, *artwork)
		}
	}
	renameCommitted = true
	return source, nil
}

type standardFileMutation struct {
	RootID              string
	ScanRunID           string
	File                DiscoveredFile
	SeenAt              time.Time
	Metadata            os.FileInfo
	Probed              *adminmetadata.ProbedMetadataFile
	Existing            localSourceRecord
	ExistingFound       bool
	Raw                 adminmetadata.MetadataSnapshot
	Lyrics              []scannedLyric
	TrackID             string
	ObjectKey           string
	MimeType            string
	UploadedSize        int64
	Checksum            string
	PreserveCueMappings bool
	Artwork             *stagedArtwork
}

func (synchronizer *ProductionSynchronizer) markSourceFailed(
	ctx context.Context,
	rootID, normalizedPath string,
	failure error,
	seenAt time.Time,
) error {
	message := truncateError(failure)
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	var sourceID *string
	err = transaction.QueryRow(ctx, `UPDATE local_music_sources SET
		status='FAILED',last_error=$3,last_seen_at=$4,updated_at=now()
		WHERE root_id=$1 AND normalized_source_path=$2 RETURNING id`,
		rootID, normalizedPath, message, seenAt).Scan(&sourceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if sourceID != nil {
		if _, err := transaction.Exec(ctx, `UPDATE tracks SET
			status='ERROR',version=version+1,updated_at=now()
			WHERE id IN(SELECT track_id FROM local_music_source_tracks WHERE source_id=$1)
			AND status<>'ARCHIVED'`, *sourceID); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}
