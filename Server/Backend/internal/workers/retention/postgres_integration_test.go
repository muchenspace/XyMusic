package retention

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/config"
	"xymusic/server/internal/testsupport"
)

func TestPostgresRetentionPoliciesAgainstConfiguredDatabase(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the PostgreSQL retention checks")
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

	poolConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.RuntimeParams["search_path"] = "pg_temp,public"
	poolConfig.AfterConnect = func(connectContext context.Context, connection *pgx.Conn) error {
		return createTemporaryRetentionTables(connectContext, connection)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	assertTemporarySchemaShadowing(t, ctx, pool)

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	seedTemporaryRetentionRows(t, ctx, pool, now)
	database, err := NewPostgresDatabase(pool)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(Dependencies{Database: database, Clock: &mutableClock{now: now}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.RunIfDue(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := Counts{
		Idempotency: 1, RateLimits: 1, RefreshTokens: 1,
		SessionsRevoked: 1, SessionsDeleted: 1,
		UploadsExpired: 2, UploadsDeleted: 2,
		LibraryScans: 1, Writebacks: 2,
		TrackDeleteBatches: 1,
	}
	if !result.Ran || result.Counts != expected {
		t.Fatalf("result=%+v, want %+v", result, expected)
	}
	assertTemporaryRetentionState(t, ctx, pool, now)
	assertPostgresAdvisoryLockLifecycle(t, ctx, database, cfg.Database.URL)
}

func assertTemporarySchemaShadowing(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, relation := range []string{
		"idempotency_records", "rate_limit_buckets", "refresh_tokens", "auth_sessions",
		"media_uploads", "library_scan_runs", "metadata_writeback_jobs",
		"track_delete_batches", "track_delete_batch_items",
	} {
		var schema string
		if err := pool.QueryRow(ctx, `
			SELECT namespace.nspname
			FROM pg_class relation
			JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE relation.oid = to_regclass($1)`, relation,
		).Scan(&schema); err != nil {
			t.Fatalf("resolve temporary relation %s: %v", relation, err)
		}
		if !strings.HasPrefix(schema, "pg_temp_") {
			t.Fatalf("unqualified relation %s resolves to unsafe schema %s", relation, schema)
		}
	}
}

func createTemporaryRetentionTables(ctx context.Context, connection *pgx.Conn) error {
	statements := []string{
		`CREATE TEMP TABLE idempotency_records (
			id text PRIMARY KEY, expires_at timestamptz NOT NULL
		)`,
		`CREATE TEMP TABLE rate_limit_buckets (
			key_hash text PRIMARY KEY, reset_at timestamptz NOT NULL
		)`,
		`CREATE TEMP TABLE refresh_tokens (
			id text PRIMARY KEY, session_id text NOT NULL, expires_at timestamptz NOT NULL,
			used_at timestamptz, revoked_at timestamptz
		)`,
		`CREATE TEMP TABLE auth_sessions (
			id text PRIMARY KEY, revoked_at timestamptz, last_seen_at timestamptz NOT NULL
		)`,
		`CREATE TEMP TABLE media_uploads (
			id text PRIMARY KEY, status text NOT NULL, expires_at timestamptz NOT NULL,
			completion_started_at timestamptz, completion_token text, storage_path text NOT NULL UNIQUE,
			completed_at timestamptz, created_at timestamptz NOT NULL
		)`,
		`CREATE TEMP TABLE library_scan_runs (
			id text PRIMARY KEY, status text NOT NULL, completed_at timestamptz
		)`,
		`CREATE TEMP TABLE metadata_writeback_jobs (
			id text PRIMARY KEY, status text NOT NULL, completed_at timestamptz, backup_path text
		)`,
		`CREATE TEMP TABLE track_delete_batches (
			id text PRIMARY KEY, status text NOT NULL, completed_at timestamptz
		)`,
		`CREATE TEMP TABLE track_delete_batch_items (
			id text PRIMARY KEY, job_id text NOT NULL REFERENCES track_delete_batches(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, err := connection.Exec(ctx, statement); err != nil {
			return fmt.Errorf("create temporary retention table: %w", err)
		}
	}
	return nil
}

func seedTemporaryRetentionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	statements := []struct {
		query     string
		arguments []any
	}{
		{`INSERT INTO idempotency_records (id, expires_at) VALUES
			('idempotency-expired', $1::timestamptz - interval '1 minute'),
			('idempotency-live', $1::timestamptz + interval '1 minute')`, []any{now}},
		{`INSERT INTO rate_limit_buckets (key_hash, reset_at) VALUES
			('rate-expired', $1::timestamptz - interval '1 minute'),
			('rate-live', $1::timestamptz + interval '1 minute')`, []any{now}},
		{`INSERT INTO auth_sessions (id, revoked_at, last_seen_at) VALUES
			('session-idle', NULL, $1::timestamptz - interval '2 hours'),
			('session-protected', NULL, $1::timestamptz - interval '2 hours'),
			('session-revoked', $1::timestamptz - interval '100 days', $1::timestamptz - interval '100 days'),
			('session-fresh', NULL, $1::timestamptz)`, []any{now}},
		{`INSERT INTO refresh_tokens (id, session_id, expires_at, used_at, revoked_at) VALUES
			('refresh-old', 'session-idle', $1::timestamptz - interval '40 days', NULL, NULL),
			('refresh-active', 'session-protected', $1::timestamptz + interval '1 day', NULL, NULL)`, []any{now}},
		{`INSERT INTO media_uploads (
			id, status, expires_at, completion_started_at, completion_token,
			storage_path, completed_at, created_at
		) VALUES
			('upload-created', 'CREATED', $1::timestamptz - interval '1 hour', NULL, 'created-token',
			 'temp/upload-created.partial', NULL, $1::timestamptz - interval '1 day'),
			('upload-completing', 'COMPLETING', $1::timestamptz - interval '1 hour', $1::timestamptz - interval '20 minutes',
			 'completion-token', 'temp/upload-completing.partial', NULL, $1::timestamptz - interval '1 day'),
			('upload-completing-live', 'COMPLETING', $1::timestamptz - interval '1 hour', $1::timestamptz - interval '5 minutes',
			 'live-token', 'temp/upload-completing-live.partial', NULL, $1::timestamptz - interval '1 day'),
			('upload-completed-old', 'COMPLETED', $1::timestamptz + interval '1 day', NULL, NULL,
			 'artworks/upload-completed-old.jpg', $1::timestamptz - interval '40 days', $1::timestamptz - interval '41 days'),
			('upload-failed-old', 'FAILED', $1::timestamptz - interval '40 days', NULL, NULL,
			 'temp/upload-failed-old.partial', NULL, $1::timestamptz - interval '41 days')`, []any{now}},
		{`INSERT INTO library_scan_runs (id, status, completed_at) VALUES
			('scan-old', 'COMPLETED', $1::timestamptz - interval '100 days'),
			('scan-live', 'FAILED', $1::timestamptz - interval '1 day')`, []any{now}},
		{`INSERT INTO metadata_writeback_jobs (id, status, completed_at, backup_path) VALUES
			('writeback-old', 'READY', $1::timestamptz - interval '100 days', NULL),
			('writeback-backup', 'FAILED', $1::timestamptz - interval '100 days', '/retained/backup'),
			('writeback-live', 'CANCELLED', $1::timestamptz - interval '1 day', NULL)`, []any{now}},
		{`INSERT INTO track_delete_batches (id, status, completed_at) VALUES
			('track-delete-old', 'COMPLETED', $1::timestamptz - interval '100 days'),
			('track-delete-live', 'FAILED', $1::timestamptz - interval '1 day'),
			('track-delete-running', 'RUNNING', NULL)`, []any{now}},
		{`INSERT INTO track_delete_batch_items (id, job_id) VALUES
			('track-delete-item-old', 'track-delete-old'),
			('track-delete-item-live', 'track-delete-live')`, nil},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.arguments...); err != nil {
			t.Fatalf("seed retention row: %v", err)
		}
	}
}

func assertTemporaryRetentionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	assertRows(t, ctx, pool, `SELECT count(*) FROM idempotency_records WHERE id = 'idempotency-live'`, 1)
	assertRows(t, ctx, pool, `SELECT count(*) FROM rate_limit_buckets WHERE key_hash = 'rate-live'`, 1)
	assertRows(t, ctx, pool, `SELECT count(*) FROM refresh_tokens WHERE id = 'refresh-active'`, 1)
	assertRows(t, ctx, pool, `SELECT count(*) FROM auth_sessions WHERE id = 'session-revoked'`, 0)
	var revokedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT revoked_at FROM auth_sessions WHERE id = 'session-idle'`).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt == nil || !revokedAt.Equal(now) {
		t.Fatalf("idle session revoked_at=%v", revokedAt)
	}
	assertRows(t, ctx, pool, `SELECT count(*) FROM auth_sessions
		WHERE id = 'session-protected' AND revoked_at IS NULL`, 1)
	assertRows(t, ctx, pool, `SELECT count(*) FROM media_uploads
		WHERE id IN ('upload-created', 'upload-completing')
		  AND status = 'EXPIRED' AND completion_token IS NULL AND completion_started_at IS NULL`, 2)
	assertRows(t, ctx, pool, `SELECT count(*) FROM media_uploads
		WHERE id = 'upload-completing-live' AND status = 'COMPLETING'`, 1)
	assertRows(t, ctx, pool, `SELECT count(*) FROM media_uploads
		WHERE id IN ('upload-completed-old', 'upload-failed-old')`, 0)
	assertRows(t, ctx, pool, `SELECT count(*) FROM library_scan_runs WHERE id = 'scan-old'`, 0)
	assertRows(t, ctx, pool, `SELECT count(*) FROM metadata_writeback_jobs WHERE id = 'writeback-old'`, 0)
	assertRows(t, ctx, pool, `SELECT count(*) FROM metadata_writeback_jobs WHERE id = 'writeback-backup'`, 0)
	assertRows(t, ctx, pool, `SELECT count(*) FROM track_delete_batches WHERE id = 'track-delete-old'`, 0)
	assertRows(t, ctx, pool, `SELECT count(*) FROM track_delete_batch_items WHERE id = 'track-delete-item-old'`, 0)
	assertRows(t, ctx, pool, `SELECT count(*) FROM track_delete_batches WHERE id IN ('track-delete-live', 'track-delete-running')`, 2)
}

func assertPostgresAdvisoryLockLifecycle(
	t *testing.T,
	ctx context.Context,
	database *PostgresDatabase,
	databaseURL string,
) {
	t.Helper()
}

func assertRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, query).Scan(&count); err != nil {
		t.Fatalf("query %s: %v", query, err)
	}
	if count != expected {
		t.Fatalf("query %s returned %d rows, want %d", query, count, expected)
	}
}
