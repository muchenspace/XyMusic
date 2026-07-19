package adminmetadata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/testsupport"
)

// TestRepositoryProductionMetadataLifecycle is opt-in because it writes
// isolated, self-cleaning rows to the configured PostgreSQL database.
func TestRepositoryProductionMetadataLifecycle(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production admin metadata lifecycle")
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

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	username := "adminmetadata_it_" + suffix
	passwordHash, err := platformsecurity.HashPassword("adminmetadata-integration-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	originalArtistName := "Metadata Original " + suffix
	updatedArtistName := "Metadata Updated " + suffix
	albumTitle := "Metadata Album " + suffix
	rootPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := "track-" + suffix + ".flac"
	checksum := strings.Repeat("a", 64)

	var actorID, originalArtistID, trackID, rootID, sourceID string
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
		_, _ = pool.Exec(cleanupContext, `delete from albums where normalized_title = $1`, normalizeLookup(albumTitle))
		_, _ = pool.Exec(cleanupContext, `delete from artists where normalized_name = any($1::varchar[])
			and not exists (select 1 from track_artists where artist_id = artists.id)
			and not exists (select 1 from album_artists where artist_id = artists.id)`,
			[]string{normalizeLookup(originalArtistName), normalizeLookup(updatedArtistName)})
		if rootID != "" {
			_, _ = pool.Exec(cleanupContext, `delete from library_roots where id = $1`, rootID)
		}
		_, _ = pool.Exec(cleanupContext, `delete from users where id = $1`, actorID)
	}
	t.Cleanup(cleanup)

	if err := pool.QueryRow(ctx, `
		insert into artists (name, normalized_name) values ($1, $2) returning id::text`,
		originalArtistName, normalizeLookup(originalArtistName)).Scan(&originalArtistID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into tracks (title, normalized_title, duration_ms, status)
		values ($1, $2, 1000, 'READY') returning id::text`,
		"Metadata Track "+suffix, "metadata track "+suffix).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into track_artists (track_id, artist_id, role, sort_order)
		values ($1, $2, 'PRIMARY', 0)`, trackID, originalArtistID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into library_roots (
			name, path, normalized_path, mode, enabled, status, scan_on_startup
		) values ($1, $2, $2, 'READ_WRITE', true, 'READY', false) returning id::text`,
		"Metadata Root "+suffix, rootPath).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into local_music_sources (
			source_path, normalized_source_path, checksum_sha256, size_bytes,
			modified_at, track_id, status, root_id
		) values ($1, $1, $2, 100, now(), $3, 'READY', $4) returning id::text`,
		sourcePath, checksum, trackID, rootID).Scan(&sourceID); err != nil {
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
	if baseline.Version != 1 || baseline.Source == nil || !baseline.Source.CanWriteBack ||
		len(baseline.Effective.Credits) != 1 || baseline.Effective.Credits[0].Name != originalArtistName {
		t.Fatalf("baseline=%+v", baseline)
	}

	updated, err := service.Update(ctx, actorID, "integration:update", trackID, MetadataMutationInput{
		ExpectedVersion: baseline.Version,
		Patch: map[string]any{
			"title":        "Updated Track " + suffix,
			"credits":      []any{map[string]any{"name": updatedArtistName, "role": "PRIMARY"}},
			"albumArtists": []any{updatedArtistName}, "album": albumTitle,
			"releaseDate": "2026-07", "trackNumber": 1, "trackTotal": 9,
			"genres": []any{"Rock"},
			"lyrics": map[string]any{"content": "integration lyric", "format": "PLAIN", "language": "en"},
		},
		Reason: "integration metadata update",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Effective.Album == nil || *updated.Effective.Album != albumTitle ||
		updated.Effective.Lyrics == nil || updated.Effective.Credits[0].Name != updatedArtistName {
		t.Fatalf("updated=%+v", updated)
	}
	var projectedTitle, projectedArtist string
	if err := pool.QueryRow(ctx, `
		select track.title, artist.name from tracks track
		join track_artists credit on credit.track_id = track.id and credit.role = 'PRIMARY'
		join artists artist on artist.id = credit.artist_id
		where track.id = $1`, trackID).Scan(&projectedTitle, &projectedArtist); err != nil {
		t.Fatal(err)
	}
	if projectedTitle != updated.Effective.Title || projectedArtist != updatedArtistName {
		t.Fatalf("projection=%q/%q", projectedTitle, projectedArtist)
	}

	batch, err := service.BatchUpdate(ctx, actorID, "integration:batch", BatchMetadataMutationInput{
		Items: []BatchMutationItem{{TrackID: trackID, ExpectedVersion: updated.Version}},
		Patch: map[string]any{"comment": "batch note"}, Reason: "integration batch update",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || batch.Items[0].Version != 3 {
		t.Fatalf("batch=%+v", batch)
	}
	revisions, err := service.Revisions(ctx, trackID, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Total < 3 || len(revisions.Items) < 3 {
		t.Fatalf("revisions=%+v", revisions)
	}
	baselineRevisionID := revisions.Items[len(revisions.Items)-1].ID
	detail, err := service.Revision(ctx, trackID, baselineRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Action != "BASELINE" || detail.Effective.Credits[0].Name != originalArtistName {
		t.Fatalf("revision=%+v", detail)
	}
	restored, err := service.Restore(
		ctx, actorID, "integration:restore", trackID, baselineRevisionID,
		VersionReasonInput{ExpectedVersion: 3, Reason: "integration restore"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != 4 || restored.Effective.Credits[0].Name != originalArtistName ||
		restored.Effective.Album != nil {
		t.Fatalf("restored=%+v", restored)
	}

	queued, err := service.EnqueueWriteback(
		ctx, actorID, "integration:enqueue", trackID,
		VersionReasonInput{ExpectedVersion: restored.Version, Reason: "integration writeback"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != WritebackPending || queued.Version != 1 {
		t.Fatalf("queued=%+v", queued)
	}
	page, err := service.ListWritebacks(ctx, WritebackListInput{
		Page: 1, PageSize: 10, Status: WritebackPending, TrackID: trackID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != queued.ID {
		t.Fatalf("writeback page=%+v", page)
	}
	cancelled, err := service.CancelWriteback(
		ctx, actorID, "integration:cancel", queued.ID,
		VersionReasonInput{ExpectedVersion: queued.Version, Reason: "integration cancel"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != WritebackCancelled || cancelled.Version != 2 {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	retried, err := service.RetryWriteback(
		ctx, actorID, "integration:retry", queued.ID,
		VersionReasonInput{ExpectedVersion: cancelled.Version, Reason: "integration retry"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != WritebackPending || retried.Version != 3 {
		t.Fatalf("retried=%+v", retried)
	}

	// A final expired lease without a backup becomes terminal. ClaimWriteback
	// must finish reading UPDATE ... RETURNING before it writes the audit row,
	// otherwise pgx reports "conn busy" here.
	deadAttemptID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		update metadata_writeback_jobs set status = 'PROCESSING', attempts = max_attempts,
			attempt_id = $2, stage = 'PREPARING', locked_by = 'dead-worker',
			locked_until = now() - interval '1 minute', next_attempt_at = now() - interval '1 minute'
		where id = $1`, retried.ID, deadAttemptID); err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimWriteback(ctx, "recovery-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("unexpected exhausted claim=%+v", claimed)
	}
	exhausted, err := repository.FindWriteback(ctx, retried.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.Status != WritebackFailed || exhausted.LastErrorCode == nil ||
		*exhausted.LastErrorCode != "WORKER_LEASE_EXPIRED" {
		t.Fatalf("exhausted=%+v", exhausted)
	}

	legacyPending, err := service.RetryWriteback(
		ctx, actorID, "integration:legacy-retry", exhausted.ID,
		VersionReasonInput{ExpectedVersion: exhausted.Version, Reason: "integration legacy retry"},
	)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(rootPath, "legacy-backup-pointer")
	outputChecksum := strings.Repeat("b", 64)
	if _, err := pool.Exec(ctx, `
		update metadata_writeback_jobs set backup_path = $2, backup_expires_at = now(),
			stage = 'PREPARED', output_checksum_sha256 = $3 where id = $1`,
		legacyPending.ID, legacyPath, outputChecksum); err != nil {
		t.Fatal(err)
	}
	cancelledLegacy, err := service.CancelWriteback(
		ctx, actorID, "integration:cancel-legacy", legacyPending.ID,
		VersionReasonInput{ExpectedVersion: legacyPending.Version, Reason: "cancel legacy pointer"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if cancelledLegacy.Status != WritebackCancelled || cancelledLegacy.OutputChecksumSHA256 != nil ||
		cancelledLegacy.Stage != StageQueued {
		t.Fatalf("cancelled legacy writeback=%+v", cancelledLegacy)
	}
	cancelledLegacyRecord, err := repository.FindWriteback(ctx, cancelledLegacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelledLegacyRecord.BackupPath != nil || cancelledLegacyRecord.BackupExpiresAt != nil {
		t.Fatalf("cancelled legacy record=%+v", cancelledLegacyRecord)
	}

	retriedLegacy, err := service.RetryWriteback(
		ctx, actorID, "integration:retry-legacy", cancelledLegacy.ID,
		VersionReasonInput{ExpectedVersion: cancelledLegacy.Version, Reason: "retry without backup"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if retriedLegacy.Status != WritebackPending || retriedLegacy.OutputChecksumSHA256 != nil ||
		retriedLegacy.Stage != StageQueued {
		t.Fatalf("retried legacy writeback=%+v", retriedLegacy)
	}
	retriedLegacyRecord, err := repository.FindWriteback(ctx, retriedLegacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retriedLegacyRecord.BackupPath != nil || retriedLegacyRecord.BackupExpiresAt != nil ||
		retriedLegacyRecord.ExpectedSourceChecksum != checksum {
		t.Fatalf("retried legacy record=%+v", retriedLegacyRecord)
	}
	claim, err := repository.ClaimWriteback(ctx, "normal-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.AttemptID == nil || claim.Attempts != 1 ||
		claim.BackupPath != nil || claim.Stage != StagePreparing {
		t.Fatalf("normal claim=%+v", claim)
	}
	if err := repository.FailWriteback(
		ctx, claim.ID, "normal-worker", *claim.AttemptID,
		errors.New("transient process failure"), time.Time{},
	); err != nil {
		t.Fatal(err)
	}
	pendingAgain, err := repository.FindWriteback(ctx, claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pendingAgain.Status != WritebackPending || pendingAgain.BackupPath != nil ||
		pendingAgain.Stage != StageQueued || pendingAgain.OutputChecksumSHA256 != nil {
		t.Fatalf("pending retry state=%+v", pendingAgain)
	}

	finalAttemptID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		update metadata_writeback_jobs set status = 'PROCESSING', attempts = max_attempts,
			attempt_id = $2, backup_path = $3, stage = 'FILE_REPLACED',
			output_checksum_sha256 = $4, locked_by = 'dead-final-worker',
			locked_until = now() - interval '1 minute', next_attempt_at = now() - interval '1 minute'
		where id = $1`, pendingAgain.ID, finalAttemptID, legacyPath, outputChecksum); err != nil {
		t.Fatal(err)
	}
	finalClaim, err := repository.ClaimWriteback(ctx, "final-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if finalClaim == nil || finalClaim.Status != WritebackProcessing ||
		finalClaim.Attempts != finalClaim.MaxAttempts || finalClaim.AttemptID == nil ||
		*finalClaim.AttemptID != finalAttemptID || finalClaim.Stage != StageFileReplaced ||
		finalClaim.OutputChecksumSHA256 == nil || *finalClaim.OutputChecksumSHA256 != outputChecksum ||
		finalClaim.BackupPath != nil || finalClaim.BackupExpiresAt != nil ||
		finalClaim.LockedBy == nil || *finalClaim.LockedBy != "final-worker" {
		t.Fatalf("exhausted rollback recovery claim=%+v", finalClaim)
	}
}
