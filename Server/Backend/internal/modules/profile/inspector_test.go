package profile

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

type inspectorStorageStub struct {
	observed       StoredObject
	uploadedKey    string
	uploadedMIME   string
	uploadedSHA256 string
}

func (*inspectorStorageStub) CreateUploadURL(context.Context, UploadURLRequest) (string, error) {
	return "", errors.New("unexpected CreateUploadURL call")
}
func (stub *inspectorStorageStub) DownloadToFile(_ context.Context, _ string, destination string, _ int64) (StoredObject, error) {
	if err := os.WriteFile(destination, []byte("input-image"), 0o600); err != nil {
		return StoredObject{}, err
	}
	return stub.observed, nil
}
func (stub *inspectorStorageStub) UploadFile(_ context.Context, objectKey, path, contentType, checksum string) (int64, error) {
	stub.uploadedKey = objectKey
	stub.uploadedMIME = contentType
	stub.uploadedSHA256 = checksum
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

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
		return CommandResult{Stdout: `{"streams":[{"codec_type":"video","codec_name":"mjpeg","width":1600,"height":800}]}`}, nil
	default:
		return CommandResult{}, errors.New("unexpected command")
	}
}

func TestFFmpegAvatarInspectorValidatesAndNormalizesObject(t *testing.T) {
	checksum := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	storage := &inspectorStorageStub{observed: StoredObject{
		SizeBytes:      11,
		ContentType:    "image/png",
		ETag:           "etag-1",
		ChecksumSHA256: checksum,
		MetadataSHA256: checksum,
	}}
	runner := &inspectorRunnerStub{}
	inspector, err := newFFmpegAvatarInspector(storage, "ffprobe", "ffmpeg", runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := inspector.Inspect(context.Background(), AvatarUpload{
		ID:                     "upload-1",
		TargetID:               "user-1",
		ObjectKey:              "uploads/user-1/upload-1",
		ExpectedSize:           11,
		ExpectedMIMEType:       "image/png",
		ExpectedChecksumSHA256: checksum,
	}, `"etag-1"`)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 3 || result.ObjectKey != "media/artwork/user_avatar/user-1/upload-1.jpg" ||
		result.MIMEType != "image/jpeg" || result.Width != 1600 || result.Height != 800 || result.SizeBytes != int64(len("normalized-jpeg")) {
		t.Fatalf("inspection = %#v calls=%d", result, runner.calls)
	}
	if storage.uploadedKey != result.ObjectKey || storage.uploadedMIME != "image/jpeg" || storage.uploadedSHA256 != result.ChecksumSHA256 {
		t.Fatalf("uploaded = %q %q %q", storage.uploadedKey, storage.uploadedMIME, storage.uploadedSHA256)
	}
	if len(result.CleanupKeys) != 2 || result.CleanupKeys[0] != "uploads/user-1/upload-1" || result.CleanupKeys[1] != result.ObjectKey {
		t.Fatalf("cleanup keys = %#v", result.CleanupKeys)
	}
}

func TestFFmpegAvatarInspectorRejectsActualObjectMismatchBeforeProcessing(t *testing.T) {
	storage := &inspectorStorageStub{observed: StoredObject{
		SizeBytes:      12,
		ContentType:    "image/jpeg",
		ETag:           "different",
		ChecksumSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		MetadataSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	runner := &inspectorRunnerStub{}
	inspector, err := newFFmpegAvatarInspector(storage, "ffprobe", "ffmpeg", runner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = inspector.Inspect(context.Background(), AvatarUpload{
		ID:                     "upload-1",
		TargetID:               "user-1",
		ObjectKey:              "uploads/user-1/upload-1",
		ExpectedSize:           11,
		ExpectedMIMEType:       "image/png",
		ExpectedChecksumSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "expected")
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeMediaUploadMismatch || runner.calls != 0 {
		t.Fatalf("error = %#v calls=%d", err, runner.calls)
	}
	mismatches, ok := applicationError.Metadata["mismatches"].([]string)
	if !ok || len(mismatches) != 5 {
		t.Fatalf("mismatches = %#v", applicationError.Metadata["mismatches"])
	}
}

func TestImageProbeValidation(t *testing.T) {
	width, height, codec, err := parseImageProbe(`{"streams":[{"codec_type":"video","codec_name":"webp","width":512,"height":512}]}`)
	if err != nil || width != 512 || height != 512 || codec != "webp" || !codecMatchesMIME(codec, "image/webp") {
		t.Fatalf("probe = %d %d %q %v", width, height, codec, err)
	}
	if validImageDimensions(8192, 8192) {
		t.Fatal("image above the 32 megapixel budget was accepted")
	}
}
