package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
		wantPrefix := "media/checkpoints/" + store.job.TrackID + "/"
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

func TestWorkerReusesReadyVariantsBySourceChecksumAndProfileVersion(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	profiles := AudioVariantProfiles("aac")
	store := &workerStoreStub{
		job: &MediaJob{
			ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1,
			AttemptID: &attemptID,
		},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
		reusable: []GeneratedVariant{{
			Profile: profiles[0], ObjectKey: "media/variants/track/old/data_saver.m4a",
			ChecksumSHA256: "variant-checksum", SizeBytes: 42,
			SourceChecksumSHA256: checksum, ProfileVersion: "v7",
		}},
	}
	runner := &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
	}
	storage := &workerStorageStub{
		source: source,
		objectStats: map[string]objectStat{
			"media/variants/track/old/data_saver.m4a": {sizeBytes: 42, checksum: "variant-checksum", exists: true},
		},
	}
	worker := newTestWorker(t, store, storage, runner, Options{ProfileVersion: "v7", FFmpegThreads: 3})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.reuseCalls != 1 || store.commit == nil || len(store.commit.Generated) != len(profiles) {
		t.Fatalf("reuse calls/commit = %d/%#v", store.reuseCalls, store.commit)
	}
	if !store.commit.Generated[0].Reused || store.commit.Generated[0].ObjectKey != "media/variants/track/old/data_saver.m4a" {
		t.Fatalf("reused variant = %#v", store.commit.Generated[0])
	}
	if len(storage.uploaded) != len(profiles)-1 {
		t.Fatalf("uploaded variants = %#v", storage.uploaded)
	}
	joined := strings.Join(runner.ffmpegArguments, " ")
	wantThreads := min(3, max(1, runtime.GOMAXPROCS(0)/2))
	if !strings.Contains(joined, "-threads "+fmt.Sprint(wantThreads)) || strings.Count(joined, "-map 0:a:0 -vn") != len(profiles)-1 {
		t.Fatalf("ffmpeg arguments = %s", joined)
	}
}

func TestWorkerReusesUploadedCheckpointBeforeRunningFFmpeg(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	profiles := AudioVariantProfiles("aac")
	checkpointKey := checkpointVariantObjectKey("track", checksum, "v1", mediaRange{StartMS: 0, EndMS: 1_000}, profiles[0])
	store := &workerStoreStub{
		job: &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	storage := &workerStorageStub{source: source, objectStats: map[string]objectStat{
		checkpointKey: {sizeBytes: 42, checksum: "checkpoint-checksum", exists: true},
	}}
	runner := &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
	}
	worker := newTestWorker(t, store, storage, runner, Options{})
	worked, err := worker.RunNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.commit == nil || len(store.commit.Generated) != len(profiles) ||
		store.commit.Generated[0].ObjectKey != checkpointKey || store.commit.Generated[0].Reused {
		t.Fatalf("checkpoint commit = %#v", store.commit)
	}
	if len(storage.uploaded) != len(profiles)-1 || strings.Count(strings.Join(runner.ffmpegArguments, " "), "-map 0:a:0 -vn") != len(profiles)-1 {
		t.Fatalf("checkpoint reuse uploads=%#v ffmpeg=%v", storage.uploaded, runner.ffmpegArguments)
	}
}

func TestVerifyReusableVariantsDropsMissingOrChangedObjects(t *testing.T) {
	checksum := "present-checksum"
	variants := []GeneratedVariant{
		{ObjectKey: "present", SizeBytes: 10, ChecksumSHA256: checksum},
		{ObjectKey: "missing", SizeBytes: 10, ChecksumSHA256: checksum},
		{ObjectKey: "wrong-size", SizeBytes: 10, ChecksumSHA256: checksum},
		{ObjectKey: "wrong-checksum", SizeBytes: 10, ChecksumSHA256: checksum},
	}
	verifier := &workerStorageStub{objectStats: map[string]objectStat{
		"present":        {sizeBytes: 10, checksum: checksum, exists: true},
		"wrong-size":     {sizeBytes: 9, checksum: checksum, exists: true},
		"wrong-checksum": {sizeBytes: 10, checksum: "other-checksum", exists: true},
	}}
	verified, err := verifyReusableVariants(context.Background(), verifier, variants)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified) != 1 || verified[0].ObjectKey != "present" {
		t.Fatalf("verified variants = %#v", verified)
	}
}

func TestWorkerClampsConfiguredFFmpegThreadsToCPUShare(t *testing.T) {
	worker := newTestWorker(t, &workerStoreStub{}, &workerStorageStub{}, &workerRunnerStub{}, Options{
		Workers: 4, FFmpegThreads: 64,
	})
	want := max(1, runtime.GOMAXPROCS(0)/4)
	if worker.ffmpegThreads != want {
		t.Fatalf("ffmpeg threads = %d, want %d", worker.ffmpegThreads, want)
	}
}

func TestWorkerDrainWaitsForClaimedJobAndRejectsNewClaims(t *testing.T) {
	attemptID := "attempt"
	source := []byte("source")
	digest := sha256.Sum256(source)
	checksum := hex.EncodeToString(digest[:])
	store := &workerStoreStub{
		job: &MediaJob{ID: "job", SourceAssetID: "asset", TrackID: "track", Generation: 1, AttemptID: &attemptID},
		source: &SourceAsset{ID: "asset", ObjectKey: "source", SizeBytes: int64(len(source)), ChecksumSHA256: &checksum},
	}
	runner := &workerRunnerStub{
		probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"1"}}`,
		blockFFmpeg: true, ffmpegStarted: make(chan struct{}), releaseFFmpeg: make(chan struct{}),
	}
	worker := newTestWorker(t, store, &workerStorageStub{source: source}, runner, Options{})
	finished := make(chan error, 1)
	go func() {
		_, err := worker.RunNext(context.Background())
		finished <- err
	}()
	select {
	case <-runner.ffmpegStarted:
	case <-time.After(time.Second):
		t.Fatal("ffmpeg did not start")
	}
	if _, err := worker.RunNext(context.Background()); err != nil && !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("claim while draining before drain = %v", err)
	}
	drainDone := make(chan error, 1)
	go func() { drainDone <- worker.Drain(context.Background()) }()
	select {
	case err := <-drainDone:
		t.Fatalf("drain returned before active job finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := worker.RunNext(context.Background()); !errors.Is(err, ErrWorkerClosed) {
		t.Fatalf("claim while draining error = %v", err)
	}
	close(runner.releaseFFmpeg)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("drained job error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drained job did not finish")
	}
	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("drain error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not finish")
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
	if len(store.enqueued) != 0 {
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
	if len(store.enqueued) == 0 {
		t.Fatalf("abandoned objects = %#v", store.enqueued)
	}
	for _, cleanup := range store.enqueued {
		if !strings.HasPrefix(cleanup.key, "media/checkpoints/track/") ||
			cleanup.reason != "ABANDONED_MEDIA_ATTEMPT" {
			t.Fatalf("abandoned object cleanup = %#v", cleanup)
		}
	}
}

func TestUploadVariantsUsesBoundedParallelUploads(t *testing.T) {
	storage := &parallelUploadStorage{
		started: make(chan struct{}, 4),
		release: make(chan struct{}),
	}
	worker := &Worker{storage: storage, uploadSemaphore: make(chan struct{}, 2)}
	directory := t.TempDir()
	planned := make([]plannedVariant, 0, 4)
	for index := 0; index < 4; index++ {
		path := filepath.Join(directory, fmt.Sprintf("variant-%d.m4a", index))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("variant-%d", index)), 0o600); err != nil {
			t.Fatal(err)
		}
		planned = append(planned, plannedVariant{
			Profile: AudioVariantProfile{Quality: fmt.Sprintf("Q%d", index), Extension: "m4a", MIMEType: "audio/mp4"},
			Path:    path, ObjectKey: fmt.Sprintf("variant/%d", index),
		})
	}
	type uploadResult struct {
		variants []GeneratedVariant
		err      error
	}
	resultChannel := make(chan uploadResult, 1)
	go func() {
		variants, _, err := worker.uploadVariants(context.Background(), planned, "source-checksum")
		resultChannel <- uploadResult{variants: variants, err: err}
	}()
	for index := 0; index < 2; index++ {
		select {
		case <-storage.started:
		case <-time.After(time.Second):
			t.Fatal("upload worker did not start in parallel")
		}
	}
	storage.mu.Lock()
	maximum := storage.maximumInFlight
	storage.mu.Unlock()
	if maximum != 2 {
		t.Fatalf("maximum upload concurrency = %d, want 2", maximum)
	}
	close(storage.release)
	select {
	case result := <-resultChannel:
		if result.err != nil || len(result.variants) != len(planned) {
			t.Fatalf("upload result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("parallel uploads did not finish")
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
	runner *workerRunnerStub,
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
	reuseErr          error
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
	_ context.Context,
	_, _, _ string,
	_ []AudioVariantProfile,
) ([]GeneratedVariant, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reuseCalls++
	if store.reuseErr != nil {
		return nil, store.reuseErr
	}
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
	mu          sync.Mutex
	source      []byte
	uploaded    []string
	deleted     []string
	deleteErr   error
	uploadErrAt int
	uploadCalls int
	objectStats map[string]objectStat
}

type objectStat struct {
	sizeBytes int64
	checksum  string
	exists    bool
	err       error
}

type parallelUploadStorage struct {
	mu              sync.Mutex
	started         chan struct{}
	release         chan struct{}
	inFlight        int
	maximumInFlight int
}

func (storage *parallelUploadStorage) DownloadToFile(context.Context, string, string, int64) (DownloadedObject, error) {
	return DownloadedObject{}, errors.New("not implemented")
}

func (storage *parallelUploadStorage) UploadFile(_ context.Context, _ string, path, _, _ string) (int64, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	storage.mu.Lock()
	storage.inFlight++
	storage.maximumInFlight = max(storage.maximumInFlight, storage.inFlight)
	storage.mu.Unlock()
	storage.started <- struct{}{}
	<-storage.release
	storage.mu.Lock()
	storage.inFlight--
	storage.mu.Unlock()
	return int64(len(value)), nil
}

func (storage *parallelUploadStorage) Delete(context.Context, string) error { return nil }

func (storage *parallelUploadStorage) StatObject(context.Context, string) (int64, string, bool, error) {
	return 0, "", false, nil
}

func (storage *workerStorageStub) DownloadToFile(_ context.Context, _ string, path string, _ int64) (DownloadedObject, error) {
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

func (storage *workerStorageStub) Delete(_ context.Context, key string) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.deleted = append(storage.deleted, key)
	return storage.deleteErr
}

func (storage *workerStorageStub) StatObject(_ context.Context, key string) (int64, string, bool, error) {
	stat := storage.objectStats[key]
	return stat.sizeBytes, stat.checksum, stat.exists, stat.err
}

type workerRunnerStub struct {
	mu                sync.Mutex
	probe             string
	ffmpegArguments   []string
	blockFFmpeg       bool
	ffmpegStarted     chan struct{}
	ffmpegStartedOnce sync.Once
	releaseFFmpeg     chan struct{}
}

func (runner *workerRunnerStub) Run(ctx context.Context, executable string, arguments []string, _ time.Duration) (ProcessResult, error) {
	if executable == "ffprobe" {
		return ProcessResult{Stdout: runner.probe}, nil
	}
	runner.mu.Lock()
	runner.ffmpegArguments = append([]string(nil), arguments...)
	runner.mu.Unlock()
	if runner.ffmpegStarted != nil {
		runner.ffmpegStartedOnce.Do(func() { close(runner.ffmpegStarted) })
	}
	if runner.blockFFmpeg {
		if runner.releaseFFmpeg == nil {
			<-ctx.Done()
			return ProcessResult{}, context.Cause(ctx)
		}
		select {
		case <-ctx.Done():
			return ProcessResult{}, context.Cause(ctx)
		case <-runner.releaseFFmpeg:
		}
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
