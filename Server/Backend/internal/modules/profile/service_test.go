package profile

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/shared/apperror"
)

type profileStoreStub struct {
	updateProfile          func(context.Context, string, int, ProfileChanges, time.Time) error
	createAvatarUpload     func(context.Context, CreateUploadParams) (AvatarUpload, error)
	markAvatarUploadFailed func(context.Context, string, string) error
	claimCompletion        func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error)
	completionStatus       func(context.Context, string, string) (string, error)
	finalizeCompletion     func(context.Context, FinalizeAvatarParams) error
	failCompletion         func(context.Context, string, string, bool, []string, string, time.Time) error
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
func (stub *profileStoreStub) FailAvatarCompletion(ctx context.Context, uploadID, token string, retryable bool, keys []string, reason string, now time.Time) error {
	if stub.failCompletion == nil {
		return errors.New("unexpected FailAvatarCompletion call")
	}
	return stub.failCompletion(ctx, uploadID, token, retryable, keys, reason, now)
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

type avatarStorageStub struct {
	request UploadURLRequest
	url     string
}

func (stub *avatarStorageStub) CreateUploadURL(_ context.Context, request UploadURLRequest) (string, error) {
	stub.request = request
	return stub.url, nil
}
func (*avatarStorageStub) DownloadToFile(context.Context, string, string, int64) (StoredObject, error) {
	return StoredObject{}, errors.New("unexpected DownloadToFile call")
}
func (*avatarStorageStub) UploadFile(context.Context, string, string, string, string) (int64, error) {
	return 0, errors.New("unexpected UploadFile call")
}

type avatarInspectorStub struct {
	upload AvatarUpload
	etag   string
	result InspectedAvatar
	err    error
}

func (stub *avatarInspectorStub) Inspect(_ context.Context, upload AvatarUpload, etag string) (InspectedAvatar, error) {
	stub.upload = upload
	stub.etag = etag
	return stub.result, stub.err
}

type fixedProfileClock struct{ now time.Time }

func (clock fixedProfileClock) Now() time.Time { return clock.now }

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
	service := newProfileTestService(t, store, reader, idempotency, &avatarStorageStub{}, &avatarInspectorStub{}, now)
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
	service := newProfileTestService(t, &profileStoreStub{}, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarStorageStub{}, &avatarInspectorStub{}, time.Now())
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
	service = newProfileTestService(t, store, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarStorageStub{}, &avatarInspectorStub{}, time.Now())
	_, err = service.UpdateCurrentUser(context.Background(), "user-1", "profile-key-2", UpdateProfileInput{
		ExpectedVersion: 1,
		DisplayName:     OptionalString{Set: true, Value: "Alice"},
	})
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeVersionConflict || applicationError.Metadata["currentVersion"] != 2 {
		t.Fatalf("version conflict = %#v", err)
	}
}

func TestCreateAvatarUploadReservesOnlyActorAndReturnsSignedHeaderContract(t *testing.T) {
	now := time.Date(2026, time.July, 16, 3, 0, 0, 0, time.UTC)
	store := &profileStoreStub{}
	store.createAvatarUpload = func(_ context.Context, input CreateUploadParams) (AvatarUpload, error) {
		if input.ActorID != "user-1" || input.ID != "upload-1" || input.ObjectKey != "uploads/user-1/upload-1" {
			t.Fatalf("reservation = %#v", input)
		}
		return AvatarUpload{
			ID:                     input.ID,
			Purpose:                AvatarUploadPurpose,
			TargetID:               input.ActorID,
			UploaderID:             input.ActorID,
			ObjectKey:              input.ObjectKey,
			ExpectedSize:           input.SizeBytes,
			ExpectedChecksumSHA256: input.ChecksumSHA256,
			ExpectedMIMEType:       input.ContentType,
			Status:                 UploadStatusCreated,
			ExpiresAt:              input.ExpiresAt,
		}, nil
	}
	storage := &avatarStorageStub{url: "https://objects.example/upload"}
	service := newProfileTestService(t, store, &currentUserReaderStub{}, &directProfileIdempotency{}, storage, &avatarInspectorStub{}, now)
	service.newID = func() string { return "upload-1" }
	checksum := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	result, err := service.CreateAvatarUpload(context.Background(), "user-1", "trace-123", "avatar-key-1", CreateAvatarUploadInput{
		FileName:       "avatar.PNG",
		ContentType:    "image/png",
		SizeBytes:      1024,
		ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Body.TargetID != "user-1" || result.Body.Purpose != AvatarUploadPurpose || result.Body.UploadURL != storage.url {
		t.Fatalf("result = %#v", result.Body)
	}
	decoded, _ := hex.DecodeString(checksum)
	if result.Body.RequiredHeaders["x-amz-checksum-sha256"] != base64.StdEncoding.EncodeToString(decoded) ||
		result.Body.RequiredHeaders["content-type"] != "image/png" ||
		result.Body.RequiredHeaders["content-length"] != "1024" {
		t.Fatalf("headers = %#v", result.Body.RequiredHeaders)
	}
	if storage.request.ObjectKey != "uploads/user-1/upload-1" || storage.request.ChecksumSHA256 != checksum {
		t.Fatalf("storage request = %#v", storage.request)
	}
}

func TestAvatarReservationValidationMatchesLegacyLimits(t *testing.T) {
	service := newProfileTestService(t, &profileStoreStub{}, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarStorageStub{}, &avatarInspectorStub{}, time.Now())
	tests := []struct {
		name  string
		input CreateAvatarUploadInput
		code  apperror.Code
	}{
		{name: "five MiB", input: validAvatarInput(AvatarMaximumBytes + 1), code: apperror.CodePayloadTooLarge},
		{name: "mime", input: func() CreateAvatarUploadInput {
			value := validAvatarInput(1)
			value.ContentType = "image/gif"
			return value
		}(), code: apperror.CodeValidationError},
		{name: "mime case", input: func() CreateAvatarUploadInput {
			value := validAvatarInput(1)
			value.ContentType = "IMAGE/PNG"
			return value
		}(), code: apperror.CodeValidationError},
		{name: "checksum", input: func() CreateAvatarUploadInput {
			value := validAvatarInput(1)
			value.ChecksumSHA256 = "ABC"
			return value
		}(), code: apperror.CodeValidationError},
		{name: "extension", input: func() CreateAvatarUploadInput {
			value := validAvatarInput(1)
			value.FileName = "avatar.gif"
			return value
		}(), code: apperror.CodeValidationError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.CreateAvatarUpload(context.Background(), "user-1", "trace", "avatar-key", test.input)
			if !apperror.IsCode(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestCompleteAvatarUploadInspectsAndAtomicallyAttachesAvatar(t *testing.T) {
	now := time.Date(2026, time.July, 16, 4, 0, 0, 0, time.UTC)
	upload := AvatarUpload{
		ID:                     "upload-1",
		Purpose:                AvatarUploadPurpose,
		TargetID:               "user-1",
		UploaderID:             "user-1",
		ObjectKey:              "uploads/user-1/upload-1",
		ExpectedSize:           100,
		ExpectedChecksumSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExpectedMIMEType:       "image/png",
		Status:                 UploadStatusCompleting,
	}
	store := &profileStoreStub{}
	store.claimCompletion = func(_ context.Context, actorID, uploadID, token string, actualNow time.Time, lease time.Duration) (CompletionClaim, error) {
		if actorID != "user-1" || uploadID != "upload-1" || token != "completion-token" || actualNow != now || lease != completionLease {
			t.Fatalf("unexpected claim: %q %q %q %v %v", actorID, uploadID, token, actualNow, lease)
		}
		return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: token}, nil
	}
	store.finalizeCompletion = func(_ context.Context, input FinalizeAvatarParams) error {
		if input.ActorID != "user-1" || input.UploadID != "upload-1" || input.CompletionToken != "completion-token" ||
			input.AssetID != "asset-1" || input.Inspected.ObjectKey != "media/avatar.jpg" || input.TraceID != "trace-complete" {
			t.Fatalf("finalize = %#v", input)
		}
		return nil
	}
	inspector := &avatarInspectorStub{result: InspectedAvatar{
		ObjectKey:      "media/avatar.jpg",
		MIMEType:       "image/jpeg",
		SizeBytes:      80,
		ChecksumSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Width:          128,
		Height:         128,
		CleanupKeys:    []string{upload.ObjectKey, "media/avatar.jpg"},
	}}
	reader := &currentUserReaderStub{user: compatibleCurrentUser()}
	service := newProfileTestService(t, store, reader, &directProfileIdempotency{}, &avatarStorageStub{}, inspector, now)
	ids := []string{"completion-token", "asset-1"}
	service.newID = func() string {
		value := ids[0]
		ids = ids[1:]
		return value
	}
	result, err := service.CompleteAvatarUpload(context.Background(), "user-1", "trace-complete", "upload-1", "complete-key", CompleteAvatarUploadInput{
		ObservedETag: OptionalString{Set: true, Value: `"etag"`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspector.upload.ID != "upload-1" || inspector.etag != "etag" || result.Body.ID != "user-1" || reader.calls != 1 {
		t.Fatalf("inspection/result = %#v etag=%q result=%#v", inspector.upload, inspector.etag, result.Body)
	}
}

func TestCompleteAvatarUploadQueuesFailedObjectAndDoesNotAttach(t *testing.T) {
	now := time.Now().UTC()
	upload := AvatarUpload{ID: "upload-1", ObjectKey: "uploads/user-1/upload-1", TargetID: "user-1", UploaderID: "user-1", Purpose: AvatarUploadPurpose}
	store := &profileStoreStub{}
	store.claimCompletion = func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
		return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: "token"}, nil
	}
	failed := false
	store.failCompletion = func(_ context.Context, uploadID, token string, retryable bool, keys []string, reason string, _ time.Time) error {
		failed = true
		if uploadID != "upload-1" || token != "token" || retryable || len(keys) != 1 || keys[0] != upload.ObjectKey || reason != "UPLOAD_FAILED_MEDIA_UPLOAD_MISMATCH" {
			t.Fatalf("failure = %q %q %v %#v %q", uploadID, token, retryable, keys, reason)
		}
		return nil
	}
	inspector := &avatarInspectorStub{err: apperror.Unprocessable(apperror.CodeMediaUploadMismatch, "bad image", nil)}
	service := newProfileTestService(t, store, &currentUserReaderStub{}, &directProfileIdempotency{}, &avatarStorageStub{}, inspector, now)
	service.newID = func() string { return "token" }
	_, err := service.CompleteAvatarUpload(context.Background(), "user-1", "trace", "upload-1", "complete-key", CompleteAvatarUploadInput{})
	if !apperror.IsCode(err, apperror.CodeMediaUploadMismatch) || !failed {
		t.Fatalf("error = %v failed=%v", err, failed)
	}
}

func validAvatarInput(size int64) CreateAvatarUploadInput {
	return CreateAvatarUploadInput{
		FileName:       "avatar.png",
		ContentType:    "image/png",
		SizeBytes:      size,
		ChecksumSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
}

func newProfileTestService(
	t *testing.T,
	store Store,
	reader CurrentUserReader,
	idempotency Idempotency,
	storage AvatarObjectStorage,
	inspector AvatarInspector,
	now time.Time,
) *Service {
	t.Helper()
	service, err := NewService(config.Config{
		Storage: config.Storage{SignedURLTTLSeconds: 300, MaxUploadBytes: 1024 * 1024 * 1024},
		Media:   config.Media{FFprobePath: "ffprobe", FFmpegPath: "ffmpeg"},
	}, ServiceDependencies{
		Repository:   store,
		CurrentUsers: reader,
		Idempotency:  idempotency,
		Storage:      storage,
		Inspector:    inspector,
		Clock:        fixedProfileClock{now: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
