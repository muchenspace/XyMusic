package adminsources

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkFilesystemScannerPipelineLoad(b *testing.B) {
	for _, fileCount := range []int{256, 1_000, 3_000, 5_000} {
		for _, workers := range []int{4, 16} {
			for _, commitWorkers := range []int{2, 4} {
				b.Run(scannerLoadBenchmarkName(fileCount, workers, commitWorkers), func(b *testing.B) {
					root := b.TempDir()
					for index := 0; index < fileCount; index++ {
						path := filepath.Join(root, "song-"+strconv.Itoa(index)+".flac")
						if err := os.WriteFile(path, []byte("fake flac payload"), 0o600); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportAllocs()
					b.ResetTimer()
					var discovery, scanNs, prepared, hashed, probed, uploaded, uploadNs, committed, commitNs, batchCalls int64
					for iteration := 0; iteration < b.N; iteration++ {
						b.StopTimer()
						discoveryStarted := time.Now()
						discovered, err := discoverLibraryFiles(root, nil, nil)
						if err != nil {
							b.Fatal(err)
						}
						if len(discovered) != fileCount {
							b.Fatalf("discovered files = %d, want %d", len(discovered), fileCount)
						}
						atomic.AddInt64(&discovery, time.Since(discoveryStarted).Nanoseconds())
						pipeline := &scannerLoadPipeline{
							prepared: &prepared, hashed: &hashed, probed: &probed,
							uploaded: &uploaded, uploadNs: &uploadNs,
							committed: &committed, commitNs: &commitNs, batchCalls: &batchCalls,
						}
						scanner, err := NewFilesystemScannerWithOptions(FilesystemScannerOptions{
							Synchronizer: pipeline, Workers: workers, CommitWorkers: commitWorkers,
						})
						if err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
						scanStarted := time.Now()
						result, err := scanner.Scan(context.Background(), ScanInput{Directory: root})
						atomic.AddInt64(&scanNs, time.Since(scanStarted).Nanoseconds())
						if err != nil {
							b.Fatal(err)
						}
						if result.ProcessedFiles != fileCount || result.FailedFiles != 0 {
							b.Fatalf("scan result = %+v", result)
						}
						b.StopTimer()
					}
					b.ReportMetric(float64(fileCount*b.N)/(float64(atomic.LoadInt64(&scanNs))/float64(time.Second)), "files/sec")
					b.ReportMetric(float64(atomic.LoadInt64(&discovery))/float64(b.N), "discovery_ns/scan")
					b.ReportMetric(float64(atomic.LoadInt64(&discovery))/float64(fileCount*b.N), "discovery_ns/file")
					b.ReportMetric(float64(atomic.LoadInt64(&hashed))/float64(fileCount*b.N), "hash_ns/file")
					b.ReportMetric(float64(atomic.LoadInt64(&probed))/float64(fileCount*b.N), "probe_ns/file")
					b.ReportMetric(float64(atomic.LoadInt64(&uploadNs))/float64(fileCount*b.N), "upload_ns/file")
					b.ReportMetric(float64(atomic.LoadInt64(&commitNs))/float64(fileCount*b.N), "commit_ns/file")
					b.ReportMetric(float64(atomic.LoadInt64(&prepared))/float64(b.N), "prepare/file")
					b.ReportMetric(float64(atomic.LoadInt64(&uploaded))/float64(b.N), "upload/file")
					b.ReportMetric(float64(atomic.LoadInt64(&committed))/float64(b.N), "commit/file")
					b.ReportMetric(float64(atomic.LoadInt64(&batchCalls))/float64(b.N), "commit_batches/scan")
				})
			}
		}
	}
}

func scannerLoadBenchmarkName(fileCount, workers, commitWorkers int) string {
	return "files-" + strconv.Itoa(fileCount) + "/prepare-" + strconv.Itoa(workers) + "/commit-" + strconv.Itoa(commitWorkers)
}

type scannerLoadPipeline struct {
	prepared   *int64
	hashed     *int64
	probed     *int64
	uploaded   *int64
	uploadNs   *int64
	committed  *int64
	commitNs   *int64
	batchCalls *int64
}

func (pipeline *scannerLoadPipeline) PrepareScan(
	ctx context.Context, _, _ string,
) (context.Context, func(), error) {
	return context.WithValue(ctx, sourceScanSnapshotContextKey{}, &sourceScanSnapshot{
		sidecarsByDir: make(map[string]*sidecarDirectoryState),
	}), nil, nil
}

func (pipeline *scannerLoadPipeline) PrepareFile(
	ctx context.Context, _, _ string, file DiscoveredFile, _ time.Time,
) (any, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	if file.FileInfo == nil {
		if _, err := os.Stat(file.AudioPath); err != nil {
			return nil, true, err
		}
	}
	atomic.AddInt64(pipeline.prepared, 1)
	hashStarted := time.Now()
	if _, err := fileSHA256(file.AudioPath); err != nil {
		return nil, true, err
	}
	if pipeline.hashed != nil {
		atomic.AddInt64(pipeline.hashed, time.Since(hashStarted).Nanoseconds())
	}
	probeStarted := time.Now()
	// Keep the probe stage deterministic without requiring an external codec
	// process for this scheduler/load benchmark.
	time.Sleep(150 * time.Microsecond)
	if pipeline.probed != nil {
		atomic.AddInt64(pipeline.probed, time.Since(probeStarted).Nanoseconds())
	}
	return struct{}{}, true, nil
}

func (pipeline *scannerLoadPipeline) ProcessPreparedFile(
	ctx context.Context, _, _ string, _ DiscoveredFile, _ time.Time, _ any,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	uploadStarted := time.Now()
	atomic.AddInt64(pipeline.uploaded, 1)
	time.Sleep(150 * time.Microsecond)
	if pipeline.uploadNs != nil {
		atomic.AddInt64(pipeline.uploadNs, time.Since(uploadStarted).Nanoseconds())
	}
	commitStarted := time.Now()
	atomic.AddInt64(pipeline.committed, 1)
	time.Sleep(150 * time.Microsecond)
	if pipeline.commitNs != nil {
		atomic.AddInt64(pipeline.commitNs, time.Since(commitStarted).Nanoseconds())
	}
	return nil
}

func (pipeline *scannerLoadPipeline) ProcessPreparedFileBatch(
	ctx context.Context,
	_ string,
	_ string,
	files []PreparedScanBatchFile,
	_ time.Time,
) []error {
	results := make([]error, len(files))
	if err := ctx.Err(); err != nil {
		for index := range results {
			results[index] = err
		}
		return results
	}
	for range files {
		uploadStarted := time.Now()
		atomic.AddInt64(pipeline.uploaded, 1)
		time.Sleep(150 * time.Microsecond)
		if pipeline.uploadNs != nil {
			atomic.AddInt64(pipeline.uploadNs, time.Since(uploadStarted).Nanoseconds())
		}
	}
	commitStarted := time.Now()
	time.Sleep(150 * time.Microsecond)
	atomic.AddInt64(pipeline.committed, int64(len(files)))
	if pipeline.commitNs != nil {
		elapsed := time.Since(commitStarted).Nanoseconds()
		atomic.AddInt64(pipeline.commitNs, elapsed/int64(max(1, len(files))))
	}
	if pipeline.batchCalls != nil {
		atomic.AddInt64(pipeline.batchCalls, 1)
	}
	return results
}

func (*scannerLoadPipeline) HandlePreparedFileFailure(
	context.Context, string, string, DiscoveredFile, time.Time, error,
) error {
	return nil
}

func (*scannerLoadPipeline) ProcessFile(context.Context, string, string, DiscoveredFile, time.Time) error {
	return nil
}

func (*scannerLoadPipeline) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return 0, nil
}
