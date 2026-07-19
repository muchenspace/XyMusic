package admintagscraping

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
)

func TestProductionFingerprintUsesConfiguredToolAndAcoustID(t *testing.T) {
	if os.Getenv("XYMUSIC_LIVE_FINGERPRINT") == "" {
		t.Skip("set XYMUSIC_LIVE_FINGERPRINT=1 to run fpcalc and AcoustID")
	}
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to load fingerprint configuration")
	}
	absolutePath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolutePath).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absolutePath))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scraping.FPcalcPath == "" || cfg.Scraping.AcoustIDClient == "" {
		t.Skip("fpcalc and AcoustID are not configured")
	}
	fpcalcPath := cfg.Scraping.FPcalcPath
	if _, statErr := os.Stat(fpcalcPath); statErr != nil {
		fallback, lookupErr := exec.LookPath(filepath.Base(fpcalcPath))
		if lookupErr != nil {
			t.Skipf("configured fpcalc is unavailable: %v", statErr)
		}
		t.Logf("configured fpcalc is unavailable; testing the PATH executable %s", fallback)
		fpcalcPath = fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var trackID string
	err = pool.Pool.QueryRow(ctx, `
		SELECT mapping.track_id
		FROM local_music_source_tracks mapping
		JOIN local_music_sources source ON source.id = mapping.source_id
		LEFT JOIN library_roots root ON root.id = source.root_id
		WHERE source.status = 'READY'
		ORDER BY source.updated_at DESC LIMIT 1`).Scan(&trackID)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no ready local source is available for fingerprinting")
	}
	if err != nil {
		t.Fatal(err)
	}
	store := NewRepository(pool.Pool)
	fpcalcService, err := NewService(ServiceDependencies{
		Store:                   store,
		Music:                   &fpcalcOnlyMusic{},
		Fingerprinter:           ConfiguredFingerprinter(fpcalcPath),
		Artwork:                 &integrationArtworkNoop{},
		DefaultLibraryDirectory: cfg.LocalLibrary.Directory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fpcalcService.Fingerprint(ctx, trackID); err != nil {
		t.Fatalf("configured fpcalc: %v", err)
	}
	if len(strings.TrimSpace(cfg.Scraping.AcoustIDClient)) < 8 {
		t.Skip("configured AcoustID client ID is rejected by the live service")
	}

	transport := &statusRecordingTransport{delegate: http.DefaultTransport}
	service, err := NewService(ServiceDependencies{
		Store:                   store,
		Music:                   NewMusicPlatformClient(&http.Client{Transport: transport}, cfg.Scraping.AcoustIDClient),
		Fingerprinter:           ConfiguredFingerprinter(fpcalcPath),
		Artwork:                 &integrationArtworkNoop{},
		DefaultLibraryDirectory: cfg.LocalLibrary.Directory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Fingerprint(ctx, trackID); err != nil {
		t.Fatalf("%v (upstream statuses: %v)", err, transport.statusesSnapshot())
	}
}

type integrationArtworkNoop struct{}

func (*integrationArtworkNoop) ApplyAlbumArtwork(context.Context, string, string, string, DownloadedArtwork) error {
	return nil
}
func (*integrationArtworkNoop) ApplyArtistArtwork(
	context.Context,
	string,
	string,
	string,
	int,
	bool,
	DownloadedArtwork,
) error {
	return nil
}

type fpcalcOnlyMusic struct{}

func (*fpcalcOnlyMusic) Search(context.Context, Source, string) ([]Candidate, error) {
	return nil, errors.New("unexpected search call")
}
func (*fpcalcOnlyMusic) SearchArtists(context.Context, Source, string) ([]ArtistCandidate, error) {
	return nil, errors.New("unexpected artist search call")
}
func (*fpcalcOnlyMusic) Lyric(context.Context, Source, string) (string, error) {
	return "", errors.New("unexpected lyric call")
}
func (*fpcalcOnlyMusic) AcoustID(context.Context, float64, string) ([]Candidate, error) {
	return []Candidate{}, nil
}
func (*fpcalcOnlyMusic) DownloadArtwork(context.Context, string) (DownloadedArtwork, error) {
	return DownloadedArtwork{}, errors.New("unexpected artwork call")
}

type statusRecordingTransport struct {
	delegate http.RoundTripper
	mu       sync.Mutex
	statuses []int
}

func (transport *statusRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.delegate.RoundTrip(request)
	if response != nil {
		transport.mu.Lock()
		transport.statuses = append(transport.statuses, response.StatusCode)
		transport.mu.Unlock()
	}
	return response, err
}

func (transport *statusRecordingTransport) statusesSnapshot() []int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]int(nil), transport.statuses...)
}
