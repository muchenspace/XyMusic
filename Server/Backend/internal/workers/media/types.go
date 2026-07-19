package media

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	defaultLease            = 120 * time.Second
	defaultHeartbeat        = 30 * time.Second
	defaultCancellationPoll = time.Second
	defaultProbeTimeout     = 30 * time.Second
	defaultTranscodeTimeout = 15 * time.Minute
	defaultIdleDelay        = time.Second
	maxProcessStdoutBytes   = 4 * 1024 * 1024
	maxProcessStderrBytes   = 256 * 1024
)

type MediaJob struct {
	ID              string
	Type            string
	SourceAssetID   string
	TrackID         string
	Status          string
	Attempts        int
	MaxAttempts     int
	Version         int
	Generation      int
	AttemptID       *string
	CancelRequested bool
	PublishOnReady  bool
	Payload         json.RawMessage
	LockedBy        *string
	LockedUntil     *time.Time
	HeartbeatAt     *time.Time
	NextAttemptAt   time.Time
	LastError       *string
	LastErrorCode   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SourceAsset struct {
	ID             string
	ObjectKey      string
	SizeBytes      int64
	ChecksumSHA256 *string
}

type CleanupJob struct {
	ID            string
	ObjectKey     string
	Reason        string
	Status        string
	Attempts      int
	MaxAttempts   int
	AttemptID     *string
	LockedBy      *string
	LockedUntil   *time.Time
	NextAttemptAt time.Time
	LastError     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AudioVariantProfile struct {
	Quality       string
	Extension     string
	MIMEType      string
	Codec         string
	Container     string
	TargetBitrate *int
	FFmpegArgs    []string
}

type GeneratedVariant struct {
	Profile        AudioVariantProfile
	ObjectKey      string
	ChecksumSHA256 string
	SizeBytes      int64
}

type CommitMediaJob struct {
	Job         MediaJob
	WorkerID    string
	DurationMS  int64
	SampleRate  *int
	Generated   []GeneratedVariant
	CompletedAt time.Time
}

type JobControl struct {
	Owned           bool
	CancelRequested bool
}

type Store interface {
	ClaimMediaJob(context.Context, string, time.Time, time.Duration) (*MediaJob, error)
	RenewMediaLease(context.Context, string, string, string, time.Time, time.Time) (bool, error)
	MediaJobControl(context.Context, string, string, string) (JobControl, error)
	FindReadySourceAsset(context.Context, string) (*SourceAsset, error)
	CommitMediaJob(context.Context, CommitMediaJob) ([]string, error)
	FailMediaJob(context.Context, MediaJob, string, error, time.Time) error
	ScheduleReplacedAssetCleanup(context.Context, []string, time.Time) error
	EnqueueObjectCleanup(context.Context, string, string, time.Time) error
	ClaimObjectCleanup(context.Context, string, time.Time, time.Duration) (*CleanupJob, error)
	ReadyAssetReferencesObject(context.Context, string) (bool, error)
	CompleteObjectCleanup(context.Context, CleanupJob, string, bool, time.Time) (bool, error)
	FailObjectCleanup(context.Context, CleanupJob, string, error, time.Time) error
}

type DownloadedObject struct {
	SizeBytes      int64
	ChecksumSHA256 string
}

type ObjectStorage interface {
	DownloadToFile(context.Context, string, string, int64) (DownloadedObject, error)
	UploadFile(context.Context, string, string, string, string) (int64, error)
	Delete(context.Context, string) error
}

type ProcessResult struct {
	Stdout          string
	Stderr          string
	ExitCode        int
	TimedOut        bool
	StdoutTruncated bool
}

type ProcessRunner interface {
	Run(context.Context, string, []string, time.Duration) (ProcessResult, error)
}

type Logger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
	Error(string, map[string]any)
}

type Clock interface {
	Now() time.Time
}

type NoopLogger struct{}

func (NoopLogger) Info(string, map[string]any)  {}
func (NoopLogger) Warn(string, map[string]any)  {}
func (NoopLogger) Error(string, map[string]any) {}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type WorkerError struct {
	Code        string
	Message     string
	Interrupted bool
}

func (err *WorkerError) Error() string {
	if err == nil {
		return ""
	}
	return err.Code + ": " + err.Message
}

func newWorkerError(code, message string) error {
	return &WorkerError{Code: code, Message: message}
}

func newInterruptedError(code, message string) error {
	return &WorkerError{Code: code, Message: message, Interrupted: true}
}

func isInterrupted(err error) bool {
	var workerError *WorkerError
	return errors.As(err, &workerError) && workerError.Interrupted
}

func workerErrorCode(err error) string {
	if err == nil {
		return "UNKNOWN"
	}
	var workerError *WorkerError
	if errors.As(err, &workerError) && strings.TrimSpace(workerError.Code) != "" {
		return truncateRunes(workerError.Code, 100)
	}
	message := err.Error()
	if before, _, found := strings.Cut(message, ":"); found {
		message = before
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "UNKNOWN"
	}
	return truncateRunes(message, 100)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
