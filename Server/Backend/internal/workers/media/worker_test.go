package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWorkerProcessesLosslessSegmentAndCommitsFencedVariants(t *testing.T) {
	attemptID := "22222222-2222-4222-8222-222222222222"
	sourceBytes := []byte("lossless-source")
	digest := sha256.Sum256(sourceBytes)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job: &MediaJob{
			ID: "11111111-1111-4111-8111-111111111111", SourceAssetID: "source-asset",
			TrackID: "33333333-3333-4333-8333-333333333333", Generation: 4,
			AttemptID: &attemptID, PublishOnReady: true,
			Payload: []byte(`{"segmentStartMs":250,"segmentEndMs":1250}`),
		},
		source: &SourceAsset{
			ID: "source-asset", ObjectKey: "media/source.flac", SizeBytes: int64(len(sourceBytes)),
			ChecksumSHA256: &checksum,
		},
		replaced: []string{"old-asset"},
	}
	storage := &workerStorageStub{source: sourceBytes}
	runner := &workerRunnerStub{probe: `{"streams":[{"codec_type":"audio","codec_name":"flac","sample_rate":"44100"}],"format":{"duration":"2.000"}}`}
	worker := newTestWorker(t, store, storage, runner, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.commit == nil || store.commit.DurationMS != 1_000 || store.commit.SampleRate == nil ||
		*store.commit.SampleRate != 44_100 || len(store.commit.Generated) != 4 {
		t.Fatalf("commit = %#v", store.commit)
	}
	for _, output := range store.commit.Generated {
		wantPrefix := "media/variants/" + store.job.TrackID + "/" + store.job.ID + "/" + attemptID + "/"
		if !strings.HasPrefix(output.ObjectKey, wantPrefix) || output.SizeBytes == 0 || output.ChecksumSHA256 == "" {
			t.Fatalf("generated output = %#v", output)
		}
	}
	if len(store.scheduled) != 1 || store.scheduled[0] != "old-asset" {
		t.Fatalf("scheduled cleanup = %#v", store.scheduled)
	}
	if len(store.enqueued) != 0 || store.failed != nil {
		t.Fatalf("abandoned=%#v failed=%v", store.enqueued, store.failed)
	}
	if len(storage.uploaded) != 4 || runner.ffmpegArguments == nil {
		t.Fatalf("uploads=%#v ffmpeg=%#v", storage.uploaded, runner.ffmpegArguments)
	}
	joined := strings.Join(runner.ffmpegArguments, " ")
	if !strings.Contains(joined, "-ss 0.250") || !strings.Contains(joined, "-t 1.000") ||
		strings.Count(joined, "-map 0:a:0 -vn") != 4 {
		t.Fatalf("ffmpeg arguments = %s", joined)
	}
}

func TestWorkerAppliesConfiguredFFmpegThreadBudget(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job:    &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	runner := &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
	}
	worker := newTestWorker(t, store, &workerStorageStub{source: source}, runner, Options{FFmpegThreads: 3})
	if _, err := worker.RunNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	arguments := strings.Join(runner.ffmpegArguments, " ")
	if !strings.Contains(arguments, "-threads 3") {
		t.Fatalf("FFmpeg thread budget missing from arguments: %s", arguments)
	}
}

func TestWorkerUsesBoundedMediaResourceDefaults(t *testing.T) {
	worker := newTestWorker(t, &workerStoreStub{}, &workerStorageStub{}, &workerRunnerStub{}, Options{Workers: 8})
	if worker.probeWorkers != 4 || cap(worker.probeGate) != 4 {
		t.Fatalf("probe budget=%d capacity=%d, want 4", worker.probeWorkers, cap(worker.probeGate))
	}
	if worker.storageWorkers != 16 || cap(worker.storageGate) != 16 {
		t.Fatalf("storage budget=%d capacity=%d, want 16", worker.storageWorkers, cap(worker.storageGate))
	}

	base := Options{
		Store: &workerStoreStub{}, Storage: &workerStorageStub{},
		Workers: 1, FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
		WorkerID: "test-worker", TemporaryRoot: t.TempDir(),
	}
	for name, options := range map[string]Options{
		"probe":   func() Options { copy := base; copy.ProbeWorkers = 65; return copy }(),
		"storage": func() Options { copy := base; copy.StorageWorkers = 65; return copy }(),
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("New accepted invalid %s budget", name)
		}
	}
}

func TestWorkerLimitsConcurrentFFprobeCalls(t *testing.T) {
	const (
		limit = 2
		calls = 8
	)
	runner := &blockingProbeRunner{
		started: make(chan struct{}, calls), release: make(chan struct{}),
	}
	worker := newTestWorker(t, &workerStoreStub{}, &workerStorageStub{}, runner, Options{
		Workers: calls, ProbeWorkers: limit,
	})

	var group sync.WaitGroup
	errorsChannel := make(chan error, calls)
	for range calls {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _, err := worker.probeSource(context.Background(), "unused", nil)
			errorsChannel <- err
		}()
	}
	for range limit {
		select {
		case <-runner.started:
		case <-time.After(time.Second):
			t.Fatal("ffprobe calls did not reach the configured concurrency")
		}
	}
	time.Sleep(20 * time.Millisecond)
	active, maximum := runner.snapshot()
	if active != limit || maximum > limit {
		t.Fatalf("ffprobe concurrency active=%d maximum=%d, want active=%d maximum<=%d", active, maximum, limit, limit)
	}
	close(runner.release)
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("ffprobe call failed: %v", err)
		}
	}
}

func TestWorkerLimitsConcurrentReusableVariantChecks(t *testing.T) {
	const (
		limit = 2
		count = 8
	)
	storage := &blockingStatStorage{
		started: make(chan struct{}, count), release: make(chan struct{}),
	}
	worker := newTestWorker(t, &workerStoreStub{}, &workerStorageStub{}, &workerRunnerStub{}, Options{
		Workers: count, StorageWorkers: limit,
	})
	variants := make([]GeneratedVariant, count)
	for index := range variants {
		variants[index] = GeneratedVariant{
			Profile:   AudioVariantProfile{Quality: "Q" + strconv.Itoa(index)},
			ObjectKey: "object-" + strconv.Itoa(index), ChecksumSHA256: "checksum", SizeBytes: 42,
		}
	}

	type verificationResult struct {
		variants []GeneratedVariant
		err      error
	}
	resultChannel := make(chan verificationResult, 1)
	go func() {
		verified, err := worker.verifyReusableVariants(context.Background(), storage, variants)
		resultChannel <- verificationResult{variants: verified, err: err}
	}()
	for range limit {
		select {
		case <-storage.started:
		case <-time.After(time.Second):
			t.Fatal("object checks did not reach the configured concurrency")
		}
	}
	time.Sleep(20 * time.Millisecond)
	active, maximum := storage.snapshot()
	if active != limit || maximum > limit {
		t.Fatalf("object-check concurrency active=%d maximum=%d, want active=%d maximum<=%d", active, maximum, limit, limit)
	}
	close(storage.release)
	result := <-resultChannel
	if result.err != nil || len(result.variants) != count {
		t.Fatalf("verified variants=%d error=%v", len(result.variants), result.err)
	}
}

func TestWorkerReusesScanProbeHintWithoutRunningFFprobe(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job: &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1,
			AttemptID: &attemptID, Payload: []byte(`{"sourceProbe":{"durationMs":1250,"codec":"flac","sampleRate":48000}}`)},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	runner := &workerRunnerStub{}
	worker := newTestWorker(t, store, &workerStorageStub{source: source}, runner, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if runner.probeCalls != 0 || store.commit == nil || store.commit.DurationMS != 1_250 ||
		store.commit.SampleRate == nil || *store.commit.SampleRate != 48_000 {
		t.Fatalf("probe calls/commit = %d/%#v", runner.probeCalls, store.commit)
	}
}

func TestWorkerReusesVerifiedReadyVariantsBeforeRunningFFmpeg(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	profiles := AudioVariantProfiles("aac")
	store := &workerStoreStub{
		job: &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1,
			AttemptID: &attemptID, Payload: []byte(`{"sourceProbe":{"durationMs":1000,"codec":"aac"}}`)},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	storage := &workerStorageStub{source: source, objectStats: map[string]objectStat{}}
	storage.objectStats["source"] = objectStat{sizeBytes: int64(len(source)), checksum: checksum, exists: true}
	for _, profile := range profiles {
		key := "media/variants/track/old/" + strings.ToLower(profile.Quality) + "." + profile.Extension
		store.reusable = append(store.reusable, GeneratedVariant{
			Profile: profile, ObjectKey: key, ChecksumSHA256: "variant-" + profile.Quality,
			SizeBytes: 42, SourceChecksumSHA256: checksum, ProfileVersion: "v1",
		})
		storage.objectStats[key] = objectStat{sizeBytes: 42, checksum: "variant-" + profile.Quality, exists: true}
	}
	runner := &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
	}
	worker := newTestWorker(t, store, storage, runner, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.reuseCalls != 1 || store.commit == nil || len(store.commit.Generated) != len(profiles) {
		t.Fatalf("reuse calls/commit = %d/%#v", store.reuseCalls, store.commit)
	}
	for _, output := range store.commit.Generated {
		if !output.Reused || output.SourceChecksumSHA256 != checksum || output.ProfileVersion != "v1" {
			t.Fatalf("reused output = %#v", output)
		}
	}
	if len(storage.uploaded) != 0 || storage.downloadCalls != 0 || runner.ffmpegArguments != nil {
		t.Fatalf("reused variants still downloaded, ran ffmpeg, or uploaded: downloads=%d uploads=%#v ffmpeg=%#v",
			storage.downloadCalls, storage.uploaded, runner.ffmpegArguments)
	}
}

func TestWorkerQueuesEveryUploadedObjectWhenCommitIsSuperseded(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job:       &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source:    &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
		commitErr: newInterruptedError("JOB_SUPERSEDED", "a newer media generation owns this track"),
	}
	worker := newTestWorker(t, store, &workerStorageStub{source: source}, &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac","sample_rate":"48000"}],"format":{"duration":"1"}}`,
	}, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if len(store.enqueued) != 3 {
		t.Fatalf("abandoned objects = %#v", store.enqueued)
	}
	for _, cleanup := range store.enqueued {
		if cleanup.reason != "ABANDONED_MEDIA_ATTEMPT" {
			t.Fatalf("cleanup = %#v", cleanup)
		}
	}
	if workerErrorCode(store.failed) != "JOB_SUPERSEDED" || len(store.scheduled) != 0 {
		t.Fatalf("failed=%v scheduled=%#v", store.failed, store.scheduled)
	}
}

func TestWorkerQueuesCurrentObjectWhenUploadFailsAfterObjectCreation(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job:    &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	storage := &workerStorageStub{source: source, uploadErrAt: 1}
	worker := newTestWorker(t, store, storage, &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
	}, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if len(store.enqueued) == 0 || len(store.enqueued) > 3 {
		t.Fatalf("abandoned objects = %#v", store.enqueued)
	}
	for _, cleanup := range store.enqueued {
		if cleanup.reason != "ABANDONED_MEDIA_ATTEMPT" ||
			!strings.HasSuffix(cleanup.key, ".m4a") {
			t.Fatalf("abandoned objects = %#v", store.enqueued)
		}
	}
}

func TestWorkerPersistsCancellationObservedDuringTranscode(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job:     &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source:  &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
		control: JobControl{Owned: true, CancelRequested: true},
	}
	runner := &workerRunnerStub{
		probe:       `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
		blockFFmpeg: true,
	}
	worker := newTestWorker(t, store, &workerStorageStub{source: source}, runner, Options{
		Lease: time.Second, Heartbeat: 500 * time.Millisecond, CancellationPoll: 5 * time.Millisecond,
	})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if workerErrorCode(store.failed) != "JOB_CANCELLED" || !isInterrupted(store.failed) {
		t.Fatalf("failure = %v", store.failed)
	}
}

func TestWorkerRunsObjectCleanupOnlyWhenNoMediaJobExists(t *testing.T) {
	attemptID := "cleanup-attempt"
	store := &workerStoreStub{cleanup: &CleanupJob{
		ID: "cleanup", ObjectKey: "media/old", Attempts: 1, MaxAttempts: 20, AttemptID: &attemptID,
	}}
	storage := &workerStorageStub{}
	worker := newTestWorker(t, store, storage, &workerRunnerStub{}, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != "media/old" || !store.cleanupCompleted {
		t.Fatalf("deleted=%#v completed=%v", storage.deleted, store.cleanupCompleted)
	}

	attemptID = "cleanup-attempt-2"
	deleteError := errors.New("storage unavailable")
	store = &workerStoreStub{cleanup: &CleanupJob{
		ID: "cleanup-2", ObjectKey: "media/old-2", Attempts: 2, MaxAttempts: 20, AttemptID: &attemptID,
	}}
	storage = &workerStorageStub{deleteErr: deleteError}
	worker = newTestWorker(t, store, storage, &workerRunnerStub{}, Options{})
	worked, err = worker.RunNext(context.Background())
	if err != nil || !worked || !errors.Is(store.cleanupFailed, deleteError) || store.cleanupCompleted {
		t.Fatalf("worked=%v error=%v failed=%v completed=%v", worked, err, store.cleanupFailed, store.cleanupCompleted)
	}
}

func TestCloseCancelsAndWaitsForActiveTranscode(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job:     &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source:  &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
		control: JobControl{Owned: true},
	}
	runner := &workerRunnerStub{
		probe:       `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
		blockFFmpeg: true, ffmpegStarted: make(chan struct{}),
	}
	worker := newTestWorker(t, store, &workerStorageStub{source: source}, runner, Options{})
	done := make(chan error, 1)
	go func() {
		_, err := worker.RunNext(context.Background())
		done <- err
	}()
	select {
	case <-runner.ffmpegStarted:
	case <-time.After(time.Second):
		t.Fatal("ffmpeg did not start")
	}
	queued := make(chan error, 1)
	go func() {
		_, err := worker.RunNext(context.Background())
		queued <- err
	}()
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunNext did not stop before Close returned")
	}
	select {
	case err := <-queued:
		if err != nil && !errors.Is(err, ErrWorkerClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued RunNext did not stop before Close returned")
	}
	if workerErrorCode(store.failed) != "WORKER_STOPPED" || !isInterrupted(store.failed) {
		t.Fatalf("failure = %v", store.failed)
	}
	if store.claimCalls != 1 {
		t.Fatalf("media jobs claimed after close: %d", store.claimCalls)
	}
	if _, err := worker.RunNext(context.Background()); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("closed RunNext error = %v", err)
	}
}

func newTestWorker(
	t *testing.T,
	store *workerStoreStub,
	storage *workerStorageStub,
	runner ProcessRunner,
	overrides Options,
) *Worker {
	t.Helper()
	overrides.Store = store
	overrides.Storage = storage
	overrides.Runner = runner
	overrides.FFmpegPath = "ffmpeg"
	overrides.FFprobePath = "ffprobe"
	overrides.WorkerID = "test-worker"
	overrides.TemporaryRoot = t.TempDir()
	worker, err := New(overrides)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return worker
}

type cleanupEnqueue struct {
	key    string
	reason string
}

type workerStoreStub struct {
	mu                sync.Mutex
	job               *MediaJob
	jobClaimed        bool
	claimCalls        int
	source            *SourceAsset
	cleanup           *CleanupJob
	cleanupClaimed    bool
	control           JobControl
	replaced          []string
	commitErr         error
	commit            *CommitMediaJob
	failed            error
	scheduled         []string
	enqueued          []cleanupEnqueue
	reusable          []GeneratedVariant
	reuseCalls        int
	cleanupReferenced bool
	cleanupCompleted  bool
	cleanupFailed     error
}

func (store *workerStoreStub) ClaimMediaJob(context.Context, string, time.Time, time.Duration) (*MediaJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimCalls++
	if store.jobClaimed {
		return nil, nil
	}
	store.jobClaimed = true
	return store.job, nil
}

func (store *workerStoreStub) RenewMediaLease(context.Context, string, string, string, time.Time, time.Time) (bool, error) {
	return true, nil
}

func (store *workerStoreStub) MediaJobControl(context.Context, string, string, string) (JobControl, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.control.Owned && !store.control.CancelRequested {
		return JobControl{Owned: true}, nil
	}
	return store.control, nil
}

func (store *workerStoreStub) FindReadySourceAsset(context.Context, string) (*SourceAsset, error) {
	return store.source, nil
}

func (store *workerStoreStub) FindReusableVariants(
	_ context.Context, _, _, _ string, _ []AudioVariantProfile,
) ([]GeneratedVariant, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reuseCalls++
	return append([]GeneratedVariant(nil), store.reusable...), nil
}

func (store *workerStoreStub) CommitMediaJob(_ context.Context, input CommitMediaJob) ([]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	copy := input
	store.commit = &copy
	return append([]string(nil), store.replaced...), store.commitErr
}

func (store *workerStoreStub) FailMediaJob(_ context.Context, _ MediaJob, _ string, err error, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failed = err
	return nil
}

func (store *workerStoreStub) ScheduleReplacedAssetCleanup(_ context.Context, ids []string, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.scheduled = append([]string(nil), ids...)
	return nil
}

func (store *workerStoreStub) EnqueueObjectCleanup(_ context.Context, key, reason string, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.enqueued = append(store.enqueued, cleanupEnqueue{key: key, reason: reason})
	return nil
}

func (store *workerStoreStub) ClaimObjectCleanup(context.Context, string, time.Time, time.Duration) (*CleanupJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.cleanupClaimed {
		return nil, nil
	}
	store.cleanupClaimed = true
	return store.cleanup, nil
}

func (store *workerStoreStub) ReadyAssetReferencesObject(context.Context, string) (bool, error) {
	return store.cleanupReferenced, nil
}

func (store *workerStoreStub) CompleteObjectCleanup(context.Context, CleanupJob, string, bool, time.Time) (bool, error) {
	store.cleanupCompleted = true
	return true, nil
}

func (store *workerStoreStub) FailObjectCleanup(_ context.Context, _ CleanupJob, _ string, err error, _ time.Time) error {
	store.cleanupFailed = err
	return nil
}

type workerStorageStub struct {
	mu            sync.Mutex
	source        []byte
	downloadCalls int
	uploaded      []string
	deleted       []string
	deleteErr     error
	uploadErrAt   int
	uploadCalls   int
	objectStats   map[string]objectStat
}

type objectStat struct {
	sizeBytes int64
	checksum  string
	exists    bool
	err       error
}

func (storage *workerStorageStub) DownloadToFile(_ context.Context, _ string, path string, _ int64) (DownloadedObject, error) {
	storage.mu.Lock()
	storage.downloadCalls++
	storage.mu.Unlock()
	if err := os.WriteFile(path, storage.source, 0o600); err != nil {
		return DownloadedObject{}, err
	}
	digest := sha256.Sum256(storage.source)
	return DownloadedObject{SizeBytes: int64(len(storage.source)), ChecksumSHA256: hex.EncodeToString(digest[:])}, nil
}

func (storage *workerStorageStub) UploadFile(_ context.Context, key, path, _, checksum string) (int64, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(value)
	if hex.EncodeToString(digest[:]) != checksum {
		return 0, errors.New("checksum mismatch")
	}
	storage.mu.Lock()
	storage.uploadCalls++
	if storage.uploadErrAt > 0 && storage.uploadCalls == storage.uploadErrAt {
		storage.mu.Unlock()
		return 0, errors.New("post-upload validation failed")
	}
	storage.uploaded = append(storage.uploaded, key)
	storage.mu.Unlock()
	return int64(len(value)), nil
}

func (storage *workerStorageStub) StatObject(_ context.Context, key string) (int64, string, bool, error) {
	stat := storage.objectStats[key]
	return stat.sizeBytes, stat.checksum, stat.exists, stat.err
}

func (storage *workerStorageStub) Delete(_ context.Context, key string) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.deleted = append(storage.deleted, key)
	return storage.deleteErr
}

type workerRunnerStub struct {
	mu                sync.Mutex
	probe             string
	probeCalls        int
	ffmpegArguments   []string
	blockFFmpeg       bool
	ffmpegStarted     chan struct{}
	ffmpegStartedOnce sync.Once
}

type blockingProbeRunner struct {
	mu        sync.Mutex
	active    int
	maxActive int
	started   chan struct{}
	release   chan struct{}
}

func (runner *blockingProbeRunner) Run(ctx context.Context, executable string, _ []string, _ time.Duration) (ProcessResult, error) {
	if executable != "ffprobe" {
		return ProcessResult{}, errors.New("unexpected executable")
	}
	runner.mu.Lock()
	runner.active++
	if runner.active > runner.maxActive {
		runner.maxActive = runner.active
	}
	runner.mu.Unlock()
	runner.started <- struct{}{}
	select {
	case <-runner.release:
	case <-ctx.Done():
		runner.mu.Lock()
		runner.active--
		runner.mu.Unlock()
		return ProcessResult{}, ctx.Err()
	}
	runner.mu.Lock()
	runner.active--
	runner.mu.Unlock()
	return ProcessResult{Stdout: `{"streams":[{"codec_type":"audio","codec_name":"flac","sample_rate":"44100"}],"format":{"duration":"1"}}`}, nil
}

func (runner *blockingProbeRunner) snapshot() (active, maximum int) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.active, runner.maxActive
}

type blockingStatStorage struct {
	mu        sync.Mutex
	active    int
	maxActive int
	started   chan struct{}
	release   chan struct{}
}

func (storage *blockingStatStorage) DownloadToFile(context.Context, string, string, int64) (DownloadedObject, error) {
	return DownloadedObject{}, errors.New("download not used")
}

func (storage *blockingStatStorage) UploadFile(context.Context, string, string, string, string) (int64, error) {
	return 0, errors.New("upload not used")
}

func (storage *blockingStatStorage) StatObject(ctx context.Context, _ string) (int64, string, bool, error) {
	storage.mu.Lock()
	storage.active++
	if storage.active > storage.maxActive {
		storage.maxActive = storage.active
	}
	storage.mu.Unlock()
	storage.started <- struct{}{}
	select {
	case <-storage.release:
	case <-ctx.Done():
		storage.mu.Lock()
		storage.active--
		storage.mu.Unlock()
		return 0, "", false, ctx.Err()
	}
	storage.mu.Lock()
	storage.active--
	storage.mu.Unlock()
	return 42, "checksum", true, nil
}

func (storage *blockingStatStorage) Delete(context.Context, string) error {
	return errors.New("delete not used")
}

func (storage *blockingStatStorage) snapshot() (active, maximum int) {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	return storage.active, storage.maxActive
}

func (runner *workerRunnerStub) Run(ctx context.Context, executable string, arguments []string, _ time.Duration) (ProcessResult, error) {
	if executable == "ffprobe" {
		runner.mu.Lock()
		runner.probeCalls++
		probe := runner.probe
		runner.mu.Unlock()
		return ProcessResult{Stdout: probe}, nil
	}
	runner.mu.Lock()
	runner.ffmpegArguments = append([]string(nil), arguments...)
	runner.mu.Unlock()
	if runner.ffmpegStarted != nil {
		runner.ffmpegStartedOnce.Do(func() { close(runner.ffmpegStarted) })
	}
	if runner.blockFFmpeg {
		<-ctx.Done()
		return ProcessResult{}, context.Cause(ctx)
	}
	for _, argument := range arguments {
		if filepath.Ext(argument) != ".m4a" && filepath.Ext(argument) != ".flac" {
			continue
		}
		if err := os.WriteFile(argument, []byte("variant:"+filepath.Base(argument)), 0o600); err != nil {
			return ProcessResult{}, err
		}
	}
	return ProcessResult{}, nil
}
