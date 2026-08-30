package localmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalMediaStoreInitializationAndPing(t *testing.T) {
	tempDir := t.TempDir()
	assetDir := filepath.Join(tempDir, "assets")
	transcodeDir := filepath.Join(tempDir, "transcode")

	store, err := NewStore(assetDir, transcodeDir, 10*1024*1024)
	if err != nil {
		t.Fatalf("unexpected NewStore error: %v", err)
	}

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("unexpected Ping error: %v", err)
	}
}

func TestPathTraversalPrevention(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	traversalPaths := []string{
		"../escape.txt",
		"../../etc/passwd",
		"subdir/../../outside.txt",
		"..\\win_escape.txt",
	}

	for _, p := range traversalPaths {
		_, err := store.ResolveAssetPath(p)
		if err == nil {
			t.Errorf("expected path traversal error for %q, got nil", p)
		}
	}
}

func TestUploadStreamValidationAndCommit(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("test audio content payload 12345")
	hasher := sha256.New()
	hasher.Write(data)
	expectedSHA := hex.EncodeToString(hasher.Sum(nil))

	tempUploadPath := filepath.Join(store.AssetDirectory(), "temp", "test.partial")
	written, checksum, err := store.WriteUploadStream(
		context.Background(),
		bytes.NewReader(data),
		int64(len(data)),
		expectedSHA,
		tempUploadPath,
	)
	if err != nil {
		t.Fatalf("unexpected WriteUploadStream error: %v", err)
	}
	if written != int64(len(data)) {
		t.Fatalf("written %d, expected %d", written, len(data))
	}
	if checksum != expectedSHA {
		t.Fatalf("checksum %s, expected %s", checksum, expectedSHA)
	}

	relPath, err := store.CommitUpload(context.Background(), tempUploadPath, "audio/test_audio.bin")
	if err != nil {
		t.Fatalf("unexpected CommitUpload error: %v", err)
	}
	if !strings.Contains(relPath, "audio/test_audio.bin") {
		t.Fatalf("unexpected relative path: %s", relPath)
	}

	info, err := store.StatAsset(relPath)
	if err != nil {
		t.Fatalf("unexpected StatAsset error: %v", err)
	}
	if info.Size() != int64(len(data)) {
		t.Fatalf("file size %d, expected %d", info.Size(), len(data))
	}

	if err := store.DeleteAsset(relPath); err != nil {
		t.Fatalf("unexpected DeleteAsset error: %v", err)
	}
}

func TestUploadStreamChecksumMismatch(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(filepath.Join(tempDir, "assets"), filepath.Join(tempDir, "transcode"), 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("mismatched data")
	tempUploadPath := filepath.Join(store.AssetDirectory(), "temp", "bad.partial")
	_, _, err = store.WriteUploadStream(
		context.Background(),
		bytes.NewReader(data),
		int64(len(data)),
		"0000000000000000000000000000000000000000000000000000000000000000",
		tempUploadPath,
	)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if _, statErr := os.Stat(tempUploadPath); !os.IsNotExist(statErr) {
		t.Fatal("temp file should be deleted upon failure")
	}
}
