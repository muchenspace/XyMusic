package adminsources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/adminmetadata"
)

func (synchronizer *ProductionSynchronizer) storeStandardFile(
	ctx context.Context,
	input standardFileMutation,
) (localSourceRecord, bool, error) {
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("begin local library file synchronization: %w", err)
	}
	defer transaction.Rollback(ctx)
	if input.ExistingFound {
		locked, err := scanLocalSource(transaction.QueryRow(ctx, `SELECT `+localSourceColumns+`
			FROM local_music_sources WHERE id=$1 FOR UPDATE`, input.Existing.ID))
		if err != nil {
			return localSourceRecord{}, false, fmt.Errorf("lock local library source: %w", err)
		}
		input.Existing = locked
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
	}
	effective, overridesLyrics, err := effectiveScanMetadata(ctx, transaction, input.TrackID, input.Raw)
	if err != nil {
		return localSourceRecord{}, false, err
	}
	artistIDs, albumArtistIDs, err := resolveMetadataArtists(ctx, transaction, effective)
	if err != nil {
		return localSourceRecord{}, false, err
	}
	var albumID *string
	if effective.Album != nil {
		preferredAlbum, err := currentTrackAlbum(ctx, transaction, input.TrackID)
		if err != nil {
			return localSourceRecord{}, false, err
		}
		value, err := resolveScanAlbum(ctx, transaction, *effective.Album, albumArtistIDs, effective.ReleaseDate, preferredAlbum)
		if err != nil {
			return localSourceRecord{}, false, err
		}
		albumID = &value
	}
	if err := upsertScanTrack(ctx, transaction, input.TrackID, input.ExistingFound, effective, albumID, artistIDs); err != nil {
		return localSourceRecord{}, false, err
	}
	var assetID string
	err = transaction.QueryRow(ctx, `INSERT INTO media_assets(
		object_key,kind,mime_type,size_bytes,checksum_sha256,status
	) VALUES($1,'AUDIO_SOURCE',$2,$3,$4,'READY')
	ON CONFLICT(object_key) DO UPDATE SET size_bytes=EXCLUDED.size_bytes,
		checksum_sha256=EXCLUDED.checksum_sha256,mime_type=EXCLUDED.mime_type,
		status='READY',updated_at=now() RETURNING id`,
		input.ObjectKey, input.MimeType, input.UploadedSize, input.Checksum).Scan(&assetID)
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("store local library source asset: %w", err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET
		status='CANCELLED',cancel_requested=true,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
		last_error_code='SUPERSEDED',last_error='A newer source generation superseded this media job',
		version=version+1,updated_at=now()
		WHERE track_id=$1 AND status IN('PENDING','PROCESSING')`, input.TrackID); err != nil {
		return localSourceRecord{}, false, fmt.Errorf("supersede previous local library media jobs: %w", err)
	}
	var generation int
	if err := transaction.QueryRow(ctx, `UPDATE tracks SET
		media_generation=media_generation+1,version=version+1,updated_at=now()
		WHERE id=$1 RETURNING media_generation`, input.TrackID).Scan(&generation); err != nil {
		return localSourceRecord{}, false, fmt.Errorf("advance local library track generation: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"sourcePath": input.File.RelativePath, "originalFileName": filepath.Base(input.File.AudioPath),
	})
	if err != nil {
		return localSourceRecord{}, false, err
	}
	var jobID string
	err = transaction.QueryRow(ctx, `INSERT INTO media_jobs(
		type,source_asset_id,track_id,generation,idempotency_key,payload,publish_on_ready,scan_run_id
	) VALUES('INGEST_TRACK',$1,$2,$3,$4,$5::jsonb,true,NULLIF($6,'')::uuid) RETURNING id`,
		assetID, input.TrackID, generation,
		fmt.Sprintf("local-library:%s:%d:%s", input.TrackID, generation, input.Checksum), payload, input.ScanRunID,
	).Scan(&jobID)
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("create local library media job: %w", err)
	}
	now := synchronizer.now()
	var source localSourceRecord
	if input.ExistingFound {
		source, err = scanLocalSource(transaction.QueryRow(ctx, `UPDATE local_music_sources SET
			root_id=$2,source_path=$3,normalized_source_path=$4,checksum_sha256=$5,size_bytes=$6,
			modified_at=$7,track_id=$8,source_asset_id=$9,media_job_id=$10,status='PROCESSING',
			last_error=NULL,last_seen_at=$11,updated_at=$12 WHERE id=$1 RETURNING `+localSourceColumns,
			input.Existing.ID, input.RootID, input.File.RelativePath, normalizePlatformPath(input.File.RelativePath),
			input.Checksum, input.Metadata.Size(), input.Metadata.ModTime(), input.TrackID, assetID, jobID, input.SeenAt, now))
	} else {
		source, err = scanLocalSource(transaction.QueryRow(ctx, `INSERT INTO local_music_sources(
			root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,modified_at,
			track_id,source_asset_id,media_job_id,status,last_error,last_seen_at,updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'PROCESSING',NULL,$10,$11) RETURNING `+localSourceColumns,
			input.RootID, input.File.RelativePath, normalizePlatformPath(input.File.RelativePath), input.Checksum,
			input.Metadata.Size(), input.Metadata.ModTime(), input.TrackID, assetID, jobID, input.SeenAt, now))
	}
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("store local library source record: %w", err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO local_music_source_tracks(
		source_id,track_id,media_job_id,segment_index,start_ms,end_ms,cue_path,cue_checksum_sha256
	) VALUES($1,$2,$3,0,0,NULL,NULL,NULL)
	ON CONFLICT(source_id,track_id) DO UPDATE SET media_job_id=EXCLUDED.media_job_id,
		segment_index=0,start_ms=0,end_ms=NULL,cue_path=NULL,cue_checksum_sha256=NULL,updated_at=now()`,
		source.ID, input.TrackID, jobID); err != nil {
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
			if _, err := transaction.Exec(ctx, `UPDATE tracks SET
				status='ARCHIVED',version=version+1,updated_at=now() WHERE id=ANY($1::uuid[])`, staleTrackIDs); err != nil {
				return localSourceRecord{}, false, fmt.Errorf("archive stale CUE tracks: %w", err)
			}
		}
	}
	if input.Existing.SourceAssetID != nil && *input.Existing.SourceAssetID != assetID {
		var staleObjectKey string
		err := transaction.QueryRow(ctx, `UPDATE media_assets SET status='DELETE_PENDING',updated_at=now()
			WHERE id=$1 RETURNING object_key`, *input.Existing.SourceAssetID).Scan(&staleObjectKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return localSourceRecord{}, false, fmt.Errorf("retire replaced local library asset: %w", err)
		}
		if err == nil {
			if err := enqueueCleanupTx(ctx, transaction, staleObjectKey, "REPLACED_LIBRARY_SOURCE"); err != nil {
				return localSourceRecord{}, false, err
			}
		}
	}
	artworkUsed, err := attachAlbumArtwork(ctx, transaction, albumID, input.Artwork)
	if err != nil {
		return localSourceRecord{}, false, err
	}
	if err := recordScanMetadata(ctx, transaction, input.TrackID, source.ID, input.Raw, input.Checksum, input.SeenAt); err != nil {
		return localSourceRecord{}, false, err
	}
	if !overridesLyrics {
		if err := syncScannedLyrics(ctx, transaction, input.TrackID, input.Lyrics); err != nil {
			return localSourceRecord{}, false, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return localSourceRecord{}, false, fmt.Errorf("commit local library file synchronization: %w", err)
	}
	return source, artworkUsed, nil
}

func (synchronizer *ProductionSynchronizer) findSource(
	ctx context.Context,
	rootID, normalizedPath string,
) (localSourceRecord, bool, error) {
	source, err := scanLocalSource(synchronizer.database.QueryRow(ctx, `SELECT `+localSourceColumns+`
		FROM local_music_sources WHERE root_id=$1 AND normalized_source_path=$2`, rootID, normalizedPath))
	if errors.Is(err, pgx.ErrNoRows) {
		return localSourceRecord{}, false, nil
	}
	if err != nil {
		return localSourceRecord{}, false, fmt.Errorf("find local library source: %w", err)
	}
	return source, true, nil
}

func (synchronizer *ProductionSynchronizer) findRenameCandidates(
	ctx context.Context,
	rootID, checksum string,
	seenAt time.Time,
) ([]localSourceRecord, error) {
	rows, err := synchronizer.database.Query(ctx, `SELECT `+localSourceColumns+`
		FROM local_music_sources WHERE root_id=$1 AND checksum_sha256=$2 AND last_seen_at<$3 LIMIT 2`,
		rootID, checksum, seenAt)
	if err != nil {
		return nil, fmt.Errorf("find renamed local library source: %w", err)
	}
	defer rows.Close()
	result := make([]localSourceRecord, 0, 2)
	for rows.Next() {
		source, err := scanLocalSource(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, rows.Err()
}

func scanLocalSource(scanner rowScanner) (localSourceRecord, error) {
	var source localSourceRecord
	err := scanner.Scan(
		&source.ID, &source.RootID, &source.SourcePath, &source.NormalizedPath,
		&source.Checksum, &source.SizeBytes, &source.ModifiedAt, &source.TrackID,
		&source.SourceAssetID, &source.MediaJobID, &source.Status, &source.LastSeenAt,
	)
	return source, err
}

const localSourceColumns = `
	id,root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,modified_at,
	track_id,source_asset_id,media_job_id,status,last_seen_at`

type trackArtistAssignment struct {
	ArtistID string
	Role     adminmetadata.CreditRole
	Order    int
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
	ids, err := resolveScanArtists(ctx, transaction, names)
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
	for _, key := range keys {
		if _, err := transaction.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "artist:"+key); err != nil {
			return nil, fmt.Errorf("lock local library artist: %w", err)
		}
	}
	result := make(map[string]string, len(keys))
	if len(keys) > 0 {
		rows, err := transaction.Query(ctx, `SELECT id,normalized_name FROM artists
			WHERE normalized_name=ANY($1::text[]) ORDER BY id`, keys)
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
			}
		}
		rows.Close()
	}
	for _, key := range keys {
		if result[key] != "" {
			continue
		}
		var id string
		if err := transaction.QueryRow(ctx, `INSERT INTO artists(name,normalized_name)
			VALUES($1,$2) RETURNING id`, displays[key], key).Scan(&id); err != nil {
			return nil, fmt.Errorf("create local library artist: %w", err)
		}
		result[key] = id
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
) (string, error) {
	if len(artistIDs) == 0 {
		return "", errors.New("album artist is required")
	}
	normalizedTitle := normalizeCatalogText(title)
	if _, err := transaction.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "album:"+normalizedTitle); err != nil {
		return "", fmt.Errorf("lock local library album: %w", err)
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
	rows.Close()
	for _, candidate := range preferredFirst(candidates, preferredID) {
		creditRows, err := transaction.Query(ctx, `SELECT artist_id FROM album_artists
			WHERE album_id=$1 AND role='PRIMARY' ORDER BY sort_order,artist_id`, candidate)
		if err != nil {
			return "", err
		}
		credits := make([]string, 0)
		for creditRows.Next() {
			var id string
			if err := creditRows.Scan(&id); err != nil {
				creditRows.Close()
				return "", err
			}
			credits = append(credits, id)
		}
		creditRows.Close()
		if equalStrings(credits, artistIDs) {
			return candidate, nil
		}
	}
	var albumID string
	err = transaction.QueryRow(ctx, `INSERT INTO albums(title,normalized_title,release_date)
		VALUES($1,$2,$3) RETURNING id`, title, normalizedTitle, catalogDate(releaseDate)).Scan(&albumID)
	if err != nil {
		return "", fmt.Errorf("create local library album: %w", err)
	}
	for order, artistID := range artistIDs {
		if _, err := transaction.Exec(ctx, `INSERT INTO album_artists(
			album_id,artist_id,role,sort_order) VALUES($1,$2,'PRIMARY',$3)`, albumID, artistID, order); err != nil {
			return "", fmt.Errorf("link local library album artist: %w", err)
		}
	}
	return albumID, nil
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
	albumID *string,
	artists []trackArtistAssignment,
) error {
	discNumber := metadata.DiscNumber
	if discNumber == nil {
		value := 1
		discNumber = &value
	}
	var previousAlbum *string
	if exists {
		if err := transaction.QueryRow(ctx, `SELECT album_id FROM tracks WHERE id=$1 FOR UPDATE`, trackID).Scan(&previousAlbum); err != nil {
			return fmt.Errorf("lock existing local library track: %w", err)
		}
		if _, err := transaction.Exec(ctx, `UPDATE tracks SET
			title=$2,normalized_title=$3,album_id=$4,track_number=$5,disc_number=$6,
			status=CASE WHEN status='ARCHIVED' THEN status ELSE 'READY' END,
			version=version+1,updated_at=now() WHERE id=$1`,
			trackID, metadata.Title, normalizeCatalogText(metadata.Title), albumID, metadata.TrackNumber, discNumber); err != nil {
			return fmt.Errorf("update local library track: %w", err)
		}
		if _, err := transaction.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1`, trackID); err != nil {
			return fmt.Errorf("replace local library track artists: %w", err)
		}
	} else if _, err := transaction.Exec(ctx, `INSERT INTO tracks(
		id,title,normalized_title,album_id,track_number,disc_number,status
	) VALUES($1,$2,$3,$4,$5,$6,'READY')`,
		trackID, metadata.Title, normalizeCatalogText(metadata.Title), albumID, metadata.TrackNumber, discNumber); err != nil {
		return fmt.Errorf("create local library track: %w", err)
	}
	for _, artist := range artists {
		if _, err := transaction.Exec(ctx, `INSERT INTO track_artists(
			track_id,artist_id,role,sort_order) VALUES($1,$2,$3,$4)`,
			trackID, artist.ArtistID, artist.Role, artist.Order); err != nil {
			return fmt.Errorf("link local library track artist: %w", err)
		}
	}
	if exists && !sameOptionalString(previousAlbum, albumID) {
		if err := deleteAlbumIfEmpty(ctx, transaction, previousAlbum); err != nil {
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
		if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata_revisions(
			track_id,metadata_version,action,raw_tags,overrides,effective_tags,reason
		) VALUES($1,1,'SCAN',$2::jsonb,'{}'::jsonb,$2::jsonb,'Initial metadata captured during library scan')`,
			trackID, rawJSON); err != nil {
			return fmt.Errorf("create initial local library metadata revision: %w", err)
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
		effective, err := adminmetadata.ApplyMetadataOverrides(raw, overrides)
		if err != nil {
			return err
		}
		effectiveJSON, _ := json.Marshal(effective)
		if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata_revisions(
			track_id,metadata_version,action,raw_tags,overrides,effective_tags,reason
		) VALUES($1,$2,'SCAN',$3::jsonb,$4::jsonb,$5::jsonb,'Source tags changed during library scan')`,
			trackID, nextVersion, rawJSON, overridesJSON, effectiveJSON); err != nil {
			return fmt.Errorf("create local library metadata revision: %w", err)
		}
	}
	return nil
}

func syncScannedLyrics(ctx context.Context, transaction pgx.Tx, trackID string, incoming []scannedLyric) error {
	languages := make([]string, 0, len(incoming))
	for _, lyric := range incoming {
		languages = append(languages, lyric.Language)
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
	for _, lyric := range incoming {
		if _, err := transaction.Exec(ctx, `INSERT INTO lyrics(
			track_id,language,format,content,origin,is_default
		) VALUES($1,$2,$3,$4,$5,false)
		ON CONFLICT(track_id,language) DO UPDATE SET format=EXCLUDED.format,content=EXCLUDED.content,
			origin=EXCLUDED.origin,asset_id=NULL,version=lyrics.version+1,updated_at=now()
		WHERE lyrics.origin IN('SCAN','EXTERNAL')`,
			trackID, lyric.Language, lyric.Format, lyric.Content, lyric.Origin); err != nil {
			return fmt.Errorf("store scanned local library lyric: %w", err)
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
	desired := ""
	for _, lyric := range incoming {
		if lyric.IsDefault {
			desired = lyric.Language
			break
		}
	}
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

func (synchronizer *ProductionSynchronizer) syncUnchangedSidecars(
	ctx context.Context,
	source localSourceRecord,
	sidecars []scannedLyric,
	seenAt time.Time,
) error {
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	var rawJSON, overridesJSON []byte
	err = transaction.QueryRow(ctx, `SELECT raw_tags,overrides FROM track_metadata
		WHERE track_id=$1 FOR UPDATE`, source.TrackID).Scan(&rawJSON, &overridesJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return transaction.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var raw adminmetadata.MetadataSnapshot
	var overrides adminmetadata.MetadataOverrides
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
		return err
	}
	if len(sidecars) > 0 {
		selected := sidecars[0]
		for _, lyric := range sidecars {
			if lyric.IsDefault {
				selected = lyric
				break
			}
		}
		raw.Lyrics = &adminmetadata.MetadataLyrics{Content: selected.Content, Format: selected.Format, Language: selected.Language}
	} else {
		raw.Lyrics = nil
	}
	if err := recordScanMetadata(ctx, transaction, source.TrackID, source.ID, raw, source.Checksum, seenAt); err != nil {
		return err
	}
	if _, overridden := overrides[string(adminmetadata.FieldLyrics)]; !overridden {
		if err := syncScannedLyrics(ctx, transaction, source.TrackID, sidecars); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

func (synchronizer *ProductionSynchronizer) sourceHasExternalLyrics(ctx context.Context, sourceID string) (bool, error) {
	var exists bool
	err := synchronizer.database.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM local_music_source_tracks mapping JOIN lyrics lyric ON lyric.track_id=mapping.track_id
		WHERE mapping.source_id=$1 AND lyric.origin='EXTERNAL')`, sourceID).Scan(&exists)
	return exists, err
}
