package adminmetadata

import (
	"bytes"
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
	platformsecurity "xymusic/server/internal/platform/security"
	platformstorage "xymusic/server/internal/platform/storage"
	"xymusic/server/internal/testsupport"
)

// TestProductionWritebackWorker performs a real file replacement using the
// configured FFmpeg/FFprobe binaries and the configured PostgreSQL schema.
func TestProductionWritebackWorker(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production metadata writeback worker")
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	objects, err := platformstorage.Open(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	username := "metadata_worker_it_" + suffix
	passwordHash, err := platformsecurity.HashPassword("metadata-worker-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	artistName := "Worker Artist " + suffix
	originalTitle := "Worker Original " + suffix
	updatedTitle := "Worker Written " + suffix
	rootPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceRelativePath := "worker-" + suffix + ".flac"
	sourcePath := filepath.Join(rootPath, sourceRelativePath)
	artworkPath := filepath.Join(rootPath, "cover-"+suffix+".jpg")
	runner := OSProcessRunner{}
	generated, err := runner.Run(ctx, cfg.Media.FFmpegPath, []string{
		"-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=0.5",
		"-c:a", "flac", "-metadata", "title=" + originalTitle,
		"-metadata", "artist=" + artistName, sourcePath,
	}, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if generated.TimedOut || generated.ExitCode != 0 {
		t.Fatalf("generate FLAC exit=%d timeout=%v stderr=%s", generated.ExitCode, generated.TimedOut, generated.Stderr)
	}
	generated, err = runner.Run(ctx, cfg.Media.FFmpegPath, []string{
		"-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=64x64",
		"-frames:v", "1", artworkPath,
	}, 30*time.Second)
	if err != nil || generated.TimedOut || generated.ExitCode != 0 {
		t.Fatalf("generate artwork exit=%d timeout=%v error=%v stderr=%s", generated.ExitCode, generated.TimedOut, err, generated.Stderr)
	}
	artworkBytes, err := os.ReadFile(artworkPath)
	if err != nil {
		t.Fatal(err)
	}
	artworkDigest := sha256.Sum256(artworkBytes)
	originalChecksum, err := sha256File(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	var actorID, artistID, albumID, artworkAssetID, trackID, rootID, sourceID string
	artworkObjectKey := "integration/writeback-artwork-" + suffix + ".jpg"
	if err := pool.QueryRow(ctx, `
		insert into users (username, normalized_username, password_hash, role)
		values ($1, $1, $2, 'ADMIN') returning id::text`, username, passwordHash).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `delete from audit_logs where actor_id = $1`, actorID)
		if trackID != "" {
			_, _ = pool.Exec(cleanupContext, `delete from tracks where id = $1`, trackID)
		}
		if albumID != "" {
			_, _ = pool.Exec(cleanupContext, `delete from albums where id = $1`, albumID)
		}
		if artworkAssetID != "" {
			_, _ = pool.Exec(cleanupContext, `delete from media_assets where id = $1`, artworkAssetID)
		}
		_ = objects.Delete(cleanupContext, artworkObjectKey)
		if artistID != "" {
			_, _ = pool.Exec(cleanupContext, `delete from artists where id = $1
				and not exists (select 1 from track_artists where artist_id = artists.id)
				and not exists (select 1 from album_artists where artist_id = artists.id)`, artistID)
		}
		if rootID != "" {
			_, _ = pool.Exec(cleanupContext, `delete from library_roots where id = $1`, rootID)
		}
		_, _ = pool.Exec(cleanupContext, `delete from users where id = $1`, actorID)
	}
	t.Cleanup(cleanup)
	if err := objects.Put(ctx, artworkObjectKey, bytes.NewReader(artworkBytes), int64(len(artworkBytes)), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into media_assets (uploader_id,object_key,kind,mime_type,size_bytes,checksum_sha256,status)
		values ($1,$2,'ARTWORK','image/jpeg',$3,$4,'READY') returning id::text`,
		actorID, artworkObjectKey, len(artworkBytes), hex.EncodeToString(artworkDigest[:])).Scan(&artworkAssetID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into albums (title,normalized_title,cover_asset_id)
		values ($1,$2,$3) returning id::text`,
		"Worker Album "+suffix, normalizeLookup("Worker Album "+suffix), artworkAssetID).Scan(&albumID); err != nil {
		t.Fatal(err)
	}

	if err := pool.QueryRow(ctx, `
		insert into artists (name, normalized_name) values ($1, $2) returning id::text`,
		artistName, normalizeLookup(artistName)).Scan(&artistID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into tracks (title, normalized_title, album_id, duration_ms, status)
		values ($1, $2, $3, 500, 'READY') returning id::text`,
		originalTitle, normalizeLookup(originalTitle), albumID).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into track_artists (track_id, artist_id, role, sort_order)
		values ($1, $2, 'PRIMARY', 0)`, trackID, artistID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into library_roots (
			name, path, normalized_path, mode, enabled, status, scan_on_startup
		) values ($1, $2, $2, 'READ_WRITE', true, 'READY', false) returning id::text`,
		"Worker Root "+suffix, rootPath).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into local_music_sources (
			source_path, normalized_source_path, checksum_sha256, size_bytes,
			modified_at, track_id, status, root_id
		) values ($1, $1, $2, $3, $4, $5, 'READY', $6) returning id::text`,
		sourceRelativePath, originalChecksum, fileInfo.Size(), fileInfo.ModTime(),
		trackID, rootID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into local_music_source_tracks (source_id, track_id, segment_index, start_ms)
		values ($1, $2, 0, 0)`, sourceID, trackID); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := service.Metadata(ctx, trackID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(ctx, actorID, "integration:worker-update", trackID, MetadataMutationInput{
		ExpectedVersion: baseline.Version,
		Patch:           map[string]any{"title": updatedTitle, "genres": []any{"Rock", "Test"}},
		Reason:          "production writeback worker integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.EnqueueWriteback(ctx, actorID, "integration:worker-enqueue", trackID, VersionReasonInput{
		ExpectedVersion: updated.Version, Reason: "production writeback worker integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWritebackWorker(WorkerDependencies{
		Store: repository, FFmpegPath: cfg.Media.FFmpegPath, FFprobePath: cfg.Media.FFprobePath,
		Artwork: objects, Runner: OSProcessRunner{}, Logger: NoopLogger{}, Clock: SystemClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked := false
	for attempt := 0; attempt < 20 && !worked; attempt++ {
		worked, err = worker.RunNext(ctx, "metadata-worker-integration-"+suffix)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !worked {
		var status string
		var nextAttempt, databaseNow time.Time
		var attempts, maxAttempts int
		queryErr := pool.QueryRow(ctx, `select status::text, next_attempt_at, now(), attempts, max_attempts
			from metadata_writeback_jobs where id = $1`, queued.ID).Scan(
			&status, &nextAttempt, &databaseNow, &attempts, &maxAttempts,
		)
		t.Fatalf("writeback worker did not claim the queued job: status=%s next=%s dbNow=%s attempts=%d/%d queryErr=%v",
			status, nextAttempt, databaseNow, attempts, maxAttempts, queryErr)
	}
	job, err := repository.FindWriteback(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != WritebackReady || job.Stage != StageCommitted ||
		job.OutputChecksumSHA256 == nil || job.BackupPath != nil || job.BackupExpiresAt != nil {
		t.Fatalf("completed job=%+v", job)
	}
	outputChecksum, err := sha256File(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if outputChecksum != *job.OutputChecksumSHA256 || outputChecksum == originalChecksum {
		t.Fatalf("output checksum=%s job=%v original=%s", outputChecksum, job.OutputChecksumSHA256, originalChecksum)
	}
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".bak") || strings.Contains(name, ".xymusic-backup-") ||
			strings.Contains(name, ".xymusic-rollback-") || strings.HasSuffix(name, ".tmp") {
			t.Fatalf("writeback left an unexpected file: %s", entry.Name())
		}
	}
	probed, err := ProbeMetadataFile(ctx, sourcePath, cfg.Media.FFprobePath, OSProcessRunner{})
	if err != nil {
		t.Fatal(err)
	}
	expected := updated.Effective
	expected.HasArtwork = true
	if !MetadataSnapshotsEqual(probed.Metadata, expected) {
		t.Fatalf("written metadata=%+v expected=%+v", probed.Metadata, expected)
	}
	stored, err := service.Metadata(ctx, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != updated.Version+1 || stored.Raw.Title != updatedTitle ||
		stored.Source == nil || stored.Source.ChecksumSHA256 != outputChecksum {
		t.Fatalf("stored metadata=%+v", stored)
	}
}
