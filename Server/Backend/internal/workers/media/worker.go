package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrWorkerClosed = errors.New("media worker is closed")

type Options struct {
	Store            Store
	Storage          ObjectStorage
	FFmpegPath       string
	FFprobePath      string
	WorkerID         string
	Runner           ProcessRunner
	Logger           Logger
	Clock            Clock
	Lease            time.Duration
	Heartbeat        time.Duration
	CancellationPoll time.Duration
	ProbeTimeout     time.Duration
	TranscodeTimeout time.Duration
	TemporaryRoot    string
}

type Worker struct {
	store            Store
	storage          ObjectStorage
	ffmpegPath       string
	ffprobePath      string
	workerID         string
	runner           ProcessRunner
	logger           Logger
	clock            Clock
	lease            time.Duration
	heartbeat        time.Duration
	cancellationPoll time.Duration
	probeTimeout     time.Duration
	transcodeTimeout time.Duration
	temporaryRoot    string

	lifecycle context.Context
	stop      context.CancelCauseFunc
	runMu     sync.Mutex
	stateMu   sync.Mutex
	closed    bool
	active    sync.WaitGroup
}

func New(options Options) (*Worker, error) {
	if options.Store == nil {
		return nil, errors.New("media worker store is required")
	}
	if options.Storage == nil {
		return nil, errors.New("media worker object storage is required")
	}
	if strings.TrimSpace(options.FFmpegPath) == "" {
		return nil, errors.New("media worker ffmpeg path is required")
	}
	if strings.TrimSpace(options.FFprobePath) == "" {
		return nil, errors.New("media worker ffprobe path is required")
	}
	if strings.TrimSpace(options.WorkerID) == "" {
		options.WorkerID = "media-" + uuid.NewString()
	}
	if options.Runner == nil {
		options.Runner = OSProcessRunner{}
	}
	if options.Logger == nil {
		options.Logger = NoopLogger{}
	}
	if options.Clock == nil {
		options.Clock = SystemClock{}
	}
	if options.Lease == 0 {
		options.Lease = defaultLease
	}
	if options.Heartbeat == 0 {
		options.Heartbeat = defaultHeartbeat
	}
	if options.CancellationPoll == 0 {
		options.CancellationPoll = defaultCancellationPoll
	}
	if options.ProbeTimeout == 0 {
		options.ProbeTimeout = defaultProbeTimeout
	}
	if options.TranscodeTimeout == 0 {
		options.TranscodeTimeout = defaultTranscodeTimeout
	}
	if options.Lease <= 0 || options.Heartbeat <= 0 || options.Heartbeat >= options.Lease ||
		options.CancellationPoll <= 0 || options.ProbeTimeout <= 0 || options.TranscodeTimeout <= 0 {
		return nil, errors.New("media worker timing configuration is invalid")
	}
	lifecycle, stop := context.WithCancelCause(context.Background())
	return &Worker{
		store: options.Store, storage: options.Storage,
		ffmpegPath: strings.TrimSpace(options.FFmpegPath), ffprobePath: strings.TrimSpace(options.FFprobePath),
		workerID: options.WorkerID, runner: options.Runner, logger: options.Logger, clock: options.Clock,
		lease: options.Lease, heartbeat: options.Heartbeat, cancellationPoll: options.CancellationPoll,
		probeTimeout: options.ProbeTimeout, transcodeTimeout: options.TranscodeTimeout,
		temporaryRoot: options.TemporaryRoot, lifecycle: lifecycle, stop: stop,
	}, nil
}

func (worker *Worker) Run(ctx context.Context, idleDelay time.Duration) error {
	if idleDelay <= 0 {
		idleDelay = defaultIdleDelay
	}
	for {
		worked, err := worker.RunNext(ctx)
		if errors.Is(err, ErrWorkerClosed) || errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
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
		case <-worker.lifecycle.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (worker *Worker) RunNext(ctx context.Context) (bool, error) {
	if !worker.beginOperation() {
		return false, ErrWorkerClosed
	}
	defer worker.active.Done()
	worker.runMu.Lock()
	defer worker.runMu.Unlock()
	if ctx.Err() != nil || worker.lifecycle.Err() != nil {
		return false, nil
	}
	operationContext, dispose := linkContexts(ctx, worker.lifecycle)
	defer dispose()
	if err := operationContext.Err(); err != nil {
		return false, nil
	}
	job, err := worker.store.ClaimMediaJob(
		operationContext, worker.workerID, worker.clock.Now(), worker.lease,
	)
	if err != nil {
		return false, err
	}
	if job == nil {
		return worker.runCleanupNext(operationContext)
	}
	return true, worker.runMediaJob(operationContext, *job)
}

func (worker *Worker) Close() error {
	worker.stateMu.Lock()
	if worker.closed {
		worker.stateMu.Unlock()
		return nil
	}
	worker.closed = true
	worker.stop(newInterruptedError("WORKER_STOPPED", "media worker stopped"))
	worker.stateMu.Unlock()
	worker.active.Wait()
	return nil
}

func (worker *Worker) WorkerID() string { return worker.workerID }

func (worker *Worker) beginOperation() bool {
	worker.stateMu.Lock()
	defer worker.stateMu.Unlock()
	if worker.closed {
		return false
	}
	worker.active.Add(1)
	return true
}

func (worker *Worker) runMediaJob(ctx context.Context, job MediaJob) error {
	processContext, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	monitorDone := make(chan struct{})
	if job.AttemptID != nil {
		go func() {
			defer close(monitorDone)
			worker.monitorMediaJob(processContext, cancel, done, job.ID, *job.AttemptID)
		}()
	} else {
		close(monitorDone)
	}
	processErr := worker.process(processContext, job)
	close(done)
	cancel(newInterruptedError("WORKER_STOPPED", "media worker stopped"))
	<-monitorDone
	if processErr == nil {
		worker.logger.Info("media.job.completed", map[string]any{"jobId": job.ID, "trackId": job.TrackID})
		return nil
	}
	failureContext, failureCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	failureErr := worker.store.FailMediaJob(
		failureContext, job, worker.workerID, processErr, worker.clock.Now(),
	)
	failureCancel()
	event := "media.job.failed"
	if isInterrupted(processErr) {
		event = "media.job.interrupted"
	}
	worker.logger.Warn(event, map[string]any{"jobId": job.ID, "message": safeWorkerError(processErr)})
	return failureErr
}

func (worker *Worker) monitorMediaJob(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	done <-chan struct{},
	jobID, attemptID string,
) {
	heartbeat := time.NewTicker(worker.heartbeat)
	cancellation := time.NewTicker(worker.cancellationPoll)
	defer heartbeat.Stop()
	defer cancellation.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-heartbeat.C:
			checkContext, checkCancel := context.WithTimeout(ctx, min(worker.heartbeat, 10*time.Second))
			now := worker.clock.Now()
			owned, err := worker.store.RenewMediaLease(
				checkContext, jobID, attemptID, worker.workerID, now, now.Add(worker.lease),
			)
			checkCancel()
			if err != nil {
				worker.logger.Warn("media.job.heartbeat_failed", map[string]any{
					"jobId": jobID, "message": safeWorkerError(err),
				})
				continue
			}
			if !owned {
				cancel(newInterruptedError("JOB_LEASE_LOST", "media job lease was lost"))
				return
			}
		case <-cancellation.C:
			checkContext, checkCancel := context.WithTimeout(ctx, min(worker.cancellationPoll, 10*time.Second))
			control, err := worker.store.MediaJobControl(
				checkContext, jobID, attemptID, worker.workerID,
			)
			checkCancel()
			if err != nil {
				worker.logger.Warn("media.job.cancellation_check_failed", map[string]any{
					"jobId": jobID, "message": safeWorkerError(err),
				})
				continue
			}
			if control.CancelRequested {
				cancel(newInterruptedError("JOB_CANCELLED", "media job cancellation was requested"))
				return
			}
			if !control.Owned {
				cancel(newInterruptedError("JOB_LEASE_LOST", "media job lease was lost"))
				return
			}
		}
	}
}

func (worker *Worker) process(ctx context.Context, job MediaJob) (processErr error) {
	if job.AttemptID == nil {
		return newWorkerError("JOB_ATTEMPT_MISSING", "claimed media job has no attempt id")
	}
	if err := contextCause(ctx); err != nil {
		return err
	}
	source, err := worker.store.FindReadySourceAsset(ctx, job.SourceAssetID)
	if err != nil {
		return contextError(ctx, err)
	}
	if source == nil {
		return newWorkerError("SOURCE_ASSET_UNAVAILABLE", "source asset is not ready")
	}
	directory, err := os.MkdirTemp(worker.temporaryRoot, "xymusic-media-")
	if err != nil {
		return err
	}
	generated := make([]GeneratedVariant, 0, 4)
	attemptedObjectKeys := make([]string, 0, 4)
	committed := false
	defer func() {
		if !committed && len(attemptedObjectKeys) > 0 {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			for _, objectKey := range attemptedObjectKeys {
				_ = worker.store.EnqueueObjectCleanup(
					cleanupContext, objectKey, "ABANDONED_MEDIA_ATTEMPT", worker.clock.Now(),
				)
			}
		}
		_ = os.RemoveAll(directory)
	}()
	inputPath := filepath.Join(directory, "source")
	downloaded, err := worker.storage.DownloadToFile(
		ctx, source.ObjectKey, inputPath, source.SizeBytes,
	)
	if err != nil {
		return contextError(ctx, err)
	}
	if downloaded.SizeBytes != source.SizeBytes {
		return newWorkerError("SOURCE_SIZE_MISMATCH", "source asset size changed")
	}
	if source.ChecksumSHA256 != nil && downloaded.ChecksumSHA256 != *source.ChecksumSHA256 {
		return newWorkerError("SOURCE_CHECKSUM_MISMATCH", "source asset checksum changed")
	}
	probeOutput, err := worker.runProcess(ctx, worker.ffprobePath, []string{
		"-v", "error", "-show_entries", "format=duration:stream=codec_type,codec_name,sample_rate,bit_rate",
		"-of", "json", inputPath,
	}, worker.probeTimeout, "FFPROBE")
	if err != nil {
		return err
	}
	probe, err := parseProbe(probeOutput)
	if err != nil {
		return err
	}
	audio, durationMS, err := probe.audio()
	if err != nil {
		return err
	}
	segment, err := mediaSegment(job.Payload, durationMS)
	if err != nil {
		return err
	}
	outputDurationMS := segment.EndMS - segment.StartMS
	profiles := AudioVariantProfiles(audio.CodecName)
	type plannedVariant struct {
		Profile AudioVariantProfile
		Path    string
	}
	planned := make([]plannedVariant, 0, len(profiles))
	arguments := []string{"-nostdin", "-v", "error", "-y"}
	if segment.StartMS > 0 {
		arguments = append(arguments, "-ss", seconds(segment.StartMS))
	}
	if segment.EndMS < durationMS {
		arguments = append(arguments, "-t", seconds(outputDurationMS))
	}
	arguments = append(arguments, "-i", inputPath)
	for _, profile := range profiles {
		outputPath := filepath.Join(
			directory, strings.ToLower(profile.Quality)+"."+profile.Extension,
		)
		planned = append(planned, plannedVariant{Profile: profile, Path: outputPath})
		arguments = append(arguments, "-map", "0:a:0", "-vn")
		arguments = append(arguments, profile.FFmpegArgs...)
		arguments = append(arguments, outputPath)
	}
	if _, err := worker.runProcess(
		ctx, worker.ffmpegPath, arguments, worker.transcodeTimeout, "FFMPEG",
	); err != nil {
		return err
	}
	for _, output := range planned {
		if err := contextCause(ctx); err != nil {
			return err
		}
		checksum, err := sha256File(output.Path)
		if err != nil {
			return err
		}
		objectKey := VariantObjectKey(job.TrackID, job.ID, *job.AttemptID, output.Profile)
		attemptedObjectKeys = append(attemptedObjectKeys, objectKey)
		sizeBytes, err := worker.storage.UploadFile(
			ctx, objectKey, output.Path, output.Profile.MIMEType, checksum,
		)
		if err != nil {
			return contextError(ctx, err)
		}
		generated = append(generated, GeneratedVariant{
			Profile: output.Profile, ObjectKey: objectKey,
			ChecksumSHA256: checksum, SizeBytes: sizeBytes,
		})
	}
	completedAt := worker.clock.Now()
	replacedAssetIDs, err := worker.store.CommitMediaJob(ctx, CommitMediaJob{
		Job: job, WorkerID: worker.workerID, DurationMS: outputDurationMS,
		SampleRate: audio.sampleRate(), Generated: generated, CompletedAt: completedAt,
	})
	if err != nil {
		return contextError(ctx, err)
	}
	committed = true
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cleanupCancel()
	if err := worker.store.ScheduleReplacedAssetCleanup(
		cleanupContext, replacedAssetIDs, worker.clock.Now(),
	); err != nil {
		worker.logger.Warn("media.variant.cleanup_schedule_failed", map[string]any{
			"jobId": job.ID, "trackId": job.TrackID, "message": safeWorkerError(err),
		})
	}
	return nil
}

func (worker *Worker) runCleanupNext(ctx context.Context) (bool, error) {
	cleanup, err := worker.store.ClaimObjectCleanup(
		ctx, worker.workerID, worker.clock.Now(), worker.lease,
	)
	if err != nil || cleanup == nil {
		return false, err
	}
	if cleanup.AttemptID == nil {
		return false, nil
	}
	referenced, cleanupErr := worker.store.ReadyAssetReferencesObject(ctx, cleanup.ObjectKey)
	if cleanupErr == nil && !referenced {
		cleanupErr = worker.storage.Delete(ctx, cleanup.ObjectKey)
	}
	if cleanupErr == nil {
		_, cleanupErr = worker.store.CompleteObjectCleanup(
			ctx, *cleanup, worker.workerID, referenced, worker.clock.Now(),
		)
	}
	if cleanupErr != nil {
		failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		failureErr := worker.store.FailObjectCleanup(
			failureContext, *cleanup, worker.workerID, cleanupErr, worker.clock.Now(),
		)
		cancel()
		if failureErr != nil {
			return true, failureErr
		}
	}
	return true, nil
}

func (worker *Worker) runProcess(
	ctx context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
	label string,
) (string, error) {
	if err := contextCause(ctx); err != nil {
		return "", err
	}
	result, err := worker.runner.Run(ctx, executable, arguments, timeout)
	if err != nil {
		return "", contextError(ctx, err)
	}
	if err := contextCause(ctx); err != nil {
		return "", err
	}
	if result.TimedOut {
		return "", newWorkerError(label+"_TIMEOUT", fmt.Sprintf("process exceeded %d ms", timeout.Milliseconds()))
	}
	if result.StdoutTruncated {
		return "", newWorkerError(
			label+"_OUTPUT_TOO_LARGE",
			fmt.Sprintf("process output exceeded %d bytes", maxProcessStdoutBytes),
		)
	}
	if result.ExitCode != 0 {
		return "", newWorkerError(label+"_FAILED", lastRunes(result.Stderr, 1_000))
	}
	return result.Stdout, nil
}

type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  *struct {
		Duration any `json:"duration"`
	} `json:"format"`
}

type probeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Sample    string `json:"sample_rate"`
	Bitrate   string `json:"bit_rate"`
}

func parseProbe(value string) (probeResult, error) {
	var result probeResult
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return probeResult{}, newWorkerError("FFPROBE_INVALID_OUTPUT", "ffprobe returned invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return probeResult{}, newWorkerError("FFPROBE_INVALID_OUTPUT", "ffprobe returned invalid JSON")
	}
	return result, nil
}

func (probe probeResult) audio() (probeStream, int64, error) {
	var audio *probeStream
	for index := range probe.Streams {
		if probe.Streams[index].CodecType == "audio" {
			audio = &probe.Streams[index]
			break
		}
	}
	if audio == nil || probe.Format == nil {
		return probeStream{}, 0, newWorkerError("INVALID_MEDIA", "ffprobe found no valid audio stream")
	}
	duration, err := javascriptNumber(probe.Format.Duration)
	if err != nil {
		return probeStream{}, 0, newWorkerError("INVALID_MEDIA", "ffprobe found no valid audio stream")
	}
	durationMS := math.Round(duration * 1_000)
	if durationMS < 1 || durationMS > 9_007_199_254_740_991 || math.IsNaN(durationMS) || math.IsInf(durationMS, 0) {
		return probeStream{}, 0, newWorkerError("INVALID_MEDIA", "ffprobe found no valid audio stream")
	}
	return *audio, int64(durationMS), nil
}

func (stream probeStream) sampleRate() *int {
	if stream.Sample == "" {
		return nil
	}
	value, err := strconv.Atoi(stream.Sample)
	if err != nil {
		return nil
	}
	return &value
}

func javascriptNumber(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		return strconv.ParseFloat(string(typed), 64)
	case float64:
		return typed, nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, nil
		}
		return strconv.ParseFloat(trimmed, 64)
	case nil:
		return 0, nil
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, errors.New("not numeric")
	}
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func contextCause(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return nil
}

func contextError(ctx context.Context, err error) error {
	if cause := contextCause(ctx); cause != nil {
		return cause
	}
	return err
}

func linkContexts(parent, lifecycle context.Context) (context.Context, func()) {
	linked, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	stopParent := context.AfterFunc(parent, func() {
		cause := context.Cause(parent)
		if cause == nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			cause = newInterruptedError("WORKER_STOPPED", "media worker stopped")
		}
		cancel(cause)
	})
	stopLifecycle := context.AfterFunc(lifecycle, func() {
		cause := context.Cause(lifecycle)
		if cause == nil {
			cause = newInterruptedError("WORKER_STOPPED", "media worker stopped")
		}
		cancel(cause)
	})
	return linked, func() {
		stopParent()
		stopLifecycle()
		cancel(nil)
	}
}
