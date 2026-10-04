package profile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/database"
	platformidempotency "xymusic/server/internal/platform/idempotency"
	"xymusic/server/internal/platform/localmedia"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/testsupport"
)

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
	defer pool.Close()

	localMedia, err := localmedia.NewStore(cfg.Paths.MediaAssetDirectory, cfg.MediaStorage.MaxUploadBytes)
	if err != nil {
		t.Fatal(err)
	}

	cipher, err := platformsecurity.NewPayloadCipher(cfg.Security.IdempotencyEncryptionSecret)
	if err != nil {
		t.Fatal(err)
	}
	platformIdempotency := platformidempotency.New(pool.Pool, cipher)
	idempotencyService := NewPersistentIdempotency(platformIdempotency)

	inspector, err := NewFFmpegAvatarInspector(
		localMedia,
		cfg.Media.FFprobePath,
		cfg.Media.FFmpegPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	identityStore := identity.NewRepository(pool.Pool)
	accessTokens := identity.NewAccessTokenService(cfg.Security.AccessTokenSecret, 15*time.Minute)
	identityService, err := identity.NewService(
		cfg.Security.RefreshTokenTTLSeconds,
		cfg.Registration.Enabled,
		identity.ServiceDependencies{
			Repository:   identityStore,
			AccessTokens: accessTokens,
			Idempotency:  identity.NewPersistentRefreshIdempotency(platformIdempotency),
			ArtworkURLs:  &artworkURLStub{},
			Passwords:    identity.SecurityPasswordManager{},
			Secrets:      identity.SecuritySecretHasher{},
			OpaqueToken:  identity.CreateOpaqueToken,
		})
	if err != nil {
		t.Fatal(err)
	}

	profileStore := NewRepository(pool.Pool)
	profileService, err := NewService(
		cfg.MediaStorage.UploadTTLSeconds,
		cfg.MediaStorage.MaxUploadBytes,
		ServiceDependencies{
			Repository:   profileStore,
			CurrentUsers: identityService,
			Idempotency:  idempotencyService,
			LocalMedia:   localMedia,
			Inspector:    inspector,
		})
	if err != nil {
		t.Fatal(err)
	}

	username := "test_" + uuid.NewString()[:8]
	userID := uuid.NewString()
	passwordHash, err := platformsecurity.HashPassword("ValidPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(
		id, username, normalized_username, password_hash, role, status
	) VALUES($1, $2, $2, $3, 'USER', 'ACTIVE')`, userID, username, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(user_id, display_name) VALUES($1, $2)`, userID, username); err != nil {
		t.Fatal(err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for x := 0; x < 100; x++ {
		for y := 0; y < 100; y++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var pngBuffer bytes.Buffer
	if err := png.Encode(&pngBuffer, img); err != nil {
		t.Fatal(err)
	}
	pngBytes := pngBuffer.Bytes()
	hasher := sha256.New()
	hasher.Write(pngBytes)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	uploadReservation, err := profileService.CreateAvatarUpload(ctx, userID, "avatar-key-1", CreateAvatarUploadInput{
		FileName:       "avatar.png",
		ContentType:    "image/png",
		SizeBytes:      int64(len(pngBytes)),
		ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}

	uploadID := uploadReservation.Body.ID
	if err := profileService.UploadDirect(ctx, userID, uploadID, bytes.NewReader(pngBytes), int64(len(pngBytes))); err != nil {
		t.Fatal(err)
	}

	completed, err := profileService.CompleteAvatarUpload(ctx, userID, uploadID, "comp-key-1", CompleteAvatarUploadInput{})
	if err != nil {
		t.Fatal(err)
	}

	if completed.Body.Avatar == nil || completed.Body.Avatar.URL == "" {
		t.Fatalf("expected avatar url in completed user dto: %#v", completed.Body)
	}
}

type artworkURLStub struct{}

func (*artworkURLStub) PresentArtwork(id string, checksum *string, _ time.Time) (string, string, error) {
	v := "1"
	if checksum != nil {
		v = *checksum
	}
	return "/api/v1/assets/" + id + "/" + v, id + ":" + v, nil
}
