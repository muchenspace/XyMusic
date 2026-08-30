package adminmedia

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/platform/localmedia"
)

type mediaCommandRunnerStub struct {
	calls int
}

func (stub *mediaCommandRunnerStub) Run(_ context.Context, executable string, arguments []string, _ time.Duration) (CommandResult, error) {
	stub.calls++
	if executable == "ffprobe" {
		// Audio probe
		if strings.Contains(strings.Join(arguments, " "), "a:0") {
			return CommandResult{Stdout: `{"format":{"duration":"180.5","bit_rate":"320000"},"streams":[{"sample_rate":"44100","channels":2}]}`}, nil
		}
		// Image probe
		return CommandResult{Stdout: `{"streams":[{"codec_type":"video","codec_name":"png","width":1000,"height":1000}]}`}, nil
	}
	if executable == "ffmpeg" {
		if err := os.WriteFile(arguments[len(arguments)-1], []byte("norm-image"), 0o600); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	}
	return CommandResult{}, errors.New("unknown executable")
}

func TestInspectorAudioSuccess(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	inputRelPath := "temp/upload_1.partial"
	inputAbsPath := filepath.Join(mediaStore.AssetDirectory(), "temp", "upload_1.partial")
	_ = os.MkdirAll(filepath.Dir(inputAbsPath), 0o755)
	if err := os.WriteFile(inputAbsPath, []byte("audio-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	inspector, err := newFFmpegMediaInspector(mediaStore, "ffprobe", "ffmpeg", &mediaCommandRunnerStub{})
	if err != nil {
		t.Fatal(err)
	}

	inspected, err := inspector.Inspect(context.Background(), UploadReservation{
		ID:                     "upload-1",
		Purpose:                PurposeTrackSource,
		StoragePath:            inputRelPath,
		ExpectedSize:           int64(len("audio-data")),
		ExpectedMIMEType:       "audio/flac",
		ExpectedChecksumSHA256: "abc",
		OriginalFileName:       "track.flac",
	})
	if err != nil {
		t.Fatalf("unexpected Inspect error: %v", err)
	}

	if inspected.Kind != "AUDIO_SOURCE" || inspected.StoragePath != "sources/upload-1.flac" {
		t.Fatalf("unexpected inspected audio: %+v", inspected)
	}
	if inspected.DurationMs == nil || *inspected.DurationMs != 180500 {
		t.Fatalf("unexpected duration: %v", inspected.DurationMs)
	}
}
