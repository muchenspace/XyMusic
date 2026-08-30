package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/testsupport"
)

const migrationTestDatabaseEnvironment = "XYMUSIC_MIGRATION_TEST_DATABASE_URL"

func TestBaselineMigrationDirectoryIsSelfContained(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("baseline migration count = %d, want 1", len(migrations))
	}
	migration := migrations[0]
	if migration.Tag != "0000_initial" || migration.CreatedAt < 1 || migration.Hash == "" || len(migration.SQL) == 0 {
		t.Fatalf("invalid baseline migration: %+v", migration)
	}
	for _, marker := range []string{
		"CREATE TABLE users",
		"CREATE TABLE albums",
		"random_key double precision NOT NULL",
		"CREATE TABLE metadata_writeback_jobs",
		"CREATE TABLE tag_scraping_job_items",
	} {
		if !strings.Contains(strings.Join(migration.SQL, "\n"), marker) {
			t.Fatalf("baseline migration is missing %q", marker)
		}
	}
}

// This test executes the current one-shot baseline against a real PostgreSQL
// transaction in an isolated schema. It deliberately does not exercise the
// removed 0001..0036 upgrade chain: the debug build has a single authoritative
// schema and must initialize a fresh instance from that baseline.
func TestBaselineMigrationExecutesAgainstPostgres(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv(migrationTestDatabaseEnvironment))
	if databaseURL == "" {
		t.Skip("set " + migrationTestDatabaseEnvironment + " to run the PostgreSQL baseline migration test")
	}
	testsupport.RequireWriteIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to migration test database: %v", err)
	}
	defer connection.Close(context.WithoutCancel(ctx))

	transaction, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))

	schema := "xymusic_baseline_" + strings.ReplaceAll(strings.ToLower(uuid.NewString()), "-", "")
	if _, err := transaction.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := transaction.Exec(ctx, "SET LOCAL search_path TO "+pgx.Identifier{schema}.Sanitize()+", public"); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for index, statement := range migrations[0].SQL {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := transaction.Exec(ctx, statement); err != nil {
			t.Fatalf("execute baseline migration statement %d: %v", index, err)
		}
	}

	var tableCount int
	if err := transaction.QueryRow(ctx, `
		SELECT count(*)::int
		FROM information_schema.tables
		WHERE table_schema = $1
		  AND table_name = ANY($2::text[])`, schema, []string{
		"users", "albums", "tracks", "library_roots", "library_scan_runs",
		"metadata_writeback_jobs", "tag_scraping_jobs", "tag_scraping_job_items",
	}).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 8 {
		t.Fatalf("baseline created %d key tables, want 8", tableCount)
	}

	var randomKey, uploadUpdatedAt bool
	if err := transaction.QueryRow(ctx, `
		SELECT
			exists (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'albums' AND column_name = 'random_key'),
			exists (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'media_uploads' AND column_name = 'updated_at')`, schema).Scan(&randomKey, &uploadUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if !randomKey || !uploadUpdatedAt {
		t.Fatalf("baseline column contract missing: albums.random_key=%v media_uploads.updated_at=%v", randomKey, uploadUpdatedAt)
	}
}
