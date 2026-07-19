package adminjobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	var assetID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO media_assets (
			uploader_id, object_key, kind, mime_type, size_bytes, checksum_sha256, status
		) VALUES ($1, $2, 'AUDIO_SOURCE', 'audio/flac', 100, $3, 'READY') RETURNING id`,
		actorID, "integration/jobs/"+suffix+".flac", strings.Repeat("a", 64),
	).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	var mediaJobID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO media_jobs (
			type, source_asset_id, track_id, status, attempts, idempotency_key,
			payload, last_error, last_error_code
		) VALUES ('INGEST_TRACK', $1, $2, 'FAILED', 2, $3, '{}'::jsonb,
			'integration media failure', 'DEPENDENCY_UNAVAILABLE') RETURNING id`,
		assetID, trackID, "integration-job-"+suffix,
	).Scan(&mediaJobID); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := transaction.QueryRow(ctx, `
		INSERT INTO local_music_sources (
			root_id, source_path, normalized_source_path, checksum_sha256, size_bytes,
			modified_at, track_id, source_asset_id, media_job_id, status, last_error
		) VALUES ($1, $2, $2, $3, 100, now(), $4, $5, $6, 'FAILED', 'integration failure')
		RETURNING id`,
		rootID, "track-"+suffix+".flac", strings.Repeat("b", 64), trackID, assetID, mediaJobID,
	).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO local_music_source_tracks (source_id, track_id, media_job_id)
		VALUES ($1, $2, $3)`, sourceID, trackID, mediaJobID); err != nil {
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
	if total < 2 || !containsJob(items, mediaJobID, JobTypeMediaProcess, JobStatusFailed) ||
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
		Search: `%_\`, Status: JobStatusFailed, Type: JobTypeMediaProcess,
		Sort: SortCreatedAt, Order: SortDescending, Limit: 1,
	}); err != nil {
		t.Fatalf("list jobs escaped search: %v", err)
	}
	if _, err := repository.FindJob(ctx, mediaJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EventState(ctx); err != nil {
		t.Fatal(err)
	}

	retryReason := "integration media retry"
	if err := repository.RetryMediaOrScan(ctx, actorID, "trace-integration", mediaJobID, &retryReason); err != nil {
		t.Fatal(err)
	}
	assertMediaState(t, ctx, transaction, mediaJobID, "PENDING", 0, false, 2)
	assertTrackGeneration(t, ctx, transaction, trackID, 1, 2)
	assertSourceState(t, ctx, transaction, sourceID, "PROCESSING", nil)

	if err := repository.CancelMediaOrScan(ctx, actorID, "trace-integration", mediaJobID, nil); err != nil {
		t.Fatal(err)
	}
	assertMediaState(t, ctx, transaction, mediaJobID, "CANCELLED", 0, true, 3)
	cancelled := "Cancelled by an administrator"
	assertSourceState(t, ctx, transaction, sourceID, "FAILED", &cancelled)

	if err := repository.RetryMediaOrScan(ctx, actorID, "trace-integration", scanJobID, nil); err != nil {
		t.Fatal(err)
	}
	assertScanState(t, ctx, transaction, scanJobID, "PENDING", false, false)
	if err := repository.CancelMediaOrScan(ctx, actorID, "trace-integration", scanJobID, nil); err != nil {
		t.Fatal(err)
	}
	assertScanState(t, ctx, transaction, scanJobID, "CANCELLED", true, true)

	var auditCount int
	if err := transaction.QueryRow(ctx, `
		SELECT count(*)::int FROM audit_logs
		WHERE actor_id = $1 AND target_id IN ($2, $3)
		  AND action IN ('admin.job.retry', 'admin.job.cancel')`,
		actorID, mediaJobID, scanJobID,
	).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 4 {
		t.Fatalf("audit count=%d", auditCount)
	}
}

func containsJob(items []JobRecord, id string, jobType JobType, status JobStatus) bool {
	for _, item := range items {
		if item.ID == id && item.Type == jobType && item.Status == status {
			return true
		}
	}
	return false
}

func assertMediaState(
	t *testing.T,
	ctx context.Context,
	database integrationQueryer,
	jobID, expectedStatus string,
	expectedAttempts int,
	expectedCancel bool,
	expectedVersion int,
) {
	t.Helper()
	var status string
	var attempts, version int
	var cancelRequested bool
	var lastError, lastErrorCode *string
	if err := database.QueryRow(ctx, `
		SELECT status::text, attempts, cancel_requested, version, last_error, last_error_code
		FROM media_jobs WHERE id = $1`, jobID,
	).Scan(&status, &attempts, &cancelRequested, &version, &lastError, &lastErrorCode); err != nil {
		t.Fatal(err)
	}
	if status != expectedStatus || attempts != expectedAttempts || cancelRequested != expectedCancel ||
		version != expectedVersion || lastError != nil || lastErrorCode != nil {
		t.Fatalf("media state=%s/%d/%t/%d/%v/%v", status, attempts, cancelRequested, version, lastError, lastErrorCode)
	}
}

func assertTrackGeneration(
	t *testing.T,
	ctx context.Context,
	database integrationQueryer,
	trackID string,
	expectedGeneration, expectedVersion int,
) {
	t.Helper()
	var generation, version int
	if err := database.QueryRow(ctx, `
		SELECT media_generation, version FROM tracks WHERE id = $1`, trackID,
	).Scan(&generation, &version); err != nil {
		t.Fatal(err)
	}
	if generation != expectedGeneration || version != expectedVersion {
		t.Fatalf("track generation/version=%d/%d", generation, version)
	}
}

func assertSourceState(
	t *testing.T,
	ctx context.Context,
	database integrationQueryer,
	sourceID, expectedStatus string,
	expectedError *string,
) {
	t.Helper()
	var status string
	var lastError *string
	if err := database.QueryRow(ctx, `
		SELECT status, last_error FROM local_music_sources WHERE id = $1`, sourceID,
	).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != expectedStatus || !equalOptionalString(lastError, expectedError) {
		t.Fatalf("source status/error=%s/%v", status, lastError)
	}
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
