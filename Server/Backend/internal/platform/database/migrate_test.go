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
	if len(migrations) != 37 {
		t.Fatalf("expected 37 migrations, got %d", len(migrations))
	}
	if migrations[0].Tag != "0000_initial" || migrations[25].Tag != "0025_track_permanent_delete_batches" ||
		migrations[26].Tag != "0026_remove_writeback_backup_references" ||
		migrations[27].Tag != "0027_artist_artwork_scraping_jobs" || migrations[28].Tag != "0028_lyrics_timing" ||
		migrations[29].Tag != "0029_media_variant_reuse" || migrations[30].Tag != "0030_tag_scraping_claim_indexes" ||
		migrations[31].Tag != "0031_local_music_scan_indexes" || migrations[32].Tag != "0032_tag_scraping_large_batches" ||
		migrations[33].Tag != "0033_tag_scraping_item_retries" || migrations[34].Tag != "0034_scan_state_and_writeback_authority" || migrations[35].Tag != "0035_configuration_managed_library_roots" || migrations[36].Tag != "0036_track_manual_archive_flag" {
		t.Fatalf("unexpected migration boundaries: %s - %s - %s - %s - %s - %s - %s - %s - %s - %s - %s - %s - %s", migrations[0].Tag, migrations[25].Tag, migrations[26].Tag, migrations[27].Tag, migrations[28].Tag, migrations[29].Tag, migrations[30].Tag, migrations[31].Tag, migrations[32].Tag, migrations[33].Tag, migrations[34].Tag, migrations[35].Tag, migrations[36].Tag)
	}
	if len(migrations[0].SQL) < 2 || len(migrations[0].Hash) != 64 {
		t.Fatalf("migration parsing is incompatible: %#v", migrations[0])
	}
}

func TestTrackManualArchiveFlagMigrationAddsTrackColumn(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) <= 36 || migrations[36].Tag != "0036_track_manual_archive_flag" {
		t.Fatalf("track archive flag migration is unavailable: count=%d", len(migrations))
	}
	sql := strings.ToUpper(strings.Join(migrations[36].SQL, "\n"))
	if !strings.Contains(sql, "ALTER TABLE TRACKS ADD COLUMN ARCHIVED_MANUALLY BOOLEAN NOT NULL DEFAULT FALSE") {
		t.Fatalf("track archive flag migration does not add the track column: %s", sql)
	}
}
func TestConfigurationManagedRootMigrationAddsOwnershipColumn(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) <= 35 || migrations[35].Tag != "0035_configuration_managed_library_roots" {
		t.Fatalf("configuration-managed root migration is unavailable: count=%d", len(migrations))
	}
	sql := strings.ToUpper(strings.Join(migrations[35].SQL, "\n"))
	for _, expected := range []string{
		"ADD COLUMN CONFIGURATION_MANAGED BOOLEAN NOT NULL DEFAULT FALSE",
		"SET CONFIGURATION_MANAGED = TRUE",
		"ORDER BY CREATED_AT ASC, ID ASC",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("configuration-managed root migration does not contain %q: %s", expected, sql)
		}
	}
}
func TestScanStateWritebackAuthorityMigrationRepairsExistingDatabases(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := strings.ToUpper(strings.Join(migrations[5].SQL, "\n"))
	if strings.Contains(legacySchema, "XYMUSIC_SYNC_LIBRARY_ROOT_SCAN_STATE") {
		t.Fatalf("historical managed library migration was rewritten: %s", legacySchema)
	}
	legacyFencing := strings.ToUpper(strings.Join(migrations[7].SQL, "\n"))
	if !strings.Contains(legacyFencing, `ALTER TABLE "LIBRARY_SCAN_RUNS"`) {
		t.Fatalf("historical worker fencing migration was rewritten: %s", legacyFencing)
	}
	fix := strings.ToUpper(strings.Join(migrations[34].SQL, "\n"))
	for _, expected := range []string{
		"XYMUSIC_SYNC_LIBRARY_ROOT_SCAN_STATE",
		"LIBRARY_SCAN_RUNS_ROOT_STATE_TRIGGER",
		"AFTER INSERT OR UPDATE OF STATUS",
		"NEW.STATUS IN ('PENDING', 'RUNNING')",
		"ACTIVE.STATUS IN ('PENDING', 'RUNNING')",
		"NEW.STATUS IN ('COMPLETED', 'FAILED', 'CANCELLED')",
		"ROOT.VERSION <> NEW.ROOT_VERSION THEN NULL",
		"STATE.LATEST_ROOT_VERSION <> ROOT.VERSION",
		"ROOT.STATUS = 'SCANNING'",
	} {
		if !strings.Contains(fix, expected) {
			t.Fatalf("scan state authority migration does not contain %q: %s", expected, fix)
		}
	}
	if strings.Contains(fix, "DROP COLUMN TAG_WRITEBACK_ENABLED") {
		t.Fatalf("scan state repair must not destructively drop the legacy writeback column: %s", fix)
	}
}

func TestLocalMusicScanMigrationAddsRootScopedScanIndexes(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[31].SQL, "\n"))
	for _, expected := range []string{
		"LOCAL_MUSIC_SOURCES_ROOT_SEEN_INDEX",
		"LOCAL_MUSIC_SOURCES_ROOT_CHECKSUM_INDEX",
		"ROOT_ID", "LAST_SEEN_AT", "CHECKSUM_SHA256",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("local music scan migration does not contain %q: %s", expected, sql)
		}
	}
}

func TestTagScrapingLargeBatchMigrationRaisesJobLimit(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[32].SQL, "\n"))
	if !strings.Contains(sql, `"TOTAL" BETWEEN 1 AND 5000`) {
		t.Fatalf("large tag scraping batch migration does not raise the total limit: %s", sql)
	}
}

func TestTagScrapingItemRetryMigrationAddsDurableClaimState(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[33].SQL, "\n"))
	for _, expected := range []string{
		"TAG_SCRAPING_JOB_ITEMS",
		"ATTEMPTS",
		"MAX_ATTEMPTS",
		"NEXT_ATTEMPT_AT",
		"TAG_SCRAPING_JOB_ITEMS_ATTEMPTS_CHECK",
		"TAG_SCRAPING_JOB_ITEMS_PENDING_READY_INDEX",
		"WHERE \"STATUS\" = 'PENDING'",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("tag scraping retry migration does not contain %q: %s", expected, sql)
		}
	}
}

func TestTagScrapingClaimMigrationAddsPartialWorkIndexes(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(strings.Join(migrations[30].SQL, "\n"))
	for _, expected := range []string{
		"TAG_SCRAPING_JOB_ITEMS_PENDING_POSITION_INDEX",
		"TAG_SCRAPING_JOB_ITEMS_RUNNING_LEASE_INDEX",
		"TAG_SCRAPING_JOB_ITEMS_RECOVERY_INDEX",
		"WHERE \"STATUS\" = 'PENDING'",
		"WHERE \"STATUS\" = 'RUNNING'",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("tag scraping claim migration does not contain %q: %s", expected, sql)
		}
	}
}

func TestMediaVariantReuseMigrationAddsFutureReuseMetadata(t *testing.T) {
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
		"TRACK_METADATA", "RAW_TAGS", "OVERRIDES",
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
