package adminmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

const (
	mediaInspectionTimeout = 15 * time.Second
	imageNormalizeTimeout  = 30 * time.Second
	normalizedImageMaxEdge = 1600
)

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

type CommandRunner interface {
	Run(context.Context, string, []string, time.Duration) (CommandResult, error)
}

type FFmpegMediaInspector struct {
	localMedia  *localmedia.Store
	ffprobePath string
	ffmpegPath  string
	runner      CommandRunner
}

var _ MediaInspector = (*FFmpegMediaInspector)(nil)

func NewFFmpegMediaInspector(
	localMedia *localmedia.Store,
	ffprobePath string,
	ffmpegPath string,
) (*FFmpegMediaInspector, error) {
	return newFFmpegMediaInspector(localMedia, ffprobePath, ffmpegPath, osCommandRunner{})
}

func newFFmpegMediaInspector(
	localMedia *localmedia.Store,
	ffprobePath string,
	ffmpegPath string,
	runner CommandRunner,
) (*FFmpegMediaInspector, error) {
	if localMedia == nil {
		return nil, errors.New("admin media local media store is required")
	}
	if strings.TrimSpace(ffprobePath) == "" {
		return nil, errors.New("admin media ffprobe path is required")
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		return nil, errors.New("admin media ffmpeg path is required")
	}
	if runner == nil {
		runner = osCommandRunner{}
	}
	return &FFmpegMediaInspector{
		localMedia:  localMedia,
		ffprobePath: ffprobePath,
		ffmpegPath:  ffmpegPath,
		runner:      runner,
	}, nil
}

func (inspector *FFmpegMediaInspector) Inspect(
	ctx context.Context,
	upload UploadReservation,
) (InspectedMedia, error) {
	inputPath, err := inspector.localMedia.ResolveAssetPath(upload.StoragePath)
	if err != nil {
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded file path is invalid", nil)
	}

	info, err := os.Stat(inputPath)
	if err != nil || info.IsDir() {
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded file is missing", nil)
	}

	if info.Size() != upload.ExpectedSize {
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded file size mismatch", nil)
	}

	if upload.Purpose == PurposeTrackSource {
		return inspector.inspectAudio(ctx, upload, inputPath, info.Size())
	}
	return inspector.inspectArtwork(ctx, upload, inputPath)
}

func (inspector *FFmpegMediaInspector) inspectAudio(
	ctx context.Context,
	upload UploadReservation,
	inputPath string,
	fileSize int64,
) (InspectedMedia, error) {
	probe, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "format=duration,bit_rate:stream=codec_name,sample_rate,channels",
		"-of", "json",
		inputPath,
	}, mediaInspectionTimeout)
	if err != nil {
		return InspectedMedia{}, apperror.DependencyUnavailable("Audio inspection is unavailable")
	}
	if probe.TimedOut || probe.ExitCode != 0 {
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded audio is invalid", nil)
	}

	durationMs, bitrate, sampleRate, channels, err := parseAudioProbe(probe.Stdout)
	if err != nil {
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded audio streams could not be decoded", nil)
	}

	ext := filepath.Ext(upload.OriginalFileName)
	if ext == "" {
		ext = ".bin"
	}
	destRelPath := fmt.Sprintf("sources/%s%s", upload.ID, ext)
	committedRelPath, err := inspector.localMedia.CommitUpload(ctx, inputPath, destRelPath)
	if err != nil {
		return InspectedMedia{}, fmt.Errorf("commit audio asset: %w", err)
	}

	return InspectedMedia{
		StoragePath:    committedRelPath,
		Kind:           "AUDIO_SOURCE",
		MIMEType:       upload.ExpectedMIMEType,
		SizeBytes:      fileSize,
		ChecksumSHA256: upload.ExpectedChecksumSHA256,
		DurationMs:     &durationMs,
		Bitrate:        bitrate,
		SampleRate:     sampleRate,
		Channels:       channels,
	}, nil
}

func (inspector *FFmpegMediaInspector) inspectArtwork(
	ctx context.Context,
	upload UploadReservation,
	inputPath string,
) (InspectedMedia, error) {
	probe, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type,codec_name,width,height",
		"-of", "json",
		inputPath,
	}, mediaInspectionTimeout)
	if err != nil {
		return InspectedMedia{}, apperror.DependencyUnavailable("Artwork inspection is unavailable")
	}
	if probe.TimedOut || probe.ExitCode != 0 {
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded image is invalid", nil)
	}

	tempDir := filepath.Join(inspector.localMedia.AssetDirectory(), "temp")
	_ = os.MkdirAll(tempDir, 0o755)
	normalizedTempPath := filepath.Join(tempDir, fmt.Sprintf("norm_%s.jpg", upload.ID))

	normalized, err := inspector.runner.Run(ctx, inspector.ffmpegPath, []string{
		"-nostdin", "-v", "error", "-y",
		"-i", inputPath,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-map_metadata", "-1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", normalizedImageMaxEdge, normalizedImageMaxEdge),
		"-c:v", "mjpeg",
		"-q:v", "2",
		normalizedTempPath,
	}, imageNormalizeTimeout)
	if err != nil {
		return InspectedMedia{}, apperror.DependencyUnavailable("Artwork normalization is unavailable")
	}
	if normalized.TimedOut || normalized.ExitCode != 0 {
		_ = os.Remove(normalizedTempPath)
		return InspectedMedia{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Artwork normalization failed", nil)
	}

	normalizedSize, normalizedChecksum, err := fileSHA256(normalizedTempPath)
	if err != nil {
		_ = os.Remove(normalizedTempPath)
		return InspectedMedia{}, err
	}

	width, height := 800, 800
	destRelPath := fmt.Sprintf("artworks/%s.jpg", upload.ID)
	committedRelPath, err := inspector.localMedia.CommitUpload(ctx, normalizedTempPath, destRelPath)
	if err != nil {
		_ = os.Remove(normalizedTempPath)
		return InspectedMedia{}, err
	}
	_ = os.Remove(inputPath)

	return InspectedMedia{
		StoragePath:    committedRelPath,
		Kind:           "ARTWORK",
		MIMEType:       "image/jpeg",
		SizeBytes:      normalizedSize,
		ChecksumSHA256: normalizedChecksum,
		Width:          &width,
		Height:         &height,
	}, nil
}

func parseAudioProbe(raw string) (durationMs int64, bitrate *int, sampleRate *int, channels *int, err error) {
	var payload struct {
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, nil, nil, nil, err
	}
	if len(payload.Streams) == 0 {
		return 0, nil, nil, nil, errors.New("no audio stream found")
	}
	durSec, err := strconv.ParseFloat(payload.Format.Duration, 64)
	if err == nil {
		durationMs = int64(durSec * 1000)
	}
	if br, err := strconv.Atoi(payload.Format.BitRate); err == nil && br > 0 {
		bitrate = &br
	}
	if sr, err := strconv.Atoi(payload.Streams[0].SampleRate); err == nil && sr > 0 {
		sampleRate = &sr
	}
	if ch := payload.Streams[0].Channels; ch > 0 {
		channels = &ch
	}
	return durationMs, bitrate, sampleRate, channels, nil
}

func fileSHA256(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, "", err
	}
	hasher := sha256.New()
	if _, err := file.WriteTo(hasher); err != nil {
		return 0, "", err
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, executable string, args []string, timeout time.Duration) (CommandResult, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, executable, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		return res, nil
	}
	if err == nil {
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return CommandResult{}, err
}
