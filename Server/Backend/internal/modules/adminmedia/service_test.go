package adminmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

type mediaStoreStub struct {
	createUpload           func(context.Context, UploadReservation) error
	findUpload             func(context.Context, string) (UploadReservation, error)
	claimUploadCompletion  func(context.Context, string, string, time.Time, time.Duration) (CompletionClaim, error)
	uploadCompletionStatus func(context.Context, string) (string, error)
	finalizeUpload         func(context.Context, FinalizeUploadParams) error
	failUploadCompletion   func(context.Context, string, string, bool, string, time.Time) error
}

func (stub *mediaStoreStub) CreateUpload(ctx context.Context, upload UploadReservation) error {
	if stub.createUpload == nil {
		return nil
	}
	return stub.createUpload(ctx, upload)
}
func (stub *mediaStoreStub) FindUpload(ctx context.Context, uploadID string) (UploadReservation, error) {
	if stub.findUpload == nil {
		return UploadReservation{}, nil
	}
	return stub.findUpload(ctx, uploadID)
}
func (stub *mediaStoreStub) ClaimUploadCompletion(ctx context.Context, uploadID, token string, now time.Time, lease time.Duration) (CompletionClaim, error) {
	if stub.claimUploadCompletion == nil {
		return CompletionClaim{}, nil
	}
	return stub.claimUploadCompletion(ctx, uploadID, token, now, lease)
}
func (stub *mediaStoreStub) UploadCompletionStatus(ctx context.Context, uploadID string) (string, error) {
	if stub.uploadCompletionStatus == nil {
		return "", nil
	}
	return stub.uploadCompletionStatus(ctx, uploadID)
}
func (stub *mediaStoreStub) FinalizeUpload(ctx context.Context, input FinalizeUploadParams) error {
	if stub.finalizeUpload == nil {
		return nil
	}
	return stub.finalizeUpload(ctx, input)
}
func (stub *mediaStoreStub) FailUploadCompletion(ctx context.Context, uploadID, token string, retryable bool, reason string, now time.Time) error {
	if stub.failUploadCompletion == nil {
		return nil
	}
	return stub.failUploadCompletion(ctx, uploadID, token, retryable, reason, now)
}
func (stub *mediaStoreStub) AbandonUpload(ctx context.Context, actorID, uploadID string) error {
	return nil
}

type directMediaIdempotency struct{}

func (directMediaIdempotency) ExecuteReservation(
	_ context.Context,
	_ IdempotencyInput,
	op func() (UploadReservationDTO, error),
) (UploadReservationDTO, bool, error) {
	body, err := op()
	return body, false, err
}

func (directMediaIdempotency) ExecuteCompletion(
	_ context.Context,
	_ IdempotencyInput,
	op func() (UploadCompletionDTO, error),
) (UploadCompletionDTO, bool, error) {
	body, err := op()
	return body, false, err
}

type mediaInspectorStub struct {
	result InspectedMedia
	err    error
}

func (stub *mediaInspectorStub) Inspect(_ context.Context, _ UploadReservation) (InspectedMedia, error) {
	return stub.result, stub.err
}

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

func newTestMediaService(
	t *testing.T,
	store Store,
	inspector MediaInspector,
	now time.Time,
) *Service {
	t.Helper()
	tempDir := t.TempDir()
	mediaStore, err := localmedia.NewStore(
		filepath.Join(tempDir, "assets"),
		filepath.Join(tempDir, "transcode"),
		1024*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	service, err := NewService(config.Config{
		MediaStorage: config.MediaStorage{
			UploadTTLSeconds: 300,
			MaxUploadBytes:   1024 * 1024 * 1024,
		},
	}, ServiceDependencies{
		Repository:  store,
		Idempotency: directMediaIdempotency{},
		LocalMedia:  mediaStore,
		Inspector:   inspector,
		Clock:       fixedClock{now: now},
		IDGenerator: func() string { return "upload-1" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCreateUploadReturnsUploadPath(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	checksum := strings.Repeat("a", 64)
	var created UploadReservation
	store := &mediaStoreStub{
		createUpload: func(_ context.Context, input UploadReservation) error {
			created = input
			return nil
		},
	}
	service := newTestMediaService(t, store, &mediaInspectorStub{}, now)
	res, _, err := service.CreateUpload(context.Background(), "admin-1", "key-1", CreateUploadInput{
		Purpose:        PurposeTrackSource,
		TargetID:       "00000000-0000-0000-0000-000000000001",
		FileName:       "source.flac",
		ContentType:    "audio/flac",
		SizeBytes:      1024,
		ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "upload-1" || res.Method != "PUT" || res.UploadPath != "/api/v1/admin/media/uploads/upload-1/content" {
		t.Fatalf("unexpected res: %#v", res)
	}
	if created.ID != "upload-1" || created.StoragePath != "temp/upload_upload-1.partial" {
		t.Fatalf("unexpected created: %#v", created)
	}
}

func TestDirectUploadAndComplete(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	data := []byte("test audio stream content 1234567890")
	hasher := sha256.New()
	hasher.Write(data)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	store := &mediaStoreStub{
		findUpload: func(_ context.Context, uploadID string) (UploadReservation, error) {
			return UploadReservation{
				ID:                     uploadID,
				Purpose:                PurposeTrackSource,
				TargetID:               "00000000-0000-0000-0000-000000000001",
				StoragePath:            "temp/upload_upload-1.partial",
				Status:                 UploadStatusCreated,
				ExpectedSize:           int64(len(data)),
				ExpectedChecksumSHA256: checksum,
			}, nil
		},
		claimUploadCompletion: func(_ context.Context, uploadID, token string, _ time.Time, _ time.Duration) (CompletionClaim, error) {
			return CompletionClaim{
				Outcome: CompletionClaimed,
				Token:   token,
				Upload: UploadReservation{
					ID:          uploadID,
					Purpose:     PurposeTrackSource,
					TargetID:    "00000000-0000-0000-0000-000000000001",
					StoragePath: "temp/upload_upload-1.partial",
					Status:      UploadStatusCompleting,
				},
			}, nil
		},
		finalizeUpload: func(_ context.Context, input FinalizeUploadParams) error {
			if input.UploadID != "upload-1" || input.Inspected.Kind != "AUDIO_SOURCE" {
				t.Fatalf("unexpected finalize: %#v", input)
			}
			return nil
		},
	}

	dur := int64(120000)
	inspector := &mediaInspectorStub{
		result: InspectedMedia{
			StoragePath:    "sources/upload-1.flac",
			Kind:           "AUDIO_SOURCE",
			MIMEType:       "audio/flac",
			SizeBytes:      int64(len(data)),
			ChecksumSHA256: "abc",
			DurationMs:     &dur,
		},
	}

	service := newTestMediaService(t, store, inspector, now)

	// Direct upload
	err := service.UploadDirect(context.Background(), "upload-1", bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	// Complete upload
	res, _, err := service.CompleteUpload(context.Background(), "admin-1", "upload-1", "key-comp", CompleteUploadInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != UploadStatusCompleted || res.UploadID != "upload-1" {
		t.Fatalf("unexpected complete res: %#v", res)
	}
}

func TestValidationErrors(t *testing.T) {
	service := newTestMediaService(t, &mediaStoreStub{}, &mediaInspectorStub{}, time.Now())
	_, _, err := service.CreateUpload(context.Background(), "admin-1", "key-1", CreateUploadInput{
		Purpose: "INVALID",
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
