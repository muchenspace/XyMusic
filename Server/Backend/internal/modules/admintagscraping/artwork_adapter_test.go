package admintagscraping

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestAdminMediaArtworkApplierCompletesWithBoundedExecutionFence(t *testing.T) {
	requestContext := context.Background()
	fence := &BatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}
	applyContext := withBatchMutationFence(requestContext, fence)
	media := &artworkMediaStub{
		createUpload: func(_ context.Context, _ string, key string, _ MediaCreateUploadInput) (MediaUploadReservation, bool, error) {
			if len(key) < 8 {
				t.Fatalf("create idempotency key = %q", key)
			}
			return MediaUploadReservation{ID: "upload-1"}, false, nil
		},
		uploadDirect: func(context.Context, string, io.Reader, int64) error {
			return nil
		},
		completeUpload: func(ctx context.Context, _, uploadID string, key string, input MediaCompleteUploadInput) (MediaUploadCompletion, bool, error) {
			if len(key) < 8 {
				t.Fatalf("complete idempotency key = %q", key)
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("completion inherited request cancellation: %v", err)
			}
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				t.Fatal("completion context is not bounded")
			}
			executionFence, ok := input.CompletionFence.(*artworkCompletionFence)
			if uploadID != "upload-1" || !ok || executionFence.mutationFence != fence ||
				executionFence.executionContext != applyContext {
				t.Fatalf("completion input = %q / %#v", uploadID, input.CompletionFence)
			}
			return MediaUploadCompletion{UploadID: uploadID}, false, nil
		},
	}
	adapter, err := NewAdminMediaArtworkApplier(media)
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.ApplyAlbumArtwork(
		applyContext,
		"admin-1",
		"album-1",
		DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestArtworkCompletionFenceAllowsDirectApplyWithoutBatchFence(t *testing.T) {
	if err := (&artworkCompletionFence{executionContext: context.Background()}).Lock(context.Background(), nil); err != nil {
		t.Fatalf("direct artwork completion fence error = %v", err)
	}
}

func TestAdminMediaArtworkApplierCancellationDuringCompletionPreventsAttachAndCleansUp(t *testing.T) {
	requestContext, cancelRequest := context.WithCancel(context.Background())
	abandoned := false
	media := &artworkMediaStub{
		createUpload: func(_ context.Context, _ string, key string, _ MediaCreateUploadInput) (MediaUploadReservation, bool, error) {
			if len(key) < 8 {
				t.Fatalf("create idempotency key = %q", key)
			}
			return MediaUploadReservation{ID: "upload-1"}, false, nil
		},
		uploadDirect: func(context.Context, string, io.Reader, int64) error {
			return nil
		},
		completeUpload: func(ctx context.Context, _, uploadID string, key string, input MediaCompleteUploadInput) (MediaUploadCompletion, bool, error) {
			if len(key) < 8 {
				t.Fatalf("complete idempotency key = %q", key)
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("completion inherited request cancellation: %v", err)
			}
			cancelRequest()
			executionFence, ok := input.CompletionFence.(*artworkCompletionFence)
			if !ok {
				t.Fatal("completion fence missing")
			}
			if err := executionFence.Lock(ctx, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("fence lock error = %v, want context.Canceled", err)
			}
			return MediaUploadCompletion{}, false, errors.New("synthetic fence failure")
		},
		abandonUpload: func(ctx context.Context, _, uploadID string) error {
			if uploadID == "upload-1" {
				abandoned = true
			}
			return nil
		},
	}
	adapter, err := NewAdminMediaArtworkApplier(media)
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.ApplyAlbumArtwork(
		requestContext,
		"admin-1",
		"album-1",
		DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
	)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !abandoned {
		t.Fatal("upload was not abandoned")
	}
}

func TestAdminMediaArtworkApplierArtistScrape(t *testing.T) {
	mutationFence := &BatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}
	applyContext := withBatchMutationFence(context.Background(), mutationFence)
	candidate := ArtistCandidate{
		Source:   SourceQMusic,
		ID:       "qq-artist",
		Name:     "Artist Name",
		ImageURL: "https://y.qq.com/music/photo_new/T001R300x300M0000000000000.jpg",
		Score:    1.0,
	}
	applyContext = withArtistArtworkDetails(applyContext, "operator scrape", candidate)
	media := &artworkMediaStub{
		createUpload: func(_ context.Context, actorID string, key string, input MediaCreateUploadInput) (MediaUploadReservation, bool, error) {
			if len(key) < 8 {
				t.Fatalf("artist create idempotency key = %q", key)
			}
			if actorID != "admin-1" || input.Purpose != MediaPurposeArtistArtwork || input.TargetID != "artist-1" ||
				input.FileName != "scraped-artist.png" || input.ContentType != "image/png" || input.SizeBytes != 5 {
				t.Fatalf("artist upload input = %#v", input)
			}
			return MediaUploadReservation{ID: "upload-1"}, false, nil
		},
		uploadDirect: func(context.Context, string, io.Reader, int64) error { return nil },
		completeUpload: func(ctx context.Context, _, uploadID string, key string, input MediaCompleteUploadInput) (MediaUploadCompletion, bool, error) {
			if len(key) < 8 {
				t.Fatalf("artist complete idempotency key = %q", key)
			}
			if uploadID != "upload-1" {
				t.Fatalf("upload ID = %q", uploadID)
			}
			fence, ok := input.CompletionFence.(*artistArtworkCompletionFence)
			if !ok || fence.artistID != "artist-1" || fence.expectedVersion != 3 || !fence.overwrite ||
				fence.reason != "operator scrape" || fence.candidate.ID != "qq-artist" ||
				fence.mutationFence != mutationFence {
				t.Fatalf("artist completion fence = %#v", input.CompletionFence)
			}
			if ctx.Err() != nil {
				t.Fatalf("completion context error = %v", ctx.Err())
			}
			return MediaUploadCompletion{UploadID: uploadID}, false, nil
		},
	}
	adapter, err := NewAdminMediaArtworkApplier(media)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.ApplyArtistArtwork(
		applyContext, "admin-1", "artist-1", 3, true,
		DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
	); err != nil {
		t.Fatal(err)
	}
}

type artworkMediaStub struct {
	createUpload   func(context.Context, string, string, MediaCreateUploadInput) (MediaUploadReservation, bool, error)
	uploadDirect   func(context.Context, string, io.Reader, int64) error
	completeUpload func(context.Context, string, string, string, MediaCompleteUploadInput) (MediaUploadCompletion, bool, error)
	abandonUpload  func(context.Context, string, string) error
}

func (stub *artworkMediaStub) CreateUpload(ctx context.Context, actorID string, key string, input MediaCreateUploadInput) (MediaUploadReservation, bool, error) {
	return stub.createUpload(ctx, actorID, key, input)
}

func (stub *artworkMediaStub) UploadDirect(ctx context.Context, uploadID string, body io.Reader, contentLength int64) error {
	return stub.uploadDirect(ctx, uploadID, body, contentLength)
}

func (stub *artworkMediaStub) CompleteUpload(ctx context.Context, actorID, uploadID string, key string, input MediaCompleteUploadInput) (MediaUploadCompletion, bool, error) {
	return stub.completeUpload(ctx, actorID, uploadID, key, input)
}

func (stub *artworkMediaStub) AbandonUpload(ctx context.Context, actorID, uploadID string) error {
	if stub.abandonUpload == nil {
		return errors.New("unexpected AbandonUpload call")
	}
	return stub.abandonUpload(ctx, actorID, uploadID)
}
