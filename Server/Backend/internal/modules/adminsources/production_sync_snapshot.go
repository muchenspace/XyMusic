package adminsources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type sourceScanSnapshot struct {
	rootPath              string
	sourcesByPath         map[string]*localSourceRecord
	renameCandidates      map[string][]*localSourceRecord
	assetsByID            map[string]sourceScanAsset
	mappingsBySource      map[string][]cueMapping
	externalLyricsByID    map[string]struct{}
	missingArtworkTracks  map[string]struct{}
	missingArtworkSources map[string]struct{}
	seenSourcesMu         sync.Mutex
	seenSourceIDs         map[string]struct{}
	renameClaimedIDs      map[string]struct{}
	sidecarsMu            sync.Mutex
	sidecarsByDir         map[string]*sidecarDirectoryState
	catalogMu             sync.RWMutex
	artworkMu             sync.Mutex
	artworkByChecksum     map[string]*stagedArtwork
	artistIDsByName       map[string]string
	albumIDsByKey         map[string]string
}

type sourceScanAsset struct {
	storagePath string
	sizeBytes   int64
	checksum    *string
	ready       bool
}

type sidecarDirectoryState struct {
	once   sync.Once
	byStem map[string][]string
	err    error
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
	return context.WithValue(ctx, sourceScanSnapshotContextKey{}, snapshot), snapshot.release, nil
}

func (synchronizer *ProductionSynchronizer) loadSourceScanSnapshot(
	ctx context.Context,
	rootID string,
) (*sourceScanSnapshot, error) {
	rootPath, err := synchronizer.rootPath(ctx, rootID)
	if err != nil {
		return nil, err
	}
	snapshot := &sourceScanSnapshot{
		rootPath:              rootPath,
		sourcesByPath:         make(map[string]*localSourceRecord),
		renameCandidates:      make(map[string][]*localSourceRecord),
		assetsByID:            make(map[string]sourceScanAsset),
		mappingsBySource:      make(map[string][]cueMapping),
		externalLyricsByID:    make(map[string]struct{}),
		missingArtworkTracks:  make(map[string]struct{}),
		missingArtworkSources: make(map[string]struct{}),
		seenSourceIDs:         make(map[string]struct{}),
		renameClaimedIDs:      make(map[string]struct{}),
		sidecarsByDir:         make(map[string]*sidecarDirectoryState),
		artistIDsByName:       make(map[string]string),
		albumIDsByKey:         make(map[string]string),
		artworkByChecksum:     make(map[string]*stagedArtwork),
	}

	sourceRows, err := synchronizer.database.Query(ctx, `
		SELECT `+localSourceColumnsWithTrack+`
		FROM local_music_sources source
		LEFT JOIN LATERAL (
			SELECT track_id
			FROM local_music_source_tracks
			WHERE source_id = source.id
			ORDER BY cue_track_number NULLS FIRST, segment_index, track_id
			LIMIT 1
		) track_link ON true
		WHERE source.root_id = $1`, rootID)
	if err != nil {
		return nil, fmt.Errorf("preload local library sources: %w", err)
	}
	for sourceRows.Next() {
		source, scanErr := scanLocalSourceWithTrack(sourceRows)
		if scanErr != nil {
			sourceRows.Close()
			return nil, fmt.Errorf("read preloaded local library source: %w", scanErr)
		}
		sourceCopy := source
		snapshot.sourcesByPath[source.NormalizedPath] = &sourceCopy
		snapshot.renameCandidates[source.Checksum] = append(snapshot.renameCandidates[source.Checksum], &sourceCopy)
	}
	if err := sourceRows.Err(); err != nil {
		sourceRows.Close()
		return nil, fmt.Errorf("iterate preloaded local library sources: %w", err)
	}
	sourceRows.Close()

	mappingRows, err := synchronizer.database.Query(ctx, `
		SELECT mapping.source_id, mapping.track_id, mapping.cue_track_number,
			mapping.cue_start_time_ms, mapping.cue_end_time_ms
		FROM local_music_source_tracks mapping
		JOIN local_music_sources source ON source.id = mapping.source_id
		WHERE source.root_id = $1
		  AND (mapping.cue_path IS NOT NULL OR mapping.cue_track_number IS NOT NULL
		       OR mapping.cue_start_time_ms IS NOT NULL OR mapping.cue_end_time_ms IS NOT NULL)
		ORDER BY mapping.source_id, mapping.cue_track_number NULLS FIRST`, rootID)
	if err != nil {
		return nil, fmt.Errorf("preload local library CUE mappings: %w", err)
	}
	for mappingRows.Next() {
		var sourceID string
		var mapping cueMapping
		if scanErr := mappingRows.Scan(
			&sourceID, &mapping.TrackID, &mapping.Number,
			&mapping.StartMS, &mapping.EndMS,
		); scanErr != nil {
			mappingRows.Close()
			return nil, fmt.Errorf("read preloaded local library CUE mapping: %w", scanErr)
		}
		snapshot.mappingsBySource[sourceID] = append(snapshot.mappingsBySource[sourceID], mapping)
	}
	if err := mappingRows.Err(); err != nil {
		mappingRows.Close()
		return nil, fmt.Errorf("iterate preloaded local library CUE mappings: %w", err)
	}
	mappingRows.Close()

	lyricRows, err := synchronizer.database.Query(ctx, `
		SELECT source.id
		FROM local_music_sources source
		WHERE source.root_id = $1
		  AND EXISTS (
			SELECT 1
			FROM local_music_source_tracks mapping
			JOIN lyrics lyric ON lyric.track_id = mapping.track_id
			WHERE mapping.source_id = source.id AND lyric.origin = 'EXTERNAL'
		  )`, rootID)
	if err != nil {
		return nil, fmt.Errorf("preload local library external lyrics state: %w", err)
	}
	for lyricRows.Next() {
		var sourceID string
		if scanErr := lyricRows.Scan(&sourceID); scanErr != nil {
			lyricRows.Close()
			return nil, fmt.Errorf("read preloaded local library external lyrics state: %w", scanErr)
		}
		snapshot.externalLyricsByID[sourceID] = struct{}{}
	}

	if err := lyricRows.Err(); err != nil {
		lyricRows.Close()
		return nil, fmt.Errorf("iterate preloaded local library external lyrics state: %w", err)
	}
	lyricRows.Close()

	if synchronizer.artworkEnabled() {
		artworkRows, err := synchronizer.database.Query(ctx, `
			SELECT DISTINCT mapping.source_id, mapping.track_id
			FROM local_music_source_tracks mapping
			JOIN local_music_sources source ON source.id = mapping.source_id
			JOIN tracks track ON track.id = mapping.track_id
			JOIN track_metadata metadata ON metadata.track_id = track.id
			JOIN albums album ON album.id = track.album_id
			LEFT JOIN media_assets asset
				ON asset.id = album.cover_asset_id AND asset.status = 'READY'
			WHERE source.root_id = $1
			  AND metadata.raw_tags->>'hasArtwork' = 'true'
			  AND asset.id IS NULL`, rootID)
		if err != nil {
			return nil, fmt.Errorf("preload local library missing artwork state: %w", err)
		}
		for artworkRows.Next() {
			var sourceID, trackID string
			if scanErr := artworkRows.Scan(&sourceID, &trackID); scanErr != nil {
				artworkRows.Close()
				return nil, fmt.Errorf("read preloaded local library missing artwork state: %w", scanErr)
			}
			snapshot.missingArtworkTracks[trackID] = struct{}{}
			snapshot.missingArtworkSources[sourceID] = struct{}{}
		}
		if err := artworkRows.Err(); err != nil {
			artworkRows.Close()
			return nil, fmt.Errorf("iterate preloaded local library missing artwork state: %w", err)
		}
		artworkRows.Close()
	}

	return snapshot, nil
}

func (snapshot *sourceScanSnapshot) release() {
	if snapshot == nil {
		return
	}
	snapshot.sourcesByPath = nil
	snapshot.assetsByID = nil
	snapshot.renameCandidates = nil
	snapshot.mappingsBySource = nil
	snapshot.externalLyricsByID = nil
	snapshot.missingArtworkTracks = nil
	snapshot.missingArtworkSources = nil
	snapshot.seenSourceIDs = nil
	snapshot.renameClaimedIDs = nil
	snapshot.sidecarsByDir = nil
	snapshot.artworkByChecksum = nil
	snapshot.artistIDsByName = nil
	snapshot.albumIDsByKey = nil
}

func sourceScanSnapshotFromContext(ctx context.Context) *sourceScanSnapshot {
	if ctx == nil {
		return nil
	}
	value := ctx.Value(sourceScanSnapshotContextKey{})
	snapshot, _ := value.(*sourceScanSnapshot)
	return snapshot
}

func (snapshot *sourceScanSnapshot) needsArtwork(trackID string) bool {
	if snapshot == nil || trackID == "" {
		return false
	}
	_, exists := snapshot.missingArtworkTracks[trackID]
	return exists
}

func (snapshot *sourceScanSnapshot) needsArtworkForSource(sourceID string) bool {
	if snapshot == nil || sourceID == "" {
		return false
	}
	_, exists := snapshot.missingArtworkSources[sourceID]
	return exists
}

func (snapshot *sourceScanSnapshot) findSource(normalizedPath string) (localSourceRecord, bool) {
	if snapshot == nil {
		return localSourceRecord{}, false
	}
	source, exists := snapshot.sourcesByPath[normalizedPath]
	if !exists || source == nil {
		return localSourceRecord{}, false
	}
	return *source, true
}

func (snapshot *sourceScanSnapshot) markSourceSeen(sourceID string) {
	if snapshot == nil || sourceID == "" {
		return
	}
	snapshot.seenSourcesMu.Lock()
	snapshot.seenSourceIDs[sourceID] = struct{}{}
	snapshot.seenSourcesMu.Unlock()
}

func (snapshot *sourceScanSnapshot) claimRenameCandidate(sourceID string) bool {
	if snapshot == nil || sourceID == "" {
		return true
	}
	snapshot.seenSourcesMu.Lock()
	defer snapshot.seenSourcesMu.Unlock()
	if _, claimed := snapshot.renameClaimedIDs[sourceID]; claimed {
		return false
	}
	if _, seen := snapshot.seenSourceIDs[sourceID]; seen {
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

func (snapshot *sourceScanSnapshot) sidecarNamesForStem(directory string, stem string) ([]string, error) {
	if snapshot == nil {
		return nil, errors.New("snapshot is required")
	}
	directory = filepath.Clean(directory)
	stem = normalizePlatformPath(stem)
	snapshot.sidecarsMu.Lock()
	state, exists := snapshot.sidecarsByDir[directory]
	if !exists {
		state = &sidecarDirectoryState{}
		snapshot.sidecarsByDir[directory] = state
	}
	snapshot.sidecarsMu.Unlock()

	state.once.Do(func() {
		entries, err := os.ReadDir(directory)
		if err != nil {
			state.err = err
			return
		}
		byStem := make(map[string][]string)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".lrc" || ext == ".txt" {
				fileStem := normalizePlatformPath(strings.TrimSuffix(name, filepath.Ext(name)))
				byStem[fileStem] = append(byStem[fileStem], name)
				// A localized sidecar is named <audio-stem>.<language>.<ext>.
				// Index it under the base stem too, otherwise the snapshot fast
				// path silently misses translated lyrics.
				if separator := strings.LastIndex(fileStem, "."); separator > 0 {
					byStem[fileStem[:separator]] = append(byStem[fileStem[:separator]], name)
				}
			}
		}
		state.byStem = byStem
	})

	if state.err != nil {
		return nil, state.err
	}
	return append([]string(nil), state.byStem[stem]...), nil
}

func (snapshot *sourceScanSnapshot) sourceSeen(sourceID string) bool {
	if snapshot == nil || sourceID == "" {
		return false
	}
	snapshot.seenSourcesMu.Lock()
	defer snapshot.seenSourcesMu.Unlock()
	_, seen := snapshot.seenSourceIDs[sourceID]
	return seen
}

func (snapshot *sourceScanSnapshot) artworkForChecksum(checksum string) (*stagedArtwork, bool) {
	if snapshot == nil || strings.TrimSpace(checksum) == "" {
		return nil, false
	}
	snapshot.artworkMu.Lock()
	defer snapshot.artworkMu.Unlock()
	artwork, exists := snapshot.artworkByChecksum[checksum]
	return artwork, exists
}

func (snapshot *sourceScanSnapshot) forgetArtwork(checksum string) {
	if snapshot == nil || strings.TrimSpace(checksum) == "" {
		return
	}
	snapshot.artworkMu.Lock()
	delete(snapshot.artworkByChecksum, checksum)
	snapshot.artworkMu.Unlock()
}

func (snapshot *sourceScanSnapshot) hasArtworkPath(storagePath string) bool {
	if snapshot == nil || strings.TrimSpace(storagePath) == "" {
		return false
	}
	snapshot.artworkMu.Lock()
	defer snapshot.artworkMu.Unlock()
	for _, artwork := range snapshot.artworkByChecksum {
		if artwork != nil && artwork.StoragePath == storagePath {
			return true
		}
	}
	return false
}

func (snapshot *sourceScanSnapshot) rememberArtwork(checksum string, artwork *stagedArtwork) {
	if snapshot == nil || strings.TrimSpace(checksum) == "" || artwork == nil {
		return
	}
	snapshot.artworkMu.Lock()
	if snapshot.artworkByChecksum == nil {
		snapshot.artworkByChecksum = make(map[string]*stagedArtwork)
	}
	snapshot.artworkByChecksum[checksum] = artwork
	snapshot.artworkMu.Unlock()
}

func (snapshot *sourceScanSnapshot) catalogArtist(name string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	snapshot.catalogMu.RLock()
	defer snapshot.catalogMu.RUnlock()
	id, ok := snapshot.artistIDsByName[name]
	return id, ok
}

func (snapshot *sourceScanSnapshot) catalogAlbum(key string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	snapshot.catalogMu.RLock()
	defer snapshot.catalogMu.RUnlock()
	id, ok := snapshot.albumIDsByKey[key]
	return id, ok
}

func (snapshot *sourceScanSnapshot) rememberCatalog(cache *scanCatalogCache) {
	if snapshot == nil || cache == nil {
		return
	}
	snapshot.catalogMu.Lock()
	defer snapshot.catalogMu.Unlock()
	for name, id := range cache.artistIDs {
		snapshot.artistIDsByName[name] = id
	}
	for key, id := range cache.albumIDs {
		snapshot.albumIDsByKey[key] = id
	}
}

func (snapshot *sourceScanSnapshot) forgetAlbum(albumID string) {
	if snapshot == nil {
		return
	}
	snapshot.catalogMu.Lock()
	defer snapshot.catalogMu.Unlock()
	for key, id := range snapshot.albumIDsByKey {
		if id == albumID {
			delete(snapshot.albumIDsByKey, key)
		}
	}
}
