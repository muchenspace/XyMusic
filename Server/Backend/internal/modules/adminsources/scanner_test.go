package adminsources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestFilesystemScannerDiscoversPatternsCUEAndRecordsFailures(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.flac")
	for path, content := range map[string]string{
		filepath.Join(root, "album", "song.flac"): "flac",
		filepath.Join(root, "skip.mp3"):           "mp3",
		filepath.Join(root, "disc.wav"):           "wav",
		filepath.Join(root, "disc.cue"):           `FILE "disc.wav" WAVE`,
		filepath.Join(root, "bad.cue"):            `FILE "../` + filepath.Base(outside) + `" WAVE`,
		outside:                                   "outside",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	synchronizer := &fileSynchronizerStub{archived: 2}
	scanner, err := NewFilesystemScanner(synchronizer)
	if err != nil {
		t.Fatal(err)
	}
	var progress []ScanProgress
	result, err := scanner.Scan(context.Background(), ScanInput{
		ScanRunID: testRunID, RootID: testRootID, Directory: root,
		IncludePatterns: []string{"**/*.flac", "**/*.wav"}, ExcludePatterns: []string{"skip*"},
		OnProgress: func(_ context.Context, value ScanProgress) error {
			progress = append(progress, value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DiscoveredFiles != 3 || result.ProcessedFiles != 3 || result.FailedFiles != 1 || result.ArchivedFiles != 2 {
		t.Fatalf("result=%+v", result)
	}
	if len(progress) != 4 || progress[0] != (ScanProgress{}) || progress[len(progress)-1].ProcessedFiles != 3 {
		t.Fatalf("progress=%+v", progress)
	}
	paths := make([]string, 0, len(synchronizer.files))
	var cue, failure bool
	for _, file := range synchronizer.files {
		paths = append(paths, file.RelativePath)
		cue = cue || file.CuePath != ""
		failure = failure || file.ScanError != nil
	}
	sort.Strings(paths)
	if !cue || !failure {
		t.Fatalf("files=%+v", synchronizer.files)
	}
	for _, scanRunID := range synchronizer.scanRunIDs {
		if scanRunID != testRunID {
			t.Fatalf("scan run id=%q want=%q", scanRunID, testRunID)
		}
	}
}

func TestFilesystemScannerHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}
	synchronizer := &fileSynchronizerStub{}
	scanner, _ := NewFilesystemScanner(synchronizer)
	_, err := scanner.Scan(context.Background(), ScanInput{
		RootID: testRootID, Directory: root,
		IsCancelled: func(context.Context) (bool, error) { return true, nil },
	})
	if !errors.Is(err, ErrScanCancelled) {
		t.Fatalf("err=%v", err)
	}
	if len(synchronizer.files) != 0 {
		t.Fatalf("processed files=%d", len(synchronizer.files))
	}
}

type fileSynchronizerStub struct {
	mu         sync.Mutex
	files      []DiscoveredFile
	scanRunIDs []string
	archived   int
}

func (stub *fileSynchronizerStub) ProcessFile(
	_ context.Context,
	_, scanRunID string,
	file DiscoveredFile,
	_ time.Time,
) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.files = append(stub.files, file)
	stub.scanRunIDs = append(stub.scanRunIDs, scanRunID)
	return nil
}
func (stub *fileSynchronizerStub) ArchiveMissing(context.Context, string, time.Time, time.Time) (int, error) {
	return stub.archived, nil
}
