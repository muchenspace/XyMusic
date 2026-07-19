package adminmetadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/config"
)

func TestProductionRemuxEmbedsAlbumArtworkInMP3(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production artwork writeback checks")
	}
	absEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absEnvironmentPath))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.mp3")
	artworkPath := filepath.Join(directory, "cover.jpg")
	outputPath := filepath.Join(directory, "output.mp3")
	runner := OSProcessRunner{}
	for _, command := range [][]string{
		{"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5", "-c:a", "libmp3lame", "-metadata", "title=Before", sourcePath},
		{"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=64x64", "-frames:v", "1", artworkPath},
	} {
		result, runErr := runner.Run(ctx, cfg.Media.FFmpegPath, command, 30*time.Second)
		if runErr != nil || result.TimedOut || result.ExitCode != 0 {
			t.Fatalf("generate fixture exit=%d timeout=%v error=%v stderr=%s", result.ExitCode, result.TimedOut, runErr, result.Stderr)
		}
	}
	before, err := ProbeMetadataFile(ctx, sourcePath, cfg.Media.FFprobePath, runner)
	if err != nil {
		t.Fatal(err)
	}
	if before.Metadata.HasArtwork {
		t.Fatal("fixture unexpectedly contains artwork")
	}
	expected := before.Metadata
	expected.Title = "After"
	expected.HasArtwork = true
	if err := RemuxMetadataToFile(ctx, sourcePath, outputPath, artworkPath, cfg.Media.FFmpegPath, expected, runner); err != nil {
		t.Fatal(err)
	}
	after, err := ProbeMetadataFile(ctx, outputPath, cfg.Media.FFprobePath, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Metadata.HasArtwork || after.Metadata.Title != "After" {
		t.Fatalf("written metadata=%+v", after.Metadata)
	}
	if err := VerifyMetadataRemux(before, after, expected); err != nil {
		t.Fatal(err)
	}
}
