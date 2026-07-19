package adminmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestInspectorRejectsObjectMetadataMismatchBeforeMediaCommands(t *testing.T) {
	runner := &mediaCommandRunnerStub{}
	storage := &mediaStorageStub{downloadToFile: func(_ context.Context, _ string, path string, _ int64) (StoredObject, error) {
		if err := os.WriteFile(path, []byte("mismatch"), 0o600); err != nil {
			return StoredObject{}, err
		}
		return StoredObject{
			SizeBytes: 8, ContentType: "image/jpeg", ETag: "actual-etag",
			ChecksumSHA256: stringOf('b', 64), MetadataSHA256: stringOf('c', 64),
		}, nil
	}}
	inspector, err := newFFmpegUploadInspector(storage, "ffprobe", "ffmpeg", runner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = inspector.Inspect(context.Background(), MediaUpload{
		ID: "upload-1", Purpose: PurposeArtistArtwork, TargetID: "artist-1",
		ObjectKey: "uploads/admin/upload-1", ExpectedSize: 9,
		ExpectedMIMEType: "image/png", ExpectedChecksumSHA256: stringOf('a', 64),
	}, "expected-etag")
	if !apperror.IsCode(err, apperror.CodeMediaUploadMismatch) || runner.calls != 0 {
		t.Fatalf("error/calls = %v / %d", err, runner.calls)
	}
}

func TestInspectorValidatesTrackBinaryAndAudioStream(t *testing.T) {
	payload := testWAV()
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	storage := &mediaStorageStub{downloadToFile: func(_ context.Context, _ string, path string, maximum int64) (StoredObject, error) {
		if maximum != int64(len(payload))+1 {
			t.Fatalf("maximum = %d", maximum)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			return StoredObject{}, err
		}
		return StoredObject{
			SizeBytes: int64(len(payload)), ContentType: "audio/wav",
			ETag: "etag-1", ChecksumSHA256: checksum, MetadataSHA256: checksum,
		}, nil
	}}
	runner := &mediaCommandRunnerStub{run: func(_ context.Context, command string, arguments []string, timeout time.Duration) (CommandResult, error) {
		if command != "ffprobe" || timeout != mediaInspectionTimeout || len(arguments) == 0 {
			t.Fatalf("command = %q %#v %s", command, arguments, timeout)
		}
		return CommandResult{Stdout: `{"streams":[{"codec_type":"audio"}]}`}, nil
	}}
	inspector, err := newFFmpegUploadInspector(storage, "ffprobe", "ffmpeg", runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := inspector.Inspect(context.Background(), MediaUpload{
		ID: "upload-1", Purpose: PurposeTrackSource, TargetID: "track-1",
		ObjectKey: "uploads/admin/upload-1", ExpectedSize: int64(len(payload)),
		ExpectedMIMEType: "audio/wav", ExpectedChecksumSHA256: checksum,
	}, `"etag-1"`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObjectKey != "uploads/admin/upload-1" || result.MIMEType != "audio/wav" ||
		result.ChecksumSHA256 != checksum || runner.calls != 1 {
		t.Fatalf("result/calls = %#v / %d", result, runner.calls)
	}
}

func TestInspectorNormalizesArtworkAndReturnsCleanupKeys(t *testing.T) {
	payload := testPNG(t)
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	storage := &mediaStorageStub{}
	storage.downloadToFile = func(_ context.Context, _ string, path string, _ int64) (StoredObject, error) {
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			return StoredObject{}, err
		}
		return StoredObject{
			SizeBytes: int64(len(payload)), ContentType: "image/png",
			ETag: "etag-1", ChecksumSHA256: checksum, MetadataSHA256: checksum,
		}, nil
	}
	storage.uploadFile = func(_ context.Context, key, path, contentType, suppliedChecksum string) (int64, error) {
		if key != "media/artwork/artist_artwork/artist-1/upload-1.jpg" || contentType != "image/jpeg" {
			t.Fatalf("normalized upload = %q %q", key, contentType)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		digest := sha256.Sum256(data)
		if suppliedChecksum != hex.EncodeToString(digest[:]) {
			t.Fatalf("normalized checksum = %q", suppliedChecksum)
		}
		return int64(len(data)), nil
	}
	runner := &mediaCommandRunnerStub{run: func(_ context.Context, command string, arguments []string, _ time.Duration) (CommandResult, error) {
		switch {
		case command == "ffprobe" && arguments[len(arguments)-1] != "":
			if runnerCallPath(arguments) == "normalized.jpg" {
				return CommandResult{Stdout: `{"streams":[{"codec_type":"video","codec_name":"mjpeg","width":4,"height":3}]}`}, nil
			}
			return CommandResult{Stdout: `{"streams":[{"codec_type":"video","codec_name":"png","width":4,"height":3}]}`}, nil
		case command == "ffmpeg":
			outputPath := arguments[len(arguments)-1]
			if err := writeTestJPEG(outputPath); err != nil {
				return CommandResult{}, err
			}
			return CommandResult{}, nil
		default:
			return CommandResult{}, errors.New("unexpected command")
		}
	}}
	inspector, err := newFFmpegUploadInspector(storage, "ffprobe", "ffmpeg", runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := inspector.Inspect(context.Background(), MediaUpload{
		ID: "upload-1", Purpose: PurposeArtistArtwork, TargetID: "artist-1",
		ObjectKey: "uploads/admin/upload-1", ExpectedSize: int64(len(payload)),
		ExpectedMIMEType: "image/png", ExpectedChecksumSHA256: checksum,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.ObjectKey != "media/artwork/artist_artwork/artist-1/upload-1.jpg" ||
		result.MIMEType != "image/jpeg" || result.Width == nil || *result.Width != 4 ||
		result.Height == nil || *result.Height != 3 ||
		len(result.CleanupKeys) != 2 || runner.calls != 3 {
		t.Fatalf("result/calls = %#v / %d", result, runner.calls)
	}
}

type mediaCommandRunnerStub struct {
	calls int
	run   func(context.Context, string, []string, time.Duration) (CommandResult, error)
}

func (runner *mediaCommandRunnerStub) Run(ctx context.Context, command string, arguments []string, timeout time.Duration) (CommandResult, error) {
	runner.calls++
	if runner.run == nil {
		return CommandResult{}, errors.New("unexpected command")
	}
	return runner.run(ctx, command, arguments, timeout)
}

func runnerCallPath(arguments []string) string {
	if len(arguments) == 0 {
		return ""
	}
	path := arguments[len(arguments)-1]
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] == '/' || path[index] == '\\' {
			return path[index+1:]
		}
	}
	return path
}

func writeTestJPEG(path string) error {
	imageValue := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			imageValue.Set(x, y, color.RGBA{R: uint8(x * 40), G: uint8(y * 60), B: 180, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encodeErr := jpeg.Encode(file, imageValue, &jpeg.Options{Quality: 90})
	closeErr := file.Close()
	return errors.Join(encodeErr, closeErr)
}

func testWAV() []byte {
	data := []byte{128, 130, 126, 128}
	var output bytes.Buffer
	output.WriteString("RIFF")
	_ = binary.Write(&output, binary.LittleEndian, uint32(36+len(data)))
	output.WriteString("WAVEfmt ")
	_ = binary.Write(&output, binary.LittleEndian, uint32(16))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint32(8000))
	_ = binary.Write(&output, binary.LittleEndian, uint32(8000))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(8))
	output.WriteString("data")
	_ = binary.Write(&output, binary.LittleEndian, uint32(len(data)))
	output.Write(data)
	return output.Bytes()
}
