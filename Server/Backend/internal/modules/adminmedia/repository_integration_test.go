package adminmedia

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
	"xymusic/server/internal/testsupport"
)

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

	username := "adminmedia_" + userID[:8]
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `delete from media_uploads where id in ($1, $2)`, artworkUploadID, trackUploadID)
		_, _ = pool.Exec(cleanupCtx, `delete from tracks where id = $1`, trackID)
		_, _ = pool.Exec(cleanupCtx, `delete from albums where id = $1`, albumID)
		_, _ = pool.Exec(cleanupCtx, `delete from artists where id = $1`, artistID)
		_, _ = pool.Exec(cleanupCtx, `delete from media_assets where id in ($1, $2)`, artworkAssetID, trackAssetID)
		_, _ = pool.Exec(cleanupCtx, `delete from users where id = $1`, userID)
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

	payloadCipher, err := platformsecurity.NewPayloadCipher(cfg.Security.IdempotencyEncryptionSecret)
	if err != nil {
		t.Fatal(err)
	}
	persistentIdempotency := NewPersistentIdempotency(sharedidempotency.New(pool.Pool, payloadCipher))
	_ = persistentIdempotency

	repository := NewRepository(pool.Pool)
	now := time.Now().UTC().Truncate(time.Millisecond)

	err = repository.CreateUpload(ctx, UploadReservation{
		ID:                     artworkUploadID,
		Purpose:                PurposeArtistArtwork,
		TargetID:               artistID,
		UploaderID:             userID,
		StoragePath:            "temp/art_" + artworkUploadID + ".partial",
		ExpectedSize:           128,
		ExpectedChecksumSHA256: strings.Repeat("a", 64),
		ExpectedMIMEType:       "image/png",
		OriginalFileName:       "art.png",
		Status:                 UploadStatusCreated,
		ExpiresAt:              now.Add(5 * time.Minute),
		CreatedAt:              now,
	})
	if err != nil {
		t.Fatal(err)
	}

	claim, err := repository.ClaimUploadCompletion(ctx, artworkUploadID, uuid.NewString(), now, completionLease)
	if err != nil || claim.Outcome != CompletionClaimed {
		t.Fatalf("artwork claim = %#v error=%v", claim, err)
	}

	w, h := 800, 800
	err = repository.FinalizeUpload(ctx, FinalizeUploadParams{
		UploadID:        artworkUploadID,
		CompletionToken: claim.Token,
		AssetID:         artworkAssetID,
		Inspected: InspectedMedia{
			StoragePath:    "artworks/" + artworkUploadID + ".jpg",
			Kind:           "ARTWORK",
			MIMEType:       "image/jpeg",
			SizeBytes:      96,
			ChecksumSHA256: strings.Repeat("b", 64),
			Width:          &w,
			Height:         &h,
		},
		Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}

	var attachedArtwork string
	if err := pool.QueryRow(ctx, `select artwork_asset_id from artists where id = $1`, artistID).Scan(&attachedArtwork); err != nil || attachedArtwork != artworkAssetID {
		t.Fatalf("artist artwork = %q error=%v", attachedArtwork, err)
	}

	// Test Track upload
	err = repository.CreateUpload(ctx, UploadReservation{
		ID:                     trackUploadID,
		Purpose:                PurposeTrackSource,
		TargetID:               trackID,
		TrackID:                &trackID,
		UploaderID:             userID,
		StoragePath:            "temp/track_" + trackUploadID + ".partial",
		ExpectedSize:           1024,
		ExpectedChecksumSHA256: strings.Repeat("c", 64),
		ExpectedMIMEType:       "audio/flac",
		OriginalFileName:       "track.flac",
		Status:                 UploadStatusCreated,
		ExpiresAt:              now.Add(5 * time.Minute),
		CreatedAt:              now,
	})
	if err != nil {
		t.Fatal(err)
	}

	trackClaim, err := repository.ClaimUploadCompletion(ctx, trackUploadID, uuid.NewString(), now, completionLease)
	if err != nil || trackClaim.Outcome != CompletionClaimed {
		t.Fatalf("track claim = %#v error=%v", trackClaim, err)
	}

	dur := int64(180000)
	err = repository.FinalizeUpload(ctx, FinalizeUploadParams{
		UploadID:        trackUploadID,
		CompletionToken: trackClaim.Token,
		AssetID:         trackAssetID,
		Inspected: InspectedMedia{
			StoragePath:    "sources/" + trackUploadID + ".flac",
			Kind:           "AUDIO_SOURCE",
			MIMEType:       "audio/flac",
			SizeBytes:      1024,
			ChecksumSHA256: strings.Repeat("c", 64),
			DurationMs:     &dur,
		},
		Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}

	var trackSourceAsset string
	var trackDuration int64
	var publishedAt *time.Time
	if err := pool.QueryRow(ctx, `select source_asset_id, duration_ms, published_at from tracks where id = $1`, trackID).Scan(&trackSourceAsset, &trackDuration, &publishedAt); err != nil || trackSourceAsset != trackAssetID || trackDuration != dur || publishedAt == nil {
		t.Fatalf("track source asset = %q duration = %d publishedAt = %v error=%v", trackSourceAsset, trackDuration, publishedAt, err)
	}
}
