package adminmedia

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/shared/apperror"
)

const (
	avatarMaximumBytes       int64 = 5 * 1024 * 1024
	artworkMaximumBytes      int64 = 20 * 1024 * 1024
	completionLease                = 10 * time.Minute
	completionWaitAttempts         = 50
	completionWaitInterval         = 200 * time.Millisecond
	completionCleanupTimeout       = 10 * time.Second
)

var checksumPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ServiceDependencies struct {
	Repository  Store
	Storage     ObjectStorage
	Inspector   UploadInspector
	Clock       Clock
	Sleeper     Sleeper
	IDGenerator func() string
}

type Service struct {
	repository     Store
	storage        ObjectStorage
	inspector      UploadInspector
	clock          Clock
	sleeper        Sleeper
	newID          func() string
	uploadURLTTL   time.Duration
	maxUploadBytes int64
}

func NewService(cfg config.Config, dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil {
		return nil, errors.New("admin media repository is required")
	}
	if dependencies.Storage == nil {
		return nil, errors.New("admin media object storage is required")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = systemClock{}
	}
	if dependencies.Sleeper == nil {
		dependencies.Sleeper = contextSleeper{}
	}
	if dependencies.IDGenerator == nil {
		dependencies.IDGenerator = uuid.NewString
	}
	uploadURLTTL := time.Duration(cfg.Storage.SignedURLTTLSeconds) * time.Second
	if uploadURLTTL <= 0 {
		return nil, errors.New("admin media upload URL TTL must be positive")
	}
	if cfg.Storage.MaxUploadBytes < 1 {
		return nil, errors.New("admin media maximum upload size must be positive")
	}
	if dependencies.Inspector == nil {
		inspector, err := NewFFmpegUploadInspector(
			dependencies.Storage,
			cfg.Media.FFprobePath,
			cfg.Media.FFmpegPath,
		)
		if err != nil {
			return nil, err
		}
		dependencies.Inspector = inspector
	}
	return &Service{
		repository:     dependencies.Repository,
		storage:        dependencies.Storage,
		inspector:      dependencies.Inspector,
		clock:          dependencies.Clock,
		sleeper:        dependencies.Sleeper,
		newID:          dependencies.IDGenerator,
		uploadURLTTL:   uploadURLTTL,
		maxUploadBytes: cfg.Storage.MaxUploadBytes,
	}, nil
}

func (service *Service) CreateUpload(
	ctx context.Context,
	actorID string,
	traceID string,
	input CreateUploadInput,
) (UploadReservationDTO, error) {
	normalized, err := service.validateUpload(input)
	if err != nil {
		return UploadReservationDTO{}, err
	}
	now := service.clock.Now().UTC()
	uploadID := service.newID()
	upload, err := service.repository.CreateUpload(ctx, CreateUploadParams{
		ID:             uploadID,
		ActorID:        actorID,
		Purpose:        normalized.Purpose,
		TargetID:       normalized.TargetID,
		ObjectKey:      "uploads/" + actorID + "/" + uploadID,
		FileName:       normalized.FileName,
		ContentType:    normalized.ContentType,
		SizeBytes:      normalized.SizeBytes,
		ChecksumSHA256: normalized.ChecksumSHA256,
		ExpiresAt:      now.Add(service.uploadURLTTL),
		Now:            now,
		MaximumBytes:   service.maxUploadBytes,
	})
	if err != nil {
		return UploadReservationDTO{}, err
	}
	uploadURL, err := service.storage.CreateUploadURL(ctx, UploadURLRequest{
		ObjectKey:      upload.ObjectKey,
		ContentType:    upload.ExpectedMIMEType,
		ContentLength:  upload.ExpectedSize,
		ChecksumSHA256: upload.ExpectedChecksumSHA256,
		Expires:        service.uploadURLTTL,
	})
	if err != nil {
		_ = service.repository.MarkUploadFailed(ctx, actorID, upload.ID)
		return UploadReservationDTO{}, apperror.DependencyUnavailable("Object storage upload signing is unavailable")
	}
	if err := service.repository.WriteAudit(ctx, AuditWrite{
		ActorID:    actorID,
		Action:     "media.upload.create",
		TargetType: "media_upload",
		TargetID:   upload.ID,
		TraceID:    traceID,
		Details: map[string]any{
			"purpose":   normalized.Purpose,
			"targetId":  normalized.TargetID,
			"sizeBytes": normalized.SizeBytes,
		},
	}); err != nil {
		_ = service.repository.MarkUploadFailed(ctx, actorID, upload.ID)
		return UploadReservationDTO{}, err
	}
	return UploadReservationDTO{
		ID:        upload.ID,
		Purpose:   upload.Purpose,
		TargetID:  upload.TargetID,
		Status:    upload.Status,
		Method:    http.MethodPut,
		UploadURL: uploadURL,
		RequiredHeaders: map[string]string{
			"content-type":          upload.ExpectedMIMEType,
			"content-length":        fmt.Sprintf("%d", upload.ExpectedSize),
			"x-amz-checksum-sha256": checksumBase64(upload.ExpectedChecksumSHA256),
			"x-amz-meta-sha256":     upload.ExpectedChecksumSHA256,
		},
		ExpiresAt: formatTimestamp(upload.ExpiresAt),
	}, nil
}

func (service *Service) UploadContent(
	ctx context.Context,
	actorID string,
	uploadID string,
	contentType string,
	contentLength int64,
	body io.Reader,
) error {
	upload, err := service.repository.FindUploadForContent(ctx, actorID, uploadID)
	if err != nil {
		return err
	}
	if upload.Status != UploadStatusCreated {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			fmt.Sprintf("Upload content cannot be sent from %s", upload.Status),
			nil,
		)
	}
	if !upload.ExpiresAt.After(service.clock.Now()) {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Media upload has expired",
			nil,
		)
	}
	if normalizeContentType(contentType) != strings.ToLower(upload.ExpectedMIMEType) {
		return apperror.Validation("Upload content type does not match the reservation")
	}
	if contentLength >= 0 && contentLength != upload.ExpectedSize {
		return apperror.Validation("Upload content length does not match the reservation")
	}
	if body == nil {
		return apperror.Validation("Upload content is required")
	}
	directory, err := os.MkdirTemp("", "xymusic-admin-upload-")
	if err != nil {
		return fmt.Errorf("create media upload staging directory: %w", err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, upload.ID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create media upload staging file: %w", err)
	}
	hasher := sha256.New()
	received, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(body, upload.ExpectedSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(copyErr, &maximumBytesError) {
			return apperror.PayloadTooLarge("Upload exceeds the reserved size")
		}
		return fmt.Errorf("read media upload content: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close media upload staging file: %w", closeErr)
	}
	if received > upload.ExpectedSize {
		return apperror.PayloadTooLarge("Upload exceeds the reserved size")
	}
	if received != upload.ExpectedSize {
		return apperror.Validation("Upload content size does not match the reservation")
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != upload.ExpectedChecksumSHA256 {
		return apperror.Validation("Upload checksum does not match the reservation")
	}
	if err := validateFileMIME(path, upload.ExpectedMIMEType); err != nil {
		return err
	}
	storedSize, err := service.storage.UploadFile(
		ctx,
		upload.ObjectKey,
		path,
		upload.ExpectedMIMEType,
		checksum,
	)
	if err != nil {
		return apperror.DependencyUnavailable("Uploaded content could not be stored")
	}
	if storedSize != upload.ExpectedSize {
		return apperror.DependencyUnavailable("Uploaded content size changed during storage")
	}
	return nil
}

func (service *Service) AbandonUpload(ctx context.Context, actorID, uploadID string) error {
	return service.repository.AbandonUpload(ctx, actorID, uploadID, service.clock.Now().UTC())
}

func (service *Service) CompleteUpload(
	ctx context.Context,
	actorID string,
	traceID string,
	uploadID string,
	input CompleteUploadInput,
) (UploadCompletionDTO, error) {
	observedETag, err := validateCompleteUpload(input)
	if err != nil {
		return UploadCompletionDTO{}, err
	}
	claim, err := service.repository.ClaimCompletion(
		ctx,
		actorID,
		uploadID,
		service.newID(),
		service.clock.Now().UTC(),
		completionLease,
	)
	if err != nil {
		return UploadCompletionDTO{}, err
	}
	switch claim.Outcome {
	case CompletionFinished:
		return completionDTO(completedUpload(claim.Upload)), nil
	case CompletionExpired:
		return UploadCompletionDTO{}, apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			"Media upload has expired",
			nil,
		)
	case CompletionInProgress:
		completed, waitErr := service.awaitCompletion(ctx, actorID, uploadID)
		if waitErr != nil {
			return UploadCompletionDTO{}, waitErr
		}
		if completed != nil {
			return completionDTO(*completed), nil
		}
		return UploadCompletionDTO{}, apperror.Conflict(
			apperror.CodeResourceConflict,
			"Media upload completion is already in progress",
			nil,
		)
	case CompletionClaimed:
	default:
		return UploadCompletionDTO{}, errors.New("admin media repository returned an invalid completion outcome")
	}

	inspected, inspectErr := service.inspector.Inspect(ctx, claim.Upload, observedETag)
	if inspectErr != nil {
		cleanupKeys := inspected.CleanupKeys
		var inspectionFailure *UploadInspectionFailure
		if errors.As(inspectErr, &inspectionFailure) {
			cleanupKeys = inspectionFailure.CleanupKeys
		}
		if cleanupErr := service.failCompletion(
			ctx,
			claim,
			cleanupKeys,
			inspectErr,
			input.CompletionFence != nil,
		); cleanupErr != nil {
			return UploadCompletionDTO{}, errors.Join(inspectErr, cleanupErr)
		}
		return UploadCompletionDTO{}, inspectErr
	}
	completed, finalizeErr := service.repository.FinalizeCompletion(ctx, FinalizeCompletionParams{
		ActorID:         actorID,
		TraceID:         traceID,
		UploadID:        uploadID,
		CompletionToken: claim.Token,
		AssetID:         service.newID(),
		JobID:           service.newID(),
		Inspected:       inspected,
		CompletionFence: input.CompletionFence,
		Now:             service.clock.Now().UTC(),
	})
	if finalizeErr != nil {
		recoveryContext, cancelRecovery := context.WithTimeout(
			context.WithoutCancel(ctx),
			completionCleanupTimeout,
		)
		current, lookupErr := service.repository.CompletionStatus(recoveryContext, actorID, uploadID)
		cancelRecovery()
		if lookupErr == nil && current.Status == UploadStatusCompleted && current.AssetID != nil {
			return completionDTO(completedUpload(current)), nil
		}
		cleanupKeys := append([]string{}, inspected.CleanupKeys...)
		cleanupKeys = append(cleanupKeys, inspected.ObjectKey, claim.Upload.ObjectKey)
		if cleanupErr := service.failCompletion(
			ctx,
			claim,
			uniqueStrings(cleanupKeys),
			finalizeErr,
			input.CompletionFence != nil,
		); cleanupErr != nil {
			return UploadCompletionDTO{}, errors.Join(finalizeErr, cleanupErr)
		}
		return UploadCompletionDTO{}, finalizeErr
	}
	return completionDTO(completed), nil
}

func (service *Service) GetJob(ctx context.Context, jobID string) (MediaJobDTO, error) {
	job, err := service.repository.FindJob(ctx, jobID)
	if err != nil {
		return MediaJobDTO{}, err
	}
	return presentJob(job), nil
}

func (service *Service) RetryJob(
	ctx context.Context,
	actorID string,
	traceID string,
	jobID string,
	input RetryJobInput,
) (MediaJobDTO, error) {
	if input.ExpectedVersion < 1 {
		return MediaJobDTO{}, apperror.Validation("expectedVersion must be a positive integer")
	}
	var reason *string
	if input.Reason.Set {
		if length := javascriptStringLength(input.Reason.Value); length < 1 || length > 500 {
			return MediaJobDTO{}, apperror.Validation("reason is invalid")
		}
		reason = &input.Reason.Value
	}
	job, err := service.repository.RetryJob(ctx, RetryJobParams{
		ActorID:         actorID,
		TraceID:         traceID,
		JobID:           jobID,
		ExpectedVersion: input.ExpectedVersion,
		Reason:          reason,
		Now:             service.clock.Now().UTC(),
	})
	if err != nil {
		return MediaJobDTO{}, err
	}
	return presentJob(job), nil
}

func (service *Service) awaitCompletion(
	ctx context.Context,
	actorID string,
	uploadID string,
) (*CompletedUpload, error) {
	for range completionWaitAttempts {
		if err := service.sleeper.Sleep(ctx, completionWaitInterval); err != nil {
			return nil, err
		}
		upload, err := service.repository.CompletionStatus(ctx, actorID, uploadID)
		if err != nil {
			return nil, err
		}
		if upload.Status == UploadStatusCompleted && upload.AssetID != nil {
			completed := completedUpload(upload)
			return &completed, nil
		}
		if upload.Status != UploadStatusCompleting {
			return nil, nil
		}
	}
	return nil, nil
}

func (service *Service) failCompletion(
	ctx context.Context,
	claim CompletionClaim,
	cleanupKeys []string,
	cause error,
	forceFailure bool,
) error {
	if len(cleanupKeys) == 0 {
		cleanupKeys = []string{claim.Upload.ObjectKey}
	}
	retryable := !forceFailure && apperror.IsCode(cause, apperror.CodeDependencyUnavailable)
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), completionCleanupTimeout)
	defer cancel()
	return service.repository.FailCompletion(
		cleanupContext,
		claim.Upload.ID,
		claim.Token,
		retryable,
		cleanupKeys,
		failureReason(cause),
		service.clock.Now().UTC(),
	)
}

func (service *Service) validateUpload(input CreateUploadInput) (CreateUploadInput, error) {
	if !validPurpose(input.Purpose) {
		return CreateUploadInput{}, apperror.Validation("purpose is invalid")
	}
	if strings.TrimSpace(input.FileName) == "" || javascriptStringLength(input.FileName) > 255 {
		return CreateUploadInput{}, apperror.Validation("fileName is invalid")
	}
	if input.SizeBytes < 1 {
		return CreateUploadInput{}, apperror.Validation("sizeBytes is invalid")
	}
	if input.SizeBytes > service.maxUploadBytes {
		return CreateUploadInput{}, apperror.PayloadTooLarge(
			fmt.Sprintf("Upload exceeds %d bytes", service.maxUploadBytes),
		)
	}
	if input.Purpose == PurposeUserAvatar && input.SizeBytes > avatarMaximumBytes {
		return CreateUploadInput{}, apperror.PayloadTooLarge("Avatar upload exceeds 5 MiB")
	}
	if input.Purpose != PurposeTrackSource && input.Purpose != PurposeUserAvatar &&
		input.SizeBytes > artworkMaximumBytes {
		return CreateUploadInput{}, apperror.PayloadTooLarge("Artwork upload exceeds 20 MiB")
	}
	if !checksumPattern.MatchString(input.ChecksumSHA256) {
		return CreateUploadInput{}, apperror.Validation("checksumSha256 is invalid")
	}
	contentType := strings.ToLower(input.ContentType)
	audio := input.Purpose == PurposeTrackSource
	if audio {
		if _, allowed := allowedAudioMIMETypes[contentType]; !allowed {
			return CreateUploadInput{}, apperror.Validation("contentType is not allowed")
		}
	} else if _, allowed := allowedImageMIMETypes[contentType]; !allowed {
		return CreateUploadInput{}, apperror.Validation("contentType is not allowed")
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(input.FileName)), ".")
	if audio {
		if _, allowed := allowedAudioExtensions[extension]; !allowed {
			return CreateUploadInput{}, apperror.Validation("fileName has an unsupported extension")
		}
	} else if _, allowed := allowedImageExtensions[extension]; !allowed {
		return CreateUploadInput{}, apperror.Validation("fileName has an unsupported extension")
	}
	input.ContentType = contentType
	return input, nil
}

func validateCompleteUpload(input CompleteUploadInput) (string, error) {
	if !input.ObservedETag.Set {
		return "", nil
	}
	if length := javascriptStringLength(input.ObservedETag.Value); length < 1 || length > 200 {
		return "", apperror.Validation("observedEtag is invalid")
	}
	return normalizeETag(input.ObservedETag.Value), nil
}

func completionDTO(completed CompletedUpload) UploadCompletionDTO {
	status := UploadStatusCompleted
	if completed.JobID != nil {
		status = JobStatusProcessing
	}
	return UploadCompletionDTO{
		UploadID: completed.UploadID,
		Status:   status,
		AssetID:  completed.AssetID,
		JobID:    completed.JobID,
	}
}

func presentJob(job MediaJob) MediaJobDTO {
	var nextAttemptAt *string
	if job.Status == JobStatusPending {
		formatted := formatTimestamp(job.NextAttemptAt)
		nextAttemptAt = &formatted
	}
	return MediaJobDTO{
		ID:               job.ID,
		Type:             job.Type,
		Status:           job.Status,
		Attempts:         job.Attempts,
		MaxAttempts:      job.MaxAttempts,
		CancelRequested:  job.CancelRequested,
		LastErrorCode:    job.LastErrorCode,
		LastErrorMessage: userFacingOperationalError(job.LastError, job.LastErrorCode),
		NextAttemptAt:    nextAttemptAt,
		Version:          job.Version,
		CreatedAt:        formatTimestamp(job.CreatedAt),
		UpdatedAt:        formatTimestamp(job.UpdatedAt),
	}
}

func userFacingOperationalError(message, code *string) *string {
	if message == nil || strings.TrimSpace(*message) == "" {
		return nil
	}
	normalized := strings.TrimSpace(*message)
	known := map[string]string{
		"Cancelled by an administrator":                              "\u4efb\u52a1\u5df2\u7531\u7ba1\u7406\u5458\u53d6\u6d88\u3002",
		"Music source was disabled":                                  "\u97f3\u4e50\u6e90\u5df2\u505c\u7528\uff0c\u4efb\u52a1\u5df2\u53d6\u6d88\u3002",
		"Media job lease expired after all retry attempts were used": "\u5a92\u4f53\u5904\u7406\u591a\u6b21\u91cd\u8bd5\u540e\u4ecd\u672a\u5b8c\u6210\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u72b6\u6001\u3002",
		"A newer upload superseded this media job":                   "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684\u4e0a\u4f20\u66ff\u4ee3\u3002",
		"A newer source generation superseded this media job":        "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684\u97f3\u4e50\u6e90\u7248\u672c\u66ff\u4ee3\u3002",
		"A newer CUE definition superseded this media job":           "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684 CUE \u5b9a\u4e49\u66ff\u4ee3\u3002",
	}
	if value, exists := known[normalized]; exists {
		return &value
	}
	if code != nil {
		byCode := map[string]string{
			"MEDIA_UPLOAD_MISMATCH":    "\u5a92\u4f53\u6587\u4ef6\u6821\u9a8c\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u6587\u4ef6\u683c\u5f0f\u540e\u91cd\u8bd5\u3002",
			"DEPENDENCY_UNAVAILABLE":   "\u76f8\u5173\u5904\u7406\u670d\u52a1\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u914d\u7f6e\u540e\u91cd\u8bd5\u3002",
			"SOURCE_SIZE_MISMATCH":     "\u4ece\u5bf9\u8c61\u5b58\u50a8\u8bfb\u53d6\u7684\u6e90\u97f3\u9891\u4e0d\u5b8c\u6574\uff0c\u5c1a\u672a\u5f00\u59cb\u8f6c\u7801\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"SOURCE_CHECKSUM_MISMATCH": "\u4ece\u5bf9\u8c61\u5b58\u50a8\u8bfb\u53d6\u7684\u6e90\u97f3\u9891\u6821\u9a8c\u5931\u8d25\uff0c\u5c1a\u672a\u5f00\u59cb\u8f6c\u7801\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WORKER_LEASE_EXPIRED":     "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
		}
		if value, exists := byCode[*code]; exists {
			return &value
		}
	}
	if containsHan(normalized) && !sensitiveOperationalDetail(normalized) {
		value := strings.Join(strings.Fields(normalized), " ")
		value = truncateRunes(value, 1_000)
		return &value
	}
	value := "\u4efb\u52a1\u6267\u884c\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\uff1b\u5982\u95ee\u9898\u6301\u7eed\u51fa\u73b0\uff0c\u8bf7\u67e5\u770b\u670d\u52a1\u7aef\u65e5\u5fd7\u3002"
	return &value
}

func containsHan(value string) bool {
	for _, character := range value {
		if character >= '\u3400' && character <= '\u9fff' {
			return true
		}
	}
	return false
}

func sensitiveOperationalDetail(value string) bool {
	for _, pattern := range operationalSensitivePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	return string([]rune(value)[:maximum])
}

func validPurpose(value UploadPurpose) bool {
	return value == PurposeTrackSource || value == PurposeArtistArtwork ||
		value == PurposeAlbumArtwork || value == PurposeUserAvatar
}

func normalizeContentType(value string) string {
	if separator := strings.IndexByte(value, ';'); separator >= 0 {
		value = value[:separator]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func checksumBase64(value string) string {
	decoded, _ := hex.DecodeString(value)
	return base64.StdEncoding.EncodeToString(decoded)
}

func normalizeETag(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), `"`, "")
}

func failureReason(err error) string {
	if applicationError, ok := apperror.As(err); ok {
		return "UPLOAD_FAILED_" + string(applicationError.Code)
	}
	return "UPLOAD_FAILED_INTERNAL_ERROR"
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func javascriptStringLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type contextSleeper struct{}

func (contextSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var (
	allowedAudioMIMETypes = map[string]struct{}{
		"audio/flac": {}, "audio/mpeg": {}, "audio/mp4": {}, "audio/ogg": {},
		"audio/wav": {}, "audio/x-wav": {},
	}
	allowedAudioExtensions = map[string]struct{}{
		"flac": {}, "mp3": {}, "m4a": {}, "mp4": {}, "ogg": {}, "opus": {}, "wav": {},
	}
	allowedImageMIMETypes = map[string]struct{}{
		"image/jpeg": {}, "image/png": {}, "image/webp": {},
	}
	allowedImageExtensions = map[string]struct{}{
		"jpg": {}, "jpeg": {}, "png": {}, "webp": {},
	}
	operationalSensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`[A-Za-z]:[\\/]`),
		regexp.MustCompile(`(?i)(?:postgres|postgresql)://`),
		regexp.MustCompile(`(?i)\bBearer\s+`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.`),
		regexp.MustCompile(`\b(?:EACCES|EEXIST|EINVAL|EIO|ENOENT|ENOTDIR|EPERM|ETIMEDOUT|ECONNREFUSED|ECONNRESET|SQLSTATE)\b`),
	}
)
