package profile

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/apperror"
)

const (
	maximumActiveAvatarUploads = 3
	avatarUploadByteBudget     = 15 * 1024 * 1024
	completionLease            = 10 * time.Minute
	completionWaitAttempts     = 50
	completionWaitInterval     = 200 * time.Millisecond
)

var checksumPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ServiceDependencies struct {
	Repository   Store
	CurrentUsers CurrentUserReader
	Idempotency  Idempotency
	Storage      AvatarObjectStorage
	Inspector    AvatarInspector
	Clock        Clock
	Sleeper      Sleeper
	IDGenerator  func() string
}

type Service struct {
	repository     Store
	currentUsers   CurrentUserReader
	idempotency    Idempotency
	storage        AvatarObjectStorage
	inspector      AvatarInspector
	clock          Clock
	sleeper        Sleeper
	newID          func() string
	uploadURLTTL   time.Duration
	maxUploadBytes int64
}

func NewService(cfg config.Config, dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil {
		return nil, errors.New("profile repository is required")
	}
	if dependencies.CurrentUsers == nil {
		return nil, errors.New("profile current-user reader is required")
	}
	if dependencies.Idempotency == nil {
		return nil, errors.New("profile idempotency service is required")
	}
	if dependencies.Storage == nil {
		return nil, errors.New("profile object storage is required")
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
		return nil, errors.New("profile upload URL TTL must be positive")
	}
	if cfg.Storage.MaxUploadBytes < 1 {
		return nil, errors.New("profile maximum upload size must be positive")
	}
	if dependencies.Inspector == nil {
		inspector, err := NewFFmpegAvatarInspector(
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
		currentUsers:   dependencies.CurrentUsers,
		idempotency:    dependencies.Idempotency,
		storage:        dependencies.Storage,
		inspector:      dependencies.Inspector,
		clock:          dependencies.Clock,
		sleeper:        dependencies.Sleeper,
		newID:          dependencies.IDGenerator,
		uploadURLTTL:   uploadURLTTL,
		maxUploadBytes: cfg.Storage.MaxUploadBytes,
	}, nil
}

func (service *Service) GetCurrentUser(ctx context.Context, userID string) (identity.CurrentUserDTO, error) {
	return service.currentUsers.CurrentUser(ctx, userID)
}

func (service *Service) UpdateCurrentUser(
	ctx context.Context,
	userID string,
	idempotencyKey string,
	input UpdateProfileInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	return service.idempotency.ExecuteCurrentUser(ctx, IdempotencyInput{
		ActorID: userID,
		Scope:   "profile.update",
		Key:     idempotencyKey,
		Payload: updateProfilePayload(input),
	}, http.StatusOK, func() (identity.CurrentUserDTO, error) {
		changes, err := validateProfileUpdate(input)
		if err != nil {
			return identity.CurrentUserDTO{}, err
		}
		if err := service.repository.UpdateProfile(
			ctx,
			userID,
			input.ExpectedVersion,
			changes,
			service.clock.Now().UTC(),
		); err != nil {
			return identity.CurrentUserDTO{}, err
		}
		return service.currentUsers.CurrentUser(ctx, userID)
	})
}

func (service *Service) CreateAvatarUpload(
	ctx context.Context,
	userID string,
	traceID string,
	idempotencyKey string,
	input CreateAvatarUploadInput,
) (MutationResult[AvatarUploadDTO], error) {
	return service.idempotency.ExecuteAvatarUpload(ctx, IdempotencyInput{
		ActorID: userID,
		Scope:   "profile.avatar.upload.create",
		Key:     idempotencyKey,
		Payload: input,
	}, http.StatusCreated, func() (AvatarUploadDTO, error) {
		normalized, err := service.validateAvatarUpload(input)
		if err != nil {
			return AvatarUploadDTO{}, err
		}
		now := service.clock.Now().UTC()
		uploadID := service.newID()
		objectKey := "uploads/" + userID + "/" + uploadID
		upload, err := service.repository.CreateAvatarUpload(ctx, CreateUploadParams{
			ID:             uploadID,
			ActorID:        userID,
			TraceID:        traceID,
			ObjectKey:      objectKey,
			FileName:       normalized.FileName,
			ContentType:    normalized.ContentType,
			SizeBytes:      normalized.SizeBytes,
			ChecksumSHA256: normalized.ChecksumSHA256,
			ExpiresAt:      now.Add(service.uploadURLTTL),
			Now:            now,
		})
		if err != nil {
			return AvatarUploadDTO{}, err
		}
		uploadURL, err := service.storage.CreateUploadURL(ctx, UploadURLRequest{
			ObjectKey:      upload.ObjectKey,
			ContentType:    upload.ExpectedMIMEType,
			ContentLength:  upload.ExpectedSize,
			ChecksumSHA256: upload.ExpectedChecksumSHA256,
			Expires:        service.uploadURLTTL,
		})
		if err != nil {
			_ = service.repository.MarkAvatarUploadFailed(ctx, userID, upload.ID)
			return AvatarUploadDTO{}, apperror.DependencyUnavailable("Object storage upload signing is unavailable")
		}
		return AvatarUploadDTO{
			ID:        upload.ID,
			Purpose:   AvatarUploadPurpose,
			TargetID:  userID,
			Status:    UploadStatusCreated,
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
	})
}

func (service *Service) CompleteAvatarUpload(
	ctx context.Context,
	userID string,
	traceID string,
	uploadID string,
	idempotencyKey string,
	input CompleteAvatarUploadInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	return service.idempotency.ExecuteCurrentUser(ctx, IdempotencyInput{
		ActorID: userID,
		Scope:   "profile.avatar.upload.complete:" + uploadID,
		Key:     idempotencyKey,
		Payload: completeUploadPayload(input),
	}, http.StatusOK, func() (identity.CurrentUserDTO, error) {
		observedETag, err := validateCompleteUpload(input)
		if err != nil {
			return identity.CurrentUserDTO{}, err
		}
		claim, err := service.repository.ClaimAvatarCompletion(
			ctx,
			userID,
			uploadID,
			service.newID(),
			service.clock.Now().UTC(),
			completionLease,
		)
		if err != nil {
			return identity.CurrentUserDTO{}, err
		}
		switch claim.Outcome {
		case CompletionFinished:
			return service.currentUsers.CurrentUser(ctx, userID)
		case CompletionExpired:
			return identity.CurrentUserDTO{}, apperror.Conflict(
				apperror.CodeInvalidStateTransition,
				"Media upload has expired",
				nil,
			)
		case CompletionInProgress:
			completed, waitErr := service.awaitAvatarCompletion(ctx, userID, uploadID)
			if waitErr != nil {
				return identity.CurrentUserDTO{}, waitErr
			}
			if completed {
				return service.currentUsers.CurrentUser(ctx, userID)
			}
			return identity.CurrentUserDTO{}, apperror.Conflict(
				apperror.CodeResourceConflict,
				"Media upload completion is already in progress",
				nil,
			)
		case CompletionClaimed:
		default:
			return identity.CurrentUserDTO{}, errors.New("profile repository returned an invalid completion outcome")
		}

		inspected, inspectErr := service.inspector.Inspect(ctx, claim.Upload, observedETag)
		if inspectErr != nil {
			cleanupKeys := inspected.CleanupKeys
			var inspectionFailure *AvatarInspectionFailure
			if errors.As(inspectErr, &inspectionFailure) {
				cleanupKeys = inspectionFailure.CleanupKeys
			}
			service.failCompletion(ctx, claim, cleanupKeys, inspectErr)
			return identity.CurrentUserDTO{}, inspectErr
		}
		finalizeErr := service.repository.FinalizeAvatarCompletion(ctx, FinalizeAvatarParams{
			ActorID:         userID,
			TraceID:         traceID,
			UploadID:        uploadID,
			CompletionToken: claim.Token,
			AssetID:         service.newID(),
			Inspected:       inspected,
			Now:             service.clock.Now().UTC(),
		})
		if finalizeErr != nil {
			service.failCompletion(ctx, claim, inspected.CleanupKeys, finalizeErr)
			return identity.CurrentUserDTO{}, finalizeErr
		}
		return service.currentUsers.CurrentUser(ctx, userID)
	})
}

func (service *Service) awaitAvatarCompletion(ctx context.Context, actorID, uploadID string) (bool, error) {
	for range completionWaitAttempts {
		if err := service.sleeper.Sleep(ctx, completionWaitInterval); err != nil {
			return false, err
		}
		status, err := service.repository.AvatarCompletionStatus(ctx, actorID, uploadID)
		if err != nil {
			return false, err
		}
		if status == UploadStatusCompleted {
			return true, nil
		}
		if status != UploadStatusCompleting {
			return false, nil
		}
	}
	return false, nil
}

func (service *Service) failCompletion(
	ctx context.Context,
	claim CompletionClaim,
	cleanupKeys []string,
	cause error,
) {
	if len(cleanupKeys) == 0 {
		cleanupKeys = []string{claim.Upload.ObjectKey}
	}
	retryable := apperror.IsCode(cause, apperror.CodeDependencyUnavailable)
	_ = service.repository.FailAvatarCompletion(
		ctx,
		claim.Upload.ID,
		claim.Token,
		retryable,
		cleanupKeys,
		failureReason(cause),
		service.clock.Now().UTC(),
	)
}

func validateProfileUpdate(input UpdateProfileInput) (ProfileChanges, error) {
	if input.ExpectedVersion < 1 {
		return ProfileChanges{}, apperror.Validation("expectedVersion must be a positive integer")
	}
	if !input.DisplayName.Set && !input.Bio.Set {
		return ProfileChanges{}, apperror.Validation("At least one profile field must be supplied")
	}
	changes := ProfileChanges{}
	if input.DisplayName.Set {
		rawLength := javascriptStringLength(input.DisplayName.Value)
		if rawLength < 1 || rawLength > 64 {
			return ProfileChanges{}, apperror.Validation("displayName must contain 1 to 64 characters")
		}
		displayName := strings.TrimSpace(input.DisplayName.Value)
		length := javascriptStringLength(displayName)
		if length < 1 || length > 64 {
			return ProfileChanges{}, apperror.Validation("displayName must contain 1 to 64 characters")
		}
		changes.DisplayNameSet = true
		changes.DisplayName = displayName
	}
	if input.Bio.Set {
		changes.BioSet = true
		if input.Bio.Value != nil {
			if javascriptStringLength(*input.Bio.Value) > 500 {
				return ProfileChanges{}, apperror.Validation("bio cannot exceed 500 characters")
			}
			bio := strings.TrimSpace(*input.Bio.Value)
			if javascriptStringLength(bio) > 500 {
				return ProfileChanges{}, apperror.Validation("bio cannot exceed 500 characters")
			}
			changes.Bio = &bio
		}
	}
	return changes, nil
}

func (service *Service) validateAvatarUpload(input CreateAvatarUploadInput) (CreateAvatarUploadInput, error) {
	if strings.TrimSpace(input.FileName) == "" || javascriptStringLength(input.FileName) > 255 {
		return CreateAvatarUploadInput{}, apperror.Validation("fileName is invalid")
	}
	if input.SizeBytes < 1 {
		return CreateAvatarUploadInput{}, apperror.Validation("sizeBytes is invalid")
	}
	if input.SizeBytes > service.maxUploadBytes {
		return CreateAvatarUploadInput{}, apperror.PayloadTooLarge(
			fmt.Sprintf("Upload exceeds %d bytes", service.maxUploadBytes),
		)
	}
	if input.SizeBytes > AvatarMaximumBytes {
		return CreateAvatarUploadInput{}, apperror.PayloadTooLarge("Avatar upload exceeds 5 MiB")
	}
	if !checksumPattern.MatchString(input.ChecksumSHA256) {
		return CreateAvatarUploadInput{}, apperror.Validation("checksumSha256 is invalid")
	}
	contentType := input.ContentType
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return CreateAvatarUploadInput{}, apperror.Validation("contentType is not allowed")
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(input.FileName)), ".")
	if extension != "jpg" && extension != "jpeg" && extension != "png" && extension != "webp" {
		return CreateAvatarUploadInput{}, apperror.Validation("fileName has an unsupported extension")
	}
	input.ContentType = contentType
	return input, nil
}

func validateCompleteUpload(input CompleteAvatarUploadInput) (string, error) {
	if !input.ObservedETag.Set {
		return "", nil
	}
	length := javascriptStringLength(input.ObservedETag.Value)
	if length < 1 || length > 200 {
		return "", apperror.Validation("observedEtag is invalid")
	}
	return normalizeETag(input.ObservedETag.Value), nil
}

func updateProfilePayload(input UpdateProfileInput) map[string]any {
	payload := map[string]any{"expectedVersion": input.ExpectedVersion}
	if input.DisplayName.Set {
		payload["displayName"] = input.DisplayName.Value
	}
	if input.Bio.Set {
		payload["bio"] = input.Bio.Value
	}
	return payload
}

func completeUploadPayload(input CompleteAvatarUploadInput) map[string]any {
	payload := make(map[string]any)
	if input.ObservedETag.Set {
		payload["observedEtag"] = input.ObservedETag.Value
	}
	return payload
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
