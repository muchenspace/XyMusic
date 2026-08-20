package adminmutation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

func (repository *Repository) ArchiveTracksBatch(
	ctx context.Context,
	actorID string,
	traceID string,
	input []BatchTrackItemInput,
) ([]BatchArchiveItemRecord, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin batch track archive: %w", err)
	}
	defer tx.Rollback(ctx)

	items := append([]BatchTrackItemInput(nil), input...)
	sort.Slice(items, func(left, right int) bool { return items[left].TrackID < items[right].TrackID })
	prepared := make([]BatchTrackItemInput, 0, len(items))
	for _, item := range items {
		state, err := lockTrackMutationState(ctx, tx, item.TrackID)
		if err != nil {
			return nil, err
		}
		if state.Version != item.ExpectedVersion {
			return nil, versionConflict("Track", item.ExpectedVersion, state.Version, map[string]any{"trackId": item.TrackID})
		}
		if state.Status == "ARCHIVED" {
			return nil, apperror.New(
				apperror.CodeInvalidStateTransition,
				"Track is already archived",
				apperror.WithMetadata(map[string]any{"trackId": item.TrackID}),
			)
		}
		prepared = append(prepared, item)
	}

	for _, item := range prepared {
		if err := cancelTrackWritebacksForArchive(ctx, tx, item.TrackID); err != nil {
			return nil, err
		}
	}

	batchID := uuid.NewString()
	now := time.Now().UTC()
	versions := make(map[string]int, len(prepared))
	for _, item := range prepared {
		var version int
		err := tx.QueryRow(ctx, `UPDATE tracks
			SET status='ARCHIVED',version=version+1,updated_at=$3
			WHERE id=$1 AND version=$2 AND status<>'ARCHIVED'
			RETURNING version`, item.TrackID, item.ExpectedVersion, now).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.Conflict(
				apperror.CodeVersionConflict,
				"Track changed during the batch archive operation",
				map[string]any{"trackId": item.TrackID},
			)
		}
		if err != nil {
			return nil, fmt.Errorf("archive batch track: %w", err)
		}
		if err := writeAudit(ctx, tx, actorID, "admin.track.archive", "track", item.TrackID, traceID, map[string]any{
			"batchId": batchID, "batchSize": len(prepared),
		}); err != nil {
			return nil, err
		}
		versions[item.TrackID] = version
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit batch track archive: %w", err)
	}
	result := make([]BatchArchiveItemRecord, 0, len(input))
	for _, item := range input {
		result = append(result, BatchArchiveItemRecord{
			TrackID: item.TrackID, Status: "ARCHIVED", Version: versions[item.TrackID],
		})
	}
	return result, nil
}
