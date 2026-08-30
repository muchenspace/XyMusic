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
	SessionID  string
	TrackID    string
	SourcePath string
	CacheKey   string
	CueStartMs *int64
	CueEndMs   *int64
	Profile    OutputProfile
	ExpiresAt  time.Time
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
// version and output profile. The partial file is deliberately kept readable
// while FFmpeg is running, so a client can start playback before transcoding
// has finished. The final file is only used after FFmpeg exits successfully.
type cacheJob struct {
	key           string
	params        TranscodeSessionParams
	finalPath     string
	partialPath   string
	cacheable     bool
	started       bool
	completed     bool
	finalized     bool
	err           error
	cancel        context.CancelFunc
	lastAccess    time.Time
	activeReaders int
	sessionRefs   int
	readyChan     chan struct{}
	mu            sync.RWMutex
}

type cacheRecord struct {
	path       string
	jobKey     string
	size       int64
	lastAccess time.Time
}

// StreamHandle owns one reader lease. Complete streams can be passed to
// http.ServeContent; incomplete streams use a growingFileReader that follows
// the FFmpeg output until the shared job reaches EOF.
type StreamHandle struct {
	Reader   io.ReadCloser
	Path     string
	Complete bool
	Size     int64
	ModTime  time.Time
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
	stopCleanup      chan struct{}
	now              func() time.Time
	closeOnce        sync.Once
	closed           atomic.Bool
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
		idleTimeout = 60 * time.Second
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
				_ = os.Remove(job.partialPath)
			}
			if !job.completed {
				if job.cancel != nil {
					job.cancel()
				}
				_ = os.Remove(job.partialPath)
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
	close(job.readyChan)
	return true
}

func (manager *TranscodeSessionManager) RegisterSession(params TranscodeSessionParams) {
	if manager == nil || manager.closed.Load() {
		return
	}
	manager.sessionsMu.Lock()
	defer manager.sessionsMu.Unlock()
	if manager.closed.Load() {
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
}

// AcquireTranscode preserves the old completed-file API. HTTP playback uses
// OpenStreamAt instead, which does not wait for the whole song to finish.
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
	sessionID, trackID, quality, codec string,
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

// OpenStreamAt starts (or joins) a shared transcode and returns immediately.
// The returned growing reader blocks only when it has caught up with FFmpeg's
// current output, allowing HTTP playback to begin with the first encoded
// packets instead of waiting for the entire song.
func (manager *TranscodeSessionManager) OpenStreamAt(ctx context.Context, sessionID string, start int64) (*StreamHandle, error) {
	if manager == nil || manager.closed.Load() {
		return nil, context.Canceled
	}
	if start < 0 {
		return nil, apperror.Validation("stream start must not be negative")
	}
	manager.sessionsMu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !exists {
		return nil, apperror.NotFound("Playback session was not found or expired")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	session.mu.Lock()
	session.lastAccess = manager.now().UTC()
	job := session.job
	path := session.tempPath
	if session.completed && (job == nil || !job.completed) {
		job = nil
	}
	if session.err != nil {
		err := session.err
		session.mu.Unlock()
		return nil, err
	}
	session.activeReaders++
	session.mu.Unlock()

	if job == nil {
		file, err := os.Open(path)
		if err != nil {
			manager.releaseSessionReader(session, nil)
			return nil, err
		}
		stat, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			manager.releaseSessionReader(session, nil)
			return nil, statErr
		}
		return &StreamHandle{
			Reader:   file,
			Path:     path,
			Complete: true,
			Size:     stat.Size(),
			ModTime:  stat.ModTime(),
			Release:  manager.releaseSessionReaderFunc(session, nil, file),
		}, nil
	}

	if ctx.Err() != nil {
		manager.releaseSessionReader(session, job)
		return nil, ctx.Err()
	}
	manager.ensureJobStarted(job)
	manager.tryFinalizeJob(job)

	job.mu.Lock()
	if job.completed && job.err != nil {
		err := job.err
		job.mu.Unlock()
		manager.releaseSessionReader(session, job)
		return nil, err
	}
	job.activeReaders++
	job.lastAccess = manager.now().UTC()
	complete := job.completed
	path = manager.jobPathLocked(job)
	var size int64
	var modTime time.Time
	if complete {
		if stat, statErr := os.Stat(path); statErr == nil {
			size, modTime = stat.Size(), stat.ModTime()
		}
	}
	job.mu.Unlock()
	manager.touchCacheJob(job)

	if complete {
		file, err := os.Open(path)
		if err != nil {
			manager.releaseSessionReader(session, job)
			return nil, err
		}
		return &StreamHandle{
			Reader:   file,
			Path:     path,
			Complete: true,
			Size:     size,
			ModTime:  modTime,
			Release:  manager.releaseSessionReaderFunc(session, job, file),
		}, nil
	}
	return &StreamHandle{
		Reader:   &growingFileReader{job: job, offset: start, ctx: ctx},
		Path:     path,
		Complete: false,
		Release:  manager.releaseSessionReaderFunc(session, job, nil),
	}, nil
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

	select {
	case manager.semaphore <- struct{}{}:
	case <-ctx.Done():
		manager.failJobFromContext(job, ctx)
		return
	case <-time.After(5 * time.Second):
		manager.failJob(job, ErrMaxTranscodeLimit, false)
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
	_ = manager.now().Sub(startTime).Milliseconds()
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
	if job.partialPath != "" {
		_ = os.Remove(job.partialPath)
	}
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
	if params.CueEndMs != nil && *params.CueEndMs > startMs {
		args = append(args, "-t", fmt.Sprintf("%.3f", float64(*params.CueEndMs-startMs)/1000.0))
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
	if manager.closed.Load() {
		return
	}
	now := manager.now().UTC()
	manager.sessionsMu.Lock()
	for id, session := range manager.sessions {
		session.mu.Lock()
		active := session.started && !session.completed
		readers := session.activeReaders
		expired := readers == 0 &&
			((!session.params.ExpiresAt.IsZero() && !now.Before(session.params.ExpiresAt)) ||
				(!active && now.Sub(session.lastAccess) > manager.idleTimeout))
		if expired {
			job := session.job
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
		if completed && !job.cacheable && readers == 0 && refs == 0 && now.Sub(lastAccess) > manager.idleTimeout {
			_ = os.Remove(job.partialPath)
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
	manager.cacheMu.Lock()
	defer manager.cacheMu.Unlock()
	if job, exists := manager.cacheJobs[key]; exists {
		return job
	}
	job := &cacheJob{
		key:        key,
		params:     params,
		cacheable:  strings.TrimSpace(params.CacheKey) != "",
		readyChan:  make(chan struct{}),
		lastAccess: manager.now().UTC(),
	}
	if job.cacheable {
		job.finalPath, job.partialPath = manager.cachePaths(params.CacheKey, params.Profile.Container)
		if stat, err := os.Stat(job.finalPath); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
			job.completed = true
			job.finalized = true
			close(job.readyChan)
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
	} else {
		job.partialPath = filepath.Join(manager.localMedia.TranscodeDirectory(), fmt.Sprintf("%s_%s.%s", params.SessionID, uuid.NewString()[:8], params.Profile.Container))
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

func (manager *TranscodeSessionManager) jobPath(job *cacheJob) string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return manager.jobPathLocked(job)
}

func (manager *TranscodeSessionManager) jobPathLocked(job *cacheJob) string {
	if job.finalized && job.finalPath != "" {
		return job.finalPath
	}
	return job.partialPath
}

func (manager *TranscodeSessionManager) tryFinalizeJob(job *cacheJob) {
	if job == nil || !job.cacheable {
		return
	}
	job.mu.Lock()
	if !job.completed || job.finalized || job.partialPath == "" {
		job.mu.Unlock()
		return
	}
	partialPath, finalPath, key := job.partialPath, job.finalPath, job.key
	job.mu.Unlock()

	if err := os.Rename(partialPath, finalPath); err != nil {
		if stat, statErr := os.Stat(finalPath); statErr == nil && stat.Size() > 0 {
			_ = os.Remove(partialPath)
		} else {
			return
		}
	}
	stat, err := os.Stat(finalPath)
	if err != nil || stat.Size() == 0 {
		return
	}
	job.mu.Lock()
	job.finalized = true
	job.lastAccess = manager.now().UTC()
	job.mu.Unlock()
	manager.cacheMu.Lock()
	if old, exists := manager.cacheFiles[finalPath]; exists {
		manager.cacheBytes -= old.size
	}
	manager.cacheFiles[finalPath] = cacheRecord{
		path: finalPath, jobKey: key, size: stat.Size(), lastAccess: manager.now().UTC(),
	}
	manager.cacheBytes += stat.Size()
	manager.cacheMu.Unlock()
}

func (manager *TranscodeSessionManager) loadCacheIndex() {
	entries, err := os.ReadDir(manager.localMedia.TranscodeDirectory())
	if err != nil {
		return
	}
	manager.cacheMu.Lock()
	defer manager.cacheMu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), cacheFilePrefix) || strings.Contains(entry.Name(), cachePartialMarker) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() <= 0 {
			continue
		}
		path := filepath.Join(manager.localMedia.TranscodeDirectory(), entry.Name())
		manager.cacheFiles[path] = cacheRecord{path: path, size: info.Size(), lastAccess: info.ModTime()}
		manager.cacheBytes += info.Size()
	}
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
	manager.cacheMu.Lock()
	if record, exists := manager.cacheFiles[path]; exists {
		record.lastAccess = manager.now().UTC()
		record.jobKey = jobKey
		manager.cacheFiles[path] = record
	}
	manager.cacheMu.Unlock()
}

func (manager *TranscodeSessionManager) enforceCacheLimit() {
	for {
		manager.cacheMu.Lock()
		if manager.cacheBytes <= manager.cacheMaxBytes {
			manager.cacheMu.Unlock()
			return
		}
		candidates := make([]cacheRecord, 0, len(manager.cacheFiles))
		for _, record := range manager.cacheFiles {
			candidates = append(candidates, record)
		}
		manager.cacheMu.Unlock()
		if len(candidates) == 0 {
			return
		}
		oldest := cacheRecord{}
		found := false
		for _, candidate := range candidates {
			manager.cacheMu.Lock()
			job := manager.cacheJobs[candidate.jobKey]
			manager.cacheMu.Unlock()
			if job != nil {
				job.mu.RLock()
				protected := !job.completed || job.activeReaders > 0 || job.sessionRefs > 0
				job.mu.RUnlock()
				if protected {
					continue
				}
			}
			if !found || candidate.lastAccess.Before(oldest.lastAccess) {
				oldest, found = candidate, true
			}
		}
		if !found {
			return
		}
		manager.cacheMu.Lock()
		current, exists := manager.cacheFiles[oldest.path]
		if exists {
			delete(manager.cacheFiles, oldest.path)
			manager.cacheBytes -= current.size
			if job := manager.cacheJobs[current.jobKey]; job != nil {
				job.mu.RLock()
				protected := !job.completed || job.activeReaders > 0 || job.sessionRefs > 0
				job.mu.RUnlock()
				if protected {
					manager.cacheFiles[oldest.path] = current
					manager.cacheBytes += current.size
					manager.cacheMu.Unlock()
					continue
				}
			}
		}
		manager.cacheMu.Unlock()
		if !exists {
			continue
		}
		if err := os.Remove(oldest.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			manager.cacheMu.Lock()
			if _, stillPresent := manager.cacheFiles[oldest.path]; !stillPresent {
				manager.cacheFiles[oldest.path] = current
				manager.cacheBytes += current.size
			}
			manager.cacheMu.Unlock()
			return
		}
		manager.cacheMu.Lock()
		if job := manager.cacheJobs[oldest.jobKey]; job != nil {
			job.mu.Lock()
			if job.completed && job.finalized && job.finalPath == oldest.path && job.sessionRefs == 0 && job.activeReaders == 0 {
				delete(manager.cacheJobs, oldest.jobKey)
			}
			job.mu.Unlock()
		}
		manager.cacheMu.Unlock()
	}
}

type growingFileReader struct {
	job      *cacheJob
	offset   int64
	ctx      context.Context
	file     *os.File
	filePath string
	closed   atomic.Bool
	mu       sync.Mutex
}

func (reader *growingFileReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	for {
		if reader.closed.Load() {
			return 0, io.ErrClosedPipe
		}
		if err := reader.ctx.Err(); err != nil {
			return 0, err
		}
		reader.job.mu.RLock()
		path := reader.jobPathLocked()
		completed := reader.job.completed
		jobErr := reader.job.err
		readyChan := reader.job.readyChan
		reader.job.mu.RUnlock()
		if jobErr != nil {
			return 0, jobErr
		}
		reader.mu.Lock()
		if path != "" && (reader.file == nil || reader.filePath != path) {
			if reader.file != nil {
				_ = reader.file.Close()
			}
			file, err := os.Open(path)
			if err == nil {
				reader.file, reader.filePath = file, path
			}
		}
		var n int
		var readErr error
		if reader.file != nil {
			n, readErr = reader.file.ReadAt(buffer, reader.offset)
		}
		reader.mu.Unlock()
		if n > 0 {
			reader.offset += int64(n)
			reader.job.mu.Lock()
			reader.job.lastAccess = time.Now().UTC()
			reader.job.mu.Unlock()
			return n, nil
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, os.ErrClosed) {
			return 0, readErr
		}
		if completed {
			return 0, io.EOF
		}
		timer := time.NewTimer(cachePollInterval)
		select {
		case <-reader.ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return 0, reader.ctx.Err()
		case <-readyChan:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (reader *growingFileReader) jobPathLocked() string {
	if reader.job.finalized && reader.job.finalPath != "" {
		return reader.job.finalPath
	}
	return reader.job.partialPath
}

func (reader *growingFileReader) Close() error {
	if reader.closed.Swap(true) {
		return nil
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.file != nil {
		err := reader.file.Close()
		reader.file = nil
		return err
	}
	return nil
}
