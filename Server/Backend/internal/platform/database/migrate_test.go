package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationsCanBeRead(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 4 {
		t.Fatalf("expected baseline plus three migrations, got %d", len(migrations))
	}
	if migrations[0].Tag != "0000_initial" {
		t.Fatalf("unexpected migration tag: %s", migrations[0].Tag)
	}
	if len(migrations[0].SQL) < 2 {
		t.Fatalf("migration parsing is incompatible: %#v", migrations[0])
	}
	sql := strings.ToUpper(strings.Join(migrations[0].SQL, "\n"))
	for _, expected := range []string{
		"CREATE TABLE USERS",
		"CREATE TABLE AUTH_SESSIONS",
		"CREATE TABLE MEDIA_ASSETS",
		"STORAGE_PATH",
		"CREATE TABLE TRACKS",
		"SOURCE_ASSET_ID",
		"ARCHIVED_MANUALLY",
		"CREATE TABLE LOCAL_MUSIC_SOURCES",
		"CREATE TABLE LOCAL_MUSIC_SOURCE_TRACKS",
		"CREATE TABLE MEDIA_UPLOADS",
		"XYMUSIC_SYNC_LIBRARY_ROOT_SCAN_STATE",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("baseline migration missing expected element %q", expected)
		}
	}
	for _, forbidden := range []string{
		"TRACK_VARIANTS",
		"MEDIA_JOBS",
		"OBJECT_CLEANUP_JOBS",
		"OBJECT_KEY",
		"AUDIO_VARIANT",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("baseline migration contains forbidden legacy element %q", forbidden)
		}
	}
	if migrations[1].Tag != "0001_remove_tag_scraping_batch_limit" {
		t.Fatalf("unexpected tag scraping batch migration: %#v", migrations[1])
	}
	batchSQL := strings.ToUpper(strings.Join(migrations[1].SQL, "\n"))
	if !strings.Contains(batchSQL, "DROP CONSTRAINT IF EXISTS TAG_SCRAPING_JOBS_TOTAL_CHECK") ||
		!strings.Contains(batchSQL, "CHECK (TOTAL >= 1)") {
		t.Fatalf("tag scraping batch migration does not remove the upper bound: %s", batchSQL)
	}
	if migrations[2].Tag != "0002_dismissed_default_library_roots" {
		t.Fatalf("unexpected dismissed default library roots migration: %#v", migrations[2])
	}
	dismissedSQL := strings.ToUpper(strings.Join(migrations[2].SQL, "\n"))
	if !strings.Contains(dismissedSQL, "CREATE TABLE DISMISSED_DEFAULT_LIBRARY_ROOTS") ||
		!strings.Contains(dismissedSQL, "NORMALIZED_PATH") {
		t.Fatalf("dismissed default library roots migration missing table definition: %s", dismissedSQL)
	}
	if migrations[3].Tag != "0003_asset_reference_indexes" {
		t.Fatalf("unexpected asset reference indexes migration: %#v", migrations[3])
	}
	assetIndexesSQL := strings.ToUpper(strings.Join(migrations[3].SQL, "\n"))
	for _, expectedIndex := range []string{
		"LOCAL_MUSIC_SOURCES_SOURCE_ASSET_INDEX",
		"LYRICS_ASSET_ID_INDEX",
		"MEDIA_UPLOADS_ASSET_ID_INDEX",
		"ARTISTS_ARTWORK_ASSET_ID_INDEX",
		"ALBUMS_COVER_ASSET_ID_INDEX",
		"PLAYLISTS_COVER_ASSET_ID_INDEX",
		"USER_PROFILES_AVATAR_ASSET_ID_INDEX",
	} {
		if !strings.Contains(assetIndexesSQL, expectedIndex) {
			t.Fatalf("asset reference indexes migration missing %s: %s", expectedIndex, assetIndexesSQL)
		}
	}
}

func TestBaselineMigrationDoesNotDuplicateTagScrapingAttemptsConstraint(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 1 {
		t.Fatalf("expected a baseline migration, got %d", len(migrations))
	}
	sql := strings.Join(migrations[0].SQL, "\n")
	start := strings.Index(sql, "CREATE TABLE tag_scraping_job_items (")
	if start < 0 {
		t.Fatal("tag scraping item table is missing from the baseline migration")
	}
	remainder := sql[start:]
	end := strings.Index(remainder, "\n);")
	if end < 0 {
		t.Fatal("tag scraping item table definition is incomplete")
	}
	table := remainder[:end]
	if strings.Contains(table, "attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0)") {
		t.Fatal("tag scraping item attempts must not combine an implicit and explicit constraint with the same generated name")
	}
	if count := strings.Count(table, "CONSTRAINT tag_scraping_job_items_attempts_check"); count != 1 {
		t.Fatalf("tag scraping item attempts constraint count=%d, want 1", count)
	}
}
