package adminsources

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/platform/mediafile"
)

type SourceMetadataProbe interface {
	Probe(context.Context, string) (mediafile.ProbedMetadataFile, error)
}

type ResourceBudget interface {
	Acquire(context.Context, int) error
	Release(int)
}

type FFprobeMetadataProbe struct {
	executable string
	runner     mediafile.ProcessRunner
}

func NewFFprobeMetadataProbe(executable string, runner mediafile.ProcessRunner) (*FFprobeMetadataProbe, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("local library ffprobe path is required")
	}
	if runner == nil {
		runner = mediafile.OSProcessRunner{}
	}
	return &FFprobeMetadataProbe{executable: executable, runner: runner}, nil
}

func (probe *FFprobeMetadataProbe) Probe(ctx context.Context, path string) (mediafile.ProbedMetadataFile, error) {
	return mediafile.ProbeMetadataFile(ctx, path, probe.executable, probe.runner)
}

type ProductionSynchronizerOptions struct {
	Database     *pgxpool.Pool
	Probe        SourceMetadataProbe
	Now          func() time.Time
	ProbeWorkers int
	ProbeBudget  ResourceBudget
	// LocalMedia and FFmpegPath enable extraction of embedded cover art during
	// a library scan. They are optional so lightweight synchronizer tests and
	// installations without FFmpeg can still scan audio metadata.
	LocalMedia     *localmedia.Store
	FFmpegPath     string
	ArtworkRunner  mediafile.ProcessRunner
	ArtworkWorkers int
}

type ProductionSynchronizer struct {
	database      syncDatabase
	probe         SourceMetadataProbe
	now           func() time.Time
	probeGate     chan struct{}
	probeBudget   ResourceBudget
	localMedia    *localmedia.Store
	ffmpegPath    string
	artworkRunner mediafile.ProcessRunner
	artworkGate   chan struct{}
}

type syncDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}

type scanTransactionContextKey struct{}
type scanBatchCatalogContextKey struct{}
type scanPreparedStabilityContextKey struct{}

func withScanTransaction(ctx context.Context, transaction pgx.Tx) context.Context {
	return context.WithValue(ctx, scanTransactionContextKey{}, transaction)
}

func scanTransactionFromContext(ctx context.Context) pgx.Tx {
	if ctx == nil {
		return nil
	}
	transaction, _ := ctx.Value(scanTransactionContextKey{}).(pgx.Tx)
	return transaction
}

func withScanBatchCatalog(ctx context.Context, cache *scanCatalogCache) context.Context {
	return context.WithValue(ctx, scanBatchCatalogContextKey{}, cache)
}

func scanBatchCatalogFromContext(ctx context.Context) *scanCatalogCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(scanBatchCatalogContextKey{}).(*scanCatalogCache)
	return cache
}

func withPreparedStabilityChecked(ctx context.Context) context.Context {
	return context.WithValue(ctx, scanPreparedStabilityContextKey{}, true)
}

func preparedStabilityWasChecked(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	checked, _ := ctx.Value(scanPreparedStabilityContextKey{}).(bool)
	return checked
}

type scanBatchDatabase struct {
	transaction pgx.Tx
}

func (database *scanBatchDatabase) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	return database.transaction.Query(ctx, query, arguments...)
}

func (database *scanBatchDatabase) QueryRow(ctx context.Context, query string, arguments ...any) pgx.Row {
	return database.transaction.QueryRow(ctx, query, arguments...)
}

func (database *scanBatchDatabase) Exec(ctx context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	return database.transaction.Exec(ctx, query, arguments...)
}

func (database *scanBatchDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	return database.transaction.Begin(ctx)
}

var _ FileSynchronizer = (*ProductionSynchronizer)(nil)
var _ ScanPipeline = (*ProductionSynchronizer)(nil)
var _ PreparedScanBatchPipeline = (*ProductionSynchronizer)(nil)
var _ ScanPreparer = (*ProductionSynchronizer)(nil)
var _ ScanFinalizer = (*ProductionSynchronizer)(nil)

func NewProductionSynchronizer(options ProductionSynchronizerOptions) (*ProductionSynchronizer, error) {
	if options.Database == nil {
		return nil, errors.New("local library synchronizer database is required")
	}
	if options.Probe == nil {
		return nil, errors.New("local library synchronizer metadata probe is required")
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.ProbeWorkers < 0 || options.ProbeWorkers > 128 {
		return nil, errors.New("local library probe worker count must be between 0 and 128")
	}
	probeWorkers := options.ProbeWorkers
	if probeWorkers == 0 {
		probeWorkers = max(8, min(64, runtime.GOMAXPROCS(0)*4))
	}
	if options.ArtworkWorkers < 0 || options.ArtworkWorkers > 64 {
		return nil, errors.New("local library artwork worker count must be between 0 and 64")
	}
	artworkWorkers := options.ArtworkWorkers
	if artworkWorkers == 0 {
		// Cover extraction is FFmpeg work, but one worker leaves most CPUs idle
		// on large libraries. Allow a bounded pool without making it unlimited.
		artworkWorkers = max(2, min(16, runtime.GOMAXPROCS(0)*2))
	}
	artworkRunner := options.ArtworkRunner
	if artworkRunner == nil {
		artworkRunner = mediafile.OSProcessRunner{}
	}
	return &ProductionSynchronizer{
		database:      options.Database,
		probe:         options.Probe,
		now:           options.Now,
		probeGate:     make(chan struct{}, probeWorkers),
		probeBudget:   options.ProbeBudget,
		localMedia:    options.LocalMedia,
		ffmpegPath:    strings.TrimSpace(options.FFmpegPath),
		artworkRunner: artworkRunner,
		artworkGate:   make(chan struct{}, artworkWorkers),
	}, nil
}

type preparedStandardFile struct {
	Metadata         os.FileInfo
	Checksum         string
	Probed           *mediafile.ProbedMetadataFile
	Sidecars         []scannedLyric
	SidecarsReady    bool
	Existing         localSourceRecord
	ExistingFound    bool
	UnchangedReady   bool
	NeedsSidecarSync bool
}
