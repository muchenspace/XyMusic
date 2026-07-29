package database

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/testsupport"
)

const migrationTestDatabaseEnvironment = "XYMUSIC_MIGRATION_TEST_DATABASE_URL"

type lyricsTimingMigrationCase struct {
	id      string
	content string
	want    string
}

func TestLyricsTimingMigrationPostgresBehavior(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv(migrationTestDatabaseEnvironment))
	if databaseURL == "" {
		t.Skip("set " + migrationTestDatabaseEnvironment + " to run the PostgreSQL migration behavior test")
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

	schema := "xymusic_migration_0028_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schemaIdentifier := pgx.Identifier{schema}.Sanitize()
	if _, err := transaction.Exec(ctx, "CREATE SCHEMA "+schemaIdentifier); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := transaction.Exec(ctx, "SET LOCAL search_path TO "+schemaIdentifier); err != nil {
		t.Fatalf("isolate migration search path: %v", err)
	}

	createLyricsTimingMigrationTables(t, ctx, transaction)
	cases := seedLyricsTimingMigrationFixtures(t, ctx, transaction)
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) <= 28 || migrations[28].Tag != "0028_lyrics_timing" {
		t.Fatalf("lyrics timing migration is unavailable: count=%d", len(migrations))
	}
	for index, statement := range migrations[28].SQL {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := transaction.Exec(ctx, statement); err != nil {
			t.Fatalf("execute lyrics timing migration statement %d: %v", index, err)
		}
	}

	assertLyricsTimingMigrationRows(t, ctx, transaction, cases)
	assertLyricsTimingMetadataBackfill(t, ctx, transaction)
	assertLyricsTimingColumnContract(t, ctx, transaction, schema)
	assertLyricsTimingMigrationCleanup(t, ctx, transaction, schema)
	assertLyricsTimingRequiresExplicitInsert(t, ctx, transaction)
}

func TestTagScrapingProgressMigrationPostgresBehavior(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv(migrationTestDatabaseEnvironment))
	if databaseURL == "" {
		t.Skip("set " + migrationTestDatabaseEnvironment + " to run the PostgreSQL migration behavior test")
	}
	testsupport.RequireWriteIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to migration test database: %v", err)
	}
	defer connection.Close(context.WithoutCancel(ctx))

	schema := "xymusic_migration_0030_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schemaIdentifier := pgx.Identifier{schema}.Sanitize()
	if _, err := connection.Exec(ctx, "CREATE SCHEMA "+schemaIdentifier); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	defer func() {
		_, _ = connection.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaIdentifier+" CASCADE")
	}()

	itemStatusType := schemaIdentifier + ".\"tag_scraping_item_status\""
	jobStatusType := schemaIdentifier + ".\"tag_scraping_job_status\""
	itemsTable := schemaIdentifier + ".\"tag_scraping_job_items\""
	for _, statement := range []string{
		`CREATE TYPE ` + jobStatusType + ` AS ENUM ('PENDING', 'RUNNING', 'COMPLETED', 'CANCELLED', 'FAILED')`,
		`CREATE TYPE ` + itemStatusType + ` AS ENUM ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'SKIPPED')`,
		`CREATE TABLE ` + itemsTable + ` (
			id text PRIMARY KEY,
			status ` + itemStatusType + ` NOT NULL,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			updated_at timestamp with time zone NOT NULL DEFAULT now()
		)`,
		`INSERT INTO ` + itemsTable + ` (id, status) VALUES ('pending', 'PENDING'), ('running', 'RUNNING')`,
	} {
		if _, err := connection.Exec(ctx, statement); err != nil {
			t.Fatalf("create migration fixture: %v", err)
		}
	}
	for _, statement := range []string{
		`ALTER TYPE ` + jobStatusType + ` ADD VALUE IF NOT EXISTS 'CANCELLING'`,
		`ALTER TYPE ` + itemStatusType + ` ADD VALUE IF NOT EXISTS 'CANCELLED'`,
	} {
		if _, err := connection.Exec(ctx, statement); err != nil {
			t.Fatalf("prepare migration enum: %v", err)
		}
	}

	transaction, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))
	if _, err := transaction.Exec(ctx, "SET LOCAL search_path TO "+schemaIdentifier); err != nil {
		t.Fatalf("isolate migration search path: %v", err)
	}

	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) <= 30 || migrations[30].Tag != "0030_tag_scraping_progress_and_cancellation" {
		t.Fatalf("tag scraping progress migration is unavailable: count=%d", len(migrations))
	}
	statement := strings.ReplaceAll(
		strings.Join(migrations[30].SQL, "\n"),
		`"public".`,
		schemaIdentifier+".",
	)
	if _, err := transaction.Exec(ctx, statement); err != nil {
		t.Fatalf("execute tag scraping progress migration in one transaction: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit tag scraping progress migration: %v", err)
	}

	for _, fixture := range []struct {
		id    string
		stage string
	}{
		{id: "pending", stage: "WAITING_EXECUTION"},
		{id: "running", stage: "WAITING_EXECUTION"},
	} {
		var stage string
		if err := connection.QueryRow(ctx, "SELECT stage FROM "+itemsTable+" WHERE id=$1", fixture.id).Scan(&stage); err != nil {
			t.Fatalf("read migrated stage for %s: %v", fixture.id, err)
		}
		if stage != fixture.stage {
			t.Fatalf("migrated stage for %s = %q, want %q", fixture.id, stage, fixture.stage)
		}
	}

	if _, err := connection.Exec(ctx, "INSERT INTO "+itemsTable+" (id, status) VALUES ('cancelled', 'CANCELLED')"); err != nil {
		t.Fatalf("use new item status after migration commit: %v", err)
	}
	var status string
	if err := connection.QueryRow(ctx, "SELECT status::text FROM "+itemsTable+" WHERE id='cancelled'").Scan(&status); err != nil {
		t.Fatalf("read newly added item status: %v", err)
	}
	if status != "CANCELLED" {
		t.Fatalf("new item status = %q, want CANCELLED", status)
	}
}

func createLyricsTimingMigrationTables(t *testing.T, ctx context.Context, transaction pgx.Tx) {
	t.Helper()
	statements := []string{
		`CREATE TABLE lyrics (
			id text PRIMARY KEY,
			format text NOT NULL,
			content text NOT NULL
		)`,
		`CREATE TABLE track_metadata (
			id text PRIMARY KEY,
			raw_tags jsonb NOT NULL,
			overrides jsonb NOT NULL
		)`,
		`CREATE TABLE track_metadata_revisions (
			id text PRIMARY KEY,
			raw_tags jsonb NOT NULL,
			overrides jsonb NOT NULL,
			effective_tags jsonb NOT NULL
		)`,
		`CREATE TABLE metadata_writeback_jobs (
			id text PRIMARY KEY,
			metadata_snapshot jsonb NOT NULL
		)`,
		`CREATE TABLE idempotency_records (id text PRIMARY KEY)`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(ctx, statement); err != nil {
			t.Fatalf("create migration fixture table: %v", err)
		}
	}
}

func seedLyricsTimingMigrationFixtures(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
) []lyricsTimingMigrationCase {
	t.Helper()
	cases := []lyricsTimingMigrationCase{
		{id: "ordinary", content: "[00:01.00]ordinary line", want: "LINE"},
		{
			id:      "metadata-enhanced",
			content: "[ar:Artist]\n[ti:Song]\n[offset:0]\n[00:01]<00:01>word",
			want:    "WORD",
		},
		{
			id:      "timed-mixed",
			content: "[00:01]<00:01>word\n[00:02]ordinary line",
			want:    "LINE",
		},
		{
			id:      "untimed-mixed",
			content: "[00:01]<00:01>word\nordinary lyric",
			want:    "LINE",
		},
		{
			id:      "malformed-marker",
			content: "[00:01]<00:01>valid<00:60>invalid",
			want:    "LINE",
		},
		{id: "blank-word-text", content: "[00:01]<00:01>", want: "LINE"},
		{
			id:      "crlf-enhanced",
			content: "[ar:Artist]\r\n[00:01.0]<00:01.0>first\r\n[00:02.00]<00:02.00>second",
			want:    "WORD",
		},
		{
			id:      "decreasing",
			content: "[00:10]<00:11>late<00:10>early",
			want:    "LINE",
		},
		{
			id:      "equal",
			content: "[00:10]<00:10>first<00:10>second",
			want:    "WORD",
		},
		{
			id:      "cross-line-reset",
			content: "[00:10]<00:10>first<00:11>later\n[00:02]<00:02>second<00:03>later",
			want:    "WORD",
		},
		{
			id:      "fraction-decrease",
			content: "[00:10]<00:10.1>later<00:10:099>earlier",
			want:    "LINE",
		},
		{
			id:      "fraction-equal",
			content: "[00:10]<00:10.1>first<00:10:100>second",
			want:    "WORD",
		},
		{
			id:      "fraction-increase",
			content: "[00:10]<00:10:1>first<00:10.20>second<00:10:300>third",
			want:    "WORD",
		},
	}
	for _, test := range cases {
		if _, err := transaction.Exec(ctx,
			`INSERT INTO lyrics (id, format, content) VALUES ($1, 'LRC', $2)`,
			test.id, test.content,
		); err != nil {
			t.Fatalf("seed lyrics timing case %s: %v", test.id, err)
		}
	}

	wordContent := "[ar:Artist]\n[00:01]<00:01>word"
	lineContent := "[00:01]ordinary line"
	timedMixedContent := "[00:01]<00:01>word\n[00:02]ordinary line"
	equalContent := "[00:10]<00:10>first<00:10>second"
	decreasingContent := "[00:10]<00:11>late<00:10>early"
	crossLineContent := "[00:10]<00:10>first<00:11>later\n[00:02]<00:02>second<00:03>later"
	if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata (id, raw_tags, overrides) VALUES (
		'metadata',
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $1::text)),
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $2::text))
	)`, wordContent, lineContent); err != nil {
		t.Fatalf("seed track metadata: %v", err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata_revisions (
		id, raw_tags, overrides, effective_tags
	) VALUES (
		'revision',
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $1::text)),
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $2::text)),
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $3::text))
	)`, timedMixedContent, equalContent, decreasingContent); err != nil {
		t.Fatalf("seed metadata revision: %v", err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO metadata_writeback_jobs (id, metadata_snapshot) VALUES (
		'writeback',
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $1::text))
	)`, crossLineContent); err != nil {
		t.Fatalf("seed metadata writeback: %v", err)
	}
	if _, err := transaction.Exec(ctx,
		`INSERT INTO idempotency_records (id) VALUES ('first'), ('second')`,
	); err != nil {
		t.Fatalf("seed idempotency records: %v", err)
	}
	return cases
}

func assertLyricsTimingMigrationRows(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	cases []lyricsTimingMigrationCase,
) {
	t.Helper()
	for _, test := range cases {
		var actual string
		if err := transaction.QueryRow(ctx,
			`SELECT timing::text FROM lyrics WHERE id=$1`, test.id,
		).Scan(&actual); err != nil {
			t.Fatalf("read lyrics timing case %s: %v", test.id, err)
		}
		if actual != test.want {
			t.Fatalf("lyrics timing case %s=%s, want %s", test.id, actual, test.want)
		}
	}
}

func assertLyricsTimingMetadataBackfill(t *testing.T, ctx context.Context, transaction pgx.Tx) {
	t.Helper()
	var raw, overrides string
	if err := transaction.QueryRow(ctx, `SELECT
		raw_tags #>> '{lyrics,timing}', overrides #>> '{lyrics,timing}'
		FROM track_metadata WHERE id='metadata'`).Scan(&raw, &overrides); err != nil {
		t.Fatal(err)
	}
	if raw != "WORD" || overrides != "LINE" {
		t.Fatalf("track metadata timing raw/overrides=%s/%s, want WORD/LINE", raw, overrides)
	}

	var revisionRaw, revisionOverrides, effective string
	if err := transaction.QueryRow(ctx, `SELECT
		raw_tags #>> '{lyrics,timing}', overrides #>> '{lyrics,timing}',
		effective_tags #>> '{lyrics,timing}'
		FROM track_metadata_revisions WHERE id='revision'`).Scan(
		&revisionRaw, &revisionOverrides, &effective,
	); err != nil {
		t.Fatal(err)
	}
	if revisionRaw != "LINE" || revisionOverrides != "WORD" || effective != "LINE" {
		t.Fatalf("revision timing raw/overrides/effective=%s/%s/%s, want LINE/WORD/LINE",
			revisionRaw, revisionOverrides, effective)
	}

	var snapshot string
	if err := transaction.QueryRow(ctx, `SELECT metadata_snapshot #>> '{lyrics,timing}'
		FROM metadata_writeback_jobs WHERE id='writeback'`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot != "WORD" {
		t.Fatalf("writeback snapshot timing=%s, want WORD", snapshot)
	}
}

func assertLyricsTimingColumnContract(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	schema string,
) {
	t.Helper()
	var nullable string
	var defaultValue *string
	if err := transaction.QueryRow(ctx, `SELECT is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='lyrics' AND column_name='timing'`, schema,
	).Scan(&nullable, &defaultValue); err != nil {
		t.Fatal(err)
	}
	if nullable != "NO" || defaultValue != nil {
		t.Fatalf("lyrics timing nullable/default=%s/%v, want NO/<nil>", nullable, defaultValue)
	}
}

func assertLyricsTimingMigrationCleanup(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	schema string,
) {
	t.Helper()
	var helperCount int
	if err := transaction.QueryRow(ctx, `SELECT count(*)
		FROM pg_proc procedure
		JOIN pg_namespace namespace ON namespace.oid=procedure.pronamespace
		WHERE namespace.nspname=$1 AND procedure.proname='xymusic_detect_lyrics_timing'`, schema,
	).Scan(&helperCount); err != nil {
		t.Fatal(err)
	}
	if helperCount != 0 {
		t.Fatalf("lyrics timing migration helper remains: %d", helperCount)
	}
	var idempotencyCount int
	if err := transaction.QueryRow(ctx, `SELECT count(*) FROM idempotency_records`).Scan(&idempotencyCount); err != nil {
		t.Fatal(err)
	}
	if idempotencyCount != 0 {
		t.Fatalf("idempotency records remaining=%d, want 0", idempotencyCount)
	}
}

func assertLyricsTimingRequiresExplicitInsert(t *testing.T, ctx context.Context, transaction pgx.Tx) {
	t.Helper()
	if _, err := transaction.Exec(ctx, `SAVEPOINT lyrics_timing_required`); err != nil {
		t.Fatal(err)
	}
	_, insertError := transaction.Exec(ctx, `INSERT INTO lyrics (id, format, content)
		VALUES ('missing-timing', 'LRC', '[00:01]line')`)
	if _, err := transaction.Exec(ctx, `ROLLBACK TO SAVEPOINT lyrics_timing_required`); err != nil {
		t.Fatalf("restore transaction after omitted timing insert: %v", err)
	}
	if _, err := transaction.Exec(ctx, `RELEASE SAVEPOINT lyrics_timing_required`); err != nil {
		t.Fatalf("release omitted timing savepoint: %v", err)
	}
	if insertError == nil {
		t.Fatal("inserting lyrics without timing succeeded")
	}
	var postgresError *pgconn.PgError
	if !errors.As(insertError, &postgresError) || postgresError.Code != "23502" {
		t.Fatalf("omitted timing insert error=%v, want PostgreSQL not-null violation", insertError)
	}
	var alive int
	if err := transaction.QueryRow(ctx, `SELECT 1`).Scan(&alive); err != nil || alive != 1 {
		t.Fatalf("migration test transaction is unusable after savepoint recovery: %v", err)
	}
}
