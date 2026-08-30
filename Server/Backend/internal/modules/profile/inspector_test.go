package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/platform/localmedia"
)

type inspectorRunnerStub struct {
	calls int
}

func (runner *inspectorRunnerStub) Run(_ context.Context, executable string, arguments []string, _ time.Duration) (CommandResult, error) {
	runner.calls++
	switch runner.calls {
	case 1:
		if executable != "ffprobe" {
			return CommandResult{}, errors.New("expected ffprobe")
		}
		return CommandResult{Stdout: `{"streams":[{"codec_type":"video","codec_name":"png","width":2000,"height":1000}]}`}, nil
	case 2:
		if executable != "ffmpeg" {
			return CommandResult{}, errors.New("expected ffmpeg")
		}
		if err := os.WriteFile(arguments[len(arguments)-1], []byte("normalized-jpeg"), 0o600); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	case 3:
		if executable != "ffprobe" {
			return CommandResult{}, errors.New("expected ffprobe")
		}
		return CommandResult{Stdout: `{"streams":[{"codec_type":"video","codec_name":"mjpeg","width":1600,"height":800}]}`}, nil
	default:
		return CommandResult{}, errors.New("too many command runner invocations")
	}
}

func TestFFmpegAvatarInspectorSuccess(t *testing.T) {
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		10*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	inputRelPath := "temp/avatar_upload-1.partial"
	inputAbsPath := filepath.Join(mediaStore.AssetDirectory(), "temp", "avatar_upload-1.partial")
	_ = os.MkdirAll(filepath.Dir(inputAbsPath), 0o755)
	if err := os.WriteFile(inputAbsPath, []byte("raw-png-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &inspectorRunnerStub{}
	inspector, err := newFFmpegAvatarInspector(mediaStore, "ffprobe", "ffmpeg", runner)
	if err != nil {
		t.Fatal(err)
	}

	upload := AvatarUpload{
		ID:               "upload-1",
		StoragePath:      inputRelPath,
		ExpectedSize:     int64(len("raw-png-data")),
		ExpectedMIMEType: "image/png",
	}

	inspected, err := inspector.Inspect(context.Background(), upload)
	if err != nil {
		t.Fatalf("unexpected Inspect error: %v", err)
	}

	if inspected.StoragePath != "avatars/upload-1.jpg" {
		t.Fatalf("unexpected storage path: %s", inspected.StoragePath)
	}
	if inspected.Width != 1600 || inspected.Height != 800 {
		t.Fatalf("unexpected dimensions: %dx%d", inspected.Width, inspected.Height)
	}
	if inspected.MIMEType != "image/jpeg" {
		t.Fatalf("unexpected MIME type: %s", inspected.MIMEType)
	}
}
