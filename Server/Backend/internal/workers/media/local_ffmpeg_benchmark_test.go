package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestWorkerProcessesShortAudioWithLocalFFmpeg(t *testing.T) {
	ffmpeg, ffprobe := localFFmpegTools(t)
	source := makeLocalAudioFixture(t, ffmpeg)
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	attemptID := "22222222-2222-4222-8222-222222222222"
	store := &workerStoreStub{
		job: &MediaJob{ID: "11111111-1111-4111-8111-111111111111", SourceAssetID: "asset",
			TrackID: "33333333-3333-4333-8333-333333333333", Generation: 1, AttemptID: &attemptID},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	worker := newLocalFFmpegWorker(t, store, &workerStorageStub{source: source}, ffmpeg, ffprobe, 2)
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.commit == nil || len(store.commit.Generated) != 4 {
		t.Fatalf("commit = %#v", store.commit)
	}
	for _, output := range store.commit.Generated {
		if output.SizeBytes <= 0 || output.ChecksumSHA256 == "" || output.Profile.Quality == "" {
			t.Fatalf("generated output = %#v", output)
		}
	}
}

func BenchmarkWorkerLocalFFmpeg(b *testing.B) {
	ffmpeg, ffprobe := localFFmpegTools(b)
	source := makeLocalAudioFixture(b, ffmpeg)
	for _, workers := range []int{1, 2, 4} {
		b.Run("workers-"+fmt.Sprint(workers), func(b *testing.B) {
			benchmarkLocalFFmpegJobs(b, source, ffmpeg, ffprobe, workers, 1)
		})
	}
}

func BenchmarkWorkerLocalFFmpegLongAudio(b *testing.B) {
	ffmpeg, ffprobe := localFFmpegTools(b)
	source := makeLocalAudioFixtureWithDuration(b, ffmpeg, "5")
	for _, workers := range []int{1, 2, 4} {
		for _, threads := range []int{1, 2} {
			workers, threads := workers, threads
			b.Run(fmt.Sprintf("duration-5s/workers-%d/threads-%d", workers, threads), func(b *testing.B) {
				benchmarkLocalFFmpegJobs(b, source, ffmpeg, ffprobe, workers, threads)
			})
		}
	}
}

func benchmarkLocalFFmpegJobs(
	b *testing.B,
	source []byte,
	ffmpeg, ffprobe string,
	workers, ffmpegThreads int,
) {
	b.Helper()
	temporaryRoot := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	started := time.Now()
	var claimCalls, completionCalls int64
	for iteration := 0; iteration < b.N; iteration++ {
		jobs := make([]*MediaJob, workers)
		for index := range jobs {
			attemptID := fmt.Sprintf("attempt-%d-%d", iteration, index)
			jobs[index] = &MediaJob{
				ID: fmt.Sprintf("job-%d-%d", iteration, index), SourceAssetID: "asset",
				TrackID: fmt.Sprintf("track-%d-%d", iteration, index), Generation: 1, AttemptID: &attemptID,
			}
		}
		digest := sha256.Sum256(source)
		checksum := hex.EncodeToString(digest[:])
		store := &localFFmpegLoadStore{
			jobs: jobs, source: &SourceAsset{ID: "asset", ObjectKey: "source",
				SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
		}
		storage := &workerStorageStub{source: source}
		worker, err := New(Options{
			Store: store, Storage: storage, Workers: workers,
			FFmpegPath: ffmpeg, FFprobePath: ffprobe, FFmpegThreads: ffmpegThreads,
			WorkerID: "local-ffmpeg-benchmark", TemporaryRoot: temporaryRoot,
			ProfileVersion: "benchmark", TranscodeTimeout: time.Minute,
		})
		if err != nil {
			b.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var group sync.WaitGroup
		errorsFound := make(chan error, workers)
		for index := 0; index < workers; index++ {
			group.Add(1)
			go func() {
				defer group.Done()
				for {
					worked, runErr := worker.RunNext(ctx)
					if runErr != nil {
						errorsFound <- runErr
						return
					}
					if !worked {
						return
					}
				}
			}()
		}
		group.Wait()
		cancel()
		_ = worker.Close()
		select {
		case err := <-errorsFound:
			b.Fatal(err)
		default:
		}
		if store.commits != workers || store.failures != 0 {
			b.Fatalf("commits/failures = %d/%d", store.commits, store.failures)
		}
		claimCalls += int64(store.claimCalls)
		completionCalls += int64(store.commits)
	}
	b.ReportMetric(float64(workers*b.N)/time.Since(started).Seconds(), "jobs/sec")
	b.ReportMetric(float64(claimCalls)/float64(workers*b.N), "claim_calls/job")
	b.ReportMetric(float64(completionCalls)/float64(workers*b.N), "completion_calls/job")
}

func localFFmpegTools(t testing.TB) (string, string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skipf("ffmpeg is unavailable: %v", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skipf("ffprobe is unavailable: %v", err)
	}
	return ffmpeg, ffprobe
}

func makeLocalAudioFixture(t testing.TB, ffmpeg string) []byte {
	return makeLocalAudioFixtureWithDuration(t, ffmpeg, "0.25")
}

func makeLocalAudioFixtureWithDuration(t testing.TB, ffmpeg, duration string) []byte {
	t.Helper()
	path := t.TempDir() + string(os.PathSeparator) + "source.flac"
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration="+duration,
		"-c:a", "flac", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate local audio fixture: %v: %s", err, output)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func newLocalFFmpegWorker(
	t testing.TB,
	store Store,
	storage ObjectStorage,
	ffmpeg, ffprobe string,
	workers int,
) *Worker {
	t.Helper()
	worker, err := New(Options{
		Store: store, Storage: storage, Workers: workers,
		FFmpegPath: ffmpeg, FFprobePath: ffprobe, FFmpegThreads: 1,
		WorkerID: "local-ffmpeg-test", TemporaryRoot: t.TempDir(), ProfileVersion: "test",
		TranscodeTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return worker
}

type localFFmpegLoadStore struct {
	mu         sync.Mutex
	jobs       []*MediaJob
	claimCalls int
	commits    int
	failures   int
	source     *SourceAsset
}

func (store *localFFmpegLoadStore) ClaimMediaJob(context.Context, string, time.Time, time.Duration) (*MediaJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimCalls++
	if len(store.jobs) == 0 {
		return nil, nil
	}
	job := store.jobs[0]
	store.jobs = store.jobs[1:]
	return job, nil
}

func (*localFFmpegLoadStore) RenewMediaLease(context.Context, string, string, string, time.Time, time.Time) (bool, error) {
	return true, nil
}

func (*localFFmpegLoadStore) MediaJobControl(context.Context, string, string, string) (JobControl, error) {
	return JobControl{Owned: true}, nil
}

func (store *localFFmpegLoadStore) FindReadySourceAsset(context.Context, string) (*SourceAsset, error) {
	return store.source, nil
}

func (store *localFFmpegLoadStore) CommitMediaJob(context.Context, CommitMediaJob) ([]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.commits++
	return nil, nil
}

func (store *localFFmpegLoadStore) FailMediaJob(context.Context, MediaJob, string, error, time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failures++
	return nil
}

func (*localFFmpegLoadStore) ScheduleReplacedAssetCleanup(context.Context, []string, time.Time) error {
	return nil
}

func (*localFFmpegLoadStore) EnqueueObjectCleanup(context.Context, string, string, time.Time) error {
	return nil
}

func (*localFFmpegLoadStore) ClaimObjectCleanup(context.Context, string, time.Time, time.Duration) (*CleanupJob, error) {
	return nil, nil
}

func (*localFFmpegLoadStore) ReadyAssetReferencesObject(context.Context, string) (bool, error) {
	return false, nil
}

func (*localFFmpegLoadStore) CompleteObjectCleanup(context.Context, CleanupJob, string, bool, time.Time) (bool, error) {
	return true, nil
}

func (*localFFmpegLoadStore) FailObjectCleanup(context.Context, CleanupJob, string, error, time.Time) error {
	return nil
}

var _ Store = (*localFFmpegLoadStore)(nil)
