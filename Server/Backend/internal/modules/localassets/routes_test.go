package localassets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/platform/localmedia"
)

type mockStore struct {
	assets map[string]*AssetRecord
}

func (m *mockStore) FindReadyAsset(_ context.Context, assetID string) (*AssetRecord, error) {
	return m.assets[assetID], nil
}

func TestNewCleanerValidatesDependencies(t *testing.T) {
	if _, err := NewCleaner(nil, nil); err == nil {
		t.Fatal("expected cleaner dependency validation error")
	}
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCleaner(nil, mediaStore); err == nil {
		t.Fatal("expected cleaner database validation error")
	}
}

func TestServeAssetSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	assetDir := filepath.Join(tempDir, "assets")
	transcodeDir := filepath.Join(tempDir, "transcode")

	mediaStore, err := localmedia.NewStore(assetDir, transcodeDir, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	assetID := uuid.NewString()
	relPath := "artworks/" + assetID + ".png"
	absPath := filepath.Join(assetDir, "artworks", assetID+".png")
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	fileContent := []byte("fake png image content here")
	if err := os.WriteFile(absPath, fileContent, 0o644); err != nil {
		t.Fatal(err)
	}

	checksum := "abc123sha"
	now := time.Now().UTC()
	store := &mockStore{
		assets: map[string]*AssetRecord{
			assetID: {
				ID:             assetID,
				StoragePath:    relPath,
				Kind:           "ARTWORK",
				MimeType:       "image/png",
				SizeBytes:      int64(len(fileContent)),
				ChecksumSHA256: &checksum,
				Status:         "READY",
				UpdatedAt:      now,
			},
		},
	}

	routes, err := NewRoutes(store, mediaStore)
	if err != nil {
		t.Fatal(err)
	}

	engine := gin.New()
	routes.Register(engine)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+assetID+"/"+checksum, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("expected image/png, got %s", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected cache control: %s", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.String() != string(fileContent) {
		t.Fatalf("body mismatch")
	}

	// Test HEAD
	headReq := httptest.NewRequest(http.MethodHead, "/api/v1/assets/"+assetID+"/"+checksum, nil)
	headRec := httptest.NewRecorder()
	engine.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for HEAD, got %d", headRec.Code)
	}
}

func TestServeAssetNotFoundOrMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	mediaStore, _ := localmedia.NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)

	store := &mockStore{assets: map[string]*AssetRecord{}}
	routes, _ := NewRoutes(store, mediaStore)

	engine := gin.New()
	routes.Register(engine)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+uuid.NewString()+"/v1", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
