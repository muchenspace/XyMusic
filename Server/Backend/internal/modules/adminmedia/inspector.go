package adminmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"

	"xymusic/server/internal/shared/apperror"
)

const (
	mediaInspectionTimeout = 15 * time.Second
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

type FFmpegUploadInspector struct {
	storage     ObjectStorage
	ffprobePath string
	ffmpegPath  string
	runner      CommandRunner
}

var _ UploadInspector = (*FFmpegUploadInspector)(nil)

func NewFFmpegUploadInspector(
	storage ObjectStorage,
	ffprobePath string,
	ffmpegPath string,
) (*FFmpegUploadInspector, error) {
	return newFFmpegUploadInspector(storage, ffprobePath, ffmpegPath, osCommandRunner{})
}

func newFFmpegUploadInspector(
	storage ObjectStorage,
	ffprobePath string,
	ffmpegPath string,
	runner CommandRunner,
) (*FFmpegUploadInspector, error) {
	if storage == nil {
		return nil, errors.New("admin media inspector object storage is required")
	}
	if strings.TrimSpace(ffprobePath) == "" {
		return nil, errors.New("admin media inspector ffprobe path is required")
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		return nil, errors.New("admin media inspector ffmpeg path is required")
	}
	if runner == nil {
		return nil, errors.New("admin media inspector command runner is required")
	}
	return &FFmpegUploadInspector{
		storage:     storage,
		ffprobePath: ffprobePath,
		ffmpegPath:  ffmpegPath,
		runner:      runner,
	}, nil
}

func (inspector *FFmpegUploadInspector) Inspect(
	ctx context.Context,
	upload MediaUpload,
	observedETag string,
) (InspectedUpload, error) {
	directory, err := os.MkdirTemp("", "xymusic-upload-inspection-")
	if err != nil {
		return InspectedUpload{}, fmt.Errorf("create media inspection directory: %w", err)
	}
	defer os.RemoveAll(directory)
	inputPath := filepath.Join(directory, "input")
	observed, err := inspector.storage.DownloadToFile(
		ctx,
		upload.ObjectKey,
		inputPath,
		upload.ExpectedSize+1,
	)
	if err != nil {
		return InspectedUpload{}, apperror.DependencyUnavailable("Uploaded object could not be read from object storage")
	}
	mismatches := observedObjectMismatches(upload, observed, observedETag)
	if len(mismatches) > 0 {
		return InspectedUpload{}, apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			"Uploaded object mismatched: "+strings.Join(mismatches, ", "),
			map[string]any{"mismatches": mismatches},
		)
	}
	if err := validateFileMIME(inputPath, upload.ExpectedMIMEType); err != nil {
		return InspectedUpload{}, err
	}
	if upload.Purpose == PurposeTrackSource {
		if err := inspector.requireAudioStream(ctx, inputPath); err != nil {
			return InspectedUpload{}, err
		}
		return InspectedUpload{
			ObjectKey:      upload.ObjectKey,
			MIMEType:       upload.ExpectedMIMEType,
			SizeBytes:      observed.SizeBytes,
			ChecksumSHA256: observed.ChecksumSHA256,
			CleanupKeys:    []string{upload.ObjectKey},
		}, nil
	}

	probe, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type,codec_name,width,height",
		"-of", "json",
		inputPath,
	}, mediaInspectionTimeout)
	if err != nil {
		return InspectedUpload{}, apperror.DependencyUnavailable("Image inspection is unavailable")
	}
	if probe.TimedOut {
		return InspectedUpload{}, mediaMismatch("Image processing timed out")
	}
	if probe.ExitCode != 0 {
		return InspectedUpload{}, mediaMismatch(
			"Uploaded image is invalid: " + truncateDiagnostic(probe.Stderr, 300),
		)
	}
	width, height, codec, err := parseImageProbe(probe.Stdout)
	if err != nil || !validImageDimensions(width, height) || !codecMatchesMIME(codec, upload.ExpectedMIMEType) {
		return InspectedUpload{}, mediaMismatch("Uploaded image dimensions or encoding are invalid or unsafe")
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
		return InspectedUpload{}, apperror.DependencyUnavailable("Image normalization is unavailable")
	}
	if normalized.TimedOut {
		return InspectedUpload{}, mediaMismatch("Image processing timed out")
	}
	if normalized.ExitCode != 0 {
		return InspectedUpload{}, mediaMismatch(
			"Uploaded image is invalid: " + truncateDiagnostic(normalized.Stderr, 300),
		)
	}
	normalizedSize, normalizedChecksum, err := fileSizeAndSHA256(outputPath)
	if err != nil {
		return InspectedUpload{}, fmt.Errorf("inspect normalized artwork: %w", err)
	}
	finalWidth, finalHeight, err := inspector.probeNormalizedImage(ctx, outputPath)
	if err != nil {
		return InspectedUpload{}, err
	}
	objectKey := "media/artwork/" + strings.ToLower(string(upload.Purpose)) + "/" +
		upload.TargetID + "/" + upload.ID + ".jpg"
	storedSize, err := inspector.storage.UploadFile(
		ctx,
		objectKey,
		outputPath,
		"image/jpeg",
		normalizedChecksum,
	)
	cleanupKeys := []string{upload.ObjectKey, objectKey}
	if err != nil {
		return InspectedUpload{}, &UploadInspectionFailure{
			Cause:       apperror.DependencyUnavailable("Normalized artwork could not be stored"),
			CleanupKeys: cleanupKeys,
		}
	}
	if storedSize != normalizedSize {
		return InspectedUpload{}, &UploadInspectionFailure{
			Cause:       mediaMismatch("Normalized artwork size changed during storage"),
			CleanupKeys: cleanupKeys,
		}
	}
	return InspectedUpload{
		ObjectKey:      objectKey,
		MIMEType:       "image/jpeg",
		SizeBytes:      normalizedSize,
		ChecksumSHA256: normalizedChecksum,
		Width:          &finalWidth,
		Height:         &finalHeight,
		CleanupKeys:    cleanupKeys,
	}, nil
}

func (inspector *FFmpegUploadInspector) requireAudioStream(ctx context.Context, path string) error {
	probe, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_type",
		"-of", "json",
		path,
	}, mediaInspectionTimeout)
	if err != nil {
		return apperror.DependencyUnavailable("Audio inspection is unavailable")
	}
	if probe.TimedOut {
		return mediaMismatch("Audio inspection timed out")
	}
	if probe.ExitCode != 0 {
		return mediaMismatch("Uploaded audio is invalid: " + truncateDiagnostic(probe.Stderr, 300))
	}
	var payload struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(probe.Stdout), &payload); err != nil {
		return mediaMismatch("Uploaded audio could not be decoded")
	}
	for _, stream := range payload.Streams {
		if stream.CodecType == "audio" {
			return nil
		}
	}
	return mediaMismatch("Uploaded file does not contain an audio stream")
}

func (inspector *FFmpegUploadInspector) probeNormalizedImage(
	ctx context.Context,
	path string,
) (int, int, error) {
	result, err := inspector.runner.Run(ctx, inspector.ffprobePath, []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type,codec_name,width,height",
		"-of", "json",
		path,
	}, mediaInspectionTimeout)
	if err != nil {
		return 0, 0, apperror.DependencyUnavailable("Normalized artwork inspection is unavailable")
	}
	if result.TimedOut || result.ExitCode != 0 {
		return 0, 0, mediaMismatch("Normalized artwork could not be verified")
	}
	width, height, codec, err := parseImageProbe(result.Stdout)
	if err != nil || codec != "mjpeg" || !validImageDimensions(width, height) ||
		width > normalizedImageMaxEdge || height > normalizedImageMaxEdge {
		return 0, 0, mediaMismatch("Normalized artwork could not be verified")
	}
	return width, height, nil
}

type UploadInspectionFailure struct {
	Cause       error
	CleanupKeys []string
}

func (failure *UploadInspectionFailure) Error() string {
	if failure == nil || failure.Cause == nil {
		return "media upload inspection failed"
	}
	return failure.Cause.Error()
}

func (failure *UploadInspectionFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func observedObjectMismatches(upload MediaUpload, observed StoredObject, observedETag string) []string {
	var mismatches []string
	if observed.SizeBytes != upload.ExpectedSize {
		mismatches = append(mismatches, "sizeBytes")
	}
	if normalizeContentType(observed.ContentType) != strings.ToLower(upload.ExpectedMIMEType) {
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

func validateFileMIME(path, expected string) error {
	detected, err := mimetype.DetectFile(path)
	if err != nil {
		return fmt.Errorf("detect uploaded media MIME type: %w", err)
	}
	detectedType := normalizeContentType(detected.String())
	if !compatibleDetectedMIME(strings.ToLower(expected), detectedType) {
		return apperror.Unprocessable(
			apperror.CodeMediaUploadMismatch,
			fmt.Sprintf("Uploaded binary MIME type %s does not match %s", detectedType, expected),
			map[string]any{"mismatches": []string{"binaryMimeType"}},
		)
	}
	return nil
}

func compatibleDetectedMIME(expected, detected string) bool {
	if expected == detected {
		return true
	}
	switch expected {
	case "audio/flac":
		return detected == "audio/x-flac"
	case "audio/mpeg":
		return detected == "audio/mp3"
	case "audio/mp4":
		return detected == "video/mp4" || detected == "application/mp4"
	case "audio/ogg":
		return detected == "application/ogg"
	case "audio/wav", "audio/x-wav":
		return detected == "audio/wav" || detected == "audio/x-wav" || detected == "audio/vnd.wave"
	default:
		return false
	}
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
	if _, err := io.Copy(hasher, file); err != nil {
		return 0, "", err
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

func mediaMismatch(detail string) error {
	return apperror.Unprocessable(apperror.CodeMediaUploadMismatch, detail, nil)
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
