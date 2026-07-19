package admintagscraping

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/modules/adminmedia"
	"xymusic/server/internal/shared/apperror"
)

func (adapter *AdminMediaArtworkApplier) ApplyArtistArtwork(
	ctx context.Context,
	actorID string,
	traceID string,
	artistID string,
	expectedVersion int,
	overwrite bool,
	artwork DownloadedArtwork,
) error {
	details := artistArtworkDetailsFromContext(ctx)
	details.reason = normalizeText(details.reason)
	if expectedVersion < 1 || javascriptLength(details.reason) < 2 || javascriptLength(details.reason) > 500 ||
		validateArtistCandidate(details.candidate) != nil {
		return apperror.Validation("Artist artwork apply context is invalid")
	}
	digest := sha256.Sum256(artwork.Bytes)
	upload, err := adapter.media.CreateUpload(ctx, actorID, traceID, adminmedia.CreateUploadInput{
		Purpose:        adminmedia.PurposeArtistArtwork,
		TargetID:       artistID,
		FileName:       "scraped-artist." + artwork.Extension,
		ContentType:    artwork.ContentType,
		SizeBytes:      int64(len(artwork.Bytes)),
		ChecksumSHA256: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return err
	}
	if err := adapter.media.UploadContent(
		ctx,
		actorID,
		upload.ID,
		artwork.ContentType,
		int64(len(artwork.Bytes)),
		bytes.NewReader(artwork.Bytes),
	); err != nil {
		return adapter.abandonAfterFailure(ctx, actorID, upload.ID, err)
	}
	completionContext, cancelCompletion := artworkFollowupContext(ctx)
	_, err = adapter.media.CompleteUpload(
		completionContext,
		actorID,
		traceID,
		upload.ID,
		adminmedia.CompleteUploadInput{CompletionFence: &artistArtworkCompletionFence{
			executionContext: ctx,
			mutationFence:    completionMutationFenceFromContext(ctx),
			actorID:          actorID,
			traceID:          traceID,
			artistID:         artistID,
			expectedVersion:  expectedVersion,
			overwrite:        overwrite,
			reason:           details.reason,
			candidate:        details.candidate,
		}},
	)
	cancelCompletion()
	if err != nil {
		return adapter.abandonAfterFailure(ctx, actorID, upload.ID, err)
	}
	return nil
}

type artistArtworkCompletionFence struct {
	executionContext context.Context
	mutationFence    artworkMutationFence
	actorID          string
	traceID          string
	artistID         string
	expectedVersion  int
	overwrite        bool
	reason           string
	candidate        ArtistCandidate
}

func (fence *artistArtworkCompletionFence) Lock(ctx context.Context, tx pgx.Tx) error {
	if fence == nil {
		return nil
	}
	if fence.executionContext != nil {
		if err := fence.executionContext.Err(); err != nil {
			return err
		}
	}
	if fence.mutationFence != nil {
		if err := fence.mutationFence.Lock(ctx, tx); err != nil {
			return err
		}
	}
	if fence.artistID == "" || fence.expectedVersion < 1 {
		return apperror.Validation("Artist artwork completion fence is invalid")
	}
	var version int
	var artworkAssetID *string
	err := tx.QueryRow(ctx, `
		SELECT version, artwork_asset_id::text
		FROM artists
		WHERE id = $1
		FOR UPDATE`, fence.artistID).Scan(&version, &artworkAssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Artist was not found")
	}
	if err != nil {
		return fmt.Errorf("lock artist artwork target: %w", err)
	}
	if version != fence.expectedVersion {
		return apperror.Conflict(apperror.CodeVersionConflict, "Artist version is stale", map[string]any{
			"expectedVersion": fence.expectedVersion,
			"currentVersion":  version,
		})
	}
	if artworkAssetID != nil && !fence.overwrite {
		return apperror.Conflict(
			apperror.CodeResourceConflict,
			"Artist already has artwork; enable overwrite to replace it",
			map[string]any{"artistId": fence.artistID},
		)
	}
	details, err := json.Marshal(map[string]any{
		"provider":   fence.candidate.Source,
		"externalId": fence.candidate.ID,
		"reason":     fence.reason,
		"overwrite":  fence.overwrite,
	})
	if err != nil {
		return fmt.Errorf("encode artist artwork scrape audit: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (actor_id, action, target_type, target_id, result, trace_id, details)
		VALUES ($1, 'ARTIST_ARTWORK_SCRAPED', 'artist', $2, 'SUCCESS', $3, $4::jsonb)`,
		fence.actorID, fence.artistID, fence.traceID, details); err != nil {
		return fmt.Errorf("audit artist artwork scrape: %w", err)
	}
	if successFence, ok := fence.mutationFence.(artistArtworkSuccessFence); ok {
		if err := successFence.CommitSuccess(ctx, tx, fence.candidate); err != nil {
			return err
		}
	}
	return nil
}

var _ adminmedia.CompletionFence = (*artistArtworkCompletionFence)(nil)
