package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/modules/adminmetadata"
	"xymusic/server/internal/platform/localmedia"
)

type SourceMetadataProbe interface {
	Probe(context.Context, string) (adminmetadata.ProbedMetadataFile, error)
}

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
	Database     *pgxpool.Pool
	Probe        SourceMetadataProbe
	Now          func() time.Time
	ProbeWorkers int
	ProbeBudget  ResourceBudget
	// LocalMedia and FFmpegPath enable extraction of embedded cover art during
	// a library scan. They are optional so lightweight synchronizer tests and
	// installations without FFmpeg can still scan audio metadata.
	LocalMedia     *localmedia.Store
	FFmpegPath     string
	ArtworkRunner  adminmetadata.ProcessRunner
	ArtworkWorkers int
}

type ProductionSynchronizer struct {
	database      syncDatabase
	probe         SourceMetadataProbe
	now           func() time.Time
	probeGate     chan struct{}
	probeBudget   ResourceBudget
	localMedia    *localmedia.Store
	ffmpegPath    string
	artworkRunner adminmetadata.ProcessRunner
	artworkGate   chan struct{}
}

type syncDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}

type scanTransactionContextKey struct{}
type scanBatchCatalogContextKey struct{}
type scanPreparedStabilityContextKey struct{}

func withScanTransaction(ctx context.Context, transaction pgx.Tx) context.Context {
	return context.WithValue(ctx, scanTransactionContextKey{}, transaction)
}

func scanTransactionFromContext(ctx context.Context) pgx.Tx {
	if ctx == nil {
		return nil
	}
	transaction, _ := ctx.Value(scanTransactionContextKey{}).(pgx.Tx)
	return transaction
}

func withScanBatchCatalog(ctx context.Context, cache *scanCatalogCache) context.Context {
	return context.WithValue(ctx, scanBatchCatalogContextKey{}, cache)
}

func scanBatchCatalogFromContext(ctx context.Context) *scanCatalogCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(scanBatchCatalogContextKey{}).(*scanCatalogCache)
	return cache
}

func withPreparedStabilityChecked(ctx context.Context) context.Context {
	return context.WithValue(ctx, scanPreparedStabilityContextKey{}, true)
}

func preparedStabilityWasChecked(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	checked, _ := ctx.Value(scanPreparedStabilityContextKey{}).(bool)
	return checked
}

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
var _ PreparedScanBatchPipeline = (*ProductionSynchronizer)(nil)
var _ ScanPreparer = (*ProductionSynchronizer)(nil)
var _ ScanFinalizer = (*ProductionSynchronizer)(nil)

func NewProductionSynchronizer(options ProductionSynchronizerOptions) (*ProductionSynchronizer, error) {
	if options.Database == nil {
		return nil, errors.New("local library synchronizer database is required")
	}
	if options.Probe == nil {
		return nil, errors.New("local library synchronizer metadata probe is required")
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.ProbeWorkers < 0 || options.ProbeWorkers > 128 {
		return nil, errors.New("local library probe worker count must be between 0 and 128")
	}
	probeWorkers := options.ProbeWorkers
	if probeWorkers == 0 {
		probeWorkers = max(8, min(64, runtime.GOMAXPROCS(0)*4))
	}
	if options.ArtworkWorkers < 0 || options.ArtworkWorkers > 64 {
		return nil, errors.New("local library artwork worker count must be between 0 and 64")
	}
	artworkWorkers := options.ArtworkWorkers
	if artworkWorkers == 0 {
		// Cover extraction is FFmpeg work, but one worker leaves most CPUs idle
		// on large libraries. Allow a bounded pool without making it unlimited.
		artworkWorkers = max(2, min(16, runtime.GOMAXPROCS(0)*2))
	}
	artworkRunner := options.ArtworkRunner
	if artworkRunner == nil {
		artworkRunner = adminmetadata.OSProcessRunner{}
	}
	return &ProductionSynchronizer{
		database:      options.Database,
		probe:         options.Probe,
		now:           options.Now,
		probeGate:     make(chan struct{}, probeWorkers),
		probeBudget:   options.ProbeBudget,
		localMedia:    options.LocalMedia,
		ffmpegPath:    strings.TrimSpace(options.FFmpegPath),
		artworkRunner: artworkRunner,
		artworkGate:   make(chan struct{}, artworkWorkers),
	}, nil
}

type preparedStandardFile struct {
	Metadata         os.FileInfo
	Checksum         string
	Probed           *adminmetadata.ProbedMetadataFile
	Sidecars         []scannedLyric
	SidecarsReady    bool
	Existing         localSourceRecord
	ExistingFound    bool
	UnchangedReady   bool
	NeedsSidecarSync bool
}

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
	_ = synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.RelativePath), err, seenAt)
	return err
}

func (synchronizer *ProductionSynchronizer) ProcessPreparedFileBatch(
	ctx context.Context,
	rootID string,
	scanRunID string,
	batch []PreparedScanBatchFile,
	seenAt time.Time,
) []error {
	result := make([]error, len(batch))
	if len(batch) == 0 {
		return result
	}

	// Only the stable, non-reusable standard-file path is safe to place in a
	// shared transaction. Reusable files may need their own sidecar transaction
	// and a file that changed after preparation must fall back to the normal
	// path. Keeping this guard cheap preserves the per-file error contract.
	preparedFiles := make([]*preparedStandardFile, len(batch))
	for index, item := range batch {
		prepared, ok := item.Prepared.(*preparedStandardFile)
		if !ok || prepared == nil || prepared.UnchangedReady {
			return synchronizer.processPreparedFilesIndividually(ctx, rootID, scanRunID, batch, seenAt)
		}
		stable, err := synchronizer.preparedFileStillStable(item.File, prepared)
		if err != nil || !stable {
			return synchronizer.processPreparedFilesIndividually(ctx, rootID, scanRunID, batch, seenAt)
		}
		preparedFiles[index] = prepared
	}

	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return synchronizer.processPreparedFilesIndividually(ctx, rootID, scanRunID, batch, seenAt)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))
	batchCatalog := newScanCatalogCache()
	batchContext := withPreparedStabilityChecked(withScanBatchCatalog(withScanTransaction(ctx, transaction), batchCatalog))

	for index, item := range batch {
		if err := ctx.Err(); err != nil {
			return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, err)
		}
		// A savepoint isolates one bad file while allowing the remaining files
		// to share the transaction and its catalog/index work.
		savepoint := fmt.Sprintf("xymusic_scan_file_%d", index)
		if _, err := transaction.Exec(batchContext, "SAVEPOINT "+savepoint); err != nil {
			batchErr := fmt.Errorf("create prepared local library scan savepoint: %w", err)
			return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
		}
		result[index] = synchronizer.ProcessPreparedFile(
			batchContext, rootID, scanRunID, item.File, seenAt, preparedFiles[index],
		)
		if result[index] != nil {
			if rollbackErr := rollbackScanSavepoint(batchContext, transaction, savepoint); rollbackErr != nil {
				batchErr := errors.Join(result[index], rollbackErr)
				return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
			}
			continue
		}
		if _, releaseErr := transaction.Exec(batchContext, "RELEASE SAVEPOINT "+savepoint); releaseErr != nil {
			result[index] = releaseErr
			if rollbackErr := rollbackScanSavepoint(batchContext, transaction, savepoint); rollbackErr != nil {
				batchErr := errors.Join(result[index], rollbackErr)
				return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
			}
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		batchErr := fmt.Errorf("commit prepared local library scan batch: %w", err)
		return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		snapshot.rememberCatalog(batchCatalog)
	}
	for index, item := range batch {
		if result[index] != nil && !errors.Is(result[index], context.Canceled) && !errors.Is(result[index], ErrScanCancelled) {
			result[index] = synchronizer.finishPreparedFileError(ctx, rootID, item.File, seenAt, result[index])
		}
	}
	return result
}

func (synchronizer *ProductionSynchronizer) abortPreparedFileBatch(
	ctx context.Context,
	rootID string,
	batch []PreparedScanBatchFile,
	seenAt time.Time,
	transaction pgx.Tx,
	result []error,
	batchErr error,
) []error {
	fillUncommittedBatchErrors(result, batchErr)
	cleanupContext := context.WithoutCancel(ctx)
	_ = transaction.Rollback(cleanupContext)
	for index, item := range batch {
		if result[index] == nil || errors.Is(result[index], context.Canceled) || errors.Is(result[index], ErrScanCancelled) {
			continue
		}
		result[index] = synchronizer.finishPreparedFileError(cleanupContext, rootID, item.File, seenAt, result[index])
	}
	return result
}

func fillUncommittedBatchErrors(result []error, err error) {
	for index := range result {
		if result[index] == nil {
			result[index] = err
		}
	}
}

func rollbackScanSavepoint(ctx context.Context, transaction pgx.Tx, savepoint string) error {
	cleanupContext := context.WithoutCancel(ctx)
	if _, err := transaction.Exec(cleanupContext, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		return err
	}
	_, err := transaction.Exec(cleanupContext, "RELEASE SAVEPOINT "+savepoint)
	return err
}

func (synchronizer *ProductionSynchronizer) processPreparedFilesIndividually(
	ctx context.Context,
	rootID string,
	scanRunID string,
	batch []PreparedScanBatchFile,
	seenAt time.Time,
) []error {
	result := make([]error, len(batch))
	for index, item := range batch {
		result[index] = synchronizer.ProcessPreparedFile(ctx, rootID, scanRunID, item.File, seenAt, item.Prepared)
	}
	return result
}

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
		return synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.RelativePath), file.ScanError, seenAt)
	}
	var err error
	if file.CuePath != "" {
		err = synchronizer.syncCueFile(ctx, rootID, scanRunID, file, seenAt)
	} else {
		_, err = synchronizer.syncStandardFile(ctx, rootID, scanRunID, file, seenAt, false)
	}
	if err != nil {
		_ = synchronizer.markSourceFailed(ctx, rootID, normalizePlatformPath(file.RelativePath), err, seenAt)
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

	var artwork *stagedArtwork
	if !options.PreserveCueMappings {
		artwork, err = synchronizer.stageArtwork(ctx, file.AudioPath, probed.Metadata.HasArtwork, checksum)
		if err != nil {
			return localSourceRecord{}, err
		}
	}
	catalogCache := newScanCatalogCache()
	source, artworkUsed, err := synchronizer.storeStandardFile(ctx, standardFileMutation{
		RootID: rootID, ScanRunID: scanRunID, File: file, Metadata: metadata,
		Raw: probed.Metadata, Probed: probed, Checksum: checksum,
		Existing: existing, ExistingFound: found,
		PreserveCueMappings: options.PreserveCueMappings,
		Lyrics:              mergeLyrics(sidecars, probed.Metadata.Lyrics),
		Artwork:             artwork, CatalogCache: catalogCache,
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

func (synchronizer *ProductionSynchronizer) probeFile(
	ctx context.Context,
	path string,
) (adminmetadata.ProbedMetadataFile, error) {
	if synchronizer.probeBudget != nil {
		if err := synchronizer.probeBudget.Acquire(ctx, 1); err != nil {
			return adminmetadata.ProbedMetadataFile{}, err
		}
		defer synchronizer.probeBudget.Release(1)
	}
	if synchronizer.probeGate != nil {
		select {
		case <-ctx.Done():
			return adminmetadata.ProbedMetadataFile{}, ctx.Err()
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
