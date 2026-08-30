package admintagscraping

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/modules/adminmedia"
)

type AdminMediaAPI interface {
	CreateUpload(context.Context, string, string, adminmedia.CreateUploadInput) (adminmedia.UploadReservationDTO, bool, error)
	UploadDirect(context.Context, string, io.Reader, int64) error
	CompleteUpload(context.Context, string, string, string, adminmedia.CompleteUploadInput) (adminmedia.UploadCompletionDTO, bool, error)
	AbandonUpload(context.Context, string, string) error
}

const artworkCompletionTimeout = 2 * time.Minute

type AdminMediaArtworkApplier struct {
	media AdminMediaAPI
}

var _ AdminMediaAPI = (*adminmedia.Service)(nil)
var _ ArtworkApplier = (*AdminMediaArtworkApplier)(nil)

func NewAdminMediaArtworkApplier(media AdminMediaAPI) (*AdminMediaArtworkApplier, error) {
	if media == nil {
		return nil, errors.New("admin media service is required for scraped artwork")
	}
	return &AdminMediaArtworkApplier{media: media}, nil
}

func (adapter *AdminMediaArtworkApplier) ApplyAlbumArtwork(
	ctx context.Context,
	actorID string,
	albumID string,
	artwork DownloadedArtwork,
) error {
	digest := sha256.Sum256(artwork.Bytes)
	upload, _, err := adapter.media.CreateUpload(ctx, actorID, uuid.NewString(), adminmedia.CreateUploadInput{
		Purpose:        adminmedia.PurposeAlbumArtwork,
		TargetID:       albumID,
		FileName:       "scraped-cover." + artwork.Extension,
		ContentType:    artwork.ContentType,
		SizeBytes:      int64(len(artwork.Bytes)),
		ChecksumSHA256: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return err
	}
	if err := adapter.media.UploadDirect(
		ctx, upload.ID, bytes.NewReader(artwork.Bytes), int64(len(artwork.Bytes)),
	); err != nil {
		return adapter.abandonAfterFailure(ctx, actorID, upload.ID, err)
	}
	completionContext, cancelCompletion := artworkFollowupContext(ctx)
	_, _, err = adapter.media.CompleteUpload(
		completionContext,
		actorID,
		upload.ID,
		uuid.NewString(),
		adminmedia.CompleteUploadInput{CompletionFence: &artworkCompletionFence{
			executionContext: ctx,
			mutationFence:    completionMutationFenceFromContext(ctx),
		}},
	)
	cancelCompletion()
	if err != nil {
		return adapter.abandonAfterFailure(ctx, actorID, upload.ID, err)
	}
	return nil
}

func (adapter *AdminMediaArtworkApplier) abandonAfterFailure(
	ctx context.Context,
	actorID string,
	uploadID string,
	cause error,
) error {
	cleanupContext, cancelCleanup := artworkFollowupContext(ctx)
	defer cancelCleanup()
	if cleanupErr := adapter.media.AbandonUpload(cleanupContext, actorID, uploadID); cleanupErr != nil {
		return errors.Join(cause, cleanupErr)
	}
	return cause
}

func artworkFollowupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), artworkCompletionTimeout)
}

type artworkCompletionFence struct {
	executionContext context.Context
	mutationFence    artworkMutationFence
}

func (fence *artworkCompletionFence) Lock(ctx context.Context, tx pgx.Tx) error {
	if fence == nil {
		return nil
	}
	if fence.executionContext != nil {
		if err := fence.executionContext.Err(); err != nil {
			return err
		}
	}
	if fence.mutationFence == nil {
		return nil
	}
	return fence.mutationFence.Lock(ctx, tx)
}
