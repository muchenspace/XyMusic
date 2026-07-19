package adminmetadata

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProductionMetadataWritebackContainerRoundTrips(t *testing.T) {
	ffmpegPath := strings.TrimSpace(os.Getenv("XYMUSIC_TEST_FFMPEG"))
	ffprobePath := strings.TrimSpace(os.Getenv("XYMUSIC_TEST_FFPROBE"))
	if ffmpegPath == "" || ffprobePath == "" {
		t.Skip("set XYMUSIC_TEST_FFMPEG and XYMUSIC_TEST_FFPROBE to run media Tag round-trip checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runner := OSProcessRunner{}

	for _, fixture := range []struct {
		name      string
		extension string
		codec     string
	}{
		{name: "Vorbis", extension: ".ogg", codec: "libvorbis"},
		{name: "Opus", extension: ".opus", codec: "libopus"},
	} {
		t.Run(fixture.name+" overwrites existing stream tags", func(t *testing.T) {
			directory := t.TempDir()
			sourcePath := filepath.Join(directory, "source"+fixture.extension)
			outputPath := filepath.Join(directory, "output"+fixture.extension)
			runMetadataFixtureCommand(t, ctx, runner, ffmpegPath, []string{
				"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=2",
				"-c:a", fixture.codec, "-metadata", "title=Before", "-metadata", "artist=Artist", sourcePath,
			})
			before, err := ProbeMetadataFile(ctx, sourcePath, ffprobePath, runner)
			if err != nil {
				t.Fatal(err)
			}
			expected := before.Metadata
			expected.Title = "After"
			if err := RemuxMetadataToFile(ctx, sourcePath, outputPath, "", ffmpegPath, expected, runner); err != nil {
				t.Fatal(err)
			}
			after, err := ProbeMetadataFile(ctx, outputPath, ffprobePath, runner)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyMetadataRemux(before, after, expected); err != nil {
				t.Fatalf("metadata after writeback=%+v: %v", after.Metadata, err)
			}
		})
	}

	t.Run("MP3 preserves YYYY-MM", func(t *testing.T) {
		directory := t.TempDir()
		sourcePath := filepath.Join(directory, "source.mp3")
		outputPath := filepath.Join(directory, "output.mp3")
		runMetadataFixtureCommand(t, ctx, runner, ffmpegPath, []string{
			"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5",
			"-c:a", "libmp3lame", "-metadata", "title=Song", "-metadata", "artist=Artist", sourcePath,
		})
		before, err := ProbeMetadataFile(ctx, sourcePath, ffprobePath, runner)
		if err != nil {
			t.Fatal(err)
		}
		expected := before.Metadata
		date := "2024-05"
		expected.ReleaseDate = &date
		if err := RemuxMetadataToFile(ctx, sourcePath, outputPath, "", ffmpegPath, expected, runner); err != nil {
			t.Fatal(err)
		}
		after, err := ProbeMetadataFile(ctx, outputPath, ffprobePath, runner)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyMetadataRemux(before, after, expected); err != nil {
			t.Fatalf("metadata after writeback=%+v: %v", after.Metadata, err)
		}
	})

	t.Run("M4A preserves attached artwork", func(t *testing.T) {
		directory := t.TempDir()
		audioPath := filepath.Join(directory, "audio.m4a")
		artworkPath := filepath.Join(directory, "cover.jpg")
		sourcePath := filepath.Join(directory, "source.m4a")
		outputPath := filepath.Join(directory, "output.m4a")
		runMetadataFixtureCommand(t, ctx, runner, ffmpegPath, []string{
			"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5",
			"-c:a", "aac", "-metadata", "title=Before", "-metadata", "artist=Artist", audioPath,
		})
		runMetadataFixtureCommand(t, ctx, runner, ffmpegPath, []string{
			"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=64x64",
			"-frames:v", "1", "-c:v", "mjpeg", artworkPath,
		})
		runMetadataFixtureCommand(t, ctx, runner, ffmpegPath, []string{
			"-nostdin", "-v", "error", "-y", "-i", audioPath, "-i", artworkPath,
			"-map", "0:a", "-map", "1:v:0", "-c", "copy", "-disposition:v:0", "attached_pic", sourcePath,
		})
		before, err := ProbeMetadataFile(ctx, sourcePath, ffprobePath, runner)
		if err != nil {
			t.Fatal(err)
		}
		if !before.Metadata.HasArtwork {
			t.Fatal("fixture does not contain attached artwork")
		}
		expected := before.Metadata
		expected.Title = "After"
		if err := RemuxMetadataToFile(ctx, sourcePath, outputPath, "", ffmpegPath, expected, runner); err != nil {
			t.Fatal(err)
		}
		after, err := ProbeMetadataFile(ctx, outputPath, ffprobePath, runner)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyMetadataRemux(before, after, expected); err != nil {
			t.Fatalf("metadata after writeback=%+v: %v", after.Metadata, err)
		}
	})

	t.Run("long lyrics bypass the Windows command-line limit", func(t *testing.T) {
		directory := t.TempDir()
		sourcePath := filepath.Join(directory, "source.flac")
		outputPath := filepath.Join(directory, "output.flac")
		runMetadataFixtureCommand(t, ctx, runner, ffmpegPath, []string{
			"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5",
			"-c:a", "flac", "-metadata", "title=Song", "-metadata", "artist=Artist", sourcePath,
		})
		before, err := ProbeMetadataFile(ctx, sourcePath, ffprobePath, runner)
		if err != nil {
			t.Fatal(err)
		}
		expected := before.Metadata
		expected.Lyrics = &MetadataLyrics{
			Content: cleanMultiline(strings.Repeat("A long lyric line with =, ;, # and \\\r\n", 1_000)),
			Format:  "PLAIN", Language: "zh-cn",
		}
		if err := RemuxMetadataToFile(ctx, sourcePath, outputPath, "", ffmpegPath, expected, runner); err != nil {
			t.Fatal(err)
		}
		after, err := ProbeMetadataFile(ctx, outputPath, ffprobePath, runner)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyMetadataRemux(before, after, expected); err != nil {
			t.Fatalf("lyrics length after writeback=%d: %v", len(after.Metadata.Lyrics.Content), err)
		}
	})
}

func runMetadataFixtureCommand(
	t *testing.T,
	ctx context.Context,
	runner ProcessRunner,
	ffmpegPath string,
	arguments []string,
) {
	t.Helper()
	result, err := runner.Run(ctx, ffmpegPath, arguments, 30*time.Second)
	if err != nil || result.TimedOut || result.ExitCode != 0 {
		t.Fatalf("fixture command exit=%d timeout=%v error=%v stderr=%s", result.ExitCode, result.TimedOut, err, result.Stderr)
	}
}
