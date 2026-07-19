package playlist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/testsupport"
)

func TestRepositoryAgainstConfiguredPostgres(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the PostgreSQL playlist repository test")
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	var ownerID string
	if err := pool.QueryRow(ctx, "SELECT id FROM users ORDER BY id LIMIT 1").Scan(&ownerID); err != nil {
		t.Fatalf("isolated playlist database has no seeded user: %v", err)
	}
	trackIDs := make([]string, 0, 3)
	for index := range 3 {
		trackID := uuid.NewString()
		if _, err := pool.Exec(ctx, `INSERT INTO tracks(
			id,title,normalized_title,duration_ms,status,published_at
		) VALUES($1,$2,$3,1000,'READY',now())`,
			trackID, "Playlist Integration "+trackID, "playlist integration "+trackID,
		); err != nil {
			t.Fatalf("insert integration track %d: %v", index, err)
		}
		trackIDs = append(trackIDs, trackID)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupContext, "DELETE FROM tracks WHERE id = ANY($1::uuid[])", trackIDs); err != nil {
			t.Errorf("clean playlist integration tracks: %v", err)
		}
	})

	repository := NewRepository(pool.Pool)
	created, err := repository.CreatePlaylist(ctx, CreatePlaylistParams{
		OwnerID: ownerID, Name: "__codex_playlist_integration_" + uuid.NewString(), Visibility: VisibilityPrivate,
	})
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM playlists WHERE id = $1 AND owner_id = $2", created.ID, ownerID)
	}()

	if _, err := repository.FindPlaylist(ctx, created.ID); err != nil {
		t.Fatalf("FindPlaylist: %v", err)
	}
	for _, sort := range []Sort{SortUpdatedDesc, SortNameAsc, SortNameDesc} {
		if _, err := repository.ListOwned(ctx, ListOwnedQuery{OwnerID: ownerID, Sort: sort, Limit: 2}); err != nil {
			t.Fatalf("ListOwned %s: %v", sort, err)
		}
	}

	description := "integration"
	updated, err := repository.UpdatePlaylist(ctx, UpdatePlaylistParams{
		OwnerID: ownerID, PlaylistID: created.ID, ExpectedVersion: created.Version,
		SetDescription: true, Description: &description,
	})
	if err != nil {
		t.Fatalf("UpdatePlaylist: %v", err)
	}
	if _, err := repository.UpdatePlaylist(ctx, UpdatePlaylistParams{
		OwnerID: ownerID, PlaylistID: created.ID, ExpectedVersion: created.Version,
		Name: stringPointer("stale"),
	}); err == nil {
		t.Fatal("stale UpdatePlaylist unexpectedly succeeded")
	} else {
		var conflict *VersionConflictError
		if !errors.As(err, &conflict) || conflict.CurrentVersion != updated.Version {
			t.Fatalf("stale update error = %v", err)
		}
	}

	version := updated.Version
	entryIDs := make([]string, 0, len(trackIDs))
	for index, trackID := range trackIDs {
		var after *string
		if index == 2 {
			after = &entryIDs[0]
		}
		mutation, err := repository.AddTrack(ctx, AddTrackParams{
			OwnerID: ownerID, PlaylistID: created.ID, ExpectedVersion: version,
			TrackID: trackID, InsertAfterEntryID: after, Now: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("AddTrack %d: %v", index, err)
		}
		version = mutation.Version
		entryIDs = append(entryIDs, mutation.Entry.ID)
	}
	entries, err := repository.ListEntries(ctx, ListEntriesQuery{PlaylistID: created.ID, Limit: MaxPlaylistEntries})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	for index, entry := range entries {
		if entry.Position != index {
			t.Fatalf("entry positions after insert = %#v", entries)
		}
	}
	if len(entryIDs) > 0 {
		if _, err := repository.AddTrack(ctx, AddTrackParams{
			OwnerID: ownerID, PlaylistID: created.ID, ExpectedVersion: version,
			TrackID: trackIDs[0], Now: time.Now().UTC(),
		}); !errors.Is(err, ErrDuplicateTrack) {
			t.Fatalf("duplicate AddTrack error = %v", err)
		}
	}
	if len(entryIDs) > 1 {
		ordered := append([]string(nil), entryIDs...)
		for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
			ordered[left], ordered[right] = ordered[right], ordered[left]
		}
		mutation, err := repository.Reorder(ctx, ReorderParams{
			OwnerID: ownerID, PlaylistID: created.ID, ExpectedVersion: version,
			OrderedEntryIDs: ordered, Now: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("Reorder: %v", err)
		}
		version = mutation.Version
		entries, err = repository.ListEntries(ctx, ListEntriesQuery{PlaylistID: created.ID, Limit: MaxPlaylistEntries})
		if err != nil || entries[0].ID != ordered[0] {
			t.Fatalf("entries after reorder = %#v, %v", entries, err)
		}
	}
	if len(entryIDs) > 0 {
		mutation, err := repository.RemoveTrack(ctx, RemoveTrackParams{
			OwnerID: ownerID, PlaylistID: created.ID, EntryID: entryIDs[0],
			ExpectedVersion: version, Now: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("RemoveTrack: %v", err)
		}
		version = mutation.Version
	}
	if err := repository.DeletePlaylist(ctx, ownerID, created.ID, version); err != nil {
		t.Fatalf("DeletePlaylist: %v", err)
	}
}

func stringPointer(value string) *string { return &value }
