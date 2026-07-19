package retention

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const (
	BatchSize           = 500
	MaxBatchesPerPolicy = 20
	RunInterval         = time.Hour
	RetryInterval       = 5 * time.Minute
	AdvisoryLockName    = "xymusic.retention"
)

type Cutoffs struct {
	RefreshTokens   time.Time
	RevokedSessions time.Time
	Uploads         time.Time
	OperationalJobs time.Time
	Audit           time.Time
}

func RetentionCutoffs(now time.Time) Cutoffs {
	return Cutoffs{
		RefreshTokens:   now.Add(-30 * 24 * time.Hour),
		RevokedSessions: now.Add(-90 * 24 * time.Hour),
		Uploads:         now.Add(-30 * 24 * time.Hour),
		OperationalJobs: now.Add(-90 * 24 * time.Hour),
		Audit:           now.Add(-365 * 24 * time.Hour),
	}
}

type Counts struct {
	Idempotency        int64
	RateLimits         int64
	RefreshTokens      int64
	SessionsRevoked    int64
	SessionsDeleted    int64
	UploadsExpired     int64
	UploadsDeleted     int64
	MediaJobs          int64
	LibraryScans       int64
	Writebacks         int64
	ObjectCleanupJobs  int64
	TrackDeleteBatches int64
	Audit              int64
}

func (counts Counts) Fields() map[string]any {
	return map[string]any{
		"idempotency":        counts.Idempotency,
		"rateLimits":         counts.RateLimits,
		"refreshTokens":      counts.RefreshTokens,
		"sessionsRevoked":    counts.SessionsRevoked,
		"sessionsDeleted":    counts.SessionsDeleted,
		"uploadsExpired":     counts.UploadsExpired,
		"uploadsDeleted":     counts.UploadsDeleted,
		"mediaJobs":          counts.MediaJobs,
		"libraryScans":       counts.LibraryScans,
		"writebacks":         counts.Writebacks,
		"objectCleanupJobs":  counts.ObjectCleanupJobs,
		"trackDeleteBatches": counts.TrackDeleteBatches,
		"audit":              counts.Audit,
	}
}

type Result struct {
	Ran    bool
	Counts Counts
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type Logger interface {
	Info(message string, fields map[string]any)
}

type NoopLogger struct{}

func (NoopLogger) Info(string, map[string]any) {}

type SlogLogger struct {
	Logger *slog.Logger
}

func (logger SlogLogger) Info(message string, fields map[string]any) {
	if logger.Logger == nil {
		return
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attributes := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		attributes = append(attributes, key, fields[key])
	}
	logger.Logger.Info(message, attributes...)
}

type Executor interface {
	Execute(ctx context.Context, statement string, arguments ...any) (int64, error)
}

type Database interface {
	WithAdvisoryLock(
		ctx context.Context,
		name string,
		operation func(Executor) error,
	) error
}

type Dependencies struct {
	Database Database
	Logger   Logger
	Clock    Clock
}

type Worker struct {
	database Database
	logger   Logger
	clock    Clock

	scheduleMu sync.Mutex
	nextRunAt  time.Time
}

func NewWorker(dependencies Dependencies) (*Worker, error) {
	if dependencies.Database == nil {
		return nil, errors.New("retention database is required")
	}
	if dependencies.Logger == nil {
		dependencies.Logger = NoopLogger{}
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	return &Worker{
		database: dependencies.Database,
		logger:   dependencies.Logger,
		clock:    dependencies.Clock,
	}, nil
}

// RunIfDue runs every retention policy when the hourly schedule is due. A
// forced run bypasses the due check but still advances the regular schedule.
func (worker *Worker) RunIfDue(ctx context.Context, force bool) (Result, error) {
	now := worker.clock.Now()
	worker.scheduleMu.Lock()
	if !force && !worker.nextRunAt.IsZero() && now.Before(worker.nextRunAt) {
		worker.scheduleMu.Unlock()
		return Result{}, nil
	}
	worker.nextRunAt = now.Add(RunInterval)
	worker.scheduleMu.Unlock()

	result := Result{Ran: true}
	err := worker.database.WithAdvisoryLock(ctx, AdvisoryLockName, func(executor Executor) error {
		var applyErr error
		result.Counts, applyErr = apply(ctx, executor, now)
		return applyErr
	})
	if err != nil {
		retryAt := worker.clock.Now().Add(RetryInterval)
		worker.scheduleMu.Lock()
		worker.nextRunAt = retryAt
		worker.scheduleMu.Unlock()
		return result, err
	}
	worker.logger.Info("retention.completed", result.Counts.Fields())
	return result, nil
}

func (worker *Worker) NextRunAt() time.Time {
	worker.scheduleMu.Lock()
	defer worker.scheduleMu.Unlock()
	return worker.nextRunAt
}
