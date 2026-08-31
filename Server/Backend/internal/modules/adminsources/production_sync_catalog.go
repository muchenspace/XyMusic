package adminsources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/adminmetadata"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

func lockStandardFileSource(
	ctx context.Context,
	transaction pgx.Tx,
	input standardFileMutation,
) (localSourceRecord, bool, error) {
	query := `SELECT ` + localSourceColumnsWithTrack + ` FROM local_music_sources source LEFT JOIN local_music_source_tracks track_link ON track_link.source_id = source.id WHERE `
	var argument any
	var secondArgument any
	if input.ExistingFound {
		query += `source.id=$1 FOR UPDATE OF source`
		argument = input.Existing.ID
	} else {
		query += `source.root_id=$1 AND source.normalized_source_path=$2 FOR UPDATE OF source`
		argument = input.RootID
		secondArgument = normalizePlatformPath(input.File.RelativePath)
	}
	var (
		locked localSourceRecord
		err    error
	)
	if input.ExistingFound {
		locked, err = scanLocalSourceWithTrack(transaction.QueryRow(ctx, query, argument))
	} else {
		locked, err = scanLocalSourceWithTrack(transaction.QueryRow(ctx, query, argument, secondArgument))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if input.ExistingFound {
			return localSourceRecord{}, false, errors.New("local library source disappeared during synchronization")
		}
		return localSourceRecord{}, false, nil
	}
	if err != nil {
		return localSourceRecord{}, false, err
	}
	return locked, true, nil
}

func (synchronizer *ProductionSynchronizer) storeStandardFile(
	ctx context.Context,
	input standardFileMutation,
) (localSourceRecord, bool, error) {
	if transaction := scanTransactionFromContext(ctx); transaction != nil {
		return synchronizer.storeStandardFileInTransaction(ctx, transaction, input)
	}
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("begin local library file synchronization: %w", err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))
	source, artworkUsed, err := synchronizer.storeStandardFileInTransaction(ctx, transaction, input)
	if err != nil {
		return localSourceRecord{}, false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return localSourceRecord{}, false, fmt.Errorf("commit local library file synchronization: %w", err)
	}
	return source, artworkUsed, nil
}

func (synchronizer *ProductionSynchronizer) storeStandardFileInTransaction(
	ctx context.Context,
	transaction pgx.Tx,
	input standardFileMutation,
) (localSourceRecord, bool, error) {
	locked, exists, err := lockStandardFileSource(ctx, transaction, input)
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("lock local library source: %w", err)
	}
	if exists {
		input.Existing = locked
		input.ExistingFound = true
		if locked.RootID != input.RootID {
			return localSourceRecord{}, false, fmt.Errorf("local library source belongs to another music root")
		}
		if locked.TrackID != nil {
			input.TrackID = *locked.TrackID
		}
		pathChanging := locked.RootID != input.RootID ||
			locked.NormalizedPath != normalizePlatformPath(input.File.RelativePath)
		if pathChanging {
			var blocked bool
			if err := transaction.QueryRow(ctx, `SELECT EXISTS(
				SELECT 1 FROM metadata_writeback_jobs
				WHERE source_id=$1 AND status IN ('PENDING','PROCESSING')
			)`, locked.ID).Scan(&blocked); err != nil {
				return localSourceRecord{}, false, fmt.Errorf("check Tag writeback path freeze: %w", err)
			}
			if blocked {
				return localSourceRecord{}, false, fmt.Errorf("Tag writeback keeps the local source path frozen")
			}
		}
		if locked.Status == SourceFileMissing {
			now := synchronizer.now()
			if _, err := transaction.Exec(ctx, `UPDATE tracks track SET
				status=CASE WHEN track.published_at IS NOT NULL AND track.duration_ms>0
				THEN 'READY'::catalog_status ELSE 'ERROR'::catalog_status END,
				version=track.version+1,updated_at=$3
				WHERE track.status='ARCHIVED' AND track.updated_at=$2
				AND EXISTS(
					SELECT 1 FROM local_music_source_tracks mapping
					WHERE mapping.source_id=$1 AND mapping.track_id=track.id
				)
				AND NOT track.archived_manually`, locked.ID, locked.UpdatedAt, now); err != nil {
				return localSourceRecord{}, false, fmt.Errorf("restore incorrectly archived local library tracks: %w", err)
			}
		}
	}
	if input.TrackID == "" {
		input.TrackID = uuid.NewString()
	}
	effective := input.Raw
	var overridesLyrics bool
	if input.ExistingFound {
		effective, overridesLyrics, err = effectiveScanMetadata(ctx, transaction, input.TrackID, input.Raw)
		if err != nil {
			return localSourceRecord{}, false, err
		}
	}
	catalogCache := input.CatalogCache
	if catalogCache == nil {
		catalogCache = newScanCatalogCache()
	}
	artistIDs, albumArtistIDs, err := resolveMetadataArtists(ctx, transaction, effective, catalogCache)
	if err != nil {
		return localSourceRecord{}, false, err
	}
	var albumID *string
	if effective.Album != nil {
		var preferredAlbum *string
		if input.ExistingFound {
			preferredAlbum, err = currentTrackAlbum(ctx, transaction, input.TrackID)
			if err != nil {
				return localSourceRecord{}, false, err
			}
		}
		value, err := resolveScanAlbum(ctx, transaction, *effective.Album, albumArtistIDs, effective.ReleaseDate, preferredAlbum, catalogCache)
		if err != nil {
			return localSourceRecord{}, false, err
		}
		albumID = &value
	}
	var durationMS *int64
	if input.Probed != nil {
		durationMS = input.Probed.DurationMS
	}
	if err := upsertScanTrack(ctx, transaction, input.TrackID, input.ExistingFound, effective, durationMS, albumID, artistIDs, catalogCache); err != nil {
		return localSourceRecord{}, false, err
	}
	now := synchronizer.now()
	var source localSourceRecord
	if input.ExistingFound {
		source, err = scanLocalSource(transaction.QueryRow(ctx, `UPDATE local_music_sources SET
			root_id=$2,source_path=$3,normalized_source_path=$4,checksum_sha256=$5,size_bytes=$6,
			modified_at=$7,status='READY',
			last_error=NULL,last_seen_at=$8,updated_at=$9 WHERE id=$1 RETURNING `+localSourceColumns,
			input.Existing.ID, input.RootID, input.File.RelativePath, normalizePlatformPath(input.File.RelativePath),
			input.Checksum, input.Metadata.Size(), input.Metadata.ModTime(), input.SeenAt, now))
	} else {
		source, err = scanLocalSource(transaction.QueryRow(ctx, `INSERT INTO local_music_sources(
			root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,modified_at,
			status,last_error,last_seen_at,updated_at
		) VALUES($1,$2,$3,$4,$5,$6,'READY',NULL,$7,$8) RETURNING `+localSourceColumns,
			input.RootID, input.File.RelativePath, normalizePlatformPath(input.File.RelativePath), input.Checksum,
			input.Metadata.Size(), input.Metadata.ModTime(), input.SeenAt, now))
	}
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("store local library source record: %w", err)
	}
	source.TrackID = &input.TrackID
	if _, err := transaction.Exec(ctx, `INSERT INTO local_music_source_tracks(
		source_id,track_id
	) VALUES($1,$2)
	ON CONFLICT(source_id,track_id) DO NOTHING`,
		source.ID, input.TrackID); err != nil {
		return localSourceRecord{}, false, fmt.Errorf("link local library source track: %w", err)
	}
	if !input.PreserveCueMappings {
		rows, err := transaction.Query(ctx, `DELETE FROM local_music_source_tracks
			WHERE source_id=$1 AND track_id<>$2 RETURNING track_id`, source.ID, input.TrackID)
		if err != nil {
			return localSourceRecord{}, false, fmt.Errorf("remove stale CUE source mappings: %w", err)
		}
		staleTrackIDs := make([]string, 0)
		for rows.Next() {
			var trackID string
			if err := rows.Scan(&trackID); err != nil {
				rows.Close()
				return localSourceRecord{}, false, err
			}
			staleTrackIDs = append(staleTrackIDs, trackID)
		}
		rows.Close()
		if len(staleTrackIDs) > 0 {
			if _, err := deleteOrphanedTracks(ctx, transaction, staleTrackIDs); err != nil {
				return localSourceRecord{}, false, fmt.Errorf("delete stale CUE tracks: %w", err)
			}
		}
	}
	artworkUsed, err := attachAlbumArtwork(ctx, transaction, albumID, input.Artwork)
	if err != nil {
		return localSourceRecord{}, false, err
	}
	if input.ExistingFound {
		err = recordScanMetadata(ctx, transaction, input.TrackID, source.ID, input.Raw, input.Checksum, input.SeenAt)
	} else {
		err = recordNewScanMetadata(ctx, transaction, input.TrackID, source.ID, input.Raw, input.Checksum, input.SeenAt)
	}
	if err != nil {
		return localSourceRecord{}, false, err
	}
	if !overridesLyrics {
		var lyricErr error
		if input.ExistingFound {
			lyricErr = syncScannedLyrics(ctx, transaction, input.TrackID, input.Lyrics)
		} else {
			lyricErr = syncNewScannedLyrics(ctx, transaction, input.TrackID, input.Lyrics)
		}
		if lyricErr != nil {
			return localSourceRecord{}, false, lyricErr
		}
	}
	return source, artworkUsed, nil
}

func recordNewScanMetadata(
	ctx context.Context,
	transaction pgx.Tx,
	trackID, sourceID string,
	raw adminmetadata.MetadataSnapshot,
	checksum string,
	scannedAt time.Time,
) error {
	rawJSON, err := encodeScanMetadata(raw)
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata(
		track_id,source_id,raw_tags,overrides,raw_checksum_sha256,last_scanned_at
	) VALUES($1,$2,$3::jsonb,'{}'::jsonb,$4,$5)`, trackID, sourceID, rawJSON, checksum, scannedAt); err != nil {
		return fmt.Errorf("create scanned local library metadata: %w", err)
	}
	return nil
}

func encodeScanMetadata(raw adminmetadata.MetadataSnapshot) ([]byte, error) {
	if raw.Lyrics != nil {
		if err := sharedlyrics.ValidateDocument(raw.Lyrics.Format, raw.Lyrics.Timing, raw.Lyrics.Content); err != nil {
			return nil, fmt.Errorf("validate scanned local library metadata lyrics: %w", err)
		}
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode scanned local library metadata: %w", err)
	}
	return rawJSON, nil
}

func syncNewScannedLyrics(
	ctx context.Context,
	transaction pgx.Tx,
	trackID string,
	incoming []scannedLyric,
) error {
	if len(incoming) == 0 {
		return nil
	}
	languages := make([]string, len(incoming))
	formats := make([]string, len(incoming))
	timings := make([]string, len(incoming))
	contents := make([]string, len(incoming))
	origins := make([]string, len(incoming))
	defaults := make([]bool, len(incoming))
	for index, lyric := range incoming {
		if err := sharedlyrics.ValidateDocument(lyric.Format, lyric.Timing, lyric.Content); err != nil {
			return fmt.Errorf("validate scanned lyric %d: %w", index, err)
		}
		languages[index], formats[index], timings[index] = lyric.Language, lyric.Format, string(lyric.Timing)
		contents[index], origins[index], defaults[index] = lyric.Content, lyric.Origin, lyric.IsDefault
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO lyrics(
		track_id,language,format,timing,content,origin,is_default)
		SELECT $1,lyric.language,lyric.format::lyrics_format,lyric.timing::lyrics_timing,
			lyric.content,lyric.origin::lyrics_origin,lyric.is_default
		FROM unnest($2::text[],$3::text[],$4::text[],$5::text[],$6::text[],$7::bool[])
			AS lyric(language,format,timing,content,origin,is_default)`,
		trackID, languages, formats, timings, contents, origins, defaults); err != nil {
		return fmt.Errorf("store scanned local library lyrics: %w", err)
	}
	return nil
}

type trackArtistAssignment struct {
	ArtistID string
	Role     adminmetadata.CreditRole
	Order    int
}

// scanCatalogCache is intentionally scoped to one write transaction. It can
// reuse rows created earlier in a multi-track CUE transaction without making
// uncommitted IDs visible to another scan.
type scanCatalogCache struct {
	artistIDs map[string]string
	albumIDs  map[string]string
}

func newScanCatalogCache() *scanCatalogCache {
	return &scanCatalogCache{
		artistIDs: make(map[string]string),
		albumIDs:  make(map[string]string),
	}
}

func (cache *scanCatalogCache) merge(other *scanCatalogCache) {
	if cache == nil || other == nil {
		return
	}
	for key, id := range other.artistIDs {
		cache.artistIDs[key] = id
	}
	for key, id := range other.albumIDs {
		cache.albumIDs[key] = id
	}
}

func (cache *scanCatalogCache) forgetAlbum(albumID string) {
	if cache == nil || albumID == "" {
		return
	}
	for key, cachedID := range cache.albumIDs {
		if cachedID == albumID {
			delete(cache.albumIDs, key)
		}
	}
}

func effectiveScanMetadata(
	ctx context.Context,
	transaction pgx.Tx,
	trackID string,
	raw adminmetadata.MetadataSnapshot,
) (adminmetadata.MetadataSnapshot, bool, error) {
	var encoded []byte
	err := transaction.QueryRow(ctx,
		`SELECT overrides FROM track_metadata WHERE track_id=$1 FOR UPDATE`, trackID).Scan(&encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return raw, false, nil
	}
	if err != nil {
		return adminmetadata.MetadataSnapshot{}, false, fmt.Errorf("lock local library metadata overrides: %w", err)
	}
	var overrides adminmetadata.MetadataOverrides
	if err := json.Unmarshal(encoded, &overrides); err != nil {
		return adminmetadata.MetadataSnapshot{}, false, fmt.Errorf("decode local library metadata overrides: %w", err)
	}
	effective, err := adminmetadata.ApplyMetadataOverrides(raw, overrides)
	if err != nil {
		return adminmetadata.MetadataSnapshot{}, false, fmt.Errorf("apply local library metadata overrides: %w", err)
	}
	_, overridesLyrics := overrides[string(adminmetadata.FieldLyrics)]
	return effective, overridesLyrics, nil
}

func resolveMetadataArtists(
	ctx context.Context,
	transaction pgx.Tx,
	metadata adminmetadata.MetadataSnapshot,
	cache *scanCatalogCache,
) ([]trackArtistAssignment, []string, error) {
	credits := metadata.Credits
	if len(credits) == 0 {
		credits = []adminmetadata.MetadataCredit{{Name: "Unknown Artist", Role: adminmetadata.CreditPrimary}}
	}
	albumArtists := metadata.AlbumArtists
	if len(albumArtists) == 0 {
		for _, credit := range credits {
			if credit.Role == adminmetadata.CreditPrimary {
				albumArtists = append(albumArtists, credit.Name)
			}
		}
	}
	if len(albumArtists) == 0 {
		albumArtists = []string{"Unknown Artist"}
	}
	names := make([]string, 0, len(credits)+len(albumArtists))
	for _, credit := range credits {
		names = append(names, credit.Name)
	}
	names = append(names, albumArtists...)
	ids, err := resolveScanArtists(ctx, transaction, names, cache)
	if err != nil {
		return nil, nil, err
	}
	assignments := make([]trackArtistAssignment, 0, len(credits))
	roleOrders := make(map[adminmetadata.CreditRole]int)
	seenCredits := make(map[string]struct{})
	for _, credit := range credits {
		artistID := ids[normalizeCatalogText(credit.Name)]
		key := artistID + ":" + string(credit.Role)
		if artistID == "" {
			return nil, nil, fmt.Errorf("artist was not resolved: %s", credit.Name)
		}
		if _, duplicate := seenCredits[key]; duplicate {
			continue
		}
		seenCredits[key] = struct{}{}
		assignments = append(assignments, trackArtistAssignment{
			ArtistID: artistID, Role: credit.Role, Order: roleOrders[credit.Role],
		})
		roleOrders[credit.Role]++
	}
	albumArtistIDs := make([]string, 0, len(albumArtists))
	seenAlbumArtists := make(map[string]struct{})
	for _, name := range albumArtists {
		id := ids[normalizeCatalogText(name)]
		if id == "" {
			return nil, nil, fmt.Errorf("album artist was not resolved: %s", name)
		}
		if _, duplicate := seenAlbumArtists[id]; !duplicate {
			seenAlbumArtists[id] = struct{}{}
			albumArtistIDs = append(albumArtistIDs, id)
		}
	}
	return assignments, albumArtistIDs, nil
}

func resolveScanArtists(
	ctx context.Context,
	transaction pgx.Tx,
	names []string,
	cache *scanCatalogCache,
) (map[string]string, error) {
	displays := make(map[string]string)
	for _, name := range names {
		display := strings.Join(strings.Fields(norm.NFKC.String(name)), " ")
		key := normalizeCatalogText(display)
		if key != "" {
			if _, exists := displays[key]; !exists {
				displays[key] = display
			}
		}
	}
	keys := make([]string, 0, len(displays))
	for key := range displays {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	missingKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if cache != nil {
			if id := cache.artistIDs[key]; id != "" {
				result[key] = id
				continue
			}
		}
		if batchCache := scanBatchCatalogFromContext(ctx); batchCache != nil {
			if id := batchCache.artistIDs[key]; id != "" {
				result[key] = id
				if cache != nil {
					cache.artistIDs[key] = id
				}
				continue
			}
		}
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			if id, exists := snapshot.catalogArtist(key); exists {
				result[key] = id
				if cache != nil {
					cache.artistIDs[key] = id
				}
				continue
			}
		}
		missingKeys = append(missingKeys, key)
	}
	if len(missingKeys) > 0 {
		lockKeys := make([]string, 0, len(missingKeys))
		for _, key := range missingKeys {
			lockKeys = append(lockKeys, "artist:"+key)
		}
		var lockedCount int64
		if err := transaction.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT pg_advisory_xact_lock(hashtextextended(lock_key,0))
		FROM unnest($1::text[]) AS requested(lock_key)
	) AS locks`, lockKeys).Scan(&lockedCount); err != nil {
			return nil, fmt.Errorf("lock local library artists: %w", err)
		}
		if lockedCount != int64(len(missingKeys)) {
			return nil, fmt.Errorf("lock local library artists: locked %d of %d", lockedCount, len(missingKeys))
		}
		rows, err := transaction.Query(ctx, `SELECT id,normalized_name FROM artists
			WHERE normalized_name=ANY($1::text[]) ORDER BY id`, missingKeys)
		if err != nil {
			return nil, fmt.Errorf("find local library artists: %w", err)
		}
		for rows.Next() {
			var id, key string
			if err := rows.Scan(&id, &key); err != nil {
				rows.Close()
				return nil, err
			}
			if result[key] == "" {
				result[key] = id
				if cache != nil {
					cache.artistIDs[key] = id
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate local library artists: %w", err)
		}
		rows.Close()
	}
	newKeys := make([]string, 0, len(missingKeys))
	newNames := make([]string, 0, len(missingKeys))
	for _, key := range missingKeys {
		if result[key] != "" {
			continue
		}
		newKeys = append(newKeys, key)
		newNames = append(newNames, displays[key])
	}
	if len(newKeys) > 0 {
		rows, err := transaction.Query(ctx, `INSERT INTO artists(name,normalized_name)
			SELECT requested.display_name,requested.normalized_name
			FROM unnest($1::text[],$2::text[]) AS requested(display_name,normalized_name)
			RETURNING id,normalized_name`, newNames, newKeys)
		if err != nil {
			return nil, fmt.Errorf("create local library artists: %w", err)
		}
		for rows.Next() {
			var id, key string
			if err := rows.Scan(&id, &key); err != nil {
				rows.Close()
				return nil, err
			}
			result[key] = id
			if cache != nil {
				cache.artistIDs[key] = id
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate created local library artists: %w", err)
		}
		rows.Close()
		for _, key := range newKeys {
			if result[key] == "" {
				return nil, fmt.Errorf("created local library artist has no id: %s", key)
			}
		}
	}
	return result, nil
}

func resolveScanAlbum(
	ctx context.Context,
	transaction pgx.Tx,
	title string,
	artistIDs []string,
	releaseDate *string,
	preferredID *string,
	cache *scanCatalogCache,
) (string, error) {
	if len(artistIDs) == 0 {
		return "", errors.New("album artist is required")
	}
	normalizedTitle := normalizeCatalogText(title)
	cacheKey := scanAlbumCacheKey(normalizedTitle, artistIDs, preferredID)
	if cache != nil {
		if albumID := cache.albumIDs[cacheKey]; albumID != "" {
			return albumID, nil
		}
	}
	if batchCache := scanBatchCatalogFromContext(ctx); batchCache != nil {
		if albumID := batchCache.albumIDs[cacheKey]; albumID != "" {
			if cache != nil {
				cache.albumIDs[cacheKey] = albumID
			}
			return albumID, nil
		}
	}
	catalogLockHeld := false
	if preferredID == nil {
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			if _, err := transaction.Exec(ctx,
				`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "album:"+normalizedTitle); err != nil {
				return "", fmt.Errorf("lock local library album: %w", err)
			}
			catalogLockHeld = true
			if albumID, exists := snapshot.catalogAlbum(cacheKey); exists {
				valid, err := validateCachedScanAlbum(ctx, transaction, albumID, normalizedTitle, artistIDs)
				if err != nil {
					return "", err
				}
				if valid {
					if cache != nil {
						cache.albumIDs[cacheKey] = albumID
					}
					return albumID, nil
				}
				snapshot.forgetAlbum(albumID)
			}
		}
	}
	if !catalogLockHeld {
		if _, err := transaction.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "album:"+normalizedTitle); err != nil {
			return "", fmt.Errorf("lock local library album: %w", err)
		}
	}
	rows, err := transaction.Query(ctx, `SELECT id FROM albums
		WHERE normalized_title=$1 ORDER BY id FOR UPDATE`, normalizedTitle)
	if err != nil {
		return "", fmt.Errorf("find local library albums: %w", err)
	}
	candidates := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		candidates = append(candidates, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", fmt.Errorf("iterate local library albums: %w", err)
	}
	rows.Close()
	artistCredits := make(map[string][]string, len(candidates))
	if len(candidates) > 0 {
		creditRows, err := transaction.Query(ctx, `SELECT album_id,artist_id FROM album_artists
			WHERE album_id=ANY($1::uuid[]) AND role='PRIMARY'
			ORDER BY album_id,sort_order,artist_id`, candidates)
		if err != nil {
			return "", fmt.Errorf("find local library album artists: %w", err)
		}
		for creditRows.Next() {
			var albumID, artistID string
			if err := creditRows.Scan(&albumID, &artistID); err != nil {
				creditRows.Close()
				return "", err
			}
			artistCredits[albumID] = append(artistCredits[albumID], artistID)
		}
		if err := creditRows.Err(); err != nil {
			creditRows.Close()
			return "", fmt.Errorf("iterate local library album artists: %w", err)
		}
		creditRows.Close()
	}
	for _, candidate := range preferredFirst(candidates, preferredID) {
		if equalStrings(artistCredits[candidate], artistIDs) {
			if cache != nil {
				cache.albumIDs[cacheKey] = candidate
			}
			return candidate, nil
		}
	}
	var albumID string
	err = transaction.QueryRow(ctx, `INSERT INTO albums(title,normalized_title,release_date)
		VALUES($1,$2,$3) RETURNING id`, title, normalizedTitle, catalogDate(releaseDate)).Scan(&albumID)
	if err != nil {
		return "", fmt.Errorf("create local library album: %w", err)
	}
	artistOrders := make([]int32, len(artistIDs))
	for order := range artistIDs {
		artistOrders[order] = int32(order)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO album_artists(
		album_id,artist_id,role,sort_order)
		SELECT $1,requested.artist_id,'PRIMARY'::artist_credit_role,requested.sort_order
		FROM unnest($2::uuid[],$3::int4[]) AS requested(artist_id,sort_order)`,
		albumID, artistIDs, artistOrders); err != nil {
		return "", fmt.Errorf("link local library album artists: %w", err)
	}
	if cache != nil {
		cache.albumIDs[cacheKey] = albumID
	}
	return albumID, nil
}

func validateCachedScanAlbum(
	ctx context.Context,
	transaction pgx.Tx,
	albumID, normalizedTitle string,
	artistIDs []string,
) (bool, error) {
	var foundID string
	err := transaction.QueryRow(ctx, `SELECT album.id
		FROM albums album
		WHERE album.id=$1 AND album.normalized_title=$2
		AND ARRAY(SELECT link.artist_id
			FROM album_artists link WHERE link.album_id=album.id AND link.role='PRIMARY'
			ORDER BY link.sort_order,link.artist_id)
			=$3::uuid[] FOR UPDATE OF album`, albumID, normalizedTitle, artistIDs).Scan(&foundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate cached local library album: %w", err)
	}
	return foundID == albumID, nil
}

func scanAlbumCacheKey(normalizedTitle string, artistIDs []string, preferredID *string) string {
	key := normalizedTitle + "\x00" + strings.Join(artistIDs, "\x00")
	if preferredID != nil {
		key += "\x00preferred:" + *preferredID
	}
	return key
}

func currentTrackAlbum(ctx context.Context, transaction pgx.Tx, trackID string) (*string, error) {
	var albumID *string
	err := transaction.QueryRow(ctx, `SELECT album_id FROM tracks WHERE id=$1`, trackID).Scan(&albumID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return albumID, err
}

func upsertScanTrack(
	ctx context.Context,
	transaction pgx.Tx,
	trackID string,
	exists bool,
	metadata adminmetadata.MetadataSnapshot,
	durationMS *int64,
	albumID *string,
	artists []trackArtistAssignment,
	cache *scanCatalogCache,
) error {
	discNumber := metadata.DiscNumber
	if discNumber == nil {
		value := 1
		discNumber = &value
	}
	var previousAlbum *string
	dur := int64(0)
	if durationMS != nil && *durationMS > 0 {
		dur = *durationMS
	}
	if exists {
		if err := transaction.QueryRow(ctx, `WITH current AS (
			SELECT album_id FROM tracks WHERE id=$1 FOR UPDATE
		) UPDATE tracks AS track SET
			title=$2,normalized_title=$3,album_id=$4,track_number=$5,disc_number=$6,duration_ms=$7,
			status=CASE WHEN status='ARCHIVED' THEN status ELSE 'READY' END,
			-- Local source synchronization is the authoritative readiness boundary
			-- now that playback transcodes on demand. The old media worker used
			-- to fill published_at after generating variants; without that worker
			-- scanned tracks would remain in the derived PROCESSING state forever.
			published_at=CASE WHEN status='ARCHIVED' THEN published_at ELSE COALESCE(published_at, now()) END,
			version=version+1,updated_at=now()
			FROM current WHERE track.id=$1 RETURNING current.album_id`,
			trackID, metadata.Title, normalizeCatalogText(metadata.Title), albumID, metadata.TrackNumber, discNumber, dur).Scan(&previousAlbum); err != nil {
			return fmt.Errorf("update local library track: %w", err)
		}
	} else if _, err := transaction.Exec(ctx, `INSERT INTO tracks(
		id,title,normalized_title,album_id,track_number,disc_number,duration_ms,status,published_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,'READY',now())`,
		trackID, metadata.Title, normalizeCatalogText(metadata.Title), albumID, metadata.TrackNumber, discNumber, dur); err != nil {
		return fmt.Errorf("create local library track: %w", err)
	}
	artistIDs := make([]string, len(artists))
	roles := make([]string, len(artists))
	artistOrders := make([]int32, len(artists))
	for index, artist := range artists {
		artistIDs[index] = artist.ArtistID
		roles[index] = string(artist.Role)
		artistOrders[index] = int32(artist.Order)
	}
	// Data-modifying CTEs share the statement snapshot with the INSERT. Using
	// `WITH deleted AS (DELETE ...) INSERT ...` can therefore see the old
	// track_artists row and fail with a duplicate-key error on rescans. Delete
	// and insert as separate commands in the same transaction so the new
	// artist links are visible to the INSERT.
	if exists {
		if _, err := transaction.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1`, trackID); err != nil {
			return fmt.Errorf("clear local library track artists: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO track_artists(track_id,artist_id,role,sort_order)
		SELECT $1,requested.artist_id,requested.role::artist_credit_role,requested.sort_order
		FROM unnest($2::uuid[],$3::text[],$4::int4[]) AS requested(artist_id,role,sort_order)`,
		trackID, artistIDs, roles, artistOrders); err != nil {
		return fmt.Errorf("link local library track artists: %w", err)
	}
	if exists && !sameOptionalString(previousAlbum, albumID) {
		if err := deleteAlbumIfEmpty(ctx, transaction, previousAlbum, cache); err != nil {
			return err
		}
	}
	return nil
}

func recordScanMetadata(
	ctx context.Context,
	transaction pgx.Tx,
	trackID, sourceID string,
	raw adminmetadata.MetadataSnapshot,
	checksum string,
	scannedAt time.Time,
) error {
	if raw.Lyrics != nil {
		if err := sharedlyrics.ValidateDocument(raw.Lyrics.Format, raw.Lyrics.Timing, raw.Lyrics.Content); err != nil {
			return fmt.Errorf("validate scanned local library metadata lyrics: %w", err)
		}
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encode scanned local library metadata: %w", err)
	}
	var existingRaw, overridesJSON []byte
	var existingSource, existingChecksum *string
	var version int
	err = transaction.QueryRow(ctx, `SELECT raw_tags,overrides,source_id,raw_checksum_sha256,version
		FROM track_metadata WHERE track_id=$1 FOR UPDATE`, trackID).Scan(
		&existingRaw, &overridesJSON, &existingSource, &existingChecksum, &version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata(
			track_id,source_id,raw_tags,overrides,raw_checksum_sha256,last_scanned_at
		) VALUES($1,$2,$3::jsonb,'{}'::jsonb,$4,$5)`, trackID, sourceID, rawJSON, checksum, scannedAt); err != nil {
			return fmt.Errorf("create scanned local library metadata: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock scanned local library metadata: %w", err)
	}
	var previousRaw adminmetadata.MetadataSnapshot
	if err := json.Unmarshal(existingRaw, &previousRaw); err != nil {
		return fmt.Errorf("decode previous local library metadata: %w", err)
	}
	var overrides adminmetadata.MetadataOverrides
	if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
		return fmt.Errorf("decode local library metadata overrides: %w", err)
	}
	changed := !adminmetadata.MetadataSnapshotsEqual(previousRaw, raw) ||
		existingSource == nil || *existingSource != sourceID || existingChecksum == nil || *existingChecksum != checksum
	nextVersion := version
	if changed {
		nextVersion++
	}
	if _, err := transaction.Exec(ctx, `UPDATE track_metadata SET
		source_id=$2,raw_tags=$3::jsonb,raw_checksum_sha256=$4,last_scanned_at=$5,
		version=$6,updated_at=now() WHERE track_id=$1`,
		trackID, sourceID, rawJSON, checksum, scannedAt, nextVersion); err != nil {
		return fmt.Errorf("update scanned local library metadata: %w", err)
	}
	if changed {
	}
	return nil
}

func syncScannedLyrics(ctx context.Context, transaction pgx.Tx, trackID string, incoming []scannedLyric) error {
	desired := ""
	for index, lyric := range incoming {
		if err := sharedlyrics.ValidateDocument(lyric.Format, lyric.Timing, lyric.Content); err != nil {
			return fmt.Errorf("validate scanned lyric %d: %w", index, err)
		}
		if desired == "" && lyric.IsDefault {
			desired = lyric.Language
		}
	}
	uniqueIncoming := make([]scannedLyric, 0, len(incoming))
	positions := make(map[string]int, len(incoming))
	for _, lyric := range incoming {
		if position, exists := positions[lyric.Language]; exists {
			uniqueIncoming[position] = lyric
			continue
		}
		positions[lyric.Language] = len(uniqueIncoming)
		uniqueIncoming = append(uniqueIncoming, lyric)
	}
	incoming = uniqueIncoming
	languages := make([]string, 0, len(incoming))
	formats := make([]string, 0, len(incoming))
	timings := make([]string, 0, len(incoming))
	contents := make([]string, 0, len(incoming))
	origins := make([]string, 0, len(incoming))
	for _, lyric := range incoming {
		languages = append(languages, lyric.Language)
		formats = append(formats, lyric.Format)
		timings = append(timings, string(lyric.Timing))
		contents = append(contents, lyric.Content)
		origins = append(origins, lyric.Origin)
	}
	if len(languages) == 0 {
		if _, err := transaction.Exec(ctx, `DELETE FROM lyrics
			WHERE track_id=$1 AND origin IN('SCAN','EXTERNAL')`, trackID); err != nil {
			return fmt.Errorf("remove stale scanned lyrics: %w", err)
		}
	} else if _, err := transaction.Exec(ctx, `DELETE FROM lyrics
		WHERE track_id=$1 AND origin IN('SCAN','EXTERNAL') AND NOT(language=ANY($2::text[]))`, trackID, languages); err != nil {
		return fmt.Errorf("remove stale scanned lyric languages: %w", err)
	}
	if len(incoming) > 0 {
		if _, err := transaction.Exec(ctx, `INSERT INTO lyrics(
			track_id,language,format,timing,content,origin,is_default)
			SELECT $1,lyric.language,lyric.format::lyrics_format,lyric.timing::lyrics_timing,
				lyric.content,lyric.origin::lyrics_origin,false
			FROM unnest($2::text[],$3::text[],$4::text[],$5::text[],$6::text[])
				AS lyric(language,format,timing,content,origin)
			ON CONFLICT(track_id,language) DO UPDATE SET format=EXCLUDED.format,timing=EXCLUDED.timing,
				content=EXCLUDED.content,origin=EXCLUDED.origin,asset_id=NULL,
				version=lyrics.version+1,updated_at=now()
			WHERE lyrics.origin IN('SCAN','EXTERNAL')`,
			trackID, languages, formats, timings, contents, origins); err != nil {
			return fmt.Errorf("store scanned local library lyrics: %w", err)
		}
	}
	var protected bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lyrics
		WHERE track_id=$1 AND is_default=true AND origin IN('MANUAL','SCRAPED'))`, trackID).Scan(&protected); err != nil {
		return err
	}
	if protected {
		return nil
	}
	var selectedID string
	err := transaction.QueryRow(ctx, `SELECT id FROM lyrics WHERE track_id=$1
		ORDER BY CASE WHEN language=$2 THEN 0 ELSE 1 END,created_at,id LIMIT 1`, trackID, desired).Scan(&selectedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `UPDATE lyrics SET is_default=false,updated_at=now() WHERE track_id=$1`, trackID); err != nil {
		return err
	}
	_, err = transaction.Exec(ctx, `UPDATE lyrics SET is_default=true,updated_at=now() WHERE id=$1`, selectedID)
	return err
}
