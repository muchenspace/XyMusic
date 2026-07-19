package adminmetadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	writebackLease              = 120 * time.Second
	writebackHeartbeat          = 30 * time.Second
	writebackCancellationPoll   = 2 * time.Second
	defaultWritebackIdleDelay   = time.Second
	writebackFailureSaveTimeout = 15 * time.Second
	maximumEmbeddedArtworkBytes = 20 << 20
)

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type NoopLogger struct{}

func (NoopLogger) Info(string, map[string]any)  {}
func (NoopLogger) Warn(string, map[string]any)  {}
func (NoopLogger) Error(string, map[string]any) {}

type WorkerDependencies struct {
	Store       WorkerStore
	FFmpegPath  string
	FFprobePath string
	Artwork     ArtworkDownloader
	Runner      ProcessRunner
	Logger      Logger
	Clock       Clock
}

type WritebackWorker struct {
	store       WorkerStore
	ffmpegPath  string
	ffprobePath string
	artwork     ArtworkDownloader
	runner      ProcessRunner
	logger      Logger
	clock       Clock
}

func NewWritebackWorker(dependencies WorkerDependencies) (*WritebackWorker, error) {
	if dependencies.Store == nil {
		return nil, errors.New("admin metadata worker store is required")
	}
	if strings.TrimSpace(dependencies.FFmpegPath) == "" {
		return nil, errors.New("admin metadata worker ffmpeg path is required")
	}
	if strings.TrimSpace(dependencies.FFprobePath) == "" {
		return nil, errors.New("admin metadata worker ffprobe path is required")
	}
	if dependencies.Runner == nil {
		dependencies.Runner = OSProcessRunner{}
	}
	if dependencies.Logger == nil {
		dependencies.Logger = NoopLogger{}
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	return &WritebackWorker{
		store: dependencies.Store, ffmpegPath: dependencies.FFmpegPath,
		ffprobePath: dependencies.FFprobePath, runner: dependencies.Runner,
		artwork: dependencies.Artwork,
		logger:  dependencies.Logger, clock: dependencies.Clock,
	}, nil
}

func (worker *WritebackWorker) Run(ctx context.Context, workerID string, idleDelay time.Duration) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("metadata writeback worker id is required")
	}
	if idleDelay <= 0 {
		idleDelay = defaultWritebackIdleDelay
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		worked, err := worker.RunNext(ctx, workerID)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		timer := time.NewTimer(idleDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (worker *WritebackWorker) RunNext(ctx context.Context, workerID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, nil
	}
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("metadata writeback worker id is required")
	}
	job, err := worker.store.ClaimWriteback(ctx, workerID, writebackLease)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	if job.Status == WritebackCancelled {
		return true, nil
	}
	if job.AttemptID == nil {
		return true, NewWritebackError("WRITEBACK_LEASE_LOST", "Claimed writeback has no attempt id")
	}
	attemptID := *job.AttemptID
	recovering := writebackNeedsReconciliation(*job)
	processContext, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancel(NewWritebackError("WORKER_STOPPED", "Metadata writeback worker is stopping"))
		case <-done:
		}
	}()
	go worker.heartbeat(processContext, cancel, done, job.ID, workerID, attemptID, !recovering)
	processErr := worker.process(processContext, job.ID, workerID, attemptID)
	close(done)
	cancel(nil)
	if processErr == nil {
		worker.logger.Info("metadata.writeback.completed", map[string]any{
			"jobId": job.ID, "trackId": job.TrackID,
		})
		return true, nil
	}
	failureContext, failureCancel := context.WithTimeout(context.WithoutCancel(ctx), writebackFailureSaveTimeout)
	var failErr error
	if job.Stage == StageCommitted {
		failErr = worker.store.ReleaseCommittedRollback(
			failureContext, job.ID, workerID, attemptID, processErr, time.Minute,
		)
	} else if recovering {
		failErr = worker.store.ReleaseTransientRollback(
			failureContext, job.ID, workerID, attemptID, processErr, time.Minute,
		)
	} else {
		failErr = worker.store.FailWriteback(
			failureContext, job.ID, workerID, attemptID, processErr, worker.clock.Now(),
		)
	}
	failureCancel()
	worker.logger.Warn("metadata.writeback.failed", map[string]any{
		"jobId": job.ID, "trackId": job.TrackID,
		"code": writebackErrorCode(processErr), "message": safeWritebackError(processErr),
	})
	if failErr != nil {
		return true, failErr
	}
	return true, nil
}

func (worker *WritebackWorker) heartbeat(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	done <-chan struct{},
	jobID, workerID, attemptID string,
	allowCancellation bool,
) {
	heartbeatTicker := time.NewTicker(writebackHeartbeat)
	cancellationTicker := time.NewTicker(writebackCancellationPoll)
	defer heartbeatTicker.Stop()
	defer cancellationTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-cancellationTicker.C:
			if !allowCancellation {
				continue
			}
			requested, err := worker.store.WritebackCancellationRequested(
				ctx, jobID, workerID, attemptID,
			)
			if err != nil {
				worker.logger.Warn("metadata.writeback.cancellation_check_failed", map[string]any{
					"jobId": jobID, "message": safeWritebackError(err),
				})
				cancel(err)
				return
			}
			if requested {
				cancel(NewWritebackError("WRITEBACK_CANCELLED", "Metadata writeback was cancelled"))
				return
			}
		case <-heartbeatTicker.C:
			if err := worker.store.RenewWritebackLease(
				ctx, jobID, workerID, attemptID, writebackLease,
			); err != nil {
				worker.logger.Warn("metadata.writeback.heartbeat_failed", map[string]any{
					"jobId": jobID, "message": safeWritebackError(err),
				})
				cancel(err)
				return
			}
		}
	}
}

func (worker *WritebackWorker) process(
	ctx context.Context,
	jobID, workerID, attemptID string,
) (processErr error) {
	contextRecord, err := worker.store.LoadWritebackContext(ctx, jobID, workerID, attemptID)
	if err != nil {
		return err
	}
	if err := assertWritebackPathSnapshot(contextRecord); err != nil {
		return err
	}
	if contextRecord.Job.Stage == StageCommitted && writebackNeedsReconciliation(contextRecord.Job) {
		return worker.cleanupCommittedRollback(ctx, contextRecord.Job, workerID, attemptID)
	}
	if writebackNeedsTransientRecovery(contextRecord.Job) {
		return worker.recoverTransientRollback(ctx, contextRecord, workerID, attemptID)
	}
	if contextRecord.Job.CancelRequested {
		return NewWritebackError("WRITEBACK_CANCELLED", "Metadata writeback was cancelled")
	}
	if err := assertWritebackTrackActive(contextRecord); err != nil {
		return err
	}
	if err := assertWritableSource(
		contextRecord.RootMode, contextRecord.Enabled, contextRecord.Status, contextRecord.Source.Status,
	); err != nil {
		return err
	}
	expectedMetadata, err := decodeSnapshot(contextRecord.Job.MetadataSnapshot)
	if err != nil {
		return err
	}
	sourcePath, err := safeSourcePath(
		contextRecord.Job.RootPathSnapshot,
		contextRecord.Job.SourcePathSnapshot,
	)
	if err != nil {
		return err
	}
	paths := WritebackPaths(sourcePath, jobID, attemptID)
	artworkPath := ""
	rollbackActive := false
	originalChecksum := ""
	var expectedOutputChecksum string
	defer func() {
		_ = os.Remove(paths.Temporary)
		if artworkPath != "" {
			_ = os.Remove(artworkPath)
		}
		if !rollbackActive {
			return
		}
		stateContext, cancel := context.WithTimeout(context.Background(), writebackLease)
		defer cancel()
		job, stateErr := worker.store.FindWriteback(stateContext, jobID)
		if stateErr != nil {
			worker.logger.Warn("metadata.writeback.rollback_state_failed", map[string]any{
				"jobId": jobID, "message": safeWritebackError(stateErr),
			})
			return
		}
		if job.Status != WritebackProcessing || job.LockedBy == nil || *job.LockedBy != workerID ||
			job.AttemptID == nil || *job.AttemptID != attemptID {
			return
		}
		if job.Stage == StageCommitted && job.OutputChecksumSHA256 != nil &&
			*job.OutputChecksumSHA256 == expectedOutputChecksum {
			if cleanupErr := worker.cleanupCommittedRollback(
				stateContext, job, workerID, attemptID,
			); cleanupErr != nil {
				processErr = cleanupErr
				return
			}
			rollbackActive, processErr = false, nil
			return
		}
		if !writebackNeedsTransientRecovery(job) {
			return
		}
		if renewErr := worker.store.RenewWritebackLease(
			stateContext, jobID, workerID, attemptID, writebackLease,
		); renewErr != nil {
			return
		}
		if restoreErr := restoreReplacementRollback(
			sourcePath, paths.Temporary, paths.Rollback,
			originalChecksum, expectedOutputChecksum,
		); restoreErr != nil {
			processErr = restoreErr
			worker.logger.Error("metadata.writeback.rollback_failed", map[string]any{
				"jobId": jobID, "message": safeWritebackError(restoreErr),
			})
			return
		}
		if completeErr := worker.store.CompleteTransientRollback(
			stateContext, jobID, workerID, attemptID,
		); completeErr != nil {
			processErr = completeErr
			return
		}
		rollbackActive, processErr = false, nil
	}()

	if err := worker.throwIfCancelled(ctx, jobID, workerID, attemptID); err != nil {
		return err
	}
	originalChecksum, err = sha256File(sourcePath)
	if err != nil {
		return err
	}
	if originalChecksum != contextRecord.Job.ExpectedSourceChecksum ||
		originalChecksum != contextRecord.Source.ChecksumSHA256 {
		return NewWritebackError("SOURCE_CHANGED", "The source file changed after the writeback was queued")
	}
	originalFile, err := os.Stat(sourcePath)
	if err != nil {
		return filesystemWritebackError(err, "The source file is unavailable")
	}
	before, err := ProbeMetadataFile(ctx, sourcePath, worker.ffprobePath, worker.runner)
	if err != nil {
		return err
	}
	hasAudio := false
	for _, stream := range before.Streams {
		if stream.CodecType == "audio" {
			hasAudio = true
			break
		}
	}
	if !hasAudio {
		return NewWritebackError("NO_AUDIO_STREAM", "The source file does not contain an audio stream")
	}
	if contextRecord.Artwork != nil {
		if !SupportsEmbeddedArtworkWriteback(sourcePath) {
			return NewWritebackError("ARTWORK_WRITEBACK_UNSUPPORTED", "This source container cannot safely embed album artwork")
		}
		if worker.artwork == nil {
			return NewWritebackError("ARTWORK_STORAGE_UNAVAILABLE", "Album artwork storage is unavailable for Tag writeback")
		}
		if err := assertArtworkStreamsSafe(before); err != nil {
			return err
		}
		extension, err := artworkExtension(contextRecord.Artwork.MIMEType)
		if err != nil {
			return err
		}
		artworkPath = paths.Artwork + extension
		if err := worker.artwork.DownloadToFile(ctx, contextRecord.Artwork.ObjectKey, artworkPath, maximumEmbeddedArtworkBytes); err != nil {
			return wrapWritebackError("ARTWORK_DOWNLOAD_FAILED", "Album artwork could not be downloaded for Tag writeback", err)
		}
		expectedMetadata.HasArtwork = true
	}
	if err := RemuxMetadataToFile(
		ctx, sourcePath, paths.Temporary, artworkPath, worker.ffmpegPath, expectedMetadata, worker.runner,
	); err != nil {
		return err
	}
	if err := os.Chmod(paths.Temporary, originalFile.Mode()); err != nil {
		return filesystemWritebackError(err, "Unable to preserve source file permissions")
	}
	if err := syncFile(paths.Temporary); err != nil {
		return err
	}
	after, err := ProbeMetadataFile(ctx, paths.Temporary, worker.ffprobePath, worker.runner)
	if err != nil {
		return err
	}
	if err := VerifyMetadataRemux(before, after, expectedMetadata); err != nil {
		return err
	}
	outputChecksum, err := sha256File(paths.Temporary)
	if err != nil {
		return err
	}
	expectedOutputChecksum = outputChecksum
	if err := worker.throwIfCancelled(ctx, jobID, workerID, attemptID); err != nil {
		return err
	}
	unchangedChecksum, err := sha256File(sourcePath)
	if err != nil {
		return err
	}
	if unchangedChecksum != originalChecksum {
		return NewWritebackError("SOURCE_CHANGED", "The source file changed while metadata was being written")
	}
	if err := worker.store.MarkWritebackPrepared(
		ctx, jobID, workerID, attemptID, outputChecksum,
	); err != nil {
		return err
	}
	if err := worker.store.RenewWritebackLease(
		ctx, jobID, workerID, attemptID, writebackLease,
	); err != nil {
		return err
	}
	locked, err := worker.store.LoadWritebackContext(ctx, jobID, workerID, attemptID)
	if err != nil {
		return err
	}
	if locked.Job.CancelRequested {
		return NewWritebackError("WRITEBACK_CANCELLED", "Metadata writeback was cancelled")
	}
	if err := assertWritableSource(locked.RootMode, locked.Enabled, locked.Status, locked.Source.Status); err != nil {
		return err
	}
	if err := assertWritebackPathSnapshot(locked); err != nil {
		return err
	}
	if err := assertWritebackContextUnchanged(locked, originalChecksum, expectedMetadata); err != nil {
		return err
	}
	finalSourceChecksum, err := sha256File(sourcePath)
	if err != nil {
		return err
	}
	if finalSourceChecksum != originalChecksum {
		return NewWritebackError("SOURCE_CHANGED", "The source file changed immediately before replacement")
	}
	if err := worker.throwIfCancelled(ctx, jobID, workerID, attemptID); err != nil {
		return err
	}
	if err := replaceSourceFile(
		sourcePath, paths.Temporary, paths.Rollback, originalChecksum,
	); err != nil {
		if rollback, inspectErr := os.Lstat(paths.Rollback); inspectErr == nil &&
			rollback.Mode().IsRegular() && rollback.Mode()&os.ModeSymlink == 0 {
			rollbackActive = true
		}
		return err
	}
	rollbackActive = true
	replacedChecksum, err := sha256File(sourcePath)
	if err != nil {
		return err
	}
	if replacedChecksum != outputChecksum {
		return NewWritebackError("SOURCE_CHANGED", "The replaced source file failed checksum verification")
	}
	outputFile, err := os.Stat(sourcePath)
	if err != nil {
		return filesystemWritebackError(err, "Unable to inspect replaced source file")
	}
	if err := worker.store.MarkWritebackFileReplaced(
		ctx, jobID, workerID, attemptID, outputChecksum,
	); err != nil {
		return err
	}
	if err := worker.store.CommitWriteback(ctx, WritebackCommit{
		JobID: jobID, WorkerID: workerID, AttemptID: attemptID,
		OriginalSHA256: originalChecksum, OutputSHA256: outputChecksum,
		OutputSize: outputFile.Size(), OutputModified: outputFile.ModTime(),
		Metadata: expectedMetadata,
	}); err != nil {
		return err
	}
	if err := removeReplacementRollback(paths.Rollback, filepath.Dir(sourcePath)); err != nil {
		return err
	}
	if err := worker.store.CompleteCommittedRollback(ctx, jobID, workerID, attemptID); err != nil {
		return err
	}
	rollbackActive = false
	return nil
}

func (worker *WritebackWorker) throwIfCancelled(
	ctx context.Context,
	jobID, workerID, attemptID string,
) error {
	if err := ctx.Err(); err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return NewWritebackError("WORKER_STOPPED", "Metadata writeback worker is stopping")
	}
	cancelled, err := worker.store.WritebackCancellationRequested(ctx, jobID, workerID, attemptID)
	if err != nil {
		return err
	}
	if cancelled {
		return NewWritebackError("WRITEBACK_CANCELLED", "Metadata writeback was cancelled")
	}
	return nil
}

type WritebackPathSet struct {
	Temporary string
	Artwork   string
	Rollback  string
}

func WritebackPaths(sourcePath, jobID, attemptID string) WritebackPathSet {
	directory := filepath.Dir(sourcePath)
	fileName := truncateRunes(filepath.Base(sourcePath), 100)
	if fileName == "" {
		fileName = "audio"
	}
	jobSuffix := safePathToken(jobID)
	attemptSuffix := safePathToken(attemptID)
	return WritebackPathSet{
		Temporary: filepath.Join(directory, "."+fileName+".xymusic-"+jobSuffix+"-"+attemptSuffix+".tmp"),
		Artwork:   filepath.Join(directory, "."+fileName+".xymusic-artwork-"+jobSuffix+"-"+attemptSuffix),
		Rollback:  filepath.Join(directory, "."+fileName+".xymusic-rollback-"+jobSuffix+"-"+attemptSuffix+".tmp"),
	}
}

func SupportsEmbeddedArtworkWriteback(sourcePath string) bool {
	switch strings.ToLower(filepath.Ext(sourcePath)) {
	case ".mp3", ".flac", ".m4a", ".mp4":
		return true
	default:
		return false
	}
}

func artworkExtension(mimeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	default:
		return "", NewWritebackError("ARTWORK_FORMAT_UNSUPPORTED", "Album artwork must be a JPEG or PNG image")
	}
}

func assertArtworkStreamsSafe(file ProbedMetadataFile) error {
	for _, stream := range file.Streams {
		if stream.CodecType == "video" && !stream.AttachedPicture {
			return NewWritebackError("ARTWORK_WRITEBACK_UNSUPPORTED", "The source contains a non-artwork video stream")
		}
	}
	return nil
}

func assertWritebackPathSnapshot(contextRecord WritebackContext) error {
	job := contextRecord.Job
	if strings.TrimSpace(job.RootPathSnapshot) == "" ||
		strings.TrimSpace(job.SourcePathSnapshot) == "" {
		return NewWritebackError("SOURCE_PATH_CHANGED", "The writeback source path snapshot is unavailable")
	}
	if !samePath(job.RootPathSnapshot, contextRecord.RootPath) ||
		!sameRelativePath(job.SourcePathSnapshot, contextRecord.Source.SourcePath) {
		return NewWritebackError("SOURCE_PATH_CHANGED", "The managed source path changed after the writeback was queued")
	}
	return nil
}

func assertWritebackTrackActive(contextRecord WritebackContext) error {
	if contextRecord.TrackStatus == "ARCHIVED" {
		return NewWritebackError("INVALID_STATE_TRANSITION", "Archived tracks cannot complete Tag writeback")
	}
	return nil
}

func sameRelativePath(left, right string) bool {
	normalize := func(value string) string {
		return filepath.Clean(filepath.FromSlash(strings.ReplaceAll(value, "\\", "/")))
	}
	return samePath(normalize(left), normalize(right))
}

func writebackNeedsTransientRecovery(job WritebackJob) bool {
	return writebackNeedsReconciliation(job) &&
		(job.Stage == StagePrepared || job.Stage == StageFileReplaced)
}

func writebackNeedsReconciliation(job WritebackJob) bool {
	return job.Status == WritebackProcessing && job.AttemptID != nil &&
		job.OutputChecksumSHA256 != nil &&
		(job.Stage == StagePrepared || job.Stage == StageFileReplaced || job.Stage == StageCommitted)
}

func safeSourcePath(rootPath, sourceRelativePath string) (string, error) {
	root, candidate, err := safeSourceCandidate(rootPath, sourceRelativePath)
	if err != nil {
		return "", err
	}
	entry, err := os.Lstat(candidate)
	if err != nil {
		return "", filesystemWritebackError(err, "The source file is unavailable")
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return "", NewWritebackError("UNSAFE_SOURCE_PATH", "Symbolic-link source files cannot be modified")
	}
	source, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", filesystemWritebackError(err, "The source file is unavailable")
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", filesystemWritebackError(err, "The source file is unavailable")
	}
	if !isContainedPath(root, source) {
		return "", NewWritebackError("UNSAFE_SOURCE_PATH", "The resolved source file escapes its root")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", filesystemWritebackError(err, "The source file is unavailable")
	}
	if !info.Mode().IsRegular() {
		return "", NewWritebackError("SOURCE_NOT_FILE", "The source path is not a regular file")
	}
	return source, nil
}

func safeSourceCandidatePath(rootPath, sourceRelativePath string) (string, error) {
	root, candidate, err := safeSourceCandidate(rootPath, sourceRelativePath)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		return "", filesystemWritebackError(err, "The source directory is unavailable")
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", filesystemWritebackError(err, "The source directory is unavailable")
	}
	if !isContainedPath(root, parent) {
		return "", NewWritebackError("UNSAFE_SOURCE_PATH", "The resolved source directory escapes its root")
	}
	return filepath.Join(parent, filepath.Base(candidate)), nil
}

func safeSourceCandidate(rootPath, sourceRelativePath string) (string, string, error) {
	if sourceRelativePath == "" || filepath.IsAbs(sourceRelativePath) || strings.ContainsRune(sourceRelativePath, '\x00') {
		return "", "", NewWritebackError("UNSAFE_SOURCE_PATH", "The source path is invalid")
	}
	root, err := filepath.Abs(filepath.Clean(rootPath))
	if err != nil {
		return "", "", filesystemWritebackError(err, "The managed source root is unavailable")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", filesystemWritebackError(err, "The managed source root is unavailable")
	}
	relative := filepath.FromSlash(strings.ReplaceAll(sourceRelativePath, "\\", "/"))
	candidate, err := filepath.Abs(filepath.Join(root, relative))
	if err != nil {
		return "", "", NewWritebackError("UNSAFE_SOURCE_PATH", "The source path is invalid")
	}
	if !isContainedPath(root, candidate) {
		return "", "", NewWritebackError("UNSAFE_SOURCE_PATH", "The source path escapes its root")
	}
	return root, candidate, nil
}

func (worker *WritebackWorker) recoverTransientRollback(
	ctx context.Context,
	contextRecord WritebackContext,
	workerID, attemptID string,
) error {
	job := contextRecord.Job
	if !writebackNeedsTransientRecovery(job) || job.OutputChecksumSHA256 == nil {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Writeback recovery state is incomplete")
	}
	if err := worker.store.RenewWritebackLease(
		ctx, job.ID, workerID, attemptID, writebackLease,
	); err != nil {
		return err
	}
	sourcePath, err := safeSourceCandidatePath(job.RootPathSnapshot, job.SourcePathSnapshot)
	if err != nil {
		return err
	}
	paths := WritebackPaths(sourcePath, job.ID, attemptID)
	originalChecksum := job.ExpectedSourceChecksum
	outputChecksum := *job.OutputChecksumSHA256

	rollback, rollbackErr := os.Lstat(paths.Rollback)
	if rollbackErr != nil && !errors.Is(rollbackErr, os.ErrNotExist) {
		return filesystemWritebackError(rollbackErr, "Unable to inspect the temporary source rollback")
	}
	rollbackExists := rollbackErr == nil
	if rollbackExists {
		if !rollback.Mode().IsRegular() || rollback.Mode()&os.ModeSymlink != 0 {
			return NewWritebackError("ROLLBACK_FAILED", "The temporary source rollback is unsafe")
		}
		checksum, err := sha256File(paths.Rollback)
		if err != nil || checksum != originalChecksum {
			return NewWritebackError("ROLLBACK_FAILED", "The temporary source rollback failed verification")
		}
	}

	sourceExists, sourceChecksum, err := inspectRecoverySource(sourcePath)
	if err != nil {
		return err
	}
	switch {
	case !rollbackExists && sourceExists && sourceChecksum == originalChecksum:
		// PREPARED may be persisted before the atomic rename begins. The source
		// is still original, so only exact attempt artifacts need cleanup.
	case !rollbackExists:
		return NewWritebackError("ROLLBACK_FAILED", "The temporary source rollback is missing")
	case !sourceExists:
		if err := moveFileNoReplace(paths.Rollback, sourcePath); err != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to restore the original source file")
		}
		if restored, err := sha256File(sourcePath); err != nil || restored != originalChecksum {
			return NewWritebackError("ROLLBACK_FAILED", "The restored source failed verification")
		}
		if err := syncDirectory(filepath.Dir(sourcePath)); err != nil {
			return err
		}
	case sourceChecksum == originalChecksum:
		if err := removeReplacementRollback(paths.Rollback, filepath.Dir(sourcePath)); err != nil {
			return err
		}
	case sourceChecksum == outputChecksum:
		if err := restoreReplacementRollback(
			sourcePath, paths.Temporary, paths.Rollback, originalChecksum, outputChecksum,
		); err != nil {
			return err
		}
	default:
		return NewWritebackError("ROLLBACK_FAILED", "The source changed before temporary rollback recovery")
	}
	if err := cleanupTransientAttemptArtifacts(paths); err != nil {
		return err
	}
	if err := worker.store.CompleteTransientRollback(
		ctx, job.ID, workerID, attemptID,
	); err != nil {
		return err
	}
	worker.logger.Warn("metadata.writeback.transient_rollback_recovered", map[string]any{
		"jobId": job.ID,
	})
	return nil
}

func (worker *WritebackWorker) cleanupCommittedRollback(
	ctx context.Context,
	job WritebackJob,
	workerID, attemptID string,
) error {
	if job.Status != WritebackProcessing || job.Stage != StageCommitted ||
		job.OutputChecksumSHA256 == nil {
		return NewWritebackError("WRITEBACK_LEASE_LOST", "Committed rollback cleanup state is incomplete")
	}
	if err := worker.store.RenewWritebackLease(
		ctx, job.ID, workerID, attemptID, writebackLease,
	); err != nil {
		return err
	}
	sourcePath, err := safeSourceCandidatePath(job.RootPathSnapshot, job.SourcePathSnapshot)
	if err != nil {
		return err
	}
	paths := WritebackPaths(sourcePath, job.ID, attemptID)
	sourceExists, sourceChecksum, err := inspectRecoverySource(sourcePath)
	if err != nil {
		return err
	}
	if !sourceExists || sourceChecksum != *job.OutputChecksumSHA256 {
		return NewWritebackError("ROLLBACK_FAILED", "The committed source cannot be verified for rollback cleanup")
	}
	rollback, rollbackErr := os.Lstat(paths.Rollback)
	if rollbackErr != nil && !errors.Is(rollbackErr, os.ErrNotExist) {
		return filesystemWritebackError(rollbackErr, "Unable to inspect committed temporary rollback")
	}
	if rollbackErr == nil {
		if !rollback.Mode().IsRegular() || rollback.Mode()&os.ModeSymlink != 0 {
			return NewWritebackError("ROLLBACK_FAILED", "The committed temporary rollback is unsafe")
		}
		rollbackChecksum, err := sha256File(paths.Rollback)
		if err != nil || rollbackChecksum != job.ExpectedSourceChecksum {
			return NewWritebackError("ROLLBACK_FAILED", "The committed temporary rollback failed verification")
		}
		if err := removeReplacementRollback(paths.Rollback, filepath.Dir(sourcePath)); err != nil {
			return err
		}
	}
	if err := cleanupTransientAttemptArtifacts(paths); err != nil {
		return err
	}
	return worker.store.CompleteCommittedRollback(ctx, job.ID, workerID, attemptID)
}

func inspectRecoverySource(path string) (bool, string, error) {
	entry, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", filesystemWritebackError(err, "Unable to inspect the writeback source")
	}
	if !entry.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 {
		return false, "", NewWritebackError("UNSAFE_SOURCE_PATH", "The writeback source is not a regular file")
	}
	checksum, err := sha256File(path)
	if err != nil {
		return false, "", err
	}
	return true, checksum, nil
}

func cleanupTransientAttemptArtifacts(paths WritebackPathSet) error {
	for _, path := range []string{
		paths.Temporary,
		paths.Artwork + ".jpg",
		paths.Artwork + ".png",
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return filesystemWritebackError(err, "Unable to remove a temporary writeback artifact")
		}
	}
	return syncDirectory(filepath.Dir(paths.Temporary))
}

func restoreReplacementRollback(
	sourcePath, temporaryPath, rollbackPath, expectedOriginalChecksum, expectedOutputChecksum string,
) error {
	rollback, err := os.Lstat(rollbackPath)
	if err != nil || !rollback.Mode().IsRegular() || rollback.Mode()&os.ModeSymlink != 0 {
		return NewWritebackError("ROLLBACK_FAILED", "The temporary source rollback is unavailable")
	}
	rollbackChecksum, err := sha256File(rollbackPath)
	if err != nil || rollbackChecksum != expectedOriginalChecksum {
		return NewWritebackError("ROLLBACK_FAILED", "The temporary source rollback failed verification")
	}

	source, sourceErr := os.Lstat(sourcePath)
	if errors.Is(sourceErr, os.ErrNotExist) {
		if err := moveFileNoReplace(rollbackPath, sourcePath); err != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to restore the original source file")
		}
		return syncDirectory(filepath.Dir(sourcePath))
	}
	if sourceErr != nil || !source.Mode().IsRegular() || source.Mode()&os.ModeSymlink != 0 {
		return NewWritebackError("ROLLBACK_FAILED", "The replaced source cannot be restored safely")
	}
	sourceChecksum, err := sha256File(sourcePath)
	if err != nil || sourceChecksum != expectedOutputChecksum {
		return NewWritebackError("ROLLBACK_FAILED", "The replaced source changed before rollback")
	}

	_ = os.Remove(temporaryPath)
	if err := moveFileNoReplace(sourcePath, temporaryPath); err != nil {
		return NewWritebackError("ROLLBACK_FAILED", "Unable to stage the replaced source for rollback")
	}
	restoreOutput := func() error {
		if err := moveFileNoReplace(temporaryPath, sourcePath); err != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to restore either source version")
		}
		return NewWritebackError("ROLLBACK_FAILED", "Unable to restore the original source file")
	}
	if err := moveFileNoReplace(rollbackPath, sourcePath); err != nil {
		return restoreOutput()
	}
	restoredChecksum, err := sha256File(sourcePath)
	if err != nil || restoredChecksum != expectedOriginalChecksum {
		if moveErr := moveFileNoReplace(sourcePath, rollbackPath); moveErr != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to preserve the temporary source rollback")
		}
		return restoreOutput()
	}
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return filesystemWritebackError(err, "Unable to remove the replaced temporary source")
	}
	return syncDirectory(filepath.Dir(sourcePath))
}

func replaceSourceFile(
	sourcePath, temporaryPath, rollbackPath, expectedSourceChecksum string,
) error {
	_ = os.Remove(rollbackPath)
	if err := moveFileNoReplace(sourcePath, rollbackPath); err != nil {
		return filesystemWritebackError(err, "Unable to atomically replace source file")
	}
	if err := syncDirectory(filepath.Dir(sourcePath)); err != nil {
		_ = moveFileNoReplace(rollbackPath, sourcePath)
		return err
	}
	rollbackChecksum, err := sha256File(rollbackPath)
	if err != nil {
		_ = moveFileNoReplace(rollbackPath, sourcePath)
		return err
	}
	if rollbackChecksum != expectedSourceChecksum {
		if err := moveFileNoReplace(rollbackPath, sourcePath); err != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to restore a concurrently modified source file")
		}
		return NewWritebackError("SOURCE_CHANGED", "The source file changed while replacement began")
	}
	if _, err := os.Lstat(sourcePath); err == nil {
		return NewWritebackError("SOURCE_CHANGED", "The source path was recreated while replacement began")
	} else if !errors.Is(err, os.ErrNotExist) {
		return filesystemWritebackError(err, "Unable to verify the source replacement path")
	}
	if err := moveFileNoReplace(temporaryPath, sourcePath); err != nil {
		_ = moveFileNoReplace(rollbackPath, sourcePath)
		return filesystemWritebackError(err, "Unable to atomically replace source file")
	}
	if err := syncFile(sourcePath); err != nil {
		_ = os.Remove(sourcePath)
		if restoreErr := moveFileNoReplace(rollbackPath, sourcePath); restoreErr != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to restore source during replacement")
		}
		return err
	}
	if err := syncDirectory(filepath.Dir(sourcePath)); err != nil {
		_ = os.Remove(sourcePath)
		if restoreErr := moveFileNoReplace(rollbackPath, sourcePath); restoreErr != nil {
			return NewWritebackError("ROLLBACK_FAILED", "Unable to restore source after directory synchronization failed")
		}
		return err
	}
	return nil
}

func removeReplacementRollback(rollbackPath, directory string) error {
	if err := os.Remove(rollbackPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return filesystemWritebackError(err, "Unable to remove replacement rollback file")
	}
	return syncDirectory(directory)
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return filesystemWritebackError(err, "Unable to open temporary media file")
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return filesystemWritebackError(err, "Unable to synchronize media file")
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, os.ErrPermission) {
			return nil
		}
		return filesystemWritebackError(err, "Unable to open the media directory for synchronization")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, os.ErrPermission) {
			return nil
		}
		return filesystemWritebackError(err, "Unable to synchronize the media directory")
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", filesystemWritebackError(err, "Unable to checksum media file")
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", filesystemWritebackError(err, "Unable to checksum media file")
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func isContainedPath(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func safePathToken(value string) string {
	value = unsafePathTokenPattern.ReplaceAllString(value, "")
	if len(value) > 64 {
		value = value[:64]
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

var unsafePathTokenPattern = regexp.MustCompile(`[^a-zA-Z0-9]`)
