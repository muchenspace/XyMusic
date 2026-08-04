package adminsources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceScanSnapshotCoalescesObjectStats(t *testing.T) {
	storage := &snapshotStorage{
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	snapshot := &sourceScanSnapshot{objectStats: make(map[string]*sourceObjectStat)}
	const callers = 16
	var group sync.WaitGroup
	group.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer group.Done()
			size, checksum, exists, err := snapshot.statObject(
				context.Background(), storage, "library/source.flac",
			)
			if err != nil || size != 100 || checksum != "checksum" || !exists {
				t.Errorf("stat result = %d/%q/%v/%v", size, checksum, exists, err)
			}
		}()
	}
	select {
	case <-storage.entered:
	case <-time.After(time.Second):
		t.Fatal("object stat did not start")
	}
	close(storage.release)
	group.Wait()
	if calls := storage.calls.Load(); calls != 1 {
		t.Fatalf("object stat calls = %d, want 1", calls)
	}
}

func TestProductionSynchronizerReusesRecentReadyObjectStat(t *testing.T) {
	storage := &snapshotStorage{size: 100, checksum: "checksum"}
	now := time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)
	synchronizer := &ProductionSynchronizer{
		storage: storage, now: func() time.Time { return now },
		readySourceObjectStatTTL: time.Minute,
		readyObjectStats:         make(map[string]readySourceObjectStat),
	}
	checksum := "checksum"
	for index := 0; index < 2; index++ {
		size, gotChecksum, exists, err := synchronizer.statReadySourceObject(
			context.Background(), nil, "library/source.flac", 100, &checksum,
		)
		if err != nil || size != 100 || gotChecksum != checksum || !exists {
			t.Fatalf("stat %d = %d/%q/%v/%v", index, size, gotChecksum, exists, err)
		}
	}
	if calls := storage.calls.Load(); calls != 1 {
		t.Fatalf("object stat calls = %d, want 1", calls)
	}
}

func TestProductionSynchronizerStrictReadyObjectStatDoesNotCache(t *testing.T) {
	storage := &snapshotStorage{size: 100, checksum: "checksum"}
	synchronizer := &ProductionSynchronizer{storage: storage}
	checksum := "checksum"
	for index := 0; index < 2; index++ {
		if _, _, exists, err := synchronizer.statReadySourceObject(
			context.Background(), nil, "library/source.flac", 100, &checksum,
		); err != nil || !exists {
			t.Fatalf("stat %d = %v/%v", index, exists, err)
		}
	}
	if calls := storage.calls.Load(); calls != 2 {
		t.Fatalf("object stat calls = %d, want 2", calls)
	}
}

func TestSourceScanSnapshotCoalescesArtworkExtraction(t *testing.T) {
	snapshot := &sourceScanSnapshot{}
	var calls atomic.Int32
	produce := func() (*stagedArtwork, error) {
		calls.Add(1)
		return &stagedArtwork{ObjectKey: "library/artwork/shared.jpg", SizeBytes: 42, Checksum: "cover"}, nil
	}
	if artwork, err := snapshot.coalescedArtwork(context.Background(), "source", produce); err != nil || artwork == nil {
		t.Fatalf("initial artwork = %#v/%v", artwork, err)
	}
	const callers = 16
	results := make(chan *stagedArtwork, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer group.Done()
			artwork, err := snapshot.coalescedArtwork(context.Background(), "source", func() (*stagedArtwork, error) {
				calls.Add(1_000)
				return nil, errors.New("cached artwork became an owner")
			})
			if err != nil {
				t.Errorf("coalesced artwork error = %v", err)
				return
			}
			results <- artwork
		}()
	}
	group.Wait()
	close(results)
	if calls.Load() != 1 {
		t.Fatalf("artwork extraction calls = %d, want 1", calls.Load())
	}
	for artwork := range results {
		if artwork == nil || artwork.ObjectKey != "library/artwork/shared.jpg" {
			t.Fatalf("coalesced artwork = %#v", artwork)
		}
	}
}

func TestSourceScanSnapshotWakesArtworkWaitersAfterFailureAndAllowsRetry(t *testing.T) {
	snapshot := &sourceScanSnapshot{}
	wantErr := errors.New("synthetic artwork failure")
	var calls atomic.Int32
	if _, err := snapshot.coalescedArtwork(context.Background(), "source", func() (*stagedArtwork, error) {
		calls.Add(1)
		return nil, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("owner artwork error = %v", err)
	}
	if _, found := snapshot.cachedArtwork("source"); found {
		t.Fatal("failed artwork was cached")
	}

	call := &sourceArtworkCall{done: make(chan struct{})}
	snapshot.artworkMu.Lock()
	snapshot.artworkCalls = map[string]*sourceArtworkCall{"source": call}
	snapshot.artworkMu.Unlock()
	secondResult := make(chan error, 1)
	go func() {
		_, err := snapshot.coalescedArtwork(context.Background(), "source", func() (*stagedArtwork, error) {
			return nil, errors.New("waiter became owner")
		})
		secondResult <- err
	}()
	snapshot.artworkMu.Lock()
	call.err = wantErr
	close(call.done)
	snapshot.artworkMu.Unlock()
	if err := <-secondResult; !errors.Is(err, wantErr) {
		t.Fatalf("waiting artwork error = %v", err)
	}
	snapshot.artworkMu.Lock()
	delete(snapshot.artworkCalls, "source")
	snapshot.artworkMu.Unlock()

	artwork, err := snapshot.coalescedArtwork(context.Background(), "source", func() (*stagedArtwork, error) {
		calls.Add(1)
		return &stagedArtwork{ObjectKey: "library/artwork/retry.jpg", SizeBytes: 1, Checksum: "retry"}, nil
	})
	if err != nil || artwork == nil || artwork.ObjectKey != "library/artwork/retry.jpg" {
		t.Fatalf("retry artwork = %#v/%v", artwork, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("retry artwork extraction calls = %d, want 2", calls.Load())
	}
	if _, found := snapshot.cachedArtwork("source"); !found {
		t.Fatal("successful artwork retry was not cached")
	}
}

func TestSourceScanSnapshotServesReadOnlyLookups(t *testing.T) {
	assetID := "asset"
	checksum := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := localSourceRecord{
		ID: "source", RootID: "root", NormalizedPath: "album/song.flac",
		Checksum: checksum, SizeBytes: 100, LastSeenAt: time.Now().Add(-time.Hour), SourceAssetID: &assetID,
	}
	mappings := []cueMapping{{TrackID: "track", Segment: 0, StartMS: 0}}
	snapshot := &sourceScanSnapshot{
		rootPath:         `C:\Music`,
		sourcesByPath:    map[string]localSourceRecord{source.NormalizedPath: source},
		renameCandidates: map[string][]localSourceRecord{checksum: {source}},
		assetsByID: map[string]sourceScanAsset{assetID: {
			objectKey: "library/source.flac", sizeBytes: 100, checksum: &checksum, ready: true,
		}},
		mappingsBySource:   map[string][]cueMapping{source.ID: mappings},
		externalLyricsByID: map[string]bool{source.ID: true},
		objectStats:        make(map[string]*sourceObjectStat),
	}
	ctx := context.WithValue(context.Background(), sourceScanSnapshotContextKey{}, snapshot)
	synchronizer := &ProductionSynchronizer{storage: &snapshotStorage{size: 100, checksum: checksum}}

	got, found, err := synchronizer.findSource(ctx, "root", source.NormalizedPath)
	if err != nil || !found || got.ID != source.ID {
		t.Fatalf("find source = %+v/%v/%v", got, found, err)
	}
	candidates, err := synchronizer.findRenameCandidates(ctx, "root", checksum, time.Now())
	if err != nil || len(candidates) != 1 || candidates[0].ID != source.ID {
		t.Fatalf("rename candidates = %+v/%v", candidates, err)
	}
	if rootPath, err := synchronizer.rootPath(ctx, "root"); err != nil || rootPath != `C:\Music` {
		t.Fatalf("root path = %q/%v", rootPath, err)
	}
	if external, err := synchronizer.sourceHasExternalLyrics(ctx, source.ID); err != nil || !external {
		t.Fatalf("external lyrics = %v/%v", external, err)
	}
	gotMappings, err := synchronizer.sourceMappings(ctx, source.ID, false)
	if err != nil || len(gotMappings) != 1 || gotMappings[0].TrackID != "track" {
		t.Fatalf("source mappings = %+v/%v", gotMappings, err)
	}
	reusable, err := synchronizer.readySourceAssetReusable(ctx, source)
	if err != nil || !reusable {
		t.Fatalf("reusable source asset = %v/%v", reusable, err)
	}
}

func TestSourceScanSnapshotClaimsRenameCandidatesOnce(t *testing.T) {
	snapshot := &sourceScanSnapshot{}
	if !snapshot.claimRenameCandidate("source") {
		t.Fatal("first rename claim was rejected")
	}
	if snapshot.claimRenameCandidate("source") {
		t.Fatal("duplicate rename claim was accepted")
	}
	if !snapshot.sourceSeen("source") {
		t.Fatal("claimed source was not treated as seen")
	}
	snapshot.releaseRenameCandidate("source")
	if snapshot.sourceSeen("source") {
		t.Fatal("released rename claim remained marked as seen")
	}
	snapshot.markSourceSeen("source")
	if snapshot.claimRenameCandidate("source") {
		t.Fatal("source already seen by its current path was claimed")
	}
}

func TestSourceScanSnapshotCachesCommittedArtworkBySourceChecksum(t *testing.T) {
	snapshot := &sourceScanSnapshot{}
	want := stagedArtwork{ObjectKey: "library/artwork/cover.jpg", SizeBytes: 42, Checksum: "checksum"}
	snapshot.rememberArtwork("source-checksum", want)
	got, found := snapshot.cachedArtwork("source-checksum")
	if !found || got == nil || *got != want {
		t.Fatalf("cached artwork=%#v found=%v", got, found)
	}
	got.ObjectKey = "mutated"
	again, found := snapshot.cachedArtwork("source-checksum")
	if !found || again == nil || again.ObjectKey != want.ObjectKey {
		t.Fatalf("cached artwork was returned by reference: %#v", again)
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
	snapshot := &sourceScanSnapshot{}
	first, err := snapshot.sidecarNames(directory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshot.sidecarNames(filepath.Join(directory, "."))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("sidecar names = %#v/%#v", first, second)
	}
	if first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("cached sidecar names differ: %#v/%#v", first, second)
	}
	ctx := context.WithValue(context.Background(), sourceScanSnapshotContextKey{}, snapshot)
	lyrics, err := readSidecarLyricsCached(ctx.Value(sourceScanSnapshotContextKey{}).(*sourceScanSnapshot), filepath.Join(directory, "song.part.flac"))
	if err != nil || len(lyrics) != 2 || lyrics[0].Language != "und" || lyrics[1].Language != "en" {
		t.Fatalf("stem-indexed lyrics = %#v/%v", lyrics, err)
	}
}

func BenchmarkSourceScanSnapshotLookup(b *testing.B) {
	sources := make(map[string]localSourceRecord, 5_000)
	paths := make([]string, 5_000)
	for index := 0; index < 5_000; index++ {
		path := "album/track-" + strconv.Itoa(index)
		paths[index] = path
		sources[path] = localSourceRecord{ID: path, NormalizedPath: path}
	}
	snapshot := &sourceScanSnapshot{sourcesByPath: sources}
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

type snapshotStorage struct {
	calls    atomic.Int32
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	size     int64
	checksum string
}

func (storage *snapshotStorage) UploadFile(context.Context, string, string, string, string) (int64, error) {
	panic("unexpected UploadFile call")
}

func (storage *snapshotStorage) StatObject(ctx context.Context, _ string) (int64, string, bool, error) {
	storage.calls.Add(1)
	if storage.entered != nil {
		storage.once.Do(func() { close(storage.entered) })
		select {
		case <-storage.release:
		case <-ctx.Done():
			return 0, "", false, ctx.Err()
		}
	}
	if storage.size == 0 {
		storage.size = 100
	}
	if storage.checksum == "" {
		storage.checksum = "checksum"
	}
	return storage.size, storage.checksum, true, nil
}
