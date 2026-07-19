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
	storage     AvatarObjectStorage
	ffprobePath string
	ffmpegPath  string
	runner      CommandRunner
}

var _ AvatarInspector = (*FFmpegAvatarInspector)(nil)

func NewFFmpegAvatarInspector(
	storage AvatarObjectStorage,
	ffprobePath string,
	ffmpegPath string,
) (*FFmpegAvatarInspector, error) {
	return newFFmpegAvatarInspector(storage, ffprobePath, ffmpegPath, osCommandRunner{})
}

func newFFmpegAvatarInspector(
	storage AvatarObjectStorage,
	ffprobePath string,
	ffmpegPath string,
	runner CommandRunner,
) (*FFmpegAvatarInspector, error) {
	if storage == nil {
		return nil, errors.New("avatar inspector object storage is required")
	}
	if strings.TrimSpace(ffprobePath) == "" {
		return nil, errors.New("avatar inspector ffprobe path is required")
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		return nil, errors.New("avatar inspector ffmpeg path is required")
	}
	if runner == nil {
		return nil, errors.New("avatar inspector command runner is required")
	}
	return &FFmpegAvatarInspector{
		storage:     storage,
		ffprobePath: ffprobePath,
		ffmpegPath:  ffmpegPath,
		runner:      runner,
	}, nil
}

func (inspector *FFmpegAvatarInspector) Inspect(
	ctx context.Context,
	upload AvatarUpload,
	observedETag string,
) (InspectedAvatar, error) {
	directory, err := os.MkdirTemp("", "xymusic-avatar-inspection-")
	if err != nil {
		return InspectedAvatar{}, fmt.Errorf("create avatar inspection directory: %w", err)
	}
	defer os.RemoveAll(directory)
	inputPath := filepath.Join(directory, "input")
	observed, err := inspector.storage.DownloadToFile(ctx, upload.ObjectKey, inputPath, upload.ExpectedSize+1)
	if err != nil {
		return InspectedAvatar{}, apperror.DependencyUnavailable("Uploaded avatar could not be read from object storage")
	}
	mismatches := observedObjectMismatches(upload, observed, observedETag)
	if len(mismatches) > 0 {
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Uploaded object mismatched: "+strings.Join(mismatches, ", "),
			map[string]any{"mismatches": mismatches},
		)
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
			"Uploaded image is invalid: "+truncateDiagnostic(probe.Stderr, 300),
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
	outputPath := filepath.Join(directory, "normalized.jpg")
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
		outputPath,
	}, imageNormalizeTimeout)
	if err != nil {
		return InspectedAvatar{}, apperror.DependencyUnavailable("Avatar image normalization is unavailable")
	}
	if normalized.TimedOut {
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Image processing timed out",
			nil,
		)
	}
	if normalized.ExitCode != 0 {
		return InspectedAvatar{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Uploaded image is invalid: "+truncateDiagnostic(normalized.Stderr, 300),
			nil,
		)
	}
	normalizedSize, normalizedChecksum, err := fileSizeAndSHA256(outputPath)
	if err != nil {
		return InspectedAvatar{}, fmt.Errorf("inspect normalized avatar: %w", err)
	}
	finalWidth, finalHeight, err := probeNormalizedImage(ctx, inspector, outputPath)
	if err != nil {
		return InspectedAvatar{}, err
	}
	objectKey := "media/artwork/user_avatar/" + upload.TargetID + "/" + upload.ID + ".jpg"
	storedSize, err := inspector.storage.UploadFile(
		ctx,
		objectKey,
		outputPath,
		"image/jpeg",
		normalizedChecksum,
	)
	cleanupKeys := []string{upload.ObjectKey, objectKey}
	if err != nil {
		return InspectedAvatar{}, &AvatarInspectionFailure{
			Cause:       apperror.DependencyUnavailable("Normalized avatar could not be stored"),
			CleanupKeys: cleanupKeys,
		}
	}
	if storedSize != normalizedSize {
		return InspectedAvatar{}, &AvatarInspectionFailure{
			Cause: apperror.Unprocessable(
				apperror.CodeMediaUploadMismatch,
				"Normalized avatar size changed during storage",
				nil,
			),
			CleanupKeys: cleanupKeys,
		}
	}
	return InspectedAvatar{
		ObjectKey:      objectKey,
		MIMEType:       "image/jpeg",
		SizeBytes:      normalizedSize,
		ChecksumSHA256: normalizedChecksum,
		Width:          finalWidth,
		Height:         finalHeight,
		CleanupKeys:    cleanupKeys,
	}, nil
}

// AvatarInspectionFailure retains object keys created before an inspection
// failed, allowing the transactional upload lifecycle to enqueue cleanup.
type AvatarInspectionFailure struct {
	Cause       error
	CleanupKeys []string
}

func (failure *AvatarInspectionFailure) Error() string {
	if failure == nil || failure.Cause == nil {
		return "avatar inspection failed"
	}
	return failure.Cause.Error()
}

func (failure *AvatarInspectionFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func observedObjectMismatches(upload AvatarUpload, observed StoredObject, observedETag string) []string {
	var mismatches []string
	if observed.SizeBytes != upload.ExpectedSize {
		mismatches = append(mismatches, "sizeBytes")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(observed.ContentType, ";")[0]))
	if contentType != strings.ToLower(upload.ExpectedMIMEType) {
		mismatches = append(mismatches, "contentType")
	}
	if observed.ChecksumSHA256 != upload.ExpectedChecksumSHA256 {
		mismatches = append(mismatches, "checksumSha256")
	}
	if observed.MetadataSHA256 != "" && observed.MetadataSHA256 != upload.ExpectedChecksumSHA256 {
		mismatches = append(mismatches, "metadataSha256")
	}
	if observedETag != "" && normalizeETag(observed.ETag) != normalizeETag(observedETag) {
		mismatches = append(mismatches, "etag")
	}
	return mismatches
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

func truncateDiagnostic(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
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
