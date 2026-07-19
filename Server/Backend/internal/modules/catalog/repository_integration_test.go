package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/testsupport"
)

// This integration test uses the isolated test database so its seeded user is
// deterministic while exercising every catalog repository query shape.
func TestRepositoryAgainstConfiguredPostgres(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the PostgreSQL catalog repository test")
	}
	testsupport.RequireWriteIntegration(t)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var userID string
	if err := pool.QueryRow(ctx, "SELECT id FROM users ORDER BY id LIMIT 1").Scan(&userID); err != nil {
		t.Fatalf("isolated catalog database has no seeded user: %v", err)
	}
	repository := NewRepository(pool.Pool)

	tracks, err := repository.ListTracks(ctx, ListTracksQuery{
		UserID: userID, Sort: TrackSortPublishedDesc, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListTracks: %v", err)
	}
	if _, err := repository.RandomTracks(ctx, userID, 0.5, true, 2); err != nil {
		t.Fatalf("RandomTracks upper range: %v", err)
	}
	if _, err := repository.RandomTracks(ctx, userID, 0.5, false, 2); err != nil {
		t.Fatalf("RandomTracks wrapped range: %v", err)
	}
	if len(tracks) > 0 {
		if _, err := repository.FindTrack(ctx, userID, tracks[0].ID); err != nil {
			t.Fatalf("FindTrack: %v", err)
		}
		if _, _, err := repository.ListLyrics(ctx, ListLyricsQuery{TrackID: tracks[0].ID, Limit: 20}); err != nil {
			t.Fatalf("ListLyrics: %v", err)
		}
	}
	cursorTime := time.Now().UTC()
	cursorTitle := "cursor title"
	cursorDisc, cursorTrack := 1, 0
	trackCursorQueries := []ListTracksQuery{
		{UserID: userID, Sort: TrackSortPublishedDesc, After: &TrackCursor{PublishedAt: &cursorTime, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
		{UserID: userID, Sort: TrackSortTitleAsc, After: &TrackCursor{Title: &cursorTitle, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
		{UserID: userID, Sort: TrackSortTitleDesc, ArtistID: "00000000-0000-0000-0000-000000000001", After: &TrackCursor{Title: &cursorTitle, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
		{UserID: userID, Sort: TrackSortAlbumOrderAsc, AlbumID: "00000000-0000-0000-0000-000000000001", After: &TrackCursor{DiscNumber: &cursorDisc, TrackNumber: &cursorTrack, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
	}
	for _, query := range trackCursorQueries {
		if _, err := repository.ListTracks(ctx, query); err != nil {
			t.Fatalf("ListTracks cursor query %s: %v", query.Sort, err)
		}
	}

	artists, err := repository.ListArtists(ctx, ListArtistsQuery{Sort: ArtistSortNameAsc, Limit: 2})
	if err != nil {
		t.Fatalf("ListArtists: %v", err)
	}
	if len(artists) > 0 {
		if _, err := repository.FindArtist(ctx, artists[0].ID); err != nil {
			t.Fatalf("FindArtist: %v", err)
		}
	}
	if _, err := repository.ListArtists(ctx, ListArtistsQuery{
		Sort: ArtistSortNameDesc, After: &SearchCursor{Value: "cursor", ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1,
	}); err != nil {
		t.Fatalf("ListArtists descending cursor: %v", err)
	}

	albums, err := repository.ListAlbums(ctx, ListAlbumsQuery{Sort: AlbumSortReleaseDateDesc, Limit: 2})
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if _, err := repository.RandomAlbums(ctx, 0.5, true, 2); err != nil {
		t.Fatalf("RandomAlbums upper range: %v", err)
	}
	if _, err := repository.RandomAlbums(ctx, 0.5, false, 2); err != nil {
		t.Fatalf("RandomAlbums wrapped range: %v", err)
	}
	if len(albums) > 0 {
		if _, err := repository.FindAlbum(ctx, albums[0].ID); err != nil {
			t.Fatalf("FindAlbum: %v", err)
		}
	}
	releaseDate := "2026-01-01"
	albumCursorQueries := []ListAlbumsQuery{
		{Sort: AlbumSortReleaseDateDesc, After: &AlbumCursor{ReleaseDate: &releaseDate, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
		{Sort: AlbumSortReleaseDateDesc, After: &AlbumCursor{NullRelease: true, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
		{Sort: AlbumSortTitleAsc, ArtistID: "00000000-0000-0000-0000-000000000001", After: &AlbumCursor{Title: &cursorTitle, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
		{Sort: AlbumSortTitleDesc, After: &AlbumCursor{Title: &cursorTitle, ID: "00000000-0000-0000-0000-000000000001"}, Limit: 1},
	}
	for _, query := range albumCursorQueries {
		if _, err := repository.ListAlbums(ctx, query); err != nil {
			t.Fatalf("ListAlbums cursor query %s: %v", query.Sort, err)
		}
	}

	search := SearchQuery{UserID: userID, NormalizedQuery: "a", Pattern: "%a%", Limit: 2}
	if _, err := repository.SearchTracks(ctx, search); err != nil {
		t.Fatalf("SearchTracks: %v", err)
	}
	if _, err := repository.SearchArtists(ctx, search); err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if _, err := repository.SearchAlbums(ctx, search); err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	trigramSearch := SearchQuery{
		UserID: userID, NormalizedQuery: "music", Pattern: "%music%", UseTrigram: true,
		After: &SearchCursor{Value: "cursor", ID: "00000000-0000-0000-0000-000000000001"}, Limit: 2,
	}
	if _, err := repository.SearchTracks(ctx, trigramSearch); err != nil {
		t.Fatalf("SearchTracks trigram cursor: %v", err)
	}
	if _, err := repository.SearchArtists(ctx, trigramSearch); err != nil {
		t.Fatalf("SearchArtists trigram cursor: %v", err)
	}
	if _, err := repository.SearchAlbums(ctx, trigramSearch); err != nil {
		t.Fatalf("SearchAlbums trigram cursor: %v", err)
	}
}
