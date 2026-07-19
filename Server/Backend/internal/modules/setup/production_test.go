package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"xymusic/server/internal/config"
)

func TestFileConfigurationRepositoryUsesAtomicStoreContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	repository := NewFileConfigurationRepository(path)
	candidate, err := config.Parse(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Save(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := repository.Load(context.Background())
	if err != nil || !exists {
		t.Fatalf("saved configuration did not load: exists=%v err=%v", exists, err)
	}
	if loaded.Database.URL != candidate.Database.URL || loaded.Storage.Bucket != candidate.Storage.Bucket {
		t.Fatalf("configuration round trip mismatch: %#v", loaded)
	}
	if _, err := os.Stat(path + ".next"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("atomic write temporary file remains: %v", err)
	}
	if err := repository.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := repository.Load(context.Background()); err != nil || exists {
		t.Fatalf("cleared repository still appears configured: exists=%v err=%v", exists, err)
	}

	if err := os.WriteFile(path, []byte("not an environment assignment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Load(context.Background()); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("malformed environment should use the setup sentinel, got %v", err)
	}
}

func TestOSSourceValidatorValidatesReadWriteDirectoryAndPatterns(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "music")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	enabled := true
	syncOnStartup := true
	source, err := (OSSourceValidator{}).Validate(context.Background(), SourceInput{
		Name: " Music ", Directory: "music", Mode: "READ_WRITE",
		Enabled: &enabled, SyncOnStartup: &syncOnStartup,
		IncludePatterns: []string{" **/*.flac ", "**/*.flac"},
		ExcludePatterns: []string{".trash/**"},
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if source.Name != "Music" || source.Path != directory || source.Status != "UNKNOWN" {
		t.Fatalf("source normalization mismatch: %#v", source)
	}
	if len(source.IncludePatterns) != 1 || source.IncludePatterns[0] != "**/*.flac" {
		t.Fatalf("source patterns were not normalized: %#v", source.IncludePatterns)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("write probe left files behind: %#v", entries)
	}
}

func TestProductionAdaptersRejectInvalidStorageAndProbeListener(t *testing.T) {
	if _, err := (ProductionObjectStorageFactory{}).Open(config.Storage{Endpoint: "ftp://invalid"}); err == nil {
		t.Fatal("invalid object storage endpoint was accepted")
	}
	if err := (NetworkListenerProbe{}).Check(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("ephemeral listener probe failed: %v", err)
	}
}

func TestCommandMediaToolRecognizesBundledFFmpeg(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the repository bundles the Windows FFmpeg runtime")
	}
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "RunTime", "ffmpeg", "ffmpeg.exe"))
	if _, err := os.Stat(path); err != nil {
		t.Skipf("bundled FFmpeg is unavailable: %v", err)
	}
	version, err := (CommandMediaTool{}).Version(context.Background(), path, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(version), "ffmpeg ") {
		t.Fatalf("unexpected FFmpeg version: %q", version)
	}
}
