package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/localmedia"
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
	LocalMedia   *localmedia.Store
	Inspector    AvatarInspector
	Clock        Clock
	Sleeper      Sleeper
	IDGenerator  func() string
}

type Service struct {
	repository     Store
	currentUsers   CurrentUserReader
	idempotency    Idempotency
	localMedia     *localmedia.Store
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
	if dependencies.LocalMedia == nil {
		return nil, errors.New("profile local media store is required")
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
	uploadURLTTL := time.Duration(cfg.MediaStorage.UploadTTLSeconds) * time.Second
	if uploadURLTTL <= 0 {
		uploadURLTTL = 300 * time.Second
	}
	maxUploadBytes := cfg.MediaStorage.MaxUploadBytes
	if maxUploadBytes < 1 {
		maxUploadBytes = 1024 * 1024 * 1024
	}
	if dependencies.Inspector == nil {
		inspector, err := NewFFmpegAvatarInspector(
			dependencies.LocalMedia,
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
		localMedia:     dependencies.LocalMedia,
		inspector:      dependencies.Inspector,
		clock:          dependencies.Clock,
		sleeper:        dependencies.Sleeper,
		newID:          dependencies.IDGenerator,
		uploadURLTTL:   uploadURLTTL,
		maxUploadBytes: maxUploadBytes,
	}, nil
}

func (s *Service) GetCurrentUser(ctx context.Context, actorID string) (identity.CurrentUserDTO, error) {
	return s.currentUsers.CurrentUser(ctx, actorID)
}

func (s *Service) UpdateCurrentUser(
	ctx context.Context,
	actorID string,
	idempotencyKey string,
	input UpdateProfileInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	changes, err := validateProfileChanges(input)
	if err != nil {
		return MutationResult[identity.CurrentUserDTO]{}, err
	}
	return s.idempotency.ExecuteCurrentUser(
		ctx,
		IdempotencyInput{
			ActorID: actorID,
			Scope:   "profile.update",
			Key:     idempotencyKey,
			Payload: input,
		},
		input.ExpectedVersion,
		func() (identity.CurrentUserDTO, error) {
			now := s.clock.Now().UTC()
			if err := s.repository.UpdateProfile(ctx, actorID, input.ExpectedVersion, changes, now); err != nil {
				return identity.CurrentUserDTO{}, err
			}
			return s.currentUsers.CurrentUser(ctx, actorID)
		},
	)
}

func (s *Service) CreateAvatarUpload(
	ctx context.Context,
	actorID string,
	idempotencyKey string,
	input CreateAvatarUploadInput,
) (MutationResult[AvatarUploadDTO], error) {
	if err := validateCreateAvatarInput(input, s.maxUploadBytes); err != nil {
		return MutationResult[AvatarUploadDTO]{}, err
	}
	return s.idempotency.ExecuteAvatarUpload(
		ctx,
		IdempotencyInput{
			ActorID: actorID,
			Scope:   "profile.avatar.upload",
			Key:     idempotencyKey,
			Payload: input,
		},
		0,
		func() (AvatarUploadDTO, error) {
			uploadID := s.newID()
			now := s.clock.Now().UTC()
			expiresAt := now.Add(s.uploadURLTTL)
			relStoragePath := filepath.ToSlash(filepath.Join("temp", fmt.Sprintf("avatar_%s.partial", uploadID)))

			upload, err := s.repository.CreateAvatarUpload(ctx, CreateUploadParams{
				ID:             uploadID,
				ActorID:        actorID,
				StoragePath:    relStoragePath,
				FileName:       input.FileName,
				ContentType:    input.ContentType,
				SizeBytes:      input.SizeBytes,
				ChecksumSHA256: input.ChecksumSHA256,
				ExpiresAt:      expiresAt,
				Now:            now,
			})
			if err != nil {
				return AvatarUploadDTO{}, err
			}

			uploadPath := fmt.Sprintf("/api/v1/users/me/avatar/uploads/%s", upload.ID)
			return AvatarUploadDTO{
				ID:              upload.ID,
				Purpose:         upload.Purpose,
				TargetID:        upload.TargetID,
				Status:          upload.Status,
				Method:          "PUT",
				UploadURL:       uploadPath,
				UploadPath:      uploadPath,
				RequiredHeaders: map[string]string{"Content-Type": input.ContentType},
				ExpiresAt:       formatTime(upload.ExpiresAt),
			}, nil
		},
	)
}

func (s *Service) UploadDirect(
	ctx context.Context,
	actorID string,
	uploadID string,
	body io.Reader,
	contentLength int64,
) error {
	upload, err := s.repository.FindAvatarUpload(ctx, actorID, uploadID)
	if err != nil {
		return err
	}
	if upload.Status != UploadStatusCreated {
		return apperror.Conflict(
			apperror.CodeInvalidStateTransition,
			fmt.Sprintf("Upload cannot receive data in status %s", upload.Status),
			nil,
		)
	}

	tempPath, err := s.localMedia.ResolveAssetPath(upload.StoragePath)
	if err != nil {
		return err
	}

	_, _, err = s.localMedia.WriteUploadStream(
		ctx,
		body,
		upload.ExpectedSize,
		upload.ExpectedChecksumSHA256,
		tempPath,
	)
	return err
}

func (s *Service) CompleteAvatarUpload(
	ctx context.Context,
	actorID string,
	uploadID string,
	idempotencyKey string,
	input CompleteAvatarUploadInput,
) (MutationResult[identity.CurrentUserDTO], error) {
	return s.idempotency.ExecuteCurrentUser(
		ctx,
		IdempotencyInput{
			ActorID: actorID,
			Scope:   "profile.avatar.complete:" + uploadID,
			Key:     idempotencyKey,
			Payload: input,
		},
		0,
		func() (identity.CurrentUserDTO, error) {
			now := s.clock.Now().UTC()
			completionToken := s.newID()
			claim, err := s.repository.ClaimAvatarCompletion(
				ctx,
				actorID,
				uploadID,
				completionToken,
				now,
				completionLease,
			)
			if err != nil {
				return identity.CurrentUserDTO{}, err
			}

			switch claim.Outcome {
			case CompletionFinished:
				return s.currentUsers.CurrentUser(ctx, actorID)
			case CompletionExpired:
				return identity.CurrentUserDTO{}, apperror.Conflict(
					apperror.CodeInvalidStateTransition,
					"Upload has expired",
					nil,
				)
			case CompletionInProgress:
				return s.waitForAvatarCompletion(ctx, actorID, uploadID)
			case CompletionClaimed:
				// Proceed to inspect
			default:
				return identity.CurrentUserDTO{}, errors.New("unknown completion outcome")
			}

			inspected, err := s.inspector.Inspect(ctx, claim.Upload)
			if err != nil {
				_ = s.repository.FailAvatarCompletion(ctx, uploadID, completionToken, false, err.Error(), s.clock.Now().UTC())
				return identity.CurrentUserDTO{}, err
			}

			finalizeErr := s.repository.FinalizeAvatarCompletion(ctx, FinalizeAvatarParams{
				ActorID:         actorID,
				UploadID:        uploadID,
				CompletionToken: completionToken,
				AssetID:         s.newID(),
				Inspected:       inspected,
				Now:             s.clock.Now().UTC(),
			})
			if finalizeErr != nil {
				return identity.CurrentUserDTO{}, finalizeErr
			}

			return s.currentUsers.CurrentUser(ctx, actorID)
		},
	)
}

func (s *Service) waitForAvatarCompletion(
	ctx context.Context,
	actorID string,
	uploadID string,
) (identity.CurrentUserDTO, error) {
	for attempt := 0; attempt < completionWaitAttempts; attempt++ {
		if err := s.sleeper.Sleep(ctx, completionWaitInterval); err != nil {
			return identity.CurrentUserDTO{}, err
		}
		status, err := s.repository.AvatarCompletionStatus(ctx, actorID, uploadID)
		if err != nil {
			return identity.CurrentUserDTO{}, err
		}
		if status == UploadStatusCompleted {
			return s.currentUsers.CurrentUser(ctx, actorID)
		}
		if status != UploadStatusCompleting {
			return identity.CurrentUserDTO{}, apperror.Conflict(
				apperror.CodeInvalidStateTransition,
				"Avatar upload completion failed",
				nil,
			)
		}
	}
	return identity.CurrentUserDTO{}, apperror.Conflict(
		apperror.CodeResourceConflict,
		"Avatar upload completion is taking too long",
		nil,
	)
}

func validateProfileChanges(input UpdateProfileInput) (ProfileChanges, error) {
	if !input.DisplayName.Set && !input.Bio.Set {
		return ProfileChanges{}, apperror.Validation("at least one of displayName or bio must be provided")
	}
	changes := ProfileChanges{}
	if input.DisplayName.Set {
		name := strings.TrimSpace(input.DisplayName.Value)
		if length := javascriptStringLength(name); length < 1 || length > 64 {
			return ProfileChanges{}, apperror.Validation("displayName must contain 1 to 64 characters")
		}
		changes.DisplayNameSet = true
		changes.DisplayName = name
	}
	if input.Bio.Set {
		changes.BioSet = true
		if input.Bio.Value != nil {
			bio := strings.TrimSpace(*input.Bio.Value)
			if length := javascriptStringLength(bio); length > 500 {
				return ProfileChanges{}, apperror.Validation("bio must contain at most 500 characters")
			}
			changes.Bio = &bio
		}
	}
	return changes, nil
}

func validateCreateAvatarInput(input CreateAvatarUploadInput, maxBytes int64) error {
	if length := javascriptStringLength(input.FileName); length < 1 || length > 255 {
		return apperror.Validation("fileName must contain 1 to 255 characters")
	}
	if input.ContentType != "image/jpeg" && input.ContentType != "image/png" && input.ContentType != "image/webp" {
		return apperror.Validation("contentType must be image/jpeg, image/png, or image/webp")
	}
	if input.SizeBytes < 1 || input.SizeBytes > AvatarMaximumBytes || input.SizeBytes > maxBytes {
		return apperror.Validation(fmt.Sprintf("sizeBytes must be between 1 and %d", AvatarMaximumBytes))
	}
	if !checksumPattern.MatchString(input.ChecksumSHA256) {
		return apperror.Validation("checksumSha256 must be a 64-character hex string")
	}
	return nil
}

func javascriptStringLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func formatTime(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
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
