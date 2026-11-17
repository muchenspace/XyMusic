package localassets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/platform/localmedia"
)

const (
	assetCleanupBatchSize = 100
	assetCleanupGrace     = time.Minute
)

// Cleaner removes local asset files after the owning database references have
// been removed. The database row is marked DELETED only after the file has
// been removed, so a transient filesystem failure remains retryable.
type Cleaner struct {
	pool  *pgxpool.Pool
	media *localmedia.Store
}

type pendingAsset struct {
	id          string
	storagePath string
}

func NewCleaner(pool *pgxpool.Pool, media *localmedia.Store) (*Cleaner, error) {
	if pool == nil {
		return nil, errors.New("local asset cleaner database is required")
	}
	if media == nil {
		return nil, errors.New("local asset cleaner media store is required")
	}
	return &Cleaner{pool: pool, media: media}, nil
}

func (cleaner *Cleaner) RunOnce(ctx context.Context) (bool, error) {
	if cleaner == nil || cleaner.pool == nil || cleaner.media == nil {
		return false, errors.New("local asset cleaner is not initialized")
	}
	tx, err := cleaner.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin local asset cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT asset.id, asset.storage_path
		FROM media_assets asset
		WHERE asset.status = 'DELETE_PENDING'
		  AND asset.updated_at <= now() - ($1::double precision * interval '1 second')
		  AND NOT EXISTS (SELECT 1 FROM tracks track WHERE track.source_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM local_music_sources source WHERE source.source_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM lyrics lyric WHERE lyric.asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM media_uploads upload WHERE upload.asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM artists artist WHERE artist.artwork_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM albums album WHERE album.cover_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM playlists playlist WHERE playlist.cover_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM user_profiles profile WHERE profile.avatar_asset_id = asset.id)
		ORDER BY asset.updated_at, asset.id
		LIMIT $2
		FOR UPDATE OF asset SKIP LOCKED`, assetCleanupGrace.Seconds(), assetCleanupBatchSize)
	if err != nil {
		return false, fmt.Errorf("list pending local assets: %w", err)
	}

	assets := make([]pendingAsset, 0, assetCleanupBatchSize)
	for rows.Next() {
		var asset pendingAsset
		if err := rows.Scan(&asset.id, &asset.storagePath); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan pending local asset: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("iterate pending local assets: %w", err)
	}
	rows.Close()
	if len(assets) == 0 {
		return false, nil
	}

	deletedIDs := make([]string, 0, len(assets))
	var deleteErr error
	for _, asset := range assets {
		if err := cleaner.media.DeleteAsset(asset.storagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			deleteErr = fmt.Errorf("delete local asset %s: %w", asset.id, err)
			break
		}
		deletedIDs = append(deletedIDs, asset.id)
	}

	worked := false
	if len(deletedIDs) > 0 {
		command, err := tx.Exec(ctx, `
			UPDATE media_assets asset
			SET status = 'DELETED', updated_at = now()
			WHERE asset.id = ANY($1::uuid[])
			  AND asset.status = 'DELETE_PENDING'
			  AND NOT EXISTS (SELECT 1 FROM tracks track WHERE track.source_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM local_music_sources source WHERE source.source_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM lyrics lyric WHERE lyric.asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM media_uploads upload WHERE upload.asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM artists artist WHERE artist.artwork_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM albums album WHERE album.cover_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM playlists playlist WHERE playlist.cover_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM user_profiles profile WHERE profile.avatar_asset_id = asset.id)`, deletedIDs)
		if err != nil {
			return false, fmt.Errorf("mark local assets deleted: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit local asset cleanup: %w", err)
		}
		worked = command.RowsAffected() > 0
	}
	if deleteErr != nil {
		return worked, deleteErr
	}
	return worked, nil
}
