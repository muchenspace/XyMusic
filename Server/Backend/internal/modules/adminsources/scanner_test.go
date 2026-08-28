package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFilesystemScannerDiscoversPatternsCUEAndRecordsFailures(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.flac")
	for path, content := range map[string]string{
		filepath.Join(root, "album", "song.flac"): "flac",
		filepath.Join(root, "skip.mp3"):           "mp3",
		filepath.Join(root, "disc.wav"):           "wav",
		filepath.Join(root, "disc.cue"):           `FILE "disc.wav" WAVE`,
		filepath.Join(root, "bad.cue"):            `FILE "../` + filepath.Base(outside) + `" WAVE`,
		outside:                                   "outside",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	synchronizer := &fileSynchronizerStub{archived: 2}
	scanner, err := NewFilesystemScanner(synchronizer)
	if err != nil {
		t.Fatal(err)
	}
	var progress []ScanProgress
	result, err := scanner.Scan(context.Background(), ScanInput{
		ScanRunID: testRunID, RootID: testRootID, Directory: root,
		IncludePatterns: []string{"**/*.flac", "**/*.wav"}, ExcludePatterns: []string{"skip*"},
		OnProgress: func(_ context.Context, value ScanProgress) error {
			progress = append(progress, value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DiscoveredFiles != 3 || result.ProcessedFiles != 3 || result.FailedFiles != 1 || result.ArchivedFiles != 2 {
		t.Fatalf("result=%+v", result)
	}
	if len(progress) != 4 || progress[0] != (ScanProgress{}) || progress[len(progress)-1].ProcessedFiles != 3 {
		t.Fatalf("progress=%+v", progress)
	}
	paths := make([]string, 0, len(synchronizer.files))
	var cue, failure bool
	for _, file := range synchronizer.files {
		paths = append(paths, file.RelativePath)
		cue = cue || file.CuePath != ""
		failure = failure || file.ScanError != nil
	}
	sort.Strings(paths)
	if !cue || !failure {
		t.Fatalf("files=%+v", synchronizer.files)
	}
	for _, scanRunID := range synchronizer.scanRunIDs {
		if scanRunID != testRunID {
			t.Fatalf("scan run id=%q want=%q", scanRunID, testRunID)
		}
	}
}

func TestFilesystemScannerDoesNotBlockAfterProgressCallbackFails(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 128; index++ {
		path := filepath.Join(root, fmt.Sprintf("song-%03d.flac", index))
		if err := os.WriteFile(path, []byte("flac"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	synchronizer := &fileSynchronizerStub{}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	callbackErr := errors.New("progress callback failed")
	done := make(chan error, 1)
	go func() {
		_, scanErr := scanner.Scan(context.Background(), ScanInput{
			RootID: testRootID, Directory: root,
			OnProgress: func(_ context.Context, progress ScanProgress) error {
				if progress.ProcessedFiles > 0 {
					return callbackErr
				}
				return nil
			},
		})
		done <- scanErr
	}()
	select {
	case scanErr := <-done:
		if !errors.Is(scanErr, callbackErr) {
			t.Fatalf("scan error = %v, want progress callback error", scanErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scan blocked after progress callback failure")
	}
}

func TestFilesystemScannerUsesPreparedCommitBatchesWithItemIsolation(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 7; index++ {
		path := filepath.Join(root, "song-"+strconv.Itoa(index)+".flac")
		if err := os.WriteFile(path, []byte("flac"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	synchronizer := &preparedBatchSynchronizer{failPath: "song-3.flac"}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 4, CommitWorkers: 1, CommitBatchSize: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), ScanInput{
		RootID: testRootID, ScanRunID: testRunID, Directory: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProcessedFiles != 7 || result.FailedFiles != 1 {
		t.Fatalf("result=%+v", result)
	}
	if synchronizer.batchItems.Load() != 7 || synchronizer.singleItems.Load() != 0 {
		t.Fatalf("batch items=%d single items=%d", synchronizer.batchItems.Load(), synchronizer.singleItems.Load())
	}
	if synchronizer.batchCalls.Load() < 2 {
		t.Fatalf("batch calls=%d, expected multiple bounded windows", synchronizer.batchCalls.Load())
	}
}

func TestDiscoverLibraryFilesCarriesAudioFileInfo(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	if err := os.WriteFile(path, []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	files, err := discoverLibraryFiles(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].FileInfo == nil {
		t.Fatalf("discovered files = %+v", files)
	}
	if files[0].FileInfo.Size() != want.Size() ||
		files[0].FileInfo.ModTime() != want.ModTime() {
		t.Fatalf("file info = %v/%v, want %v/%v",
			files[0].FileInfo.Size(), files[0].FileInfo.ModTime(), want.Size(), want.ModTime())
	}
}

func TestFilesystemScannerHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &fileSynchronizerStub{}
	scanner, _ := NewFilesystemScanner(synchronizer)
	_, err := scanner.Scan(context.Background(), ScanInput{
		RootID: testRootID, Directory: root,
		IsCancelled: func(context.Context) (bool, error) { return true, nil },
	})
	if !errors.Is(err, ErrScanCancelled) {
		t.Fatalf("err=%v", err)
	}
	if len(synchronizer.files) != 0 {
		t.Fatalf("processed files=%d", len(synchronizer.files))
	}
}

func TestFilesystemScannerChecksCancellationBeforePreparingSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &preparingFileSynchronizer{}
	scanner, err := NewFilesystemScanner(synchronizer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = scanner.Scan(context.Background(), ScanInput{
		RootID: testRootID, Directory: root,
		IsCancelled: func(context.Context) (bool, error) { return true, nil },
	})
	if !errors.Is(err, ErrScanCancelled) {
		t.Fatalf("err=%v", err)
	}
	if synchronizer.prepareCalls.Load() != 0 {
		t.Fatalf("snapshot preparation calls=%d", synchronizer.prepareCalls.Load())
	}
}

func TestFilesystemScannerCancelsBlockedProcess(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &blockingFileSynchronizer{started: make(chan struct{})}
	var controlCalls atomic.Int32
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, scanErr := scanner.Scan(context.Background(), ScanInput{
			RootID: testRootID, Directory: root,
			IsCancelled: func(context.Context) (bool, error) {
				return controlCalls.Add(1) >= 3, nil
			},
		})
		done <- scanErr
	}()
	select {
	case <-synchronizer.started:
	case <-time.After(time.Second):
		t.Fatal("file processing did not start")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrScanCancelled) {
			t.Fatalf("scan error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked file processing was not cancelled")
	}
}

func TestFilesystemScannerUsesConfiguredWorkers(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 9; index++ {
		path := filepath.Join(root, fmt.Sprintf("song-%02d.flac", index))
		if err := os.WriteFile(path, []byte("flac"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	synchronizer := &concurrentFileSynchronizer{}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), ScanInput{Directory: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.DiscoveredFiles != 9 || result.ProcessedFiles != 9 || synchronizer.maximum.Load() != 3 {
		t.Fatalf("result=%+v maximum=%d", result, synchronizer.maximum.Load())
	}
}

func TestFilesystemScannerSeparatesPreparationFromBoundedCommitStage(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 8; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("song-%02d.flac", index)), []byte("flac"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	synchronizer := &pipelineSynchronizer{}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 4, CommitWorkers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), ScanInput{Directory: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProcessedFiles != 8 || synchronizer.processed.Load() != 8 {
		t.Fatalf("result=%+v processed=%d", result, synchronizer.processed.Load())
	}
	if maximum := synchronizer.maximumCommit.Load(); maximum > 2 {
		t.Fatalf("commit stage exceeded configured bound: %d", maximum)
	}
	if synchronizer.prepared.Load() != 8 {
		t.Fatalf("prepared=%d", synchronizer.prepared.Load())
	}
}

func TestFilesystemScannerRecordsPreparedFileFailuresThroughCommitStage(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 4; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("song-%02d.flac", index)), []byte("flac"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	synchronizer := &preparedFailureSynchronizer{}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 2, CommitWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), ScanInput{Directory: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProcessedFiles != 4 || result.FailedFiles != 4 || synchronizer.handled.Load() != 4 {
		t.Fatalf("result=%+v handled=%d", result, synchronizer.handled.Load())
	}
}

func TestFilesystemScannerCancelsBlockedPreparation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &blockingPrepareSynchronizer{started: make(chan struct{})}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 1, CommitWorkers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, scanErr := scanner.Scan(ctx, ScanInput{Directory: root})
		done <- scanErr
	}()
	select {
	case <-synchronizer.started:
	case <-time.After(time.Second):
		t.Fatal("file preparation did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrScanCancelled) {
			t.Fatalf("scan error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked preparation was not cancelled")
	}
}

func TestFilesystemScannerFlushesBeforeArchivingMissingSources(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &finalizingFileSynchronizer{}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(context.Background(), ScanInput{RootID: testRootID, Directory: root}); err != nil {
		t.Fatal(err)
	}
	if !synchronizer.flushed || !synchronizer.archivedAfterFlush {
		t.Fatalf("flush/archive order: flushed=%v archivedAfterFlush=%v", synchronizer.flushed, synchronizer.archivedAfterFlush)
	}
}

func TestFilesystemScannerTimesOutBlockedFinalization(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &blockingFinalizationSynchronizer{}
	scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
		Synchronizer: synchronizer, Workers: 1, FinalizeTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = scanner.Scan(context.Background(), ScanInput{RootID: testRootID, Directory: root})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("scan finalization error=%v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("scan finalization timeout took too long: %s", time.Since(started))
	}
}

func BenchmarkFilesystemScanner(b *testing.B) {
	root := b.TempDir()
	for index := 0; index < 2_000; index++ {
		path := filepath.Join(root, fmt.Sprintf("track-%04d.flac", index))
		if err := os.WriteFile(path, []byte("flac"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	for _, workers := range []int{1, 8} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			synchronizer := &fileSynchronizerStub{}
			scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
				Synchronizer: synchronizer, Workers: workers,
			})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				result, err := scanner.Scan(context.Background(), ScanInput{Directory: root})
				if err != nil || result.ProcessedFiles != 2_000 {
					b.Fatalf("result=%+v error=%v", result, err)
				}
				synchronizer.mu.Lock()
				synchronizer.files = synchronizer.files[:0]
				synchronizer.scanRunIDs = synchronizer.scanRunIDs[:0]
				synchronizer.mu.Unlock()
			}
		})
	}
}

func BenchmarkFilesystemScannerRealisticWorkload(b *testing.B) {
	for _, fileCount := range []int{1_000, 5_000} {
		b.Run(strconv.Itoa(fileCount), func(b *testing.B) {
			root := b.TempDir()
			filesPerDirectory := 100
			for index := 0; index < fileCount; index++ {
				directory := filepath.Join(root, fmt.Sprintf("album-%03d", index/filesPerDirectory))
				if err := os.MkdirAll(directory, 0o700); err != nil {
					b.Fatal(err)
				}
				name := fmt.Sprintf("track-%03d", index%filesPerDirectory)
				if err := os.WriteFile(filepath.Join(directory, name+".flac"), []byte("audio"), 0o600); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+".lrc"), []byte("[00:01.00]lyrics"), 0o600); err != nil {
					b.Fatal(err)
				}
			}

			synchronizer := &filesystemWorkloadSynchronizer{}
			scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
				Synchronizer: synchronizer, Workers: 8,
			})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				result, err := scanner.Scan(context.Background(), ScanInput{
					RootID: testRootID, Directory: root,
					IsCancelled: func(context.Context) (bool, error) {
						return false, nil
					},
				})
				if err != nil || result.ProcessedFiles != fileCount || synchronizer.processed.Load() != int32(fileCount) {
					b.Fatalf("result=%+v error=%v processed=%d", result, err, synchronizer.processed.Load())
				}
				synchronizer.processed.Store(0)
			}
		})
	}
}

type filesystemWorkloadSynchronizer struct {
	processed atomic.Int32
}

type preparingFileSynchronizer struct {
	prepareCalls atomic.Int32
}

func (synchronizer *preparingFileSynchronizer) PrepareScan(
	ctx context.Context, _ string, _ string,
) (context.Context, func(), error) {
	synchronizer.prepareCalls.Add(1)
	return ctx, nil, nil
}

func (*preparingFileSynchronizer) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return nil
}

func (*preparingFileSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

type preparedFailureSynchronizer struct {
	handled atomic.Int32
}

func (*preparedFailureSynchronizer) PrepareFile(
	context.Context, string, string, DiscoveredFile, time.Time,
) (any, bool, error) {
	return nil, true, errors.New("prepared metadata failed")
}

func (*preparedFailureSynchronizer) ProcessPreparedFile(
	context.Context, string, string, DiscoveredFile, time.Time, any,
) error {
	return errors.New("unexpected prepared commit")
}

func (synchronizer *preparedFailureSynchronizer) HandlePreparedFileFailure(
	context.Context, string, string, DiscoveredFile, time.Time, error,
) error {
	synchronizer.handled.Add(1)
	return nil
}

func (*preparedFailureSynchronizer) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return errors.New("unexpected fallback process")
}

func (*preparedFailureSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

type blockingPrepareSynchronizer struct {
	started   chan struct{}
	startOnce sync.Once
}

func (synchronizer *blockingPrepareSynchronizer) PrepareFile(
	ctx context.Context, _ string, _ string, _ DiscoveredFile, _ time.Time,
) (any, bool, error) {
	synchronizer.startOnce.Do(func() { close(synchronizer.started) })
	<-ctx.Done()
	return nil, true, ctx.Err()
}

func (*blockingPrepareSynchronizer) ProcessPreparedFile(
	context.Context, string, string, DiscoveredFile, time.Time, any,
) error {
	return nil
}

func (*blockingPrepareSynchronizer) HandlePreparedFileFailure(
	context.Context, string, string, DiscoveredFile, time.Time, error,
) error {
	return nil
}

func (*blockingPrepareSynchronizer) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return nil
}

func (*blockingPrepareSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

type pipelineSynchronizer struct {
	activeCommit  atomic.Int32
	maximumCommit atomic.Int32
	prepared      atomic.Int32
	processed     atomic.Int32
}

type preparedBatchSynchronizer struct {
	failPath    string
	batchCalls  atomic.Int64
	batchItems  atomic.Int64
	singleItems atomic.Int64
}

func (synchronizer *preparedBatchSynchronizer) PrepareFile(
	context.Context, string, string, DiscoveredFile, time.Time,
) (any, bool, error) {
	return nil, true, nil
}

func (synchronizer *preparedBatchSynchronizer) ProcessPreparedFile(
	context.Context, string, string, DiscoveredFile, time.Time, any,
) error {
	synchronizer.singleItems.Add(1)
	return nil
}

func (*preparedBatchSynchronizer) HandlePreparedFileFailure(
	context.Context, string, string, DiscoveredFile, time.Time, error,
) error {
	return nil
}

func (synchronizer *preparedBatchSynchronizer) ProcessPreparedFileBatch(
	_ context.Context, _ string, _ string, files []PreparedScanBatchFile, _ time.Time,
) []error {
	return synchronizer.processBatch(files)
}

func (synchronizer *preparedBatchSynchronizer) processBatch(files []PreparedScanBatchFile) []error {
	synchronizer.batchCalls.Add(1)
	synchronizer.batchItems.Add(int64(len(files)))
	results := make([]error, len(files))
	for index, file := range files {
		if filepath.Base(file.File.RelativePath) == synchronizer.failPath {
			results[index] = errors.New("synthetic per-file commit failure")
		}
	}
	return results
}

func (synchronizer *preparedBatchSynchronizer) ProcessFile(
	context.Context, string, string, DiscoveredFile, time.Time,
) error {
	return nil
}

func (*preparedBatchSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

func (synchronizer *pipelineSynchronizer) PrepareFile(
	context.Context, string, string, DiscoveredFile, time.Time,
) (any, bool, error) {
	synchronizer.prepared.Add(1)
	return struct{}{}, true, nil
}

func (synchronizer *pipelineSynchronizer) ProcessPreparedFile(
	ctx context.Context, _ string, _ string, _ DiscoveredFile, _ time.Time, _ any,
) error {
	active := synchronizer.activeCommit.Add(1)
	for {
		maximum := synchronizer.maximumCommit.Load()
		if active <= maximum || synchronizer.maximumCommit.CompareAndSwap(maximum, active) {
			break
		}
	}
	select {
	case <-time.After(10 * time.Millisecond):
	case <-ctx.Done():
		synchronizer.activeCommit.Add(-1)
		return ctx.Err()
	}
	synchronizer.processed.Add(1)
	synchronizer.activeCommit.Add(-1)
	return nil
}

func (*pipelineSynchronizer) HandlePreparedFileFailure(context.Context, string, string, DiscoveredFile, time.Time, error) error {
	return nil
}

func (*pipelineSynchronizer) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return errors.New("unexpected unprepared file")
}

func (*pipelineSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

func (synchronizer *filesystemWorkloadSynchronizer) PrepareScan(
	ctx context.Context,
	_, _ string,
) (context.Context, func(), error) {
	snapshot := &sourceScanSnapshot{sidecarsByDir: make(map[string]*sidecarDirectoryState)}
	return context.WithValue(ctx, sourceScanSnapshotContextKey{}, snapshot), nil, nil
}

func (synchronizer *filesystemWorkloadSynchronizer) ProcessFile(
	ctx context.Context,
	_, _ string,
	file DiscoveredFile,
	_ time.Time,
) error {
	if _, err := os.Stat(file.AudioPath); err != nil {
		return err
	}
	if _, err := fileSHA256(file.AudioPath); err != nil {
		return err
	}
	if _, err := readSidecarLyricsCached(sourceScanSnapshotFromContext(ctx), file.AudioPath); err != nil {
		return err
	}
	synchronizer.processed.Add(1)
	return nil
}

func (*filesystemWorkloadSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

type fileSynchronizerStub struct {
	mu         sync.Mutex
	files      []DiscoveredFile
	scanRunIDs []string
	archived   int
}

type blockingFinalizationSynchronizer struct{}

func (*blockingFinalizationSynchronizer) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return nil
}

func (*blockingFinalizationSynchronizer) ArchiveMissing(ctx context.Context, _ string, _, _ time.Time) (int, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

type finalizingFileSynchronizer struct {
	flushed            bool
	archivedAfterFlush bool
}

func (*finalizingFileSynchronizer) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return nil
}

func (synchronizer *finalizingFileSynchronizer) FlushScan(context.Context, string, time.Time) error {
	synchronizer.flushed = true
	return nil
}

func (synchronizer *finalizingFileSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	synchronizer.archivedAfterFlush = synchronizer.flushed
	return 0, nil
}

func (stub *fileSynchronizerStub) ProcessFile(
	_ context.Context,
	_, scanRunID string,
	file DiscoveredFile,
	_ time.Time,
) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.files = append(stub.files, file)
	stub.scanRunIDs = append(stub.scanRunIDs, scanRunID)
	return nil
}
func (stub *fileSynchronizerStub) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return stub.archived, nil
}

type concurrentFileSynchronizer struct {
	active    atomic.Int32
	maximum   atomic.Int32
	processed atomic.Int32
}

type blockingFileSynchronizer struct {
	started   chan struct{}
	startOnce sync.Once
}

func (synchronizer *blockingFileSynchronizer) ProcessFile(
	ctx context.Context,
	_, _ string,
	_ DiscoveredFile,
	_ time.Time,
) error {
	synchronizer.startOnce.Do(func() { close(synchronizer.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (*blockingFileSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}

func (synchronizer *concurrentFileSynchronizer) ProcessFile(
	context.Context,
	string,
	string,
	DiscoveredFile,
	time.Time,
) error {
	active := synchronizer.active.Add(1)
	for {
		maximum := synchronizer.maximum.Load()
		if active <= maximum || synchronizer.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	synchronizer.processed.Add(1)
	synchronizer.active.Add(-1)
	return nil
}

func (synchronizer *concurrentFileSynchronizer) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}
