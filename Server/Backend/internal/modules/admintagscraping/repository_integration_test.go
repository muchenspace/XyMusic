package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/adminmedia"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/testsupport"
)

func TestRepositoryReadsConfiguredProductionScrapingState(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production tag scraping queries")
	}
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
	t.Cleanup(pool.Close)
	repository := NewRepository(pool.Pool)
	for _, query := range []string{
		`EXPLAIN SELECT id FROM tag_scraping_jobs WHERE status IN ('PENDING','RUNNING') ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`,
		`EXPLAIN SELECT id FROM tag_scraping_job_items WHERE job_id = '00000000-0000-0000-0000-000000000000'
		 AND status = 'PENDING' ORDER BY position FOR UPDATE SKIP LOCKED LIMIT 1`,
	} {
		if _, err := pool.Pool.Exec(ctx, query); err != nil {
			t.Fatalf("batch claim SQL: %v", err)
		}
	}

	var trackID string
	err = pool.Pool.QueryRow(ctx, "SELECT track_id FROM track_metadata ORDER BY updated_at DESC LIMIT 1").Scan(&trackID)
	if err == nil {
		metadata, loadErr := repository.loadMetadata(ctx, trackID)
		if loadErr != nil {
			t.Fatalf("loadMetadata: %v", loadErr)
		}
		if metadata.TrackID != trackID || metadata.Version < 1 {
			t.Fatalf("metadata=%#v", metadata)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("select track metadata: %v", err)
	}

	err = pool.Pool.QueryRow(ctx, `
		SELECT mapping.track_id FROM local_music_source_tracks mapping
		JOIN local_music_sources source ON source.id = mapping.source_id
		ORDER BY source.updated_at DESC LIMIT 1`).Scan(&trackID)
	if err == nil {
		if _, lookupErr := repository.FingerprintSource(ctx, trackID); lookupErr != nil {
			t.Fatalf("FingerprintSource: %v", lookupErr)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("select local source mapping: %v", err)
	}

	var jobID string
	err = pool.Pool.QueryRow(ctx, "SELECT id FROM tag_scraping_jobs ORDER BY created_at DESC LIMIT 1").Scan(&jobID)
	if err == nil {
		if _, _, lookupErr := repository.Batch(ctx, jobID, nil); lookupErr != nil {
			t.Fatalf("Batch: %v", lookupErr)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("select tag scraping batch: %v", err)
	}
}

func TestProductionArchivedTrackScrapingGuards(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run archived track scraping guards")
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()
	actorID := uuid.NewString()
	trackID := uuid.NewString()
	rootID := uuid.NewString()
	sourceID := uuid.NewString()
	rootPath := filepath.Join(os.TempDir(), "tag-scraping-archived-"+suffix)
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM audit_logs WHERE actor_id = $1", actorID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM local_music_source_tracks WHERE track_id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM local_music_sources WHERE id = $1", sourceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM track_metadata WHERE track_id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM tracks WHERE id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM library_roots WHERE id = $1", rootID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM users WHERE id = $1", actorID)
	}
	t.Cleanup(cleanup)
	cleanup()

	username := "it_tag_archived_" + suffix[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users(
		id,username,normalized_username,password_hash,role,status
	) VALUES($1,$2,$2,$3,'ADMIN','ACTIVE')`, actorID, username, integrationPasswordHash(t, suffix)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks(
		id,title,normalized_title,status
	) VALUES($1,$2,$3,'ARCHIVED')`, trackID, "Archived Track "+suffix[:8], "archived track "+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots(
		id,name,path,normalized_path,mode,enabled,scan_on_startup,status
	) VALUES($1,$2,$3,$3,'READ_WRITE',true,false,'READY')`, rootID, "Archived Root "+suffix[:8], rootPath); err != nil {
		t.Fatal(err)
	}
	checksum := strings.Repeat("a", 56) + suffix[:8]
	sourcePath := "archived-" + suffix[:8] + ".flac"
	if _, err := pool.Exec(ctx, `INSERT INTO local_music_sources(
		id,root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,
		modified_at,track_id,status
	) VALUES($1,$2,$3,$3,$4,100,now(),$5,'READY')`, sourceID, rootID, sourcePath, checksum, trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO local_music_source_tracks(
		source_id,track_id,segment_index,start_ms
	) VALUES($1,$2,0,0)`, sourceID, trackID); err != nil {
		t.Fatal(err)
	}
	rawTags, err := json.Marshal(MetadataSnapshot{
		Title: "Archived Track", Credits: []MetadataCredit{}, AlbumArtists: []string{}, Genres: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO track_metadata(track_id,source_id,raw_tags,overrides,version)
		VALUES($1,$2,$3::jsonb,'{}'::jsonb,1)`, trackID, sourceID, rawTags); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	metadata, err := repository.Metadata(ctx, trackID)
	if err != nil || metadata.TrackStatus != archivedTrackStatus {
		t.Fatalf("metadata=%+v error=%v", metadata, err)
	}
	var baselineRevisions int
	if err := pool.QueryRow(ctx, "SELECT count(*)::int FROM track_metadata_revisions WHERE track_id=$1", trackID).Scan(&baselineRevisions); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FingerprintSource(ctx, trackID); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) || !isArchivedTrackError(err) {
		t.Fatalf("fingerprint error = %#v", err)
	}
	if _, err := repository.UpdateMetadata(
		ctx, actorID, "integration:archived-update", trackID, 1,
		MetadataPatch{"title": "Must not change"}, "archived track guard",
	); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) || !isArchivedTrackError(err) {
		t.Fatalf("metadata update error = %#v", err)
	}
	var version, revisions, audits int
	var overrides string
	if err := pool.QueryRow(ctx, `SELECT version,overrides::text,
		(SELECT count(*)::int FROM track_metadata_revisions WHERE track_id=$1),
		(SELECT count(*)::int FROM audit_logs WHERE actor_id=$2 AND target_id=$1)
		FROM track_metadata WHERE track_id=$1`, trackID, actorID).Scan(
		&version, &overrides, &revisions, &audits,
	); err != nil {
		t.Fatal(err)
	}
	if version != 1 || overrides != "{}" || revisions != baselineRevisions || audits != 0 {
		t.Fatalf(
			"version/overrides/revisions(want %d)/audits = %d/%s/%d/%d",
			baselineRevisions, version, overrides, revisions, audits,
		)
	}
}

func TestProductionBatchCancellationAcrossServiceInstances(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production tag scraping cancellation")
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()
	actorID := uuid.NewString()
	trackID := uuid.NewString()
	var jobID string
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if jobID != "" {
			_, _ = pool.Exec(cleanupContext, "DELETE FROM tag_scraping_jobs WHERE id = $1", jobID)
		}
		_, _ = pool.Exec(cleanupContext, "DELETE FROM metadata_writeback_jobs WHERE track_id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM tracks WHERE id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM users WHERE id = $1", actorID)
	}
	t.Cleanup(cleanup)
	cleanup()

	username := "it_tag_cancel_" + suffix[:8]
	passwordHash, err := platformsecurity.HashPassword("tag-cancel-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(
		id,username,normalized_username,password_hash,role,status
	) VALUES($1,$2,$2,$3,'ADMIN','ACTIVE')`, actorID, username, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks(
		id,title,normalized_title,status
	) VALUES($1,'Original title',$2,'READY')`, trackID, "original title "+suffix); err != nil {
		t.Fatal(err)
	}
	rawTags, err := json.Marshal(MetadataSnapshot{
		Title: "Original title", Credits: []MetadataCredit{}, AlbumArtists: []string{}, Genres: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO track_metadata(track_id,raw_tags,overrides,version)
		VALUES($1,$2::jsonb,'{}'::jsonb,1)`, trackID, rawTags); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	processor := newBlockingBatchProcessor(false)
	api, err := NewBatchService(BatchServiceDependencies{Store: repository, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	workerID := "tag-cancel-integration-" + suffix
	worker, err := NewBatchService(BatchServiceDependencies{
		Store: repository, Processor: processor, WorkerID: workerID,
		Lease: 2 * time.Minute, Heartbeat: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := api.Create(ctx, actorID, CreateBatchInput{
		Items: []BatchItemInput{{TrackID: trackID, ExpectedVersion: 1}},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true, Overwrite: true}, WriteBack: true,
			Reason: "cross-process cancellation integration",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID = created.ID
	if _, err := pool.Exec(ctx, "UPDATE tag_scraping_jobs SET created_at = '2000-01-01' WHERE id = $1", jobID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claim, err := repository.ClaimBatchItem(ctx, workerID, now, 2*time.Minute)
	if err != nil || claim.Item == nil || claim.Item.Job.ID != jobID {
		t.Fatalf("claim=%+v error=%v", claim, err)
	}
	processed := make(chan struct{})
	go func() {
		worker.processItem(context.Background(), *claim.Item)
		close(processed)
	}()
	waitForSignal(t, processor.started, "production batch search did not start")
	if _, err := api.Cancel(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	fencedContext := withBatchMutationFence(ctx, &BatchMutationFence{
		JobID:     jobID,
		ItemID:    claim.Item.Item.ID,
		AttemptID: claim.Item.AttemptID,
		WorkerID:  workerID,
	})
	if _, err := repository.UpdateMetadata(
		fencedContext, actorID, "integration:cancelled-metadata", trackID, 1,
		MetadataPatch{"title": "Must not be applied"}, "cancelled batch",
	); !errors.Is(err, errBatchCancellationRequested) {
		t.Fatalf("cancelled metadata update error = %v", err)
	}
	if _, err := repository.EnqueueWriteback(
		fencedContext, actorID, "integration:cancelled-writeback", trackID, 1, "cancelled batch",
	); !errors.Is(err, errBatchCancellationRequested) {
		t.Fatalf("cancelled writeback enqueue error = %v", err)
	}
	control, err := repository.RenewBatchItemLease(
		ctx, jobID, claim.Item.Item.ID, claim.Item.AttemptID, workerID, now.Add(2*time.Minute),
	)
	if err != nil || !control.Owned || !control.CancelRequested {
		t.Fatalf("lease control=%+v error=%v", control, err)
	}
	close(processor.release)
	waitForSignal(t, processed, "production batch item did not stop after cancellation")
	job, items, err := repository.Batch(ctx, jobID, nil)
	if err != nil || len(items) != 1 || items[0].Status != ItemSkipped || processor.applyCalls.Load() != 0 {
		t.Fatalf("job/items/apply=%+v/%+v/%d error=%v", job, items, processor.applyCalls.Load(), err)
	}
	finished, err := repository.FinishBatch(ctx, jobID, time.Now().UTC())
	if err != nil || !finished {
		t.Fatalf("finish=%v error=%v", finished, err)
	}
	job, _, err = repository.Batch(ctx, jobID, nil)
	if err != nil || job.Status != JobCancelled || job.Succeeded != 0 || job.Failed != 0 {
		t.Fatalf("cancelled job=%+v error=%v", job, err)
	}
	var version, writebacks int
	var overrides string
	if err := pool.QueryRow(ctx, `SELECT version,overrides::text,
		(SELECT count(*)::int FROM metadata_writeback_jobs WHERE track_id=$1)
		FROM track_metadata WHERE track_id=$1`, trackID).Scan(&version, &overrides, &writebacks); err != nil {
		t.Fatal(err)
	}
	if version != 1 || overrides != "{}" || writebacks != 0 {
		t.Fatalf("metadata version/overrides/writebacks=%d/%s/%d", version, overrides, writebacks)
	}
}

func TestProductionBatchAttemptFenceAndCancelledCompletion(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production tag scraping attempt fencing")
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()
	actorID := uuid.NewString()
	artistID := uuid.NewString()
	trackID := uuid.NewString()
	jobIDs := make([]string, 0, 2)
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, jobID := range jobIDs {
			_, _ = pool.Exec(cleanupContext, "DELETE FROM tag_scraping_jobs WHERE id = $1", jobID)
		}
		_, _ = pool.Exec(cleanupContext, "DELETE FROM audit_logs WHERE actor_id = $1", actorID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM tracks WHERE id = $1", trackID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM artists WHERE id = $1", artistID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM users WHERE id = $1", actorID)
	}
	t.Cleanup(cleanup)
	cleanup()

	username := "it_tag_fence_" + suffix[:8]
	artistName := "Attempt Fence Artist " + suffix[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users(
		id,username,normalized_username,password_hash,role,status
	) VALUES($1,$2,$2,$3,'ADMIN','ACTIVE')`, actorID, username, integrationPasswordHash(t, suffix)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO artists(id,name,normalized_name)
		VALUES($1,$2,$3)`, artistID, artistName, normalizeLookup(artistName)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks(
		id,title,normalized_title,status
	) VALUES($1,$2,$3,'READY')`, trackID, "Attempt Fence Original "+suffix[:8], "attempt fence original "+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO track_artists(track_id,artist_id,role,sort_order)
		VALUES($1,$2,'PRIMARY',0)`, trackID, artistID); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	createBatch := func(expectedVersion int) string {
		t.Helper()
		jobID, createErr := repository.CreateBatch(ctx, actorID, CreateBatchInput{
			Items: []BatchItemInput{{TrackID: trackID, ExpectedVersion: expectedVersion}},
			Options: BatchOptions{
				Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
				Fields: ApplyFields{Title: true, Overwrite: true}, Reason: "attempt fence integration",
			},
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		jobIDs = append(jobIDs, jobID)
		if _, createErr = pool.Exec(ctx, "UPDATE tag_scraping_jobs SET created_at = '1900-01-01' WHERE id = $1", jobID); createErr != nil {
			t.Fatal(createErr)
		}
		return jobID
	}

	jobID := createBatch(1)
	now := time.Now().UTC()
	claimA, err := repository.ClaimBatchItem(ctx, "attempt-worker-a", now, time.Minute)
	if err != nil || claimA.Item == nil || claimA.Item.Job.ID != jobID {
		t.Fatalf("claim A=%+v error=%v", claimA, err)
	}
	claimB, err := repository.ClaimBatchItem(ctx, "attempt-worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil || claimB.Item == nil || claimB.Item.Job.ID != jobID || claimB.Item.AttemptID == claimA.Item.AttemptID {
		t.Fatalf("claim B=%+v error=%v", claimB, err)
	}
	fenceAContext := withBatchMutationFence(ctx, &BatchMutationFence{
		JobID: jobID, ItemID: claimA.Item.Item.ID, AttemptID: claimA.Item.AttemptID, WorkerID: "attempt-worker-a",
	})
	if _, err := repository.Metadata(fenceAContext, trackID); !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("stale Metadata error = %v", err)
	}
	if _, err := repository.UpdateMetadata(
		fenceAContext, actorID, "integration:stale-update", trackID, 1,
		MetadataPatch{"title": "Stale attempt must not apply"}, "stale attempt",
	); !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("stale UpdateMetadata error = %v", err)
	}
	completed, err := repository.CompleteBatchItem(
		ctx, jobID, claimA.Item.Item.ID, claimA.Item.AttemptID, "attempt-worker-a",
		ItemSucceeded, nil, "stale completion", now.Add(2*time.Minute),
	)
	if completed || !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("stale CompleteBatchItem=%v error=%v", completed, err)
	}
	var metadataRows int
	if err := pool.QueryRow(ctx, "SELECT count(*)::int FROM track_metadata WHERE track_id = $1", trackID).Scan(&metadataRows); err != nil {
		t.Fatal(err)
	}
	job, items, err := repository.Batch(ctx, jobID, nil)
	if err != nil || metadataRows != 0 || job.Processed != 0 || job.Succeeded != 0 || job.Failed != 0 ||
		len(items) != 1 || items[0].Status != ItemRunning || items[0].AttemptID == nil || *items[0].AttemptID != claimB.Item.AttemptID {
		t.Fatalf("state after stale attempt: metadata=%d job=%+v items=%+v error=%v", metadataRows, job, items, err)
	}

	fenceBContext := withBatchMutationFence(ctx, &BatchMutationFence{
		JobID: jobID, ItemID: claimB.Item.Item.ID, AttemptID: claimB.Item.AttemptID, WorkerID: "attempt-worker-b",
	})
	baseline, err := repository.Metadata(fenceBContext, trackID)
	if err != nil || baseline.Version != 1 {
		t.Fatalf("current attempt baseline=%+v error=%v", baseline, err)
	}
	updatedTitle := "Attempt Fence Updated " + suffix[:8]
	updated, err := repository.UpdateMetadata(
		fenceBContext, actorID, "integration:current-update", trackID, baseline.Version,
		MetadataPatch{"title": updatedTitle}, "current attempt",
	)
	if err != nil || updated.Version != 2 || updated.Effective.Title != updatedTitle {
		t.Fatalf("current attempt update=%+v error=%v", updated, err)
	}
	completed, err = repository.CompleteBatchItem(
		ctx, jobID, claimB.Item.Item.ID, claimB.Item.AttemptID, "attempt-worker-b",
		ItemSucceeded, nil, "completed by current attempt", now.Add(2*time.Minute),
	)
	if err != nil || !completed {
		t.Fatalf("current CompleteBatchItem=%v error=%v", completed, err)
	}
	if finished, err := repository.FinishBatch(ctx, jobID, now.Add(2*time.Minute)); err != nil || !finished {
		t.Fatalf("FinishBatch=%v error=%v", finished, err)
	}
	job, items, err = repository.Batch(ctx, jobID, nil)
	if err != nil || job.Status != JobCompleted || job.Processed != 1 || job.Succeeded != 1 || job.Failed != 0 ||
		len(items) != 1 || items[0].Status != ItemSucceeded {
		t.Fatalf("completed current attempt job=%+v items=%+v error=%v", job, items, err)
	}

	cancelledJobID := createBatch(updated.Version)
	cancelledClaim, err := repository.ClaimBatchItem(ctx, "cancelled-completion-worker", now.Add(3*time.Minute), time.Minute)
	if err != nil || cancelledClaim.Item == nil || cancelledClaim.Item.Job.ID != cancelledJobID {
		t.Fatalf("cancelled completion claim=%+v error=%v", cancelledClaim, err)
	}
	if err := repository.RequestBatchCancel(ctx, cancelledJobID); err != nil {
		t.Fatal(err)
	}
	candidate := &Candidate{ID: "must-be-discarded", Name: "Must Be Discarded", Source: SourceQMusic}
	completed, err = repository.CompleteBatchItem(
		ctx, cancelledJobID, cancelledClaim.Item.Item.ID, cancelledClaim.Item.AttemptID, "cancelled-completion-worker",
		ItemSucceeded, candidate, "must be replaced", now.Add(3*time.Minute),
	)
	if err != nil || !completed {
		t.Fatalf("cancelled CompleteBatchItem=%v error=%v", completed, err)
	}
	cancelledJob, cancelledItems, err := repository.Batch(ctx, cancelledJobID, nil)
	if err != nil || cancelledJob.Processed != 1 || cancelledJob.Succeeded != 0 || cancelledJob.Failed != 0 ||
		len(cancelledItems) != 1 || cancelledItems[0].Status != ItemSkipped || cancelledItems[0].Candidate != nil ||
		cancelledItems[0].Source != nil || pointerValue(cancelledItems[0].Message) != "The batch was cancelled" {
		t.Fatalf("cancelled completion job=%+v items=%+v error=%v", cancelledJob, cancelledItems, err)
	}
	if finished, err := repository.FinishBatch(ctx, cancelledJobID, now.Add(3*time.Minute)); err != nil || !finished {
		t.Fatalf("finish cancelled batch=%v error=%v", finished, err)
	}
	cancelledJob, _, err = repository.Batch(ctx, cancelledJobID, nil)
	if err != nil || cancelledJob.Status != JobCancelled || cancelledJob.Succeeded != 0 || cancelledJob.Failed != 0 {
		t.Fatalf("finished cancelled job=%+v error=%v", cancelledJob, err)
	}
}

func TestProductionStaleBatchAttemptCannotCommitMutations(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production stale-attempt fencing")
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()
	actorID := uuid.NewString()
	albumID := uuid.NewString()
	trackID := uuid.NewString()
	uploadID := uuid.NewString()
	assetID := uuid.NewString()
	raceUploadID := uuid.NewString()
	raceAssetID := uuid.NewString()
	objectKeys := []string{
		"uploads/" + actorID + "/" + uploadID,
		"media/artwork/album_artwork/" + albumID + "/" + uploadID + ".jpg",
		"uploads/" + actorID + "/" + raceUploadID,
		"media/artwork/album_artwork/" + albumID + "/" + raceUploadID + ".jpg",
	}
	var jobID, laterJobID string
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `delete from object_cleanup_jobs where object_key = any($1::varchar[])`, objectKeys)
		_, _ = pool.Exec(cleanupContext, `delete from media_uploads where id = any($1::uuid[])`, []string{uploadID, raceUploadID})
		_, _ = pool.Exec(cleanupContext, `delete from media_assets where id = any($1::uuid[])`, []string{assetID, raceAssetID})
		for _, cleanupJobID := range []string{jobID, laterJobID} {
			if cleanupJobID != "" {
				_, _ = pool.Exec(cleanupContext, `delete from tag_scraping_jobs where id = $1`, cleanupJobID)
			}
		}
		_, _ = pool.Exec(cleanupContext, `delete from metadata_writeback_jobs where track_id = $1`, trackID)
		_, _ = pool.Exec(cleanupContext, `delete from track_metadata where track_id = $1`, trackID)
		_, _ = pool.Exec(cleanupContext, `delete from tracks where id = $1`, trackID)
		_, _ = pool.Exec(cleanupContext, `delete from albums where id = $1`, albumID)
		_, _ = pool.Exec(cleanupContext, `delete from users where id = $1`, actorID)
	}
	t.Cleanup(cleanup)
	cleanup()

	username := "it_tag_stale_" + suffix[:8]
	if _, err := pool.Exec(ctx, `insert into users(
		id,username,normalized_username,password_hash,role,status
	) values($1,$2,$2,$3,'ADMIN','ACTIVE')`, actorID, username, integrationPasswordHash(t, suffix)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into albums(id,title,normalized_title)
		values($1,'Stale attempt album',$2)`, albumID, "stale attempt album "+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into tracks(
		id,album_id,title,normalized_title,status
	) values($1,$2,'Original title',$3,'READY')`, trackID, albumID, "original title "+suffix); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	batchInput := CreateBatchInput{
		Items: []BatchItemInput{{TrackID: trackID, ExpectedVersion: 1}},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields:    ApplyFields{Title: true, Cover: true, Overwrite: true},
			WriteBack: true, Reason: "stale attempt integration",
		},
	}
	jobID, err = repository.CreateBatch(ctx, actorID, batchInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update tag_scraping_jobs set created_at = '1999-01-01' where id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	attemptA, err := repository.ClaimBatchItem(ctx, "worker-a", time.Now().UTC(), time.Minute)
	if err != nil || attemptA.Item == nil || attemptA.Item.Job.ID != jobID ||
		attemptA.Item.Item.LockedUntil == nil {
		t.Fatalf("attempt A claim = %+v error=%v", attemptA, err)
	}
	laterJobID, err = repository.CreateBatch(ctx, actorID, batchInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update tag_scraping_jobs set created_at = '2000-01-01' where id = $1`, laterJobID); err != nil {
		t.Fatal(err)
	}
	leaseStillActiveNow := attemptA.Item.Item.LockedUntil.Add(-time.Second)
	laterClaim, err := repository.ClaimBatchItem(ctx, "later-worker", leaseStillActiveNow, time.Minute)
	if err != nil || laterClaim.Item == nil || laterClaim.Item.Job.ID != laterJobID {
		t.Fatalf("claim behind active lease = %+v error=%v", laterClaim, err)
	}
	if finished, err := repository.FinishBatch(ctx, jobID, leaseStillActiveNow); err != nil || finished {
		t.Fatalf("active leased batch finished=%v error=%v", finished, err)
	}
	leasedJob, leasedItems, err := repository.Batch(ctx, jobID, nil)
	if err != nil || leasedJob.Status != JobRunning || len(leasedItems) != 1 ||
		leasedItems[0].AttemptID == nil || *leasedItems[0].AttemptID != attemptA.Item.AttemptID {
		t.Fatalf("active leased batch changed job=%+v items=%+v error=%v", leasedJob, leasedItems, err)
	}
	attemptBNow := attemptA.Item.Item.LockedUntil.Add(time.Second)
	attemptB, err := repository.ClaimBatchItem(ctx, "worker-b", attemptBNow, time.Minute)
	if err != nil || attemptB.Item == nil || attemptB.Item.Item.ID != attemptA.Item.Item.ID ||
		attemptB.Item.AttemptID == attemptA.Item.AttemptID {
		t.Fatalf("attempt B claim = %+v error=%v", attemptB, err)
	}
	staleContext := withBatchMutationFence(ctx, &BatchMutationFence{
		JobID: jobID, ItemID: attemptA.Item.Item.ID,
		AttemptID: attemptA.Item.AttemptID, WorkerID: "worker-a",
	})
	if _, err := repository.UpdateMetadata(
		staleContext,
		actorID,
		"integration:stale-metadata",
		trackID,
		1,
		MetadataPatch{"title": "Must not be applied"},
		"stale attempt",
	); !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("stale metadata update error = %v", err)
	}
	var metadataCount int
	if err := pool.QueryRow(ctx, `select count(*)::int from track_metadata where track_id = $1`, trackID).Scan(&metadataCount); err != nil {
		t.Fatal(err)
	}
	if metadataCount != 0 {
		t.Fatalf("stale attempt created %d metadata rows", metadataCount)
	}
	rawTags, err := json.Marshal(MetadataSnapshot{
		Title: "Original title", Credits: []MetadataCredit{}, AlbumArtists: []string{}, Genres: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into track_metadata(track_id,raw_tags,overrides,version)
		values($1,$2::jsonb,'{}'::jsonb,1)`, trackID, rawTags); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EnqueueWriteback(
		staleContext,
		actorID,
		"integration:stale-writeback",
		trackID,
		1,
		"stale attempt",
	); !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("stale writeback enqueue error = %v", err)
	}
	var writebackCount int
	if err := pool.QueryRow(ctx, `select count(*)::int from metadata_writeback_jobs where track_id = $1`, trackID).Scan(&writebackCount); err != nil {
		t.Fatal(err)
	}
	if writebackCount != 0 {
		t.Fatalf("stale attempt enqueued %d writebacks", writebackCount)
	}

	mediaRepository := adminmedia.NewRepository(pool.Pool)
	now := time.Now().UTC()
	upload, err := mediaRepository.CreateUpload(ctx, adminmedia.CreateUploadParams{
		ID: uploadID, ActorID: actorID, Purpose: adminmedia.PurposeAlbumArtwork,
		TargetID: albumID, ObjectKey: objectKeys[0], FileName: "stale.png",
		ContentType: "image/png", SizeBytes: 64,
		ChecksumSHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(5 * time.Minute),
		Now: now, MaximumBytes: cfg.Storage.MaxUploadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := mediaRepository.ClaimCompletion(ctx, actorID, upload.ID, uuid.NewString(), now, 10*time.Minute)
	if err != nil || completion.Outcome != adminmedia.CompletionClaimed {
		t.Fatalf("media completion claim = %+v error=%v", completion, err)
	}
	_, err = mediaRepository.FinalizeCompletion(ctx, adminmedia.FinalizeCompletionParams{
		ActorID: actorID, TraceID: "integration:stale-artwork", UploadID: upload.ID,
		CompletionToken: completion.Token, AssetID: assetID,
		Inspected: adminmedia.InspectedUpload{
			ObjectKey: objectKeys[1], MIMEType: "image/jpeg", SizeBytes: 48,
			ChecksumSHA256: strings.Repeat("b", 64), CleanupKeys: objectKeys,
		},
		CompletionFence: batchMutationFenceFromContext(staleContext),
		Now:             now.Add(time.Second),
	})
	if !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("stale artwork completion error = %v", err)
	}
	var assetCount int
	var coverAssetID *string
	if err := pool.QueryRow(ctx, `select count(*)::int from media_assets where id = $1`, assetID).Scan(&assetCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select cover_asset_id from albums where id = $1`, albumID).Scan(&coverAssetID); err != nil {
		t.Fatal(err)
	}
	if assetCount != 0 || coverAssetID != nil {
		t.Fatalf("stale artwork asset/cover = %d / %v", assetCount, coverAssetID)
	}

	raceUpload, err := mediaRepository.CreateUpload(ctx, adminmedia.CreateUploadParams{
		ID: raceUploadID, ActorID: actorID, Purpose: adminmedia.PurposeAlbumArtwork,
		TargetID: albumID, ObjectKey: objectKeys[2], FileName: "cancel-race.png",
		ContentType: "image/png", SizeBytes: 64,
		ChecksumSHA256: strings.Repeat("c", 64), ExpiresAt: now.Add(5 * time.Minute),
		Now: now, MaximumBytes: cfg.Storage.MaxUploadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	inspector := &blockingArtworkInspector{
		started: make(chan struct{}),
		release: make(chan struct{}),
		result: adminmedia.InspectedUpload{
			ObjectKey: objectKeys[3], MIMEType: "image/jpeg", SizeBytes: 48,
			ChecksumSHA256: strings.Repeat("d", 64), CleanupKeys: objectKeys[2:],
		},
	}
	defer func() {
		select {
		case <-inspector.release:
		default:
			close(inspector.release)
		}
	}()
	generatedIDs := []string{uuid.NewString(), raceAssetID, uuid.NewString()}
	generatedIndex := 0
	mediaService, err := adminmedia.NewService(cfg, adminmedia.ServiceDependencies{
		Repository: mediaRepository,
		Storage:    integrationMediaStorage{},
		Inspector:  inspector,
		Clock:      integrationMediaClock{now},
		IDGenerator: func() string {
			value := generatedIDs[generatedIndex]
			generatedIndex++
			return value
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	currentContext := withBatchMutationFence(ctx, &BatchMutationFence{
		JobID: jobID, ItemID: attemptB.Item.Item.ID,
		AttemptID: attemptB.Item.AttemptID, WorkerID: "worker-b",
	})
	completionContext, cancelCompletion := artworkFollowupContext(currentContext)
	defer cancelCompletion()
	completionResult := make(chan error, 1)
	go func() {
		_, completeErr := mediaService.CompleteUpload(
			completionContext,
			actorID,
			"integration:cancel-artwork-race",
			raceUpload.ID,
			adminmedia.CompleteUploadInput{CompletionFence: &artworkCompletionFence{
				executionContext: currentContext,
				mutationFence:    batchMutationFenceFromContext(currentContext),
			}},
		)
		completionResult <- completeErr
	}()
	waitForSignal(t, inspector.started, "artwork inspection did not reach the final-fence barrier")
	if err := repository.RequestBatchCancel(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	close(inspector.release)
	completionErr := <-completionResult
	if !errors.Is(completionErr, errBatchCancellationRequested) {
		t.Fatalf("cancelled artwork completion error = %v", completionErr)
	}
	var raceStatus string
	var raceAssetCount, raceCleanupCount int
	if err := pool.QueryRow(ctx, `select status::text from media_uploads where id = $1`, raceUpload.ID).Scan(&raceStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from media_assets where id = $1`, raceAssetID).Scan(&raceAssetCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from object_cleanup_jobs
		where object_key = any($1::varchar[])`, objectKeys[2:]).Scan(&raceCleanupCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select cover_asset_id from albums where id = $1`, albumID).Scan(&coverAssetID); err != nil {
		t.Fatal(err)
	}
	if raceStatus != adminmedia.UploadStatusFailed || raceAssetCount != 0 || raceCleanupCount != 2 || coverAssetID != nil {
		t.Fatalf("cancelled artwork status/asset/cleanup/cover = %q / %d / %d / %v",
			raceStatus, raceAssetCount, raceCleanupCount, coverAssetID)
	}
	var runningAttempt, lockedBy, itemStatus string
	if err := pool.QueryRow(ctx, `select attempt_id::text,locked_by,status::text
		from tag_scraping_job_items where id = $1`, attemptB.Item.Item.ID).Scan(
		&runningAttempt,
		&lockedBy,
		&itemStatus,
	); err != nil {
		t.Fatal(err)
	}
	if runningAttempt != attemptB.Item.AttemptID || lockedBy != "worker-b" || itemStatus != string(ItemRunning) {
		t.Fatalf("current attempt ownership = %q / %q / %q", runningAttempt, lockedBy, itemStatus)
	}
}

type blockingArtworkInspector struct {
	started chan struct{}
	release chan struct{}
	result  adminmedia.InspectedUpload
}

func (inspector *blockingArtworkInspector) Inspect(
	ctx context.Context,
	_ adminmedia.MediaUpload,
	_ string,
) (adminmedia.InspectedUpload, error) {
	close(inspector.started)
	select {
	case <-ctx.Done():
		return adminmedia.InspectedUpload{}, ctx.Err()
	case <-inspector.release:
		return inspector.result, nil
	}
}

type integrationMediaStorage struct{}

func integrationPasswordHash(t *testing.T, suffix string) string {
	t.Helper()
	hash, err := platformsecurity.HashPassword("tag-scraping-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func (integrationMediaStorage) CreateUploadURL(
	context.Context,
	adminmedia.UploadURLRequest,
) (string, error) {
	return "", errors.New("unexpected integration CreateUploadURL call")
}

func (integrationMediaStorage) DownloadToFile(
	context.Context,
	string,
	string,
	int64,
) (adminmedia.StoredObject, error) {
	return adminmedia.StoredObject{}, errors.New("unexpected integration DownloadToFile call")
}

func (integrationMediaStorage) UploadFile(
	context.Context,
	string,
	string,
	string,
	string,
) (int64, error) {
	return 0, errors.New("unexpected integration UploadFile call")
}

type integrationMediaClock struct{ now time.Time }

func (clock integrationMediaClock) Now() time.Time { return clock.now }
