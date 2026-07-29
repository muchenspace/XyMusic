package admintagscraping

import (
	"context"
	"errors"
	"io"
	"testing"

	"xymusic/server/internal/modules/adminmedia"
)

func TestAdminMediaArtworkApplierCompletesWithBoundedExecutionFence(t *testing.T) {
	requestContext := context.Background()
	fence := &BatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}
	applyContext := withBatchMutationFence(requestContext, fence)
	media := &artworkMediaStub{
		createUpload: func(context.Context, string, string, adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, error) {
			return adminmedia.UploadReservationDTO{ID: "upload-1"}, nil
		},
		uploadContent: func(context.Context, string, string, string, int64, io.Reader) error {
			return nil
		},
		completeUpload: func(ctx context.Context, _, _, uploadID string, input adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, error) {
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
			return adminmedia.UploadCompletionDTO{UploadID: uploadID}, nil
		},
	}
	adapter, err := NewAdminMediaArtworkApplier(media)
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.ApplyAlbumArtwork(
		applyContext,
		"admin-1",
		"trace-12345678",
		"album-1",
		DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdminMediaArtworkApplierCancellationDuringCompletionPreventsAttachAndCleansUp(t *testing.T) {
	requestContext, cancelRequest := context.WithCancel(context.Background())
	attached := false
	abandoned := false
	media := &artworkMediaStub{
		createUpload: func(context.Context, string, string, adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, error) {
			return adminmedia.UploadReservationDTO{ID: "upload-1"}, nil
		},
		uploadContent: func(context.Context, string, string, string, int64, io.Reader) error {
			return nil
		},
		completeUpload: func(ctx context.Context, _, _, uploadID string, input adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, error) {
			if err := ctx.Err(); err != nil {
				t.Fatalf("completion inherited request cancellation: %v", err)
			}
			cancelRequest()
			if err := input.CompletionFence.Lock(ctx, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("execution fence error = %v", err)
			} else {
				return adminmedia.UploadCompletionDTO{}, err
			}
			attached = true
			return adminmedia.UploadCompletionDTO{UploadID: uploadID}, nil
		},
		abandonUpload: func(ctx context.Context, _, _ string) error {
			if err := ctx.Err(); err != nil {
				t.Fatalf("cleanup inherited request cancellation: %v", err)
			}
			abandoned = true
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
		"trace-12345678",
		"album-1",
		DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
	)
	if !errors.Is(err, context.Canceled) || attached || !abandoned {
		t.Fatalf("error/attached/abandoned = %v / %v / %v", err, attached, abandoned)
	}
}

func TestAdminMediaArtworkApplierAbandonsReservationAfterUploadOrCompletionFailure(t *testing.T) {
	tests := []struct {
		name            string
		uploadError     error
		completionError error
	}{
		{name: "upload", uploadError: errors.New("upload failed")},
		{name: "completion", completionError: errors.New("completion failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestContext, cancelRequest := context.WithCancel(context.Background())
			abandoned := false
			media := &artworkMediaStub{
				createUpload: func(context.Context, string, string, adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, error) {
					return adminmedia.UploadReservationDTO{ID: "upload-1"}, nil
				},
				uploadContent: func(context.Context, string, string, string, int64, io.Reader) error {
					if test.uploadError != nil {
						cancelRequest()
					}
					return test.uploadError
				},
				completeUpload: func(context.Context, string, string, string, adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, error) {
					cancelRequest()
					return adminmedia.UploadCompletionDTO{}, test.completionError
				},
				abandonUpload: func(ctx context.Context, actorID, uploadID string) error {
					if err := ctx.Err(); err != nil {
						t.Fatalf("abandon inherited request cancellation: %v", err)
					}
					if _, hasDeadline := ctx.Deadline(); !hasDeadline {
						t.Fatal("abandon context is not bounded")
					}
					if actorID != "admin-1" || uploadID != "upload-1" {
						t.Fatalf("abandon args = %q / %q", actorID, uploadID)
					}
					abandoned = true
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
				"trace-12345678",
				"album-1",
				DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
			)
			expected := test.uploadError
			if expected == nil {
				expected = test.completionError
			}
			if !errors.Is(err, expected) || !abandoned {
				t.Fatalf("error/abandoned = %v / %v", err, abandoned)
			}
		})
	}
}

func TestAdminMediaArtworkApplierBuildsArtistUploadWithAtomicCompletionFence(t *testing.T) {
	candidate := ArtistCandidate{
		Source: SourceQMusic, ID: "qq-artist", Name: "Artist",
		ImageURL: "https://y.qq.com/music/photo_new/T001R500x500M000qq-artist.jpg",
		Aliases:  []string{}, Score: 2,
	}
	mutationFence := &BatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}
	applyContext := withArtistArtworkDetails(
		withArtworkMutationFence(context.Background(), mutationFence), "operator scrape", candidate,
	)
	media := &artworkMediaStub{
		createUpload: func(_ context.Context, actorID, traceID string, input adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, error) {
			if actorID != "admin-1" || traceID != "trace-12345678" ||
				input.Purpose != adminmedia.PurposeArtistArtwork || input.TargetID != "artist-1" ||
				input.FileName != "scraped-artist.png" || input.ContentType != "image/png" || input.SizeBytes != 5 {
				t.Fatalf("artist upload input = %#v", input)
			}
			return adminmedia.UploadReservationDTO{ID: "upload-1"}, nil
		},
		uploadContent: func(context.Context, string, string, string, int64, io.Reader) error { return nil },
		completeUpload: func(ctx context.Context, _, _, uploadID string, input adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, error) {
			if uploadID != "upload-1" {
				t.Fatalf("upload ID = %q", uploadID)
			}
			fence, ok := input.CompletionFence.(*artistArtworkCompletionFence)
			if !ok || fence.artistID != "artist-1" || fence.expectedVersion != 3 || !fence.overwrite ||
				fence.actorID != "admin-1" || fence.traceID != "trace-12345678" ||
				fence.reason != "operator scrape" || fence.candidate.ID != "qq-artist" ||
				fence.mutationFence != mutationFence {
				t.Fatalf("artist completion fence = %#v", input.CompletionFence)
			}
			if ctx.Err() != nil {
				t.Fatalf("completion context error = %v", ctx.Err())
			}
			return adminmedia.UploadCompletionDTO{UploadID: uploadID}, nil
		},
	}
	adapter, err := NewAdminMediaArtworkApplier(media)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.ApplyArtistArtwork(
		applyContext, "admin-1", "trace-12345678", "artist-1", 3, true,
		DownloadedArtwork{Bytes: []byte("image"), ContentType: "image/png", Extension: "png"},
	); err != nil {
		t.Fatal(err)
	}
}

type artworkMediaStub struct {
	createUpload   func(context.Context, string, string, adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, error)
	uploadContent  func(context.Context, string, string, string, int64, io.Reader) error
	completeUpload func(context.Context, string, string, string, adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, error)
	abandonUpload  func(context.Context, string, string) error
}

func (stub *artworkMediaStub) CreateUpload(ctx context.Context, actorID, traceID string, input adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, error) {
	return stub.createUpload(ctx, actorID, traceID, input)
}

func (stub *artworkMediaStub) UploadContent(ctx context.Context, actorID, uploadID, contentType string, contentLength int64, body io.Reader) error {
	return stub.uploadContent(ctx, actorID, uploadID, contentType, contentLength, body)
}

func (stub *artworkMediaStub) CompleteUpload(ctx context.Context, actorID, traceID, uploadID string, input adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, error) {
	return stub.completeUpload(ctx, actorID, traceID, uploadID, input)
}

func (stub *artworkMediaStub) AbandonUpload(ctx context.Context, actorID, uploadID string) error {
	if stub.abandonUpload == nil {
		return errors.New("unexpected AbandonUpload call")
	}
	return stub.abandonUpload(ctx, actorID, uploadID)
}
