package adminmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/config"
	"xymusic/server/internal/shared/apperror"
)

func TestCreateUploadReservesSignsAuditsAndPresentsRequiredHeaders(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 456789000, time.UTC)
	checksum := stringOf('a', 64)
	var created CreateUploadParams
	var audit AuditWrite
	store := &mediaStoreStub{}
	store.createUpload = func(_ context.Context, input CreateUploadParams) (MediaUpload, error) {
		created = input
		return MediaUpload{
			ID: input.ID, Purpose: input.Purpose, TargetID: input.TargetID,
			UploaderID: input.ActorID, ObjectKey: input.ObjectKey,
			ExpectedSize: input.SizeBytes, ExpectedChecksumSHA256: input.ChecksumSHA256,
			ExpectedMIMEType: input.ContentType, OriginalFileName: input.FileName,
			Status: UploadStatusCreated, ExpiresAt: input.ExpiresAt, CreatedAt: input.Now,
		}, nil
	}
	store.writeAudit = func(_ context.Context, input AuditWrite) error { audit = input; return nil }
	storage := &mediaStorageStub{createURL: func(_ context.Context, input UploadURLRequest) (string, error) {
		if input.ContentLength != 123 || input.ChecksumSHA256 != checksum || input.Expires != 5*time.Minute {
			t.Fatalf("upload URL request = %#v", input)
		}
		return "https://storage.test/signed", nil
	}}
	service := newMediaService(t, store, storage, &mediaInspectorStub{}, fixedClock{now}, ids("upload-1"))
	result, err := service.CreateUpload(context.Background(), "admin-1", "trace-12345678", CreateUploadInput{
		Purpose:        PurposeTrackSource,
		TargetID:       "00000000-0000-0000-0000-000000000001",
		FileName:       "source.FLAC",
		ContentType:    "Audio/FLAC",
		SizeBytes:      123,
		ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "upload-1" || created.ContentType != "audio/flac" ||
		created.ObjectKey != "uploads/admin-1/upload-1" || created.MaximumBytes != 1024*1024*1024 {
		t.Fatalf("created = %#v", created)
	}
	if result.UploadURL != "https://storage.test/signed" || result.Method != "PUT" ||
		result.RequiredHeaders["content-length"] != "123" ||
		result.RequiredHeaders["x-amz-meta-sha256"] != checksum ||
		result.ExpiresAt != "2026-07-16T01:07:03.456Z" {
		t.Fatalf("result = %#v", result)
	}
	if audit.Action != "media.upload.create" || audit.TargetID != "upload-1" ||
		audit.Details["purpose"] != PurposeTrackSource {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestCreateUploadMarksReservationFailedWhenSigningFails(t *testing.T) {
	now := time.Now().UTC()
	var failedActor, failedUpload string
	store := &mediaStoreStub{
		createUpload: func(_ context.Context, input CreateUploadParams) (MediaUpload, error) {
			return MediaUpload{
				ID: input.ID, Purpose: input.Purpose, TargetID: input.TargetID,
				UploaderID: input.ActorID, ObjectKey: input.ObjectKey,
				ExpectedSize: input.SizeBytes, ExpectedChecksumSHA256: input.ChecksumSHA256,
				ExpectedMIMEType: input.ContentType, Status: UploadStatusCreated,
				ExpiresAt: input.ExpiresAt,
			}, nil
		},
		markUploadFailed: func(_ context.Context, actorID, uploadID string) error {
			failedActor, failedUpload = actorID, uploadID
			return nil
		},
	}
	storage := &mediaStorageStub{createURL: func(context.Context, UploadURLRequest) (string, error) {
		return "", errors.New("storage down")
	}}
	service := newMediaService(t, store, storage, &mediaInspectorStub{}, fixedClock{now}, ids("upload-1"))
	_, err := service.CreateUpload(context.Background(), "admin-1", "trace-12345678", validImageUpload())
	if !apperror.IsCode(err, apperror.CodeDependencyUnavailable) ||
		failedActor != "admin-1" || failedUpload != "upload-1" {
		t.Fatalf("error/failed = %v / %q %q", err, failedActor, failedUpload)
	}
}

func TestUploadContentStrictlyChecksLengthChecksumAndBinaryMIME(t *testing.T) {
	payload := testPNG(t)
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	now := time.Now().UTC()
	upload := MediaUpload{
		ID: "upload-1", Purpose: PurposeArtistArtwork,
		UploaderID: "admin-1", ObjectKey: "uploads/admin-1/upload-1",
		ExpectedSize: int64(len(payload)), ExpectedChecksumSHA256: checksum,
		ExpectedMIMEType: "image/png", Status: UploadStatusCreated,
		ExpiresAt: now.Add(time.Minute),
	}
	store := &mediaStoreStub{findUploadForContent: func(context.Context, string, string) (MediaUpload, error) {
		return upload, nil
	}}
	var stored []byte
	storage := &mediaStorageStub{uploadFile: func(_ context.Context, key, path, contentType, suppliedChecksum string) (int64, error) {
		var err error
		stored, err = os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		if key != upload.ObjectKey || contentType != "image/png" || suppliedChecksum != checksum {
			t.Fatalf("upload args = %q %q %q", key, contentType, suppliedChecksum)
		}
		return int64(len(stored)), nil
	}}
	service := newMediaService(t, store, storage, &mediaInspectorStub{}, fixedClock{now}, ids())
	if err := service.UploadContent(
		context.Background(), "admin-1", "upload-1", "image/png; charset=binary",
		int64(len(payload)), bytes.NewReader(payload),
	); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, payload) {
		t.Fatal("stored payload differs")
	}

	tests := []struct {
		name        string
		length      int64
		body        []byte
		contentType string
		checksum    string
		code        apperror.Code
	}{
		{"declared length", int64(len(payload) - 1), payload, "image/png", checksum, apperror.CodeValidationError},
		{"too many bytes", -1, append(append([]byte(nil), payload...), 0), "image/png", checksum, apperror.CodePayloadTooLarge},
		{"too few bytes", -1, payload[:len(payload)-1], "image/png", checksum, apperror.CodeValidationError},
		{"wrong content type", int64(len(payload)), payload, "image/jpeg", checksum, apperror.CodeValidationError},
		{"wrong checksum", int64(len(payload)), payload, "image/png", stringOf('b', 64), apperror.CodeValidationError},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			candidate := upload
			candidate.ExpectedChecksumSHA256 = item.checksum
			store.findUploadForContent = func(context.Context, string, string) (MediaUpload, error) {
				return candidate, nil
			}
			err := service.UploadContent(
				context.Background(), "admin-1", "upload-1", item.contentType,
				item.length, bytes.NewReader(item.body),
			)
			if !apperror.IsCode(err, item.code) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	textPayload := bytes.Repeat([]byte("x"), len(payload))
	textDigest := sha256.Sum256(textPayload)
	candidate := upload
	candidate.ExpectedChecksumSHA256 = hex.EncodeToString(textDigest[:])
	store.findUploadForContent = func(context.Context, string, string) (MediaUpload, error) { return candidate, nil }
	err := service.UploadContent(
		context.Background(), "admin-1", "upload-1", "image/png",
		int64(len(textPayload)), bytes.NewReader(textPayload),
	)
	if !apperror.IsCode(err, apperror.CodeMediaUploadMismatch) {
		t.Fatalf("binary MIME error = %v", err)
	}
}

func TestAbandonUploadDelegatesActorUploadAndClock(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 30, 0, 0, time.UTC)
	called := false
	store := &mediaStoreStub{abandonUpload: func(_ context.Context, actorID, uploadID string, abandonedAt time.Time) error {
		called = true
		if actorID != "admin-1" || uploadID != "upload-1" || abandonedAt != now {
			t.Fatalf("abandon args = %q %q %v", actorID, uploadID, abandonedAt)
		}
		return nil
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, &mediaInspectorStub{}, fixedClock{now}, ids())
	if err := service.AbandonUpload(context.Background(), "admin-1", "upload-1"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("abandon repository was not called")
	}
}

func TestCompleteTrackUploadInspectsFinalizesAndReturnsProcessingJob(t *testing.T) {
	now := time.Date(2026, 7, 16, 2, 0, 0, 0, time.UTC)
	upload := MediaUpload{
		ID: "upload-1", Purpose: PurposeTrackSource, TargetID: "track-1",
		UploaderID: "admin-1", ObjectKey: "uploads/admin-1/upload-1",
		Status: UploadStatusCompleting,
	}
	var finalized FinalizeCompletionParams
	fence := &completionFenceStub{}
	jobID := "job-1"
	store := &mediaStoreStub{
		claimCompletion: func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
			return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: "claim-1"}, nil
		},
		finalizeCompletion: func(_ context.Context, input FinalizeCompletionParams) (CompletedUpload, error) {
			finalized = input
			return CompletedUpload{UploadID: input.UploadID, AssetID: input.AssetID, JobID: &jobID}, nil
		},
	}
	inspector := &mediaInspectorStub{inspect: func(_ context.Context, got MediaUpload, etag string) (InspectedUpload, error) {
		if got.ID != upload.ID || etag != "etag-value" {
			t.Fatalf("inspect = %#v %q", got, etag)
		}
		return InspectedUpload{
			ObjectKey: got.ObjectKey, MIMEType: "audio/flac", SizeBytes: 10,
			ChecksumSHA256: stringOf('a', 64), CleanupKeys: []string{got.ObjectKey},
		}, nil
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, inspector, fixedClock{now},
		ids("claim-generated", "asset-1", "job-generated"))
	result, err := service.CompleteUpload(context.Background(), "admin-1", "trace-12345678", "upload-1", CompleteUploadInput{
		ObservedETag:    OptionalString{Set: true, Value: `"etag-value"`},
		CompletionFence: fence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != JobStatusProcessing || result.JobID == nil || *result.JobID != jobID ||
		result.AssetID != "asset-1" {
		t.Fatalf("result = %#v", result)
	}
	if finalized.CompletionToken != "claim-1" || finalized.JobID != "job-generated" ||
		finalized.TraceID != "trace-12345678" || finalized.Now != now || finalized.CompletionFence != fence {
		t.Fatalf("finalized = %#v", finalized)
	}
}

func TestCompleteUploadRecoversCommittedResultWithCancelledRequestContext(t *testing.T) {
	now := time.Now().UTC()
	assetID := "asset-committed"
	upload := MediaUpload{
		ID: "upload-1", Purpose: PurposeAlbumArtwork, UploaderID: "admin-1",
		ObjectKey: "uploads/admin-1/upload-1", Status: UploadStatusCompleting,
	}
	requestContext, cancelRequest := context.WithCancel(context.Background())
	cleanupCalled := false
	store := &mediaStoreStub{
		claimCompletion: func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
			return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: "claim-1"}, nil
		},
		finalizeCompletion: func(context.Context, FinalizeCompletionParams) (CompletedUpload, error) {
			cancelRequest()
			return CompletedUpload{}, context.Canceled
		},
		completionStatus: func(ctx context.Context, _, _ string) (MediaUpload, error) {
			if err := ctx.Err(); err != nil {
				t.Fatalf("completion recovery inherited request cancellation: %v", err)
			}
			completed := upload
			completed.Status = UploadStatusCompleted
			completed.AssetID = &assetID
			return completed, nil
		},
		failCompletion: func(context.Context, string, string, bool, []string, string, time.Time) error {
			cleanupCalled = true
			return nil
		},
	}
	inspector := &mediaInspectorStub{inspect: func(context.Context, MediaUpload, string) (InspectedUpload, error) {
		return InspectedUpload{ObjectKey: upload.ObjectKey, CleanupKeys: []string{upload.ObjectKey}}, nil
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, inspector, fixedClock{now},
		ids("claim-generated", "asset-generated", "job-generated"))
	result, err := service.CompleteUpload(requestContext, "admin-1", "trace-12345678", upload.ID, CompleteUploadInput{})
	if err != nil || result.AssetID != assetID || cleanupCalled {
		t.Fatalf("result/error/cleanup = %#v / %v / %v", result, err, cleanupCalled)
	}
}

func TestCompleteUploadCleansAllStagingObjectsAfterFenceFailure(t *testing.T) {
	now := time.Now().UTC()
	upload := MediaUpload{
		ID: "upload-1", Purpose: PurposeAlbumArtwork, UploaderID: "admin-1",
		ObjectKey: "uploads/admin-1/upload-1", Status: UploadStatusCompleting,
	}
	fenceErr := errors.New("batch attempt fence lost")
	requestContext, cancelRequest := context.WithCancel(context.Background())
	var cleaned []string
	store := &mediaStoreStub{
		claimCompletion: func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
			return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: "claim-1"}, nil
		},
		finalizeCompletion: func(ctx context.Context, input FinalizeCompletionParams) (CompletedUpload, error) {
			if input.CompletionFence == nil {
				t.Fatal("completion fence was not forwarded")
			}
			cancelRequest()
			return CompletedUpload{}, fenceErr
		},
		completionStatus: func(ctx context.Context, _, _ string) (MediaUpload, error) {
			if err := ctx.Err(); err != nil {
				t.Fatalf("completion status inherited request cancellation: %v", err)
			}
			return upload, nil
		},
		failCompletion: func(ctx context.Context, _, _ string, retryable bool, keys []string, _ string, _ time.Time) error {
			if err := ctx.Err(); err != nil {
				t.Fatalf("cleanup inherited request cancellation: %v", err)
			}
			if retryable {
				t.Fatal("fence loss must fail the upload reservation")
			}
			cleaned = append([]string(nil), keys...)
			return nil
		},
	}
	inspector := &mediaInspectorStub{inspect: func(context.Context, MediaUpload, string) (InspectedUpload, error) {
		return InspectedUpload{
			ObjectKey:   "media/artwork/album_artwork/album-1/upload-1.jpg",
			CleanupKeys: []string{upload.ObjectKey},
		}, nil
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, inspector, fixedClock{now},
		ids("claim-generated", "asset-generated", "job-generated"))
	_, err := service.CompleteUpload(requestContext, "admin-1", "trace-12345678", upload.ID, CompleteUploadInput{
		CompletionFence: &completionFenceStub{},
	})
	if !errors.Is(err, fenceErr) {
		t.Fatalf("error = %v", err)
	}
	expected := []string{upload.ObjectKey, "media/artwork/album_artwork/album-1/upload-1.jpg"}
	if !reflect.DeepEqual(cleaned, expected) {
		t.Fatalf("cleanup keys = %#v, expected %#v", cleaned, expected)
	}
}

func TestCompleteUploadReleasesDependencyFailureForRetry(t *testing.T) {
	now := time.Now().UTC()
	upload := MediaUpload{
		ID: "upload-1", Purpose: PurposeAlbumArtwork, UploaderID: "admin-1",
		ObjectKey: "uploads/admin-1/upload-1", Status: UploadStatusCompleting,
	}
	var retryable bool
	var cleanup []string
	store := &mediaStoreStub{
		claimCompletion: func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
			return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: "claim-1"}, nil
		},
		failCompletion: func(_ context.Context, _, _ string, canRetry bool, keys []string, reason string, _ time.Time) error {
			retryable = canRetry
			cleanup = append([]string(nil), keys...)
			if reason != "UPLOAD_FAILED_DEPENDENCY_UNAVAILABLE" {
				t.Fatalf("reason = %q", reason)
			}
			return nil
		},
	}
	inspector := &mediaInspectorStub{inspect: func(context.Context, MediaUpload, string) (InspectedUpload, error) {
		return InspectedUpload{}, apperror.DependencyUnavailable("storage unavailable")
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, inspector, fixedClock{now}, ids("claim-generated"))
	_, err := service.CompleteUpload(context.Background(), "admin-1", "trace-12345678", "upload-1", CompleteUploadInput{})
	if !apperror.IsCode(err, apperror.CodeDependencyUnavailable) || !retryable ||
		!reflect.DeepEqual(cleanup, []string{upload.ObjectKey}) {
		t.Fatalf("error/retry/cleanup = %v / %v / %#v", err, retryable, cleanup)
	}
}

func TestCompleteUploadWithBatchFenceCleansDependencyFailureInsteadOfRetrying(t *testing.T) {
	now := time.Now().UTC()
	upload := MediaUpload{
		ID: "upload-1", Purpose: PurposeAlbumArtwork, UploaderID: "admin-1",
		ObjectKey: "uploads/admin-1/upload-1", Status: UploadStatusCompleting,
	}
	normalizedKey := "media/artwork/album_artwork/album-1/upload-1.jpg"
	var retryable bool
	var cleanup []string
	store := &mediaStoreStub{
		claimCompletion: func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
			return CompletionClaim{Outcome: CompletionClaimed, Upload: upload, Token: "claim-1"}, nil
		},
		failCompletion: func(_ context.Context, _, _ string, canRetry bool, keys []string, _ string, _ time.Time) error {
			retryable = canRetry
			cleanup = append([]string(nil), keys...)
			return nil
		},
	}
	inspector := &mediaInspectorStub{inspect: func(context.Context, MediaUpload, string) (InspectedUpload, error) {
		return InspectedUpload{}, &UploadInspectionFailure{
			Cause:       apperror.DependencyUnavailable("normalization storage unavailable"),
			CleanupKeys: []string{upload.ObjectKey, normalizedKey},
		}
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, inspector, fixedClock{now}, ids("claim-generated"))
	_, err := service.CompleteUpload(context.Background(), "admin-1", "trace-12345678", upload.ID, CompleteUploadInput{
		CompletionFence: &completionFenceStub{},
	})
	if !apperror.IsCode(err, apperror.CodeDependencyUnavailable) || retryable ||
		!reflect.DeepEqual(cleanup, []string{upload.ObjectKey, normalizedKey}) {
		t.Fatalf("error/retry/cleanup = %v / %v / %#v", err, retryable, cleanup)
	}
}

func TestCompleteUploadReturnsPersistedCompletionWithoutInspection(t *testing.T) {
	assetID := "asset-1"
	jobID := "job-1"
	inspected := false
	store := &mediaStoreStub{claimCompletion: func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error) {
		return CompletionClaim{Outcome: CompletionFinished, Upload: MediaUpload{
			ID: "upload-1", Status: UploadStatusCompleted, AssetID: &assetID, JobID: &jobID,
		}}, nil
	}}
	service := newMediaService(t, store, &mediaStorageStub{}, &mediaInspectorStub{inspect: func(context.Context, MediaUpload, string) (InspectedUpload, error) {
		inspected = true
		return InspectedUpload{}, nil
	}}, fixedClock{time.Now()}, ids("claim-generated"))
	result, err := service.CompleteUpload(context.Background(), "admin-1", "trace-12345678", "upload-1", CompleteUploadInput{})
	if err != nil || inspected || result.JobID == nil || *result.JobID != jobID {
		t.Fatalf("result/error/inspected = %#v / %v / %v", result, err, inspected)
	}
}

func TestJobPresentationAndRetryPreserveOptimisticVersion(t *testing.T) {
	now := time.Date(2026, 7, 16, 3, 4, 5, 987654000, time.UTC)
	lastError := "A newer upload superseded this media job"
	lastCode := "SUPERSEDED"
	var retry RetryJobParams
	store := &mediaStoreStub{
		findJob: func(context.Context, string) (MediaJob, error) {
			return MediaJob{
				ID: "job-1", Type: "INGEST_TRACK", Status: JobStatusCancelled,
				Attempts: 2, MaxAttempts: 5, CancelRequested: true,
				LastError: &lastError, LastErrorCode: &lastCode, NextAttemptAt: now,
				Version: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			}, nil
		},
		retryJob: func(_ context.Context, input RetryJobParams) (MediaJob, error) {
			retry = input
			return MediaJob{
				ID: input.JobID, Type: "INGEST_TRACK", Status: JobStatusPending,
				Attempts: 0, MaxAttempts: 5, NextAttemptAt: input.Now,
				Version: 4, CreatedAt: now.Add(-time.Hour), UpdatedAt: input.Now,
			}, nil
		},
	}
	service := newMediaService(t, store, &mediaStorageStub{}, &mediaInspectorStub{}, fixedClock{now}, ids())
	presented, err := service.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if presented.LastErrorMessage == nil || presented.NextAttemptAt != nil ||
		presented.UpdatedAt != "2026-07-16T03:04:05.987Z" {
		t.Fatalf("presented = %#v", presented)
	}
	retried, err := service.RetryJob(context.Background(), "admin-1", "trace-12345678", "job-1", RetryJobInput{
		ExpectedVersion: 3,
		Reason:          OptionalString{Set: true, Value: "operator retry"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.ExpectedVersion != 3 || retry.Reason == nil || *retry.Reason != "operator retry" ||
		retried.Status != JobStatusPending || retried.NextAttemptAt == nil {
		t.Fatalf("retry/result = %#v / %#v", retry, retried)
	}
}

func newMediaService(
	t *testing.T,
	store Store,
	storage ObjectStorage,
	inspector UploadInspector,
	clock Clock,
	idGenerator func() string,
) *Service {
	t.Helper()
	service, err := NewService(config.Config{
		Storage: config.Storage{SignedURLTTLSeconds: 300, MaxUploadBytes: 1024 * 1024 * 1024},
		Media:   config.Media{FFprobePath: "ffprobe", FFmpegPath: "ffmpeg"},
	}, ServiceDependencies{
		Repository: store, Storage: storage, Inspector: inspector,
		Clock: clock, IDGenerator: idGenerator,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func validImageUpload() CreateUploadInput {
	return CreateUploadInput{
		Purpose:        PurposeArtistArtwork,
		TargetID:       "00000000-0000-0000-0000-000000000001",
		FileName:       "art.png",
		ContentType:    "image/png",
		SizeBytes:      128,
		ChecksumSHA256: stringOf('a', 64),
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	imageValue := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			imageValue.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 50), G: uint8(y * 70), B: 160, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, imageValue); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func stringOf(value byte, count int) string { return string(bytes.Repeat([]byte{value}, count)) }

func ids(values ...string) func() string {
	index := 0
	return func() string {
		if index >= len(values) {
			return "generated-id"
		}
		value := values[index]
		index++
		return value
	}
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type completionFenceStub struct {
	lock func(context.Context, pgx.Tx) error
}

func (stub *completionFenceStub) Lock(ctx context.Context, tx pgx.Tx) error {
	if stub.lock == nil {
		return nil
	}
	return stub.lock(ctx, tx)
}

type mediaStoreStub struct {
	createUpload         func(context.Context, CreateUploadParams) (MediaUpload, error)
	markUploadFailed     func(context.Context, string, string) error
	abandonUpload        func(context.Context, string, string, time.Time) error
	findUploadForContent func(context.Context, string, string) (MediaUpload, error)
	claimCompletion      func(context.Context, string, string, string, time.Time, time.Duration) (CompletionClaim, error)
	completionStatus     func(context.Context, string, string) (MediaUpload, error)
	finalizeCompletion   func(context.Context, FinalizeCompletionParams) (CompletedUpload, error)
	failCompletion       func(context.Context, string, string, bool, []string, string, time.Time) error
	findJob              func(context.Context, string) (MediaJob, error)
	retryJob             func(context.Context, RetryJobParams) (MediaJob, error)
	writeAudit           func(context.Context, AuditWrite) error
}

func (stub *mediaStoreStub) CreateUpload(ctx context.Context, input CreateUploadParams) (MediaUpload, error) {
	if stub.createUpload == nil {
		return MediaUpload{}, errors.New("unexpected CreateUpload call")
	}
	return stub.createUpload(ctx, input)
}
func (stub *mediaStoreStub) MarkUploadFailed(ctx context.Context, actorID, uploadID string) error {
	if stub.markUploadFailed == nil {
		return errors.New("unexpected MarkUploadFailed call")
	}
	return stub.markUploadFailed(ctx, actorID, uploadID)
}
func (stub *mediaStoreStub) AbandonUpload(ctx context.Context, actorID, uploadID string, now time.Time) error {
	if stub.abandonUpload == nil {
		return errors.New("unexpected AbandonUpload call")
	}
	return stub.abandonUpload(ctx, actorID, uploadID, now)
}
func (stub *mediaStoreStub) FindUploadForContent(ctx context.Context, actorID, uploadID string) (MediaUpload, error) {
	if stub.findUploadForContent == nil {
		return MediaUpload{}, errors.New("unexpected FindUploadForContent call")
	}
	return stub.findUploadForContent(ctx, actorID, uploadID)
}
func (stub *mediaStoreStub) ClaimCompletion(ctx context.Context, actorID, uploadID, token string, now time.Time, lease time.Duration) (CompletionClaim, error) {
	if stub.claimCompletion == nil {
		return CompletionClaim{}, errors.New("unexpected ClaimCompletion call")
	}
	return stub.claimCompletion(ctx, actorID, uploadID, token, now, lease)
}
func (stub *mediaStoreStub) CompletionStatus(ctx context.Context, actorID, uploadID string) (MediaUpload, error) {
	if stub.completionStatus == nil {
		return MediaUpload{}, errors.New("unexpected CompletionStatus call")
	}
	return stub.completionStatus(ctx, actorID, uploadID)
}
func (stub *mediaStoreStub) FinalizeCompletion(ctx context.Context, input FinalizeCompletionParams) (CompletedUpload, error) {
	if stub.finalizeCompletion == nil {
		return CompletedUpload{}, errors.New("unexpected FinalizeCompletion call")
	}
	return stub.finalizeCompletion(ctx, input)
}
func (stub *mediaStoreStub) FailCompletion(ctx context.Context, uploadID, token string, retryable bool, keys []string, reason string, now time.Time) error {
	if stub.failCompletion == nil {
		return errors.New("unexpected FailCompletion call")
	}
	return stub.failCompletion(ctx, uploadID, token, retryable, keys, reason, now)
}
func (stub *mediaStoreStub) FindJob(ctx context.Context, jobID string) (MediaJob, error) {
	if stub.findJob == nil {
		return MediaJob{}, errors.New("unexpected FindJob call")
	}
	return stub.findJob(ctx, jobID)
}
func (stub *mediaStoreStub) RetryJob(ctx context.Context, input RetryJobParams) (MediaJob, error) {
	if stub.retryJob == nil {
		return MediaJob{}, errors.New("unexpected RetryJob call")
	}
	return stub.retryJob(ctx, input)
}
func (stub *mediaStoreStub) WriteAudit(ctx context.Context, input AuditWrite) error {
	if stub.writeAudit == nil {
		return errors.New("unexpected WriteAudit call")
	}
	return stub.writeAudit(ctx, input)
}

type mediaStorageStub struct {
	createURL      func(context.Context, UploadURLRequest) (string, error)
	downloadToFile func(context.Context, string, string, int64) (StoredObject, error)
	uploadFile     func(context.Context, string, string, string, string) (int64, error)
}

func (stub *mediaStorageStub) CreateUploadURL(ctx context.Context, input UploadURLRequest) (string, error) {
	if stub.createURL == nil {
		return "", errors.New("unexpected CreateUploadURL call")
	}
	return stub.createURL(ctx, input)
}
func (stub *mediaStorageStub) DownloadToFile(ctx context.Context, key, path string, maximum int64) (StoredObject, error) {
	if stub.downloadToFile == nil {
		return StoredObject{}, errors.New("unexpected DownloadToFile call")
	}
	return stub.downloadToFile(ctx, key, path, maximum)
}
func (stub *mediaStorageStub) UploadFile(ctx context.Context, key, path, contentType, checksum string) (int64, error) {
	if stub.uploadFile == nil {
		return 0, errors.New("unexpected UploadFile call")
	}
	return stub.uploadFile(ctx, key, path, contentType, checksum)
}

type mediaInspectorStub struct {
	inspect func(context.Context, MediaUpload, string) (InspectedUpload, error)
}

func (stub *mediaInspectorStub) Inspect(ctx context.Context, upload MediaUpload, etag string) (InspectedUpload, error) {
	if stub.inspect == nil {
		return InspectedUpload{}, errors.New("unexpected Inspect call")
	}
	return stub.inspect(ctx, upload, etag)
}

var _ io.Reader = (*bytes.Reader)(nil)
