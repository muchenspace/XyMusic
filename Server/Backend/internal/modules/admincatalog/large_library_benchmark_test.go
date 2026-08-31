package admincatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/shared/pagination"
)

// BenchmarkConfiguredLargeCatalogCursor is deliberately opt-in. It reads an
// operator-provided integration database, follows up to 100 cursor pages, and
// never inserts or deletes catalog data. Set XYMUSIC_SCALE_BENCHMARK=1 and
// XYMUSIC_INTEGRATION_ENV to run it against a real large library.
func BenchmarkConfiguredLargeCatalogCursor(b *testing.B) {
	if os.Getenv("XYMUSIC_SCALE_BENCHMARK") != "1" {
		b.Skip("set XYMUSIC_SCALE_BENCHMARK=1 to run the configured large-library benchmark")
	}
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		b.Skip("set XYMUSIC_INTEGRATION_ENV to point at an isolated managed configuration")
	}
	absolutePath, err := filepath.Abs(environmentPath)
	if err != nil {
		b.Fatal(err)
	}
	cfg, err := config.NewStore(absolutePath).Load()
	if err != nil {
		b.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absolutePath))
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	service, err := NewServiceWithOptions(
		NewRepository(pool.Pool), benchmarkArtworkPresenter{},
		pagination.NewCursorCodec(cfg.Security.CursorSigningSecret),
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		cursor := ""
		for page := 1; page <= 100; page++ {
			result, err := service.ListTracks(ctx, TrackListInput{
				ListInput: ListInput{
					Page: page, PageSize: 1_000, Sort: "updatedAt",
					Order: SortDescending, Cursor: cursor, CursorMode: true,
				},
			})
			if err != nil {
				b.Fatal(err)
			}
			if result.NextCursor == nil {
				break
			}
			cursor = *result.NextCursor
		}
	}
}

type benchmarkArtworkPresenter struct{}

func (benchmarkArtworkPresenter) Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error) {
	return map[string]catalog.ArtworkDTO{}, nil
}
