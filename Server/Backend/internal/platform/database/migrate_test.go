package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationsCanBeRead(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected 1 baseline migration, got %d", len(migrations))
	}
	if migrations[0].Tag != "0000_initial" {
		t.Fatalf("unexpected migration tag: %s", migrations[0].Tag)
	}
	if len(migrations[0].SQL) < 2 || len(migrations[0].Hash) != 64 {
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
}

func TestMigrationCompatibilityRequiresExactPrefix(t *testing.T) {
	available := []Migration{{CreatedAt: 1, Hash: "a"}, {CreatedAt: 2, Hash: "b"}}
	if err := AssertCompatible(available, []AppliedMigration{{CreatedAt: 1, Hash: "a"}}); err != nil {
		t.Fatal(err)
	}
	assertCompatibilityKind(t,
		AssertCompatible(available, []AppliedMigration{{CreatedAt: 1, Hash: "changed"}}),
		CompatibilityHashMismatch,
	)
	assertCompatibilityKind(t,
		AssertCompatible(available, []AppliedMigration{{CreatedAt: 2, Hash: "b"}}),
		CompatibilityHistoryForked,
	)
	assertCompatibilityKind(t,
		AssertCompatible(available, []AppliedMigration{{CreatedAt: 1, Hash: "a"}, {CreatedAt: 2, Hash: "b"}, {CreatedAt: 3, Hash: "c"}}),
		CompatibilityNewerSchema,
	)
}

func assertCompatibilityKind(t *testing.T, err error, expected CompatibilityErrorKind) {
	t.Helper()
	if !IsPermanentMigrationError(err) {
		t.Fatalf("expected permanent migration error, got %v", err)
	}
	var compatibility *CompatibilityError
	if !errors.As(err, &compatibility) || compatibility.Kind != expected {
		t.Fatalf("compatibility kind = %q, want %q", compatibility.Kind, expected)
	}
}

func TestBaselineMigrationDoesNotDuplicateTagScrapingAttemptsConstraint(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected one baseline migration, got %d", len(migrations))
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
