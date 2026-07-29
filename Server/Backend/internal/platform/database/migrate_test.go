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
	if len(migrations) != 32 {
		t.Fatalf("expected 32 migrations, got %d", len(migrations))
	}
	if migrations[0].Tag != "0000_initial" || migrations[25].Tag != "0025_track_permanent_delete_batches" ||
		migrations[26].Tag != "0026_remove_writeback_backup_references" ||
		migrations[27].Tag != "0027_artist_artwork_scraping_jobs" || migrations[28].Tag != "0028_lyrics_timing" ||
		migrations[29].Tag != "0029_media_variant_reuse" || migrations[30].Tag != "0030_tag_scraping_progress_and_cancellation" || migrations[31].Tag != "0031_tag_scraping_lease_index" {
		t.Fatalf("unexpected migration boundaries: %s - %s - %s - %s - %s - %s - %s - %s", migrations[0].Tag, migrations[25].Tag, migrations[26].Tag, migrations[27].Tag, migrations[28].Tag, migrations[29].Tag, migrations[30].Tag, migrations[31].Tag)
	}
	if len(migrations[0].SQL) < 2 || len(migrations[0].Hash) != 64 {
		t.Fatalf("migration parsing is incompatible: %#v", migrations[0])
	}
}

func TestMediaVariantReuseMigrationAddsReuseMetadata(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[29].SQL, "\n"))
	for _, expected := range []string{
		"ALTER TABLE TRACK_VARIANTS", "SOURCE_CHECKSUM_SHA256", "PROFILE_VERSION",
		"TRACK_VARIANTS_REUSE_INDEX", "WHERE STATUS = 'READY'",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("media variant reuse migration does not contain %q: %s", expected, sql)
		}
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

func TestLyricsTimingMigrationRecognizesEnhancedLRCFractionSeparators(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.Join(migrations[28].SQL, "\n")
	if !strings.Contains(sql, "([.:][0-9]{1,3})?") {
		t.Fatalf("lyrics timing migration does not accept dot and colon fractions: %s", sql)
	}
	if !strings.Contains(sql, ":[0-5][0-9]") {
		t.Fatalf("lyrics timing migration does not constrain seconds to 00-59: %s", sql)
	}
}

func TestLyricsTimingMigrationRejectsDecreasingWordTimestamps(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[28].SQL, "\n"))
	for _, expected := range []string{
		"REGEXP_MATCHES(",
		"WITH ORDINALITY AS MARKER(PARTS, POSITION)",
		"LAG(SEQUENCED.TIMESTAMP_MS) OVER (ORDER BY SEQUENCED.POSITION)",
		"RPAD(MARKER.PARTS[4], 3, '0')",
		"ORDERED.TIMESTAMP_MS < ORDERED.PREVIOUS_TIMESTAMP_MS",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("lyrics timing migration does not enforce nondecreasing word timestamps; missing %q: %s", expected, sql)
		}
	}
}

func TestLyricsTimingMigrationRequiresCompleteWordTimedDocument(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[28].SQL, "\n"))
	for _, expected := range []string{"REGEXP_SPLIT_TO_TABLE", "EXISTS", "NOT EXISTS", "REGEXP_REPLACE"} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("lyrics timing migration does not contain %q: %s", expected, sql)
		}
	}
}

func TestLyricsTimingMigrationRejectsNonMetadataUntimedLines(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.Join(migrations[28].SQL, "\n")
	for _, expected := range []string{
		`btrim(raw.line, E' \t\n\r\f\v') <> ''`,
		`raw.line !~ E'^\\s*(\\[[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?\\])+'`,
		`raw.line !~ E'^\\s*(\\[[A-Za-z][A-Za-z0-9_-]*:[^\\[\\]\\r\\n]*\\]\\s*)+$'`,
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("lyrics timing migration does not reject non-metadata untimed lines; missing %q: %s", expected, sql)
		}
	}
}

func TestLyricsTimingMigrationRejectsMalformedLaterWordMarkers(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.Join(migrations[28].SQL, "\n")
	if !strings.Contains(sql, "E'<[^>]*(>|$)'") {
		t.Fatalf("lyrics timing migration does not reject malformed word markers: %s", sql)
	}
}

func TestLyricsTimingMigrationBackfillsPersistedMetadataDocuments(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[28].SQL, "\n"))
	for _, expected := range []string{
		"TRACK_METADATA", "RAW_TAGS", "OVERRIDES", "TRACK_METADATA_REVISIONS", "EFFECTIVE_TAGS",
		"METADATA_WRITEBACK_JOBS", "METADATA_SNAPSHOT", "JSONB_SET", "DROP FUNCTION",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("lyrics timing migration does not backfill %q: %s", expected, sql)
		}
	}
}

func TestLyricsTimingMigrationRequiresExplicitFutureWrites(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[28].SQL, "\n"))
	if !strings.Contains(sql, "ALTER TABLE LYRICS ALTER COLUMN TIMING DROP DEFAULT") {
		t.Fatalf("lyrics timing migration leaves an implicit LINE default: %s", sql)
	}
}

func TestLyricsTimingMigrationInvalidatesPreContractIdempotentResponses(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[28].SQL, "\n"))
	if !strings.Contains(sql, "DELETE FROM IDEMPOTENCY_RECORDS") {
		t.Fatalf("lyrics timing migration can replay pre-contract responses: %s", sql)
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
