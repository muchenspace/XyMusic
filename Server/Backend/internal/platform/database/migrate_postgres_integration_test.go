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
	crossLineContent := "[00:10]<00:10>first<00:11>later\n[00:02]<00:02>second<00:03>later"
	if _, err := transaction.Exec(ctx, `INSERT INTO track_metadata (id, raw_tags, overrides) VALUES (
		'metadata',
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $1::text)),
		jsonb_build_object('lyrics', jsonb_build_object('format', 'LRC', 'content', $2::text))
	)`, wordContent, lineContent); err != nil {
		t.Fatalf("seed track metadata: %v", err)
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

func TestScanStateWritebackAuthorityMigrationPostgresBehavior(t *testing.T) {
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

	schema := "xymusic_migration_0034_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schemaIdentifier := pgx.Identifier{schema}.Sanitize()
	if _, err := transaction.Exec(ctx, "CREATE SCHEMA "+schemaIdentifier); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := transaction.Exec(ctx, "SET LOCAL search_path TO "+schemaIdentifier); err != nil {
		t.Fatalf("isolate migration search path: %v", err)
	}

	for _, statement := range []string{
		`CREATE TYPE library_root_status AS ENUM ('UNKNOWN','READY','SCANNING','ERROR','DISABLED')`,
		`CREATE TYPE library_scan_status AS ENUM ('PENDING','RUNNING','COMPLETED','FAILED','CANCELLED')`,
		`CREATE TABLE library_roots (
			id uuid PRIMARY KEY,
			enabled boolean NOT NULL,
			version integer NOT NULL,
			status library_root_status NOT NULL,
			last_scan_at timestamptz,
			last_error text,
			updated_at timestamptz NOT NULL,
			tag_writeback_enabled boolean NOT NULL DEFAULT false
		)`,
		`CREATE TABLE library_scan_runs (
			id uuid PRIMARY KEY,
			root_id uuid NOT NULL REFERENCES library_roots(id),
			root_version integer NOT NULL,
			status library_scan_status NOT NULL,
			completed_at timestamptz,
			last_error text,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL
		)`,
	} {
		if _, err := transaction.Exec(ctx, statement); err != nil {
			t.Fatalf("create scan-state migration fixture: %v", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	readyRootID, failedRootID := uuid.NewString(), uuid.NewString()
	staleRootID, pendingRootID := uuid.NewString(), uuid.NewString()
	previousScanAt := now.Add(-time.Hour)
	pendingPreviousScanAt := now.Add(-2 * time.Hour)
	if _, err := transaction.Exec(ctx, `INSERT INTO library_roots(
		id,enabled,version,status,last_scan_at,last_error,updated_at,tag_writeback_enabled
	) VALUES
		($1,true,1,'SCANNING',NULL,'stale ready error',$5,true),
		($2,true,1,'SCANNING',NULL,'stale failed error',$5,true),
		($3,true,2,'SCANNING',$6,'stale version error',$5,true),
		($4,true,1,'READY',$7,NULL,$5,true)`,
		readyRootID, failedRootID, staleRootID, pendingRootID, now, previousScanAt, pendingPreviousScanAt); err != nil {
		t.Fatalf("seed scan-state migration roots: %v", err)
	}
	readyCompletedAt := now.Add(time.Second)
	failedCompletedAt := now.Add(2 * time.Second)
	if _, err := transaction.Exec(ctx, `INSERT INTO library_scan_runs(
		id,root_id,root_version,status,completed_at,last_error,created_at,updated_at
	) VALUES
		($1,$5,1,'COMPLETED',$9,NULL,$8,$9),
		($2,$6,1,'FAILED',$10,'scan failed',$8,$10),
		($3,$7,1,'COMPLETED',$11,NULL,$8,$11),
		($4,$12,1,'PENDING',NULL,NULL,$8,$8)`,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(),
		readyRootID, failedRootID, staleRootID, now, readyCompletedAt, failedCompletedAt,
		now.Add(3*time.Second), pendingRootID); err != nil {
		t.Fatalf("seed scan-state migration runs: %v", err)
	}

	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) <= 34 || migrations[34].Tag != "0034_scan_state_and_writeback_authority" {
		t.Fatalf("scan-state migration is unavailable: count=%d", len(migrations))
	}
	for index, statement := range migrations[34].SQL {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := transaction.Exec(ctx, statement); err != nil {
			t.Fatalf("execute scan-state migration statement %d: %v", index, err)
		}
	}

	assertMigratedRootState(t, ctx, transaction, readyRootID, "READY", &readyCompletedAt, nil)
	failedMessage := "scan failed"
	assertMigratedRootState(t, ctx, transaction, failedRootID, "ERROR", &failedCompletedAt, &failedMessage)
	assertMigratedRootState(t, ctx, transaction, staleRootID, "UNKNOWN", &previousScanAt, nil)
	assertMigratedRootState(t, ctx, transaction, pendingRootID, "SCANNING", &pendingPreviousScanAt, nil)

	var legacyColumnExists bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='library_roots' AND column_name='tag_writeback_enabled'
	)`, schema).Scan(&legacyColumnExists); err != nil {
		t.Fatal(err)
	}
	if !legacyColumnExists {
		t.Fatal("scan state repair unexpectedly dropped the legacy tag_writeback_enabled column")
	}

	var pendingRunID string
	if err := transaction.QueryRow(ctx, `SELECT id::text FROM library_scan_runs
		WHERE root_id=$1 AND status='PENDING'`, pendingRootID).Scan(&pendingRunID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET
		status='CANCELLED',completed_at=$2,updated_at=$2 WHERE id=$1`, pendingRunID, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertMigratedRootState(t, ctx, transaction, pendingRootID, "READY", &pendingPreviousScanAt, nil)

	staleRunID := uuid.NewString()
	if _, err := transaction.Exec(ctx, `INSERT INTO library_scan_runs(
		id,root_id,root_version,status,created_at,updated_at
	) VALUES($1,$2,1,'PENDING',$3,$3)`, staleRunID, readyRootID, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertMigratedRootState(t, ctx, transaction, readyRootID, "SCANNING", &readyCompletedAt, nil)
	if _, err := transaction.Exec(ctx, `UPDATE library_roots SET
		version=2,last_error='must be cleared' WHERE id=$1`, readyRootID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET
		status='COMPLETED',completed_at=$2,updated_at=$2 WHERE id=$1`, staleRunID, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertMigratedRootState(t, ctx, transaction, readyRootID, "UNKNOWN", &readyCompletedAt, nil)

	currentRunID := uuid.NewString()
	if _, err := transaction.Exec(ctx, `INSERT INTO library_scan_runs(
		id,root_id,root_version,status,created_at,updated_at
	) VALUES($1,$2,2,'PENDING',$3,$3)`, currentRunID, readyRootID, now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	currentCompletedAt := now.Add(8 * time.Second)
	if _, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET
		status='COMPLETED',completed_at=$2,updated_at=$2 WHERE id=$1`, currentRunID, currentCompletedAt); err != nil {
		t.Fatal(err)
	}
	assertMigratedRootState(t, ctx, transaction, readyRootID, "READY", &currentCompletedAt, nil)
}

func assertMigratedRootState(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	rootID, wantStatus string,
	wantLastScanAt *time.Time,
	wantLastError *string,
) {
	t.Helper()
	var status string
	var lastScanAt *time.Time
	var lastError *string
	if err := transaction.QueryRow(ctx, `SELECT status::text,last_scan_at,last_error
		FROM library_roots WHERE id=$1`, rootID).Scan(&status, &lastScanAt, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus {
		t.Fatalf("root %s status=%s, want %s", rootID, status, wantStatus)
	}
	if (lastScanAt == nil) != (wantLastScanAt == nil) ||
		(lastScanAt != nil && !lastScanAt.Equal(*wantLastScanAt)) {
		t.Fatalf("root %s lastScanAt=%v, want %v", rootID, lastScanAt, wantLastScanAt)
	}
	if (lastError == nil) != (wantLastError == nil) ||
		(lastError != nil && *lastError != *wantLastError) {
		t.Fatalf("root %s lastError=%v, want %v", rootID, lastError, wantLastError)
	}
}

func TestConfigurationManagedRootMigrationPostgresBehavior(t *testing.T) {
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

	schema := "xymusic_migration_0035_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schemaIdentifier := pgx.Identifier{schema}.Sanitize()
	if _, err := transaction.Exec(ctx, "CREATE SCHEMA "+schemaIdentifier); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := transaction.Exec(ctx, "SET LOCAL search_path TO "+schemaIdentifier); err != nil {
		t.Fatalf("isolate migration search path: %v", err)
	}
	if _, err := transaction.Exec(ctx, `CREATE TABLE library_roots (
		id uuid PRIMARY KEY,
		created_at timestamptz NOT NULL
	)`); err != nil {
		t.Fatalf("create ownership migration fixture: %v", err)
	}
	firstID, secondID := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := transaction.Exec(ctx, `INSERT INTO library_roots(id,created_at)
		VALUES($1,$3),($2,$3 + interval '1 second')`, firstID, secondID, now); err != nil {
		t.Fatalf("seed ownership migration roots: %v", err)
	}
	migrations, err := ReadMigrations(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) <= 35 || migrations[35].Tag != "0035_configuration_managed_library_roots" {
		t.Fatalf("configuration-managed root migration is unavailable: count=%d", len(migrations))
	}
	for index, statement := range migrations[35].SQL {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := transaction.Exec(ctx, statement); err != nil {
			t.Fatalf("execute ownership migration statement %d: %v", index, err)
		}
	}
	var firstManaged, secondManaged bool
	if err := transaction.QueryRow(ctx, `SELECT configuration_managed FROM library_roots WHERE id=$1`, firstID).Scan(&firstManaged); err != nil {
		t.Fatal(err)
	}
	if err := transaction.QueryRow(ctx, `SELECT configuration_managed FROM library_roots WHERE id=$1`, secondID).Scan(&secondManaged); err != nil {
		t.Fatal(err)
	}
	if !firstManaged || secondManaged {
		t.Fatalf("ownership migration classified roots incorrectly: first=%v second=%v", firstManaged, secondManaged)
	}
}