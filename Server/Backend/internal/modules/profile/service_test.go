package profile

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

type profileStoreStub struct {
	updateProfile          func(context.Context, string, int, ProfileChanges, time.Time) error
	createAvatarUpload     func(context.Context, CreateUploadParams) (AvatarUpload, error)
	findAvatarUpload       func(context.Context, string, string) (AvatarUpload, error)
	markAvatarUploadFailed func(context.Context, string, string) error
	claimCompletion        func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error)
	completionStatus       func(context.Context, string, string) (string, error)
	finalizeCompletion     func(context.Context, FinalizeAvatarParams) error
	failCompletion         func(context.Context, string, string, bool, string, time.Time) error
}

func (stub *profileStoreStub) UpdateProfile(ctx context.Context, userID string, version int, changes ProfileChanges, now time.Time) error {
	if stub.updateProfile == nil {
		return errors.New("unexpected UpdateProfile call")
	}
	return stub.updateProfile(ctx, userID, version, changes, now)
}
func (stub *profileStoreStub) CreateAvatarUpload(ctx context.Context, input CreateUploadParams) (AvatarUpload, error) {
	if stub.createAvatarUpload == nil {
		return AvatarUpload{}, errors.New("unexpected CreateAvatarUpload call")
	}
	return stub.createAvatarUpload(ctx, input)
}
func (stub *profileStoreStub) FindAvatarUpload(ctx context.Context, actorID, uploadID string) (AvatarUpload, error) {
	if stub.findAvatarUpload == nil {
		return AvatarUpload{}, errors.New("unexpected FindAvatarUpload call")
	}
	return stub.findAvatarUpload(ctx, actorID, uploadID)
}
func (stub *profileStoreStub) MarkAvatarUploadFailed(ctx context.Context, actorID, uploadID string) error {
	if stub.markAvatarUploadFailed == nil {
		return errors.New("unexpected MarkAvatarUploadFailed call")
	}
	return stub.markAvatarUploadFailed(ctx, actorID, uploadID)
}
func (stub *profileStoreStub) ClaimAvatarCompletion(ctx context.Context, actorID, uploadID, token string, now time.Time, lease time.Duration) (CompletionClaim, error) {
	if stub.claimCompletion == nil {
		return CompletionClaim{}, errors.New("unexpected ClaimAvatarCompletion call")
	}
	return stub.claimCompletion(ctx, actorID, uploadID, token, now, lease)
}
func (stub *profileStoreStub) AvatarCompletionStatus(ctx context.Context, actorID, uploadID string) (string, error) {
	if stub.completionStatus == nil {
		return "", errors.New("unexpected AvatarCompletionStatus call")
	}
	return stub.completionStatus(ctx, actorID, uploadID)
}
func (stub *profileStoreStub) FinalizeAvatarCompletion(ctx context.Context, input FinalizeAvatarParams) error {
	if stub.finalizeCompletion == nil {
		return errors.New("unexpected FinalizeAvatarCompletion call")
	}
	return stub.finalizeCompletion(ctx, input)
}
func (stub *profileStoreStub) FailAvatarCompletion(ctx context.Context, uploadID, token string, retryable bool, reason string, now time.Time) error {
	if stub.failCompletion == nil {
		return errors.New("unexpected FailAvatarCompletion call")
	}
	return stub.failCompletion(ctx, uploadID, token, retryable, reason, now)
}

type directProfileIdempotency struct {
	last IdempotencyInput
}

func (stub *directProfileIdempotency) ExecuteCurrentUser(
	_ context.Context,
	input IdempotencyInput,
	_ int,
	operation func() (identity.CurrentUserDTO, error),
) (MutationResult[identity.CurrentUserDTO], error) {
	stub.last = input
	body, err := operation()
	return MutationResult[identity.CurrentUserDTO]{Body: body}, err
}
func (stub *directProfileIdempotency) ExecuteAvatarUpload(
	_ context.Context,
	input IdempotencyInput,
	_ int,
	operation func() (AvatarUploadDTO, error),
) (MutationResult[AvatarUploadDTO], error) {
	stub.last = input
	body, err := operation()
	return MutationResult[AvatarUploadDTO]{Body: body}, err
}

type currentUserReaderStub struct {
	calls int
	user  identity.CurrentUserDTO
}

func (stub *currentUserReaderStub) CurrentUser(context.Context, string) (identity.CurrentUserDTO, error) {
	stub.calls++
	return stub.user, nil
}

type avatarInspectorStub struct {
	upload AvatarUpload
	result InspectedAvatar
	err    error
}

func (stub *avatarInspectorStub) Inspect(_ context.Context, upload AvatarUpload) (InspectedAvatar, error) {
	stub.upload = upload
	return stub.result, stub.err
}

type fixedProfileClock struct{ now time.Time }

func (clock fixedProfileClock) Now() time.Time { return clock.now }

func newProfileTestService(
	t *testing.T,
	store Store,
	reader CurrentUserReader,
	idempotency Idempotency,
	inspector AvatarInspector,
	now time.Time,
) *Service {
	t.Helper()
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		10*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	service, err := NewService(config.Config{
		MediaStorage: config.MediaStorage{
			UploadTTLSeconds: 300,
			MaxUploadBytes:   10 * 1024 * 1024,
		},
	}, ServiceDependencies{
		Repository:   store,
		CurrentUsers: reader,
		Idempotency:  idempotency,
		LocalMedia:   mediaStore,
		Inspector:    inspector,
		Clock:        fixedProfileClock{now: now},
		IDGenerator:  func() string { return "upload-1" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func compatibleCurrentUser() identity.CurrentUserDTO {
	return identity.CurrentUserDTO{
		ID:          "user-1",
		Username:    "alice",
		DisplayName: "Alice",
		Role:        "USER",
		Status:      "ACTIVE",
		Version:     7,
		CreatedAt:   "2026-01-01T00:00:00.000Z",
		UpdatedAt:   "2026-01-01T00:00:00.000Z",
	}
}

func TestUpdateCurrentUserTrimsFieldsAndPreservesCurrentUserDTO(t *testing.T) {
	now := time.Date(2026, time.July, 16, 2, 3, 4, 0, time.UTC)
	store := &profileStoreStub{}
	store.updateProfile = func(_ context.Context, userID string, version int, changes ProfileChanges, actualNow time.Time) error {
		if userID != "user-1" || version != 7 || actualNow != now {
			t.Fatalf("unexpected update coordinates: %q %d %v", userID, version, actualNow)
		}
		if !changes.DisplayNameSet || changes.DisplayName != "Alice" || !changes.BioSet || changes.Bio == nil || *changes.Bio != "hello" {
			t.Fatalf("changes = %#v", changes)
		}
		return nil
	}
	reader := &currentUserReaderStub{user: compatibleCurrentUser()}
	idempotency := &directProfileIdempotency{}
	service := newProfileTestService(t, store, reader, idempotency, &avatarInspectorStub{}, now)
	bio := "  hello  "
	result, err := service.UpdateCurrentUser(context.Background(), "user-1", "profile-key-1", UpdateProfileInput{
		ExpectedVersion: 7,
		DisplayName:     OptionalString{Set: true, Value: "  Alice  "},
		Bio:             OptionalNullableString{Set: true, Value: &bio},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Body.DisplayName != "Alice" || reader.calls != 1 {
		t.Fatalf("result = %#v calls=%d", result.Body, reader.calls)
	}
	if idempotency.last.Scope != "profile.update" || idempotency.last.Key != "profile-key-1" {
		t.Fatalf("idempotency = %#v", idempotency.last)
	}
}

func TestUpdateCurrentUserValidationAndVersionConflictMetadata(t *testing.T) {
	service := newProfileTestService(t, &profileStoreStub{}, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarInspectorStub{}, time.Now())
	_, err := service.UpdateCurrentUser(context.Background(), "user-1", "profile-key-1", UpdateProfileInput{ExpectedVersion: 1})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("missing fields error = %v", err)
	}

	store := &profileStoreStub{}
	store.updateProfile = func(context.Context, string, int, ProfileChanges, time.Time) error {
		return apperror.Conflict(apperror.CodeVersionConflict, "stale", map[string]any{
			"expectedVersion": 1,
			"currentVersion":  2,
		})
	}
	service = newProfileTestService(t, store, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarInspectorStub{}, time.Now())
	_, err = service.UpdateCurrentUser(context.Background(), "user-1", "profile-key-2", UpdateProfileInput{
		ExpectedVersion: 1,
		DisplayName:     OptionalString{Set: true, Value: "Alice"},
	})
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeVersionConflict || applicationError.Metadata["currentVersion"] != 2 {
		t.Fatalf("version conflict = %#v", err)
	}
}

func TestCreateAvatarUploadReservesAndReturnsUploadPath(t *testing.T) {
	now := time.Date(2026, time.July, 16, 3, 0, 0, 0, time.UTC)
	store := &profileStoreStub{}
	store.createAvatarUpload = func(_ context.Context, input CreateUploadParams) (AvatarUpload, error) {
		if input.ActorID != "user-1" || input.ID != "upload-1" {
			t.Fatalf("reservation = %#v", input)
		}
		return AvatarUpload{
			ID:        input.ID,
			Purpose:   AvatarUploadPurpose,
			TargetID:  input.ActorID,
			Status:    UploadStatusCreated,
			ExpiresAt: input.ExpiresAt,
		}, nil
	}
	service := newProfileTestService(t, store, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarInspectorStub{}, now)
	result, err := service.CreateAvatarUpload(context.Background(), "user-1", "avatar-key-1", CreateAvatarUploadInput{
		FileName:       "avatar.png",
		ContentType:    "image/png",
		SizeBytes:      1024,
		ChecksumSHA256: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Body.Method != "PUT" || result.Body.UploadPath != "/api/v1/users/me/avatar/uploads/upload-1" {
		t.Fatalf("reservation result = %#v", result.Body)
	}
}

func TestDirectUploadAndCompleteAvatar(t *testing.T) {
	now := time.Date(2026, time.July, 16, 3, 0, 0, 0, time.UTC)
	data := []byte("avatar bytes content")

	store := &profileStoreStub{}
	store.findAvatarUpload = func(_ context.Context, actorID, uploadID string) (AvatarUpload, error) {
		return AvatarUpload{
			ID:                     uploadID,
			UploaderID:             actorID,
			Status:                 UploadStatusCreated,
			StoragePath:            "temp/avatar_upload-1.partial",
			ExpectedSize:           int64(len(data)),
			ExpectedChecksumSHA256: "7214b7e80d46219be3d5e27a69bcba58d56b464a8c9a6331a980eb7f2d4e8c18", // dummy
		}, nil
	}

	inspector := &avatarInspectorStub{
		result: InspectedAvatar{
			StoragePath:    "avatars/upload-1.jpg",
			MIMEType:       "image/jpeg",
			SizeBytes:      int64(len(data)),
			ChecksumSHA256: "abc",
			Width:          200,
			Height:         200,
		},
	}
	store.claimCompletion = func(_ context.Context, actorID, uploadID, token string, _ time.Time, _ time.Duration) (CompletionClaim, error) {
		return CompletionClaim{
			Outcome: CompletionClaimed,
			Token:   token,
			Upload: AvatarUpload{
				ID:          uploadID,
				UploaderID:  actorID,
				TargetID:    actorID,
				Status:      UploadStatusCompleting,
				StoragePath: "temp/avatar_upload-1.partial",
			},
		}, nil
	}
	store.finalizeCompletion = func(_ context.Context, input FinalizeAvatarParams) error {
		if input.UploadID != "upload-1" || input.Inspected.StoragePath != "avatars/upload-1.jpg" {
			t.Fatalf("unexpected finalize params: %#v", input)
		}
		return nil
	}

	reader := &currentUserReaderStub{user: compatibleCurrentUser()}
	service := newProfileTestService(t, store, reader, &directProfileIdempotency{}, inspector, now)

	// Complete avatar
	completeResult, err := service.CompleteAvatarUpload(context.Background(), "user-1", "upload-1", "key-comp", CompleteAvatarUploadInput{})
	if err != nil {
		t.Fatal(err)
	}
	if completeResult.Body.ID != "user-1" {
		t.Fatalf("complete result = %#v", completeResult.Body)
	}
}
