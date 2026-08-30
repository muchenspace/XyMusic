package playback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

type ResolvedAudioSource struct {
	TrackID        string
	SourcePath     string
	CueStartTimeMs *int64
	CueEndTimeMs   *int64
	DurationMs     int64
	SizeBytes      int64
	ChecksumSHA256 string
	SourceKind     string
	Bitrate        int
	SampleRate     *int
}

type SourceResolver interface {
	ResolveSource(ctx context.Context, trackID string) (*ResolvedAudioSource, error)
	PublishedTrackExists(ctx context.Context, trackID string) (bool, error)
}

type PlaybackSourceResolver struct {
	pool       *pgxpool.Pool
	localMedia *localmedia.Store
}

func NewPlaybackSourceResolver(pool *pgxpool.Pool, localMedia *localmedia.Store) *PlaybackSourceResolver {
	return &PlaybackSourceResolver{
		pool:       pool,
		localMedia: localMedia,
	}
}

func (r *PlaybackSourceResolver) PublishedTrackExists(ctx context.Context, trackID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tracks
			WHERE id = $1 AND status = 'READY' AND published_at IS NOT NULL
		)`, trackID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check playable track: %w", err)
	}
	return exists, nil
}

func (r *PlaybackSourceResolver) ResolveSource(ctx context.Context, trackID string) (*ResolvedAudioSource, error) {
	// 1. Try local music scan source
	var (
		sourceRelPath string
		rootPath      string
		cueStart      *int64
		cueEnd        *int64
		checksum      string
		expectedSize  int64
		expectedMod   time.Time
		durationMs    int64
	)
	scanErr := r.pool.QueryRow(ctx, `
		SELECT s.source_path, r.path, st.cue_start_time_ms, st.cue_end_time_ms,
		       s.checksum_sha256, s.size_bytes, s.modified_at, t.duration_ms
		FROM tracks t
		JOIN local_music_source_tracks st ON st.track_id = t.id
		JOIN local_music_sources s ON s.id = st.source_id
		JOIN library_roots r ON r.id = s.root_id
		WHERE t.id = $1 AND t.status = 'READY' AND t.published_at IS NOT NULL
		  AND s.status = 'READY' AND r.enabled = true
		LIMIT 1`, trackID).Scan(
		&sourceRelPath,
		&rootPath,
		&cueStart,
		&cueEnd,
		&checksum,
		&expectedSize,
		&expectedMod,
		&durationMs,
	)

	if scanErr == nil {
		realPath, err := secureSourcePath(rootPath, sourceRelPath)
		if err != nil {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio source path is invalid", nil)
		}
		info, err := os.Stat(realPath)
		if err != nil || info.IsDir() {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio source file is unavailable", nil)
		}
		if info.Size() != expectedSize || (!expectedMod.IsZero() && info.ModTime().UnixMilli() != expectedMod.UnixMilli()) {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio source file has changed", nil)
		}
		if durationMs <= 0 || expectedSize <= 0 {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio source metadata is incomplete", nil)
		}
		return &ResolvedAudioSource{
			TrackID:        trackID,
			SourcePath:     realPath,
			CueStartTimeMs: cueStart,
			CueEndTimeMs:   cueEnd,
			DurationMs:     durationMs,
			SizeBytes:      expectedSize,
			ChecksumSHA256: checksum,
			SourceKind:     "SCAN",
			Bitrate:        320000,
		}, nil
	}

	if !errors.Is(scanErr, pgx.ErrNoRows) {
		return nil, fmt.Errorf("query local scan source: %w", scanErr)
	}

	// 2. Try uploaded media asset source
	var (
		storageRelPath string
		assetChecksum  *string
		assetSize      int64
		trackDuration  int64
	)
	uploadErr := r.pool.QueryRow(ctx, `
		SELECT a.storage_path, a.checksum_sha256, a.size_bytes, t.duration_ms
		FROM tracks t
		JOIN media_assets a ON a.id = t.source_asset_id
		WHERE t.id = $1 AND t.status = 'READY' AND t.published_at IS NOT NULL
		  AND a.status = 'READY' AND a.kind = 'AUDIO_SOURCE'
		LIMIT 1`, trackID).Scan(
		&storageRelPath,
		&assetChecksum,
		&assetSize,
		&trackDuration,
	)

	if uploadErr == nil {
		realPath, err := r.localMedia.ResolveAssetPath(storageRelPath)
		if err != nil {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio asset path is invalid", nil)
		}
		info, err := os.Stat(realPath)
		if err != nil || info.IsDir() {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio asset file is unavailable", nil)
		}
		if info.Size() != assetSize {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio asset file has changed", nil)
		}
		if trackDuration <= 0 || assetSize <= 0 {
			return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Audio asset metadata is incomplete", nil)
		}
		return &ResolvedAudioSource{
			TrackID:        trackID,
			SourcePath:     realPath,
			DurationMs:     trackDuration,
			SizeBytes:      assetSize,
			ChecksumSHA256: pointerStringValue(assetChecksum),
			SourceKind:     "UPLOAD",
			Bitrate:        320000,
		}, nil
	}

	if !errors.Is(uploadErr, pgx.ErrNoRows) {
		return nil, fmt.Errorf("query uploaded source asset: %w", uploadErr)
	}

	exists, err := r.PublishedTrackExists(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperror.NotFound("Track was not found")
	}
	return nil, apperror.Unprocessable(apperror.CodeTrackNotPlayable, "No playable audio source is available", nil)
}

func pointerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func secureSourcePath(rootPath, sourcePath string) (string, error) {
	root, err := filepath.Abs(rootPath)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(sourcePath) {
		return "", errors.New("source path must be relative")
	}
	candidate, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(sourcePath)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("source path escapes root")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	realRelative, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || realRelative == ".." || strings.HasPrefix(realRelative, ".."+string(filepath.Separator)) {
		return "", errors.New("source path symlink escapes root")
	}
	return candidate, nil
}
