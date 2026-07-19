package adminmetadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWritebackWorkerReplacesSourceWithoutPersistentBackup(t *testing.T) {
	worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
	legacyBackup := filepath.Join(filepath.Dir(sourcePath), ".legacy-writeback.bak")
	if err := os.WriteFile(legacyBackup, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}

	worked, err := worker.RunNext(context.Background(), "worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if !worked || store.commit == nil || store.failErr != nil || store.job.Status != WritebackReady {
		t.Fatalf("worked=%v commit=%+v fail=%v job=%+v", worked, store.commit, store.failErr, store.job)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != "remuxed-audio" {
		t.Fatalf("source=%q err=%v", content, err)
	}
	if store.commit.OriginalSHA256 != checksumBytes(original) ||
		store.commit.OutputSHA256 == store.commit.OriginalSHA256 {
		t.Fatalf("commit=%+v", store.commit)
	}
	for _, transient := range []string{paths.Temporary, paths.Rollback} {
		if _, err := os.Stat(transient); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transient path remains: %s err=%v", transient, err)
		}
	}
	if content, err := os.ReadFile(legacyBackup); err != nil || string(content) != "legacy" {
		t.Fatalf("legacy backup was touched: content=%q err=%v", content, err)
	}
	if strings.Contains(strings.ToLower(paths.Rollback), ".bak") || !strings.HasSuffix(paths.Rollback, ".tmp") {
		t.Fatalf("rollback path must be temporary: %s", paths.Rollback)
	}
}

func TestWritebackWorkerRestoresOriginalWhenCommitFails(t *testing.T) {
	worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
	store.commitErr = errors.New("database commit unavailable")

	worked, err := worker.RunNext(context.Background(), "worker-test")
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.failErr != nil || store.commit == nil || store.transientCompletions != 1 ||
		store.job.Status != WritebackPending {
		t.Fatalf("commit=%+v failure=%v completions=%d job=%+v",
			store.commit, store.failErr, store.transientCompletions, store.job)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != string(original) {
		t.Fatalf("source=%q err=%v", content, err)
	}
	for _, transient := range []string{paths.Temporary, paths.Rollback} {
		if _, err := os.Stat(transient); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transient path remains after rollback: %s err=%v", transient, err)
		}
	}
}

func TestWritebackWorkerRecoversExpiredTransientAttempts(t *testing.T) {
	for _, test := range []struct {
		name     string
		stage    WritebackStage
		replaced bool
	}{
		{name: "prepared before rename", stage: StagePrepared},
		{name: "prepared after rename", stage: StagePrepared, replaced: true},
		{name: "file replaced", stage: StageFileReplaced, replaced: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
			arrangeTransientCrash(t, store, sourcePath, original, paths, test.stage, test.replaced)
			legacyBackup := filepath.Join(filepath.Dir(sourcePath), ".legacy.bak")
			if err := os.WriteFile(legacyBackup, []byte("untouched"), 0o600); err != nil {
				t.Fatal(err)
			}

			worked, err := worker.RunNext(context.Background(), "recovery-worker")
			if err != nil || !worked {
				t.Fatalf("worked=%v err=%v", worked, err)
			}
			if store.transientCompletions != 1 || store.job.Status != WritebackPending ||
				store.job.AttemptID != nil || store.job.Stage != StageQueued {
				t.Fatalf("completions=%d job=%+v", store.transientCompletions, store.job)
			}
			if content, err := os.ReadFile(sourcePath); err != nil || string(content) != string(original) {
				t.Fatalf("source=%q err=%v", content, err)
			}
			for _, transient := range []string{paths.Temporary, paths.Rollback} {
				if _, err := os.Stat(transient); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("transient remains: %s err=%v", transient, err)
				}
			}
			if content, err := os.ReadFile(legacyBackup); err != nil || string(content) != "untouched" {
				t.Fatalf("legacy backup changed: content=%q err=%v", content, err)
			}
		})
	}
}

func TestRecoveredTransientAttemptHonorsCancelAndMaxAttempts(t *testing.T) {
	for _, test := range []struct {
		name       string
		cancelled  bool
		exhausted  bool
		wantStatus WritebackStatus
	}{
		{name: "cancel requested", cancelled: true, wantStatus: WritebackCancelled},
		{name: "attempts exhausted", exhausted: true, wantStatus: WritebackFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
			arrangeTransientCrash(t, store, sourcePath, original, paths, StageFileReplaced, true)
			store.job.CancelRequested = test.cancelled
			if test.exhausted {
				store.job.Attempts = store.job.MaxAttempts
			}
			store.contextRecord.Job = store.job

			worked, err := worker.RunNext(context.Background(), "recovery-worker")
			if err != nil || !worked {
				t.Fatalf("worked=%v err=%v", worked, err)
			}
			if store.job.Status != test.wantStatus || store.transientCompletions != 1 {
				t.Fatalf("job=%+v completions=%d", store.job, store.transientCompletions)
			}
			if content, err := os.ReadFile(sourcePath); err != nil || string(content) != string(original) {
				t.Fatalf("source=%q err=%v", content, err)
			}
		})
	}
}

func TestCommitAndStateLookupFailureRecoversOnNextLease(t *testing.T) {
	worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
	legacyBackup := filepath.Join(filepath.Dir(sourcePath), ".legacy.bak")
	if err := os.WriteFile(legacyBackup, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.commitErr = errors.New("commit temporarily unavailable")
	store.findErr = errors.New("state lookup temporarily unavailable")
	store.failStoreErr = errors.New("failure persistence temporarily unavailable")

	worked, err := worker.RunNext(context.Background(), "worker-test")
	if !worked || !errors.Is(err, store.failStoreErr) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.job.Stage != StageFileReplaced || store.job.AttemptID == nil {
		t.Fatalf("recovery marker lost: %+v", store.job)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != "remuxed-audio" {
		t.Fatalf("source=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(paths.Rollback); err != nil || string(content) != string(original) {
		t.Fatalf("rollback=%q err=%v", content, err)
	}

	store.commitErr, store.findErr, store.failStoreErr = nil, nil, nil
	store.job.LockedBy = nil
	store.contextRecord.Job = store.job
	worked, err = worker.RunNext(context.Background(), "replacement-worker")
	if err != nil || !worked {
		t.Fatalf("replacement worked=%v err=%v", worked, err)
	}
	if store.job.Status != WritebackPending || store.transientCompletions != 1 {
		t.Fatalf("job=%+v completions=%d", store.job, store.transientCompletions)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != string(original) {
		t.Fatalf("restored source=%q err=%v", content, err)
	}
	if _, err := os.Stat(paths.Rollback); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback remains: %v", err)
	}
	if content, err := os.ReadFile(legacyBackup); err != nil || string(content) != "untouched" {
		t.Fatalf("legacy backup changed: content=%q err=%v", content, err)
	}
}

func TestCommittedWritebackCleanupContinuesOnNextLease(t *testing.T) {
	worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
	store.commitErr = errors.New("commit response lost")
	store.commitApplies = true
	store.findErr = errors.New("state lookup temporarily unavailable")
	store.failStoreErr = errors.New("failure persistence temporarily unavailable")

	worked, err := worker.RunNext(context.Background(), "worker-test")
	if !worked || !errors.Is(err, store.failStoreErr) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.job.Status != WritebackProcessing || store.job.Stage != StageCommitted ||
		store.job.AttemptID == nil {
		t.Fatalf("committed marker lost: %+v", store.job)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != "remuxed-audio" {
		t.Fatalf("source=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(paths.Rollback); err != nil || string(content) != string(original) {
		t.Fatalf("rollback=%q err=%v", content, err)
	}

	store.commitErr, store.findErr, store.failStoreErr = nil, nil, nil
	store.job.LockedBy = nil
	store.contextRecord.Job = store.job
	worked, err = worker.RunNext(context.Background(), "cleanup-worker")
	if err != nil || !worked {
		t.Fatalf("cleanup worked=%v err=%v", worked, err)
	}
	if store.job.Status != WritebackReady || store.committedCompletions != 1 ||
		store.job.AttemptID != nil {
		t.Fatalf("job=%+v completions=%d", store.job, store.committedCompletions)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != "remuxed-audio" {
		t.Fatalf("committed source changed: %q err=%v", content, err)
	}
	if _, err := os.Stat(paths.Rollback); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback remains: %v", err)
	}
}

func TestOldWorkerDoesNotRestoreAfterLeaseTakeover(t *testing.T) {
	worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
	store.commitErr = errors.New("commit unavailable")
	store.onCommit = func(store *workerStoreStub) {
		newOwner := "replacement-worker"
		store.job.LockedBy = &newOwner
		store.contextRecord.Job = store.job
	}

	worked, err := worker.RunNext(context.Background(), "worker-test")
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.transientCompletions != 0 || store.job.LockedBy == nil ||
		*store.job.LockedBy != "replacement-worker" {
		t.Fatalf("ownership/completions=%v/%d", store.job.LockedBy, store.transientCompletions)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != "remuxed-audio" {
		t.Fatalf("source=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(paths.Rollback); err != nil || string(content) != string(original) {
		t.Fatalf("rollback=%q err=%v", content, err)
	}
}

func TestCommittedRollbackCleanupFailureRemainsClaimable(t *testing.T) {
	worker, store, sourcePath, original, paths := newWritebackWorkerFixture(t)
	arrangeCommittedCrash(t, store, sourcePath, paths)
	if err := os.Mkdir(paths.Rollback, 0o700); err != nil {
		t.Fatal(err)
	}

	worked, err := worker.RunNext(context.Background(), "cleanup-worker")
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.job.Status != WritebackProcessing || store.job.Stage != StageCommitted ||
		store.job.AttemptID == nil || store.committedReleases != 1 {
		t.Fatalf("job=%+v releases=%d", store.job, store.committedReleases)
	}
	if writebackErrorCode(store.committedReleaseErr) != "ROLLBACK_FAILED" {
		t.Fatalf("release error=%v", store.committedReleaseErr)
	}
	if err := os.Remove(paths.Rollback); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Rollback, original, 0o600); err != nil {
		t.Fatal(err)
	}
	worked, err = worker.RunNext(context.Background(), "cleanup-worker-2")
	if err != nil || !worked {
		t.Fatalf("retry worked=%v err=%v", worked, err)
	}
	if store.job.Status != WritebackReady || store.committedCompletions != 1 {
		t.Fatalf("job=%+v completions=%d", store.job, store.committedCompletions)
	}
}

func TestCommittedCleanupWithoutRollbackStillVerifiesSource(t *testing.T) {
	worker, store, sourcePath, _, paths := newWritebackWorkerFixture(t)
	arrangeCommittedCrash(t, store, sourcePath, paths)
	if err := os.WriteFile(sourcePath, []byte("external-change"), 0o600); err != nil {
		t.Fatal(err)
	}

	worked, err := worker.RunNext(context.Background(), "cleanup-worker")
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if store.job.Status != WritebackProcessing || store.committedCompletions != 0 ||
		store.committedReleases != 1 || writebackErrorCode(store.committedReleaseErr) != "ROLLBACK_FAILED" {
		t.Fatalf("job=%+v completions=%d releases=%d error=%v",
			store.job, store.committedCompletions, store.committedReleases, store.committedReleaseErr)
	}
	if err := os.WriteFile(sourcePath, []byte("remuxed-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	worked, err = worker.RunNext(context.Background(), "cleanup-worker-2")
	if err != nil || !worked || store.job.Status != WritebackReady {
		t.Fatalf("retry worked=%v err=%v job=%+v", worked, err, store.job)
	}
}

func TestRestoreReplacementRollbackDoesNotOverwriteExternalChange(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "song.flac")
	temporary := filepath.Join(root, "output.tmp")
	rollback := filepath.Join(root, "rollback.tmp")
	original := []byte("original")
	output := []byte("output")
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, output, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceSourceFile(source, temporary, rollback, checksumBytes(original)); err != nil {
		t.Fatal(err)
	}
	external := []byte("external-change")
	if err := os.WriteFile(source, external, 0o600); err != nil {
		t.Fatal(err)
	}
	err := restoreReplacementRollback(
		source, temporary, rollback, checksumBytes(original), checksumBytes(output),
	)
	if writebackErrorCode(err) != "ROLLBACK_FAILED" {
		t.Fatalf("error=%v", err)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != string(external) {
		t.Fatalf("source=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(rollback); err != nil || string(content) != string(original) {
		t.Fatalf("rollback=%q err=%v", content, err)
	}
}

func TestSafeSourcePathRejectsTraversalAndSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-metadata.flac")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, err := safeSourcePath(root, "../outside-metadata.flac"); writebackErrorCode(err) != "UNSAFE_SOURCE_PATH" {
		t.Fatalf("traversal error=%v", err)
	}
	link := filepath.Join(root, "link.flac")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := safeSourcePath(root, "link.flac"); writebackErrorCode(err) != "UNSAFE_SOURCE_PATH" {
		t.Fatalf("symlink error=%v", err)
	}
}

func TestReplaceSourceFileFencesChangeAtRenameBoundary(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "song.flac")
	temporary := filepath.Join(root, "output.tmp")
	rollback := filepath.Join(root, "rollback.tmp")
	external := []byte("external-change")
	if err := os.WriteFile(source, external, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("output"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := replaceSourceFile(source, temporary, rollback, checksumBytes([]byte("original")))
	if writebackErrorCode(err) != "SOURCE_CHANGED" {
		t.Fatalf("error=%v", err)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != string(external) {
		t.Fatalf("source=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(temporary); err != nil || string(content) != "output" {
		t.Fatalf("temporary=%q err=%v", content, err)
	}
}

func TestAssertWritebackPathSnapshotRejectsCatalogMove(t *testing.T) {
	err := assertWritebackPathSnapshot(WritebackContext{
		Job:      WritebackJob{RootPathSnapshot: `D:\\music`, SourcePathSnapshot: `old\\song.flac`},
		RootPath: `D:\\music`,
		Source:   MetadataSourceRecord{SourcePath: `new\\song.flac`},
	})
	if writebackErrorCode(err) != "SOURCE_PATH_CHANGED" {
		t.Fatalf("error=%v", err)
	}
}

func TestWritebackHeartbeatCancelsOwnedJobPromptly(t *testing.T) {
	store := &workerStoreStub{}
	store.cancellationRequested.Store(true)
	worker := &WritebackWorker{store: store, logger: NoopLogger{}}
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		worker.heartbeat(ctx, cancel, done, "job", "worker", "attempt", true)
		close(heartbeatDone)
	}()
	select {
	case <-heartbeatDone:
	case <-time.After(writebackCancellationPoll + time.Second):
		close(done)
		t.Fatal("heartbeat did not observe cancellation promptly")
	}
	if writebackErrorCode(context.Cause(ctx)) != "WRITEBACK_CANCELLED" {
		t.Fatalf("cancellation cause=%v", context.Cause(ctx))
	}
	if store.cancellationChecks.Load() == 0 {
		t.Fatal("heartbeat did not query cancellation state")
	}
}

func newWritebackWorkerFixture(
	t *testing.T,
) (*WritebackWorker, *workerStoreStub, string, []byte, WritebackPathSet) {
	t.Helper()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "song.flac")
	original := []byte("original-audio")
	if err := os.WriteFile(sourcePath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	metadata, err := NormalizeMetadataSnapshot(validSnapshotValue())
	if err != nil {
		t.Fatal(err)
	}
	metadataJSON, _ := json.Marshal(metadata)
	attemptID := "00000000-0000-0000-0000-000000000010"
	jobID := "00000000-0000-0000-0000-000000000011"
	requestedBy := "00000000-0000-0000-0000-000000000012"
	workerID := "worker-test"
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	job := WritebackJob{
		ID: jobID, TrackID: "00000000-0000-0000-0000-000000000013",
		SourceID: "00000000-0000-0000-0000-000000000014", RequestedBy: &requestedBy,
		Reason: "test", MetadataSnapshot: metadataJSON, MetadataVersion: 1,
		ExpectedSourceChecksum: checksumBytes(original), Status: WritebackProcessing,
		RootPathSnapshot: root, SourcePathSnapshot: "song.flac",
		Attempts: 1, MaxAttempts: 3, Version: 2, AttemptID: &attemptID,
		Stage: StagePreparing, LockedBy: &workerID, NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	store := &workerStoreStub{
		job: job,
		contextRecord: WritebackContext{
			Job: job,
			Metadata: MetadataRecord{
				TrackID: job.TrackID, SourceID: &job.SourceID, Raw: metadataJSON,
				Overrides: json.RawMessage(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
			},
			Source: MetadataSourceRecord{
				ID: job.SourceID, SourcePath: "song.flac", Status: "READY",
				ChecksumSHA256: checksumBytes(original),
			},
			RootPath: root, RootMode: "READ_WRITE", Enabled: true, Status: "READY",
		},
	}
	worker, err := NewWritebackWorker(WorkerDependencies{
		Store: store, FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
		Runner: &workerRunnerStub{probeJSON: workerProbeJSON(t), output: []byte("remuxed-audio")},
		Clock:  fixedMetadataClock{now: now}, Logger: NoopLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker, store, sourcePath, original, WritebackPaths(sourcePath, jobID, attemptID)
}

func arrangeTransientCrash(
	t *testing.T,
	store *workerStoreStub,
	sourcePath string,
	original []byte,
	paths WritebackPathSet,
	stage WritebackStage,
	replaced bool,
) {
	t.Helper()
	output := []byte("remuxed-audio")
	outputChecksum := checksumBytes(output)
	store.job.Status = WritebackProcessing
	store.job.Stage = stage
	store.job.OutputChecksumSHA256 = &outputChecksum
	store.job.LockedBy = nil
	if replaced {
		if err := os.WriteFile(paths.Rollback, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sourcePath, output, 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(paths.Temporary, output, 0o600); err != nil {
		t.Fatal(err)
	}
	store.contextRecord.Job = store.job
}

func arrangeCommittedCrash(
	t *testing.T,
	store *workerStoreStub,
	sourcePath string,
	paths WritebackPathSet,
) {
	t.Helper()
	output := []byte("remuxed-audio")
	outputChecksum := checksumBytes(output)
	store.job.Status = WritebackProcessing
	store.job.Stage = StageCommitted
	store.job.OutputChecksumSHA256 = &outputChecksum
	store.job.LockedBy = nil
	if err := os.WriteFile(sourcePath, output, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(paths.Rollback)
	store.contextRecord.Job = store.job
}

func checksumBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

type workerRunnerStub struct {
	probeJSON string
	output    []byte
}

func (runner *workerRunnerStub) Run(
	_ context.Context,
	executable string,
	arguments []string,
	_ time.Duration,
) (ProcessResult, error) {
	switch executable {
	case "ffprobe":
		return ProcessResult{Stdout: runner.probeJSON}, nil
	case "ffmpeg":
		if len(arguments) == 0 {
			return ProcessResult{}, errors.New("ffmpeg output is missing")
		}
		if err := os.WriteFile(arguments[len(arguments)-1], runner.output, 0o600); err != nil {
			return ProcessResult{}, err
		}
		return ProcessResult{}, nil
	default:
		return ProcessResult{}, errors.New("unexpected executable")
	}
}

func workerProbeJSON(t *testing.T) string {
	t.Helper()
	output := ProbeOutput{
		Format: &struct {
			Duration string         `json:"duration"`
			Tags     map[string]any `json:"tags"`
		}{Duration: "1", Tags: map[string]any{"title": "Song", "artist": "Artist"}},
		Streams: []ProbeStream{{Index: intPointer(0), CodecType: "audio", CodecName: "flac", SampleRate: "44100", Channels: intPointer(2)}},
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type fixedMetadataClock struct{ now time.Time }

func (clock fixedMetadataClock) Now() time.Time { return clock.now }

type workerStoreStub struct {
	job           WritebackJob
	contextRecord WritebackContext
	commit        *WritebackCommit
	commitErr     error
	commitApplies bool
	findErr       error
	failStoreErr  error
	failErr       error
	onCommit      func(*workerStoreStub)

	transientCompletions int
	transientReleases    int
	committedCompletions int
	committedReleases    int
	committedReleaseErr  error

	cancellationRequested atomic.Bool
	cancellationChecks    atomic.Int32
}

func (store *workerStoreStub) FindWriteback(context.Context, string) (WritebackJob, error) {
	if store.findErr != nil {
		return WritebackJob{}, store.findErr
	}
	return store.job, nil
}

func (store *workerStoreStub) ClaimWriteback(_ context.Context, workerID string, _ time.Duration) (*WritebackJob, error) {
	if store.job.Status == WritebackProcessing {
		store.job.LockedBy = &workerID
		store.contextRecord.Job = store.job
	}
	copy := store.job
	return &copy, nil
}

func (store *workerStoreStub) LoadWritebackContext(context.Context, string, string, string) (WritebackContext, error) {
	store.contextRecord.Job = store.job
	return store.contextRecord, nil
}

func (store *workerStoreStub) RenewWritebackLease(_ context.Context, _, workerID, attemptID string, _ time.Duration) error {
	if store.job.Status != WritebackProcessing || store.job.LockedBy == nil ||
		*store.job.LockedBy != workerID || store.job.AttemptID == nil || *store.job.AttemptID != attemptID {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "lease lost")
	}
	return nil
}

func (store *workerStoreStub) WritebackCancellationRequested(context.Context, string, string, string) (bool, error) {
	store.cancellationChecks.Add(1)
	return store.cancellationRequested.Load(), nil
}

func (store *workerStoreStub) MarkWritebackPrepared(_ context.Context, _, _, _, checksum string) error {
	store.job.Stage = StagePrepared
	store.job.OutputChecksumSHA256 = &checksum
	store.contextRecord.Job = store.job
	return nil
}

func (store *workerStoreStub) MarkWritebackFileReplaced(_ context.Context, _, _, _, checksum string) error {
	store.job.Stage = StageFileReplaced
	store.job.OutputChecksumSHA256 = &checksum
	store.contextRecord.Job = store.job
	return nil
}

func (store *workerStoreStub) CompleteTransientRollback(context.Context, string, string, string) error {
	store.transientCompletions++
	store.job.LockedBy = nil
	store.job.AttemptID = nil
	store.job.Stage = StageQueued
	store.job.OutputChecksumSHA256 = nil
	if store.job.CancelRequested {
		store.job.Status = WritebackCancelled
	} else if store.job.Attempts >= store.job.MaxAttempts {
		store.job.Status = WritebackFailed
	} else {
		store.job.Status = WritebackPending
	}
	store.contextRecord.Job = store.job
	return nil
}

func (store *workerStoreStub) ReleaseTransientRollback(context.Context, string, string, string, error, time.Duration) error {
	store.transientReleases++
	store.job.LockedBy = nil
	store.contextRecord.Job = store.job
	return nil
}

func (store *workerStoreStub) CommitWriteback(_ context.Context, input WritebackCommit) error {
	store.commit = &input
	if store.onCommit != nil {
		store.onCommit(store)
	}
	if store.commitErr != nil && !store.commitApplies {
		return store.commitErr
	}
	store.job.Stage = StageCommitted
	store.job.OutputChecksumSHA256 = &input.OutputSHA256
	store.contextRecord.Job = store.job
	if store.commitErr != nil {
		return store.commitErr
	}
	return nil
}

func (store *workerStoreStub) CompleteCommittedRollback(context.Context, string, string, string) error {
	store.committedCompletions++
	store.job.Status = WritebackReady
	store.job.LockedBy = nil
	store.job.AttemptID = nil
	store.contextRecord.Job = store.job
	return nil
}

func (store *workerStoreStub) ReleaseCommittedRollback(_ context.Context, _, _, _ string, processErr error, _ time.Duration) error {
	store.committedReleases++
	store.committedReleaseErr = processErr
	store.job.LockedBy = nil
	store.contextRecord.Job = store.job
	return nil
}

func (store *workerStoreStub) FailWriteback(_ context.Context, _, workerID, attemptID string, err error, _ time.Time) error {
	if store.failStoreErr != nil {
		return store.failStoreErr
	}
	if store.job.LockedBy == nil || *store.job.LockedBy != workerID ||
		store.job.AttemptID == nil || *store.job.AttemptID != attemptID {
		return nil
	}
	store.failErr = err
	if writebackNeedsReconciliation(store.job) {
		store.job.LockedBy = nil
		store.contextRecord.Job = store.job
		return nil
	}
	store.job.Status = WritebackPending
	store.job.Stage = StageQueued
	store.job.OutputChecksumSHA256 = nil
	return nil
}
