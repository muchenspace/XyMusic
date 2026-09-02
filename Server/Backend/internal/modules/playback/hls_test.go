package playback

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/platform/localmedia"
)

func TestHLSCacheAdoptionRequiresEndTag(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "cache.hls")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeHLSFixture(t, outputDir, false)

	// A partially published (event, no end tag) directory is playable but is
	// NOT a complete cache entry ...
	if !hlsOutputReady(outputDir) {
		t.Fatal("partial HLS output is not playable")
	}
	if hlsOutputComplete(outputDir) {
		t.Fatal("partial HLS output was treated as a complete cache entry")
	}

	// ... until the transcode completes and writes the end tag.
	writeHLSFixture(t, outputDir, true)
	if !hlsOutputComplete(outputDir) {
		t.Fatal("completed HLS output was not recognized as complete")
	}
}

func TestGetOrCreateJobDoesNotAdoptAnEndTagLessCacheDirectory(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		10*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-that-must-not-run", 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	cacheKey := "track:fake:checksum:STANDARD:aac:m4a:HLS:-1:-1:1000"
	finalDir, _ := manager.hlsCachePaths(cacheKey)
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeHLSFixture(t, finalDir, false)
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: uuid.NewString(), TrackID: uuid.NewString(),
		SourcePath: "unused.wav", CacheKey: cacheKey, Delivery: StreamProtocolHLS, DurationMs: 1_000,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	manager.cacheMu.Lock()
	job := manager.cacheJobs[cacheKey]
	manager.cacheMu.Unlock()
	if job == nil {
		t.Fatal("cache job was not created")
	}
	job.mu.RLock()
	completed, finalized := job.completed, job.finalized
	job.mu.RUnlock()
	if completed || finalized {
		t.Fatal("end-tag-less cache directory was adopted as a completed job")
	}
}

func writeHLSFixture(t *testing.T, outputDir string, complete bool) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(outputDir, hlsInitName), []byte("ftyp....moov...."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "segment_000000.m4s"), []byte("moof....mdat...."), 0o644); err != nil {
		t.Fatal(err)
	}
	playlist := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:0.500,\nsegment_000000.m4s\n"
	if complete {
		playlist += "#EXT-X-ENDLIST\n"
	}
	if err := os.WriteFile(filepath.Join(outputDir, hlsPlaylistName), []byte(playlist), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompletedHLSVariantIsPersistedAndReusedByNewManager(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		10*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 44100); err != nil {
		t.Fatal(err)
	}
	profile := OutputProfile{
		Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
	}
	cacheKey := "track:hls-persist"
	first, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	firstSession := uuid.NewString()
	first.RegisterSession(TranscodeSessionParams{
		SessionID: firstSession, TrackID: uuid.NewString(), SourcePath: sourcePath,
		CacheKey: cacheKey, Delivery: StreamProtocolHLS, DurationMs: 1_000,
		Profile: profile, ExpiresAt: time.Now().Add(time.Minute),
	})
	playlist, err := first.OpenHLS(context.Background(), firstSession)
	if err != nil {
		first.Close()
		t.Fatalf("first HLS playlist: %v", err)
	}
	playlist.Release()
	finalDir, _ := first.hlsCachePaths(cacheKey)
	deadline := time.Now().Add(10 * time.Second)
	for {
		first.cacheMu.Lock()
		job := first.cacheJobs[cacheKey]
		first.cacheMu.Unlock()
		if job != nil {
			job.mu.RLock()
			completed, finalized := job.completed, job.finalized
			job.mu.RUnlock()
			if completed && finalized {
				break
			}
		}
		if time.Now().After(deadline) {
			first.Close()
			t.Fatal("HLS cache job was not finalized")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(finalDir, hlsPlaylistName)); err != nil {
		first.Close()
		t.Fatalf("persisted HLS playlist: %v", err)
	}
	first.Close()

	second, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-that-must-not-run", 1, 1, time.Minute, time.Minute, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondSession := uuid.NewString()
	second.RegisterSession(TranscodeSessionParams{
		SessionID: secondSession, TrackID: uuid.NewString(), SourcePath: sourcePath,
		CacheKey: cacheKey, Delivery: StreamProtocolHLS, DurationMs: 1_000,
		Profile: profile, ExpiresAt: time.Now().Add(time.Minute),
	})
	cached, err := second.OpenHLS(context.Background(), secondSession)
	if err != nil {
		t.Fatalf("reuse persisted HLS playlist: %v", err)
	}
	cached.Release()
	if second.metrics.TotalStarted != 0 {
		t.Fatalf("HLS cache hit started FFmpeg again: %+v", second.metrics)
	}
}

func TestHLSStartsPlaybackBeforeTranscodeCompletes(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		128*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 120*44100); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID:  sessionID,
		TrackID:    uuid.NewString(),
		SourcePath: sourcePath,
		Delivery:   StreamProtocolHLS,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})

	started := time.Now()
	handle, err := manager.OpenHLS(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("OpenHLS returned before first segment: %v", err)
	}
	defer handle.Release()
	playlist := string(handle.Content)
	if !strings.Contains(playlist, "#EXT-X-MAP:") || !strings.Contains(playlist, "segment_") {
		t.Fatalf("playlist missing init or media segment: %s", playlist)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("first playlist took too long: %s", elapsed)
	}
	if handle.Complete {
		t.Logf("ffmpeg completed before the playlist request finished; early-start assertion skipped")
	}
}

func TestHLSGeneratedSegmentsArePlayableAndBackgroundKeepsPublishing(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		128*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 120*44100); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, TrackID: uuid.NewString(), SourcePath: sourcePath,
		Delivery: StreamProtocolHLS, DurationMs: 120_000,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})

	first, err := manager.OpenHLS(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("first playlist: %v", err)
	}
	firstCount := len(hlsMediaNames(first.Content))
	if firstCount == 0 {
		t.Fatalf("first playlist has no media segments")
	}
	initHandle, err := manager.OpenHLSSegment(context.Background(), sessionID, hlsInitName)
	if err != nil {
		t.Fatalf("init segment: %v", err)
	}
	initBytes, _ := io.ReadAll(initHandle.Reader)
	initHandle.Release()
	if len(initBytes) < 64 {
		t.Fatalf("init segment too small: %d", len(initBytes))
	}
	if !bytes.Contains(initBytes, []byte("ftyp")) || !bytes.Contains(initBytes, []byte("moov")) {
		t.Fatalf("init segment is not a valid fMP4 init: size=%d", len(initBytes))
	}
	longest := "segment_000000.m4s"
	for _, name := range hlsMediaNames(first.Content) {
		if strings.Contains(name, "segment_") {
			longest = name
			break
		}
	}
	segHandle, err := manager.OpenHLSSegment(context.Background(), sessionID, longest)
	if err != nil {
		t.Fatalf("media segment: %v", err)
	}
	segRaw, _ := io.ReadAll(segHandle.Reader)
	segHandle.Release()
	if len(segRaw) < 64 {
		t.Fatalf("media segment too small: %d", len(segRaw))
	}
	if !bytes.Contains(segRaw, []byte("moof")) || !bytes.Contains(segRaw, []byte("mdat")) {
		t.Fatalf("media segment is not an fMP4 fragment: size=%d", len(segRaw))
	}
	time.Sleep(1200 * time.Millisecond)
	second, err := manager.OpenHLS(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("second playlist: %v", err)
	}
	secondCount := len(hlsMediaNames(second.Content))
	if secondCount <= firstCount {
		t.Fatalf("background transcode did not publish more segments: first=%d second=%d", firstCount, secondCount)
	}
}

func TestHLSDirectedStartGeneratesNearTargetNotFromBeginning(t *testing.T) {
	const targetMs = int64(90_000)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		128*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 120*44100); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, TrackID: uuid.NewString(), SourcePath: sourcePath,
		Delivery: StreamProtocolHLS, DurationMs: 120_000, StartPositionMs: targetMs,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	playlistHandle, err := manager.OpenHLS(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("OpenHLS: %v", err)
	}
	playlistHandle.Release()
	totalDuration := 0.0
	for {
		handle, err := manager.OpenHLS(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("OpenHLS completion poll: %v", err)
		}
		content := string(handle.Content)
		handle.Release()
		if handle.Complete {
			totalDuration = hlsTotalDuration(content)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if totalDuration < 28 || totalDuration > 33 {
		t.Fatalf("directed HLS total duration=%.2fs, want about 30s (only the tail after 90s)", totalDuration)
	}
}

func hlsTotalDuration(playlist string) float64 {
	total := 0.0
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		value := strings.TrimPrefix(line, "#EXTINF:")
		if comma := strings.Index(value, ","); comma >= 0 {
			value = value[:comma]
		}
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			total += parsed
		}
	}
	return total
}

func TestHLSDirectedStartUsesSessionLocalCacheKey(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	trackID := uuid.NewString()
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 44100); err != nil {
		t.Fatal(err)
	}
	resolver := &mockResolver{source: &ResolvedAudioSource{
		TrackID: trackID, SourcePath: sourcePath, SizeBytes: 176400,
		ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DurationMs:     1000, Bitrate: 320000,
	}, exists: true}
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(resolver, NewProfileSelector(), signer, manager, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fromZero, err := service.CreateGrant(context.Background(), uuid.NewString(), trackID, Input{
		PreferredQuality: QualityStandard,
		AcceptedCodecs:   []string{"aac"},
		StreamProtocol:   StreamProtocolHLS,
	})
	if err != nil {
		t.Fatal(err)
	}
	fromTen, err := service.CreateGrant(context.Background(), uuid.NewString(), trackID, Input{
		PreferredQuality: QualityStandard,
		AcceptedCodecs:   []string{"aac"},
		StreamProtocol:   StreamProtocolHLS,
		StartPositionMs:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fromZero.CacheKey == fromTen.CacheKey {
		t.Fatalf("directed HLS grant reused the full-rendition cache key: %q", fromTen.CacheKey)
	}
	if !strings.HasPrefix(fromTen.CacheKey, hlsDirectedKeyPrefix) {
		t.Fatalf("directed HLS cache key is not position-scoped: %q", fromTen.CacheKey)
	}
	if !strings.Contains(fromTen.CacheKey, ":start:10") {
		t.Fatalf("directed HLS cache key does not encode the start position: %q", fromTen.CacheKey)
	}
}

func TestHLSDirectedSamePositionReusesJobAcrossSessions(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	profile := OutputProfile{
		Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
	}
	directedKey := fmt.Sprintf("%strack:reuse:start:100", hlsDirectedKeyPrefix)
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: uuid.NewString(), TrackID: uuid.NewString(), SourcePath: "source.wav",
		CacheKey: directedKey, Delivery: StreamProtocolHLS, Profile: profile,
		StartPositionMs: 100, ExpiresAt: time.Now().Add(time.Minute),
	})
	first := manager.cacheJobs[directedKey]
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: uuid.NewString(), TrackID: uuid.NewString(), SourcePath: "source.wav",
		CacheKey: directedKey, Delivery: StreamProtocolHLS, Profile: profile,
		StartPositionMs: 100, ExpiresAt: time.Now().Add(time.Minute),
	})
	second := manager.cacheJobs[directedKey]
	if first == nil || second == nil || first != second {
		t.Fatalf("directed HLS same-position sessions did not share a job: first=%p second=%p", first, second)
	}
}

func TestHLSGrantUsesCueContentDurationForPositionValidation(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	trackID := uuid.NewString()
	cueStart := int64(5_000)
	cueEnd := int64(25_000)
	resolver := &mockResolver{source: &ResolvedAudioSource{
		TrackID: trackID, SourcePath: "source.wav", SizeBytes: 100,
		ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DurationMs:     20_000, CueStartTimeMs: &cueStart, CueEndTimeMs: &cueEnd, Bitrate: 320000,
	}, exists: true}
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(resolver, NewProfileSelector(), signer, manager, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.CreateGrant(context.Background(), uuid.NewString(), trackID, Input{
		PreferredQuality: QualityStandard,
		AcceptedCodecs:   []string{"aac"},
		StreamProtocol:   StreamProtocolHLS,
		StartPositionMs:  15_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.DurationMs != 20_000 {
		t.Fatalf("cue duration was reported as %d, want 20000", grant.DurationMs)
	}
	if _, err := service.CreateGrant(context.Background(), uuid.NewString(), trackID, Input{
		PreferredQuality: QualityStandard,
		AcceptedCodecs:   []string{"aac"},
		StreamProtocol:   StreamProtocolHLS,
		StartPositionMs:  20_000,
	}); err == nil {
		t.Fatalf("start position at cue content end was accepted")
	}
}

func TestSourceEndMsFallsBackToCueStartPlusContentDuration(t *testing.T) {
	cueStart := int64(5_000)
	params := TranscodeSessionParams{
		DurationMs: 20_000,
		CueStartMs: &cueStart,
	}
	if got := sourceEndMs(params); got != 25_000 {
		t.Fatalf("sourceEndMs = %d, want 25000", got)
	}
	withEnd := int64(30_000)
	params.CueEndMs = &withEnd
	if got := sourceEndMs(params); got != 30_000 {
		t.Fatalf("sourceEndMs with explicit end = %d, want 30000", got)
	}
}

func TestHLSFFmpegArgsMapDirectedStartToSourceCoordinates(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(
		mediaStore,
		"ffmpeg",
		1,
		1,
		time.Minute,
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	cueStart := int64(5_000)
	cueEnd := int64(25_000)
	params := TranscodeSessionParams{
		StartPositionMs: 15_000,
		DurationMs:      20_000,
		CueStartMs:      &cueStart,
		CueEndMs:        &cueEnd,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
	}
	args := manager.buildHLSFFmpegArgs(params, "out", "out/index.m3u8", 20_000)
	wantSS := "-ss"
	wantSSValue := "20.000"
	wantT := "-t"
	wantTValue := "5.000"
	if !containsArgPair(args, wantSS, wantSSValue) {
		t.Fatalf("HLS args missing %s %s: %v", wantSS, wantSSValue, args)
	}
	if !containsArgPair(args, wantT, wantTValue) {
		t.Fatalf("HLS args missing %s %s: %v", wantT, wantTValue, args)
	}
}

func TestProgressiveFFmpegArgsTrimCueWithoutDirection(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(
		mediaStore,
		"ffmpeg",
		1,
		1,
		time.Minute,
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	cueStart := int64(5_000)
	cueEnd := int64(25_000)
	params := TranscodeSessionParams{
		DurationMs: 20_000,
		CueStartMs: &cueStart,
		CueEndMs:   &cueEnd,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
	}
	args := manager.buildFFmpegArgs(params, "out.mp4")
	if !containsArgPair(args, "-ss", "5.000") {
		t.Fatalf("progressive args missing cue start: %v", args)
	}
	if !containsArgPair(args, "-t", "20.000") {
		t.Fatalf("progressive args missing cue duration: %v", args)
	}
}

func TestHLSDataIsCompleteRejectsHeaderOnlyFragment(t *testing.T) {
	stypOnly := []byte("00000018stypmsdh00000000msdhmsix00000034sidx01000000000000010000ac440000000000000000000000000000000000000001")
	if hlsDataIsComplete(stypOnly, "segment_000000.m4s") {
		t.Fatalf("header-only fragment was treated as complete")
	}
	if !hlsDataIsComplete([]byte("ftyp....moov...."), hlsInitName) {
		t.Fatalf("valid init content was rejected")
	}
}

func containsArgPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func TestRewriteHLSPlaylistAddsAuthorizedStableSegmentURLs(t *testing.T) {
	playlist := []byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2.000,\nsegment_000000.m4s\n#EXT-X-ENDLIST\n")
	rewritten, err := rewriteHLSPlaylist(playlist, "session", "ticket.value", "cache:key")
	if err != nil {
		t.Fatal(err)
	}
	value := string(rewritten)
	if !strings.Contains(value, `URI="hls/init.mp4?ticket=ticket.value&cacheKey=cache%3Akey"`) {
		t.Fatalf("initialization URI was not rewritten: %s", value)
	}
	if !strings.Contains(value, "hls/segment_000000.m4s?ticket=ticket.value&cacheKey=cache%3Akey") {
		t.Fatalf("segment URI was not rewritten: %s", value)
	}
	if strings.Contains(value, "segment_000000.m4s\nhls/") {
		t.Fatalf("segment rewrite inserted a duplicate line ending: %s", value)
	}
}

func TestHLSRoutesServePlaylistAndPublishedSegments(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		10*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 44100); err != nil {
		t.Fatal(err)
	}
	trackID := uuid.NewString()
	sessionID := uuid.NewString()
	userID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID:  sessionID,
		TrackID:    trackID,
		SourcePath: sourcePath,
		Delivery:   StreamProtocolHLS,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := signer.Sign(TicketClaims{
		UserID: userID, TrackID: trackID, SessionID: sessionID,
		Quality: string(QualityStandard), Codec: "aac", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(&mockResolver{}, NewProfileSelector(), signer, manager, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := NewRoutes(service, signer, manager, &dummyUserCtx{userID: userID})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	playlistPath := "/api/v1/playback/streams/" + sessionID + "/index.m3u8?ticket=" + url.QueryEscape(ticket)
	playlistResponse := httptest.NewRecorder()
	engine.ServeHTTP(playlistResponse, httptest.NewRequest(http.MethodGet, playlistPath, nil))
	if playlistResponse.Code != http.StatusOK {
		t.Fatalf("playlist status=%d body=%s", playlistResponse.Code, playlistResponse.Body.String())
	}
	if playlistResponse.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("playlist content type=%q", playlistResponse.Header().Get("Content-Type"))
	}
	playlist := playlistResponse.Body.String()
	if !strings.Contains(playlist, "hls/init.mp4?ticket=") || !strings.Contains(playlist, "hls/segment_") {
		t.Fatalf("playlist does not contain authorized segment URLs: %s", playlist)
	}
	var segmentURL string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "hls/segment_") {
			segmentURL = "/api/v1/playback/streams/" + sessionID + "/" + line
			break
		}
	}
	if segmentURL == "" {
		t.Fatalf("no media segment in playlist: %s", playlist)
	}
	segmentResponse := httptest.NewRecorder()
	engine.ServeHTTP(segmentResponse, httptest.NewRequest(http.MethodGet, segmentURL, nil))
	if segmentResponse.Code != http.StatusOK || segmentResponse.Body.Len() == 0 {
		t.Fatalf("segment status=%d bytes=%d body=%q", segmentResponse.Code, segmentResponse.Body.Len(), segmentResponse.Body.String())
	}
	if segmentResponse.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("segment missing byte ranges: %q", segmentResponse.Header().Get("Accept-Ranges"))
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, segmentURL, nil)
	rangeRequest.Header.Set("Range", "bytes=0-1")
	rangeResponse := httptest.NewRecorder()
	engine.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent {
		t.Fatalf("segment range status=%d body=%s", rangeResponse.Code, rangeResponse.Body.String())
	}
	if rangeResponse.Body.Len() != 2 {
		t.Fatalf("segment range returned %d bytes, want 2", rangeResponse.Body.Len())
	}
	if rangeResponse.Header().Get("Content-Range") == "" {
		t.Fatalf("segment range response missing Content-Range")
	}
	headRequest := httptest.NewRequest(http.MethodHead, segmentURL, nil)
	headResponse := httptest.NewRecorder()
	engine.ServeHTTP(headResponse, headRequest)
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 {
		t.Fatalf("segment HEAD status=%d body=%d", headResponse.Code, headResponse.Body.Len())
	}
	if headResponse.Header().Get("Content-Length") == "" {
		t.Fatalf("segment HEAD response missing Content-Length")
	}

	// The old byte-stream endpoint is intentionally not a second protocol for
	// an HLS session. It must not silently return a 200 body at an arbitrary
	// byte offset.
	legacy := httptest.NewRecorder()
	engine.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+sessionID+"?ticket="+url.QueryEscape(ticket), nil))
	if legacy.Code == http.StatusOK {
		t.Fatalf("HLS session was served as a progressive stream")
	}
}

func TestHLSSessionRejectsTicketFromAnotherUser(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-not-run", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	trackID := uuid.NewString()
	sessionID := uuid.NewString()
	ownerUser := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, UserID: ownerUser, TrackID: trackID, SourcePath: "source.wav",
		Delivery: StreamProtocolHLS, Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		DurationMs: 1_000, ExpiresAt: time.Now().Add(time.Minute),
	})
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	foreignTicket, err := signer.Sign(TicketClaims{
		UserID: uuid.NewString(), TrackID: trackID, SessionID: sessionID,
		Quality: string(QualityStandard), Codec: "aac", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(&mockResolver{}, NewProfileSelector(), signer, manager, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := NewRoutes(service, signer, manager, &dummyUserCtx{userID: ownerUser})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	response := httptest.NewRecorder()
	engine.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/playback/streams/"+sessionID+"/index.m3u8?ticket="+url.QueryEscape(foreignTicket),
			nil,
		),
	)
	if response.Code == http.StatusOK {
		t.Fatalf("HLS playlist was served to a ticket from another user")
	}
}
