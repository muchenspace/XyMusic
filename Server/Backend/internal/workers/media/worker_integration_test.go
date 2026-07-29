package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/testsupport"
)

func skipOrFailMediaDependency(t *testing.T, format string, arguments ...any) {
	t.Helper()
	if mediaDependencyFailuresAreFatal() {
		t.Fatalf(format, arguments...)
	}
	t.Skipf(format, arguments...)
}

func mediaDependencyFailuresAreFatal() bool {
	return os.Getenv(testsupport.WriteIntegrationEnvironment) == "1"
}

func TestMediaDependencyFailureModeFollowsWriteIntegrationGate(t *testing.T) {
	t.Setenv(testsupport.WriteIntegrationEnvironment, "")
	if mediaDependencyFailuresAreFatal() {
		t.Fatal("dependency failures must remain skippable without the isolated write gate")
	}
	t.Setenv(testsupport.WriteIntegrationEnvironment, "1")
	if !mediaDependencyFailuresAreFatal() {
		t.Fatal("dependency failures must be fatal after the isolated write gate is enabled")
	}
}

func TestProductionMediaWorkerDependencies(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to probe production media worker dependencies")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner := OSProcessRunner{}
	for _, executable := range []string{cfg.Media.FFmpegPath, cfg.Media.FFprobePath} {
		result, probeErr := runner.Run(ctx, executable, []string{"-version"}, 10*time.Second)
		if probeErr != nil || result.TimedOut || result.ExitCode != 0 {
			skipOrFailMediaDependency(t, "media executable %q is unavailable: result=%+v error=%v", executable, result, probeErr)
		}
	}
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		skipOrFailMediaDependency(t, "configured PostgreSQL is unavailable: %v", err)
	}
	defer pool.Close()
	if err := database.CheckMigrationCompatibility(ctx, pool.Pool, cfg.Paths.MigrationsDirectory); err != nil {
		t.Fatal(err)
	}
	storage, err := NewMinIOObjectStorage(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Ping(ctx); err != nil {
		skipOrFailMediaDependency(t, "configured MinIO bucket is unavailable: %v", err)
	}
}

// TestProductionMediaWorkerLifecycle is deliberately opt-in because it writes
// PostgreSQL rows, uploads and deletes MinIO objects, and invokes FFmpeg.
func TestProductionMediaWorkerLifecycle(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production media worker lifecycle")
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
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	runner := OSProcessRunner{}
	for _, executable := range []string{cfg.Media.FFmpegPath, cfg.Media.FFprobePath} {
		result, probeErr := runner.Run(ctx, executable, []string{"-version"}, 10*time.Second)
		if probeErr != nil || result.TimedOut || result.ExitCode != 0 {
			t.Fatalf("media executable %q is unavailable: result=%+v error=%v", executable, result, probeErr)
		}
	}
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("configured PostgreSQL is unavailable: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.CheckMigrationCompatibility(ctx, pool.Pool, cfg.Paths.MigrationsDirectory); err != nil {
		t.Fatalf("configured PostgreSQL schema is unavailable: %v", err)
	}
	storage, err := NewMinIOObjectStorage(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Ping(ctx); err != nil {
		t.Fatalf("configured MinIO bucket is unavailable: %v", err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	rootID := uuid.NewString()
	trackID := uuid.NewString()
	jobID := uuid.NewString()
	sourceAssetID := uuid.NewString()
	oldAssetID := uuid.NewString()
	sourceID := uuid.NewString()
	failureTrackID := uuid.NewString()
	failureJobID := uuid.NewString()
	failureAssetID := uuid.NewString()
	failureSourceID := uuid.NewString()
	prefix := "integration/mediaworker/" + suffix
	sourceKey := prefix + "/source.flac"
	oldKey := prefix + "/old-standard.m4a"
	failureKey := prefix + "/unready-source.flac"
	variantPrefix := "media/variants/" + trackID + "/" + jobID + "/"

	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		rows, queryErr := pool.Query(cleanupContext, `SELECT object_key FROM media_assets
			WHERE object_key IN($1,$2,$3) OR object_key LIKE $4`, sourceKey, oldKey, failureKey, variantPrefix+"%")
		keys := []string{sourceKey, oldKey, failureKey}
		if queryErr == nil {
			for rows.Next() {
				var key string
				if rows.Scan(&key) == nil {
					keys = append(keys, key)
				}
			}
			rows.Close()
		}
		_, _ = pool.Exec(cleanupContext, `DELETE FROM library_roots WHERE id=$1`, rootID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM object_cleanup_jobs
			WHERE object_key IN($1,$2,$3) OR object_key LIKE $4`, sourceKey, oldKey, failureKey, variantPrefix+"%")
		_, _ = pool.Exec(cleanupContext, `DELETE FROM media_jobs WHERE id=ANY($1::uuid[])`, []string{jobID, failureJobID})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM track_variants WHERE track_id=ANY($1::uuid[])`, []string{trackID, failureTrackID})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM media_assets
			WHERE id=ANY($1::uuid[]) OR object_key LIKE $2`,
			[]string{sourceAssetID, oldAssetID, failureAssetID}, variantPrefix+"%")
		_, _ = pool.Exec(cleanupContext, `DELETE FROM tracks WHERE id=ANY($1::uuid[])`, []string{trackID, failureTrackID})
		for _, key := range compactStrings(keys) {
			if deleteErr := storage.Delete(cleanupContext, key); deleteErr != nil {
				t.Errorf("delete media worker integration object %q: %v", key, deleteErr)
			}
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.flac")
	generated, err := runner.Run(ctx, cfg.Media.FFmpegPath, []string{
		"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:a", "flac", sourcePath,
	}, 30*time.Second)
	if err != nil || generated.TimedOut || generated.ExitCode != 0 {
		t.Fatalf("configured FFmpeg cannot generate the integration fixture: result=%+v error=%v", generated, err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceChecksum, err := sha256File(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.UploadFile(ctx, sourceKey, sourcePath, "audio/flac", sourceChecksum); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(directory, "old.m4a")
	if err := os.WriteFile(oldPath, []byte("old-variant"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldDigest := sha256.Sum256([]byte("old-variant"))
	oldChecksum := hex.EncodeToString(oldDigest[:])
	if _, err := storage.UploadFile(ctx, oldKey, oldPath, "audio/mp4", oldChecksum); err != nil {
		t.Fatal(err)
	}
	seedNow := time.Now().UTC()

	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seedFailed := true
	defer func() {
		if seedFailed {
			_ = transaction.Rollback(context.Background())
		}
	}()
	rootPath := filepath.Join(directory, "library-"+suffix)
	if _, err := transaction.Exec(ctx, `INSERT INTO library_roots(
		id,name,path,normalized_path,mode,enabled,scan_on_startup,status
	) VALUES($1,$2,$3,$3,'READ_ONLY',true,false,'READY')`, rootID, "Media Worker "+suffix, rootPath); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO tracks(
		id,title,normalized_title,status,media_generation
	) VALUES($1,$2,$3,'READY',1),($4,$5,$6,'READY',1)`,
		trackID, "Media Worker Track "+suffix, "media worker track "+suffix,
		failureTrackID, "Media Worker Failure "+suffix, "media worker failure "+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO media_assets(
		id,object_key,kind,mime_type,size_bytes,checksum_sha256,status
	) VALUES
		($1,$2,'AUDIO_SOURCE','audio/flac',$3,$4,'READY'),
		($5,$6,'AUDIO_VARIANT','audio/mp4',11,$7,'READY'),
		($8,$9,'AUDIO_SOURCE','audio/flac',1,$10,'PENDING')`,
		sourceAssetID, sourceKey, sourceInfo.Size(), sourceChecksum,
		oldAssetID, oldKey, oldChecksum, failureAssetID, failureKey, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO media_jobs(
		id,type,source_asset_id,track_id,generation,idempotency_key,payload,publish_on_ready,max_attempts,next_attempt_at
	) VALUES
		($1,'INGEST_TRACK',$2,$3,1,$4,$5::jsonb,true,5,$10),
		($6,'INGEST_TRACK',$7,$8,1,$9,'{}'::jsonb,true,2,$11)`,
		jobID, sourceAssetID, trackID, "media-worker-it-"+jobID,
		`{"segmentStartMs":250,"segmentEndMs":1250}`,
		failureJobID, failureAssetID, failureTrackID, "media-worker-it-"+failureJobID,
		seedNow.Add(-time.Minute), seedNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO local_music_sources(
		id,root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,modified_at,
		track_id,source_asset_id,media_job_id,status
	) VALUES
		($1,$2,$3,$3,$4,$5,$6,$7,$8,$9,'PENDING'),
		($10,$2,$11,$11,$12,1,$6,$13,$14,$15,'PENDING')`,
		sourceID, rootID, "source-"+suffix+".flac", sourceChecksum, sourceInfo.Size(), sourceInfo.ModTime(),
		trackID, sourceAssetID, jobID,
		failureSourceID, "failure-"+suffix+".flac", strings.Repeat("f", 64),
		failureTrackID, failureAssetID, failureJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO local_music_source_tracks(
		source_id,track_id,media_job_id,segment_index,start_ms,end_ms
	) VALUES($1,$2,$3,0,250,1250),($4,$5,$6,0,0,NULL)`,
		sourceID, trackID, jobID, failureSourceID, failureTrackID, failureJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO track_variants(
		track_id,asset_id,quality,mime_type,codec,container,bitrate,status
	) VALUES($1,$2,'STANDARD','audio/mp4','aac','m4a',128000,'READY')`, trackID, oldAssetID); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	seedFailed = false

	store, err := NewPostgresStore(pool.Pool)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(Options{
		Store: store, Storage: storage, FFmpegPath: cfg.Media.FFmpegPath,
		FFprobePath: cfg.Media.FFprobePath, WorkerID: "media-worker-integration-" + suffix,
		Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })

	worked, err := worker.RunNext(ctx)
	if err != nil || !worked {
		t.Fatalf("process media job worked=%v error=%v", worked, err)
	}
	var jobStatus string
	var attempts int
	var attemptID string
	var lockedBy *string
	if err := pool.QueryRow(ctx, `SELECT status::text,attempts,attempt_id,locked_by
		FROM media_jobs WHERE id=$1`, jobID).Scan(&jobStatus, &attempts, &attemptID, &lockedBy); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "READY" || attempts != 1 || attemptID == "" || lockedBy != nil {
		t.Fatalf("completed media job status=%s attempts=%d attempt=%s lockedBy=%v", jobStatus, attempts, attemptID, lockedBy)
	}
	var durationMS int64
	var trackStatus, sourceStatus string
	var publishedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status::text,duration_ms,published_at FROM tracks WHERE id=$1`, trackID).Scan(
		&trackStatus, &durationMS, &publishedAt,
	); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM local_music_sources WHERE id=$1`, sourceID).Scan(&sourceStatus); err != nil {
		t.Fatal(err)
	}
	if trackStatus != "READY" || durationMS != 1_000 || publishedAt == nil || sourceStatus != "READY" {
		t.Fatalf("track status=%s duration=%d published=%v source=%s", trackStatus, durationMS, publishedAt, sourceStatus)
	}
	rows, err := pool.Query(ctx, `SELECT variant.quality,asset.object_key,asset.size_bytes,asset.checksum_sha256
		FROM track_variants variant JOIN media_assets asset ON asset.id=variant.asset_id
		WHERE variant.track_id=$1 ORDER BY variant.quality`, trackID)
	if err != nil {
		t.Fatal(err)
	}
	variantKeys := make([]string, 0, 4)
	qualities := make([]string, 0, 4)
	for rows.Next() {
		var quality, key, checksum string
		var size int64
		if err := rows.Scan(&quality, &key, &size, &checksum); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		qualities = append(qualities, quality)
		variantKeys = append(variantKeys, key)
		downloadPath := filepath.Join(directory, "download-"+quality)
		observed, err := storage.DownloadToFile(ctx, key, downloadPath, size)
		if err != nil || observed.SizeBytes != size || observed.ChecksumSHA256 != checksum {
			rows.Close()
			t.Fatalf("variant %s observed=%+v error=%v", quality, observed, err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if strings.Join(qualities, ",") != "DATA_SAVER,HIGH,LOSSLESS,STANDARD" {
		t.Fatalf("variant qualities = %#v", qualities)
	}
	var oldStatus, cleanupStatus string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM media_assets WHERE id=$1`, oldAssetID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM object_cleanup_jobs WHERE object_key=$1`, oldKey).Scan(&cleanupStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "DELETE_PENDING" || cleanupStatus != "PENDING" {
		t.Fatalf("old asset=%s cleanup=%s", oldStatus, cleanupStatus)
	}
	worked, err = worker.RunNext(ctx)
	if err != nil || !worked {
		t.Fatalf("object cleanup worked=%v error=%v", worked, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM media_assets WHERE id=$1`, oldAssetID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM object_cleanup_jobs WHERE object_key=$1`, oldKey).Scan(&cleanupStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "DELETED" || cleanupStatus != "COMPLETED" {
		t.Fatalf("cleaned old asset=%s cleanup=%s", oldStatus, cleanupStatus)
	}
	if _, err := storage.DownloadToFile(ctx, oldKey, filepath.Join(directory, "deleted-old"), 100); err == nil {
		t.Fatal("deleted old media object is still downloadable")
	}

	if _, err := pool.Exec(ctx, `UPDATE media_jobs SET next_attempt_at=$2 WHERE id=$1`,
		failureJobID, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	worked, err = worker.RunNext(ctx)
	if err != nil || !worked {
		t.Fatalf("first failed media attempt worked=%v error=%v", worked, err)
	}
	var lastErrorCode string
	var nextAttemptAt, updatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT status::text,attempts,last_error_code,next_attempt_at,updated_at
		FROM media_jobs WHERE id=$1`, failureJobID).Scan(
		&jobStatus, &attempts, &lastErrorCode, &nextAttemptAt, &updatedAt,
	); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "PENDING" || attempts != 1 || lastErrorCode != "SOURCE_ASSET_UNAVAILABLE" ||
		nextAttemptAt.Sub(updatedAt) != 10*time.Second {
		t.Fatalf("retry status=%s attempts=%d code=%s delay=%s", jobStatus, attempts, lastErrorCode, nextAttemptAt.Sub(updatedAt))
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM local_music_sources WHERE id=$1`, failureSourceID).Scan(&sourceStatus); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "PROCESSING" {
		t.Fatalf("retry source status=%s", sourceStatus)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_jobs SET next_attempt_at=$2 WHERE id=$1`,
		failureJobID, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	worked, err = worker.RunNext(ctx)
	if err != nil || !worked {
		t.Fatalf("terminal failed media attempt worked=%v error=%v", worked, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text,attempts,last_error_code FROM media_jobs WHERE id=$1`, failureJobID).Scan(
		&jobStatus, &attempts, &lastErrorCode,
	); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM tracks WHERE id=$1`, failureTrackID).Scan(&trackStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM local_music_sources WHERE id=$1`, failureSourceID).Scan(&sourceStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "FAILED" || attempts != 2 || lastErrorCode != "SOURCE_ASSET_UNAVAILABLE" ||
		trackStatus != "ERROR" || sourceStatus != "FAILED" {
		t.Fatalf("terminal job=%s attempts=%d code=%s track=%s source=%s",
			jobStatus, attempts, lastErrorCode, trackStatus, sourceStatus)
	}

	for _, key := range variantKeys {
		if !strings.HasPrefix(key, variantPrefix+attemptID+"/") {
			t.Fatalf("variant key %q does not contain the fenced attempt id %q", key, attemptID)
		}
	}
}

func TestProductionStoreCancellationFencing(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production media worker fencing")
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
		t.Fatalf("configured PostgreSQL is unavailable: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.CheckMigrationCompatibility(ctx, pool.Pool, cfg.Paths.MigrationsDirectory); err != nil {
		t.Fatalf("configured PostgreSQL schema is unavailable: %v", err)
	}
	trackID := uuid.NewString()
	assetID := uuid.NewString()
	jobID := uuid.NewString()
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM media_jobs WHERE id=$1`, jobID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM media_assets WHERE id=$1`, assetID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM tracks WHERE id=$1`, trackID)
	}
	t.Cleanup(cleanup)
	cleanup()
	if _, err := pool.Exec(ctx, `INSERT INTO tracks(id,title,normalized_title,status,media_generation)
		VALUES($1,'Fencing Track',$2,'READY',1)`, trackID, "fencing "+trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_assets(id,object_key,kind,mime_type,size_bytes,status)
		VALUES($1,$2,'AUDIO_SOURCE','audio/flac',1,'READY')`, assetID, "integration/fencing/"+assetID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO media_jobs(
		id,type,source_asset_id,track_id,generation,idempotency_key,next_attempt_at
	) VALUES($1,'INGEST_TRACK',$2,$3,1,$4,$5)`,
		jobID, assetID, trackID, "fencing-"+jobID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	store, err := NewPostgresStore(pool.Pool)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimMediaJob(ctx, "fencing-worker", now, defaultLease)
	if err != nil || claimed == nil || claimed.ID != jobID || claimed.AttemptID == nil {
		t.Fatalf("claimed=%+v error=%v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_jobs SET cancel_requested=true WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	owned, err := store.RenewMediaLease(
		ctx, jobID, *claimed.AttemptID, "fencing-worker", now, now.Add(defaultLease),
	)
	if err != nil || owned {
		t.Fatalf("cancelled lease owned=%v error=%v", owned, err)
	}
	control, err := store.MediaJobControl(ctx, jobID, *claimed.AttemptID, "fencing-worker")
	if err != nil || !control.Owned || !control.CancelRequested {
		t.Fatalf("control=%+v error=%v", control, err)
	}
	if err := store.FailMediaJob(
		ctx, *claimed, "fencing-worker",
		newInterruptedError("JOB_CANCELLED", "media job cancellation was requested"), now.Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}
	var status, code, message string
	var lockedBy, lockedUntil *string
	if err := pool.QueryRow(ctx, `SELECT status::text,last_error_code,last_error,locked_by,locked_until::text
		FROM media_jobs WHERE id=$1`, jobID).Scan(&status, &code, &message, &lockedBy, &lockedUntil); err != nil {
		t.Fatal(err)
	}
	if status != "CANCELLED" || code != "CANCELLED" || message != "Cancelled by an administrator" ||
		lockedBy != nil || lockedUntil != nil {
		t.Fatalf("cancelled status=%s code=%s message=%q lockedBy=%v lockedUntil=%v",
			status, code, message, lockedBy, lockedUntil)
	}

	_, err = store.CommitMediaJob(ctx, CommitMediaJob{
		Job: *claimed, WorkerID: "fencing-worker", DurationMS: 1_000, CompletedAt: now.Add(2 * time.Second),
	})
	if err == nil || workerErrorCode(err) != "JOB_LEASE_LOST" || !isInterrupted(err) {
		t.Fatalf("stale completion error=%v", err)
	}
	var stillCancelled string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM media_jobs WHERE id=$1`, jobID).Scan(&stillCancelled); err != nil {
		t.Fatal(err)
	}
	if stillCancelled != "CANCELLED" {
		t.Fatalf("stale completion changed job to %s", stillCancelled)
	}
}
