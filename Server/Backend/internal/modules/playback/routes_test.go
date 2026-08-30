package playback

import (
	"encoding/json"
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

func TestStreamRouteWithTicketVerificationAndRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	assetDir := filepath.Join(tempDir, "assets")
	transcodeDir := filepath.Join(tempDir, "transcode")

	mediaStore, err := localmedia.NewStore(assetDir, transcodeDir, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	secret := "01234567890123456789012345678901"
	signer, _ := NewTicketSigner(secret)
	selector := NewProfileSelector()
	transcoder, _ := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 4, 30*time.Second, 30*time.Second)
	t.Cleanup(transcoder.Close)

	trackID := uuid.NewString()
	sessionID := uuid.NewString()
	userID := uuid.NewString()

	// Pre-create the transcoded file to simulate finished transcoding
	sampleData := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	transcodeFile := filepath.Join(transcodeDir, sessionID+"_test.m4a")
	_ = os.WriteFile(transcodeFile, sampleData, 0o644)

	transcoder.RegisterSession(TranscodeSessionParams{
		SessionID:  sessionID,
		TrackID:    trackID,
		SourcePath: "source.wav",
		Profile: OutputProfile{
			Quality:   QualityStandard,
			Codec:     "aac",
			Container: "m4a",
			MimeType:  "audio/mp4",
			Bitrate:   128000,
		},
		ExpiresAt: time.Now().Add(10 * time.Minute),
	})
	// Manually set tempPath in session to transcodeFile
	transcoder.sessionsMu.Lock()
	transcoder.sessions[sessionID].tempPath = transcodeFile
	transcoder.sessions[sessionID].completed = true
	transcoder.sessionsMu.Unlock()

	ticket, err := signer.Sign(TicketClaims{
		UserID:    userID,
		TrackID:   trackID,
		SessionID: sessionID,
		Quality:   "STANDARD",
		Codec:     "aac",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	service, _ := NewService(&mockResolver{}, selector, signer, transcoder, 15*time.Minute)
	routes, _ := NewRoutes(service, signer, transcoder, &dummyUserCtx{userID: userID})

	engine := gin.New()
	routes.Register(engine)

	// Full GET request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+sessionID+"?ticket="+ticket, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("missing Accept-Ranges: bytes header")
	}
	if rec.Body.String() != string(sampleData) {
		t.Fatalf("body mismatch")
	}

	// Range request: bytes=0-9
	rangeReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+sessionID+"?ticket="+ticket, nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeRec := httptest.NewRecorder()
	engine.ServeHTTP(rangeRec, rangeReq)

	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("expected status 206 for range request, got %d", rangeRec.Code)
	}
	if rangeRec.Body.String() != "0123456789" {
		t.Fatalf("range body mismatch: %q", rangeRec.Body.String())
	}

	// Missing ticket
	noTicketReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+sessionID, nil)
	noTicketRec := httptest.NewRecorder()
	engine.ServeHTTP(noTicketRec, noTicketReq)
	if noTicketRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing ticket, got %d", noTicketRec.Code)
	}
}

func TestStreamRouteRejectMismatchSessionTicket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	mediaStore, _ := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	signer, _ := NewTicketSigner("01234567890123456789012345678901")
	selector := NewProfileSelector()
	transcoder, _ := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 4, 30*time.Second, 30*time.Second)

	ticket, _ := signer.Sign(TicketClaims{
		UserID:    uuid.NewString(),
		TrackID:   uuid.NewString(),
		SessionID: uuid.NewString(), // mismatched session
		Quality:   "STANDARD",
		Codec:     "aac",
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})

	service, _ := NewService(&mockResolver{}, selector, signer, transcoder, 15*time.Minute)
	routes, _ := NewRoutes(service, signer, transcoder, &dummyUserCtx{})

	engine := gin.New()
	routes.Register(engine)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+uuid.NewString()+"?ticket="+ticket, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for mismatched session ticket, got %d", rec.Code)
	}
}

func TestCreatePlaybackGrantRouteReturnsServiceGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	mediaStore, _ := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	signer, _ := NewTicketSigner("01234567890123456789012345678901")
	selector := NewProfileSelector()
	transcoder, _ := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 4, 30*time.Second, 30*time.Second)

	trackID := uuid.NewString()
	userID := uuid.NewString()
	sampleRate := 44100
	source := &ResolvedAudioSource{
		TrackID:        trackID,
		SourcePath:     "sample.wav",
		DurationMs:     180000,
		Bitrate:        1411200,
		SampleRate:     &sampleRate,
		ChecksumSHA256: "abc",
		SourceKind:     "LOCAL_MUSIC",
	}

	service, _ := NewService(&mockResolver{source: source, exists: true}, selector, signer, transcoder, 15*time.Minute)
	routes, _ := NewRoutes(service, signer, transcoder, &dummyUserCtx{userID: userID})

	engine := gin.New()
	routes.Register(engine)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/"+trackID+"/playback", strings.NewReader(`{
		"preferredQuality": "STANDARD",
		"acceptedCodecs": ["aac", "mp3"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"streamUrl"`) || !strings.Contains(rec.Body.String(), `"sessionId"`) {
		t.Fatalf("unexpected playback grant body: %s", rec.Body.String())
	}
}

func TestStreamRouteRunsDynamicTranscodeForRealAudio(t *testing.T) {
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
	trackID := uuid.NewString()
	sessionID := uuid.NewString()
	userID := uuid.NewString()
	transcoder.RegisterSession(TranscodeSessionParams{
		SessionID:  sessionID,
		TrackID:    trackID,
		SourcePath: sourcePath,
		Profile: OutputProfile{
			Quality: QualityStandard, Codec: "mp3", Container: "mp3", MimeType: "audio/mpeg", Bitrate: 128000,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := signer.Sign(TicketClaims{
		UserID: userID, TrackID: trackID, SessionID: sessionID,
		Quality: "STANDARD", Codec: "mp3", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(&mockResolver{}, NewProfileSelector(), signer, transcoder, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := NewRoutes(service, signer, transcoder, &dummyUserCtx{userID: userID})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	streamURL := "/api/v1/playback/streams/" + sessionID + "?ticket=" + ticket

	request := httptest.NewRequest(http.MethodGet, streamURL, nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("dynamic stream response=%d bytes=%d body=%q", response.Code, response.Body.Len(), response.Body.String())
	}
	if response.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("dynamic stream content type=%q", response.Header().Get("Content-Type"))
	}
	if transcoder.metrics.TotalStarted != 1 || transcoder.metrics.TotalSuccess != 1 {
		t.Fatalf("dynamic stream metrics=%+v", transcoder.metrics)
	}

	repeat := httptest.NewRecorder()
	engine.ServeHTTP(repeat, httptest.NewRequest(http.MethodGet, streamURL, nil))
	if repeat.Code != http.StatusOK || repeat.Body.Len() != response.Body.Len() {
		t.Fatalf("repeat dynamic stream response=%d bytes=%d want=%d", repeat.Code, repeat.Body.Len(), response.Body.Len())
	}
	if transcoder.metrics.TotalStarted != 1 {
		t.Fatalf("repeat request started another transcode: %+v", transcoder.metrics)
	}

	head := httptest.NewRecorder()
	engine.ServeHTTP(head, httptest.NewRequest(http.MethodHead, streamURL, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD dynamic stream response=%d bytes=%d", head.Code, head.Body.Len())
	}
}

func TestStreamRouteServesLosslessSourceDirectly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	transcoder, err := NewTranscodeSessionManager(mediaStore, "ffmpeg", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer transcoder.Close()

	trackID := uuid.NewString()
	userID := uuid.NewString()
	sourcePath := filepath.Join(tempDir, "song.flac")
	sample := []byte("fLaC\x00\x00\x00\x22original-lossless-bytes")
	if err := os.WriteFile(sourcePath, sample, 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := &mockResolver{source: &ResolvedAudioSource{
		TrackID: trackID, SourcePath: sourcePath, SizeBytes: int64(len(sample)),
		ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DurationMs:     1000, Bitrate: 1_411_200,
	}, exists: true}
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(resolver, NewProfileSelector(), signer, transcoder, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := NewRoutes(service, signer, transcoder, &dummyUserCtx{userID: userID})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)

	grantRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/"+trackID+"/playback", strings.NewReader(`{"preferredQuality":"LOSSLESS","acceptedCodecs":["flac"]}`))
	grantRequest.Header.Set("Content-Type", "application/json")
	grantResponse := httptest.NewRecorder()
	engine.ServeHTTP(grantResponse, grantRequest)
	if grantResponse.Code != http.StatusOK {
		t.Fatalf("grant status=%d body=%s", grantResponse.Code, grantResponse.Body.String())
	}
	var descriptor DescriptorDTO
	if err := json.Unmarshal(grantResponse.Body.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	streamResponse := httptest.NewRecorder()
	engine.ServeHTTP(streamResponse, httptest.NewRequest(http.MethodGet, descriptor.StreamURL, nil))
	if streamResponse.Code != http.StatusOK || string(streamResponse.Body.Bytes()) != string(sample) {
		t.Fatalf("direct stream status=%d body=%q", streamResponse.Code, streamResponse.Body.Bytes())
	}
	if streamResponse.Header().Get("Content-Type") != "audio/flac" {
		t.Fatalf("direct stream content type=%q", streamResponse.Header().Get("Content-Type"))
	}
	if transcoder.metrics.TotalStarted != 0 {
		t.Fatalf("direct lossless playback started FFmpeg: %+v", transcoder.metrics)
	}
}

func TestStreamRouteServesLossySourceDirectlyForLosslessSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	transcoder, err := NewTranscodeSessionManager(mediaStore, "ffmpeg-that-must-not-run", 0, 1, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer transcoder.Close()

	trackID := uuid.NewString()
	userID := uuid.NewString()
	sourcePath := filepath.Join(tempDir, "song.mp3")
	sample := []byte("ID3-original-mp3-bytes")
	if err := os.WriteFile(sourcePath, sample, 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := &mockResolver{source: &ResolvedAudioSource{
		TrackID: trackID, SourcePath: sourcePath, SizeBytes: int64(len(sample)),
		ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DurationMs:     1000, Bitrate: 320000,
	}, exists: true}
	signer, err := NewTicketSigner("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(resolver, NewProfileSelector(), signer, transcoder, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := NewRoutes(service, signer, transcoder, &dummyUserCtx{userID: userID})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)

	grantRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/"+trackID+"/playback", strings.NewReader(`{"preferredQuality":"LOSSLESS","acceptedCodecs":["aac"]}`))
	grantRequest.Header.Set("Content-Type", "application/json")
	grantResponse := httptest.NewRecorder()
	engine.ServeHTTP(grantResponse, grantRequest)
	if grantResponse.Code != http.StatusOK {
		t.Fatalf("grant status=%d body=%s", grantResponse.Code, grantResponse.Body.String())
	}
	var descriptor struct {
		StreamURL       string           `json:"streamUrl"`
		SelectedQuality PreferredQuality `json:"selectedQuality"`
		Codec           string           `json:"codec"`
	}
	if err := json.Unmarshal(grantResponse.Body.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	if descriptor.SelectedQuality != QualityLossless || descriptor.Codec != "mp3" {
		t.Fatalf("lossless MP3 grant was changed: %+v", descriptor)
	}
	streamResponse := httptest.NewRecorder()
	engine.ServeHTTP(streamResponse, httptest.NewRequest(http.MethodGet, descriptor.StreamURL, nil))
	if streamResponse.Code != http.StatusOK || string(streamResponse.Body.Bytes()) != string(sample) {
		t.Fatalf("direct MP3 stream status=%d body=%q", streamResponse.Code, streamResponse.Body.Bytes())
	}
	if streamResponse.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("direct MP3 stream content type=%q", streamResponse.Header().Get("Content-Type"))
	}
	if transcoder.metrics.TotalStarted != 0 {
		t.Fatalf("lossless MP3 playback started FFmpeg: %+v", transcoder.metrics)
	}
}
