package adminmutation

import (
	"os"
	"path/filepath"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestSecureSourcePathAndStagedDeletionRollback(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "disc", "track.flac")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := secureSourcePath(root, filepath.Join("disc", "track.flac"))
	if err != nil || resolved != source {
		t.Fatalf("secureSourcePath=%q,%v", resolved, err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.flac")
	if _, err := secureSourcePath(root, outside); !apperror.IsCode(err, apperror.CodeForbidden) {
		t.Fatalf("outside error=%v", err)
	}
	staged, err := stageSourceFilesForDeletion([]string{source}, "track-id")
	if err != nil || len(staged) != 1 {
		t.Fatalf("stage=%#v,%v", staged, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	if failures := restoreStagedSourceFiles(staged); len(failures) != 0 {
		t.Fatalf("restore failures=%#v", failures)
	}
	content, err := os.ReadFile(source)
	if err != nil || string(content) != "audio" {
		t.Fatalf("restored=%q,%v", content, err)
	}
	staged, err = stageSourceFilesForDeletion([]string{source}, "track-id")
	if err != nil {
		t.Fatal(err)
	}
	if failures := finalizeStagedSourceFiles(staged); len(failures) != 0 {
		t.Fatalf("finalize failures=%#v", failures)
	}
	if _, err := os.Stat(staged[0].stagedPath); !os.IsNotExist(err) {
		t.Fatalf("staged file remains: %v", err)
	}
}

func TestMissingSourceIsAlreadyDeleted(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.flac")
	resolved, err := secureSourcePath(root, "missing.flac")
	if err != nil || resolved != missing {
		t.Fatalf("secureSourcePath=%q,%v", resolved, err)
	}
	staged, err := stageSourceFilesForDeletion([]string{missing}, "track-id")
	if err != nil || len(staged) != 0 {
		t.Fatalf("stage missing=%#v,%v", staged, err)
	}
}

func TestReportedDeletionCounts(t *testing.T) {
	root := t.TempDir()
	paths := []string{filepath.Join(root, "one.flac"), filepath.Join(root, "two.flac")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	staged, err := stageSourceFilesForDeletion(paths, "track-id")
	if err != nil || len(staged) != 2 {
		t.Fatalf("staged=%#v error=%v", staged, err)
	}
	if deleted, quarantined := reportedDeletionCounts(staged, []string{staged[0].stagedPath}); deleted != 1 || quarantined != 1 {
		t.Fatalf("counts=%d/%d", deleted, quarantined)
	}
	restoreStagedSourceFiles(staged)
}
