package profile

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
	imageInspectionTimeout = 15 * time.Second
	imageNormalizeTimeout  = 30 * time.Second
	maximumImageDimension  = 8192
	maximumImagePixels     = 32_000_000
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

type FFmpegAvatarInspector struct {
	localMedia  *localmedia.Store
	ffprobePath string
	ffmpegPath  string
	runner      CommandRunner
}

var _ AvatarInspector = (*FFmpegAvatarInspector)(nil)

func NewFFmpegAvatarInspector(
	localMedia *localmedia.Store,
	ffprobePath string,
	ffmpegPath string,
) (*FFmpegAvatarInspector, error) {
	return newFFmpegAvatarInspector(localMedia, ffprobePath, ffmpegPath, osCommandRunner{})
}

func newFFmpegAvatarInspector(
	localMedia *localmedia.Store,
	ffprobePath string,
	ffmpegPath string,
	runner CommandRunner,
) (*FFmpegAvatarInspector, error) {
	if localMedia == nil {
		return nil, errors.New("avatar inspector local media store is required")
	}
	if strings.TrimSpace(ffprobePath) == "" {
		return nil, errors.New("avatar inspector ffprobe path is required")
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		return nil, errors.New("avatar inspector ffmpeg path is required")
	}
	if runner == nil {
		runner = osCommandRunner{}
	}
	return &FFmpegAvatarInspector{
		localMedia:  localMedia,
		ffprobePath: ffprobePath,
		ffmpegPath:  ffmpegPath,
		runner:      runner,
	}, nil
}

func (inspector *FFmpegAvatarInspector) Inspect(
	ctx context.Context,
	upload AvatarUpload,
) (InspectedAvatar, error) {
	inputPath, err := inspector.localMedia.ResolveAssetPath(upload.StoragePath)
	if err != nil {
		return InspectedAvatar{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded avatar file path is invalid", nil)
	}

	info, err := os.Stat(inputPath)
	if err != nil || info.IsDir() {
		return InspectedAvatar{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded avatar file is missing", nil)
	}

	if info.Size() != upload.ExpectedSize {
		return InspectedAvatar{}, apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "Uploaded file size mismatch", nil)
	}

	probe, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type,codec_name,width,height",
		"-of", "json",
		inputPath,
	}, imageInspectionTimeout)
	if err != nil {
		return InspectedAvatar{}, apperror.DependencyUnavailable("Avatar image inspection is unavailable")
	}
	if probe.TimedOut {
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Image processing timed out",
			nil,
		)
	}
	if probe.ExitCode != 0 {
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Uploaded image is invalid",
			nil,
		)
	}
	width, height, codec, err := parseImageProbe(probe.Stdout)
	if err != nil || !validImageDimensions(width, height) || !codecMatchesMIME(codec, upload.ExpectedMIMEType) {
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Uploaded image dimensions or encoding are invalid or unsafe",
			nil,
		)
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
		"-vf", fmt.Sprintf(
			"scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease",
			normalizedImageMaxEdge,
			normalizedImageMaxEdge,
		),
		"-c:v", "mjpeg",
		"-q:v", "2",
		normalizedTempPath,
	}, imageNormalizeTimeout)
	if err != nil {
		return InspectedAvatar{}, apperror.DependencyUnavailable("Avatar image normalization is unavailable")
	}
	if normalized.TimedOut || normalized.ExitCode != 0 {
		_ = os.Remove(normalizedTempPath)
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Image normalization failed",
			nil,
		)
	}

	normalizedSize, normalizedChecksum, err := fileSizeAndSHA256(normalizedTempPath)
	if err != nil {
		_ = os.Remove(normalizedTempPath)
		return InspectedAvatar{}, fmt.Errorf("inspect normalized avatar: %w", err)
	}

	finalWidth, finalHeight, err := probeNormalizedImage(ctx, inspector, normalizedTempPath)
	if err != nil {
		_ = os.Remove(normalizedTempPath)
		return InspectedAvatar{}, err
	}

	destRelPath := fmt.Sprintf("avatars/%s.jpg", upload.ID)
	committedRelPath, err := inspector.localMedia.CommitUpload(ctx, normalizedTempPath, destRelPath)
	if err != nil {
		_ = os.Remove(normalizedTempPath)
		return InspectedAvatar{}, fmt.Errorf("commit normalized avatar: %w", err)
	}

	// Remove input partial file
	_ = os.Remove(inputPath)

	return InspectedAvatar{
		StoragePath:    committedRelPath,
		MIMEType:       "image/jpeg",
		SizeBytes:      normalizedSize,
		ChecksumSHA256: normalizedChecksum,
		Width:          finalWidth,
		Height:         finalHeight,
	}, nil
}

func parseImageProbe(raw string) (int, int, string, error) {
	var payload struct {
		Streams []struct {
			CodecType string      `json:"codec_type"`
			CodecName string      `json:"codec_name"`
			Width     json.Number `json:"width"`
			Height    json.Number `json:"height"`
		} `json:"streams"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return 0, 0, "", err
	}
	for _, stream := range payload.Streams {
		if stream.CodecType != "video" {
			continue
		}
		width, widthErr := strconv.Atoi(stream.Width.String())
		height, heightErr := strconv.Atoi(stream.Height.String())
		if widthErr != nil || heightErr != nil {
			return 0, 0, "", errors.New("image dimensions are not integers")
		}
		return width, height, strings.ToLower(stream.CodecName), nil
	}
	return 0, 0, "", errors.New("image stream was not found")
}

func probeNormalizedImage(
	ctx context.Context,
	inspector *FFmpegAvatarInspector,
	path string,
) (int, int, error) {
	result, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type,codec_name,width,height",
		"-of", "json",
		path,
	}, imageInspectionTimeout)
	if err != nil {
		return 0, 0, apperror.DependencyUnavailable("Normalized avatar inspection is unavailable")
	}
	if result.TimedOut || result.ExitCode != 0 {
		return 0, 0, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Normalized avatar could not be verified",
			nil,
		)
	}
	width, height, codec, err := parseImageProbe(result.Stdout)
	if err != nil || codec != "mjpeg" || !validImageDimensions(width, height) || width > normalizedImageMaxEdge || height > normalizedImageMaxEdge {
		return 0, 0, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Normalized avatar could not be verified",
			nil,
		)
	}
	return width, height, nil
}

func validImageDimensions(width, height int) bool {
	return width > 0 && height > 0 &&
		width <= maximumImageDimension && height <= maximumImageDimension &&
		int64(width)*int64(height) <= maximumImagePixels
}

func codecMatchesMIME(codec, mimeType string) bool {
	switch strings.ToLower(mimeType) {
	case "image/jpeg":
		return codec == "mjpeg" || codec == "jpeg"
	case "image/png":
		return codec == "png"
	case "image/webp":
		return codec == "webp"
	default:
		return false
	}
}

func fileSizeAndSHA256(path string) (int64, string, error) {
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

func (osCommandRunner) Run(
	ctx context.Context,
	executable string,
	arguments []string,
	timeout time.Duration,
) (CommandResult, error) {
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, executable, arguments...)
	var stdout strings.Builder
	var stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	return CommandResult{}, err
}
