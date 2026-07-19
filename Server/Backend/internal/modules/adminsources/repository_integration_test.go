package adminsources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/adminmetadata"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/testsupport"
)

func TestRepositoryRunsLibrarySourceLifecycleInConfiguredDatabase(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production library source repository checks")
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
	defer pool.Close()
	transaction, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))
	repository := &Repository{database: transaction}

	suffix := uuid.NewString()
	short := suffix[:8]
	passwordHash, err := platformsecurity.HashPassword("adminsources-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	var actorID string
	if err := transaction.QueryRow(ctx, `INSERT INTO users(
		username,normalized_username,password_hash,role
	) VALUES($1,$1,$2,'ADMIN') RETURNING id`, "it_sources_"+short, passwordHash).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	rootDirectory := t.TempDir()
	secondDirectory := filepath.Join(rootDirectory, "second")
	if err := os.Mkdir(secondDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	mutation, err := validateRootInput(rootDirectory, RootMutation{
		Name: "Integration " + short, Path: rootDirectory, Mode: RootModeReadOnly,
		Enabled: true, ScanOnStartup: false, IncludePatterns: []string{"**/*.flac"}, ExcludePatterns: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.CreateRoot(ctx, actorID, "trace-create-"+short, mutation)
	if err != nil {
		t.Fatal(err)
	}
	rootID := created.Root.ID
	if created.Root.Version != 1 || created.Root.Status != RootStatusUnknown {
		t.Fatalf("created=%+v", created.Root)
	}

	var trackID string
	if err := transaction.QueryRow(ctx, `INSERT INTO tracks(
		title,normalized_title,duration_ms,status
	) VALUES($1,$2,1000,'READY') RETURNING id`, "Source Track "+short, "source track "+short).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	var assetID string
	if err := transaction.QueryRow(ctx, `INSERT INTO media_assets(
		uploader_id,object_key,kind,mime_type,size_bytes,checksum_sha256,status
	) VALUES($1,$2,'AUDIO_SOURCE','audio/flac',100,$3,'READY') RETURNING id`,
		actorID, "integration/sources/"+suffix+".flac", strings.Repeat("a", 64)).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO media_jobs(
		type,source_asset_id,track_id,status,idempotency_key,payload
	) VALUES('INGEST_TRACK',$1,$2,'READY',$3,'{}'::jsonb)`,
		assetID, trackID, "integration-source-historical-"+suffix); err != nil {
		t.Fatal(err)
	}
	var summaryRunID string
	if err := transaction.QueryRow(ctx, `INSERT INTO library_scan_runs(
		root_id,root_version,status,started_at,completed_at
	) VALUES($1,1,'COMPLETED',now(),now()) RETURNING id`, rootID).Scan(&summaryRunID); err != nil {
		t.Fatal(err)
	}
	var mediaJobID string
	if err := transaction.QueryRow(ctx, `INSERT INTO media_jobs(
		type,source_asset_id,track_id,status,idempotency_key,payload,scan_run_id
	) VALUES('INGEST_TRACK',$1,$2,'PENDING',$3,'{}'::jsonb,$4) RETURNING id`,
		assetID, trackID, "integration-source-"+suffix, summaryRunID).Scan(&mediaJobID); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := transaction.QueryRow(ctx, `INSERT INTO local_music_sources(
		root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,modified_at,
		track_id,source_asset_id,media_job_id,status
	) VALUES($1,$2,$2,$3,100,now(),$4,$5,$6,'PROCESSING') RETURNING id`,
		rootID, "album/track-"+short+".flac", strings.Repeat("b", 64), trackID, assetID, mediaJobID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO local_music_source_tracks(
		source_id,track_id,media_job_id,segment_index,start_ms
	) VALUES($1,$2,$3,0,0)`, sourceID, trackID, mediaJobID); err != nil {
		t.Fatal(err)
	}
	views, total, err := repository.ListRootViews(ctx, RootQuery{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if total < len(views) {
		t.Fatalf("root total=%d views=%d", total, len(views))
	}
	view, found := findRootView(views, rootID)
	if !found || view.Counts.FileCount != 1 || view.Counts.TrackCount != 1 ||
		view.LatestRun == nil || view.LatestRun.ID != summaryRunID {
		t.Fatalf("root view=%+v found=%v", view, found)
	}
	files, total, err := repository.ListFiles(ctx, rootID, FileQuery{Page: 1, PageSize: 25, Query: "track", Status: SourceFileProcessing})
	if err != nil || total != 1 || len(files) != 1 || files[0].TrackID != trackID {
		t.Fatalf("files=%+v total=%d err=%v", files, total, err)
	}
	processing, err := repository.ProcessingSummary(ctx, rootID)
	if err != nil || processing.Queued != 1 || len(processing.Jobs) != 1 || processing.Jobs[0].ID != mediaJobID {
		t.Fatalf("processing=%+v err=%v", processing, err)
	}

	run1, err := repository.EnqueueScan(ctx, EnqueueScanCommand{
		RootID: rootID, ActorID: &actorID, TraceID: "trace-scan-" + short,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EnqueueScan(ctx, EnqueueScanCommand{RootID: rootID}); !apperror.IsCode(err, apperror.CodeResourceConflict) {
		t.Fatalf("duplicate enqueue err=%v", err)
	}
	if err := repository.CancelScan(ctx, CancelScanCommand{
		RootID: rootID, RunID: run1.ID, ActorID: &actorID, TraceID: "trace-cancel-" + short,
	}); err != nil {
		t.Fatal(err)
	}
	cancelledRun, err := repository.FindRun(ctx, rootID, run1.ID)
	if err != nil || cancelledRun.Status != ScanStatusCancelled || !cancelledRun.CancelRequested {
		t.Fatalf("cancelled run=%+v err=%v", cancelledRun, err)
	}

	updatedMutation, err := validateRootInput(rootDirectory, RootMutation{
		Name: "Updated " + short, Path: secondDirectory, Mode: RootModeReadWrite,
		Enabled: true, ScanOnStartup: true, IncludePatterns: []string{"**/*.flac"}, ExcludePatterns: []string{"tmp/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateRoot(ctx, UpdateRootCommand{
		ActorID: actorID, TraceID: "trace-update-" + short, RootID: rootID,
		ExpectedVersion: 1, Mutation: updatedMutation, ChangedFields: []string{"name", "path", "mode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Root.Version != 2 || updated.Root.Path != secondDirectory || updated.Root.Mode != RootModeReadWrite {
		t.Fatalf("updated=%+v", updated.Root)
	}
	if _, err := repository.UpdateRoot(ctx, UpdateRootCommand{
		ActorID: actorID, TraceID: "trace-stale-" + short, RootID: rootID,
		ExpectedVersion: 1, Mutation: updatedMutation,
	}); !apperror.IsCode(err, apperror.CodeVersionConflict) {
		t.Fatalf("stale update err=%v", err)
	}

	run2, err := repository.EnqueueScan(ctx, EnqueueScanCommand{RootID: rootID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claim, err := repository.ClaimNextScan(ctx, "integration-worker", now, 2*time.Minute)
	if err != nil || claim == nil || claim.Run.ID != run2.ID || claim.Run.AttemptID == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	attemptID := *claim.Run.AttemptID
	owned, err := repository.UpdateScanProgress(ctx, run2.ID, attemptID, "integration-worker",
		ScanProgress{DiscoveredFiles: 5, ProcessedFiles: 3, FailedFiles: 1}, now.Add(time.Second))
	if err != nil || !owned {
		t.Fatalf("progress owned=%v err=%v", owned, err)
	}
	completed, err := repository.CompleteScan(ctx, *claim, attemptID, "integration-worker",
		ScanResult{DiscoveredFiles: 5, ProcessedFiles: 5, FailedFiles: 1, ArchivedFiles: 0}, now.Add(2*time.Second))
	if err != nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	completedRun, err := repository.FindRun(ctx, rootID, run2.ID)
	if err != nil || completedRun.Status != ScanStatusCompleted || completedRun.ProcessedFiles != 5 {
		t.Fatalf("completed run=%+v err=%v", completedRun, err)
	}

	run3, err := repository.EnqueueScan(ctx, EnqueueScanCommand{RootID: rootID})
	if err != nil {
		t.Fatal(err)
	}
	claim, err = repository.ClaimNextScan(ctx, "expired-worker", now.Add(3*time.Second), time.Minute)
	if err != nil || claim == nil || claim.Run.ID != run3.ID {
		t.Fatalf("expired claim=%+v err=%v", claim, err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE library_scan_runs SET locked_until=$2 WHERE id=$1`, run3.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repository.InitializeScans(ctx, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	expiredRun, err := repository.FindRun(ctx, rootID, run3.ID)
	if err != nil || expiredRun.Status != ScanStatusFailed {
		t.Fatalf("expired run=%+v err=%v", expiredRun, err)
	}

	current, err := repository.FindRoot(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	disabledMutation := RootMutation{
		Name: current.Name, Path: current.Path, NormalizedPath: current.NormalizedPath, Mode: current.Mode,
		Enabled: false, ScanOnStartup: current.ScanOnStartup, ScanIntervalMinutes: current.ScanIntervalMinutes,
		IncludePatterns: current.IncludePatterns, ExcludePatterns: current.ExcludePatterns, Status: RootStatusDisabled,
	}
	disabled, err := repository.UpdateRoot(ctx, UpdateRootCommand{
		ActorID: actorID, TraceID: "trace-disable-" + short, RootID: rootID,
		ExpectedVersion: current.Version, Mutation: disabledMutation, ChangedFields: []string{"enabled"},
	})
	if err != nil || disabled.Root.Status != RootStatusDisabled {
		t.Fatalf("disabled=%+v err=%v", disabled.Root, err)
	}
	if _, err := repository.EnqueueScan(ctx, EnqueueScanCommand{RootID: rootID}); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("disabled enqueue err=%v", err)
	}

	reenabledMutation := disabledMutation
	reenabledMutation.Enabled = true
	reenabledMutation.Status = RootStatusUnknown
	reenabled, err := repository.UpdateRoot(ctx, UpdateRootCommand{
		ActorID: actorID, TraceID: "trace-enable-" + short, RootID: rootID,
		ExpectedVersion: disabled.Root.Version, Mutation: reenabledMutation, ChangedFields: []string{"enabled"},
	})
	if err != nil || reenabled.Root.Status != RootStatusUnknown {
		t.Fatalf("reenabled=%+v err=%v", reenabled.Root, err)
	}
	run4, err := repository.EnqueueScan(ctx, EnqueueScanCommand{RootID: rootID})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteRoot(ctx, DeleteRootCommand{
		ActorID: actorID, TraceID: "trace-active-delete-" + short, RootID: rootID,
		ExpectedVersion: reenabled.Root.Version,
	}); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("active delete err=%v", err)
	}
	if err := repository.CancelScan(ctx, CancelScanCommand{RootID: rootID, RunID: run4.ID}); err != nil {
		t.Fatal(err)
	}
	runs, total, err := repository.ListRuns(ctx, rootID, PageQuery{Page: 1, PageSize: 25})
	if err != nil || total < 4 || len(runs) < 4 {
		t.Fatalf("runs=%d total=%d err=%v", len(runs), total, err)
	}
	if err := repository.DeleteRoot(ctx, DeleteRootCommand{
		ActorID: actorID, TraceID: "trace-delete-" + short, RootID: rootID,
		ExpectedVersion: reenabled.Root.Version, ArchiveCatalog: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FindRoot(ctx, rootID); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("deleted root err=%v", err)
	}
	var trackStatus string
	if err := transaction.QueryRow(ctx, `SELECT status::text FROM tracks WHERE id=$1`, trackID).Scan(&trackStatus); err != nil {
		t.Fatal(err)
	}
	if trackStatus != "ARCHIVED" {
		t.Fatalf("track status=%s", trackStatus)
	}
	var auditCount int
	if err := transaction.QueryRow(ctx, `SELECT count(*)::int FROM audit_logs
		WHERE actor_id=$1 AND target_id=$2 AND action LIKE 'admin.library-root.%'`, actorID, rootID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 7 {
		t.Fatalf("audit count=%d", auditCount)
	}
}

func TestEnsureDefaultRootSynchronizesConfiguredRoot(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run configured default-root synchronization checks")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	directory := t.TempDir()
	repository := &Repository{database: transaction}
	initial, err := validateRootInput(directory, RootMutation{
		Name: "Configured default", Path: directory, Mode: RootModeReadOnly, Enabled: true,
		ScanOnStartup: true, IncludePatterns: []string{"*.mp3"}, ExcludePatterns: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.EnsureDefaultRoot(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	updatedMutation, err := validateRootInput(directory, RootMutation{
		Name: "Configured default updated", Path: directory, Mode: RootModeReadWrite, Enabled: true,
		ScanOnStartup: false, IncludePatterns: []string{"*.flac"}, ExcludePatterns: []string{"*.tmp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.EnsureDefaultRoot(ctx, updatedMutation)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Mode != RootModeReadWrite || updated.Name != updatedMutation.Name ||
		updated.ScanOnStartup || len(updated.IncludePatterns) != 1 || updated.IncludePatterns[0] != "*.flac" ||
		len(updated.ExcludePatterns) != 1 || updated.ExcludePatterns[0] != "*.tmp" {
		t.Fatalf("synchronized root=%+v", updated)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("version after synchronization=%d, want %d", updated.Version, created.Version+1)
	}
	unchanged, err := repository.EnsureDefaultRoot(ctx, updatedMutation)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Version != updated.Version {
		t.Fatalf("unchanged root version=%d, want %d", unchanged.Version, updated.Version)
	}
}

func TestProductionSynchronizerPersistsFilesMetadataAndCUEInConfiguredDatabase(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production local library synchronization checks")
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
	defer pool.Close()
	transaction, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))

	suffix := uuid.NewString()
	short := suffix[:8]
	passwordHash, err := platformsecurity.HashPassword("adminsources-sync-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	var actorID string
	if err := transaction.QueryRow(ctx, `INSERT INTO users(
		username,normalized_username,password_hash,role
	) VALUES($1,$1,$2,'ADMIN') RETURNING id`, "it_sync_"+short, passwordHash).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	mutation, err := validateRootInput(directory, RootMutation{
		Name: "Sync " + short, Path: directory, Mode: RootModeReadOnly, Enabled: true,
		IncludePatterns: []string{}, ExcludePatterns: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{database: transaction}
	view, err := repository.CreateRoot(ctx, actorID, "trace-sync-"+short, mutation)
	if err != nil {
		t.Fatal(err)
	}
	rootID := view.Root.ID
	storage := &syncStorageStub{}
	probe := metadataProbeStub{metadata: adminmetadata.MetadataSnapshot{
		Title: "Scanned Song", Credits: []adminmetadata.MetadataCredit{{Name: "Scan Artist", Role: adminmetadata.CreditPrimary}},
		AlbumArtists: []string{"Scan Artist"}, Album: stringPointer("Scan Album"),
		TrackNumber: intPointer(1), DiscNumber: intPointer(1), Genres: []string{},
	}}
	synchronizer := &ProductionSynchronizer{
		database: transaction, storage: storage, probe: probe, now: func() time.Time { return time.Now().UTC() },
	}
	audioPath := filepath.Join(directory, "song.flac")
	if err := os.WriteFile(audioPath, []byte("first-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "song.lrc"), []byte("[00:01.00]line"), 0o600); err != nil {
		t.Fatal(err)
	}
	seenAt := time.Now().UTC()
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: audioPath, RelativePath: "song.flac",
	}, seenAt); err != nil {
		t.Fatal(err)
	}
	var sourceID, trackID, jobID, sourceStatus string
	if err := transaction.QueryRow(ctx, `SELECT id,track_id,media_job_id,status
		FROM local_music_sources WHERE root_id=$1 AND normalized_source_path='song.flac'`, rootID).Scan(
		&sourceID, &trackID, &jobID, &sourceStatus,
	); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "PROCESSING" || len(storage.uploads) != 1 {
		t.Fatalf("source status=%s uploads=%+v", sourceStatus, storage.uploads)
	}
	var title, artistName, lyricOrigin, lyricFormat string
	if err := transaction.QueryRow(ctx, `SELECT track.title,artist.name,lyric.origin::text,lyric.format::text
		FROM tracks track JOIN track_artists credit ON credit.track_id=track.id
		JOIN artists artist ON artist.id=credit.artist_id
		JOIN lyrics lyric ON lyric.track_id=track.id
		WHERE track.id=$1 AND credit.role='PRIMARY'`, trackID).Scan(&title, &artistName, &lyricOrigin, &lyricFormat); err != nil {
		t.Fatal(err)
	}
	if title != "Scanned Song" || artistName != "Scan Artist" || lyricOrigin != "EXTERNAL" || lyricFormat != "LRC" {
		t.Fatalf("catalog=%q/%q lyric=%s/%s", title, artistName, lyricOrigin, lyricFormat)
	}
	var metadataVersion, revisionCount int
	if err := transaction.QueryRow(ctx, `SELECT metadata.version,
		(SELECT count(*)::int FROM track_metadata_revisions revision WHERE revision.track_id=metadata.track_id)
		FROM track_metadata metadata WHERE track_id=$1`, trackID).Scan(&metadataVersion, &revisionCount); err != nil {
		t.Fatal(err)
	}
	if metadataVersion != 1 || revisionCount != 1 {
		t.Fatalf("metadata version/revisions=%d/%d", metadataVersion, revisionCount)
	}
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: audioPath, RelativePath: "song.flac",
	}, seenAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var jobCount int
	if err := transaction.QueryRow(ctx, `SELECT count(*)::int FROM media_jobs WHERE track_id=$1`, trackID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatalf("unchanged job count=%d", jobCount)
	}
	if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET status='READY',updated_at=now() WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: audioPath, RelativePath: "song.flac",
	}, seenAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	var sourceObjectKey string
	if err := transaction.QueryRow(ctx, `SELECT asset.object_key FROM local_music_sources source
		JOIN media_assets asset ON asset.id=source.source_asset_id WHERE source.id=$1`, sourceID).Scan(&sourceObjectKey); err != nil {
		t.Fatal(err)
	}
	delete(storage.objects, sourceObjectKey)
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: audioPath, RelativePath: "song.flac",
	}, seenAt.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(storage.uploads) != 2 {
		t.Fatalf("missing reusable source asset was not uploaded again: uploads=%+v", storage.uploads)
	}
	if err := transaction.QueryRow(ctx, `SELECT count(*)::int FROM media_jobs WHERE track_id=$1`, trackID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 2 {
		t.Fatalf("missing reusable source asset job count=%d", jobCount)
	}
	if err := transaction.QueryRow(ctx, `SELECT media_job_id FROM local_music_sources WHERE id=$1`, sourceID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET status='READY',updated_at=now() WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status='READY' WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("second-audio-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := seenAt.Add(4 * time.Second)
	if err := os.Chtimes(audioPath, modified, modified); err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: audioPath, RelativePath: "song.flac",
	}, modified); err != nil {
		t.Fatal(err)
	}
	if err := transaction.QueryRow(ctx, `SELECT count(*)::int FROM media_jobs WHERE track_id=$1`, trackID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 3 {
		t.Fatalf("changed job count=%d", jobCount)
	}
	var latestJobID string
	if err := transaction.QueryRow(ctx, `SELECT media_job_id FROM local_music_sources WHERE id=$1`, sourceID).Scan(&latestJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET status='READY',updated_at=now() WHERE id=$1`, latestJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status='READY' WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE tracks SET status='ARCHIVED' WHERE id=$1`, trackID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("archived-audio-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivedModified := modified.Add(time.Second)
	if err := os.Chtimes(audioPath, archivedModified, archivedModified); err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: audioPath, RelativePath: "song.flac",
	}, archivedModified); err != nil {
		t.Fatal(err)
	}
	var archivedTrackStatus string
	if err := transaction.QueryRow(ctx, `SELECT status::text FROM tracks WHERE id=$1`, trackID).Scan(&archivedTrackStatus); err != nil {
		t.Fatal(err)
	}
	if archivedTrackStatus != "ARCHIVED" {
		t.Fatalf("changed archived track status=%s", archivedTrackStatus)
	}
	if err := transaction.QueryRow(ctx, `SELECT media_job_id FROM local_music_sources WHERE id=$1`, sourceID).Scan(&latestJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE media_jobs SET status='READY',updated_at=now() WHERE id=$1`, latestJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET status='READY' WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	renamedPath := filepath.Join(directory, "renamed.flac")
	if err := os.Rename(audioPath, renamedPath); err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: renamedPath, RelativePath: "renamed.flac",
	}, archivedModified.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var renamedSourceID string
	if err := transaction.QueryRow(ctx, `SELECT id FROM local_music_sources
		WHERE root_id=$1 AND normalized_source_path='renamed.flac'`, rootID).Scan(&renamedSourceID); err != nil {
		t.Fatal(err)
	}
	if renamedSourceID != sourceID {
		t.Fatalf("renamed source=%s want=%s", renamedSourceID, sourceID)
	}

	cueAudio := filepath.Join(directory, "disc.wav")
	cuePath := filepath.Join(directory, "disc.cue")
	if err := os.WriteFile(cueAudio, []byte("cue-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	cueContent := `TITLE "Cue Album"
PERFORMER "Cue Artist"
FILE "disc.wav" WAVE
  TRACK 01 AUDIO
    TITLE "First"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Second"
    INDEX 01 01:00:00`
	if err := os.WriteFile(cuePath, []byte(cueContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: cueAudio, RelativePath: "disc.wav", CuePath: cuePath,
	}, seenAt.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	var cueSourceID string
	if err := transaction.QueryRow(ctx, `SELECT id FROM local_music_sources
		WHERE root_id=$1 AND normalized_source_path='disc.wav'`, rootID).Scan(&cueSourceID); err != nil {
		t.Fatal(err)
	}
	var mappingCount, segmentEnd int
	if err := transaction.QueryRow(ctx, `SELECT count(*)::int,
		max(end_ms) FILTER(WHERE segment_index=0)::int FROM local_music_source_tracks WHERE source_id=$1`, cueSourceID).Scan(
		&mappingCount, &segmentEnd,
	); err != nil {
		t.Fatal(err)
	}
	if mappingCount != 2 || segmentEnd != 60000 {
		t.Fatalf("CUE mappings=%d first end=%d", mappingCount, segmentEnd)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET last_seen_at=$2,status='READY'
		WHERE id=$1`, sourceID, seenAt.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	archived, err := synchronizer.ArchiveMissing(ctx, rootID, seenAt, seenAt.Add(6*time.Second))
	if err != nil || archived != 1 {
		t.Fatalf("archived=%d err=%v", archived, err)
	}
	var archivedStatus string
	if err := transaction.QueryRow(ctx, `SELECT status FROM local_music_sources WHERE id=$1`, sourceID).Scan(&archivedStatus); err != nil {
		t.Fatal(err)
	}
	if archivedStatus != "MISSING" {
		t.Fatalf("archived source status=%s", archivedStatus)
	}

	probeFailure := errors.New("metadata probe failed")
	synchronizer.probe = metadataProbeFailureStub{err: probeFailure}
	historicalArchiveAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		status='READY',last_seen_at=$2,updated_at=$3 WHERE id=$1`,
		sourceID, historicalArchiveAt.Add(-time.Hour), historicalArchiveAt.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE tracks SET
		status='READY',updated_at=$2 WHERE id=$1`, trackID, historicalArchiveAt.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET last_seen_at=$2
		WHERE root_id=$1 AND id<>$3`, rootID, historicalArchiveAt, sourceID); err != nil {
		t.Fatal(err)
	}
	archived, err = synchronizer.ArchiveMissing(ctx, rootID, historicalArchiveAt, historicalArchiveAt)
	if err != nil || archived != 1 {
		t.Fatalf("historical archived=%d err=%v", archived, err)
	}
	var sourceArchivedAt, trackArchivedAt time.Time
	if err := transaction.QueryRow(ctx, `SELECT source.updated_at,track.updated_at
		FROM local_music_sources source JOIN tracks track ON track.id=$2
		WHERE source.id=$1`, sourceID, trackID).Scan(&sourceArchivedAt, &trackArchivedAt); err != nil {
		t.Fatal(err)
	}
	if !sourceArchivedAt.Equal(trackArchivedAt) {
		t.Fatalf("automatic archive timestamps differ: source=%s track=%s", sourceArchivedAt, trackArchivedAt)
	}
	failureSeenAt := historicalArchiveAt.Add(time.Second)
	err = synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: renamedPath, RelativePath: "renamed.flac",
	}, failureSeenAt)
	if !errors.Is(err, probeFailure) {
		t.Fatalf("processing failure=%v", err)
	}
	var failedSourceStatus, failedSourceError, failedTrackStatus string
	var failedLastSeenAt time.Time
	if err := transaction.QueryRow(ctx, `SELECT source.status,source.last_error,source.last_seen_at,track.status
		FROM local_music_sources source JOIN tracks track ON track.id=$2 WHERE source.id=$1`,
		sourceID, trackID).Scan(
		&failedSourceStatus, &failedSourceError, &failedLastSeenAt, &failedTrackStatus,
	); err != nil {
		t.Fatal(err)
	}
	if failedSourceStatus != "FAILED" || failedSourceError == "" || !failedLastSeenAt.Equal(failureSeenAt) {
		t.Fatalf("failed source=%s error=%q lastSeen=%s want=%s",
			failedSourceStatus, failedSourceError, failedLastSeenAt, failureSeenAt)
	}
	if failedTrackStatus != "ERROR" {
		t.Fatalf("historically archived track status=%s", failedTrackStatus)
	}
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET last_seen_at=$2
		WHERE root_id=$1 AND id<>$3`, rootID, failureSeenAt, sourceID); err != nil {
		t.Fatal(err)
	}
	archived, err = synchronizer.ArchiveMissing(ctx, rootID, failureSeenAt, failureSeenAt.Add(time.Second))
	if err != nil || archived != 0 {
		t.Fatalf("failed-but-seen archived=%d err=%v", archived, err)
	}
	if err := transaction.QueryRow(ctx, `SELECT status FROM local_music_sources WHERE id=$1`, sourceID).Scan(&failedSourceStatus); err != nil {
		t.Fatal(err)
	}
	if failedSourceStatus != "FAILED" {
		t.Fatalf("failed-but-seen source status=%s", failedSourceStatus)
	}

	manualSignatureAt := failureSeenAt.Add(2 * time.Second)
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		status='MISSING',updated_at=$2 WHERE id=$1`, sourceID, manualSignatureAt); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE tracks SET
		status='ARCHIVED',updated_at=$2 WHERE id=$1`, trackID, manualSignatureAt); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO audit_logs(
		actor_id,action,target_type,target_id,result,trace_id,details
	) VALUES($1,'admin.track.archive','track',$2,'SUCCESS',$3,'{}'::jsonb)`,
		actorID, trackID, "trace-manual-archive-"+short); err != nil {
		t.Fatal(err)
	}
	err = synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: renamedPath, RelativePath: "renamed.flac",
	}, manualSignatureAt.Add(time.Second))
	if !errors.Is(err, probeFailure) {
		t.Fatalf("manual archive processing failure=%v", err)
	}
	if err := transaction.QueryRow(ctx, `SELECT status FROM tracks WHERE id=$1`, trackID).Scan(&failedTrackStatus); err != nil {
		t.Fatal(err)
	}
	if failedTrackStatus != "ARCHIVED" {
		t.Fatalf("manually archived track status=%s", failedTrackStatus)
	}

	if _, err := transaction.Exec(ctx, `DELETE FROM audit_logs
		WHERE action='admin.track.archive' AND target_id=$1`, trackID); err != nil {
		t.Fatal(err)
	}
	mismatchedSourceAt := manualSignatureAt.Add(2 * time.Second)
	if _, err := transaction.Exec(ctx, `UPDATE local_music_sources SET
		status='MISSING',updated_at=$2 WHERE id=$1`, sourceID, mismatchedSourceAt); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE tracks SET
		status='ARCHIVED',updated_at=$2 WHERE id=$1`, trackID, mismatchedSourceAt.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	err = synchronizer.ProcessFile(ctx, rootID, "", DiscoveredFile{
		AudioPath: renamedPath, RelativePath: "renamed.flac",
	}, mismatchedSourceAt.Add(time.Second))
	if !errors.Is(err, probeFailure) {
		t.Fatalf("mismatched archive processing failure=%v", err)
	}
	if err := transaction.QueryRow(ctx, `SELECT status FROM tracks WHERE id=$1`, trackID).Scan(&failedTrackStatus); err != nil {
		t.Fatal(err)
	}
	if failedTrackStatus != "ARCHIVED" {
		t.Fatalf("timestamp-mismatched archived track status=%s", failedTrackStatus)
	}
}

type syncStoredObject struct {
	sizeBytes int64
	checksum  string
}

type syncStorageStub struct {
	uploads []string
	objects map[string]syncStoredObject
}

func (stub *syncStorageStub) UploadFile(_ context.Context, objectKey, path, _ string, checksum string) (int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != checksum {
		return 0, errors.New("storage checksum mismatch")
	}
	stub.uploads = append(stub.uploads, objectKey)
	if stub.objects == nil {
		stub.objects = make(map[string]syncStoredObject)
	}
	stub.objects[objectKey] = syncStoredObject{sizeBytes: int64(len(content)), checksum: checksum}
	return int64(len(content)), nil
}

func (stub *syncStorageStub) StatObject(
	_ context.Context,
	objectKey string,
) (sizeBytes int64, checksumSHA256 string, exists bool, err error) {
	object, exists := stub.objects[objectKey]
	return object.sizeBytes, object.checksum, exists, nil
}

type metadataProbeStub struct {
	metadata adminmetadata.MetadataSnapshot
}

func (stub metadataProbeStub) Probe(context.Context, string) (adminmetadata.ProbedMetadataFile, error) {
	return adminmetadata.ProbedMetadataFile{Metadata: stub.metadata}, nil
}

type metadataProbeFailureStub struct{ err error }

func (stub metadataProbeFailureStub) Probe(context.Context, string) (adminmetadata.ProbedMetadataFile, error) {
	return adminmetadata.ProbedMetadataFile{}, stub.err
}

func stringPointer(value string) *string { return &value }
func intPointer(value int) *int          { return &value }

func findRootView(views []RootView, rootID string) (RootView, bool) {
	for _, view := range views {
		if view.Root.ID == rootID {
			return view, true
		}
	}
	return RootView{}, false
}
