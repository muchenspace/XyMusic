package admincatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
)

func TestRepositoryQueriesConfiguredProductionCatalog(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production admin catalog queries")
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool.Pool)
	for _, sortName := range []string{"name", "createdAt", "updatedAt"} {
		items, _, err := repository.ListArtists(ctx, ArtistQuery{Sort: sortName, Order: SortAscending, Limit: 2})
		if err != nil {
			t.Fatalf("ListArtists %s: %v", sortName, err)
		}
		if len(items) > 0 {
			if _, err := repository.FindArtist(ctx, items[0].ID); err != nil {
				t.Fatalf("FindArtist: %v", err)
			}
		}
	}
	for _, sortName := range []string{"title", "createdAt", "updatedAt", "releaseDate"} {
		items, _, err := repository.ListAlbums(ctx, AlbumQuery{Sort: sortName, Order: SortDescending, Limit: 2})
		if err != nil {
			t.Fatalf("ListAlbums %s: %v", sortName, err)
		}
		if len(items) > 0 {
			if _, _, _, err := repository.FindAlbum(ctx, items[0].ID, 25, 0); err != nil {
				t.Fatalf("FindAlbum: %v", err)
			}
		}
	}
	if _, err := repository.FindDuplicateAlbums(ctx, DuplicateAlbumQuery{Limit: 25, AlbumLimit: 100}); err != nil {
		t.Fatalf("FindDuplicateAlbums: %v", err)
	}
	for _, metadataStatus := range []MetadataStatus{"", MetadataOriginal, MetadataOverridden, MetadataPendingWrite, MetadataWriteFailed} {
		items, _, err := repository.ListTracks(ctx, TrackQuery{
			Sort: "updatedAt", Order: SortDescending, MetadataStatus: metadataStatus, Limit: 2,
		})
		if err != nil {
			t.Fatalf("ListTracks metadata %s: %v", metadataStatus, err)
		}
		if len(items) > 0 {
			if _, _, err := repository.FindTrack(ctx, items[0].ID, 20, 0); err != nil {
				t.Fatalf("FindTrack: %v", err)
			}
		}
	}
	for _, audioStatus := range []AudioStatus{AudioStatusProcessing, AudioStatusReady, AudioStatusError, AudioStatusArchived} {
		if _, _, err := repository.ListTracks(ctx, TrackQuery{
			Sort: "status", Order: SortAscending, Status: audioStatus, Limit: 2,
		}); err != nil {
			t.Fatalf("ListTracks audio status %s: %v", audioStatus, err)
		}
	}
}
