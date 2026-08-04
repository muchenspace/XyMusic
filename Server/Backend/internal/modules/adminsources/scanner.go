package adminsources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/unicode/norm"
)

var supportedAudioExtensions = map[string]struct{}{
	".aac": {}, ".aif": {}, ".aiff": {}, ".ape": {}, ".caf": {}, ".flac": {},
	".m4a": {}, ".mka": {}, ".mp3": {}, ".mp4": {}, ".ogg": {}, ".opus": {},
	".wav": {}, ".webm": {}, ".wma": {},
}

const (
	scanControlFileInterval = 256
	// A short fill window lets fast preparation workers form useful database
	// batches without adding noticeable latency to a small scan.
	scanCommitBatchWait = 2 * time.Millisecond
)

type DiscoveredFile struct {
	AudioPath    string
	RelativePath string
	CuePath      string
	FileInfo     os.FileInfo
	ScanError    error
}

type FileSynchronizer interface {
	ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error
	ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error)
}

// ScanPipeline separates read-heavy preparation from the bounded database and
// object-storage commit stage. Implementations may return handled=false for
// file types that are cheaper or safer to process through ProcessFile.
type ScanPipeline interface {
	PrepareFile(context.Context, string, string, DiscoveredFile, time.Time) (prepared any, handled bool, err error)
	ProcessPreparedFile(context.Context, string, string, DiscoveredFile, time.Time, any) error
	HandlePreparedFileFailure(context.Context, string, string, DiscoveredFile, time.Time, error) error
}

// PreparedScanBatchFile is a successfully prepared file that can be committed
// by a batch-aware pipeline. The batch processor must return one error per
// input item, preserving order.
type PreparedScanBatchFile struct {
	File     DiscoveredFile
	Prepared any
}

// PreparedScanBatchPipeline is an optional optimization for synchronizers
// whose database commit can use one transaction for several independent
// prepared files. Implementations must isolate item failures themselves.
type PreparedScanBatchPipeline interface {
	ProcessPreparedFileBatch(context.Context, string, string, []PreparedScanBatchFile, time.Time) []error
}

// ScanPreparer lets a synchronizer load immutable, scan-scoped state before
// the file workers start. The returned context is passed to every ProcessFile
// call and the cleanup function runs when the scan exits.
type ScanPreparer interface {
	PrepareScan(context.Context, string, string) (context.Context, func(), error)
}

// ScanFinalizer lets a synchronizer persist scan-scoped bookkeeping after all
// file workers finish and before missing sources are archived.
type ScanFinalizer interface {
	FlushScan(context.Context, string, time.Time) error
}

type FilesystemScanner struct {
	synchronizer    FileSynchronizer
	workers         int
	commitWorkers   int
	commitBatchSize int
	now             func() time.Time
}

type FilesystemScannerOptions struct {
	Synchronizer    FileSynchronizer
	Workers         int
	CommitWorkers   int
	CommitBatchSize int
}

func NewFilesystemScanner(synchronizer FileSynchronizer) (*FilesystemScanner, error) {
	return NewFilesystemScannerWithOptions(FilesystemScannerOptions{Synchronizer: synchronizer})
}

func NewFilesystemScannerWithOptions(options FilesystemScannerOptions) (*FilesystemScanner, error) {
	synchronizer := options.Synchronizer
	if synchronizer == nil {
		return nil, errors.New("local library file synchronizer is required")
	}
	workers := options.Workers
	if workers <= 0 {
		workers = max(4, min(32, runtime.GOMAXPROCS(0)*2))
	}
	if workers > 64 {
		return nil, errors.New("local library scanner worker count must not exceed 64")
	}
	commitWorkers := options.CommitWorkers
	if commitWorkers <= 0 {
		commitWorkers = max(1, min(4, runtime.GOMAXPROCS(0)/2))
	}
	if commitWorkers > 64 {
		return nil, errors.New("local library scanner commit worker count must not exceed 64")
	}
	commitBatchSize := options.CommitBatchSize
	if commitBatchSize <= 0 {
		commitBatchSize = max(1, min(8, commitWorkers*2))
	}
	if commitBatchSize > 64 {
		return nil, errors.New("local library scanner commit batch size must not exceed 64")
	}
	return &FilesystemScanner{
		synchronizer: synchronizer, workers: workers, commitWorkers: commitWorkers,
		commitBatchSize: commitBatchSize,
		now:             func() time.Time { return time.Now().UTC() },
	}, nil
}

type libraryDiscoveryResult struct {
	count int
	err   error
}

func startLibraryFileDiscovery(
	ctx context.Context,
	root string,
	include, exclude []*regexp.Regexp,
) (<-chan DiscoveredFile, <-chan libraryDiscoveryResult) {
	files := make(chan DiscoveredFile, 1)
	done := make(chan libraryDiscoveryResult, 1)
	go func() {
		count, err := discoverLibraryFilesStream(ctx, root, include, exclude, func(file DiscoveredFile) error {
			select {
			case files <- file:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(files)
		done <- libraryDiscoveryResult{count: count, err: err}
	}()
	return files, done
}

func (scanner *FilesystemScanner) Scan(ctx context.Context, input ScanInput) (ScanResult, error) {
	metadata, err := os.Stat(input.Directory)
	if err != nil || !metadata.IsDir() {
		if err == nil {
			err = errors.New("configured path is not a directory")
		}
		return ScanResult{}, err
	}
	include, err := compilePatterns(input.IncludePatterns)
	if err != nil {
		return ScanResult{}, err
	}
	exclude, err := compilePatterns(input.ExcludePatterns)
	if err != nil {
		return ScanResult{}, err
	}
	startedAt := scanner.now()
	progress := ScanProgress{}
	if input.OnProgress != nil {
		if err := input.OnProgress(ctx, progress); err != nil {
			return ScanResult{}, err
		}
	}
	scanContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var cancellationMu sync.Mutex
	checkCancelled := func(callbackContext context.Context) (bool, error) {
		cancellationMu.Lock()
		defer cancellationMu.Unlock()
		return scanCancelled(callbackContext, input.IsCancelled)
	}
	if cancelled, err := checkCancelled(scanContext); err != nil {
		return ScanResult{}, err
	} else if cancelled {
		return ScanResult{}, ErrScanCancelled
	}

	discovered, discoveryDone := startLibraryFileDiscovery(
		scanContext, input.Directory, include, exclude,
	)
	firstFile, hasFile := <-discovered
	if !hasFile {
		result := <-discoveryDone
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				return ScanResult{}, ErrScanCancelled
			}
			return ScanResult{}, result.err
		}
		if result.count != 0 {
			return ScanResult{}, errors.New("library discovery reported files without emitting them")
		}
		if cancelled, err := checkCancelled(ctx); err != nil {
			return ScanResult{}, err
		} else if cancelled {
			return ScanResult{}, ErrScanCancelled
		}
		return scanner.finalizeScan(ctx, input, startedAt, progress, nil)
	}

	if cancelled, err := checkCancelled(scanContext); err != nil {
		cancel()
		<-discoveryDone
		return ScanResult{}, err
	} else if cancelled {
		cancel()
		<-discoveryDone
		return ScanResult{}, ErrScanCancelled
	}
	if preparer, ok := scanner.synchronizer.(ScanPreparer); ok {
		preparedContext, cleanup, err := preparer.PrepareScan(scanContext, input.RootID, input.ScanRunID)
		if err != nil {
			cancel()
			<-discoveryDone
			return ScanResult{}, err
		}
		if preparedContext != nil {
			scanContext = preparedContext
		}
		if cleanup != nil {
			defer cleanup()
		}
		if cancelled, err := checkCancelled(scanContext); err != nil {
			cancel()
			<-discoveryDone
			return ScanResult{}, err
		} else if cancelled {
			cancel()
			<-discoveryDone
			return ScanResult{}, ErrScanCancelled
		}
	}
	scanSnapshot := sourceScanSnapshotFromContext(scanContext)
	if pipeline, ok := scanner.synchronizer.(ScanPipeline); ok {
		progress, interrupted, err := scanner.runPipelineStream(
			scanContext, cancel, ctx, input, firstFile, discovered, discoveryDone,
			startedAt, checkCancelled, pipeline,
		)
		if err != nil {
			return ScanResult{}, err
		}
		if interrupted || ctx.Err() != nil {
			return ScanResult{}, ErrScanCancelled
		}
		return scanner.finalizeScan(ctx, input, startedAt, progress, scanSnapshot)
	}
	progress, interrupted, err := scanner.runFileStream(
		scanContext, cancel, ctx, input, firstFile, discovered, discoveryDone,
		startedAt, checkCancelled,
	)
	if err != nil {
		return ScanResult{}, err
	}
	if interrupted || ctx.Err() != nil {
		return ScanResult{}, ErrScanCancelled
	}
	return scanner.finalizeScan(ctx, input, startedAt, progress, scanSnapshot)
}

func (scanner *FilesystemScanner) runFileStream(
	scanContext context.Context,
	cancel context.CancelFunc,
	callbackContext context.Context,
	input ScanInput,
	firstFile DiscoveredFile,
	discovered <-chan DiscoveredFile,
	discoveryDone <-chan libraryDiscoveryResult,
	startedAt time.Time,
	checkCancelled func(context.Context) (bool, error),
) (ScanProgress, bool, error) {
	jobs := make(chan DiscoveredFile)
	var discoveredCount atomic.Int64
	var processedFiles atomic.Int64
	var failedFiles atomic.Int64
	var scanInterrupted atomic.Bool
	fatalErrors := make(chan error, 1)
	recordFatal := func(err error) {
		if err == nil {
			return
		}
		select {
		case fatalErrors <- err:
		default:
		}
		cancel()
	}
	var processResults chan error
	if input.OnProgress != nil {
		processResults = make(chan error, max(1, scanner.workers*2))
	}
	reportProcessResult := func(processErr error) {
		if processResults == nil {
			return
		}
		select {
		case processResults <- processErr:
		case <-scanContext.Done():
		}
	}
	var progressDone chan struct{}
	if input.OnProgress != nil {
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			for processErr := range processResults {
				if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
					continue
				}
				progress := ScanProgress{
					DiscoveredFiles: int(discoveredCount.Load()),
					ProcessedFiles:  int(processedFiles.Load()),
					FailedFiles:     int(failedFiles.Load()),
				}
				if err := input.OnProgress(callbackContext, progress); err != nil {
					recordFatal(err)
					return
				}
			}
		}()
	}
	var controlGroup sync.WaitGroup
	if input.IsCancelled != nil {
		controlGroup.Add(1)
		go func() {
			defer controlGroup.Done()
			ticker := time.NewTicker(scanControlPoll)
			defer ticker.Stop()
			for {
				select {
				case <-scanContext.Done():
					return
				case <-ticker.C:
					cancelled, err := checkCancelled(scanContext)
					if err != nil {
						recordFatal(err)
						return
					}
					if cancelled {
						scanInterrupted.Store(true)
						cancel()
						return
					}
				}
			}
		}()
	}
	var group sync.WaitGroup
	for workerIndex := 0; workerIndex < scanner.workers; workerIndex++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for file := range jobs {
				processErr := scanner.synchronizer.ProcessFile(
					scanContext, input.RootID, input.ScanRunID, file, startedAt,
				)
				if file.ScanError != nil {
					processErr = errors.Join(file.ScanError, processErr)
				}
				if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
					scanInterrupted.Store(true)
					cancel()
				} else {
					processedFiles.Add(1)
					if processErr != nil {
						failedFiles.Add(1)
					}
				}
				reportProcessResult(processErr)
			}
		}()
	}
	queueAborted := false
	sendFile := func(file DiscoveredFile) bool {
		discoveredCount.Add(1)
		select {
		case jobs <- file:
			return true
		case <-scanContext.Done():
			return false
		}
	}
	if !sendFile(firstFile) {
		queueAborted = true
	}
	if !queueAborted {
		for file := range discovered {
			if !sendFile(file) {
				queueAborted = true
				break
			}
		}
	}
	close(jobs)
	group.Wait()
	if processResults != nil {
		close(processResults)
		<-progressDone
	}
	cancel()
	controlGroup.Wait()
	discovery := <-discoveryDone
	select {
	case err := <-fatalErrors:
		return ScanProgress{}, scanInterrupted.Load(), err
	default:
	}
	if discovery.err != nil && !errors.Is(discovery.err, context.Canceled) &&
		!errors.Is(discovery.err, context.DeadlineExceeded) {
		return ScanProgress{}, scanInterrupted.Load(), discovery.err
	}
	if queueAborted || scanInterrupted.Load() || callbackContext.Err() != nil {
		return ScanProgress{}, true, nil
	}
	return ScanProgress{
		DiscoveredFiles: int(discovery.count),
		ProcessedFiles:  int(processedFiles.Load()),
		FailedFiles:     int(failedFiles.Load()),
	}, false, nil
}

type streamPreparedScanFileResult struct {
	file     DiscoveredFile
	prepared any
	handled  bool
	err      error
}

func (scanner *FilesystemScanner) runPipelineStream(
	scanContext context.Context,
	cancel context.CancelFunc,
	callbackContext context.Context,
	input ScanInput,
	firstFile DiscoveredFile,
	discovered <-chan DiscoveredFile,
	discoveryDone <-chan libraryDiscoveryResult,
	startedAt time.Time,
	checkCancelled func(context.Context) (bool, error),
	pipeline ScanPipeline,
) (ScanProgress, bool, error) {
	jobs := make(chan DiscoveredFile)
	prepared := make(chan streamPreparedScanFileResult, max(1, scanner.commitWorkers*2))
	var discoveredCount atomic.Int64
	var processedFiles atomic.Int64
	var failedFiles atomic.Int64
	var scanInterrupted atomic.Bool
	fatalErrors := make(chan error, 1)
	recordFatal := func(err error) {
		if err == nil {
			return
		}
		select {
		case fatalErrors <- err:
		default:
		}
		cancel()
	}
	var processResults chan error
	if input.OnProgress != nil {
		processResults = make(chan error, max(1, scanner.commitWorkers*2))
	}
	reportProcessResult := func(processErr error) {
		if processResults == nil {
			return
		}
		select {
		case processResults <- processErr:
		case <-scanContext.Done():
		}
	}
	var progressDone chan struct{}
	if input.OnProgress != nil {
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			for processErr := range processResults {
				if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
					continue
				}
				progress := ScanProgress{
					DiscoveredFiles: int(discoveredCount.Load()),
					ProcessedFiles:  int(processedFiles.Load()),
					FailedFiles:     int(failedFiles.Load()),
				}
				if err := input.OnProgress(callbackContext, progress); err != nil {
					recordFatal(err)
					return
				}
			}
		}()
	}
	var controlGroup sync.WaitGroup
	if input.IsCancelled != nil {
		controlGroup.Add(1)
		go func() {
			defer controlGroup.Done()
			ticker := time.NewTicker(scanControlPoll)
			defer ticker.Stop()
			for {
				select {
				case <-scanContext.Done():
					return
				case <-ticker.C:
					cancelled, err := checkCancelled(scanContext)
					if err != nil {
						recordFatal(err)
						return
					}
					if cancelled {
						scanInterrupted.Store(true)
						cancel()
						return
					}
				}
			}
		}()
	}
	var prepareGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < scanner.workers; workerIndex++ {
		prepareGroup.Add(1)
		go func() {
			defer prepareGroup.Done()
			for file := range jobs {
				preparedFile, handled, err := pipeline.PrepareFile(
					scanContext, input.RootID, input.ScanRunID, file, startedAt,
				)
				result := streamPreparedScanFileResult{
					file: file, prepared: preparedFile, handled: handled, err: err,
				}
				select {
				case prepared <- result:
				case <-scanContext.Done():
					return
				}
			}
		}()
	}
	batchPipeline, supportsBatch := scanner.synchronizer.(PreparedScanBatchPipeline)
	var commitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < scanner.commitWorkers; workerIndex++ {
		commitGroup.Add(1)
		go func() {
			defer commitGroup.Done()
			processOne := func(result streamPreparedScanFileResult) {
				file := result.file
				processErr := result.err
				if processErr != nil {
					if scanContext.Err() == nil && !errors.Is(processErr, context.Canceled) && !errors.Is(processErr, ErrScanCancelled) {
						if failureErr := pipeline.HandlePreparedFileFailure(
							scanContext, input.RootID, input.ScanRunID, file, startedAt, processErr,
						); failureErr != nil {
							processErr = errors.Join(processErr, failureErr)
						}
					}
				} else if result.handled {
					processErr = pipeline.ProcessPreparedFile(
						scanContext, input.RootID, input.ScanRunID, file, startedAt, result.prepared,
					)
				} else {
					processErr = scanner.synchronizer.ProcessFile(
						scanContext, input.RootID, input.ScanRunID, file, startedAt,
					)
				}
				if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
					scanInterrupted.Store(true)
					cancel()
				} else {
					processedFiles.Add(1)
					if processErr != nil {
						failedFiles.Add(1)
					}
				}
				reportProcessResult(processErr)
			}
			processBatch := func(results []streamPreparedScanFileResult) {
				batch := make([]PreparedScanBatchFile, len(results))
				for index, result := range results {
					batch[index] = PreparedScanBatchFile{File: result.file, Prepared: result.prepared}
				}
				batchErrors := batchPipeline.ProcessPreparedFileBatch(
					scanContext, input.RootID, input.ScanRunID, batch, startedAt,
				)
				if len(batchErrors) != len(results) {
					batchError := fmt.Errorf("prepared scan batch returned %d errors for %d files", len(batchErrors), len(results))
					batchErrors = make([]error, len(results))
					for index := range batchErrors {
						batchErrors[index] = batchError
					}
				}
				for index := range results {
					processErr := batchErrors[index]
					if errors.Is(processErr, ErrScanCancelled) || errors.Is(processErr, context.Canceled) {
						scanInterrupted.Store(true)
						cancel()
					} else {
						processedFiles.Add(1)
						if processErr != nil {
							failedFiles.Add(1)
						}
					}
					reportProcessResult(processErr)
				}
			}
		commitLoop:
			for {
				result, ok := <-prepared
				if !ok {
					return
				}
				if !supportsBatch || result.err != nil || !result.handled {
					processOne(result)
					continue
				}
				batch := []streamPreparedScanFileResult{result}
				closed := false
				if len(batch) < scanner.commitBatchSize {
					timer := time.NewTimer(scanCommitBatchWait)
				fillBatch:
					for len(batch) < scanner.commitBatchSize {
						select {
						case next, ok := <-prepared:
							if !ok {
								closed = true
								break fillBatch
							}
							if next.err != nil || !next.handled {
								_ = timer.Stop()
								processBatch(batch)
								processOne(next)
								if closed {
									break commitLoop
								}
								continue commitLoop
							}
							batch = append(batch, next)
						case <-timer.C:
							break fillBatch
						}
					}
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				}
				processBatch(batch)
				if closed {
					return
				}
			}
		}()
	}
	queueAborted := false
	sendFile := func(file DiscoveredFile) bool {
		discoveredCount.Add(1)
		select {
		case jobs <- file:
			return true
		case <-scanContext.Done():
			return false
		}
	}
	if !sendFile(firstFile) {
		queueAborted = true
	}
	if !queueAborted {
		for file := range discovered {
			if !sendFile(file) {
				queueAborted = true
				break
			}
		}
	}
	close(jobs)
	prepareGroup.Wait()
	close(prepared)
	commitGroup.Wait()
	if processResults != nil {
		close(processResults)
		<-progressDone
	}
	cancel()
	controlGroup.Wait()
	discovery := <-discoveryDone
	select {
	case err := <-fatalErrors:
		return ScanProgress{}, scanInterrupted.Load(), err
	default:
	}
	if discovery.err != nil && !errors.Is(discovery.err, context.Canceled) &&
		!errors.Is(discovery.err, context.DeadlineExceeded) {
		return ScanProgress{}, scanInterrupted.Load(), discovery.err
	}
	if queueAborted || scanInterrupted.Load() || callbackContext.Err() != nil {
		return ScanProgress{}, true, nil
	}
	return ScanProgress{
		DiscoveredFiles: int(discovery.count),
		ProcessedFiles:  int(processedFiles.Load()),
		FailedFiles:     int(failedFiles.Load()),
	}, false, nil
}

func (scanner *FilesystemScanner) finalizeScan(
	ctx context.Context,
	input ScanInput,
	startedAt time.Time,
	progress ScanProgress,
	scanSnapshot *sourceScanSnapshot,
) (ScanResult, error) {
	if cancelled, err := scanCancelled(ctx, input.IsCancelled); err != nil {
		return ScanResult{}, err
	} else if cancelled {
		return ScanResult{}, ErrScanCancelled
	}
	if finalizer, ok := scanner.synchronizer.(ScanFinalizer); ok {
		finalizerContext := ctx
		if scanSnapshot != nil {
			finalizerContext = context.WithValue(ctx, sourceScanSnapshotContextKey{}, scanSnapshot)
		}
		if err := finalizer.FlushScan(finalizerContext, input.RootID, startedAt); err != nil {
			return ScanResult{}, err
		}
	}
	archived, err := scanner.synchronizer.ArchiveMissing(ctx, input.RootID, startedAt, scanner.now())
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{
		DiscoveredFiles: progress.DiscoveredFiles, ProcessedFiles: progress.ProcessedFiles,
		FailedFiles: progress.FailedFiles, ArchivedFiles: archived,
	}, nil
}

// discoverLibraryFiles is retained as a materializing helper for callers and
// tests that need a deterministic snapshot. The scanner itself uses the
// streaming form below so a large audio directory is never duplicated into a
// complete in-memory work queue.
func discoverLibraryFiles(root string, include, exclude []*regexp.Regexp) ([]DiscoveredFile, error) {
	files := make([]DiscoveredFile, 0)
	_, err := discoverLibraryFilesStream(context.Background(), root, include, exclude, func(file DiscoveredFile) error {
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].RelativePath < files[j].RelativePath })
	return files, nil
}

// discoverLibraryFilesStream parses CUE ownership in a lightweight first
// pass, then emits audio files during a second directory walk. Only CUE
// ownership and error records are retained; ordinary audio paths and file
// metadata flow directly to the bounded scanner queue.
func discoverLibraryFilesStream(
	ctx context.Context,
	root string,
	include, exclude []*regexp.Regexp,
	emit func(DiscoveredFile) error,
) (int, error) {
	if emit == nil {
		return 0, errors.New("library discovery emit function is required")
	}
	ownedByTarget := make(map[string]DiscoveredFile)
	errorsByCue := make(map[string][]DiscoveredFile)
	cueError := func(cuePath string, err error) {
		key := normalizePlatformPath(cuePath)
		errorsByCue[key] = append(errorsByCue[key], DiscoveredFile{
			AudioPath: cuePath, RelativePath: relativeLibraryPath(root, cuePath),
			CuePath: cuePath, ScanError: err,
		})
	}
	walk := func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".cue" {
			return nil
		}
		cuePath := path
		references, parseErr := cueReferences(cuePath)
		if parseErr != nil {
			cueError(cuePath, parseErr)
			return nil
		}
		for _, reference := range references {
			target, resolveErr := resolveFileWithinRoot(root, filepath.Join(filepath.Dir(cuePath), reference))
			if resolveErr == nil {
				if _, supported := supportedAudioExtensions[strings.ToLower(filepath.Ext(target))]; !supported {
					resolveErr = errors.New("CUE referenced an unsupported audio container")
				}
			}
			if resolveErr != nil {
				cueError(cuePath, resolveErr)
				continue
			}
			relative := normalizedRelativeLibraryPath(root, target)
			if !matchesPatterns(relative, include, exclude) {
				continue
			}
			normalizedTarget := normalizePlatformPath(target)
			if previous, exists := ownedByTarget[normalizedTarget]; exists && previous.CuePath != cuePath {
				cueError(cuePath, errors.New("multiple CUE files reference the same audio source"))
				continue
			}
			ownedByTarget[normalizedTarget] = DiscoveredFile{
				AudioPath: target, RelativePath: relativeLibraryPath(root, target), CuePath: cuePath,
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		return 0, err
	}

	count := 0
	emitFile := func(file DiscoveredFile) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(file); err != nil {
			return err
		}
		count++
		return nil
	}
	seenCueErrors := make(map[string]struct{}, len(errorsByCue))
	secondWalk := func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".cue" {
			key := normalizePlatformPath(path)
			if records, exists := errorsByCue[key]; exists {
				for _, record := range records {
					if err := emitFile(record); err != nil {
						return err
					}
				}
				seenCueErrors[key] = struct{}{}
			}
			return nil
		}
		if _, supported := supportedAudioExtensions[extension]; !supported {
			return nil
		}
		relative := normalizedRelativeLibraryPath(root, path)
		if !matchesPatterns(relative, include, exclude) {
			return nil
		}
		normalizedPath := normalizePlatformPath(path)
		if cueFile, owned := ownedByTarget[normalizedPath]; owned {
			if info, infoErr := entry.Info(); infoErr == nil {
				cueFile.FileInfo = info
			}
			if err := emitFile(cueFile); err != nil {
				return err
			}
			delete(ownedByTarget, normalizedPath)
			return nil
		}
		file := DiscoveredFile{AudioPath: path, RelativePath: relativeLibraryPath(root, path)}
		if info, infoErr := entry.Info(); infoErr == nil {
			file.FileInfo = info
		}
		return emitFile(file)
	}
	if err := filepath.WalkDir(root, secondWalk); err != nil {
		return count, err
	}

	remaining := make([]DiscoveredFile, 0, len(ownedByTarget))
	for _, file := range ownedByTarget {
		remaining = append(remaining, file)
	}
	sort.SliceStable(remaining, func(i, j int) bool { return remaining[i].RelativePath < remaining[j].RelativePath })
	for _, file := range remaining {
		if err := emitFile(file); err != nil {
			return count, err
		}
	}
	remainingErrors := make([]DiscoveredFile, 0)
	for key, records := range errorsByCue {
		if _, seen := seenCueErrors[key]; seen {
			continue
		}
		remainingErrors = append(remainingErrors, records...)
	}
	sort.SliceStable(remainingErrors, func(i, j int) bool { return remainingErrors[i].RelativePath < remainingErrors[j].RelativePath })
	for _, file := range remainingErrors {
		if err := emitFile(file); err != nil {
			return count, err
		}
	}
	return count, nil
}

func cueReferences(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	seen := make(map[string]struct{})
	result := make([]string, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimPrefix(scanner.Text(), "\ufeff")
		match := cueQuotedReferencePattern.FindStringSubmatch(line)
		if len(match) == 0 {
			match = cueUnquotedReferencePattern.FindStringSubmatch(line)
		}
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			return nil, errors.New("CUE file reference is empty")
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("CUE sheet contains no audio files")
	}
	return result, nil
}

var (
	cueQuotedReferencePattern   = regexp.MustCompile(`(?i)^\s*FILE\s+"([^"]+)"\s+\S+`)
	cueUnquotedReferencePattern = regexp.MustCompile(`(?i)^\s*FILE\s+(.+?)\s+\S+\s*$`)
)

func compilePatterns(values []string) ([]*regexp.Regexp, error) {
	patterns := make([]*regexp.Regexp, 0, len(values))
	for _, value := range values {
		pattern, err := compileLibraryGlob(value)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func matchesPatterns(path string, include, exclude []*regexp.Regexp) bool {
	included := len(include) == 0
	for _, pattern := range include {
		if pattern.MatchString(path) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range exclude {
		if pattern.MatchString(path) {
			return false
		}
	}
	return true
}

func scanCancelled(ctx context.Context, callback func(context.Context) (bool, error)) (bool, error) {
	if ctx.Err() != nil {
		return true, nil
	}
	if callback == nil {
		return false, nil
	}
	return callback(ctx)
}

func relativeLibraryPath(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(norm.NFKC.String(value))
}

func normalizedRelativeLibraryPath(root, path string) string {
	return normalizePlatformPath(relativeLibraryPath(root, path))
}

func normalizePlatformPath(path string) string {
	value := norm.NFKC.String(filepath.ToSlash(path))
	if runtime.GOOS == "windows" {
		return strings.ToLower(value)
	}
	return value
}

func (file DiscoveredFile) String() string {
	if file.CuePath == "" {
		return file.RelativePath
	}
	return fmt.Sprintf("%s (CUE %s)", file.RelativePath, file.CuePath)
}
