package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const readyObjectStatCacheMaximumEntries = 16_384

type sourceScanSnapshot struct {
	rootPath           string
	sourcesByPath      map[string]localSourceRecord
	renameCandidates   map[string][]localSourceRecord
	assetsByID         map[string]sourceScanAsset
	mappingsBySource   map[string][]cueMapping
	externalLyricsByID map[string]bool
	seenSourcesMu      sync.Mutex
	seenSourceIDs      map[string]struct{}
	renameClaimedIDs   map[string]struct{}
	sidecarsMu         sync.Mutex
	sidecarsByDir      map[string]*sidecarDirectoryState
	objectStatsMu      sync.Mutex
	objectStats        map[string]*sourceObjectStat
	artworkMu          sync.Mutex
	artworksByChecksum map[string]stagedArtwork
	artworkCalls       map[string]*sourceArtworkCall
	artworkStates      map[string]*sourceArtworkState
	catalogMu          sync.RWMutex
	artistIDsByName    map[string]string
	albumIDsByKey      map[string]string
}

type sidecarDirectoryState struct {
	once   sync.Once
	names  []string
	byStem map[string][]string
	err    error
}

type sourceScanAsset struct {
	objectKey string
	sizeBytes int64
	checksum  *string
	ready     bool
}

type sourceObjectStat struct {
	done      chan struct{}
	sizeBytes int64
	checksum  string
	exists    bool
	err       error
}

type sourceArtworkCall struct {
	done    chan struct{}
	artwork *stagedArtwork
	err     error
}

type sourceArtworkState struct {
	artwork       stagedArtwork
	used          bool
	cleanupReason string
	cleanupQueued bool
}

type sourceArtworkCleanup struct {
	objectKey string
	reason    string
}

type sourceScanSnapshotContextKey struct{}

func (synchronizer *ProductionSynchronizer) PrepareScan(
	ctx context.Context,
	rootID string,
	_ string,
) (context.Context, func(), error) {
	snapshot, err := synchronizer.loadSourceScanSnapshot(ctx, rootID)
	if err != nil {
		return nil, nil, err
	}
	return context.WithValue(ctx, sourceScanSnapshotContextKey{}, snapshot), func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = synchronizer.flushArtworkCleanup(cleanupContext, snapshot)
	}, nil
}

func (synchronizer *ProductionSynchronizer) loadSourceScanSnapshot(
	ctx context.Context,
	rootID string,
) (*sourceScanSnapshot, error) {
	snapshot := &sourceScanSnapshot{
		sourcesByPath:      make(map[string]localSourceRecord),
		renameCandidates:   make(map[string][]localSourceRecord),
		assetsByID:         make(map[string]sourceScanAsset),
		mappingsBySource:   make(map[string][]cueMapping),
		externalLyricsByID: make(map[string]bool),
		seenSourceIDs:      make(map[string]struct{}),
		renameClaimedIDs:   make(map[string]struct{}),
		sidecarsByDir:      make(map[string]*sidecarDirectoryState),
		objectStats:        make(map[string]*sourceObjectStat),
		artworksByChecksum: make(map[string]stagedArtwork),
		artworkCalls:       make(map[string]*sourceArtworkCall),
		artworkStates:      make(map[string]*sourceArtworkState),
		artistIDsByName:    make(map[string]string),
		albumIDsByKey:      make(map[string]string),
	}
	if err := synchronizer.database.QueryRow(ctx,
		`SELECT path FROM library_roots WHERE id=$1`, rootID).Scan(&snapshot.rootPath); err != nil {
		return nil, fmt.Errorf("load local library scan root: %w", err)
	}
	// The scan only needs catalog identities already connected to this root.
	// Loading the whole artists table made scan startup scale with the global
	// catalog instead of the library being scanned; names introduced by new or
	// changed files are resolved lazily by resolveScanArtists.
	artistRows, err := synchronizer.database.Query(ctx, `
		SELECT artist.normalized_name,artist.id
		FROM artists artist
		WHERE artist.id IN (
			SELECT credit.artist_id
			FROM track_artists credit
			JOIN local_music_source_tracks mapping ON mapping.track_id=credit.track_id
			JOIN local_music_sources source ON source.id=mapping.source_id
			WHERE source.root_id=$1
			UNION
			SELECT credit.artist_id
			FROM album_artists credit
			JOIN tracks track ON track.album_id=credit.album_id
			JOIN local_music_source_tracks mapping ON mapping.track_id=track.id
			JOIN local_music_sources source ON source.id=mapping.source_id
			WHERE source.root_id=$1
		)
		ORDER BY artist.normalized_name,artist.id`, rootID,
	)
	if err != nil {
		return nil, fmt.Errorf("load local library artist catalog: %w", err)
	}
	for artistRows.Next() {
		var normalizedName, artistID string
		if err := artistRows.Scan(&normalizedName, &artistID); err != nil {
			artistRows.Close()
			return nil, fmt.Errorf("scan local library artist catalog: %w", err)
		}
		if _, exists := snapshot.artistIDsByName[normalizedName]; !exists {
			snapshot.artistIDsByName[normalizedName] = artistID
		}
	}
	if err := artistRows.Err(); err != nil {
		artistRows.Close()
		return nil, fmt.Errorf("iterate local library artist catalog: %w", err)
	}
	artistRows.Close()
	albumRows, err := synchronizer.database.Query(ctx, `
		SELECT album.normalized_title,album.id,
		       array_agg(link.artist_id ORDER BY link.sort_order,link.artist_id)
		FROM albums album
		JOIN tracks track ON track.album_id=album.id
		JOIN local_music_source_tracks mapping ON mapping.track_id=track.id
		JOIN local_music_sources source ON source.id=mapping.source_id
		JOIN album_artists link ON link.album_id=album.id AND link.role='PRIMARY'
		WHERE source.root_id=$1
		GROUP BY album.normalized_title,album.id
		ORDER BY album.normalized_title,album.id`, rootID,
	)
	if err != nil {
		return nil, fmt.Errorf("load local library album catalog: %w", err)
	}
	for albumRows.Next() {
		var normalizedTitle, albumID string
		var artistIDs []string
		if err := albumRows.Scan(&normalizedTitle, &albumID, &artistIDs); err != nil {
			albumRows.Close()
			return nil, fmt.Errorf("scan local library album catalog: %w", err)
		}
		key := scanAlbumCacheKey(normalizedTitle, artistIDs, nil)
		if _, exists := snapshot.albumIDsByKey[key]; !exists {
			snapshot.albumIDsByKey[key] = albumID
		}
	}
	if err := albumRows.Err(); err != nil {
		albumRows.Close()
		return nil, fmt.Errorf("iterate local library album catalog: %w", err)
	}
	albumRows.Close()

	rows, err := synchronizer.database.Query(ctx, `SELECT
		source.id,source.root_id,source.source_path,source.normalized_source_path,
		source.checksum_sha256,source.size_bytes,source.modified_at,source.track_id,
		source.source_asset_id,source.media_job_id,source.status,source.last_seen_at,source.updated_at,
		asset.object_key,asset.size_bytes,asset.checksum_sha256,
		EXISTS(
			SELECT 1 FROM local_music_source_tracks mapping
			JOIN lyrics lyric ON lyric.track_id=mapping.track_id
			WHERE mapping.source_id=source.id AND lyric.origin='EXTERNAL'
		)
		FROM local_music_sources source
		LEFT JOIN media_assets asset ON asset.id=source.source_asset_id
			AND asset.kind='AUDIO_SOURCE' AND asset.status='READY'
		WHERE source.root_id=$1`, rootID)
	if err != nil {
		return nil, fmt.Errorf("load local library source snapshot: %w", err)
	}
	for rows.Next() {
		var source localSourceRecord
		var objectKey *string
		var assetSize *int64
		var assetChecksum *string
		var externalLyrics bool
		if err := rows.Scan(
			&source.ID, &source.RootID, &source.SourcePath, &source.NormalizedPath,
			&source.Checksum, &source.SizeBytes, &source.ModifiedAt, &source.TrackID,
			&source.SourceAssetID, &source.MediaJobID, &source.Status, &source.LastSeenAt, &source.UpdatedAt,
			&objectKey, &assetSize, &assetChecksum, &externalLyrics,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan local library source snapshot: %w", err)
		}
		snapshot.sourcesByPath[source.NormalizedPath] = source
		snapshot.renameCandidates[source.Checksum] = append(snapshot.renameCandidates[source.Checksum], source)
		snapshot.externalLyricsByID[source.ID] = externalLyrics
		if source.SourceAssetID != nil {
			asset := sourceScanAsset{checksum: assetChecksum}
			if objectKey != nil && assetSize != nil {
				asset.objectKey, asset.sizeBytes, asset.ready = *objectKey, *assetSize, true
			}
			snapshot.assetsByID[*source.SourceAssetID] = asset
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate local library source snapshot: %w", err)
	}
	rows.Close()

	mappingRows, err := synchronizer.database.Query(ctx, `SELECT
		mapping.source_id,mapping.track_id,mapping.media_job_id,mapping.segment_index,
		mapping.start_ms,mapping.end_ms,mapping.cue_path,mapping.cue_checksum_sha256
		FROM local_music_source_tracks mapping
		JOIN local_music_sources source ON source.id=mapping.source_id
		WHERE source.root_id=$1 ORDER BY mapping.source_id,mapping.segment_index`, rootID)
	if err != nil {
		return nil, fmt.Errorf("load local library source mappings: %w", err)
	}
	for mappingRows.Next() {
		var sourceID string
		var mapping cueMapping
		if err := mappingRows.Scan(
			&sourceID, &mapping.TrackID, &mapping.MediaJobID, &mapping.Segment,
			&mapping.StartMS, &mapping.EndMS, &mapping.CuePath, &mapping.Checksum,
		); err != nil {
			mappingRows.Close()
			return nil, fmt.Errorf("scan local library source mapping snapshot: %w", err)
		}
		snapshot.mappingsBySource[sourceID] = append(snapshot.mappingsBySource[sourceID], mapping)
	}
	if err := mappingRows.Err(); err != nil {
		mappingRows.Close()
		return nil, fmt.Errorf("iterate local library source mapping snapshot: %w", err)
	}
	mappingRows.Close()
	return snapshot, nil
}

func (snapshot *sourceScanSnapshot) catalogArtist(normalizedName string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	snapshot.catalogMu.RLock()
	artistID, found := snapshot.artistIDsByName[normalizedName]
	snapshot.catalogMu.RUnlock()
	return artistID, found
}

func (snapshot *sourceScanSnapshot) catalogAlbum(key string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	snapshot.catalogMu.RLock()
	albumID, found := snapshot.albumIDsByKey[key]
	snapshot.catalogMu.RUnlock()
	return albumID, found
}

func (snapshot *sourceScanSnapshot) rememberCatalog(cache *scanCatalogCache) {
	if snapshot == nil || cache == nil {
		return
	}
	snapshot.catalogMu.Lock()
	if snapshot.artistIDsByName == nil {
		snapshot.artistIDsByName = make(map[string]string)
	}
	for normalizedName, artistID := range cache.artistIDs {
		if normalizedName != "" && artistID != "" {
			snapshot.artistIDsByName[normalizedName] = artistID
		}
	}
	if snapshot.albumIDsByKey == nil {
		snapshot.albumIDsByKey = make(map[string]string)
	}
	for key, albumID := range cache.albumIDs {
		if albumID != "" && !strings.Contains(key, "\x00preferred:") {
			snapshot.albumIDsByKey[key] = albumID
		}
	}
	snapshot.catalogMu.Unlock()
}

func (snapshot *sourceScanSnapshot) forgetAlbum(albumID string) {
	if snapshot == nil || albumID == "" {
		return
	}
	snapshot.catalogMu.Lock()
	for key, cachedID := range snapshot.albumIDsByKey {
		if cachedID == albumID {
			delete(snapshot.albumIDsByKey, key)
		}
	}
	snapshot.catalogMu.Unlock()
}

func (snapshot *sourceScanSnapshot) cachedArtwork(checksum string) (*stagedArtwork, bool) {
	if snapshot == nil || checksum == "" {
		return nil, false
	}
	snapshot.artworkMu.Lock()
	artwork, found := snapshot.artworksByChecksum[checksum]
	snapshot.artworkMu.Unlock()
	if !found {
		return nil, false
	}
	copy := artwork
	return &copy, true
}

func (snapshot *sourceScanSnapshot) rememberArtwork(checksum string, artwork stagedArtwork) {
	if snapshot == nil || checksum == "" || artwork.ObjectKey == "" {
		return
	}
	snapshot.artworkMu.Lock()
	if snapshot.artworksByChecksum == nil {
		snapshot.artworksByChecksum = make(map[string]stagedArtwork)
	}
	snapshot.artworksByChecksum[checksum] = artwork
	snapshot.rememberArtworkCandidateLocked(artwork)
	snapshot.artworkStates[artwork.ObjectKey].used = true
	snapshot.artworkMu.Unlock()
}

func (snapshot *sourceScanSnapshot) rememberArtworkCandidate(artwork stagedArtwork) {
	if snapshot == nil || artwork.ObjectKey == "" {
		return
	}
	snapshot.artworkMu.Lock()
	snapshot.rememberArtworkCandidateLocked(artwork)
	snapshot.artworkMu.Unlock()
}

func (snapshot *sourceScanSnapshot) rememberArtworkCandidateLocked(artwork stagedArtwork) {
	if snapshot.artworkStates == nil {
		snapshot.artworkStates = make(map[string]*sourceArtworkState)
	}
	state := snapshot.artworkStates[artwork.ObjectKey]
	if state == nil {
		state = &sourceArtworkState{}
		snapshot.artworkStates[artwork.ObjectKey] = state
	}
	state.artwork = artwork
}

func (snapshot *sourceScanSnapshot) deferArtworkCleanup(artwork stagedArtwork, reason string) {
	if snapshot == nil || artwork.ObjectKey == "" {
		return
	}
	snapshot.artworkMu.Lock()
	snapshot.rememberArtworkCandidateLocked(artwork)
	state := snapshot.artworkStates[artwork.ObjectKey]
	if state.cleanupReason == "" {
		state.cleanupReason = reason
	}
	snapshot.artworkMu.Unlock()
}

func (snapshot *sourceScanSnapshot) pendingArtworkCleanups() []sourceArtworkCleanup {
	if snapshot == nil {
		return nil
	}
	snapshot.artworkMu.Lock()
	defer snapshot.artworkMu.Unlock()
	result := make([]sourceArtworkCleanup, 0)
	for objectKey, state := range snapshot.artworkStates {
		if state == nil || state.used || state.cleanupQueued {
			continue
		}
		reason := state.cleanupReason
		if reason == "" {
			reason = "UNUSED_LIBRARY_ARTWORK"
		}
		result = append(result, sourceArtworkCleanup{objectKey: objectKey, reason: reason})
	}
	return result
}

func (snapshot *sourceScanSnapshot) markArtworkCleanupsQueued(cleanups []sourceArtworkCleanup) {
	if snapshot == nil || len(cleanups) == 0 {
		return
	}
	snapshot.artworkMu.Lock()
	for _, cleanup := range cleanups {
		if state := snapshot.artworkStates[cleanup.objectKey]; state != nil && !state.used {
			state.cleanupQueued = true
		}
	}
	snapshot.artworkMu.Unlock()
}

// coalescedArtwork makes concurrent files that carry the same source checksum
// share one cover extraction and upload. A failed extraction is removed from
// the in-flight map so a later scan can retry it.
func (snapshot *sourceScanSnapshot) coalescedArtwork(
	ctx context.Context,
	checksum string,
	produce func() (*stagedArtwork, error),
) (*stagedArtwork, error) {
	if snapshot == nil || checksum == "" {
		return produce()
	}
	if cached, found := snapshot.cachedArtwork(checksum); found {
		return cached, nil
	}
	snapshot.artworkMu.Lock()
	if snapshot.artworkCalls == nil {
		snapshot.artworkCalls = make(map[string]*sourceArtworkCall)
	}
	call := snapshot.artworkCalls[checksum]
	owner := false
	if call == nil {
		call = &sourceArtworkCall{done: make(chan struct{})}
		snapshot.artworkCalls[checksum] = call
		owner = true
	}
	snapshot.artworkMu.Unlock()
	if !owner {
		select {
		case <-call.done:
			if call.err != nil {
				return nil, call.err
			}
			if call.artwork == nil {
				return nil, nil
			}
			copy := *call.artwork
			return &copy, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	artwork, err := produce()
	snapshot.artworkMu.Lock()
	call.artwork, call.err = artwork, err
	if err == nil && artwork != nil {
		if snapshot.artworksByChecksum == nil {
			snapshot.artworksByChecksum = make(map[string]stagedArtwork)
		}
		snapshot.artworksByChecksum[checksum] = *artwork
		snapshot.rememberArtworkCandidateLocked(*artwork)
	}
	delete(snapshot.artworkCalls, checksum)
	close(call.done)
	snapshot.artworkMu.Unlock()
	return artwork, err
}

func sourceScanSnapshotFromContext(ctx context.Context) *sourceScanSnapshot {
	if ctx == nil {
		return nil
	}
	snapshot, _ := ctx.Value(sourceScanSnapshotContextKey{}).(*sourceScanSnapshot)
	return snapshot
}

func (snapshot *sourceScanSnapshot) markSourceSeen(sourceID string) {
	if snapshot == nil || sourceID == "" {
		return
	}
	snapshot.seenSourcesMu.Lock()
	if snapshot.seenSourceIDs == nil {
		snapshot.seenSourceIDs = make(map[string]struct{})
	}
	snapshot.seenSourceIDs[sourceID] = struct{}{}
	snapshot.seenSourcesMu.Unlock()
}

func (snapshot *sourceScanSnapshot) claimRenameCandidate(sourceID string) bool {
	if snapshot == nil || sourceID == "" {
		return false
	}
	snapshot.seenSourcesMu.Lock()
	defer snapshot.seenSourcesMu.Unlock()
	if snapshot.seenSourceIDs == nil {
		snapshot.seenSourceIDs = make(map[string]struct{})
	}
	if snapshot.renameClaimedIDs == nil {
		snapshot.renameClaimedIDs = make(map[string]struct{})
	}
	if _, exists := snapshot.seenSourceIDs[sourceID]; exists {
		return false
	}
	if _, exists := snapshot.renameClaimedIDs[sourceID]; exists {
		return false
	}
	snapshot.renameClaimedIDs[sourceID] = struct{}{}
	return true
}

func (snapshot *sourceScanSnapshot) releaseRenameCandidate(sourceID string) {
	if snapshot == nil || sourceID == "" {
		return
	}
	snapshot.seenSourcesMu.Lock()
	delete(snapshot.renameClaimedIDs, sourceID)
	snapshot.seenSourcesMu.Unlock()
}

func (snapshot *sourceScanSnapshot) sourceSeen(sourceID string) bool {
	if snapshot == nil || sourceID == "" {
		return false
	}
	snapshot.seenSourcesMu.Lock()
	_, exists := snapshot.seenSourceIDs[sourceID]
	if !exists {
		_, exists = snapshot.renameClaimedIDs[sourceID]
	}
	snapshot.seenSourcesMu.Unlock()
	return exists
}

func (snapshot *sourceScanSnapshot) seenSourceIDsSnapshot() []string {
	if snapshot == nil {
		return nil
	}
	snapshot.seenSourcesMu.Lock()
	defer snapshot.seenSourcesMu.Unlock()
	if len(snapshot.seenSourceIDs) == 0 {
		return nil
	}
	result := make([]string, 0, len(snapshot.seenSourceIDs))
	for sourceID := range snapshot.seenSourceIDs {
		result = append(result, sourceID)
	}
	return result
}

// FlushScan turns the per-file last_seen writes on the unchanged fast path
// into one database update. New, changed, and failed sources already persist
// their own generation/status mutations and do not need to be included here.
func (synchronizer *ProductionSynchronizer) FlushScan(
	ctx context.Context,
	rootID string,
	seenAt time.Time,
) (resultErr error) {
	snapshot := sourceScanSnapshotFromContext(ctx)
	defer func() {
		if snapshot == nil {
			return
		}
		if cleanupErr := synchronizer.flushArtworkCleanup(ctx, snapshot); cleanupErr != nil {
			if resultErr == nil {
				resultErr = cleanupErr
			} else {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
		}
	}()
	seenSourceIDs := snapshot.seenSourceIDsSnapshot()
	if len(seenSourceIDs) == 0 {
		return nil
	}
	now := time.Now().UTC()
	if synchronizer.now != nil {
		now = synchronizer.now()
	}
	if _, err := synchronizer.database.Exec(ctx, `UPDATE local_music_sources SET
		last_seen_at=$2,updated_at=$3
		WHERE root_id=$1 AND id=ANY($4::uuid[]) AND last_seen_at<$2`,
		rootID, seenAt, now, seenSourceIDs); err != nil {
		return fmt.Errorf("flush local library scan bookkeeping: %w", err)
	}
	return nil
}

func (snapshot *sourceScanSnapshot) sidecarNames(directory string) ([]string, error) {
	state, err := snapshot.sidecarDirectory(directory)
	if state == nil {
		return nil, err
	}
	return state.names, err
}

func (snapshot *sourceScanSnapshot) sidecarNamesForStem(directory, stem string) ([]string, error) {
	state, err := snapshot.sidecarDirectory(directory)
	if state == nil {
		return nil, err
	}
	return state.byStem[stem], err
}

func (snapshot *sourceScanSnapshot) sidecarDirectory(directory string) (*sidecarDirectoryState, error) {
	if snapshot == nil {
		return nil, nil
	}
	key := normalizePlatformPath(filepath.Clean(directory))
	snapshot.sidecarsMu.Lock()
	if snapshot.sidecarsByDir == nil {
		snapshot.sidecarsByDir = make(map[string]*sidecarDirectoryState)
	}
	state := snapshot.sidecarsByDir[key]
	if state == nil {
		state = &sidecarDirectoryState{}
		snapshot.sidecarsByDir[key] = state
	}
	snapshot.sidecarsMu.Unlock()
	state.once.Do(func() {
		entries, err := os.ReadDir(directory)
		if err != nil {
			state.err = err
			return
		}
		state.names = make([]string, 0)
		state.byStem = make(map[string][]string)
		seenByStem := make(map[string]map[string]struct{})
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			extension := strings.ToLower(filepath.Ext(entry.Name()))
			if extension != ".lrc" && extension != ".txt" {
				continue
			}
			name := entry.Name()
			state.names = append(state.names, name)
			rawStem := strings.TrimSuffix(name, filepath.Ext(name))
			addStem := func(stem string) {
				if seenByStem[stem] == nil {
					seenByStem[stem] = make(map[string]struct{})
				}
				if _, exists := seenByStem[stem][name]; exists {
					return
				}
				seenByStem[stem][name] = struct{}{}
				state.byStem[stem] = append(state.byStem[stem], name)
			}
			addStem(normalizePlatformPath(rawStem))
			if separator := strings.LastIndex(rawStem, "."); separator > 0 {
				addStem(normalizePlatformPath(rawStem[:separator]))
			}
		}
	})
	return state, state.err
}

func (snapshot *sourceScanSnapshot) statObject(
	ctx context.Context,
	storage SourceObjectStorage,
	objectKey string,
) (int64, string, bool, error) {
	return snapshot.statObjectWith(ctx, objectKey, storage.StatObject)
}

func (snapshot *sourceScanSnapshot) statObjectWith(
	ctx context.Context,
	objectKey string,
	statObject func(context.Context, string) (int64, string, bool, error),
) (int64, string, bool, error) {
	snapshot.objectStatsMu.Lock()
	if snapshot.objectStats == nil {
		snapshot.objectStats = make(map[string]*sourceObjectStat)
	}
	stat := snapshot.objectStats[objectKey]
	owner := false
	if stat == nil {
		stat = &sourceObjectStat{done: make(chan struct{})}
		snapshot.objectStats[objectKey] = stat
		owner = true
	}
	snapshot.objectStatsMu.Unlock()
	if owner {
		stat.sizeBytes, stat.checksum, stat.exists, stat.err = statObject(ctx, objectKey)
		close(stat.done)
	}
	select {
	case <-stat.done:
		return stat.sizeBytes, stat.checksum, stat.exists, stat.err
	case <-ctx.Done():
		return 0, "", false, ctx.Err()
	}
}

// statReadySourceObject keeps the integrity check on the first observation of
// an object while allowing repeated unchanged scans to reuse a recent positive
// result. Missing objects and storage errors are deliberately never cached.
func (synchronizer *ProductionSynchronizer) statReadySourceObject(
	ctx context.Context,
	snapshot *sourceScanSnapshot,
	objectKey string,
	expectedSize int64,
	expectedChecksum *string,
) (int64, string, bool, error) {
	now := time.Now().UTC()
	if synchronizer.now != nil {
		now = synchronizer.now()
	}
	if synchronizer.readySourceObjectStatTTL > 0 {
		synchronizer.readyObjectStatsMu.Lock()
		cached, exists := synchronizer.readyObjectStats[objectKey]
		fresh := exists && now.Before(cached.checkedAt.Add(synchronizer.readySourceObjectStatTTL))
		checksumMatches := expectedChecksum == nil || cached.checksum == "" ||
			strings.EqualFold(cached.checksum, *expectedChecksum)
		if fresh && cached.exists && cached.sizeBytes == expectedSize && checksumMatches {
			synchronizer.readyObjectStatsMu.Unlock()
			return cached.sizeBytes, cached.checksum, true, nil
		}
		synchronizer.readyObjectStatsMu.Unlock()
	}

	var (
		sizeBytes int64
		checksum  string
		exists    bool
		err       error
	)
	if snapshot != nil {
		sizeBytes, checksum, exists, err = snapshot.statObjectWith(ctx, objectKey, synchronizer.statObject)
	} else {
		sizeBytes, checksum, exists, err = synchronizer.statObject(ctx, objectKey)
	}
	if err == nil && exists && synchronizer.readySourceObjectStatTTL > 0 {
		synchronizer.readyObjectStatsMu.Lock()
		if synchronizer.readyObjectStats == nil {
			synchronizer.readyObjectStats = make(map[string]readySourceObjectStat)
		}
		if len(synchronizer.readyObjectStats) >= readyObjectStatCacheMaximumEntries {
			for key, cached := range synchronizer.readyObjectStats {
				if !now.Before(cached.checkedAt.Add(synchronizer.readySourceObjectStatTTL)) {
					delete(synchronizer.readyObjectStats, key)
				}
			}
			if len(synchronizer.readyObjectStats) >= readyObjectStatCacheMaximumEntries {
				for key := range synchronizer.readyObjectStats {
					delete(synchronizer.readyObjectStats, key)
					break
				}
			}
		}
		synchronizer.readyObjectStats[objectKey] = readySourceObjectStat{
			sizeBytes: sizeBytes, checksum: checksum, exists: true, checkedAt: now,
		}
		synchronizer.readyObjectStatsMu.Unlock()
	}
	return sizeBytes, checksum, exists, err
}
