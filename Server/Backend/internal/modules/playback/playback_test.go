package playback

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/platform/localmedia"
)

type mockResolver struct {
	source   *ResolvedAudioSource
	err      error
	exists   bool
	existErr error
}

func (m *mockResolver) ResolveSource(_ context.Context, _ string) (*ResolvedAudioSource, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.source, nil
}

func (m *mockResolver) PublishedTrackExists(_ context.Context, _ string) (bool, error) {
	return m.exists, m.existErr
}

func TestTicketSignerAndVerification(t *testing.T) {
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}

	claims := TicketClaims{
		UserID:    uuid.NewString(),
		TrackID:   uuid.NewString(),
		SessionID: uuid.NewString(),
		Quality:   "STANDARD",
		Codec:     "aac",
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("sign error: %v", err)
	}

	verified, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if verified.SessionID != claims.SessionID || verified.TrackID != claims.TrackID {
		t.Fatalf("verified claims mismatch: %+v", verified)
	}

	// Tampered token
	tampered := token[:len(token)-4] + "xxxx"
	if _, err := signer.Verify(tampered); err == nil {
		t.Fatal("expected error on tampered signature, got nil")
	}

	// Expired token
	expiredClaims := claims
	expiredClaims.ExpiresAt = time.Now().Add(-1 * time.Minute).Unix()
	expiredToken, _ := signer.Sign(expiredClaims)
	if _, err := signer.Verify(expiredToken); err == nil {
		t.Fatal("expected error on expired token, got nil")
	}
}

func TestProfileSelectorMatrix(t *testing.T) {
	selector := NewProfileSelector()

	// Lossless source with flac accepted
	sr := 48000
	p1, err := selector.SelectProfile(QualityLossless, []string{"flac", "aac"}, "track.flac", 1000000, &sr)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Quality != QualityLossless || p1.Codec != "flac" || p1.Container != "flac" {
		t.Fatalf("unexpected profile 1: %+v", p1)
	}

	// LOSSLESS never downgrades just because the client codec list omits the
	// source codec. The original file is always sent as-is.
	p2, err := selector.SelectProfile(QualityLossless, []string{"aac"}, "track.flac", 1000000, &sr)
	if err != nil {
		t.Fatal(err)
	}
	if !p2.Direct || p2.Quality != QualityLossless || p2.Codec != "flac" || p2.Bitrate != 1000000 {
		t.Fatalf("unexpected profile 2: %+v", p2)
	}

	// A lossy source is also direct: no encoder can reverse MP3 compression.
	pLossy, err := selector.SelectProfile(QualityLossless, []string{"aac"}, "track.mp3", 320000, &sr)
	if err != nil {
		t.Fatal(err)
	}
	if !pLossy.Direct || pLossy.Quality != QualityLossless || pLossy.Codec != "mp3" || pLossy.IsLossless {
		t.Fatalf("lossless MP3 request was transcoded or mislabeled: %+v", pLossy)
	}

	// DATA_SAVER profile
	p3, err := selector.SelectProfile(QualityDataSaver, []string{"mp3"}, "track.mp3", 320000, &sr)
	if err != nil {
		t.Fatal(err)
	}
	if p3.Quality != QualityDataSaver || p3.Codec != "mp3" || p3.Bitrate != 64000 {
		t.Fatalf("unexpected profile 3: %+v", p3)
	}
}

type dummyUserCtx struct {
	userID string
}

func (d *dummyUserCtx) CurrentUserID(_ *gin.Context) (string, error) {
	return d.userID, nil
}

func TestPlaybackGrantAndStreamRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	assetDir := filepath.Join(tempDir, "assets")
	transcodeDir := filepath.Join(tempDir, "transcode")

	mediaStore, err := localmedia.NewStore(assetDir, transcodeDir, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	signer, _ := NewTicketSigner("01234567890123456789012345678901")
	selector := NewProfileSelector()
	transcoder, _ := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 4, 30*time.Second, 30*time.Second)

	trackID := uuid.NewString()
	sourceFile := filepath.Join(tempDir, "source.wav")
	_ = os.WriteFile(sourceFile, []byte("RIFF1234WAVEfmt "), 0o644)

	resolver := &mockResolver{
		source: &ResolvedAudioSource{
			TrackID:        trackID,
			SourcePath:     sourceFile,
			DurationMs:     180000,
			SizeBytes:      16,
			ChecksumSHA256: "dummyhash",
			SourceKind:     "SCAN",
			Bitrate:        320000,
		},
		exists: true,
	}

	service, err := NewService(resolver, selector, signer, transcoder, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	userID := uuid.NewString()
	routes, err := NewRoutes(service, signer, transcoder, &dummyUserCtx{userID: userID})
	if err != nil {
		t.Fatal(err)
	}

	engine := gin.New()
	routes.Register(engine)

	// POST /api/v1/tracks/:id/playback
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/"+trackID+"/playback", strings.NewReader(`{"preferredQuality":"STANDARD","acceptedCodecs":["aac"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), `"streamUrl"`) || !strings.Contains(rec.Body.String(), `"sessionId"`) {
		t.Fatalf("response missing streamUrl or sessionId: %s", rec.Body.String())
	}
}

func TestDynamicTranscodeProducesOutputFromSourceAtRequestTime(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 4410); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, TrackID: uuid.NewString(), SourcePath: sourcePath,
		Profile:   OutputProfile{Quality: QualityStandard, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Bitrate: 128000},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	outputPath, err := manager.GetOrStartTranscode(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("dynamic transcode failed: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 0 {
		t.Fatalf("dynamic transcode output is empty: %s", outputPath)
	}
	metrics := manager.metrics
	if metrics.TotalStarted != 1 || metrics.TotalSuccess != 1 || metrics.TotalFailed != 0 {
		t.Fatalf("dynamic transcode metrics = %+v", metrics)
	}
}

func TestDynamicTranscodeSupportsDefaultAACProfile(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	transcoder, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transcoder.Close)
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 8820); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	transcoder.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, TrackID: uuid.NewString(), SourcePath: sourcePath,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "aac", Container: "m4a", MimeType: "audio/mp4", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	path, err := transcoder.GetOrStartTranscode(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("AAC transcode: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("AAC output stat=%v/%v", info, err)
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not available")
	}
	output := exec.Command(probe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "default=nw=1:nk=1", path)
	codec, err := output.Output()
	if err != nil {
		t.Fatalf("probe AAC output: %v", err)
	}
	if strings.TrimSpace(string(codec)) != "aac" {
		t.Fatalf("AAC output codec=%q", strings.TrimSpace(string(codec)))
	}
}

func TestDynamicTranscodeCapacityFailureDoesNotLeaveWaitersBlocked(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg", 1, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	manager.semaphore <- struct{}{}
	defer func() { <-manager.semaphore }()

	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, TrackID: uuid.NewString(), SourcePath: "missing.wav",
		Profile:   OutputProfile{Quality: QualityStandard, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Bitrate: 128000},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.GetOrStartTranscode(ctx, sessionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("first capacity failure = %v", err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := manager.GetOrStartTranscode(context.Background(), sessionID)
		finished <- err
	}()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter failure = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter remained blocked after capacity failure")
	}
}

func TestProfileSelectorRejectsUnsupportedCodecInsteadOfLyingAboutOutput(t *testing.T) {
	_, err := NewProfileSelector().SelectProfile(QualityStandard, []string{"wav"}, "song.flac", 320000, nil)
	if err == nil {
		t.Fatal("unsupported codec should not produce a descriptor")
	}
}

func writeTestPCM16WAV(path string, sampleRate, channels, samples int) error {
	dataSize := samples * channels * 2
	fileSize := 36 + dataSize
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	write := func(value any) error { return binary.Write(file, binary.LittleEndian, value) }
	if _, err := file.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := write(uint32(fileSize)); err != nil {
		return err
	}
	if _, err := file.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	if err := write(uint32(16)); err != nil {
		return err
	}
	if err := write(uint16(1)); err != nil {
		return err
	}
	if err := write(uint16(channels)); err != nil {
		return err
	}
	if err := write(uint32(sampleRate)); err != nil {
		return err
	}
	if err := write(uint32(sampleRate * channels * 2)); err != nil {
		return err
	}
	if err := write(uint16(channels * 2)); err != nil {
		return err
	}
	if err := write(uint16(16)); err != nil {
		return err
	}
	if _, err := file.Write([]byte("data")); err != nil {
		return err
	}
	if err := write(uint32(dataSize)); err != nil {
		return err
	}
	for index := 0; index < samples*channels; index++ {
		if err := write(int16(0)); err != nil {
			return err
		}
	}
	return nil
}

func TestLosslessProfileServesOriginalFormatWithoutTranscoding(t *testing.T) {
	profile, err := NewProfileSelector().SelectProfile(
		QualityLossless,
		[]string{"aac", "flac"},
		"album/song.flac",
		1_411_200,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.Direct || profile.Codec != "flac" || profile.Container != "flac" || profile.MimeType != "audio/flac" {
		t.Fatalf("lossless profile was not direct: %+v", profile)
	}
}

func TestCompletedTranscodeIsPersistedAndReusedByNewManager(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(tempDir, "source.wav")
	if err := writeTestPCM16WAV(sourcePath, 44100, 1, 8820); err != nil {
		t.Fatal(err)
	}
	cacheKey := "track:cache-reuse:standard:mp3"
	profile := OutputProfile{Quality: QualityStandard, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Bitrate: 128000}
	first, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	firstSession := uuid.NewString()
	first.RegisterSession(TranscodeSessionParams{SessionID: firstSession, TrackID: uuid.NewString(), SourcePath: sourcePath, CacheKey: cacheKey, Profile: profile, ExpiresAt: time.Now().Add(time.Minute)})
	firstPath, err := first.GetOrStartTranscode(nil, firstSession)
	if err != nil {
		first.Close()
		t.Fatalf("first transcode: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(firstPath), cacheFilePrefix) || strings.Contains(firstPath, cachePartialMarker) {
		first.Close()
		t.Fatalf("first transcode was not finalized into cache: %q", firstPath)
	}
	if _, err := os.Stat(firstPath); err != nil {
		first.Close()
		t.Fatalf("cache file stat: %v", err)
	}
	first.Close()

	second, err := NewTranscodeSessionManager(mediaStore, ffmpeg, 1, 1, time.Minute, time.Minute, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondSession := uuid.NewString()
	second.RegisterSession(TranscodeSessionParams{SessionID: secondSession, TrackID: uuid.NewString(), SourcePath: sourcePath, CacheKey: cacheKey, Profile: profile, ExpiresAt: time.Now().Add(time.Minute)})
	secondPath, err := second.GetOrStartTranscode(nil, secondSession)
	if err != nil {
		t.Fatalf("cached transcode: %v", err)
	}
	if second.metrics.TotalStarted != 0 {
		t.Fatalf("cache hit started FFmpeg again: %+v", second.metrics)
	}
	if secondPath != firstPath {
		t.Fatalf("cache path changed: first=%q second=%q", firstPath, secondPath)
	}
}

func TestCompletedCachedSessionSurvivesIdleTimeout(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-that-must-not-run", 0, 1, time.Second, time.Minute, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	fixedNow := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return fixedNow }
	cacheKey := "track:pause-resume-cache"
	profile := OutputProfile{Quality: QualityStandard, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Bitrate: 128000}
	finalPath, _ := manager.cachePaths(cacheKey, profile.Container)
	if err := os.WriteFile(finalPath, []byte("persisted-variant"), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID, TrackID: uuid.NewString(), SourcePath: "unused",
		CacheKey: cacheKey, Profile: profile, ExpiresAt: fixedNow.Add(time.Hour),
	})
	manager.cleanupExpiredSessionsAt(fixedNow.Add(2 * time.Second))

	manager.sessionsMu.RLock()
	_, stillRegistered := manager.sessions[sessionID]
	manager.sessionsMu.RUnlock()
	if !stillRegistered {
		t.Fatal("completed cacheable session was removed by idle timeout")
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("cache file removed during idle cleanup: %v", err)
	}
}

func TestExpiredPlaybackSessionDoesNotDeleteReusableTranscodeCache(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-that-must-not-run", 0, 1, time.Minute, time.Minute, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	fixedNow := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return fixedNow }
	cacheKey := "track:expired-session-cache"
	profile := OutputProfile{Quality: QualityStandard, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Bitrate: 128000}
	finalPath, partialPath := manager.cachePaths(cacheKey, profile.Container)
	if err := os.WriteFile(finalPath, []byte("persisted-variant"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(partialPath)

	expiredSession := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: expiredSession, TrackID: uuid.NewString(), SourcePath: "unused",
		CacheKey: cacheKey, Profile: profile, ExpiresAt: fixedNow.Add(-time.Minute),
	})
	manager.cleanupExpiredSessions()

	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("expired session removed reusable cache: %v", err)
	}
	manager.sessionsMu.RLock()
	_, stillRegistered := manager.sessions[expiredSession]
	manager.sessionsMu.RUnlock()
	if stillRegistered {
		t.Fatal("expired playback session was not removed")
	}

	nextSession := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: nextSession, TrackID: uuid.NewString(), SourcePath: "unused",
		CacheKey: cacheKey, Profile: profile, ExpiresAt: fixedNow.Add(time.Minute),
	})
	path, err := manager.GetOrStartTranscode(nil, nextSession)
	if err != nil {
		t.Fatalf("reuse persisted cache: %v", err)
	}
	if path != finalPath {
		t.Fatalf("reused path = %q, want %q", path, finalPath)
	}
	if manager.metrics.TotalStarted != 0 {
		t.Fatalf("cache reuse started FFmpeg: %+v", manager.metrics)
	}
}

func TestOpenStreamAtReturnsOnlyFiniteProgressiveOutput(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-that-must-not-run", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	sourcePath := filepath.Join(tempDir, "source.mp3")
	if err := os.WriteFile(sourcePath, []byte("finite-output"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	manager.RegisterSession(TranscodeSessionParams{
		SessionID: sessionID,
		TrackID:   uuid.NewString(),
		SourcePath: sourcePath,
		Profile: OutputProfile{
			Quality: QualityLossless, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Direct: true,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	handle, err := manager.OpenStreamAt(context.Background(), sessionID, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	if !handle.Complete {
		t.Fatal("progressive handle was not marked complete")
	}
	file, ok := handle.Reader.(*os.File)
	if !ok {
		t.Fatalf("reader type = %T, want *os.File", handle.Reader)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "finite-output" {
		t.Fatalf("finite output = %q", content)
	}
}

func TestTranscodeCacheEvictsOldestFilesWhenLimitIsExceeded(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 1, time.Minute, time.Minute, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	oldPath, _ := manager.cachePaths("old", "mp3")
	newPath, _ := manager.cachePaths("new", "mp3")
	if err := os.WriteFile(oldPath, bytes.Repeat([]byte("o"), 40), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, bytes.Repeat([]byte("n"), 40), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	manager.loadCacheIndex()
	manager.enforceCacheLimit()
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old cache file was not evicted: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new cache file was evicted: %v", err)
	}
}
