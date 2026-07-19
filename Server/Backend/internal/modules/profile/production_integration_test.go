package profile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	platformstorage "xymusic/server/internal/platform/storage"
	"xymusic/server/internal/shared/apperror"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
	"xymusic/server/internal/testsupport"
)

// TestProfileProductionAvatarLifecycle is intentionally opt-in because it
// writes isolated, self-cleaning rows and objects to the configured production
// dependencies. It proves the real PostgreSQL, MinIO and FFmpeg path rather
// than substituting an in-memory implementation.
func TestProfileProductionAvatarLifecycle(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production profile lifecycle")
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	objects, err := NewMinIOObjectStorage(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	cleanupObjects, err := platformstorage.Open(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanupProfileIntegrationRows(ctx, pool, cleanupObjects, ""); err != nil {
		t.Fatalf("clean stale profile integration rows: %v", err)
	}

	userID := uuid.NewString()
	uploadID := uuid.NewString()
	assetID := uuid.NewString()
	username := "profile_it_" + strings.ReplaceAll(userID[:12], "-", "")
	objectKey := "uploads/" + userID + "/" + uploadID
	cleanup := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := cleanupProfileIntegrationRows(cleanupContext, pool, cleanupObjects, userID); err != nil {
			t.Errorf("clean profile production integration state: %v", err)
		}
	}
	t.Cleanup(cleanup)
	passwordHash, err := platformsecurity.HashPassword("profile-integration-" + userID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		insert into users (
			id, username, normalized_username, password_hash, role, status,
			auth_version, version
		) values ($1, $2, $2, $3, 'USER', 'ACTIVE', 1, 1)`,
		userID, username, passwordHash,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into user_profiles (user_id, display_name)
		values ($1, 'Profile Integration')`, userID); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool.Pool)
	now := time.Now().UTC().Truncate(time.Millisecond)
	bio := "real PostgreSQL profile update"
	if err := repository.UpdateProfile(ctx, userID, 1, ProfileChanges{
		DisplayNameSet: true,
		DisplayName:    "Profile Integration Updated",
		BioSet:         true,
		Bio:            &bio,
	}, now); err != nil {
		t.Fatal(err)
	}
	conflict := repository.UpdateProfile(ctx, userID, 1, ProfileChanges{
		DisplayNameSet: true,
		DisplayName:    "Stale Update",
	}, now)
	if !apperror.IsCode(conflict, apperror.CodeVersionConflict) {
		t.Fatalf("stale expectedVersion error = %v", conflict)
	}
	payloadCipher, err := platformsecurity.NewPayloadCipher(cfg.Security.IdempotencyEncryptionSecret)
	if err != nil {
		t.Fatal(err)
	}
	persistentIdempotency := NewPersistentIdempotency(sharedidempotency.New(pool.Pool, payloadCipher))
	idempotentCalls := 0
	idempotencyInput := IdempotencyInput{
		ActorID: userID,
		Scope:   "profile.integration.update",
		Key:     "profile-it-" + uploadID,
		Payload: map[string]any{"expectedVersion": 2, "displayName": "Replay"},
	}
	operation := func() (identity.CurrentUserDTO, error) {
		idempotentCalls++
		return identity.CurrentUserDTO{ID: userID, Username: username, DisplayName: "Replay", Version: 2}, nil
	}
	firstReplay, err := persistentIdempotency.ExecuteCurrentUser(ctx, idempotencyInput, http.StatusOK, operation)
	if err != nil {
		t.Fatal(err)
	}
	secondReplay, err := persistentIdempotency.ExecuteCurrentUser(ctx, idempotencyInput, http.StatusOK, operation)
	if err != nil {
		t.Fatal(err)
	}
	if firstReplay.Replayed || !secondReplay.Replayed || idempotentCalls != 1 || secondReplay.Body.ID != userID {
		t.Fatalf("idempotency replay = first %#v second %#v calls=%d", firstReplay, secondReplay, idempotentCalls)
	}
	changedPayload := idempotencyInput
	changedPayload.Payload = map[string]any{"expectedVersion": 2, "displayName": "Different"}
	if _, err := persistentIdempotency.ExecuteCurrentUser(ctx, changedPayload, http.StatusOK, operation); !apperror.IsCode(err, apperror.CodeIdempotencyKeyReused) {
		t.Fatalf("reused idempotency key error = %v", err)
	}

	payload := integrationPNG(t)
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	_, err = repository.CreateAvatarUpload(ctx, CreateUploadParams{
		ID:             uploadID,
		ActorID:        userID,
		TraceID:        "profile-integration-trace",
		ObjectKey:      objectKey,
		FileName:       "avatar.png",
		ContentType:    "image/png",
		SizeBytes:      int64(len(payload)),
		ChecksumSHA256: checksum,
		ExpiresAt:      now.Add(5 * time.Minute),
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploadURL, err := objects.CreateUploadURL(ctx, UploadURLRequest{
		ObjectKey:      objectKey,
		ContentType:    "image/png",
		ContentLength:  int64(len(payload)),
		ChecksumSHA256: checksum,
		Expires:        5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = int64(len(payload))
	request.Header.Set("Content-Type", "image/png")
	request.Header.Set("Content-Length", strconv.FormatInt(int64(len(payload)), 10))
	request.Header.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(digest[:]))
	request.Header.Set("X-Amz-Meta-Sha256", checksum)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("presigned avatar PUT returned %d", response.StatusCode)
	}

	claim, err := repository.ClaimAvatarCompletion(
		ctx,
		userID,
		uploadID,
		uuid.NewString(),
		time.Now().UTC(),
		completionLease,
	)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Outcome != CompletionClaimed {
		t.Fatalf("completion claim = %#v", claim)
	}
	inspector, err := NewFFmpegAvatarInspector(objects, cfg.Media.FFprobePath, cfg.Media.FFmpegPath)
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := inspector.Inspect(ctx, claim.Upload, "")
	if err != nil {
		t.Fatal(err)
	}
	if inspected.MIMEType != "image/jpeg" || inspected.Width < 1 || inspected.Height < 1 {
		t.Fatalf("inspected avatar = %#v", inspected)
	}
	if err := repository.FinalizeAvatarCompletion(ctx, FinalizeAvatarParams{
		ActorID:         userID,
		TraceID:         "profile-integration-complete",
		UploadID:        uploadID,
		CompletionToken: claim.Token,
		AssetID:         assetID,
		Inspected:       inspected,
		Now:             time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var version int
	var avatarAssetID string
	var uploadStatus string
	if err := pool.QueryRow(ctx, `
		select u.version, p.avatar_asset_id, mu.status::text
		from users u
		join user_profiles p on p.user_id = u.id
		join media_uploads mu on mu.id = $2
		where u.id = $1`, userID, uploadID).Scan(&version, &avatarAssetID, &uploadStatus); err != nil {
		t.Fatal(err)
	}
	if version != 3 || avatarAssetID != assetID || uploadStatus != UploadStatusCompleted {
		t.Fatalf("completed state = version %d avatar %q upload %q", version, avatarAssetID, uploadStatus)
	}
}

func cleanupProfileIntegrationRows(
	ctx context.Context,
	pool *database.Pool,
	objects *platformstorage.Client,
	userID string,
) error {
	query := `select id from users where normalized_username like 'profile_it_%'`
	arguments := []any{}
	if userID != "" {
		query = `select id from users where id = $1`
		arguments = append(arguments, userID)
	}
	rows, err := pool.Query(ctx, query, arguments...)
	if err != nil {
		return err
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var cleanupErrors []error
	for _, id := range userIDs {
		objectRows, queryErr := pool.Query(ctx, `
			select object_key from media_uploads where uploader_id = $1
			union
			select 'media/artwork/user_avatar/' || target_id::text || '/' || id::text || '.jpg'
			from media_uploads where uploader_id = $1 and purpose = 'USER_AVATAR'
			union
			select object_key from media_assets where uploader_id = $1`, id)
		if queryErr != nil {
			cleanupErrors = append(cleanupErrors, queryErr)
			continue
		}
		var objectKeys []string
		for objectRows.Next() {
			var key string
			if scanErr := objectRows.Scan(&key); scanErr != nil {
				cleanupErrors = append(cleanupErrors, scanErr)
				break
			}
			objectKeys = append(objectKeys, key)
		}
		if rowsErr := objectRows.Err(); rowsErr != nil {
			cleanupErrors = append(cleanupErrors, rowsErr)
		}
		objectRows.Close()
		for _, key := range objectKeys {
			if deleteErr := objects.Delete(ctx, key); deleteErr != nil {
				cleanupErrors = append(cleanupErrors, deleteErr)
			}
		}
		statements := []struct {
			name string
			sql  string
			args []any
		}{
			{"idempotency records", `delete from idempotency_records where actor_id = $1`, []any{id}},
			{"audit logs", `delete from audit_logs where actor_id = $1`, []any{id}},
			{"cleanup jobs", `delete from object_cleanup_jobs where object_key = any($1::varchar[])`, []any{objectKeys}},
			{"profile", `delete from user_profiles where user_id = $1`, []any{id}},
			{"uploads", `delete from media_uploads where uploader_id = $1`, []any{id}},
			{"assets", `delete from media_assets where uploader_id = $1`, []any{id}},
			{"user", `delete from users where id = $1`, []any{id}},
		}
		for _, statement := range statements {
			if _, executionErr := pool.Exec(ctx, statement.sql, statement.args...); executionErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("delete %s: %w", statement.name, executionErr))
			}
		}
		var remaining int
		if verificationErr := pool.QueryRow(ctx, `select count(*)::int from users where id = $1`, id).Scan(&remaining); verificationErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("verify integration user cleanup: %w", verificationErr))
		} else if remaining != 0 {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("integration user %s was not deleted", id))
		}
	}
	return errors.Join(cleanupErrors...)
}

func integrationPNG(t *testing.T) []byte {
	t.Helper()
	imageValue := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < imageValue.Bounds().Dy(); y++ {
		for x := 0; x < imageValue.Bounds().Dx(); x++ {
			imageValue.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 9), B: 160, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, imageValue); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
