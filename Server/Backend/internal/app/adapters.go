package app

import (
	"context"
	"io"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminmedia"
	"xymusic/server/internal/modules/admintagscraping"
	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/modules/playlist"
	"xymusic/server/internal/shared/apperror"
)

// This file holds the composition-root adapters that translate between module
// contracts. They intentionally contain no behavior of their own.

type adminArtworkPresenter struct {
	delegate interface {
		Artworks(context.Context, []string) (map[string]playlist.ArtworkDTO, error)
	}
}

// adminTagScrapingMediaAdapter lets the tag scraping module depend on its own
// media upload contract while the composition root delegates to adminmedia.
type adminTagScrapingMediaAdapter struct {
	media *adminmedia.Service
}

func (adapter adminTagScrapingMediaAdapter) CreateUpload(
	ctx context.Context,
	actorID string,
	idempotencyKey string,
	input admintagscraping.MediaCreateUploadInput,
) (admintagscraping.MediaUploadReservation, bool, error) {
	reservation, replayed, err := adapter.media.CreateUpload(ctx, actorID, idempotencyKey, adminmedia.CreateUploadInput{
		Purpose:        adminmedia.UploadPurpose(input.Purpose),
		TargetID:       input.TargetID,
		FileName:       input.FileName,
		ContentType:    input.ContentType,
		SizeBytes:      input.SizeBytes,
		ChecksumSHA256: input.ChecksumSHA256,
	})
	if err != nil {
		return admintagscraping.MediaUploadReservation{}, false, err
	}
	return admintagscraping.MediaUploadReservation{ID: reservation.ID}, replayed, nil
}

func (adapter adminTagScrapingMediaAdapter) UploadDirect(
	ctx context.Context,
	uploadID string,
	body io.Reader,
	contentLength int64,
) error {
	return adapter.media.UploadDirect(ctx, uploadID, body, contentLength)
}

func (adapter adminTagScrapingMediaAdapter) CompleteUpload(
	ctx context.Context,
	actorID string,
	uploadID string,
	idempotencyKey string,
	input admintagscraping.MediaCompleteUploadInput,
) (admintagscraping.MediaUploadCompletion, bool, error) {
	completion, replayed, err := adapter.media.CompleteUpload(ctx, actorID, uploadID, idempotencyKey, adminmedia.CompleteUploadInput{
		CompletionFence: adminTagScrapingFenceAdapter{delegate: input.CompletionFence},
	})
	if err != nil {
		return admintagscraping.MediaUploadCompletion{}, false, err
	}
	return admintagscraping.MediaUploadCompletion{
		UploadID: completion.UploadID,
		AssetID:  completion.AssetID,
	}, replayed, nil
}

func (adapter adminTagScrapingMediaAdapter) AbandonUpload(
	ctx context.Context,
	actorID string,
	uploadID string,
) error {
	return adapter.media.AbandonUpload(ctx, actorID, uploadID)
}

// adminTagScrapingFenceAdapter translates the adminmedia transaction contract
// back into the tag scraping module's narrow transaction contract.
type adminTagScrapingFenceAdapter struct {
	delegate admintagscraping.MediaCompletionFence
}

func (adapter adminTagScrapingFenceAdapter) Lock(ctx context.Context, tx adminmedia.Tx) error {
	if adapter.delegate == nil {
		return nil
	}
	return adapter.delegate.Lock(ctx, adminTagScrapingTxAdapter{tx: tx})
}

type adminTagScrapingTxAdapter struct {
	tx adminmedia.Tx
}

func (adapter adminTagScrapingTxAdapter) QueryRow(
	ctx context.Context,
	sql string,
	args ...any,
) admintagscraping.MediaRow {
	return adapter.tx.QueryRow(ctx, sql, args...)
}

func (adapter adminTagScrapingTxAdapter) Exec(
	ctx context.Context,
	sql string,
	args ...any,
) (admintagscraping.MediaCommandTag, error) {
	return adapter.tx.Exec(ctx, sql, args...)
}

func (presenter adminArtworkPresenter) Artworks(
	ctx context.Context,
	assetIDs []string,
) (map[string]catalog.ArtworkDTO, error) {
	items, err := presenter.delegate.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]catalog.ArtworkDTO, len(items))
	for assetID, item := range items {
		result[assetID] = catalog.ArtworkDTO(item)
	}
	return result, nil
}

type playbackIdentityAdapter struct {
	identity *identity.Service
}

func (a *playbackIdentityAdapter) CurrentUserID(c *gin.Context) (string, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", apperror.Unauthorized(apperror.CodeAuthenticationRequired, "Authentication is required")
	}
	actor, err := a.identity.Authenticate(c.Request.Context(), authHeader)
	if err != nil {
		return "", err
	}
	return actor.UserID, nil
}
