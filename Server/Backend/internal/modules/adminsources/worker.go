package adminsources

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
)

var ErrScanCancelled = errors.New("library scan was cancelled")

type WorkerOptions struct {
	Store         WorkerStore
	Scanner       Scanner
	RootDirectory string
	DefaultRoot   config.LocalLibrary
	WorkerID      string
	Lease         time.Duration
	Heartbeat     time.Duration
	ProgressWrite time.Duration
	Now           func() time.Time
}

type Worker struct {
	store         WorkerStore
	scanner       Scanner
	rootDirectory string
	defaultRoot   config.LocalLibrary
	workerID      string
	lease         time.Duration
	heartbeat     time.Duration
	progressWrite time.Duration
	now           func() time.Time
}

var _ ScanExecutor = (*Worker)(nil)

func NewWorker(options WorkerOptions) (*Worker, error) {
	if options.Store == nil {
		return nil, errors.New("music source scan worker store is required")
	}
	if options.Scanner == nil {
		return nil, errors.New("music source scanner is required")
	}
	root := strings.TrimSpace(options.RootDirectory)
	if root == "" {
		return nil, errors.New("music source scan executable root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.New("resolve music source scan executable root: " + err.Error())
	}
	if options.WorkerID == "" {
		options.WorkerID = "scan-" + uuid.NewString()
	}
	if options.Lease == 0 {
		options.Lease = 120 * time.Second
	}
	if options.Heartbeat == 0 {
		options.Heartbeat = 30 * time.Second
	}
	if options.ProgressWrite == 0 {
		options.ProgressWrite = 500 * time.Millisecond
	}
	if options.Lease < time.Second || options.Heartbeat < 10*time.Millisecond || options.Heartbeat >= options.Lease {
		return nil, errors.New("music source scan lease and heartbeat intervals are invalid")
	}
	if options.ProgressWrite < 0 {
		return nil, errors.New("music source scan progress interval is invalid")
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Worker{
		store: options.Store, scanner: options.Scanner, rootDirectory: filepath.Clean(absolute),
		defaultRoot: options.DefaultRoot, workerID: options.WorkerID, lease: options.Lease,
		heartbeat: options.Heartbeat, progressWrite: options.ProgressWrite, now: options.Now,
	}, nil
}

func (worker *Worker) Initialize(ctx context.Context) error {
	now := worker.now()
	if err := worker.store.InitializeScans(ctx, now); err != nil {
		return err
	}
	directory := worker.defaultRoot.Directory
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(worker.rootDirectory, directory)
	}
	name := worker.defaultRoot.Name
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(filepath.Clean(directory))
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = "Music"
		}
	}
	mutation, err := validateRootInput(worker.rootDirectory, RootMutation{
		Name: name, Path: directory, Mode: RootMode(worker.defaultRoot.Mode),
		Enabled: worker.defaultRoot.Enabled, ScanOnStartup: worker.defaultRoot.SyncOnStartup,
		ScanIntervalMinutes: cloneInt(worker.defaultRoot.ScanIntervalMinutes),
		IncludePatterns:     cloneStrings(worker.defaultRoot.IncludePatterns),
		ExcludePatterns:     cloneStrings(worker.defaultRoot.ExcludePatterns),
	})
	if err != nil {
		return err
	}
	if _, err := worker.store.EnsureDefaultRoot(ctx, mutation); err != nil {
		return err
	}
	rootIDs, err := worker.store.StartupRootIDs(ctx)
	if err != nil {
		return err
	}
	for _, rootID := range rootIDs {
		if _, err := worker.store.EnqueueScan(ctx, EnqueueScanCommand{RootID: rootID, Deduplicate: true}); err != nil {
			return err
		}
	}
	return nil
}

func (worker *Worker) RunNextScan(ctx context.Context) (bool, error) {
	if err := worker.store.EnqueueScheduledScans(ctx, worker.now()); err != nil {
		return false, err
	}
	claim, err := worker.store.ClaimNextScan(ctx, worker.workerID, worker.now(), worker.lease)
	if err != nil || claim == nil {
		return false, err
	}
	if claim.Run.Status == ScanStatusCancelled {
		return true, nil
	}
	if claim.Run.AttemptID == nil {
		return true, errors.New("claimed music source scan has no attempt id")
	}
	attemptID := *claim.Run.AttemptID
	var ownershipLost atomic.Bool
	var cancellationSeen atomic.Bool
	heartbeatContext, stopHeartbeat := context.WithCancel(context.WithoutCancel(ctx))
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(worker.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatContext.Done():
				return
			case <-ticker.C:
				beatContext, cancel := context.WithTimeout(heartbeatContext, min(worker.heartbeat, 10*time.Second))
				owned, beatErr := worker.store.HeartbeatScan(
					beatContext, claim.Run.ID, attemptID, worker.workerID, worker.now(), worker.lease,
				)
				cancel()
				if beatErr == nil && !owned {
					ownershipLost.Store(true)
					return
				}
			}
		}
	}()
	defer func() {
		stopHeartbeat()
		<-heartbeatDone
	}()

	var lastProgress atomic.Int64
	result, scanErr := worker.scanner.Scan(ctx, ScanInput{
		ScanRunID: claim.Run.ID, RootID: claim.Root.ID, Directory: claim.Root.Path,
		IncludePatterns: cloneStrings(claim.Root.IncludePatterns),
		ExcludePatterns: cloneStrings(claim.Root.ExcludePatterns),
		IsCancelled: func(callbackContext context.Context) (bool, error) {
			if ctx.Err() != nil || ownershipLost.Load() {
				return true, nil
			}
			cancelled, owned, err := worker.store.ScanControl(
				callbackContext, claim.Run.ID, attemptID, worker.workerID,
			)
			if err != nil {
				return false, err
			}
			if !owned {
				ownershipLost.Store(true)
			}
			if cancelled && owned {
				cancellationSeen.Store(true)
			}
			return cancelled, nil
		},
		OnProgress: func(callbackContext context.Context, progress ScanProgress) error {
			now := worker.now()
			last := lastProgress.Load()
			if last != 0 && worker.progressWrite > 0 && now.UnixNano()-last < worker.progressWrite.Nanoseconds() {
				return nil
			}
			lastProgress.Store(now.UnixNano())
			owned, err := worker.store.UpdateScanProgress(
				callbackContext, claim.Run.ID, attemptID, worker.workerID, progress, now,
			)
			if err == nil && !owned {
				ownershipLost.Store(true)
			}
			return err
		},
	})

	finalContext, cancelFinal := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelFinal()
	interrupted := ctx.Err() != nil || ownershipLost.Load()
	if scanErr == nil && !interrupted {
		_, err := worker.store.CompleteScan(finalContext, *claim, attemptID, worker.workerID, result, worker.now())
		return true, err
	}
	status := ScanStatusFailed
	var lastError *string
	if interrupted {
		status = ScanStatusPending
	} else if errors.Is(scanErr, ErrScanCancelled) || cancellationSeen.Load() {
		status = ScanStatusCancelled
	} else {
		value := truncateError(scanErr)
		lastError = &value
	}
	_, finalizeErr := worker.store.FinalizeScanFailure(
		finalContext, *claim, attemptID, worker.workerID, status, lastError, worker.now(),
	)
	if finalizeErr != nil {
		return true, finalizeErr
	}
	return true, nil
}

func truncateError(err error) string {
	if err == nil {
		return "Library scan failed"
	}
	value := err.Error()
	if utf8.RuneCountInString(value) <= 4000 {
		return value
	}
	return string([]rune(value)[:4000])
}
