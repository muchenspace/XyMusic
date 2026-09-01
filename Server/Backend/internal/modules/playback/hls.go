package playback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"xymusic/server/internal/shared/apperror"
)

const (
	hlsPlaylistName        = "index.m3u8"
	hlsInitName            = "init.mp4"
	hlsSegmentTime         = 0.5
	hlsStartupTimeout      = 15 * time.Second
	hlsSegmentWaitTimeout  = 15 * time.Second
	hlsPollInterval        = 50 * time.Millisecond
	hlsReadRetryTimeout    = 1 * time.Second
	hlsPlaylistReadTimeout = 200 * time.Millisecond
)

var hlsSegmentNamePattern = regexp.MustCompile(`^segment_[0-9]{6}\.m4s$`)

func (manager *TranscodeSessionManager) OpenHLS(ctx context.Context, sessionID string) (*PlaylistHandle, error) {
	if manager == nil || manager.closed.Load() {
		return nil, context.Canceled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	session, job, err := manager.hlsJobForSession(sessionID)
	if err != nil {
		return nil, err
	}
	manager.acquireHLSReader(session, job)
	manager.ensureJobStarted(job)
	waitCtx, cancel := boundedHLSContext(ctx, hlsStartupTimeout)
	defer cancel()
	if err := waitForHLSReady(waitCtx, job); err != nil {
		manager.releaseSessionReader(session, job)
		return nil, err
	}
	job.mu.RLock()
	path := jobPlaylistPathLocked(job)
	complete := job.completed && job.err == nil
	job.mu.RUnlock()
	if !hlsOutputReady(filepath.Dir(path)) {
		manager.releaseSessionReader(session, job)
		return nil, ErrTranscodeFailed
	}
	playlist, err := readFileWithRetry(path, hlsStartupTimeout)
	if err != nil || len(playlist) == 0 {
		manager.releaseSessionReader(session, job)
		if err != nil {
			return nil, fmt.Errorf("read HLS playlist: %w", err)
		}
		return nil, ErrTranscodeFailed
	}
	return &PlaylistHandle{
		Path:     path,
		Content:  playlist,
		Complete: complete,
		Release:  manager.releaseSessionReaderFunc(session, job, nil),
	}, nil
}

func (manager *TranscodeSessionManager) OpenHLSSegment(
	ctx context.Context,
	sessionID string,
	name string,
) (*StreamHandle, error) {
	if manager == nil || manager.closed.Load() {
		return nil, context.Canceled
	}
	if !validHLSSegmentName(name) {
		return nil, apperror.NotFound("Playback segment was not found")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	session, job, err := manager.hlsJobForSession(sessionID)
	if err != nil {
		return nil, err
	}
	manager.acquireHLSReader(session, job)
	manager.ensureJobStarted(job)
	deadline := time.Now().Add(hlsSegmentWaitTimeout)
	for {
		if err := ctx.Err(); err != nil {
			manager.releaseSessionReader(session, job)
			return nil, err
		}
		job.mu.RLock()
		outputDir := jobOutputDirLocked(job)
		path := filepath.Join(outputDir, name)
		completed := job.completed
		jobErr := job.err
		job.mu.RUnlock()
		if jobErr != nil {
			manager.releaseSessionReader(session, job)
			return nil, jobErr
		}
		if hlsSegmentPublished(outputDir, name) {
			data, readErr := readFileWithRetry(path, hlsReadRetryTimeout)
			if readErr == nil && hlsDataIsComplete(data, name) {
				return &StreamHandle{
					Reader:   &readSeekCloser{Reader: bytes.NewReader(data)},
					Path:     path,
					Complete: true,
					Size:     int64(len(data)),
					Release:  manager.releaseSessionReaderFunc(session, job, nil),
				}, nil
			}
		}
		if completed {
			manager.releaseSessionReader(session, job)
			return nil, os.ErrNotExist
		}
		if time.Now().After(deadline) {
			manager.releaseSessionReader(session, job)
			return nil, os.ErrNotExist
		}
		timer := time.NewTimer(hlsPollInterval)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			manager.releaseSessionReader(session, job)
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (manager *TranscodeSessionManager) hlsJobForSession(sessionID string) (*sessionState, *cacheJob, error) {
	manager.sessionsMu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !exists {
		return nil, nil, apperror.NotFound("Playback session was not found or expired")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.params.Delivery != StreamProtocolHLS || session.job == nil {
		return nil, nil, apperror.Validation("Playback session is not an HLS stream")
	}
	if session.err != nil {
		return nil, nil, session.err
	}
	session.started = true
	session.lastAccess = manager.now().UTC()
	return session, session.job, nil
}

func (manager *TranscodeSessionManager) sessionCacheKey(sessionID string) (string, error) {
	manager.sessionsMu.RLock()
	session, exists := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !exists {
		return "", apperror.NotFound("Playback session was not found or expired")
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.params.CacheKey, nil
}

func (manager *TranscodeSessionManager) acquireHLSReader(session *sessionState, job *cacheJob) {
	session.mu.Lock()
	session.activeReaders++
	session.lastAccess = manager.now().UTC()
	session.mu.Unlock()
	job.mu.Lock()
	job.activeReaders++
	job.lastAccess = manager.now().UTC()
	job.mu.Unlock()
	manager.touchCacheJob(job)
}

func waitForHLSReady(ctx context.Context, job *cacheJob) error {
	job.mu.RLock()
	firstReady := job.firstReady
	firstReadyChan := job.firstReadyChan
	finishedChan := job.readyChan
	jobErr := job.err
	job.mu.RUnlock()
	if firstReady {
		return jobErr
	}
	if jobErr != nil {
		return jobErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-finishedChan:
		job.mu.RLock()
		err := job.err
		job.mu.RUnlock()
		return err
	case <-firstReadyChan:
		job.mu.RLock()
		err := job.err
		job.mu.RUnlock()
		return err
	}
}

func boundedHLSContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func validHLSSegmentName(name string) bool {
	return name == hlsInitName || hlsSegmentNamePattern.MatchString(name)
}

func hlsSegmentIsComplete(outputDir, name string) bool {
	data, err := readFileWithRetry(filepath.Join(outputDir, name), hlsReadRetryTimeout)
	if err != nil {
		return false
	}
	return hlsDataIsComplete(data, name)
}

func hlsDataIsComplete(data []byte, name string) bool {
	if name == hlsInitName {
		return bytes.Contains(data, []byte("ftyp")) && bytes.Contains(data, []byte("moov"))
	}
	return bytes.Contains(data, []byte("moof")) && bytes.Contains(data, []byte("mdat"))
}

func readFileWithRetry(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		content, err := os.ReadFile(path)
		if err == nil {
			return content, nil
		}
		if errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(hlsPollInterval)
	}
}

func jobOutputDirLocked(job *cacheJob) string {
	if job.finalized && job.finalDir != "" {
		return job.finalDir
	}
	return job.partialDir
}

func jobPlaylistPathLocked(job *cacheJob) string {
	return filepath.Join(jobOutputDirLocked(job), hlsPlaylistName)
}

func hlsPlaylistReady(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	value := string(content)
	return strings.Contains(value, "#EXT-X-MAP:") && len(hlsMediaNames(content)) > 0
}

func hlsMediaNames(content []byte) []string {
	names := make([]string, 0, 1)
	for _, line := range strings.Split(string(content), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		if validHLSSegmentName(name) && name != hlsInitName {
			names = append(names, name)
		}
	}
	return names
}

func hlsSegmentPublished(outputDir, name string) bool {
	content, err := readFileWithRetry(filepath.Join(outputDir, hlsPlaylistName), hlsPlaylistReadTimeout)
	if err != nil || len(content) == 0 {
		return false
	}
	if name == hlsInitName {
		return hlsMapName(content) == name
	}
	for _, published := range hlsMediaNames(content) {
		if published == name {
			return true
		}
	}
	return false
}

func hlsMapName(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-MAP:") {
			continue
		}
		const prefix = `URI="`
		start := strings.Index(line, prefix)
		if start < 0 {
			return ""
		}
		valueStart := start + len(prefix)
		valueEnd := strings.Index(line[valueStart:], `"`)
		if valueEnd < 0 {
			return ""
		}
		return line[valueStart : valueStart+valueEnd]
	}
	return ""
}

// hlsOutputReady is intentionally stricter than checking for a playlist and
// any *.m4s file. FFmpeg publishes the playlist and media files independently;
// the player must not receive a playlist whose init segment or first media
// segment is still being written.
func hlsOutputReady(outputDir string) bool {
	playlistPath := filepath.Join(outputDir, hlsPlaylistName)
	content, err := readFileWithRetry(playlistPath, hlsPlaylistReadTimeout)
	if err != nil || len(content) == 0 || !hlsPlaylistReady(content) {
		return false
	}
	names := hlsMediaNames(content)
	if len(names) == 0 {
		return false
	}
	if !hlsSegmentIsComplete(outputDir, hlsInitName) || !hlsSegmentIsComplete(outputDir, names[0]) {
		return false
	}
	return true
}

func directorySize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (manager *TranscodeSessionManager) runHLSJob(job *cacheJob, ctx context.Context) {
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

	job.mu.RLock()
	partialDir := job.partialDir
	playlistPath := filepath.Join(partialDir, hlsPlaylistName)
	params := job.params
	job.mu.RUnlock()
	if err := removePath(partialDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		manager.failJob(job, ErrTranscodeFailed, false)
		return
	}
	if err := os.MkdirAll(partialDir, 0o755); err != nil {
		manager.failJob(job, ErrTranscodeFailed, false)
		return
	}
	sourceStartMs := params.StartPositionMs
	if params.CueStartMs != nil && *params.CueStartMs > 0 {
		sourceStartMs += *params.CueStartMs
	}
	args := manager.buildHLSFFmpegArgs(params, partialDir, playlistPath, sourceStartMs)
	atomic.AddInt64(&manager.metrics.TotalStarted, 1)
	startTime := manager.now()
	command := exec.CommandContext(ctx, manager.ffmpegPath, args...)
	if err := command.Start(); err != nil {
		manager.recordTranscodeDuration(manager.now().Sub(startTime))
		manager.failJob(job, ErrTranscodeFailed, false)
		return
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- command.Wait() }()
	ticker := time.NewTicker(hlsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case runErr := <-waitResult:
			manager.recordTranscodeDuration(manager.now().Sub(startTime))
			if runErr != nil {
				manager.failJobFromContext(job, ctx)
				return
			}
			if !hlsOutputReady(partialDir) {
				manager.failJob(job, ErrTranscodeFailed, false)
				return
			}
			manager.signalHLSReady(job, startTime)
			manager.completeJob(job)
			return
		case <-ticker.C:
			job.mu.RLock()
			alreadyReady := job.firstReady
			job.mu.RUnlock()
			if !alreadyReady && hlsOutputReady(partialDir) {
				manager.signalHLSReady(job, startTime)
			}
		case <-ctx.Done():
			<-waitResult
			manager.recordTranscodeDuration(manager.now().Sub(startTime))
			manager.failJobFromContext(job, ctx)
			return
		}
	}
}

func (manager *TranscodeSessionManager) signalHLSReady(job *cacheJob, startTime time.Time) {
	job.mu.Lock()
	wasReady := job.firstReady
	if !wasReady {
		signalFirstReadyLocked(job)
	}
	job.mu.Unlock()
	if !wasReady {
		manager.recordTTFB(manager.now().Sub(startTime))
	}
}

func (manager *TranscodeSessionManager) completeJob(job *cacheJob) {
	job.mu.Lock()
	if job.completed {
		job.mu.Unlock()
		return
	}
	atomic.AddInt64(&manager.metrics.TotalSuccess, 1)
	size, _ := directorySize(jobOutputDirLocked(job))
	atomic.AddInt64(&manager.metrics.TotalOutputBytes, size)
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

func (manager *TranscodeSessionManager) buildHLSFFmpegArgs(
	params TranscodeSessionParams,
	outputDir string,
	playlistPath string,
	sourceStartMs int64,
) []string {
	args := []string{"-nostdin", "-y", "-hide_banner", "-loglevel", "error"}
	startMs := sourceStartMs
	if startMs < 0 {
		startMs = 0
	}
	if startMs > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", float64(startMs)/1000.0))
	}
	args = append(args, "-i", params.SourcePath, "-map", "0:a:0", "-vn")
	if endMs := sourceEndMs(params); endMs > startMs {
		args = append(args, "-t", fmt.Sprintf("%.3f", float64(endMs-startMs)/1000.0))
	}
	if manager.ffmpegThreads > 0 {
		args = append(args, "-threads", strconv.Itoa(manager.ffmpegThreads))
	}
	args = append(args,
		"-c:a", "aac",
		"-b:a", strconv.Itoa(params.Profile.Bitrate),
		"-ar", strconv.Itoa(nonZeroSampleRate(params.Profile.SampleRate)),
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%.3f", hlsSegmentTime),
		"-hls_playlist_type", "event",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments+temp_file",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", hlsInitName,
		"-hls_segment_filename", filepath.ToSlash(filepath.Join(outputDir, "segment_%06d.m4s")),
		filepath.ToSlash(playlistPath),
	)
	return args
}

func nonZeroSampleRate(value *int) int {
	if value != nil && *value > 0 {
		return *value
	}
	return 44100
}

type readSeekCloser struct {
	*bytes.Reader
}

func (reader *readSeekCloser) Close() error {
	return nil
}
