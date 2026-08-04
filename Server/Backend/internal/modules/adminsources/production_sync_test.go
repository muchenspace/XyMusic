package adminsources

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/modules/adminmetadata"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

func TestRecordScanMetadataRejectsInvalidLyricTimingBeforeDatabase(t *testing.T) {
	tests := []struct {
		name    string
		timing  sharedlyrics.Timing
		content string
	}{
		{name: "missing timing", content: "[00:01.00]line"},
		{name: "unknown timing", timing: "SYLLABLE", content: "[00:01.00]line"},
		{name: "content mismatch", timing: sharedlyrics.TimingLine, content: "[00:01.00]<00:01.00>word"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("recordScanMetadata panicked before validating lyrics: %v", recovered)
				}
			}()
			err := recordScanMetadata(context.Background(), nil, "track", "source", adminmetadata.MetadataSnapshot{
				Lyrics: &adminmetadata.MetadataLyrics{
					Format: "LRC", Timing: test.timing, Content: test.content, Language: "und",
				},
			}, "checksum", time.Now())
			if err == nil || !strings.Contains(err.Error(), "validate scanned local library metadata lyrics") {
				t.Fatalf("recordScanMetadata() error = %v", err)
			}
		})
	}
}

func TestSyncScannedLyricsRejectsInconsistentTimingBeforeDatabase(t *testing.T) {
	transaction := &scannedLyricTransactionStub{}
	err := syncScannedLyrics(context.Background(), transaction, "track", []scannedLyric{{
		Format: "LRC", Timing: sharedlyrics.TimingWord,
		Content: "[00:01.00]<00:01.00>word\n[00:02.00]ordinary line", Language: "und", Origin: "EXTERNAL",
	}})
	if err == nil || transaction.calls != 0 {
		t.Fatalf("syncScannedLyrics() error/calls = %v/%d", err, transaction.calls)
	}
}

type scannedLyricTransactionStub struct {
	pgx.Tx
	calls int
}

func (transaction *scannedLyricTransactionStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	transaction.calls++
	return pgconn.CommandTag{}, errors.New("unexpected database call")
}

func TestProcessFileTouchesExistingSourceBeforeRecordingProcessingFailure(t *testing.T) {
	seenAt := time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC)
	now := seenAt.Add(time.Second)
	database := &processFailureDatabase{
		sourceID:  "00000000-0000-4000-8000-000000000020",
		status:    SourceFileReady,
		updatedAt: seenAt.Add(-time.Hour),
	}
	synchronizer := &ProductionSynchronizer{database: database, now: func() time.Time { return now }}
	failure := errors.New("metadata probe failed")
	err := synchronizer.ProcessFile(context.Background(),
		"00000000-0000-4000-8000-000000000021", "", DiscoveredFile{
			RelativePath: "music/song.flac", ScanError: failure,
		}, seenAt)
	if !errors.Is(err, failure) {
		t.Fatalf("processing error=%v", err)
	}
	if database.beginCount != 2 || database.commitCount != 2 {
		t.Fatalf("transactions begun=%d committed=%d", database.beginCount, database.commitCount)
	}
	if database.status != SourceFileFailed || database.lastError != failure.Error() {
		t.Fatalf("source status=%s error=%q", database.status, database.lastError)
	}
	if !database.lastSeenAt.Equal(seenAt) {
		t.Fatalf("last seen=%s want=%s", database.lastSeenAt, seenAt)
	}
	if !database.trackFailed {
		t.Fatal("mapped track was not marked failed")
	}
}

func TestProcessPreparedFileSkipsCommitForReusableUnchangedSource(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "song.flac"
	if err := os.WriteFile(path, []byte("stable audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	synchronizer := &ProductionSynchronizer{}
	err = synchronizer.ProcessPreparedFile(context.Background(), "root", "run", DiscoveredFile{
		AudioPath: path, RelativePath: "song.flac", FileInfo: metadata,
	}, time.Now().UTC(), &preparedStandardFile{
		Metadata: metadata, ExistingFound: true, UnchangedReady: true,
	})
	if err != nil {
		t.Fatalf("reusable unchanged source commit = %v", err)
	}
}

func TestProductionSynchronizerResourceGatesBoundStorageAndFFmpeg(t *testing.T) {
	storage := &boundedSynchronizerStorage{}
	runner := &boundedSynchronizerRunner{}
	synchronizer := &ProductionSynchronizer{
		storage:       storage,
		runner:        runner,
		ffmpegPath:    "ffmpeg",
		storageGate:   make(chan struct{}, 2),
		ffmpegGate:    make(chan struct{}, 1),
		ffmpegThreads: 1,
	}

	var group sync.WaitGroup
	for index := 0; index < 24; index++ {
		group.Add(2)
		go func() {
			defer group.Done()
			if _, err := synchronizer.uploadFile(context.Background(), "object", "path", "audio/flac", "checksum"); err != nil {
				t.Errorf("uploadFile() error = %v", err)
			}
		}()
		go func() {
			defer group.Done()
			if _, err := synchronizer.runFFmpeg(context.Background(), []string{"-i", "source"}, time.Second); err != nil {
				t.Errorf("runFFmpeg() error = %v", err)
			}
		}()
	}
	group.Wait()
	if got := storage.max.Load(); got > 2 {
		t.Fatalf("storage concurrency = %d, want <= 2", got)
	}
	if got := runner.max.Load(); got > 1 {
		t.Fatalf("ffmpeg concurrency = %d, want <= 1", got)
	}
	if storage.max.Load() == 0 || runner.max.Load() == 0 {
		t.Fatal("resource operations did not run")
	}
}

type boundedSynchronizerStorage struct {
	active atomic.Int32
	max    atomic.Int32
}

func (storage *boundedSynchronizerStorage) UploadFile(context.Context, string, string, string, string) (int64, error) {
	current := storage.active.Add(1)
	updateAtomicMaximum(&storage.max, current)
	time.Sleep(time.Millisecond)
	storage.active.Add(-1)
	return 1, nil
}

func (*boundedSynchronizerStorage) StatObject(context.Context, string) (int64, string, bool, error) {
	return 1, "checksum", true, nil
}

type boundedSynchronizerRunner struct {
	active atomic.Int32
	max    atomic.Int32
}

func (runner *boundedSynchronizerRunner) Run(context.Context, string, []string, time.Duration) (adminmetadata.ProcessResult, error) {
	current := runner.active.Add(1)
	updateAtomicMaximum(&runner.max, current)
	time.Sleep(time.Millisecond)
	runner.active.Add(-1)
	return adminmetadata.ProcessResult{}, nil
}

func updateAtomicMaximum(maximum *atomic.Int32, value int32) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}

func TestReadySourceAssetReusableValidatesStoredReference(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	assetID := "00000000-0000-4000-8000-000000000010"
	storageFailure := errors.New("storage unavailable")
	tests := []struct {
		name      string
		row       reusableAssetRow
		storage   reusableAssetStorage
		want      bool
		wantError bool
	}{
		{
			name:    "matching object",
			row:     reusableAssetRow{objectKey: "library/source.flac", sizeBytes: 100, checksum: &checksum},
			storage: reusableAssetStorage{exists: true, sizeBytes: 100, checksum: checksum},
			want:    true,
		},
		{
			name:    "missing object",
			row:     reusableAssetRow{objectKey: "library/source.flac", sizeBytes: 100, checksum: &checksum},
			storage: reusableAssetStorage{},
		},
		{
			name:    "stored size mismatch",
			row:     reusableAssetRow{objectKey: "library/source.flac", sizeBytes: 100, checksum: &checksum},
			storage: reusableAssetStorage{exists: true, sizeBytes: 99, checksum: checksum},
		},
		{
			name:    "stored checksum mismatch",
			row:     reusableAssetRow{objectKey: "library/source.flac", sizeBytes: 100, checksum: &checksum},
			storage: reusableAssetStorage{exists: true, sizeBytes: 100, checksum: strings.Repeat("b", 64)},
		},
		{
			name:    "database asset missing",
			row:     reusableAssetRow{err: pgx.ErrNoRows},
			storage: reusableAssetStorage{exists: true, sizeBytes: 100, checksum: checksum},
		},
		{
			name:      "storage inspection failure",
			row:       reusableAssetRow{objectKey: "library/source.flac", sizeBytes: 100, checksum: &checksum},
			storage:   reusableAssetStorage{err: storageFailure},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synchronizer := &ProductionSynchronizer{
				database: reusableAssetDatabase{row: test.row},
				storage:  &test.storage,
			}
			got, err := synchronizer.readySourceAssetReusable(context.Background(), localSourceRecord{
				SourceAssetID: &assetID, SizeBytes: 100, Checksum: checksum,
			})
			if test.wantError {
				if err == nil {
					t.Fatal("expected source asset inspection error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("reusable=%v want=%v", got, test.want)
			}
		})
	}
}

type reusableAssetDatabase struct{ row reusableAssetRow }

func (database reusableAssetDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	return database.row
}
func (reusableAssetDatabase) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}
func (reusableAssetDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec call")
}
func (reusableAssetDatabase) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected Begin call")
}

type reusableAssetRow struct {
	objectKey string
	sizeBytes int64
	checksum  *string
	err       error
}

func (row reusableAssetRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*string) = row.objectKey
	*destinations[1].(*int64) = row.sizeBytes
	*destinations[2].(**string) = row.checksum
	return nil
}

type reusableAssetStorage struct {
	sizeBytes int64
	checksum  string
	exists    bool
	err       error
}

func (*reusableAssetStorage) UploadFile(context.Context, string, string, string, string) (int64, error) {
	panic("unexpected UploadFile call")
}
func (storage *reusableAssetStorage) StatObject(context.Context, string) (int64, string, bool, error) {
	return storage.sizeBytes, storage.checksum, storage.exists, storage.err
}

type processFailureDatabase struct {
	sourceID    string
	status      SourceFileStatus
	updatedAt   time.Time
	lastSeenAt  time.Time
	lastError   string
	trackFailed bool
	beginCount  int
	commitCount int
}

func (*processFailureDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}
func (*processFailureDatabase) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}
func (*processFailureDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec call")
}
func (database *processFailureDatabase) Begin(context.Context) (pgx.Tx, error) {
	database.beginCount++
	return &processFailureTransaction{database: database, phase: database.beginCount}, nil
}

type processFailureTransaction struct {
	pgx.Tx
	database *processFailureDatabase
	phase    int
}

func (transaction *processFailureTransaction) QueryRow(_ context.Context, _ string, arguments ...any) pgx.Row {
	if transaction.phase == 1 {
		return scanRowFunc(func(destinations ...any) error {
			*destinations[0].(*string) = transaction.database.sourceID
			*destinations[1].(*SourceFileStatus) = transaction.database.status
			*destinations[2].(*time.Time) = transaction.database.updatedAt
			return nil
		})
	}
	return scanRowFunc(func(destinations ...any) error {
		transaction.database.status = SourceFileFailed
		transaction.database.lastError = arguments[2].(string)
		transaction.database.lastSeenAt = arguments[3].(time.Time)
		value := transaction.database.sourceID
		*destinations[0].(**string) = &value
		return nil
	})
}

func (transaction *processFailureTransaction) Exec(
	_ context.Context,
	_ string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	if transaction.phase == 1 {
		transaction.database.lastSeenAt = arguments[1].(time.Time)
		transaction.database.updatedAt = arguments[2].(time.Time)
	} else {
		transaction.database.trackFailed = true
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (transaction *processFailureTransaction) Commit(context.Context) error {
	transaction.database.commitCount++
	return nil
}

func (*processFailureTransaction) Rollback(context.Context) error { return nil }

type scanRowFunc func(...any) error

func (row scanRowFunc) Scan(destinations ...any) error { return row(destinations...) }
