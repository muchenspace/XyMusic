package playback

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type dummyUserCtx struct {
	userID string
}

func (d *dummyUserCtx) CurrentUserID(_ *gin.Context) (string, error) {
	return d.userID, nil
}

func TestCreatePlaybackGrantRouteReturnsServiceGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	signer, _ := NewTicketSigner("01234567890123456789012345678901")

	trackID := uuid.NewString()
	userID := uuid.NewString()
	sampleRate := 44100
	source := &ResolvedAudioSource{
		TrackID:        trackID,
		SourcePath:     "sample.flac",
		DurationMs:     180000,
		Bitrate:        320000,
		SampleRate:     &sampleRate,
		SizeBytes:      500000,
		ChecksumSHA256: "abc",
		SourceKind:     "LOCAL_MUSIC",
	}

	service, _ := NewService(&mockResolver{source: source, exists: true}, signer, 15*time.Minute)
	routes, _ := NewRoutes(service, signer, &dummyUserCtx{userID: userID})

	engine := gin.New()
	routes.Register(engine)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/"+trackID+"/playback", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"streamUrl"`) || !strings.Contains(body, trackID) {
		t.Fatalf("unexpected playback grant body: %s", body)
	}
}

func TestCreatePlaybackGrantRouteRejectsRemovedQualitySelectors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	signer, _ := NewTicketSigner("01234567890123456789012345678901")
	trackID := uuid.NewString()
	service, _ := NewService(&mockResolver{
		source: &ResolvedAudioSource{TrackID: trackID, SourcePath: "sample.mp3", DurationMs: 1000, Bitrate: 128000},
		exists: true,
	}, signer, 15*time.Minute)
	routes, _ := NewRoutes(service, signer, &dummyUserCtx{userID: uuid.NewString()})
	engine := gin.New()
	routes.Register(engine)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tracks/"+trackID+"/playback",
		strings.NewReader(`{"selector":"STANDARD"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected removed selector request to fail with 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStreamRouteWithTicketVerificationAndRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()

	trackID := uuid.NewString()
	userID := uuid.NewString()
	sampleData := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	audioFile := filepath.Join(tempDir, "sample.mp3")
	if err := os.WriteFile(audioFile, sampleData, 0o644); err != nil {
		t.Fatal(err)
	}

	source := &ResolvedAudioSource{
		TrackID:        trackID,
		SourcePath:     audioFile,
		DurationMs:     1000,
		SizeBytes:      int64(len(sampleData)),
		ChecksumSHA256: "dummy-etag",
	}

	signer, _ := NewTicketSigner("01234567890123456789012345678901")
	service, _ := NewService(&mockResolver{source: source, exists: true}, signer, 15*time.Minute)
	routes, _ := NewRoutes(service, signer, &dummyUserCtx{userID: userID})

	engine := gin.New()
	routes.Register(engine)

	ticket, err := signer.Sign(TicketClaims{
		UserID:    userID,
		TrackID:   trackID,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Full GET request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+trackID+"?ticket="+ticket, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("missing Accept-Ranges: bytes header")
	}
	if rec.Body.String() != string(sampleData) {
		t.Fatalf("body mismatch, got %s", rec.Body.String())
	}

	// 2. Range request: bytes=0-9
	rangeReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+trackID+"?ticket="+ticket, nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeRec := httptest.NewRecorder()
	engine.ServeHTTP(rangeRec, rangeReq)

	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("expected status 206, got %d", rangeRec.Code)
	}
	if rangeRec.Header().Get("Content-Range") != "bytes 0-9/36" {
		t.Fatalf("unexpected Content-Range: %s", rangeRec.Header().Get("Content-Range"))
	}
	if rangeRec.Body.String() != "0123456789" {
		t.Fatalf("unexpected range body: %s", rangeRec.Body.String())
	}

	// 3. Range request: bytes=10- (open ended)
	openRangeReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+trackID+"?ticket="+ticket, nil)
	openRangeReq.Header.Set("Range", "bytes=10-")
	openRangeRec := httptest.NewRecorder()
	engine.ServeHTTP(openRangeRec, openRangeReq)

	if openRangeRec.Code != http.StatusPartialContent {
		t.Fatalf("expected status 206, got %d", openRangeRec.Code)
	}
	if openRangeRec.Body.String() != string(sampleData[10:]) {
		t.Fatalf("unexpected range body: %s", openRangeRec.Body.String())
	}

	// 4. Invalid range: bytes=500-1000 (exceeds file size)
	badRangeReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+trackID+"?ticket="+ticket, nil)
	badRangeReq.Header.Set("Range", "bytes=500-1000")
	badRangeRec := httptest.NewRecorder()
	engine.ServeHTTP(badRangeRec, badRangeReq)

	if badRangeRec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("expected status 416, got %d", badRangeRec.Code)
	}

	// 5. HEAD request
	headReq := httptest.NewRequest(http.MethodHead, "/api/v1/playback/streams/"+trackID+"?ticket="+ticket, nil)
	headRec := httptest.NewRecorder()
	engine.ServeHTTP(headRec, headReq)

	if headRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for HEAD, got %d", headRec.Code)
	}
	if headRec.Body.Len() != 0 {
		t.Fatalf("expected empty body for HEAD, got %d bytes", headRec.Body.Len())
	}

	// 6. Invalid ticket
	badTicketReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+trackID+"?ticket=invalid.ticket", nil)
	badTicketRec := httptest.NewRecorder()
	engine.ServeHTTP(badTicketRec, badTicketReq)

	if badTicketRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid ticket, got %d", badTicketRec.Code)
	}

	// 7. Ticket for a different track
	otherTrackTicket, _ := signer.Sign(TicketClaims{
		UserID:    userID,
		TrackID:   uuid.NewString(),
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	mismatchReq := httptest.NewRequest(http.MethodGet, "/api/v1/playback/streams/"+trackID+"?ticket="+otherTrackTicket, nil)
	mismatchRec := httptest.NewRecorder()
	engine.ServeHTTP(mismatchRec, mismatchReq)

	if mismatchRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for mismatched track ticket, got %d", mismatchRec.Code)
	}
}
