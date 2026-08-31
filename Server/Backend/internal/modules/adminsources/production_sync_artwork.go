package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/modules/adminmetadata"
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
	var currentReady bool
	if err := transaction.QueryRow(ctx, `
		SELECT album.cover_asset_id, COALESCE(asset.status = 'READY', false)
		FROM albums album
		LEFT JOIN media_assets asset ON asset.id = album.cover_asset_id
		WHERE album.id = $1
		FOR UPDATE OF album`, *albumID).Scan(&current, &currentReady); err != nil {
		return false, err
	}
	if current != nil && currentReady {
		return false, nil
	}
	var assetID string
	err := transaction.QueryRow(ctx, `
		INSERT INTO media_assets(
			storage_path, kind, mime_type, size_bytes, checksum_sha256, status
		) VALUES($1, 'ARTWORK', $2, $3, $4, 'READY')
		ON CONFLICT(storage_path) DO UPDATE SET
			mime_type = EXCLUDED.mime_type, size_bytes = EXCLUDED.size_bytes,
			checksum_sha256 = EXCLUDED.checksum_sha256, status = 'READY', updated_at = now()
		RETURNING id`,
		artwork.StoragePath, firstNonEmptyString(artwork.MIMEType, "image/jpeg"), artwork.SizeBytes, artwork.Checksum,
	).Scan(&assetID)
	if err != nil {
		return false, fmt.Errorf("store scanned artwork asset: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE albums SET cover_asset_id = $2, version = version + 1, updated_at = now()
		WHERE id = $1`, *albumID, assetID); err != nil {
		return false, fmt.Errorf("attach scanned album artwork: %w", err)
	}
	if current != nil {
		_, _ = transaction.Exec(ctx, `
			UPDATE media_assets asset SET status = 'DELETE_PENDING', updated_at = now()
			WHERE asset.id = $1
			  AND NOT EXISTS (SELECT 1 FROM artists WHERE artwork_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM albums WHERE cover_asset_id = asset.id)
			  AND NOT EXISTS (SELECT 1 FROM user_profiles WHERE avatar_asset_id = asset.id)`, *current)
	}
	return true, nil
}

type stagedArtwork struct {
	StoragePath    string
	SourceChecksum string
	SizeBytes      int64
	Checksum       string
	MIMEType       string
	Width          *int
	Height         *int
}

// stageArtwork extracts the first embedded picture from an audio container into
// the managed local asset directory. Artwork is deliberately not part of the
// source file itself: scan keeps reading the user's library, while clients
// receive a stable /api/v1/assets URL. A malformed/unsupported picture must
// not make an otherwise playable audio file disappear from the catalog, so a
// non-zero FFmpeg exit is treated as "no usable artwork".
func (synchronizer *ProductionSynchronizer) stageArtwork(
	ctx context.Context,
	sourcePath string,
	hasArtwork bool,
	sourceChecksum string,
) (*stagedArtwork, error) {
	if !hasArtwork || synchronizer.localMedia == nil || strings.TrimSpace(synchronizer.ffmpegPath) == "" {
		return nil, nil
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		if cached, ok := snapshot.artworkForChecksum(sourceChecksum); ok {
			return cached, nil
		}
	}

	if synchronizer.artworkGate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case synchronizer.artworkGate <- struct{}{}:
			defer func() { <-synchronizer.artworkGate }()
		}
	}
	// Another worker may have populated the scan cache while this worker was
	// waiting for the FFmpeg gate. Re-check after acquiring it to avoid a
	// duplicate extraction for the same source.
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		if cached, ok := snapshot.artworkForChecksum(sourceChecksum); ok {
			return cached, nil
		}
	}

	tempDir := filepath.Join(synchronizer.localMedia.AssetDirectory(), "temp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("create embedded artwork temp directory: %w", err)
	}
	tempFile, err := os.CreateTemp(tempDir, "embedded-cover-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create embedded artwork temp file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("close embedded artwork temp file: %w", err)
	}
	defer os.Remove(tempPath)

	runner := synchronizer.artworkRunner
	if runner == nil {
		runner = adminmetadata.OSProcessRunner{}
	}
	runExtraction := func(mapSpecifier string) (adminmetadata.ProcessResult, error) {
		arguments := []string{
			"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
			"-i", sourcePath,
			"-map", mapSpecifier,
			"-frames:v", "1",
			"-map_metadata", "-1",
			"-vf", "scale='min(1600,iw)':'min(1600,ih)':force_original_aspect_ratio=decrease",
			"-c:v", "mjpeg", "-q:v", "2",
			tempPath,
		}
		return runner.Run(ctx, synchronizer.ffmpegPath, arguments, 30*time.Second)
	}
	// Prefer a stream explicitly marked as attached artwork. This avoids
	// accidentally extracting the first frame of a real video stream from a
	// container that happens to include both video and album art. Some files
	// only carry a textual cover marker, so fall back to the first video stream.
	result, err := runExtraction("0:v:disp:attached_pic?")
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	info, statErr := os.Stat(tempPath)
	if statErr != nil || info.IsDir() || info.Size() == 0 {
		_ = os.Remove(tempPath)
		result, err = runExtraction("0:v:0?")
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, nil
		}
	}
	if result.TimedOut || result.ExitCode != 0 {
		return nil, nil
	}

	info, err = os.Stat(tempPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return nil, nil
	}
	checksum, err := fileSHA256(tempPath)
	if err != nil {
		return nil, fmt.Errorf("checksum extracted artwork: %w", err)
	}
	relativePath := filepath.ToSlash(filepath.Join("artworks", checksum+".jpg"))
	if existing, statErr := synchronizer.localMedia.StatAsset(relativePath); statErr == nil && existing.Size() == info.Size() && existing.Size() > 0 {
		artwork := &stagedArtwork{
			StoragePath: relativePath, SourceChecksum: sourceChecksum,
			SizeBytes: existing.Size(), Checksum: checksum, MIMEType: "image/jpeg",
		}
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			snapshot.rememberArtwork(sourceChecksum, artwork)
		}
		return artwork, nil
	}
	committedPath, err := synchronizer.localMedia.CommitUpload(ctx, tempPath, relativePath)
	if err != nil {
		return nil, fmt.Errorf("commit extracted artwork: %w", err)
	}
	artwork := &stagedArtwork{
		StoragePath: committedPath, SourceChecksum: sourceChecksum,
		SizeBytes: info.Size(), Checksum: checksum, MIMEType: "image/jpeg",
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		snapshot.rememberArtwork(sourceChecksum, artwork)
	}
	return artwork, nil
}

func (synchronizer *ProductionSynchronizer) artworkEnabled() bool {
	return synchronizer != nil && synchronizer.localMedia != nil && strings.TrimSpace(synchronizer.ffmpegPath) != ""
}

func (synchronizer *ProductionSynchronizer) needsArtworkForTrack(ctx context.Context, trackID *string) (bool, error) {
	if !synchronizer.artworkEnabled() || trackID == nil || strings.TrimSpace(*trackID) == "" {
		return false, nil
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		return snapshot.needsArtwork(*trackID), nil
	}
	var missing bool
	err := synchronizer.database.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM tracks track
			JOIN albums album ON album.id = track.album_id
			JOIN track_metadata metadata ON metadata.track_id = track.id
			LEFT JOIN media_assets asset
				ON asset.id = album.cover_asset_id AND asset.status = 'READY'
			WHERE track.id = $1 AND metadata.raw_tags->>'hasArtwork' = 'true' AND asset.id IS NULL
		)`, *trackID).Scan(&missing)
	if err != nil {
		return false, fmt.Errorf("check scanned track artwork: %w", err)
	}
	return missing, nil
}

func (synchronizer *ProductionSynchronizer) needsArtworkForSource(ctx context.Context, sourceID string) (bool, error) {
	if !synchronizer.artworkEnabled() || strings.TrimSpace(sourceID) == "" {
		return false, nil
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		return snapshot.needsArtworkForSource(sourceID), nil
	}
	var missing bool
	err := synchronizer.database.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM local_music_source_tracks mapping
			JOIN tracks track ON track.id = mapping.track_id
			JOIN albums album ON album.id = track.album_id
			JOIN track_metadata metadata ON metadata.track_id = track.id
			LEFT JOIN media_assets asset
				ON asset.id = album.cover_asset_id AND asset.status = 'READY'
			WHERE mapping.source_id = $1 AND metadata.raw_tags->>'hasArtwork' = 'true' AND asset.id IS NULL
		)`, sourceID).Scan(&missing)
	if err != nil {
		return false, fmt.Errorf("check scanned source artwork: %w", err)
	}
	return missing, nil
}

func (synchronizer *ProductionSynchronizer) cleanupUnreferencedArtwork(
	requestContext context.Context,
	artwork *stagedArtwork,
) {
	if artwork == nil || synchronizer.localMedia == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(requestContext), 2*time.Second)
	defer cancel()
	var referenced bool
	if err := synchronizer.database.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM media_assets WHERE storage_path = $1)`, artwork.StoragePath).Scan(&referenced); err != nil {
		// Never remove a file if the database could not confirm that it is
		// unreferenced. Retention/deployment cleanup can handle the orphan.
		return
	}
	if referenced {
		return
	}
	if snapshot := sourceScanSnapshotFromContext(requestContext); snapshot != nil {
		snapshot.forgetArtwork(artwork.SourceChecksum)
		if snapshot.hasArtworkPath(artwork.StoragePath) {
			return
		}
	}
	_ = synchronizer.localMedia.DeleteAsset(artwork.StoragePath)
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
	if batchCache := scanBatchCatalogFromContext(ctx); batchCache != nil {
		batchCache.forgetAlbum(*albumID)
	}
	if coverID != nil {
		_, _ = transaction.Exec(ctx, `UPDATE media_assets asset SET status='DELETE_PENDING',updated_at=now()
			WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM artists WHERE artwork_asset_id=asset.id)
			AND NOT EXISTS(SELECT 1 FROM albums WHERE cover_asset_id=asset.id)`, *coverID)
	}
	return nil
}
