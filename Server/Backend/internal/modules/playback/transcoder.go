package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

var (
	ErrTranscodeFailed   = apperror.Internal("Audio transcoding failed", nil)
	ErrTranscodeTimeout  = apperror.Internal("Audio transcoding timed out", nil)
	ErrMaxTranscodeLimit = apperror.PayloadTooLarge("Server transcoding capacity reached")
)

const (
	defaultTranscodeCacheMaxBytes int64 = 10 * 1024 * 1024 * 1024
	cacheFilePrefix                     = "xymusic-cache-"
	cachePartialMarker                  = ".partial."
	cachePollInterval                   = 25 * time.Millisecond
	hlsDirectedKeyPrefix                = "hls-directed:"
)

type TranscodeMetrics struct {
	ActiveSessions    int64
	TotalStarted      int64
	TotalSuccess      int64
	TotalFailed       int64
	TotalCanceled     int64
	TotalOutputBytes  int64
	AverageTTFBMs     int64
	AverageDurationMs int64
}

type TranscodeSessionParams struct {
	SessionID       string
	UserID          string
	TrackID         string
	SourcePath      string
	CacheKey        string
	Delivery        StreamProtocol
	DurationMs      int64
	CueStartMs      *int64
	CueEndMs        *int64
	StartPositionMs int64
	Profile         OutputProfile
	ExpiresAt       time.Time
}

type sessionState struct {
	params        TranscodeSessionParams
	tempPath      string
	job           *cacheJob
	started       bool
	completed     bool
	err           error
	cancel        context.CancelFunc
	lastAccess    time.Time
	activeReaders int
	readyChan     chan struct{}
	mu            sync.RWMutex
}

// cacheJob is shared by all playback sessions requesting the same source
// version and output profile. Progressive output is private until FFmpeg
// exits successfully; HLS publishes individual init/segment files while the
// job is running.
type cacheJob struct {
	key              string
	params           TranscodeSessionParams
	finalPath        string
	partialPath      string
	finalDir         string
	partialDir       string
	cacheable        bool
	started          bool
	completed        bool
	finalized        bool
	firstReady       bool
	firstReadyClosed bool
	err              error
	cancel           context.CancelFunc
	lastAccess       time.Time
	activeReaders    int
	sessionRefs      int
	readyChan        chan struct{}
	firstReadyChan   chan struct{}
	mu               sync.RWMutex
}

type cacheRecord struct {
	path       string
	jobKey     string
	size       int64
	lastAccess time.Time
}

// StreamHandle owns one reader lease. Progressive handles always point at a
// finite file; HLS handles are individual immutable segment files.
type StreamHandle struct {
	Reader   io.ReadCloser
	Path     string
	Complete bool
	Size     int64
	ModTime  time.Time
	Release  func()
}

// PlaylistHandle owns a short lease while a current HLS playlist snapshot is
// read. Segment files are opened only after they have been atomically
// published by FFmpeg.
type PlaylistHandle struct {
	Path     string
	Content  []byte
	Complete bool
	Release  func()
}

type TranscodeSessionManager struct {
	localMedia       *localmedia.Store
	ffmpegPath       string
	ffmpegThreads    int
	maxConcurrent    int
	idleTimeout      time.Duration
	transcodeTimeout time.Duration
	cacheMaxBytes    int64
	semaphore        chan struct{}
	sessionsMu       sync.RWMutex
	sessions         map[string]*sessionState
	cacheMu          sync.Mutex
	cacheJobs        map[string]*cacheJob
	cacheFiles       map[string]cacheRecord
	cacheBytes       int64
	metrics          TranscodeMetrics
	ttfbTotalMs      int64
	ttfbSamples      int64
	durationTotalMs  int64
	durationSamples  int64
	stopCleanup      chan struct{}
	now              func() time.Time
	closeOnce        sync.Once
	closed           atomic.Bool
}

func (manager *TranscodeSessionManager) recordTranscodeDuration(duration time.Duration) {
	if duration < 0 {
		return
	}
	total := atomic.AddInt64(&manager.durationTotalMs, duration.Milliseconds())
	samples := atomic.AddInt64(&manager.durationSamples, 1)
	atomic.StoreInt64(&manager.metrics.AverageDurationMs, total/samples)
}

func (manager *TranscodeSessionManager) recordTTFB(duration time.Duration) {
	if duration < 0 {
		return
	}
	total := atomic.AddInt64(&manager.ttfbTotalMs, duration.Milliseconds())
	samples := atomic.AddInt64(&manager.ttfbSamples, 1)
	atomic.StoreInt64(&manager.metrics.AverageTTFBMs, total/samples)
}

// The variadic cache limit keeps the old constructor source-compatible for
// integrations/tests while allowing the managed runtime to pass its setting.
func NewTranscodeSessionManager(
	localMedia *localmedia.Store,
	ffmpegPath string,
	ffmpegThreads int,
	maxConcurrent int,
	idleTimeout time.Duration,
	transcodeTimeout time.Duration,
	cacheMaxBytes ...int64,
) (*TranscodeSessionManager, error) {
	if localMedia == nil {
		return nil, errors.New("local media store is required")
	}
	if ffmpegThreads < 0 {
		return nil, errors.New("FFmpeg thread count must not be negative")
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		ffmpegPath = "ffmpeg"
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	if idleTimeout <= 0 {
		idleTimeout = 300 * time.Second
	}
	if transcodeTimeout <= 0 {
		transcodeTimeout = 120 * time.Second
	}
	maxCacheBytes := defaultTranscodeCacheMaxBytes
	if len(cacheMaxBytes) > 0 && cacheMaxBytes[0] > 0 {
		maxCacheBytes = cacheMaxBytes[0]
	}
	manager := &TranscodeSessionManager{
		localMedia:       localMedia,
		ffmpegPath:       ffmpegPath,
		ffmpegThreads:    ffmpegThreads,
		maxConcurrent:    maxConcurrent,
		idleTimeout:      idleTimeout,
		transcodeTimeout: transcodeTimeout,
		cacheMaxBytes:    maxCacheBytes,
		semaphore:        make(chan struct{}, maxConcurrent),
		sessions:         make(map[string]*sessionState),
		cacheJobs:        make(map[string]*cacheJob),
		cacheFiles:       make(map[string]cacheRecord),
		stopCleanup:      make(chan struct{}),
		now:              time.Now,
	}
	manager.loadCacheIndex()
	manager.enforceCacheLimit()
	go manager.runPeriodicCleanup()
	return manager, nil
}

func (manager *TranscodeSessionManager) Close() {
	if manager == nil {
		return
	}
	manager.closeOnce.Do(func() {
		manager.closed.Store(true)
		if manager.stopCleanup != nil {
			close(manager.stopCleanup)
		}

		manager.sessionsMu.Lock()
		sessions := make([]*sessionState, 0, len(manager.sessions))
		for id, session := range manager.sessions {
			sessions = append(sessions, session)
			delete(manager.sessions, id)
		}
		manager.sessionsMu.Unlock()

		for _, session := range sessions {
			var job *cacheJob
			session.mu.Lock()
			job = session.job
			if session.cancel != nil {
				session.cancel()
			}
			if finishSessionLocked(session, context.Canceled) {
				atomic.AddInt64(&manager.metrics.TotalCanceled, 1)
			}
			session.mu.Unlock()
			manager.releaseSessionReference(job)
		}

		manager.cacheMu.Lock()
		jobs := make([]*cacheJob, 0, len(manager.cacheJobs))
		for _, job := range manager.cacheJobs {
			jobs = append(jobs, job)
		}
		manager.cacheMu.Unlock()
		for _, job := range jobs {
			job.mu.Lock()
			if !job.cacheable {
				_ = removePath(jobPartialPathLocked(job))
			}
			if !job.completed {
				if job.cancel != nil {
					job.cancel()
				}
				_ = removePath(jobPartialPathLocked(job))
				if finishJobLocked(job, context.Canceled) {
					atomic.AddInt64(&manager.metrics.TotalCanceled, 1)
				}
			}
			job.mu.Unlock()
		}
		atomic.StoreInt64(&manager.metrics.ActiveSessions, 0)
	})
}

func finishSessionLocked(session *sessionState, err error) bool {
	if session.completed {
		return false
	}
	session.completed = true
	session.err = err
	close(session.readyChan)
	return true
}

func finishJobLocked(job *cacheJob, err error) bool {
	if job.completed {
		return false
	}
	job.completed = true
	job.err = err
	if job.readyChan != nil {
		close(job.readyChan)
	}
	if job.firstReadyChan != nil && !job.firstReadyClosed {
		job.firstReadyClosed = true
		close(job.firstReadyChan)
	}
	return true
}

func signalFirstReadyLocked(job *cacheJob) {
	if job == nil || job.firstReadyClosed {
		return
	}
	job.firstReady = true
	job.firstReadyClosed = true
	if job.firstReadyChan != nil {
		close(job.firstReadyChan)
	}
}

func (manager *TranscodeSessionManager) RegisterSession(params TranscodeSessionParams) {
	if manager == nil || manager.closed.Load() {
		return
	}
	if params.Delivery == "" {
		params.Delivery = StreamProtocolProgressive
	}
	manager.sessionsMu.Lock()
	if manager.closed.Load() {
		manager.sessionsMu.Unlock()
		return
	}
	if previous, exists := manager.sessions[params.SessionID]; exists {
		var previousJob *cacheJob
		previous.mu.Lock()
		previousJob = previous.job
		if previous.cancel != nil {
			previous.cancel()
		}
		if finishSessionLocked(previous, context.Canceled) {
			atomic.AddInt64(&manager.metrics.TotalCanceled, 1)
		}
		previous.mu.Unlock()
		manager.releaseSessionReference(previousJob)
		atomic.AddInt64(&manager.metrics.ActiveSessions, -1)
	}

	var job *cacheJob
	tempPath := params.SourcePath
	completed := false
	var sessionErr error
	if params.Profile.Direct {
		completed = true
	} else {
		job = manager.getOrCreateJob(params)
		tempPath = manager.jobPath(job)
		job.mu.Lock()
		job.sessionRefs++
		job.lastAccess = manager.now().UTC()
		completed = job.completed
		sessionErr = job.err
		job.mu.Unlock()
	}
	session := &sessionState{
		params:     params,
		tempPath:   tempPath,
		job:        job,
		started:    false,
		completed:  completed,
		err:        sessionErr,
		lastAccess: manager.now().UTC(),
		readyChan:  make(chan struct{}),
	}
	if completed {
		close(session.readyChan)
	}
	manager.sessions[params.SessionID] = session
	atomic.AddInt64(&manager.metrics.ActiveSessions, 1)
	manager.sessionsMu.Unlock()
}

// AcquireTranscode exposes the completed progressive representation used by
// HTTP Range responses and offline downloads.
func (manager *TranscodeSessionManager) AcquireTranscode(
	ctx context.Context, sessionID string,
) (string, func(), error) {
	path, err := manager.GetOrStartTranscode(ctx, sessionID)
	if err != nil {
		return "", nil, err
	}
	manager.sessionsMu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !exists {
		return "", nil, apperror.NotFound("Playback session was not found or expired")
	}
	return path, manager.acquireSessionReader(session), nil
}

func (manager *TranscodeSessionManager) ValidateSession(
	sessionID, userID, trackID, quality, codec string,
) error {
	if manager == nil || manager.closed.Load() {
		return ErrInvalidTicket
	}
	manager.sessionsMu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !exists {
		return ErrInvalidTicket
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.params.TrackID != trackID || string(session.params.Profile.Quality) != quality ||
		!strings.EqualFold(session.params.Profile.Codec, codec) {
		return ErrInvalidTicket
	}
	if session.params.UserID != "" && session.params.UserID != userID {
		return ErrInvalidTicket
	}
	if !session.params.ExpiresAt.IsZero() && !manager.now().Before(session.params.ExpiresAt) {
		return ErrExpiredTicket
	}
	return nil
}

func (manager *TranscodeSessionManager) GetOrStartTranscode(ctx context.Context, sessionID string) (string, error) {
	if manager == nil || manager.closed.Load() {
		return "", context.Canceled
	}
	manager.sessionsMu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !exists {
		return "", apperror.NotFound("Playback session was not found or expired")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	session.mu.Lock()
	session.lastAccess = manager.now().UTC()
	if session.params.Delivery == StreamProtocolHLS {
		session.mu.Unlock()
		return "", apperror.Validation("HLS sessions must be opened through the playlist endpoint")
	}
	if session.params.Profile.Direct || session.job == nil || (session.completed && !session.job.completed) {
		path, err := session.tempPath, session.err
		session.mu.Unlock()
		return path, err
	}
	job := session.job
	session.started = true
	session.mu.Unlock()

	if ctx.Err() != nil {
		job.mu.Lock()
		if !job.started && !job.completed {
			if finishJobLocked(job, ctx.Err()) {
				atomic.AddInt64(&manager.metrics.TotalCanceled, 1)
			}
		}
		err := job.err
		job.mu.Unlock()
		if err == nil {
			err = ctx.Err()
		}
		return "", err
	}
	manager.ensureJobStarted(job)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-job.readyChan:
		manager.tryFinalizeJob(job)
		job.mu.RLock()
		err := job.err
		path := manager.jobPathLocked(job)
		job.mu.RUnlock()
		session.mu.Lock()
		wasCompleted := session.completed
		session.completed = true
		session.err = err
		session.tempPath = path
		if !wasCompleted {
			close(session.readyChan)
		}
		session.mu.Unlock()
		if err != nil {
			return "", err
		}
		return path, nil
	}
}

// OpenCompletedStream returns a finite progressive file. A transcoded file is
// intentionally not exposed until FFmpeg has completed, because a growing
// response cannot provide a truthful Content-Length or byte Range contract.
func (manager *TranscodeSessionManager) OpenCompletedStream(ctx context.Context, sessionID string) (*StreamHandle, error) {
	path, release, err := manager.AcquireTranscode(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		release()
		return nil, err
	}
	var releaseOnce sync.Once
	releaseHandle := func() {
		releaseOnce.Do(func() {
			_ = file.Close()
			release()
		})
	}
	stat, err := file.Stat()
	if err != nil {
		releaseHandle()
		return nil, err
	}
	return &StreamHandle{
		Reader:   file,
		Path:     path,
		Complete: true,
		Size:     stat.Size(),
		ModTime:  stat.ModTime(),
		Release:  releaseHandle,
	}, nil
}

// OpenStreamAt is retained for internal callers that used the old API. The
// start offset is no longer used: HTTP Range handling belongs to
// http.ServeContent, which needs the complete finite file before it responds.
func (manager *TranscodeSessionManager) OpenStreamAt(ctx context.Context, sessionID string, start int64) (*StreamHandle, error) {
	if start < 0 {
		return nil, apperror.Validation("stream start must not be negative")
	}
	return manager.OpenCompletedStream(ctx, sessionID)
}

func (manager *TranscodeSessionManager) OpenStream(ctx context.Context, sessionID string) (*StreamHandle, error) {
	return manager.OpenStreamAt(ctx, sessionID, 0)
}

func (manager *TranscodeSessionManager) acquireSessionReader(session *sessionState) func() {
	session.mu.Lock()
	session.activeReaders++
	job := session.job
	if session.completed && (job == nil || !job.completed) {
		job = nil
	}
	if job != nil {
		job.mu.Lock()
		job.activeReaders++
		job.lastAccess = manager.now().UTC()
		job.mu.Unlock()
		manager.touchCacheJob(job)
	}
	session.lastAccess = manager.now().UTC()
	session.mu.Unlock()
	return manager.releaseSessionReaderFunc(session, job, nil)
}

func (manager *TranscodeSessionManager) releaseSessionReaderFunc(session *sessionState, job *cacheJob, file io.Closer) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if file != nil {
				_ = file.Close()
			}
			manager.releaseSessionReader(session, job)
		})
	}
}

func (manager *TranscodeSessionManager) releaseSessionReader(session *sessionState, job *cacheJob) {
	if session != nil {
		session.mu.Lock()
		if session.activeReaders > 0 {
			session.activeReaders--
		}
		session.lastAccess = manager.now().UTC()
		session.mu.Unlock()
	}
	if job != nil {
		job.mu.Lock()
		if job.activeReaders > 0 {
			job.activeReaders--
		}
		job.lastAccess = manager.now().UTC()
		job.mu.Unlock()
		manager.touchCacheJob(job)
		manager.tryFinalizeJob(job)
	}
}

func (manager *TranscodeSessionManager) releaseSessionReference(job *cacheJob) {
	if job == nil {
		return
	}
	job.mu.Lock()
	if job.sessionRefs > 0 {
		job.sessionRefs--
	}
	job.lastAccess = manager.now().UTC()
	job.mu.Unlock()
}

func (manager *TranscodeSessionManager) ensureJobStarted(job *cacheJob) {
	if job == nil || manager.closed.Load() {
		return
	}
	job.mu.Lock()
	if job.started || job.completed {
		job.mu.Unlock()
		return
	}
	job.started = true
	jobCtx, cancel := context.WithTimeout(context.Background(), manager.transcodeTimeout)
	job.cancel = cancel
	job.mu.Unlock()
	go manager.runJob(job, jobCtx)
}

func (manager *TranscodeSessionManager) runJob(job *cacheJob, ctx context.Context) {
	if job.params.Delivery == StreamProtocolHLS {
		manager.runHLSJob(job, ctx)
		return
	}

	select {
	case manager.semaphore <- struct{}{}:
	case <-ctx.Done():
		manager.failJobFromContext(job, ctx)
		return
	}
	defer func() { <-manager.semaphore }()
	if manager.closed.Load() {
		manager.failJob(job, context.Canceled, true)
		return
	}

	if job.partialPath != "" {
		_ = os.Remove(job.partialPath)
		if err := os.MkdirAll(filepath.Dir(job.partialPath), 0o755); err != nil {
			manager.failJob(job, ErrTranscodeFailed, false)
			return
		}
	}
	args := manager.buildFFmpegArgs(job.params, job.partialPath)
	atomic.AddInt64(&manager.metrics.TotalStarted, 1)
	command := exec.CommandContext(ctx, manager.ffmpegPath, args...)
	startTime := manager.now()
	runErr := command.Run()
	manager.recordTranscodeDuration(manager.now().Sub(startTime))
	if runErr != nil {
		manager.failJobFromContext(job, ctx)
		return
	}
	stat, statErr := os.Stat(job.partialPath)
	if statErr != nil || stat.Size() == 0 {
		manager.failJob(job, ErrTranscodeFailed, false)
		return
	}

	job.mu.Lock()
	if job.completed {
		job.mu.Unlock()
		return
	}
	atomic.AddInt64(&manager.metrics.TotalSuccess, 1)
	atomic.AddInt64(&manager.metrics.TotalOutputBytes, stat.Size())
	finishJobLocked(job, nil)
	cancel := job.cancel
	job.cancel = nil
	job.lastAccess = manager.now().UTC()
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	manager.tryFinalizeJob(job)
	manager.enforceCacheLimit()
}

func (manager *TranscodeSessionManager) failJobFromContext(job *cacheJob, ctx context.Context) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		manager.failJob(job, ErrTranscodeTimeout, false)
		return
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		manager.failJob(job, context.Canceled, true)
		return
	}
	manager.failJob(job, ErrTranscodeFailed, false)
}

func (manager *TranscodeSessionManager) failJob(job *cacheJob, err error, canceled bool) {
	job.mu.Lock()
	if !finishJobLocked(job, err) {
		job.mu.Unlock()
		return
	}
	cancel := job.cancel
	job.cancel = nil
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = removePath(manager.jobPartialPath(job))
	if canceled {
		atomic.AddInt64(&manager.metrics.TotalCanceled, 1)
	} else {
		atomic.AddInt64(&manager.metrics.TotalFailed, 1)
	}
	// A failed build must not poison future playback grants for the same
	// cache key. Existing waiters keep the completed error on their job; a
	// later session gets a fresh retryable job.
	manager.cacheMu.Lock()
	if current, exists := manager.cacheJobs[job.key]; exists && current == job {
		delete(manager.cacheJobs, job.key)
	}
	manager.cacheMu.Unlock()
}

func (manager *TranscodeSessionManager) buildFFmpegArgs(params TranscodeSessionParams, outputPath string) []string {
	args := []string{"-nostdin", "-y", "-hide_banner", "-loglevel", "error"}
	startMs := int64(0)
	if params.CueStartMs != nil && *params.CueStartMs > 0 {
		startMs = *params.CueStartMs
		args = append(args, "-ss", fmt.Sprintf("%.3f", float64(startMs)/1000.0))
	}
	args = append(args, "-i", params.SourcePath, "-map", "0:a:0", "-vn")
	if endMs := sourceEndMs(params); endMs > startMs {
		args = append(args, "-t", fmt.Sprintf("%.3f", float64(endMs-startMs)/1000.0))
	}
	if manager.ffmpegThreads > 0 {
		args = append(args, "-threads", strconv.Itoa(manager.ffmpegThreads))
	}

	switch strings.ToLower(params.Profile.Codec) {
	case "flac":
		args = append(args, "-c:a", "flac")
	case "aac":
		// Fragmented MP4 writes ftyp/moov and subsequent moof fragments early;
		// +faststart would wait for the complete file and defeat progressive
		// playback.
		args = append(args, "-c:a", "aac", "-b:a", strconv.Itoa(params.Profile.Bitrate), "-movflags", "+frag_keyframe+empty_moov+default_base_moof")
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", strconv.Itoa(params.Profile.Bitrate))
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", strconv.Itoa(params.Profile.Bitrate))
	default:
		args = append(args, "-c:a", "aac", "-b:a", strconv.Itoa(params.Profile.Bitrate), "-movflags", "+frag_keyframe+empty_moov+default_base_moof")
	}
	if params.Profile.SampleRate != nil && *params.Profile.SampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(*params.Profile.SampleRate))
	}
	return append(args, outputPath)
}

func sourceEndMs(params TranscodeSessionParams) int64 {
	if params.CueEndMs != nil && *params.CueEndMs > 0 {
		return *params.CueEndMs
	}
	startMs := int64(0)
	if params.CueStartMs != nil && *params.CueStartMs > 0 {
		startMs = *params.CueStartMs
	}
	if params.DurationMs > 0 {
		return startMs + params.DurationMs
	}
	return 0
}

func (manager *TranscodeSessionManager) runPeriodicCleanup() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-manager.stopCleanup:
			return
		case <-ticker.C:
			manager.cleanupExpiredSessions()
		}
	}
}

func (manager *TranscodeSessionManager) cleanupExpiredSessions() {
	manager.cleanupExpiredSessionsAt(manager.now().UTC())
}

func (manager *TranscodeSessionManager) cleanupExpiredSessionsAt(now time.Time) {
	if manager.closed.Load() {
		return
	}
	now = now.UTC()
	manager.sessionsMu.Lock()
	for id, session := range manager.sessions {
		session.mu.Lock()
		active := session.started && !session.completed
		readers := session.activeReaders
		job := session.job
		completedCacheable := false
		jobCompleted := false
		var jobErr error
		if job != nil {
			job.mu.RLock()
			jobCompleted = job.completed
			jobErr = job.err
			completedCacheable = job.cacheable && jobCompleted
			job.mu.RUnlock()
		}
		if jobCompleted && !session.completed {
			// HLS requests do not wait on the session ready channel. Mark their
			// session complete when the shared job settles so directed, non-cached
			// jobs can be reclaimed after the normal idle window.
			session.completed = true
			session.err = jobErr
			close(session.readyChan)
		}
		ticketExpired := !session.params.ExpiresAt.IsZero() && !now.Before(session.params.ExpiresAt)
		idleExpired := !active && now.Sub(session.lastAccess) > manager.idleTimeout
		// A completed cached variant is reusable for the whole ticket lifetime.
		// Do not let the short stream-idle timeout turn a normal pause into a
		// 404; the persistent cache still remains governed only by its byte cap.
		expired := readers == 0 && (ticketExpired || idleExpired && !completedCacheable)
		if expired {
			if session.cancel != nil {
				session.cancel()
			}
			if finishSessionLocked(session, context.Canceled) {
				atomic.AddInt64(&manager.metrics.TotalCanceled, 1)
			}
			delete(manager.sessions, id)
			atomic.AddInt64(&manager.metrics.ActiveSessions, -1)
			session.mu.Unlock()
			manager.releaseSessionReference(job)
			continue
		}
		session.mu.Unlock()
	}
	manager.sessionsMu.Unlock()

	manager.cleanupJobs(now)
	manager.enforceCacheLimit()
}

func (manager *TranscodeSessionManager) cleanupJobs(now time.Time) {
	manager.cacheMu.Lock()
	jobs := make([]*cacheJob, 0, len(manager.cacheJobs))
	for _, job := range manager.cacheJobs {
		jobs = append(jobs, job)
	}
	manager.cacheMu.Unlock()
	for _, job := range jobs {
		job.mu.Lock()
		completed := job.completed
		finalized := job.finalized
		readers := job.activeReaders
		refs := job.sessionRefs
		lastAccess := job.lastAccess
		cancel := job.cancel
		job.mu.Unlock()
		// Completed cacheable jobs intentionally have no time-based cleanup.
		// Their files live until enforceCacheLimit removes the oldest eligible
		// entry because the configured byte limit is exceeded.
		if completed && !job.cacheable && readers == 0 && refs == 0 && now.Sub(lastAccess) > manager.idleTimeout {
			_ = removePath(manager.jobPartialPath(job))
			manager.cacheMu.Lock()
			if current, exists := manager.cacheJobs[job.key]; exists && current == job {
				delete(manager.cacheJobs, job.key)
			}
			manager.cacheMu.Unlock()
			continue
		}
		if completed && !finalized {
			manager.tryFinalizeJob(job)
			continue
		}
		if !completed && readers == 0 && refs == 0 && now.Sub(lastAccess) > manager.idleTimeout {
			if cancel != nil {
				cancel()
			}
			manager.failJob(job, context.Canceled, true)
		}
	}
}

func (manager *TranscodeSessionManager) getOrCreateJob(params TranscodeSessionParams) *cacheJob {
	key := strings.TrimSpace(params.CacheKey)
	if key == "" {
		key = "session:" + params.SessionID
	}
	if params.Delivery == "" {
		params.Delivery = StreamProtocolProgressive
	}
	manager.cacheMu.Lock()
	defer manager.cacheMu.Unlock()
	if job, exists := manager.cacheJobs[key]; exists {
		return job
	}
	job := &cacheJob{
		key:            key,
		params:         params,
		cacheable:      strings.TrimSpace(params.CacheKey) != "" && !strings.HasPrefix(params.CacheKey, hlsDirectedKeyPrefix),
		readyChan:      make(chan struct{}),
		firstReadyChan: make(chan struct{}),
		lastAccess:     manager.now().UTC(),
	}
	if job.cacheable {
		if params.Delivery == StreamProtocolHLS {
			job.finalDir, job.partialDir = manager.hlsCachePaths(params.CacheKey)
			job.finalPath = job.finalDir
			if hlsOutputComplete(job.finalDir) {
				job.completed = true
				job.finalized = true
				job.firstReady = true
				job.firstReadyClosed = true
				close(job.readyChan)
				close(job.firstReadyChan)
				if size, sizeErr := directorySize(job.finalDir); sizeErr == nil && size > 0 {
					if existing, exists := manager.cacheFiles[job.finalDir]; exists {
						manager.cacheBytes += size - existing.size
						existing.jobKey = key
						existing.size = size
						manager.cacheFiles[job.finalDir] = existing
					} else {
						info, infoErr := os.Stat(job.finalDir)
						if infoErr == nil {
							manager.cacheFiles[job.finalDir] = cacheRecord{
								path: job.finalDir, jobKey: key, size: size, lastAccess: info.ModTime(),
							}
							manager.cacheBytes += size
						}
					}
				}
			}
		} else {
			job.finalPath, job.partialPath = manager.cachePaths(params.CacheKey, params.Profile.Container)
		}
		if params.Delivery != StreamProtocolHLS {
			if stat, err := os.Stat(job.finalPath); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
				job.completed = true
				job.finalized = true
				close(job.readyChan)
				job.firstReady = true
				job.firstReadyClosed = true
				close(job.firstReadyChan)
				if existing, exists := manager.cacheFiles[job.finalPath]; exists {
					existing.jobKey = key
					manager.cacheFiles[job.finalPath] = existing
				} else {
					manager.cacheFiles[job.finalPath] = cacheRecord{
						path: job.finalPath, jobKey: key, size: stat.Size(), lastAccess: stat.ModTime(),
					}
					manager.cacheBytes += stat.Size()
				}
			}
		}
	} else {
		if params.Delivery == StreamProtocolHLS {
			job.partialDir = filepath.Join(manager.localMedia.TranscodeDirectory(), fmt.Sprintf("%s_%s.hls", params.SessionID, uuid.NewString()[:8]))
		} else {
			job.partialPath = filepath.Join(manager.localMedia.TranscodeDirectory(), fmt.Sprintf("%s_%s.%s", params.SessionID, uuid.NewString()[:8], params.Profile.Container))
		}
	}
	manager.cacheJobs[key] = job
	return job
}

func (manager *TranscodeSessionManager) cachePaths(key, container string) (string, string) {
	digest := sha256.Sum256([]byte(key))
	base := cacheFilePrefix + hex.EncodeToString(digest[:])
	container = strings.ToLower(strings.TrimSpace(container))
	if container == "" || strings.ContainsAny(container, `/\\.`) {
		container = "bin"
	}
	finalPath := filepath.Join(manager.localMedia.TranscodeDirectory(), base+"."+container)
	partialPath := filepath.Join(manager.localMedia.TranscodeDirectory(), base+cachePartialMarker+container)
	return finalPath, partialPath
}

func (manager *TranscodeSessionManager) hlsCachePaths(key string) (string, string) {
	digest := sha256.Sum256([]byte(key))
	base := cacheFilePrefix + hex.EncodeToString(digest[:])
	return filepath.Join(manager.localMedia.TranscodeDirectory(), base+".hls"),
		filepath.Join(manager.localMedia.TranscodeDirectory(), base+cachePartialMarker+"hls")
}

func (manager *TranscodeSessionManager) jobPath(job *cacheJob) string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return manager.jobPathLocked(job)
}

func (manager *TranscodeSessionManager) jobPathLocked(job *cacheJob) string {
	if job.params.Delivery == StreamProtocolHLS {
		if job.finalized && job.finalDir != "" {
			return job.finalDir
		}
		return job.partialDir
	}
	if job.finalized && job.finalPath != "" {
		return job.finalPath
	}
	return job.partialPath
}

func (manager *TranscodeSessionManager) jobPartialPath(job *cacheJob) string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return jobPartialPathLocked(job)
}

func jobPartialPathLocked(job *cacheJob) string {
	if job.params.Delivery == StreamProtocolHLS {
		return job.partialDir
	}
	return job.partialPath
}

func jobFinalPathLocked(job *cacheJob) string {
	if job.params.Delivery == StreamProtocolHLS {
		return job.finalDir
	}
	return job.finalPath
}

func removePath(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (manager *TranscodeSessionManager) tryFinalizeJob(job *cacheJob) {
	if job == nil || !job.cacheable {
		return
	}
	job.mu.Lock()
	if !job.completed || job.finalized || job.activeReaders > 0 {
		job.mu.Unlock()
		return
	}
	if job.params.Delivery == StreamProtocolHLS {
		partialDir, finalDir, key := job.partialDir, job.finalDir, job.key
		if partialDir == "" || finalDir == "" || !hlsOutputComplete(partialDir) {
			job.mu.Unlock()
			return
		}
		if err := os.Rename(partialDir, finalDir); err != nil {
			if hlsOutputComplete(finalDir) {
				if removeErr := os.RemoveAll(partialDir); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					job.mu.Unlock()
					return
				}
			} else {
				job.mu.Unlock()
				return
			}
		}
		size, err := directorySize(finalDir)
		if err != nil || size <= 0 {
			job.mu.Unlock()
			return
		}
		now := manager.now().UTC()
		_ = os.Chtimes(finalDir, now, now)
		job.finalized = true
		job.lastAccess = now
		job.mu.Unlock()
		manager.cacheMu.Lock()
		if old, exists := manager.cacheFiles[finalDir]; exists {
			manager.cacheBytes -= old.size
		}
		manager.cacheFiles[finalDir] = cacheRecord{path: finalDir, jobKey: key, size: size, lastAccess: now}
		manager.cacheBytes += size
		manager.cacheMu.Unlock()
		return
	}
	if job.partialPath == "" {
		job.mu.Unlock()
		return
	}
	partialPath, finalPath, key := job.partialPath, job.finalPath, job.key
	if err := os.Rename(partialPath, finalPath); err != nil {
		if stat, statErr := os.Stat(finalPath); statErr == nil && stat.Size() > 0 {
			if removeErr := os.Remove(partialPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				job.mu.Unlock()
				return
			}
		} else {
			job.mu.Unlock()
			return
		}
	}
	stat, err := os.Stat(finalPath)
	if err != nil || stat.Size() == 0 {
		job.mu.Unlock()
		return
	}
	now := manager.now().UTC()
	// Persist the LRU timestamp on the cache file itself. The in-memory index
	// is rebuilt after a restart, so relying only on cacheRecord.lastAccess
	// would otherwise make old files look recently used again.
	_ = os.Chtimes(finalPath, now, now)
	job.finalized = true
	job.lastAccess = now
	job.mu.Unlock()
	manager.cacheMu.Lock()
	if old, exists := manager.cacheFiles[finalPath]; exists {
		manager.cacheBytes -= old.size
	}
	manager.cacheFiles[finalPath] = cacheRecord{
		path: finalPath, jobKey: key, size: stat.Size(), lastAccess: now,
	}
	manager.cacheBytes += stat.Size()
	manager.cacheMu.Unlock()
}

func (manager *TranscodeSessionManager) loadCacheIndex() {
	entries, err := os.ReadDir(manager.localMedia.TranscodeDirectory())
	if err != nil {
		return
	}
	// A process can stop between writing a partial output and publishing its
	// final name. Partial files are never reusable after restart; removing them
	// here also prevents an abandoned HLS directory from blocking a later job.
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), cacheFilePrefix) {
			continue
		}
		path := filepath.Join(manager.localMedia.TranscodeDirectory(), entry.Name())
		if strings.Contains(entry.Name(), cachePartialMarker) {
			_ = removePath(path)
			continue
		}
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".hls") {
			if !hlsOutputComplete(path) {
				_ = removePath(path)
			}
		}
	}
	manager.cacheMu.Lock()
	defer manager.cacheMu.Unlock()
	indexed := make(map[string]cacheRecord, len(entries))
	var total int64
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), cacheFilePrefix) || strings.Contains(entry.Name(), cachePartialMarker) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(manager.localMedia.TranscodeDirectory(), entry.Name())
		size := info.Size()
		if info.IsDir() {
			size, err = directorySize(path)
		}
		if err != nil || size <= 0 {
			continue
		}
		previous := manager.cacheFiles[path]
		indexed[path] = cacheRecord{
			path: path, jobKey: previous.jobKey, size: size, lastAccess: info.ModTime(),
		}
		total += size
	}
	// Rebuild rather than incrementing the old counters. This makes startup
	// indexing idempotent and also lets a running test/admin refresh reconcile
	// files removed outside the manager.
	manager.cacheFiles = indexed
	manager.cacheBytes = total
}

func (manager *TranscodeSessionManager) touchCacheJob(job *cacheJob) {
	if job == nil {
		return
	}
	job.mu.Lock()
	job.lastAccess = manager.now().UTC()
	path := manager.jobPathLocked(job)
	jobKey := job.key
	job.mu.Unlock()
	if path == "" {
		return
	}
	now := manager.now().UTC()
	shouldTouchFile := false
	manager.cacheMu.Lock()
	if record, exists := manager.cacheFiles[path]; exists {
		record.lastAccess = now
		record.jobKey = jobKey
		manager.cacheFiles[path] = record
		shouldTouchFile = true
	}
	manager.cacheMu.Unlock()
	if shouldTouchFile {
		// Best effort: a timestamp failure must not interrupt playback. The
		// in-memory timestamp still gives correct eviction for this process.
		_ = os.Chtimes(path, now, now)
	}
}

func (manager *TranscodeSessionManager) enforceCacheLimit() {
	for {
		manager.cacheMu.Lock()
		if manager.cacheBytes <= manager.cacheMaxBytes {
			manager.cacheMu.Unlock()
			return
		}
		candidates := make([]cacheCandidate, 0, len(manager.cacheFiles))
		for _, record := range manager.cacheFiles {
			candidates = append(candidates, cacheCandidate{
				record: record,
				job:    manager.cacheJobs[record.jobKey],
			})
		}
		manager.cacheMu.Unlock()
		if len(candidates) == 0 {
			return
		}
		sort.Slice(candidates, func(left, right int) bool {
			if candidates[left].record.lastAccess.Equal(candidates[right].record.lastAccess) {
				return candidates[left].record.path < candidates[right].record.path
			}
			return candidates[left].record.lastAccess.Before(candidates[right].record.lastAccess)
		})

		removed := false
		for _, candidate := range candidates {
			if cacheJobProtected(candidate.job) {
				continue
			}
			// Never hold cacheMu while taking job.mu. Other playback paths update
			// a job timestamp before touching the cache index; keeping the lock
			// order separate avoids a cache-eviction deadlock.
			manager.cacheMu.Lock()
			current, exists := manager.cacheFiles[candidate.record.path]
			if !exists || current.size != candidate.record.size ||
				!current.lastAccess.Equal(candidate.record.lastAccess) {
				manager.cacheMu.Unlock()
				continue
			}
			delete(manager.cacheFiles, current.path)
			manager.cacheBytes -= current.size
			manager.cacheMu.Unlock()

			if err := removePath(current.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				manager.cacheMu.Lock()
				if _, stillPresent := manager.cacheFiles[current.path]; !stillPresent {
					manager.cacheFiles[current.path] = current
					manager.cacheBytes += current.size
				}
				manager.cacheMu.Unlock()
				return
			}
			removed = true
			if candidate.job != nil {
				candidate.job.mu.Lock()
				canDropJob := candidate.job.completed && candidate.job.finalized &&
					jobFinalPathLocked(candidate.job) == current.path && candidate.job.sessionRefs == 0 &&
					candidate.job.activeReaders == 0
				candidate.job.mu.Unlock()
				if canDropJob {
					manager.cacheMu.Lock()
					if currentJob, stillCurrent := manager.cacheJobs[current.jobKey]; stillCurrent && currentJob == candidate.job {
						delete(manager.cacheJobs, current.jobKey)
					}
					manager.cacheMu.Unlock()
				}
			}
			break
		}
		if !removed {
			return
		}
	}
}

type cacheCandidate struct {
	record cacheRecord
	job    *cacheJob
}

func cacheJobProtected(job *cacheJob) bool {
	if job == nil {
		return false
	}
	job.mu.RLock()
	defer job.mu.RUnlock()
	return !job.completed || job.activeReaders > 0 || job.sessionRefs > 0
}
