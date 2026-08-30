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
	catalogMu          sync.RWMutex
	artworkMu          sync.Mutex
	artworkByChecksum  map[string]*stagedArtwork
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
	storagePath string
	sizeBytes   int64
	checksum    *string
	ready       bool
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
	return context.WithValue(ctx, sourceScanSnapshotContextKey{}, snapshot), func() {}, nil
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
		rootPath:           rootPath,
		sourcesByPath:      make(map[string]localSourceRecord),
		renameCandidates:   make(map[string][]localSourceRecord),
		assetsByID:         make(map[string]sourceScanAsset),
		mappingsBySource:   make(map[string][]cueMapping),
		externalLyricsByID: make(map[string]bool),
		seenSourceIDs:      make(map[string]struct{}),
		renameClaimedIDs:   make(map[string]struct{}),
		sidecarsByDir:      make(map[string]*sidecarDirectoryState),
		artistIDsByName:    make(map[string]string),
		albumIDsByKey:      make(map[string]string),
		artworkByChecksum:  make(map[string]*stagedArtwork),
	}

	sourceRows, err := synchronizer.database.Query(ctx, `
		SELECT `+localSourceColumnsWithTrack+`
		FROM local_music_sources source
		LEFT JOIN local_music_source_tracks track_link ON track_link.source_id = source.id
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
		snapshot.sourcesByPath[source.NormalizedPath] = source
		snapshot.renameCandidates[source.Checksum] = append(snapshot.renameCandidates[source.Checksum], source)
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
		SELECT mapping.source_id, EXISTS(
			SELECT 1 FROM lyrics lyric
			WHERE lyric.track_id = mapping.track_id AND lyric.origin = 'EXTERNAL'
		)
		FROM local_music_source_tracks mapping
		JOIN local_music_sources source ON source.id = mapping.source_id
		WHERE source.root_id = $1
		GROUP BY mapping.source_id, mapping.track_id`, rootID)
	if err != nil {
		return nil, fmt.Errorf("preload local library external lyrics state: %w", err)
	}
	for lyricRows.Next() {
		var sourceID string
		var hasExternal bool
		if scanErr := lyricRows.Scan(&sourceID, &hasExternal); scanErr != nil {
			lyricRows.Close()
			return nil, fmt.Errorf("read preloaded local library external lyrics state: %w", scanErr)
		}
		if hasExternal {
			snapshot.externalLyricsByID[sourceID] = true
		}
	}
	if err := lyricRows.Err(); err != nil {
		lyricRows.Close()
		return nil, fmt.Errorf("iterate preloaded local library external lyrics state: %w", err)
	}
	lyricRows.Close()

	return snapshot, nil
}

func sourceScanSnapshotFromContext(ctx context.Context) *sourceScanSnapshot {
	if ctx == nil {
		return nil
	}
	value := ctx.Value(sourceScanSnapshotContextKey{})
	snapshot, _ := value.(*sourceScanSnapshot)
	return snapshot
}

func (snapshot *sourceScanSnapshot) findSource(normalizedPath string) (localSourceRecord, bool) {
	if snapshot == nil {
		return localSourceRecord{}, false
	}
	source, exists := snapshot.sourcesByPath[normalizedPath]
	return source, exists
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
		names := make([]string, 0, len(entries))
		byStem := make(map[string][]string)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			names = append(names, name)
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
		state.names = names
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
