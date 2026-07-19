package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyMigrationsCanBeRead(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 28 {
		t.Fatalf("expected 28 migrations, got %d", len(migrations))
	}
	if migrations[0].Tag != "0000_initial" || migrations[25].Tag != "0025_track_permanent_delete_batches" ||
		migrations[26].Tag != "0026_remove_writeback_backup_references" ||
		migrations[27].Tag != "0027_artist_artwork_scraping_jobs" {
		t.Fatalf("unexpected migration boundaries: %s - %s - %s - %s", migrations[0].Tag, migrations[25].Tag, migrations[26].Tag, migrations[27].Tag)
	}
	if len(migrations[0].SQL) < 2 || len(migrations[0].Hash) != 64 {
		t.Fatalf("migration parsing is incompatible: %#v", migrations[0])
	}
}

func TestWritebackBackupReferenceMigrationOnlyClearsDatabasePointers(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[26].SQL, "\n"))
	for _, expected := range []string{"UPDATE METADATA_WRITEBACK_JOBS", "BACKUP_PATH = NULL", "BACKUP_EXPIRES_AT = NULL"} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("migration does not contain %q: %s", expected, sql)
		}
	}
	for _, forbidden := range []string{"DELETE FROM", "DROP COLUMN", "DROP TABLE"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration must not contain %q: %s", forbidden, sql)
		}
	}
}

func TestArtistArtworkBatchMigrationHasDurableFencing(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[27].SQL, "\n"))
	for _, expected := range []string{
		"ARTIST_ARTWORK_SCRAPING_JOBS", "ARTIST_ARTWORK_SCRAPING_JOB_ITEMS",
		"ATTEMPT_ID", "LOCKED_BY", "LOCKED_UNTIL", "ATTEMPTS", "MAX_ATTEMPTS", "NEXT_ATTEMPT_AT",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("artist artwork batch migration does not contain %q", expected)
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
