package adminjobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/testsupport"
)

func TestRepositoryRunsJobProjectionAndMutationsInConfiguredDatabase(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production administrator job repository checks")
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
		t.Fatal(err)
	}
	defer pool.Close()
	transaction, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))
	repository := &Repository{database: transaction}

	suffix := uuid.NewString()
	short := suffix[:8]
	passwordHash, err := platformsecurity.HashPassword("adminjobs-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	var actorID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO users (username, normalized_username, password_hash, role)
		VALUES ($1, $1, $2, 'ADMIN') RETURNING id`, "it_jobs_"+short, passwordHash,
	).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var rootID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO library_roots (name, path, normalized_path, mode, enabled)
		VALUES ($1, $2, $2, 'READ_ONLY', true) RETURNING id`,
		"Integration "+short, "D:/xymusic-integration/"+suffix,
	).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	var trackID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO tracks (title, normalized_title, duration_ms, status)
		VALUES ($1, $2, 1000, 'ERROR') RETURNING id`, "Integration Track "+short, "integration track "+short,
	).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO local_music_sources (
			root_id, track_id, source_path, normalized_source_path, checksum_sha256,
			size_bytes, modified_at, status
		) VALUES ($1, $2, $3, $3, repeat('a', 64), 100, now(), 'READY') RETURNING id`,
		rootID, trackID, "D:/xymusic-integration/"+suffix+".flac",
	).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	var tagJobID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO metadata_writeback_jobs (
			track_id, root_id, source_id, target_path, original_checksum_sha256,
			requested_by, status, attempts, metadata_snapshot, metadata_version,
			expected_source_checksum, payload, root_path_snapshot, source_path_snapshot,
			last_error, last_error_code
		) VALUES ($1, $2, $3, $4, repeat('a', 64), $5, 'FAILED', 2,
			'{}'::jsonb, 1, repeat('a', 64), '{}'::jsonb, $6, $7,
			'integration writeback failure', 'DEPENDENCY_UNAVAILABLE') RETURNING id`,
		trackID, rootID, sourceID, "D:/xymusic-integration/"+suffix+".flac", actorID,
		"D:/xymusic-integration/"+suffix, "D:/xymusic-integration/"+suffix+".flac",
	).Scan(&tagJobID); err != nil {
		t.Fatal(err)
	}
	var scanJobID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO library_scan_runs (
			root_id, triggered_by, status, discovered_files, processed_files,
			failed_files, started_at, completed_at, last_error
		) VALUES ($1, $2, 'FAILED', 10, 5, 1, now() - interval '1 minute', now(),
			'integration scan failure') RETURNING id`, rootID, actorID,
	).Scan(&scanJobID); err != nil {
		t.Fatal(err)
	}

	items, total, err := repository.ListJobs(ctx, ListQuery{
		Search: "Integration", Sort: SortCreatedAt, Order: SortDescending, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total < 2 || !containsJob(items, tagJobID, JobTypeTagWrite, JobStatusFailed) ||
		!containsJob(items, scanJobID, JobTypeSourceScan, JobStatusFailed) {
		t.Fatalf("projection total/items=%d/%+v", total, items)
	}
	for _, sort := range []SortField{SortCreatedAt, SortUpdatedAt, SortStatus, SortType, SortTitle} {
		if _, _, err := repository.ListJobs(ctx, ListQuery{
			Search: "Integration", Sort: sort, Order: SortAscending, Limit: 2,
		}); err != nil {
			t.Fatalf("list jobs sort %s: %v", sort, err)
		}
	}
	if _, _, err := repository.ListJobs(ctx, ListQuery{
		Search: `%_\`, Status: JobStatusFailed, Type: JobTypeTagWrite,
		Sort: SortCreatedAt, Order: SortDescending, Limit: 1,
	}); err != nil {
		t.Fatalf("list jobs escaped search: %v", err)
	}
	if _, err := repository.FindJob(ctx, tagJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EventState(ctx); err != nil {
		t.Fatal(err)
	}

	if err := repository.RetryMediaOrScan(ctx, scanJobID); err != nil {
		t.Fatal(err)
	}
	assertScanState(t, ctx, transaction, scanJobID, "PENDING", false, false)
	if err := repository.CancelMediaOrScan(ctx, scanJobID); err != nil {
		t.Fatal(err)
	}
	assertScanState(t, ctx, transaction, scanJobID, "CANCELLED", true, true)
}

func containsJob(items []JobRecord, id string, jobType JobType, status JobStatus) bool {
	for _, item := range items {
		if item.ID == id && item.Type == jobType && item.Status == status {
			return true
		}
	}
	return false
}

func assertScanState(
	t *testing.T,
	ctx context.Context,
	database integrationQueryer,
	jobID, expectedStatus string,
	expectedCancel, expectedCompleted bool,
) {
	t.Helper()
	var status string
	var cancelRequested bool
	var discovered, processed, failed int
	var startedAt, completedAt *time.Time
	if err := database.QueryRow(ctx, `
		SELECT status::text, cancel_requested, discovered_files, processed_files,
			failed_files, started_at, completed_at
		FROM library_scan_runs WHERE id = $1`, jobID,
	).Scan(&status, &cancelRequested, &discovered, &processed, &failed, &startedAt, &completedAt); err != nil {
		t.Fatal(err)
	}
	if status != expectedStatus || cancelRequested != expectedCancel || discovered != 0 || processed != 0 || failed != 0 ||
		startedAt != nil || (completedAt != nil) != expectedCompleted {
		t.Fatalf("scan state=%s/%t/%d/%d/%d/%v/%v", status, cancelRequested, discovered, processed, failed, startedAt, completedAt)
	}
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

type integrationQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
