package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

func attachAlbumArtwork(
	ctx context.Context,
	transaction pgx.Tx,
	albumID *string,
	artwork *stagedArtwork,
) (bool, error) {
	if albumID == nil || artwork == nil {
		return false, nil
	}
	var current *string
	if err := transaction.QueryRow(ctx, `SELECT cover_asset_id FROM albums WHERE id=$1 FOR UPDATE`, *albumID).Scan(&current); err != nil {
		return false, err
	}
	if current != nil {
		return false, nil
	}
	var assetID string
	err := transaction.QueryRow(ctx, `INSERT INTO media_assets(
		object_key,kind,mime_type,size_bytes,checksum_sha256,status
	) VALUES($1,'ARTWORK','image/jpeg',$2,$3,'READY')
	ON CONFLICT(object_key) DO UPDATE SET status='READY',updated_at=now() RETURNING id`,
		artwork.ObjectKey, artwork.SizeBytes, artwork.Checksum).Scan(&assetID)
	if err != nil {
		return false, err
	}
	_, err = transaction.Exec(ctx, `UPDATE albums SET cover_asset_id=$2,
		version=version+1,updated_at=now() WHERE id=$1`, *albumID, assetID)
	return err == nil, err
}

func deleteAlbumIfEmpty(
	ctx context.Context,
	transaction pgx.Tx,
	albumID *string,
	cache *scanCatalogCache,
) error {
	if albumID == nil {
		return nil
	}
	var normalizedTitle string
	err := transaction.QueryRow(ctx, `SELECT normalized_title FROM albums WHERE id=$1`, *albumID).Scan(&normalizedTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find empty local library album: %w", err)
	}
	if _, err := transaction.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "album:"+normalizedTitle); err != nil {
		return fmt.Errorf("lock empty local library album: %w", err)
	}
	var coverID *string
	err = transaction.QueryRow(ctx, `DELETE FROM albums album WHERE album.id=$1
		AND NOT EXISTS(SELECT 1 FROM tracks WHERE tracks.album_id=album.id)
		RETURNING cover_asset_id`, *albumID).Scan(&coverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete empty local library album: %w", err)
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		snapshot.forgetAlbum(*albumID)
	}
	cache.forgetAlbum(*albumID)
	if coverID == nil {
		return nil
	}
	var objectKey string
	err = transaction.QueryRow(ctx, `UPDATE media_assets asset SET status='DELETE_PENDING',updated_at=now()
		WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM artists WHERE artwork_asset_id=asset.id)
		AND NOT EXISTS(SELECT 1 FROM albums WHERE cover_asset_id=asset.id)
		RETURNING object_key`, *coverID).Scan(&objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return enqueueCleanupTx(ctx, transaction, objectKey, "EMPTY_ALBUM_AFTER_LIBRARY_SCAN")
}

func (synchronizer *ProductionSynchronizer) stageArtwork(
	ctx context.Context,
	sourcePath string,
	hasArtwork bool,
	sourceChecksum string,
) (*stagedArtwork, error) {
	if !hasArtwork || synchronizer.ffmpegPath == "" {
		return nil, nil
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		return snapshot.coalescedArtwork(ctx, sourceChecksum, func() (*stagedArtwork, error) {
			return synchronizer.stageArtworkUncached(ctx, sourcePath, hasArtwork, sourceChecksum)
		})
	}
	return synchronizer.stageArtworkUncached(ctx, sourcePath, hasArtwork, sourceChecksum)
}

func (synchronizer *ProductionSynchronizer) stageArtworkUncached(
	ctx context.Context,
	sourcePath string,
	hasArtwork bool,
	sourceChecksum string,
) (*stagedArtwork, error) {
	if !hasArtwork || synchronizer.ffmpegPath == "" {
		return nil, nil
	}
	directory, err := os.MkdirTemp("", "xymusic-cover-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	outputPath := filepath.Join(directory, "cover.jpg")
	result, err := synchronizer.runFFmpeg(ctx, []string{
		"-nostdin", "-v", "error", "-y", "-i", sourcePath,
		"-map", "0:v:0", "-frames:v", "1", "-c:v", "mjpeg", outputPath,
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if result.TimedOut || result.ExitCode != 0 {
		return nil, nil
	}
	checksum, err := fileSHA256(outputPath)
	if err != nil {
		return nil, err
	}
	objectKey := "library/artwork/" + checksum + ".jpg"
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		snapshot.rememberArtworkCandidate(stagedArtwork{
			ObjectKey: objectKey, Checksum: checksum,
		})
	}
	size, err := synchronizer.uploadFile(ctx, objectKey, outputPath, "image/jpeg", checksum)
	if err != nil {
		return nil, err
	}
	return &stagedArtwork{ObjectKey: objectKey, SizeBytes: size, Checksum: checksum}, nil
}

func (synchronizer *ProductionSynchronizer) flushArtworkCleanup(
	ctx context.Context,
	snapshot *sourceScanSnapshot,
) error {
	cleanups := snapshot.pendingArtworkCleanups()
	if len(cleanups) == 0 {
		return nil
	}
	objectKeys := make([]string, len(cleanups))
	reasons := make([]string, len(cleanups))
	for index, cleanup := range cleanups {
		objectKeys[index], reasons[index] = cleanup.objectKey, cleanup.reason
	}
	_, err := synchronizer.database.Exec(ctx, `
		INSERT INTO object_cleanup_jobs(object_key,reason)
		SELECT input.object_key,input.reason
		FROM unnest($1::text[],$2::text[]) AS input(object_key,reason)
		ON CONFLICT(object_key) DO UPDATE SET reason=EXCLUDED.reason,status='PENDING',
			attempts=0,attempt_id=NULL,locked_by=NULL,locked_until=NULL,next_attempt_at=now(),
			last_error=NULL,updated_at=now()`, objectKeys, reasons)
	if err != nil {
		return fmt.Errorf("enqueue local library artwork cleanup batch: %w", err)
	}
	snapshot.markArtworkCleanupsQueued(cleanups)
	return nil
}

func (synchronizer *ProductionSynchronizer) enqueueCleanup(ctx context.Context, objectKey, reason string) error {
	_, err := synchronizer.database.Exec(ctx, cleanupUpsertSQL, objectKey, reason)
	if err != nil {
		return fmt.Errorf("enqueue local library object cleanup: %w", err)
	}
	return nil
}

func enqueueCleanupTx(ctx context.Context, transaction pgx.Tx, objectKey, reason string) error {
	_, err := transaction.Exec(ctx, cleanupUpsertSQL, objectKey, reason)
	if err != nil {
		return fmt.Errorf("enqueue local library object cleanup: %w", err)
	}
	return nil
}

const cleanupUpsertSQL = `INSERT INTO object_cleanup_jobs(object_key,reason)
	VALUES($1,$2) ON CONFLICT(object_key) DO UPDATE SET reason=EXCLUDED.reason,status='PENDING',
	attempts=0,attempt_id=NULL,locked_by=NULL,locked_until=NULL,next_attempt_at=now(),
	last_error=NULL,updated_at=now()`
