package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

func (repository *Repository) Metadata(ctx context.Context, trackID string) (TrackMetadata, error) {
	fence := batchMutationFenceFromContext(ctx)
	if fence == nil {
		if err := repository.ensureMetadata(ctx, trackID); err != nil {
			return TrackMetadata{}, err
		}
		return repository.loadMetadata(ctx, trackID)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("begin fenced metadata load: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fence.Lock(ctx, mediaTxAdapter{tx: tx}); err != nil {
		return TrackMetadata{}, err
	}
	if err := repository.ensureMetadataWith(ctx, tx, trackID); err != nil {
		return TrackMetadata{}, err
	}
	result, err := repository.loadMetadataWith(ctx, tx, trackID)
	if err != nil {
		return TrackMetadata{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TrackMetadata{}, fmt.Errorf("commit fenced metadata load: %w", err)
	}
	return result, nil
}

func (repository *Repository) UpdateMetadata(
	ctx context.Context,
	actorID string,
	trackID string,
	expectedVersion int,
	patch MetadataPatch,
	reason string,
) (TrackMetadata, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("begin metadata update: %w", err)
	}
	defer tx.Rollback(ctx)
	if fence := batchMutationFenceFromContext(ctx); fence != nil {
		if err := fence.Lock(ctx, mediaTxAdapter{tx: tx}); err != nil {
			return TrackMetadata{}, err
		}
	}
	var trackStatus string
	err = tx.QueryRow(ctx, "SELECT status::text FROM tracks WHERE id = $1 FOR UPDATE", trackID).Scan(&trackStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackMetadata{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("lock track for metadata update: %w", err)
	}
	if trackIsArchived(trackStatus) {
		return TrackMetadata{}, archivedTrackError(trackID)
	}
	if err := repository.ensureMetadataWith(ctx, tx, trackID); err != nil {
		return TrackMetadata{}, err
	}
	var rawJSON, overridesJSON []byte
	var version int
	err = tx.QueryRow(ctx, `
		SELECT raw_tags, overrides, version
		FROM track_metadata WHERE track_id = $1 FOR UPDATE`, trackID).Scan(&rawJSON, &overridesJSON, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackMetadata{}, apperror.NotFound("Track metadata was not found")
	}
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("lock track metadata: %w", err)
	}
	if version != expectedVersion {
		return TrackMetadata{}, apperror.Conflict(apperror.CodeVersionConflict, "Track metadata version is stale", map[string]any{
			"expectedVersion": expectedVersion,
			"currentVersion":  version,
		})
	}
	raw, overrides, err := decodeMetadataDocuments(rawJSON, overridesJSON)
	if err != nil {
		return TrackMetadata{}, err
	}
	nextOverrides := cloneMap(overrides)
	for field, value := range patch {
		if !editableMetadataField(field) {
			return TrackMetadata{}, apperror.Validation("The metadata patch contains an unknown field")
		}
		nextOverrides[field] = value
	}
	if reflect.DeepEqual(normalizeComparable(overrides), normalizeComparable(nextOverrides)) {
		return TrackMetadata{}, apperror.Conflict(apperror.CodeResourceConflict, "The metadata edit does not change any field", nil)
	}
	previousEffective, err := applyOverrides(raw, overrides)
	if err != nil {
		return TrackMetadata{}, err
	}
	nextEffective, err := applyOverrides(raw, nextOverrides)
	if err != nil {
		return TrackMetadata{}, err
	}
	nextOverridesJSON, _ := json.Marshal(nextOverrides)
	nextVersion := version + 1
	command, err := tx.Exec(ctx, `
		UPDATE track_metadata
		SET overrides = $1::jsonb, updated_by = $2, version = $3, updated_at = now()
		WHERE track_id = $4 AND version = $5`, nextOverridesJSON, actorID, nextVersion, trackID, version)
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("update track metadata: %w", err)
	}
	if command.RowsAffected() != 1 {
		return TrackMetadata{}, apperror.Conflict(apperror.CodeVersionConflict, "Track metadata version is stale", map[string]any{
			"expectedVersion": expectedVersion,
			"currentVersion":  version,
		})
	}
	if err := repository.projectMetadata(ctx, tx, trackID, nextEffective, previousEffective); err != nil {
		return TrackMetadata{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TrackMetadata{}, fmt.Errorf("commit metadata update: %w", err)
	}
	return repository.loadMetadata(ctx, trackID)
}

func (repository *Repository) ensureMetadata(ctx context.Context, trackID string) error {
	return repository.ensureMetadataWith(ctx, repository.pool, trackID)
}

func (repository *Repository) ensureMetadataWith(ctx context.Context, database metadataDatabase, trackID string) error {
	command, err := database.Exec(ctx, `
		INSERT INTO track_metadata (track_id, source_id, raw_tags, raw_checksum_sha256, last_scanned_at)
		SELECT track.id, source.id,
			jsonb_build_object(
				'title', track.title,
				'credits', COALESCE((
					SELECT jsonb_agg(jsonb_build_object('name', artist.name, 'role', credit.role)
					                 ORDER BY credit.sort_order, artist.name)
					FROM track_artists credit JOIN artists artist ON artist.id = credit.artist_id
					WHERE credit.track_id = track.id
				), '[{"name":"Unknown Artist","role":"PRIMARY"}]'::jsonb),
				'albumArtists', COALESCE((
					SELECT jsonb_agg(artist.name ORDER BY credit.sort_order, artist.name)
					FROM album_artists credit JOIN artists artist ON artist.id = credit.artist_id
					WHERE credit.album_id = track.album_id AND credit.role = 'PRIMARY'
				), (
					SELECT jsonb_agg(artist.name ORDER BY credit.sort_order, artist.name)
					FROM track_artists credit JOIN artists artist ON artist.id = credit.artist_id
					WHERE credit.track_id = track.id AND credit.role = 'PRIMARY'
				), '["Unknown Artist"]'::jsonb),
				'album', album.title, 'releaseDate', album.release_date,
				'trackNumber', track.track_number, 'trackTotal', NULL,
				'discNumber', track.disc_number, 'discTotal', NULL,
				'genres', '[]'::jsonb, 'bpm', NULL, 'isrc', NULL,
				'comment', NULL, 'copyright', NULL,
					'lyrics', (
						SELECT jsonb_build_object('content', lyric.content, 'format', lyric.format, 'language', lyric.language, 'timing', lyric.timing)
					FROM lyrics lyric WHERE lyric.track_id = track.id AND lyric.content IS NOT NULL
					ORDER BY lyric.is_default DESC, lyric.created_at, lyric.id LIMIT 1
				),
				'hasArtwork', album.cover_asset_id IS NOT NULL
			), source.checksum_sha256, source.updated_at
		FROM tracks track
		LEFT JOIN albums album ON album.id = track.album_id
		LEFT JOIN LATERAL (
			SELECT local_source.id, local_source.checksum_sha256, local_source.updated_at
			FROM local_music_source_tracks mapping
			JOIN local_music_sources local_source ON local_source.id = mapping.source_id
			WHERE mapping.track_id = track.id
			ORDER BY CASE local_source.status WHEN 'READY' THEN 0 WHEN 'PROCESSING' THEN 1 ELSE 2 END,
			         local_source.updated_at DESC, local_source.id LIMIT 1
		) source ON true
		WHERE track.id = $1
		ON CONFLICT (track_id) DO NOTHING`, trackID)
	if err != nil {
		return fmt.Errorf("ensure track metadata: %w", err)
	}
	if command.RowsAffected() == 0 {
		var exists bool
		if err := database.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM track_metadata WHERE track_id = $1)", trackID).Scan(&exists); err != nil {
			return fmt.Errorf("check track metadata: %w", err)
		}
		if !exists {
			return apperror.NotFound("Track was not found")
		}
	}
	return nil
}

func (repository *Repository) loadMetadata(ctx context.Context, trackID string) (TrackMetadata, error) {
	return repository.loadMetadataWith(ctx, repository.pool, trackID)
}

func (repository *Repository) loadMetadataWith(ctx context.Context, database metadataDatabase, trackID string) (TrackMetadata, error) {
	return scanMetadataRow(database.QueryRow(ctx, `
		SELECT metadata.track_id, metadata.raw_tags, metadata.overrides, metadata.version,
		       metadata.last_scanned_at, metadata.updated_by, metadata.created_at, metadata.updated_at,
		       source.id, source.root_id, source.source_path, source.status, source.checksum_sha256,
		       root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
			       ), track.status::text,
		       COALESCE(mapping_stats.mapping_count, 0)
		FROM track_metadata metadata
		LEFT JOIN local_music_sources source ON source.id = metadata.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN tracks track ON track.id = metadata.track_id
		LEFT JOIN LATERAL (
			SELECT count(*)::int AS mapping_count
			FROM local_music_source_tracks mapping WHERE mapping.source_id = source.id
		) mapping_stats ON true
		WHERE metadata.track_id = $1`, trackID))
}

type metadataRowScanner interface {
	Scan(...any) error
}

func scanMetadataRow(row metadataRowScanner) (TrackMetadata, error) {
	var values trackMetadataRowValues
	err := row.Scan(values.scanTargets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackMetadata{}, apperror.NotFound("Track metadata was not found")
	}
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("load track metadata: %w", err)
	}
	result, err := values.build()
	if err != nil {
		return TrackMetadata{}, err
	}
	return result, nil
}

func (repository *Repository) MetadataBatch(
	ctx context.Context,
	trackIDs []string,
) (map[string]TrackMetadata, error) {
	result := make(map[string]TrackMetadata, len(trackIDs))
	if len(trackIDs) == 0 {
		return result, nil
	}
	identifiers := make([]uuid.UUID, len(trackIDs))
	for index, trackID := range trackIDs {
		identifier, parseErr := uuid.Parse(trackID)
		if parseErr != nil {
			return nil, fmt.Errorf("parse track metadata batch id: %w", parseErr)
		}
		identifiers[index] = identifier
	}
	rows, err := repository.pool.Query(ctx, `
		WITH source_stats AS (
			SELECT mapping.source_id, count(*)::int AS mapping_count
			FROM local_music_source_tracks mapping
			WHERE mapping.source_id IN (
				SELECT source_id FROM track_metadata
				WHERE track_id = ANY($1::uuid[]) AND source_id IS NOT NULL
			)
			GROUP BY mapping.source_id
		)
		SELECT metadata.track_id, metadata.raw_tags, metadata.overrides, metadata.version,
		       metadata.last_scanned_at, metadata.updated_by, metadata.created_at, metadata.updated_at,
		       source.id, source.root_id, source.source_path, source.status, source.checksum_sha256,
		       root.mode, root.enabled, EXISTS (
			         SELECT 1 FROM library_scan_runs active_scan
			         WHERE active_scan.root_id = root.id
			           AND active_scan.status = 'RUNNING' AND active_scan.locked_until > now()
		       ), track.status::text,
		       COALESCE(source_stats.mapping_count, 0)
		FROM track_metadata metadata
		LEFT JOIN local_music_sources source ON source.id = metadata.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		LEFT JOIN tracks track ON track.id = metadata.track_id
		LEFT JOIN source_stats ON source_stats.source_id = source.id
		WHERE metadata.track_id = ANY($1::uuid[])`, identifiers)

	if err != nil {
		return nil, fmt.Errorf("query track metadata batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		metadata, scanErr := scanMetadataRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result[metadata.TrackID] = metadata
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate track metadata batch: %w", err)
	}
	return result, nil
}

func (repository *Repository) projectMetadata(
	ctx context.Context,
	tx pgx.Tx,
	trackID string,
	metadata MetadataSnapshot,
	previous MetadataSnapshot,
) error {
	var currentAlbumID, currentCoverID *string
	err := tx.QueryRow(ctx, `
		SELECT track.album_id, album.cover_asset_id
		FROM tracks track LEFT JOIN albums album ON album.id = track.album_id
		WHERE track.id = $1`, trackID).Scan(&currentAlbumID, &currentCoverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NotFound("Track was not found")
	}
	if err != nil {
		return fmt.Errorf("load metadata projection context: %w", err)
	}
	artistIDs := make(map[string]string)
	names := make([]string, 0, len(metadata.Credits)+len(metadata.AlbumArtists))
	for _, credit := range metadata.Credits {
		names = append(names, credit.Name)
	}
	names = append(names, metadata.AlbumArtists...)
	for _, name := range names {
		normalized := normalizeLookup(name)
		if _, exists := artistIDs[normalized]; exists {
			continue
		}
		var artistID string
		err := tx.QueryRow(ctx, `
			SELECT id FROM artists WHERE normalized_name = $1 ORDER BY id LIMIT 1`, normalized).Scan(&artistID)
		if errors.Is(err, pgx.ErrNoRows) {
			artistID = uuid.NewString()
			if _, err := tx.Exec(ctx, `
				INSERT INTO artists (id, name, normalized_name) VALUES ($1, $2, $3)`, artistID, name, normalized); err != nil {
				return fmt.Errorf("create projected artist: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("find projected artist: %w", err)
		}
		artistIDs[normalized] = artistID
	}

	var albumID *string
	if metadata.Album != nil && strings.TrimSpace(*metadata.Album) != "" {
		normalizedTitle := normalizeLookup(*metadata.Album)
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", normalizedTitle); err != nil {
			return fmt.Errorf("lock projected album identity: %w", err)
		}
		type albumCandidate struct {
			id, releaseDate string
			coverID         *string
		}
		rows, err := tx.Query(ctx, `
			SELECT id, COALESCE(release_date::text, ''), cover_asset_id
			FROM albums WHERE normalized_title = $1 ORDER BY id FOR UPDATE`, normalizedTitle)
		if err != nil {
			return fmt.Errorf("query projected albums: %w", err)
		}
		candidates := make([]albumCandidate, 0)
		for rows.Next() {
			var candidate albumCandidate
			if err := rows.Scan(&candidate.id, &candidate.releaseDate, &candidate.coverID); err != nil {
				rows.Close()
				return fmt.Errorf("scan projected album: %w", err)
			}
			candidates = append(candidates, candidate)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate projected albums: %w", err)
		}
		desiredArtists := make([]string, 0, len(metadata.AlbumArtists))
		for _, name := range metadata.AlbumArtists {
			desiredArtists = append(desiredArtists, artistIDs[normalizeLookup(name)])
		}
		credits := make(map[string][]string)
		if len(candidates) > 0 {
			ids := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				ids = append(ids, candidate.id)
			}
			creditRows, err := tx.Query(ctx, `
				SELECT album_id, artist_id FROM album_artists
				WHERE album_id = ANY($1::uuid[]) AND role = 'PRIMARY'
				ORDER BY album_id, sort_order`, ids)
			if err != nil {
				return fmt.Errorf("query projected album credits: %w", err)
			}
			for creditRows.Next() {
				var candidateID, artistID string
				if err := creditRows.Scan(&candidateID, &artistID); err != nil {
					creditRows.Close()
					return fmt.Errorf("scan projected album credit: %w", err)
				}
				credits[candidateID] = append(credits[candidateID], artistID)
			}
			creditRows.Close()
		}
		selected := ""
		for _, candidate := range candidates {
			if stringSlicesEqual(credits[candidate.id], desiredArtists) {
				if currentAlbumID != nil && candidate.id == *currentAlbumID {
					selected = candidate.id
					break
				}
				if selected == "" {
					selected = candidate.id
				}
			}
		}
		releaseDate := catalogReleaseDate(metadata.ReleaseDate)
		if selected != "" {
			albumID = &selected
			var selectedCandidate albumCandidate
			for _, candidate := range candidates {
				if candidate.id == selected {
					selectedCandidate = candidate
					break
				}
			}
			if selectedCandidate.releaseDate != pointerValue(releaseDate) {
				if _, err := tx.Exec(ctx, `UPDATE albums SET release_date = $2, version = version + 1, updated_at = now() WHERE id = $1`, selected, releaseDate); err != nil {
					return fmt.Errorf("update projected album release date: %w", err)
				}
			}
			if metadata.HasArtwork && currentCoverID != nil && selectedCandidate.coverID == nil {
				if _, err := tx.Exec(ctx, `UPDATE albums SET cover_asset_id = $2, version = version + 1, updated_at = now() WHERE id = $1`, selected, currentCoverID); err != nil {
					return fmt.Errorf("copy projected album artwork: %w", err)
				}
			}
		} else {
			created := uuid.NewString()
			var coverID *string
			if metadata.HasArtwork {
				coverID = currentCoverID
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO albums (id, title, normalized_title, release_date, cover_asset_id)
				VALUES ($1, $2, $3, $4, $5)`, created, *metadata.Album, normalizedTitle, releaseDate, coverID); err != nil {
				return fmt.Errorf("create projected album: %w", err)
			}
			for position, artistID := range desiredArtists {
				if _, err := tx.Exec(ctx, `
					INSERT INTO album_artists (album_id, artist_id, role, sort_order)
					VALUES ($1, $2, 'PRIMARY', $3)`, created, artistID, position); err != nil {
					return fmt.Errorf("create projected album credit: %w", err)
				}
			}
			albumID = &created
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tracks SET title = $2, normalized_title = $3, album_id = $4,
		                  track_number = $5, disc_number = $6,
		                  version = version + 1, updated_at = now()
		WHERE id = $1`, trackID, metadata.Title, normalizeLookup(metadata.Title), albumID, metadata.TrackNumber, metadata.DiscNumber); err != nil {
		return fmt.Errorf("project track metadata: %w", err)
	}
	if currentAlbumID != nil && (albumID == nil || *currentAlbumID != *albumID) {
		if err := repository.deleteEmptyAlbum(ctx, tx, *currentAlbumID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM track_artists WHERE track_id = $1", trackID); err != nil {
		return fmt.Errorf("replace projected track credits: %w", err)
	}
	for position, credit := range metadata.Credits {
		if _, err := tx.Exec(ctx, `
			INSERT INTO track_artists (track_id, artist_id, role, sort_order)
			VALUES ($1, $2, $3, $4)`, trackID, artistIDs[normalizeLookup(credit.Name)], credit.Role, position); err != nil {
			return fmt.Errorf("insert projected track credit: %w", err)
		}
	}
	if previous.Lyrics != nil && (metadata.Lyrics == nil || metadata.Lyrics.Language != previous.Lyrics.Language) {
		if _, err := tx.Exec(ctx, `
			DELETE FROM lyrics WHERE track_id = $1 AND language = $2 AND format = $3
			  AND timing = $4 AND content = $5 AND asset_id IS NULL`,
			trackID, previous.Lyrics.Language, previous.Lyrics.Format, previous.Lyrics.Timing, previous.Lyrics.Content); err != nil {
			return fmt.Errorf("remove previous projected lyrics: %w", err)
		}
	}
	if metadata.Lyrics != nil {
		if _, err := tx.Exec(ctx, "UPDATE lyrics SET is_default = false, updated_at = now() WHERE track_id = $1", trackID); err != nil {
			return fmt.Errorf("clear projected default lyrics: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO lyrics (id, track_id, format, timing, language, origin, content, is_default)
			VALUES ($1, $2, $3, $4, $5, 'SCRAPED', $6, true)
			ON CONFLICT (track_id, language) DO UPDATE SET
				format = EXCLUDED.format, timing = EXCLUDED.timing, content = EXCLUDED.content, origin = 'SCRAPED',
				asset_id = NULL, is_default = true, version = lyrics.version + 1, updated_at = now()`,
			uuid.NewString(), trackID, metadata.Lyrics.Format, metadata.Lyrics.Timing, metadata.Lyrics.Language, metadata.Lyrics.Content); err != nil {
			return fmt.Errorf("upsert projected lyrics: %w", err)
		}
	}
	return nil
}

func (repository *Repository) deleteEmptyAlbum(ctx context.Context, tx pgx.Tx, albumID string) error {
	var coverID *string
	err := tx.QueryRow(ctx, `
		DELETE FROM albums album WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM tracks WHERE album_id = album.id)
		RETURNING cover_asset_id`, albumID).Scan(&coverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete empty projected album: %w", err)
	}
	if coverID == nil {
		return nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE media_assets asset SET status = 'DELETE_PENDING', updated_at = now()
		WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM artists WHERE artwork_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM albums WHERE cover_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM playlists WHERE cover_asset_id = asset.id)
		  AND NOT EXISTS (SELECT 1 FROM user_profiles WHERE avatar_asset_id = asset.id)`, *coverID)
	if err != nil {
		return fmt.Errorf("detach projected album artwork: %w", err)
	}
	return nil
}

func decodeMetadataDocuments(rawJSON, overridesJSON []byte) (MetadataSnapshot, map[string]any, error) {
	var raw MetadataSnapshot
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return MetadataSnapshot{}, nil, fmt.Errorf("decode raw track metadata: %w", err)
	}
	normalizeSnapshot(&raw)
	if err := validateStoredMetadataLyrics(raw); err != nil {
		return MetadataSnapshot{}, nil, err
	}
	overrides := make(map[string]any)
	if len(overridesJSON) > 0 {
		if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
			return MetadataSnapshot{}, nil, fmt.Errorf("decode track metadata overrides: %w", err)
		}
	}
	return raw, overrides, nil
}

func applyOverrides(raw MetadataSnapshot, overrides map[string]any) (MetadataSnapshot, error) {
	document, _ := json.Marshal(raw)
	var combined map[string]any
	if err := json.Unmarshal(document, &combined); err != nil {
		return MetadataSnapshot{}, err
	}
	for field, value := range overrides {
		combined[field] = value
	}
	combined["hasArtwork"] = raw.HasArtwork
	document, _ = json.Marshal(combined)
	var result MetadataSnapshot
	if err := json.Unmarshal(document, &result); err != nil {
		return MetadataSnapshot{}, apperror.Validation("Stored metadata overrides are invalid")
	}
	normalizeSnapshot(&result)
	if err := validateStoredMetadataLyrics(result); err != nil {
		return MetadataSnapshot{}, err
	}
	return result, nil
}

func validateStoredMetadataLyrics(snapshot MetadataSnapshot) error {
	if snapshot.Lyrics == nil {
		return nil
	}
	if err := sharedlyrics.ValidateDocument(
		snapshot.Lyrics.Format,
		snapshot.Lyrics.Timing,
		snapshot.Lyrics.Content,
	); err != nil {
		return apperror.Internal("Stored metadata lyrics violate the timing contract", err)
	}
	return nil
}

func normalizeSnapshot(snapshot *MetadataSnapshot) {
	if snapshot.Credits == nil {
		snapshot.Credits = []MetadataCredit{}
	}
	if snapshot.AlbumArtists == nil {
		snapshot.AlbumArtists = []string{}
	}
	if snapshot.Genres == nil {
		snapshot.Genres = []string{}
	}
}

func changedMetadataFields(previous, next MetadataSnapshot) []string {
	previousDocument, _ := json.Marshal(previous)
	nextDocument, _ := json.Marshal(next)
	var left, right map[string]any
	_ = json.Unmarshal(previousDocument, &left)
	_ = json.Unmarshal(nextDocument, &right)
	fields := make([]string, 0)
	for _, field := range metadataFields {
		if !reflect.DeepEqual(normalizeComparable(left[field]), normalizeComparable(right[field])) {
			fields = append(fields, field)
		}
	}
	return fields
}

func editableMetadataField(field string) bool {
	for _, candidate := range metadataFields {
		if field == candidate {
			return true
		}
	}
	return false
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func sortedMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeLookup(value string) string { return strings.ToLower(normalizeText(value)) }

func catalogReleaseDate(value *string) *string {
	if value == nil || len(*value) == 10 {
		return value
	}
	result := *value
	if len(result) == 4 {
		result += "-01-01"
	} else if len(result) == 7 {
		result += "-01"
	}
	return &result
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolPointerValue(value *bool) bool {
	return value != nil && *value
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
