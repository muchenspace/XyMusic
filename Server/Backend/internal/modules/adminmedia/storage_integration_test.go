package adminmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	platformstorage "xymusic/server/internal/platform/storage"
	"xymusic/server/internal/testsupport"
)

// TestProductionStorageRoundTrip is opt-in and writes only UUID-scoped,
// self-cleaning objects. It proves the exact signed headers and independent
// completion checksum used by the admin media adapter against real MinIO.
func TestProductionStorageRoundTrip(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run the production admin media storage round trip")
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	storage, err := NewMinIOObjectStorage(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := platformstorage.Open(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "integration/adminmedia/" + uuid.NewString()
	keys := []string{prefix + "/presigned.png", prefix + "/server.png"}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, key := range keys {
			if err := cleanup.Delete(cleanupCtx, key); err != nil {
				t.Errorf("delete integration object %q: %v", key, err)
			}
		}
	})
	payload := testPNG(t)
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])

	url, err := storage.CreateUploadURL(ctx, UploadURLRequest{
		ObjectKey: keys[0], ContentType: "image/png", ContentLength: int64(len(payload)),
		ChecksumSHA256: checksum, Expires: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = int64(len(payload))
	request.Header.Set("Content-Type", "image/png")
	request.Header.Set("Content-Length", strconv.Itoa(len(payload)))
	request.Header.Set("X-Amz-Checksum-Sha256", checksumBase64(checksum))
	request.Header.Set("X-Amz-Meta-Sha256", checksum)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("presigned upload returned %d", response.StatusCode)
	}

	directory := t.TempDir()
	presignedPath := filepath.Join(directory, "presigned.png")
	observed, err := storage.DownloadToFile(ctx, keys[0], presignedPath, int64(len(payload))+1)
	if err != nil {
		t.Fatal(err)
	}
	if observed.SizeBytes != int64(len(payload)) || observed.ContentType != "image/png" ||
		observed.ChecksumSHA256 != checksum || observed.MetadataSHA256 != checksum {
		t.Fatalf("presigned observed = %#v", observed)
	}

	serverPath := filepath.Join(directory, "server.png")
	if err := os.WriteFile(serverPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	storedSize, err := storage.UploadFile(ctx, keys[1], serverPath, "image/png", checksum)
	if err != nil {
		t.Fatal(err)
	}
	if storedSize != int64(len(payload)) {
		t.Fatalf("server upload size = %d", storedSize)
	}
	serverObserved, err := storage.DownloadToFile(
		ctx,
		keys[1],
		filepath.Join(directory, "server-downloaded.png"),
		int64(len(payload))+1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if serverObserved.ChecksumSHA256 != checksum || serverObserved.MetadataSHA256 != checksum {
		t.Fatalf("server observed = %#v", serverObserved)
	}
}
