package adminmedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

const (
	AudioSourceMaximumBytes int64 = 1024 * 1024 * 1024
	ArtworkMaximumBytes     int64 = 10 * 1024 * 1024
	completionLease               = 10 * time.Minute
	completionWaitAttempts        = 50
	completionWaitInterval        = 200 * time.Millisecond
)

var checksumPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ServiceDependencies struct {
	Repository  Store
	Idempotency Idempotency
	LocalMedia  *localmedia.Store
	Inspector   MediaInspector
	Clock       Clock
	Sleeper     Sleeper
	IDGenerator func() string
}

type Service struct {
	repository     Store
	idempotency    Idempotency
	localMedia     *localmedia.Store
	inspector      MediaInspector
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
	if dependencies.Idempotency == nil {
		return nil, errors.New("admin media idempotency is required")
	}
	if dependencies.LocalMedia == nil {
		return nil, errors.New("admin media local media store is required")
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
		inspector, err := NewFFmpegMediaInspector(
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

func (s *Service) CreateUpload(
	ctx context.Context,
	actorID string,
	idempotencyKey string,
	input CreateUploadInput,
) (UploadReservationDTO, bool, error) {
	if err := validateCreateUploadInput(input, s.maxUploadBytes); err != nil {
		return UploadReservationDTO{}, false, err
	}
	return s.idempotency.ExecuteReservation(
		ctx,
		IdempotencyInput{
			ActorID: actorID,
			Scope:   "admin.media.upload",
			Key:     idempotencyKey,
			Payload: input,
		},
		func() (UploadReservationDTO, error) {
			uploadID := s.newID()
			now := s.clock.Now().UTC()
			expiresAt := now.Add(s.uploadURLTTL)
			relStoragePath := filepath.ToSlash(filepath.Join("temp", fmt.Sprintf("upload_%s.partial", uploadID)))

			var trackID *string
			if input.Purpose == PurposeTrackSource {
				trackID = &input.TargetID
			}

			reservation := UploadReservation{
				ID:                     uploadID,
				Purpose:                input.Purpose,
				TargetID:               input.TargetID,
				TrackID:                trackID,
				UploaderID:             actorID,
				StoragePath:            relStoragePath,
				ExpectedSize:           input.SizeBytes,
				ExpectedChecksumSHA256: input.ChecksumSHA256,
				ExpectedMIMEType:       input.ContentType,
				OriginalFileName:       input.FileName,
				Status:                 UploadStatusCreated,
				ExpiresAt:              expiresAt,
				CreatedAt:              now,
			}

			if err := s.repository.CreateUpload(ctx, reservation); err != nil {
				return UploadReservationDTO{}, err
			}

			uploadPath := fmt.Sprintf("/api/v1/admin/media/uploads/%s/content", uploadID)
			return UploadReservationDTO{
				ID:              uploadID,
				Purpose:         input.Purpose,
				TargetID:        input.TargetID,
				Status:          UploadStatusCreated,
				Method:          "PUT",
				UploadURL:       uploadPath,
				UploadPath:      uploadPath,
				RequiredHeaders: map[string]string{"Content-Type": input.ContentType},
				ExpiresAt:       formatTime(expiresAt),
			}, nil
		},
	)
}

func (s *Service) UploadDirect(
	ctx context.Context,
	uploadID string,
	body io.Reader,
	contentLength int64,
) error {
	upload, err := s.repository.FindUpload(ctx, uploadID)
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

func (s *Service) CompleteUpload(
	ctx context.Context,
	actorID string,
	uploadID string,
	idempotencyKey string,
	input CompleteUploadInput,
) (UploadCompletionDTO, bool, error) {
	return s.idempotency.ExecuteCompletion(
		ctx,
		IdempotencyInput{
			ActorID: actorID,
			Scope:   "admin.media.complete:" + uploadID,
			Key:     idempotencyKey,
			Payload: input,
		},
		func() (UploadCompletionDTO, error) {
			now := s.clock.Now().UTC()
			completionToken := s.newID()
			claim, err := s.repository.ClaimUploadCompletion(
				ctx,
				uploadID,
				completionToken,
				now,
				completionLease,
			)
			if err != nil {
				return UploadCompletionDTO{}, err
			}

			switch claim.Outcome {
			case CompletionFinished:
				return UploadCompletionDTO{
					UploadID: uploadID,
					Status:   UploadStatusCompleted,
					AssetID:  *claim.Upload.AssetID,
				}, nil
			case CompletionExpired:
				return UploadCompletionDTO{}, apperror.Conflict(
					apperror.CodeInvalidStateTransition,
					"Upload has expired",
					nil,
				)
			case CompletionInProgress:
				return s.waitForUploadCompletion(ctx, uploadID)
			case CompletionClaimed:
				// Proceed to inspect and finalize
			default:
				return UploadCompletionDTO{}, errors.New("unknown completion outcome")
			}

			inspected, err := s.inspector.Inspect(ctx, claim.Upload)
			if err != nil {
				return UploadCompletionDTO{}, s.failCompletion(
					ctx, uploadID, completionToken, false, err.Error(), err,
				)
			}

			assetID := s.newID()
			finalizeErr := s.repository.FinalizeUpload(ctx, FinalizeUploadParams{
				UploadID:        uploadID,
				CompletionToken: completionToken,
				AssetID:         assetID,
				Inspected:       inspected,
				CompletionFence: input.CompletionFence,
				Now:             s.clock.Now().UTC(),
			})
			if finalizeErr != nil {
				return UploadCompletionDTO{}, s.failCompletion(
					ctx, uploadID, completionToken, false, finalizeErr.Error(), finalizeErr,
				)
			}

			return UploadCompletionDTO{
				UploadID: uploadID,
				Status:   UploadStatusCompleted,
				AssetID:  assetID,
			}, nil
		},
	)
}

func (s *Service) failCompletion(
	ctx context.Context,
	uploadID string,
	completionToken string,
	retryable bool,
	reason string,
	cause error,
) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if cleanupErr := s.repository.FailUploadCompletion(
		cleanupContext, uploadID, completionToken, retryable, reason, s.clock.Now().UTC(),
	); cleanupErr != nil {
		return errors.Join(cause, cleanupErr)
	}
	return cause
}

func (s *Service) AbandonUpload(ctx context.Context, actorID string, uploadID string) error {
	upload, err := s.repository.FindUpload(ctx, uploadID)
	if err != nil {
		return err
	}
	if upload.UploaderID != actorID {
		return apperror.Forbidden("Forbidden")
	}
	if upload.Status == UploadStatusCompleted {
		return nil
	}
	if upload.StoragePath != "" {
		_ = s.localMedia.DeleteAsset(upload.StoragePath)
	}
	return s.repository.AbandonUpload(ctx, actorID, uploadID)
}

func (s *Service) waitForUploadCompletion(
	ctx context.Context,
	uploadID string,
) (UploadCompletionDTO, error) {
	for attempt := 0; attempt < completionWaitAttempts; attempt++ {
		if err := s.sleeper.Sleep(ctx, completionWaitInterval); err != nil {
			return UploadCompletionDTO{}, err
		}
		status, err := s.repository.UploadCompletionStatus(ctx, uploadID)
		if err != nil {
			return UploadCompletionDTO{}, err
		}
		if status == UploadStatusCompleted {
			upload, err := s.repository.FindUpload(ctx, uploadID)
			if err != nil {
				return UploadCompletionDTO{}, err
			}
			return UploadCompletionDTO{
				UploadID: uploadID,
				Status:   UploadStatusCompleted,
				AssetID:  *upload.AssetID,
			}, nil
		}
		if status != UploadStatusCompleting {
			return UploadCompletionDTO{}, apperror.Conflict(
				apperror.CodeInvalidStateTransition,
				"Media upload completion failed",
				nil,
			)
		}
	}
	return UploadCompletionDTO{}, apperror.Conflict(
		apperror.CodeResourceConflict,
		"Media upload completion is taking too long",
		nil,
	)
}

func validateCreateUploadInput(input CreateUploadInput, maxBytes int64) error {
	if !input.Purpose.Valid() {
		return apperror.Validation("purpose is invalid")
	}
	if _, err := uuid.Parse(input.TargetID); err != nil {
		return apperror.Validation("targetId must be a UUID")
	}
	if length := javascriptStringLength(input.FileName); length < 1 || length > 255 {
		return apperror.Validation("fileName must contain 1 to 255 characters")
	}
	maxAllowedBytes := ArtworkMaximumBytes
	if input.Purpose == PurposeTrackSource {
		maxAllowedBytes = AudioSourceMaximumBytes
	}
	if input.SizeBytes < 1 || input.SizeBytes > maxAllowedBytes || input.SizeBytes > maxBytes {
		return apperror.Validation(fmt.Sprintf("sizeBytes must be between 1 and %d", maxAllowedBytes))
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
