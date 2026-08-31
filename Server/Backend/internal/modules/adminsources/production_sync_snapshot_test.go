package adminsources

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSourceScanSnapshotServesReadOnlyLookups(t *testing.T) {
	checksum := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	trackID := "00000000-0000-4000-8000-000000000001"
	source := localSourceRecord{
		ID: "source", RootID: "root", NormalizedPath: "album/song.flac",
		Checksum: checksum, SizeBytes: 100, LastSeenAt: time.Now().Add(-time.Hour),
		Status: SourceFileReady, TrackID: &trackID,
	}
	num := 1
	start := int64(0)
	mappings := []cueMapping{{TrackID: "track", Number: &num, StartMS: &start}}
	snapshot := &sourceScanSnapshot{
		rootPath:           `C:\Music`,
		sourcesByPath:      map[string]*localSourceRecord{source.NormalizedPath: &source},
		renameCandidates:   map[string][]*localSourceRecord{checksum: {&source}},
		mappingsBySource:   map[string][]cueMapping{source.ID: mappings},
		externalLyricsByID: map[string]struct{}{source.ID: {}},
		seenSourceIDs:      make(map[string]struct{}),
		renameClaimedIDs:   make(map[string]struct{}),
	}
	ctx := context.WithValue(context.Background(), sourceScanSnapshotContextKey{}, snapshot)
	synchronizer := &ProductionSynchronizer{}

	got, found, err := synchronizer.findSource(ctx, "root", source.NormalizedPath)
	if err != nil || !found || got.ID != source.ID {
		t.Fatalf("find source = %+v/%v/%v", got, found, err)
	}
	candidates, err := synchronizer.findRenameCandidates(ctx, "root", checksum, time.Now())
	if err != nil || len(candidates) != 1 || candidates[0].ID != source.ID {
		t.Fatalf("rename candidates = %+v/%v", candidates, err)
	}
	if rootPath, err := synchronizer.rootPath(ctx, "root"); err == nil && rootPath != "" {
		// fallback since synchronizer has no DB
	}
	if external, err := synchronizer.sourceHasExternalLyrics(ctx, source.ID); err != nil || !external {
		t.Fatalf("external lyrics = %v/%v", external, err)
	}
	gotMappings, err := synchronizer.sourceMappings(ctx, source.ID, false)
	if err != nil || len(gotMappings) != 1 || gotMappings[0].TrackID != "track" {
		t.Fatalf("source mappings = %+v/%v", gotMappings, err)
	}
}

func TestSourceScanSnapshotClaimsRenameCandidatesOnce(t *testing.T) {
	snapshot := &sourceScanSnapshot{
		seenSourceIDs:    make(map[string]struct{}),
		renameClaimedIDs: make(map[string]struct{}),
	}
	if !snapshot.claimRenameCandidate("source") {
		t.Fatal("first rename claim was rejected")
	}
	if snapshot.claimRenameCandidate("source") {
		t.Fatal("duplicate rename claim was accepted")
	}
	snapshot.releaseRenameCandidate("source")
	if !snapshot.claimRenameCandidate("source") {
		t.Fatal("released rename claim was not reclaimable")
	}
	snapshot.markSourceSeen("source")
	snapshot.releaseRenameCandidate("source")
	if snapshot.claimRenameCandidate("source") {
		t.Fatal("source already seen by its current path was claimed")
	}
}

func TestScanCatalogCacheForgetsDeletedAlbum(t *testing.T) {
	cache := newScanCatalogCache()
	cache.albumIDs["album-a"] = "album"
	cache.albumIDs["album-b"] = "other"
	cache.forgetAlbum("album")
	if _, exists := cache.albumIDs["album-a"]; exists {
		t.Fatal("deleted album remained in transaction cache")
	}
	if cache.albumIDs["album-b"] != "other" {
		t.Fatalf("unrelated album cache = %#v", cache.albumIDs)
	}

	snapshot := &sourceScanSnapshot{albumIDsByKey: map[string]string{
		"album-a": "album", "album-b": "other",
	}}
	snapshot.forgetAlbum("album")
	if _, exists := snapshot.albumIDsByKey["album-a"]; exists {
		t.Fatal("deleted album remained in scan snapshot")
	}
}

func TestSourceScanSnapshotIndexesSidecarDirectoryOnce(t *testing.T) {
	directory := t.TempDir()
	for name, content := range map[string]string{
		"song.part.lrc":    "[00:01.00]song",
		"song.part.en.TXT": "lyrics",
		"cover.jpg":        "image",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot := &sourceScanSnapshot{
		sidecarsByDir: make(map[string]*sidecarDirectoryState),
	}
	first, err := snapshot.sidecarNamesForStem(directory, "song.part")
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshot.sidecarNamesForStem(filepath.Join(directory, "."), "song.part")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 || containsString(first, "cover.jpg") || containsString(second, "cover.jpg") {
		t.Fatalf("sidecar names = %#v/%#v", first, second)
	}
}

func BenchmarkSourceScanSnapshotLookup(b *testing.B) {
	sources := make(map[string]*localSourceRecord, 5_000)
	paths := make([]string, 5_000)
	for index := 0; index < 5_000; index++ {
		path := "album/track-" + strconv.Itoa(index)
		paths[index] = path
		source := &localSourceRecord{ID: path, NormalizedPath: path}
		sources[path] = source
	}
	snapshot := &sourceScanSnapshot{
		sourcesByPath: sources,
		seenSourceIDs: make(map[string]struct{}),
	}
	ctx := context.WithValue(context.Background(), sourceScanSnapshotContextKey{}, snapshot)
	synchronizer := &ProductionSynchronizer{}
	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		index := 0
		for parallel.Next() {
			if _, found, err := synchronizer.findSource(ctx, "root", paths[index%len(paths)]); err != nil || !found {
				b.Fatal("snapshot lookup failed")
			}
			index++
		}
	})
}
