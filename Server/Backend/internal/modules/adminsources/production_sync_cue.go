package adminsources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/modules/adminmetadata"
)

type cueMapping struct {
	TrackID    string
	MediaJobID *string
	Segment    int
	StartMS    int
	EndMS      *int
	CuePath    *string
	Checksum   *string
}

func (synchronizer *ProductionSynchronizer) syncCueFile(
	ctx context.Context,
	rootID string,
	scanRunID string,
	file DiscoveredFile,
	seenAt time.Time,
) error {
	content, err := os.ReadFile(file.CuePath)
	if err != nil {
		return err
	}
	sheet, err := parseCueSheet(string(content))
	if err != nil {
		return err
	}
	audioAbsolute, err := filepath.Abs(file.AudioPath)
	if err != nil {
		return err
	}
	tracks := make([]cueTrack, 0)
	for _, track := range sheet.Tracks {
		candidate, err := filepath.Abs(filepath.Join(filepath.Dir(file.CuePath), track.File))
		if err == nil && normalizePlatformPath(candidate) == normalizePlatformPath(audioAbsolute) {
			tracks = append(tracks, track)
		}
	}
	if len(tracks) == 0 {
		return errors.New("CUE contains no tracks for the referenced audio file")
	}
	checksumBytes := sha256.Sum256(content)
	cueChecksum := hex.EncodeToString(checksumBytes[:])
	rootPath, err := synchronizer.rootPath(ctx, rootID)
	if err != nil {
		return err
	}
	relativeCue := relativeLibraryPath(rootPath, file.CuePath)
	audioInfo, err := os.Stat(file.AudioPath)
	if err != nil {
		return err
	}
	existing, found, err := synchronizer.findSource(ctx, rootID, normalizePlatformPath(file.RelativePath))
	if err != nil {
		return err
	}
	if found && existing.SizeBytes == audioInfo.Size() &&
		existing.ModifiedAt.UnixMilli() == audioInfo.ModTime().UnixMilli() {
		reusable, err := synchronizer.readySourceAssetReusable(ctx, existing)
		if err != nil {
			return err
		}
		if reusable {
			mappings, err := synchronizer.sourceMappings(ctx, existing.ID, false)
			if err != nil {
				return err
			}
			if cueMappingsMatch(mappings, tracks, relativeCue, cueChecksum) {
				_, err := synchronizer.database.Exec(ctx, `UPDATE local_music_sources SET
					last_seen_at=$2,updated_at=$3 WHERE id=$1`, existing.ID, seenAt, synchronizer.now())
				return err
			}
		}
	}
	source, err := synchronizer.syncStandardFile(ctx, rootID, scanRunID, file, seenAt, true)
	if err != nil {
		return err
	}
	if source.SourceAssetID == nil {
		return errors.New("CUE source asset was not created")
	}
	probed, err := synchronizer.probe.Probe(ctx, file.AudioPath)
	if err != nil {
		return err
	}
	artwork, err := synchronizer.stageArtwork(ctx, file.AudioPath, probed.Metadata.HasArtwork)
	if err != nil {
		return err
	}
	used, err := synchronizer.storeCueTracks(ctx, cueMutation{
		RootID: rootID, ScanRunID: scanRunID, Source: source, File: file, Sheet: sheet, Tracks: tracks,
		CuePath: relativeCue, CueChecksum: cueChecksum, Base: probed.Metadata,
		SeenAt: seenAt, Artwork: artwork,
	})
	if err != nil {
		if artwork != nil {
			_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), artwork.ObjectKey, "UNUSED_CUE_ARTWORK")
		}
		return err
	}
	if artwork != nil && !used {
		_ = synchronizer.enqueueCleanup(context.WithoutCancel(ctx), artwork.ObjectKey, "UNUSED_CUE_ARTWORK")
	}
	return nil
}

type cueMutation struct {
	RootID      string
	ScanRunID   string
	Source      localSourceRecord
	File        DiscoveredFile
	Sheet       cueSheet
	Tracks      []cueTrack
	CuePath     string
	CueChecksum string
	Base        adminmetadata.MetadataSnapshot
	SeenAt      time.Time
	Artwork     *stagedArtwork
}

func (synchronizer *ProductionSynchronizer) storeCueTracks(ctx context.Context, input cueMutation) (bool, error) {
	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin CUE source synchronization: %w", err)
	}
	defer transaction.Rollback(ctx)
	mappings, err := sourceMappingsTx(ctx, transaction, input.Source.ID, true)
	if err != nil {
		return false, err
	}
	trackIDs := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		trackIDs = append(trackIDs, mapping.TrackID)
	}
	if len(trackIDs) > 0 {
		if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET
			status='CANCELLED',cancel_requested=true,locked_by=NULL,locked_until=NULL,heartbeat_at=NULL,
			last_error_code='SUPERSEDED',last_error='A newer CUE definition superseded this media job',
			version=version+1,updated_at=now()
			WHERE track_id=ANY($1::uuid[]) AND status IN('PENDING','PROCESSING')`, trackIDs); err != nil {
			return false, fmt.Errorf("supersede previous CUE media jobs: %w", err)
		}
	}
	bySegment := make(map[int]cueMapping, len(mappings))
	for _, mapping := range mappings {
		bySegment[mapping.Segment] = mapping
	}
	newTrackIDs := make([]string, 0, len(input.Tracks))
	jobIDs := make([]string, 0, len(input.Tracks))
	var firstAlbumID *string
	for index, cueTrack := range input.Tracks {
		mapping, mapped := bySegment[index]
		trackID := uuid.NewString()
		if mapped {
			trackID = mapping.TrackID
		} else if index == 0 {
			trackID = input.Source.TrackID
		}
		artistNames := []string{cueTrack.Performer}
		if artistNames[0] == "" {
			artistNames[0] = input.Sheet.Performer
		}
		if artistNames[0] == "" {
			artistNames = primaryCreditNames(input.Base)
		}
		if len(artistNames) == 0 {
			artistNames = []string{"Unknown Artist"}
		}
		albumArtists := append([]string(nil), artistNames...)
		if input.Sheet.Performer != "" {
			albumArtists = []string{input.Sheet.Performer}
		} else if cueHasMultiplePerformers(input.Tracks) {
			albumArtists = []string{"Various Artists"}
		}
		albumTitle := input.Sheet.Title
		if albumTitle == "" && input.Base.Album != nil {
			albumTitle = *input.Base.Album
		}
		if albumTitle == "" {
			albumTitle = strings.TrimSuffix(filepath.Base(input.File.CuePath), filepath.Ext(input.File.CuePath))
		}
		releaseDate := input.Base.ReleaseDate
		if input.Sheet.Date != "" {
			value := input.Sheet.Date
			releaseDate = &value
		}
		discNumber := input.Sheet.DiscNumber
		if discNumber == nil {
			discNumber = input.Base.DiscNumber
		}
		if discNumber == nil {
			value := 1
			discNumber = &value
		}
		title := cueTrackTitle(cueTrack)
		raw := input.Base
		raw.Title = title
		raw.Credits = make([]adminmetadata.MetadataCredit, 0, len(artistNames))
		for _, name := range artistNames {
			raw.Credits = append(raw.Credits, adminmetadata.MetadataCredit{Name: name, Role: adminmetadata.CreditPrimary})
		}
		raw.AlbumArtists = albumArtists
		raw.Album = &albumTitle
		raw.ReleaseDate = releaseDate
		raw.TrackNumber = &cueTrack.Number
		raw.DiscNumber = discNumber
		raw.Lyrics = nil
		effective, overridesLyrics, err := effectiveScanMetadata(ctx, transaction, trackID, raw)
		if err != nil {
			return false, err
		}
		artistAssignments, albumArtistIDs, err := resolveMetadataArtists(ctx, transaction, effective)
		if err != nil {
			return false, err
		}
		preferredAlbum, err := currentTrackAlbum(ctx, transaction, trackID)
		if err != nil {
			return false, err
		}
		effectiveAlbumTitle := albumTitle
		if effective.Album != nil {
			effectiveAlbumTitle = *effective.Album
		}
		resolvedAlbum, err := resolveScanAlbum(
			ctx, transaction, effectiveAlbumTitle, albumArtistIDs, effective.ReleaseDate, preferredAlbum,
		)
		if err != nil {
			return false, err
		}
		if firstAlbumID == nil {
			value := resolvedAlbum
			firstAlbumID = &value
		}
		if err := upsertScanTrack(ctx, transaction, trackID, mapped || trackID == input.Source.TrackID,
			effective, &resolvedAlbum, artistAssignments); err != nil {
			return false, err
		}
		var generation int
		if err := transaction.QueryRow(ctx, `UPDATE tracks SET
			media_generation=media_generation+1,version=version+1,updated_at=now()
			WHERE id=$1 RETURNING media_generation`, trackID).Scan(&generation); err != nil {
			return false, err
		}
		payload, _ := json.Marshal(map[string]any{
			"sourcePath": input.File.RelativePath, "cuePath": input.CuePath,
			"segmentStartMs": cueTrack.StartMS, "segmentEndMs": cueTrack.EndMS,
			"originalFileName": filepath.Base(input.File.AudioPath),
		})
		var jobID string
		err = transaction.QueryRow(ctx, `INSERT INTO media_jobs(
			type,source_asset_id,track_id,generation,idempotency_key,payload,publish_on_ready,scan_run_id
		) VALUES('INGEST_TRACK',$1,$2,$3,$4,$5::jsonb,true,NULLIF($6,'')::uuid) RETURNING id`,
			*input.Source.SourceAssetID, trackID, generation,
			fmt.Sprintf("cue:%s:%s:%d:%d", input.Source.ID, input.CueChecksum, index, generation), payload, input.ScanRunID,
		).Scan(&jobID)
		if err != nil {
			return false, fmt.Errorf("create CUE media processing job: %w", err)
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO local_music_source_tracks(
			source_id,track_id,media_job_id,segment_index,start_ms,end_ms,cue_path,cue_checksum_sha256
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(source_id,track_id) DO UPDATE SET media_job_id=EXCLUDED.media_job_id,
			segment_index=EXCLUDED.segment_index,start_ms=EXCLUDED.start_ms,end_ms=EXCLUDED.end_ms,
			cue_path=EXCLUDED.cue_path,cue_checksum_sha256=EXCLUDED.cue_checksum_sha256,updated_at=now()`,
			input.Source.ID, trackID, jobID, index, cueTrack.StartMS, cueTrack.EndMS, input.CuePath, input.CueChecksum); err != nil {
			return false, fmt.Errorf("store CUE source mapping: %w", err)
		}
		if err := recordScanMetadata(ctx, transaction, trackID, input.Source.ID, raw,
			input.Source.Checksum, input.SeenAt); err != nil {
			return false, err
		}
		if !overridesLyrics {
			if err := syncScannedLyrics(ctx, transaction, trackID, nil); err != nil {
				return false, err
			}
		}
		newTrackIDs = append(newTrackIDs, trackID)
		jobIDs = append(jobIDs, jobID)
	}
	stale := make([]string, 0)
	for _, mapping := range mappings {
		if !containsString(newTrackIDs, mapping.TrackID) {
			stale = append(stale, mapping.TrackID)
		}
	}
	if len(stale) > 0 {
		if _, err := transaction.Exec(ctx, `DELETE FROM local_music_source_tracks
			WHERE source_id=$1 AND track_id=ANY($2::uuid[])`, input.Source.ID, stale); err != nil {
			return false, err
		}
		if _, err := transaction.Exec(ctx, `UPDATE tracks SET
			status='ARCHIVED',version=version+1,updated_at=now() WHERE id=ANY($1::uuid[])`, stale); err != nil {
			return false, err
		}
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		track_id=$2,media_job_id=$3,status='PROCESSING',last_error=NULL,last_seen_at=$4,updated_at=now()
		WHERE id=$1`, input.Source.ID, newTrackIDs[0], jobIDs[0], input.SeenAt); err != nil {
		return false, err
	}
	used, err := attachAlbumArtwork(ctx, transaction, firstAlbumID, input.Artwork)
	if err != nil {
		return false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit CUE source synchronization: %w", err)
	}
	return used, nil
}

func (synchronizer *ProductionSynchronizer) rootPath(ctx context.Context, rootID string) (string, error) {
	var path string
	if err := synchronizer.database.QueryRow(ctx, `SELECT path FROM library_roots WHERE id=$1`, rootID).Scan(&path); err != nil {
		return "", err
	}
	return path, nil
}

func (synchronizer *ProductionSynchronizer) sourceMappings(ctx context.Context, sourceID string, lock bool) ([]cueMapping, error) {
	if lock {
		return nil, errors.New("source mapping locks require a transaction")
	}
	rows, err := synchronizer.database.Query(ctx, `SELECT track_id,media_job_id,segment_index,start_ms,
		end_ms,cue_path,cue_checksum_sha256 FROM local_music_source_tracks
		WHERE source_id=$1 ORDER BY segment_index`, sourceID)
	if err != nil {
		return nil, err
	}
	return scanCueMappings(rows)
}

func sourceMappingsTx(ctx context.Context, transaction pgx.Tx, sourceID string, lock bool) ([]cueMapping, error) {
	query := `SELECT track_id,media_job_id,segment_index,start_ms,end_ms,cue_path,cue_checksum_sha256
		FROM local_music_source_tracks WHERE source_id=$1 ORDER BY segment_index`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := transaction.Query(ctx, query, sourceID)
	if err != nil {
		return nil, err
	}
	return scanCueMappings(rows)
}

func scanCueMappings(rows pgx.Rows) ([]cueMapping, error) {
	defer rows.Close()
	result := make([]cueMapping, 0)
	for rows.Next() {
		var mapping cueMapping
		if err := rows.Scan(&mapping.TrackID, &mapping.MediaJobID, &mapping.Segment,
			&mapping.StartMS, &mapping.EndMS, &mapping.CuePath, &mapping.Checksum); err != nil {
			return nil, err
		}
		result = append(result, mapping)
	}
	return result, rows.Err()
}

func cueMappingsMatch(mappings []cueMapping, tracks []cueTrack, cuePath, checksum string) bool {
	if len(mappings) != len(tracks) {
		return false
	}
	for index, track := range tracks {
		mapping := mappings[index]
		if mapping.Segment != index || mapping.StartMS != track.StartMS ||
			!equalOptionalInt(mapping.EndMS, track.EndMS) || mapping.CuePath == nil || *mapping.CuePath != cuePath ||
			mapping.Checksum == nil || *mapping.Checksum != checksum {
			return false
		}
	}
	return true
}

func primaryCreditNames(metadata adminmetadata.MetadataSnapshot) []string {
	result := make([]string, 0)
	for _, credit := range metadata.Credits {
		if credit.Role == adminmetadata.CreditPrimary {
			result = append(result, credit.Name)
		}
	}
	return result
}

func cueHasMultiplePerformers(tracks []cueTrack) bool {
	values := make(map[string]struct{})
	for _, track := range tracks {
		if track.Performer != "" {
			values[normalizeCatalogText(track.Performer)] = struct{}{}
		}
	}
	return len(values) > 1
}

func containsString(values []string, desired string) bool {
	for _, value := range values {
		if value == desired {
			return true
		}
	}
	return false
}

func equalOptionalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
