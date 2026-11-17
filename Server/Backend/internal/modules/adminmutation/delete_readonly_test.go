package adminmutation

import (
	"os"
	"path/filepath"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestCheckSourceDeletionModeProtectsExistingReadOnlySourceFiles(t *testing.T) {
	dir := t.TempDir()
	existingFile := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(existingFile, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingFile := filepath.Join(dir, "missing.flac")

	readOnly := "READ_ONLY"
	readWrite := "READ_WRITE"

	err := checkSourceDeletionMode(&readOnly, existingFile)
	if !apperror.IsCode(err, apperror.CodeForbidden) {
		t.Fatalf("expected forbidden for existing file in READ_ONLY root, got %v", err)
	}
	if _, statErr := os.Stat(existingFile); statErr != nil {
		t.Fatalf("existing file in READ_ONLY root must remain untouched: %v", statErr)
	}

	if err := checkSourceDeletionMode(&readOnly, missingFile); err != nil {
		t.Fatalf("missing file in READ_ONLY root should allow orphan record cleanup, got %v", err)
	}
	if err := checkSourceDeletionMode(&readWrite, existingFile); err != nil {
		t.Fatalf("READ_WRITE root should allow permanent file deletion, got %v", err)
	}
	if err := checkSourceDeletionMode(nil, existingFile); err != nil {
		t.Fatalf("nil root mode should allow permanent file deletion, got %v", err)
	}
}
