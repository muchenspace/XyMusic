package adminmedia

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
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/shared/apperror"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
	"xymusic/server/internal/testsupport"
)

// TestRepositoryProductionLifecycle is opt-in because it writes isolated,
// self-cleaning rows. It validates the transaction-heavy upload completion
// and retry SQL against the real migrated PostgreSQL schema.
func TestRepositoryProductionLifecycle(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production admin media repository lifecycle")
	}
	testsupport.RequireWriteIntegration(t)
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absoluteEnvironmentPath))
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

	userID := uuid.NewString()
	artistID := uuid.NewString()
	albumID := uuid.NewString()
	trackID := uuid.NewString()
	artworkUploadID := uuid.NewString()
	artworkAssetID := uuid.NewString()
	trackUploadID := uuid.NewString()
	trackAssetID := uuid.NewString()
	abandonedUploadID := uuid.NewString()
	fencedUploadID := uuid.NewString()
	fencedAssetID := uuid.NewString()
	jobID := uuid.NewString()
	username := "adminmedia_it_" + strings.ReplaceAll(userID[:12], "-", "")
	objectKeys := []string{
		"uploads/" + userID + "/" + artworkUploadID,
		"media/artwork/artist_artwork/" + artistID + "/" + artworkUploadID + ".jpg",
		"uploads/" + userID + "/" + trackUploadID,
		"uploads/" + userID + "/" + abandonedUploadID,
		"uploads/" + userID + "/" + fencedUploadID,
		"media/artwork/album_artwork/" + albumID + "/" + fencedUploadID + ".jpg",
		"media/artwork/must-not-clean-" + artworkUploadID + ".jpg",
	}
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		statements := []struct {
			name string
			sql  string
			args []any
		}{
			{"idempotency records", `delete from idempotency_records where actor_id = $1`, []any{userID}},
			{"audit logs", `delete from audit_logs where actor_id = $1`, []any{userID}},
			{"cleanup jobs", `delete from object_cleanup_jobs where object_key = any($1::varchar[])`, []any{objectKeys}},
			{"artist reference", `update artists set artwork_asset_id = null where id = $1`, []any{artistID}},
			{"uploads", `delete from media_uploads where id = any($1::uuid[])`, []any{[]string{artworkUploadID, trackUploadID, abandonedUploadID, fencedUploadID}}},
			{"jobs", `delete from media_jobs where id = $1`, []any{jobID}},
			{"assets", `delete from media_assets where id = any($1::uuid[])`, []any{[]string{artworkAssetID, trackAssetID, fencedAssetID}}},
			{"track", `delete from tracks where id = $1`, []any{trackID}},
			{"album", `delete from albums where id = $1`, []any{albumID}},
			{"artist", `delete from artists where id = $1`, []any{artistID}},
			{"profile", `delete from user_profiles where user_id = $1`, []any{userID}},
			{"user", `delete from users where id = $1`, []any{userID}},
		}
		for _, statement := range statements {
			if _, err := pool.Exec(cleanupCtx, statement.sql, statement.args...); err != nil {
				t.Errorf("clean admin media integration %s: %v", statement.name, err)
			}
		}
		var remaining int
		if err := pool.QueryRow(cleanupCtx, `select count(*)::int from users where id = $1`, userID).Scan(&remaining); err != nil {
			t.Errorf("verify admin media integration cleanup: %v", err)
		} else if remaining != 0 {
			t.Errorf("admin media integration user %s was not deleted", userID)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	passwordHash, err := platformsecurity.HashPassword("adminmedia-integration-" + userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into users (id, username, normalized_username, password_hash, role, status)
		values ($1, $2, $2, $3, 'ADMIN', 'ACTIVE')`, userID, username, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into user_profiles (user_id, display_name) values ($1, 'Admin Media Integration')`, userID); err != nil {
		t.Fatal(err)
	}
	payloadCipher, err := platformsecurity.NewPayloadCipher(cfg.Security.IdempotencyEncryptionSecret)
	if err != nil {
		t.Fatal(err)
	}
	persistentIdempotency := NewPersistentIdempotency(sharedidempotency.New(pool.Pool, payloadCipher))
	idempotentCalls := 0
	idempotencyInput := IdempotencyInput{
		ActorID: userID,
		Scope:   "admin.media.integration.create",
		Key:     "adminmedia-it-" + artworkUploadID,
		Payload: map[string]any{"purpose": PurposeArtistArtwork, "targetId": artistID},
	}
	operation := func() (IdempotencyResponse, error) {
		idempotentCalls++
		return IdempotencyResponse{Status: 201, Body: json.RawMessage(`{"id":"upload"}`)}, nil
	}
	first, err := persistentIdempotency.Execute(ctx, idempotencyInput, operation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := persistentIdempotency.Execute(ctx, idempotencyInput, operation)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || !second.Replayed || idempotentCalls != 1 || string(second.Body) != `{"id":"upload"}` {
		t.Fatalf("persistent idempotency = first %#v second %#v calls=%d", first, second, idempotentCalls)
	}
	changedInput := idempotencyInput
	changedInput.Payload = map[string]any{"purpose": PurposeAlbumArtwork, "targetId": albumID}
	if _, err := persistentIdempotency.Execute(ctx, changedInput, operation); !apperror.IsCode(err, apperror.CodeIdempotencyKeyReused) {
		t.Fatalf("reused idempotency key error = %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into artists (id, name, normalized_name) values ($1, 'Admin Media Artist', $2)`, artistID, "admin media artist "+artistID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into albums (id, title, normalized_title) values ($1, 'Admin Media Album', $2)`, albumID, "admin media album "+albumID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into tracks (id, album_id, title, normalized_title, status)
		values ($1, $2, 'Admin Media Track', $3, 'READY')`, trackID, albumID, "admin media track "+trackID); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	now := time.Now().UTC().Truncate(time.Millisecond)
	artworkUpload, err := repository.CreateUpload(ctx, CreateUploadParams{
		ID: artworkUploadID, ActorID: userID, Purpose: PurposeArtistArtwork,
		TargetID: artistID, ObjectKey: objectKeys[0], FileName: "art.png",
		ContentType: "image/png", SizeBytes: 128, ChecksumSHA256: stringOf('a', 64),
		ExpiresAt: now.Add(5 * time.Minute), Now: now, MaximumBytes: cfg.Storage.MaxUploadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := repository.ClaimCompletion(ctx, userID, artworkUpload.ID, uuid.NewString(), now, completionLease)
	if err != nil || claim.Outcome != CompletionClaimed {
		t.Fatalf("artwork claim = %#v error=%v", claim, err)
	}
	completedArtwork, err := repository.FinalizeCompletion(ctx, FinalizeCompletionParams{
		ActorID: userID, TraceID: "adminmedia-integration-artwork", UploadID: artworkUpload.ID,
		CompletionToken: claim.Token, AssetID: artworkAssetID, JobID: uuid.NewString(),
		Inspected: InspectedUpload{
			ObjectKey: objectKeys[1], MIMEType: "image/jpeg", SizeBytes: 96,
			ChecksumSHA256: stringOf('b', 64), CleanupKeys: []string{objectKeys[0], objectKeys[1]},
		},
		Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if completedArtwork.JobID != nil || completedArtwork.AssetID != artworkAssetID {
		t.Fatalf("completed artwork = %#v", completedArtwork)
	}
	var attachedArtwork string
	if err := pool.QueryRow(ctx, `select artwork_asset_id from artists where id = $1`, artistID).Scan(&attachedArtwork); err != nil || attachedArtwork != artworkAssetID {
		t.Fatalf("artist artwork = %q error=%v", attachedArtwork, err)
	}
	if err := repository.FailCompletion(
		ctx,
		artworkUpload.ID,
		claim.Token,
		false,
		[]string{objectKeys[6]},
		"STALE_COMPLETION_CLEANUP",
		now.Add(1500*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	var staleCleanupCount int
	if err := pool.QueryRow(ctx, `select count(*)::int from object_cleanup_jobs where object_key = $1`, objectKeys[6]).Scan(&staleCleanupCount); err != nil {
		t.Fatal(err)
	}
	if staleCleanupCount != 0 {
		t.Fatalf("stale completion cleanup jobs = %d", staleCleanupCount)
	}

	abandonedUpload, err := repository.CreateUpload(ctx, CreateUploadParams{
		ID: abandonedUploadID, ActorID: userID, Purpose: PurposeAlbumArtwork,
		TargetID: albumID, ObjectKey: objectKeys[3], FileName: "abandoned.png",
		ContentType: "image/png", SizeBytes: 64, ChecksumSHA256: stringOf('d', 64),
		ExpiresAt: now.Add(5 * time.Minute), Now: now, MaximumBytes: cfg.Storage.MaxUploadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AbandonUpload(ctx, userID, abandonedUpload.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	var abandonedStatus string
	var abandonedCleanupCount int
	if err := pool.QueryRow(ctx, `select status::text from media_uploads where id = $1`, abandonedUpload.ID).Scan(&abandonedStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from object_cleanup_jobs where object_key = $1`, abandonedUpload.ObjectKey).Scan(&abandonedCleanupCount); err != nil {
		t.Fatal(err)
	}
	if abandonedStatus != UploadStatusFailed || abandonedCleanupCount != 1 {
		t.Fatalf("abandoned upload status/cleanup = %q / %d", abandonedStatus, abandonedCleanupCount)
	}

	fencedUpload, err := repository.CreateUpload(ctx, CreateUploadParams{
		ID: fencedUploadID, ActorID: userID, Purpose: PurposeAlbumArtwork,
		TargetID: albumID, ObjectKey: objectKeys[4], FileName: "fenced.png",
		ContentType: "image/png", SizeBytes: 64, ChecksumSHA256: stringOf('e', 64),
		ExpiresAt: now.Add(5 * time.Minute), Now: now, MaximumBytes: cfg.Storage.MaxUploadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	fencedClaim, err := repository.ClaimCompletion(ctx, userID, fencedUpload.ID, uuid.NewString(), now, completionLease)
	if err != nil || fencedClaim.Outcome != CompletionClaimed {
		t.Fatalf("fenced claim = %#v error=%v", fencedClaim, err)
	}
	fenceErr := errors.New("integration completion fence lost")
	_, err = repository.FinalizeCompletion(ctx, FinalizeCompletionParams{
		ActorID: userID, TraceID: "adminmedia-integration-fenced", UploadID: fencedUpload.ID,
		CompletionToken: fencedClaim.Token, AssetID: fencedAssetID,
		Inspected: InspectedUpload{
			ObjectKey: objectKeys[5], MIMEType: "image/jpeg", SizeBytes: 48,
			ChecksumSHA256: stringOf('f', 64), CleanupKeys: []string{objectKeys[4], objectKeys[5]},
		},
		CompletionFence: &completionFenceStub{lock: func(_ context.Context, tx pgx.Tx) error {
			if tx == nil {
				t.Fatal("completion fence received a nil transaction")
			}
			return fenceErr
		}},
		Now: now.Add(3 * time.Second),
	})
	if !errors.Is(err, fenceErr) {
		t.Fatalf("fenced completion error = %v", err)
	}
	var fencedAssetCount int
	if err := pool.QueryRow(ctx, `select count(*)::int from media_assets where id = $1`, fencedAssetID).Scan(&fencedAssetCount); err != nil {
		t.Fatal(err)
	}
	if fencedAssetCount != 0 {
		t.Fatalf("fenced completion created %d assets", fencedAssetCount)
	}
	var fencedAlbumCover *string
	if err := pool.QueryRow(ctx, `select cover_asset_id from albums where id = $1`, albumID).Scan(&fencedAlbumCover); err != nil {
		t.Fatal(err)
	}
	if fencedAlbumCover != nil {
		t.Fatalf("fenced completion attached album cover %q", *fencedAlbumCover)
	}
	if err := repository.FailCompletion(
		ctx,
		fencedUpload.ID,
		fencedClaim.Token,
		false,
		[]string{objectKeys[4], objectKeys[5]},
		"BATCH_ATTEMPT_FENCE_LOST",
		now.Add(4*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	var fencedStatus string
	var fencedCleanupCount int
	if err := pool.QueryRow(ctx, `select status::text from media_uploads where id = $1`, fencedUpload.ID).Scan(&fencedStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from object_cleanup_jobs where object_key = any($1::varchar[])`, []string{objectKeys[4], objectKeys[5]}).Scan(&fencedCleanupCount); err != nil {
		t.Fatal(err)
	}
	if fencedStatus != UploadStatusFailed || fencedCleanupCount != 2 {
		t.Fatalf("fenced upload status/cleanup = %q / %d", fencedStatus, fencedCleanupCount)
	}

	trackUpload, err := repository.CreateUpload(ctx, CreateUploadParams{
		ID: trackUploadID, ActorID: userID, Purpose: PurposeTrackSource,
		TargetID: trackID, ObjectKey: objectKeys[2], FileName: "source.flac",
		ContentType: "audio/flac", SizeBytes: 256, ChecksumSHA256: stringOf('c', 64),
		ExpiresAt: now.Add(5 * time.Minute), Now: now, MaximumBytes: cfg.Storage.MaxUploadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	trackClaim, err := repository.ClaimCompletion(ctx, userID, trackUpload.ID, uuid.NewString(), now, completionLease)
	if err != nil || trackClaim.Outcome != CompletionClaimed {
		t.Fatalf("track claim = %#v error=%v", trackClaim, err)
	}
	completedTrack, err := repository.FinalizeCompletion(ctx, FinalizeCompletionParams{
		ActorID: userID, TraceID: "adminmedia-integration-track", UploadID: trackUpload.ID,
		CompletionToken: trackClaim.Token, AssetID: trackAssetID, JobID: jobID,
		Inspected: InspectedUpload{
			ObjectKey: objectKeys[2], MIMEType: "audio/flac", SizeBytes: 256,
			ChecksumSHA256: stringOf('c', 64), CleanupKeys: []string{objectKeys[2]},
		},
		Now: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if completedTrack.JobID == nil || *completedTrack.JobID != jobID {
		t.Fatalf("completed track = %#v", completedTrack)
	}
	job, err := repository.FindJob(ctx, jobID)
	if err != nil || job.Status != JobStatusPending || job.Generation != 1 || job.Version != 1 {
		t.Fatalf("created job = %#v error=%v", job, err)
	}
	if _, err := pool.Exec(ctx, `
		update media_jobs set status = 'FAILED', last_error = 'integration failure', last_error_code = 'MEDIA_UPLOAD_MISMATCH'
		where id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	reason := "integration retry"
	retried, err := repository.RetryJob(ctx, RetryJobParams{
		ActorID: userID, TraceID: "adminmedia-integration-retry", JobID: jobID,
		ExpectedVersion: 1, Reason: &reason, Now: now.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != JobStatusPending || retried.Generation != 2 || retried.Version != 2 || retried.Attempts != 0 {
		t.Fatalf("retried job = %#v", retried)
	}
	completedClaim, err := repository.ClaimCompletion(ctx, userID, trackUpload.ID, uuid.NewString(), now.Add(4*time.Second), completionLease)
	if err != nil || completedClaim.Outcome != CompletionFinished || completedClaim.Upload.JobID == nil {
		t.Fatalf("completed claim = %#v error=%v", completedClaim, err)
	}
}
