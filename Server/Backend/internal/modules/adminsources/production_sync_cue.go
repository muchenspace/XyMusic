package adminsources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/modules/adminmetadata"
)

type cueMapping struct {
	TrackID string
	Number  *int
	StartMS *int64
	EndMS   *int64
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
		existing.ModifiedAt.UnixMilli() == audioInfo.ModTime().UnixMilli() &&
		existing.Status == SourceFileReady {
		mappings, err := synchronizer.sourceMappings(ctx, existing.ID, false)
		if err != nil {
			return err
		}
		needsArtwork, err := synchronizer.needsArtworkForSource(ctx, existing.ID)
		if err != nil {
			return err
		}
		if cueMappingsMatch(mappings, tracks) && !needsArtwork {
			if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
				snapshot.markSourceSeen(existing.ID)
			}
			return nil
		}
	}
	probed, err := synchronizer.probeFile(ctx, file.AudioPath)
	if err != nil {
		return err
	}
	source, err := synchronizer.syncStandardFileWithOptions(ctx, rootID, scanRunID, file, seenAt, standardSyncOptions{
		PreserveCueMappings: true, Metadata: audioInfo, Probed: &probed,
	})
	if err != nil {
		return err
	}
	artwork, err := synchronizer.stageArtwork(ctx, file.AudioPath, probed.Metadata.HasArtwork, source.Checksum)
	if err != nil {
		return err
	}
	used, err := synchronizer.storeCueTracks(ctx, cueMutation{
		RootID: rootID, ScanRunID: scanRunID, Source: source, File: file, Sheet: sheet, Tracks: tracks,
		CuePath: relativeCue, CueChecksum: cueChecksum, Base: probed.Metadata, Probed: &probed,
		Artwork: artwork, SeenAt: seenAt,
	})
	if err != nil || !used {
		synchronizer.cleanupUnreferencedArtwork(ctx, artwork)
	}
	return err
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
	Probed      *adminmetadata.ProbedMetadataFile
	Artwork     *stagedArtwork
	SeenAt      time.Time
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

	byIndex := make(map[int]cueMapping, len(mappings))
	for index, mapping := range mappings {
		byIndex[index] = mapping
	}
	newTrackIDs := make([]string, 0, len(input.Tracks))
	catalogCache := newScanCatalogCache()
	var firstAlbumID *string
	for index, cueTrack := range input.Tracks {
		mapping, mapped := byIndex[index]
		trackID := uuid.NewString()
		if mapped {
			trackID = mapping.TrackID
		} else if index == 0 && input.Source.TrackID != nil {
			trackID = *input.Source.TrackID
		}
		existingTrack := mapped || (input.Source.TrackID != nil && trackID == *input.Source.TrackID)
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
		effective := raw
		var overridesLyrics bool
		if existingTrack {
			effective, overridesLyrics, err = effectiveScanMetadata(ctx, transaction, trackID, raw)
			if err != nil {
				return false, err
			}
		}
		artistAssignments, albumArtistIDs, err := resolveMetadataArtists(ctx, transaction, effective, catalogCache)
		if err != nil {
			return false, err
		}
		var preferredAlbum *string
		if existingTrack {
			preferredAlbum, err = currentTrackAlbum(ctx, transaction, trackID)
			if err != nil {
				return false, err
			}
		}
		effectiveAlbumTitle := albumTitle
		if effective.Album != nil {
			effectiveAlbumTitle = *effective.Album
		}
		resolvedAlbum, err := resolveScanAlbum(
			ctx, transaction, effectiveAlbumTitle, albumArtistIDs, effective.ReleaseDate, preferredAlbum, catalogCache,
		)
		if err != nil {
			return false, err
		}
		if firstAlbumID == nil {
			value := resolvedAlbum
			firstAlbumID = &value
		}

		var durationMs int64
		if cueTrack.EndMS != nil {
			durationMs = int64(*cueTrack.EndMS - cueTrack.StartMS)
		} else if input.Probed != nil && input.Probed.DurationMS != nil && *input.Probed.DurationMS > 0 {
			durationMs = *input.Probed.DurationMS - int64(cueTrack.StartMS)
		}

		if err := upsertScanTrack(ctx, transaction, trackID, existingTrack,
			effective, &durationMs, &resolvedAlbum, artistAssignments, catalogCache); err != nil {
			return false, err
		}

		if _, err := transaction.Exec(ctx, `UPDATE tracks SET
			duration_ms=$2,status='READY',version=version+1,updated_at=now()
			WHERE id=$1`, trackID, durationMs); err != nil {
			return false, err
		}

		startMs := int64(cueTrack.StartMS)
		var endMs *int64
		if cueTrack.EndMS != nil {
			v := int64(*cueTrack.EndMS)
			endMs = &v
		}
		trackNum := cueTrack.Number

		if _, err := transaction.Exec(ctx, `INSERT INTO local_music_source_tracks(
			source_id,track_id,cue_track_number,cue_start_time_ms,cue_end_time_ms
		) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(source_id,track_id) DO UPDATE SET
			cue_track_number=EXCLUDED.cue_track_number,
			cue_start_time_ms=EXCLUDED.cue_start_time_ms,
			cue_end_time_ms=EXCLUDED.cue_end_time_ms`,
			input.Source.ID, trackID, trackNum, startMs, endMs); err != nil {
			return false, fmt.Errorf("store CUE source mapping: %w", err)
		}
		if existingTrack {
			err = recordScanMetadata(ctx, transaction, trackID, input.Source.ID, raw,
				input.Source.Checksum, input.SeenAt)
		} else {
			err = recordNewScanMetadata(ctx, transaction, trackID, input.Source.ID, raw,
				input.Source.Checksum, input.SeenAt)
		}
		if err != nil {
			return false, err
		}
		if existingTrack && !overridesLyrics {
			if err := syncScannedLyrics(ctx, transaction, trackID, nil); err != nil {
				return false, err
			}
		}
		newTrackIDs = append(newTrackIDs, trackID)
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
			status='ARCHIVED',archived_manually=false,version=version+1,updated_at=now() WHERE id=ANY($1::uuid[])`, stale); err != nil {
			return false, err
		}
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		status='READY',last_error=NULL,last_seen_at=$2,updated_at=now()
		WHERE id=$1`, input.Source.ID, input.SeenAt); err != nil {
		return false, err
	}
	used, err := attachAlbumArtwork(ctx, transaction, firstAlbumID, input.Artwork)
	if err != nil {
		return false, err
	}
	return used, transaction.Commit(ctx)
}

func cueHasMultiplePerformers(tracks []cueTrack) bool {
	var first string
	for _, track := range tracks {
		if track.Performer == "" {
			continue
		}
		if first == "" {
			first = track.Performer
		} else if first != track.Performer {
			return true
		}
	}
	return false
}

func (synchronizer *ProductionSynchronizer) sourceMappings(
	ctx context.Context,
	sourceID string,
	forUpdate bool,
) ([]cueMapping, error) {
	if !forUpdate {
		if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
			if mappings, exists := snapshot.mappingsBySource[sourceID]; exists {
				return mappings, nil
			}
		}
	}
	return sourceMappingsTx(ctx, synchronizer.database, sourceID, forUpdate)
}

func sourceMappingsTx(
	ctx context.Context,
	db syncDatabase,
	sourceID string,
	forUpdate bool,
) ([]cueMapping, error) {
	query := `SELECT track_id, cue_track_number, cue_start_time_ms, cue_end_time_ms
		FROM local_music_source_tracks WHERE source_id=$1 ORDER BY cue_track_number NULLS FIRST`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	rows, err := db.Query(ctx, query, sourceID)
	if err != nil {
		return nil, fmt.Errorf("load CUE source mappings: %w", err)
	}
	defer rows.Close()
	mappings := make([]cueMapping, 0)
	for rows.Next() {
		var m cueMapping
		if err := rows.Scan(&m.TrackID, &m.Number, &m.StartMS, &m.EndMS); err != nil {
			return nil, fmt.Errorf("read CUE source mapping: %w", err)
		}
		mappings = append(mappings, m)
	}
	return mappings, rows.Err()
}

func cueMappingsMatch(
	mappings []cueMapping,
	tracks []cueTrack,
) bool {
	if len(mappings) != len(tracks) {
		return false
	}
	for index, track := range tracks {
		m := mappings[index]
		if m.Number == nil || *m.Number != track.Number {
			return false
		}
		if m.StartMS == nil || *m.StartMS != int64(track.StartMS) {
			return false
		}
		if (m.EndMS == nil) != (track.EndMS == nil) {
			return false
		}
		if m.EndMS != nil && *m.EndMS != int64(*track.EndMS) {
			return false
		}
	}
	return true
}

func containsString(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}
