package adminsources

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// querySourceTrackIDs returns every catalog track touched by the supplied
// source rows. The source table keeps a primary track for ordinary files while
// local_music_source_tracks also contains all CUE segments, so both columns
// must be considered.
func querySourceTrackIDs(ctx context.Context, transaction pgx.Tx, sourceIDs []string) ([]string, error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	rows, err := transaction.Query(ctx, `
		SELECT DISTINCT track_id::text
		FROM local_music_source_tracks
		WHERE source_id = ANY($1::uuid[])
		UNION
		SELECT DISTINCT track_id::text
		FROM local_music_sources
		WHERE id = ANY($1::uuid[]) AND track_id IS NOT NULL`, sourceIDs)
	if err != nil {
		return nil, fmt.Errorf("query source catalog tracks: %w", err)
	}
	defer rows.Close()
	trackIDs := make([]string, 0)
	for rows.Next() {
		var trackID string
		if err := rows.Scan(&trackID); err != nil {
			return nil, fmt.Errorf("scan source catalog track: %w", err)
		}
		trackIDs = append(trackIDs, trackID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source catalog tracks: %w", err)
	}
	return trackIDs, nil
}

func queryRootTrackIDs(ctx context.Context, transaction pgx.Tx, rootID string) ([]string, error) {
	rows, err := transaction.Query(ctx, `
		SELECT DISTINCT mapping.track_id::text
		FROM local_music_source_tracks mapping
		JOIN local_music_sources source ON source.id = mapping.source_id
		WHERE source.root_id = $1
		UNION
		SELECT DISTINCT source.track_id::text
		FROM local_music_sources source
		WHERE source.root_id = $1 AND source.track_id IS NOT NULL`, rootID)
	if err != nil {
		return nil, fmt.Errorf("query root catalog tracks: %w", err)
	}
	defer rows.Close()
	trackIDs := make([]string, 0)
	for rows.Next() {
		var trackID string
		if err := rows.Scan(&trackID); err != nil {
			return nil, fmt.Errorf("scan root catalog track: %w", err)
		}
		trackIDs = append(trackIDs, trackID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate root catalog tracks: %w", err)
	}
	return trackIDs, nil
}

// deleteOrphanedTracks removes catalog rows that no longer have any local
// source mapping and are not backed by an uploaded source asset. It only
// changes database rows: source files and uploaded media files are never
// touched here. Playlist links are removed first because that FK is RESTRICT.
func deleteOrphanedTracks(ctx context.Context, transaction pgx.Tx, candidates []string) (int, error) {
	candidates = uniqueIDs(candidates)
	if len(candidates) == 0 {
		return 0, nil
	}

	rows, err := transaction.Query(ctx, `
		SELECT track.id::text, track.album_id::text
		FROM tracks track
		WHERE track.id = ANY($1::uuid[])
		  AND track.source_asset_id IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM local_music_source_tracks mapping WHERE mapping.track_id = track.id
		  )
		FOR UPDATE`, candidates)
	if err != nil {
		return 0, fmt.Errorf("find orphaned catalog tracks: %w", err)
	}
	trackIDs := make([]string, 0, len(candidates))
	albumIDs := make([]string, 0)
	for rows.Next() {
		var trackID string
		var albumID *string
		if err := rows.Scan(&trackID, &albumID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan orphaned catalog track: %w", err)
		}
		trackIDs = append(trackIDs, trackID)
		if albumID != nil {
			albumIDs = append(albumIDs, *albumID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate orphaned catalog tracks: %w", err)
	}
	rows.Close()
	if len(trackIDs) == 0 {
		return 0, nil
	}

	if _, err := transaction.Exec(ctx, `DELETE FROM playlist_tracks WHERE track_id = ANY($1::uuid[])`, trackIDs); err != nil {
		return 0, fmt.Errorf("remove playlist links for deleted catalog tracks: %w", err)
	}
	deleted, err := transaction.Exec(ctx, `DELETE FROM tracks WHERE id = ANY($1::uuid[])`, trackIDs)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned catalog tracks: %w", err)
	}

	for _, albumID := range uniqueIDs(albumIDs) {
		if err := deleteAlbumIfEmpty(ctx, transaction, &albumID, nil); err != nil {
			return 0, err
		}
	}
	return int(deleted.RowsAffected()), nil
}

func uniqueIDs(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
