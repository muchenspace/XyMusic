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

func (repository *Repository) RestoreTracksBatch(
	ctx context.Context,
	actorID string,
	traceID string,
	input []BatchTrackItemInput,
) ([]BatchRestoreItemRecord, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin batch track restore: %w", err)
	}
	defer tx.Rollback(ctx)

	items := append([]BatchTrackItemInput(nil), input...)
	sort.Slice(items, func(left, right int) bool { return items[left].TrackID < items[right].TrackID })
	type preparedRestore struct {
		item  BatchTrackItemInput
		state trackMutationState
	}
	prepared := make([]preparedRestore, 0, len(items))
	for _, item := range items {
		state, err := lockTrackMutationState(ctx, tx, item.TrackID)
		if err != nil {
			return nil, err
		}
		if state.Version != item.ExpectedVersion {
			return nil, versionConflict("Track", item.ExpectedVersion, state.Version, map[string]any{"trackId": item.TrackID})
		}
		if state.Status != "ARCHIVED" {
			return nil, apperror.New(
				apperror.CodeInvalidStateTransition,
				"Only archived tracks can be restored",
				apperror.WithMetadata(map[string]any{"trackId": item.TrackID}),
			)
		}
		if state.DurationMS <= 0 {
			return nil, apperror.Unprocessable(
				apperror.CodeTrackNotPlayable,
				"Track duration must be positive",
				map[string]any{"trackId": item.TrackID},
			)
		}
		var playable bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM track_variants WHERE track_id=$1 AND status='READY'
		)`, item.TrackID).Scan(&playable); err != nil {
			return nil, fmt.Errorf("inspect batch restore playable variant: %w", err)
		}
		if !playable {
			return nil, apperror.Unprocessable(
				apperror.CodeTrackNotPlayable,
				"Track has no ready playback variant",
				map[string]any{"trackId": item.TrackID},
			)
		}
		prepared = append(prepared, preparedRestore{item: item, state: state})
	}

	now := time.Now().UTC()
	batchID := uuid.NewString()
	versions := make(map[string]int, len(prepared))
	for _, restore := range prepared {
		var version int
		err := tx.QueryRow(ctx, `UPDATE tracks SET status='READY',published_at=$3,
			version=version+1,updated_at=$3
			WHERE id=$1 AND version=$2 AND status='ARCHIVED' RETURNING version`,
			restore.item.TrackID, restore.item.ExpectedVersion, now).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.Conflict(
				apperror.CodeVersionConflict,
				"Track changed during the batch restore operation",
				map[string]any{"trackId": restore.item.TrackID},
			)
		}
		if err != nil {
			return nil, fmt.Errorf("restore batch track: %w", err)
		}
		if err := writeAudit(ctx, tx, actorID, "admin.track.restore", "track", restore.item.TrackID, traceID, map[string]any{
			"batchId": batchID, "batchSize": len(prepared),
		}); err != nil {
			return nil, err
		}
		versions[restore.item.TrackID] = version
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit batch track restore: %w", err)
	}
	result := make([]BatchRestoreItemRecord, 0, len(input))
	for _, item := range input {
		result = append(result, BatchRestoreItemRecord{
			TrackID: item.TrackID, Status: "READY", Version: versions[item.TrackID],
		})
	}
	return result, nil
}
