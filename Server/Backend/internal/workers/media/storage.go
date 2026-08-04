package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xymusic/server/internal/config"
)

type MinIOObjectStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIOObjectStorage(cfg config.Storage) (*MinIOObjectStorage, error) {
	endpoint, secure, err := normalizeStorageEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("media worker object storage bucket is required")
	}
	bucketLookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		bucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure, Region: cfg.Region, BucketLookup: bucketLookup, TrailingHeaders: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create media worker object storage: %w", err)
	}
	return &MinIOObjectStorage{client: client, bucket: cfg.Bucket}, nil
}

func (storage *MinIOObjectStorage) Ping(ctx context.Context) error {
	exists, err := storage.client.BucketExists(ctx, storage.bucket)
	if err != nil {
		return fmt.Errorf("probe media worker object storage: %w", err)
	}
	if !exists {
		return fmt.Errorf("media worker object storage bucket %q does not exist", storage.bucket)
	}
	return nil
}

func (storage *MinIOObjectStorage) DownloadToFile(
	ctx context.Context,
	objectKey, destination string,
	maximumBytes int64,
) (DownloadedObject, error) {
	if maximumBytes < 1 {
		return DownloadedObject{}, errors.New("media worker download maximum must be positive")
	}
	object, err := storage.client.GetObject(ctx, storage.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return DownloadedObject{}, fmt.Errorf("open media worker object %q: %w", objectKey, err)
	}
	defer object.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return DownloadedObject{}, fmt.Errorf("prepare media worker download directory: %w", err)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return DownloadedObject{}, fmt.Errorf("create media worker download: %w", err)
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(destination)
		}
	}()
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(object, maximumBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return DownloadedObject{}, fmt.Errorf("download media worker object %q: %w", objectKey, copyErr)
	}
	if closeErr != nil {
		return DownloadedObject{}, fmt.Errorf("close media worker object %q: %w", objectKey, closeErr)
	}
	if written > maximumBytes {
		return DownloadedObject{}, fmt.Errorf("media worker object %q exceeds its permitted size", objectKey)
	}
	completed = true
	return DownloadedObject{
		SizeBytes: written, ChecksumSHA256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func (storage *MinIOObjectStorage) UploadFile(
	ctx context.Context,
	objectKey, path, contentType, checksumSHA256 string,
) (int64, error) {
	expectedChecksum, err := decodeSHA256Hex(checksumSHA256)
	if err != nil {
		return 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open media worker upload: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("inspect media worker upload: %w", err)
	}
	uploaded, err := storage.client.PutObject(
		ctx, storage.bucket, objectKey, file, info.Size(),
		minio.PutObjectOptions{
			ContentType: contentType, UserMetadata: map[string]string{"sha256": strings.ToLower(checksumSHA256)},
			DisableMultipart: true, Checksum: minio.ChecksumSHA256,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("upload media worker object %q: %w", objectKey, err)
	}
	if uploaded.Size != info.Size() {
		_ = storage.Delete(context.WithoutCancel(ctx), objectKey)
		return 0, fmt.Errorf("uploaded media worker object %q has the wrong size", objectKey)
	}
	if uploaded.ChecksumSHA256 != "" {
		observedChecksum, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(uploaded.ChecksumSHA256))
		if decodeErr != nil || !bytes.Equal(observedChecksum, expectedChecksum) {
			_ = storage.Delete(context.WithoutCancel(ctx), objectKey)
			return 0, fmt.Errorf("uploaded media worker object %q failed checksum validation", objectKey)
		}
		return uploaded.Size, nil
	}
	stored, err := storage.client.StatObject(ctx, storage.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("validate media worker object %q: %w", objectKey, err)
	}
	metadataChecksum := stored.UserMetadata["sha256"]
	if metadataChecksum == "" {
		metadataChecksum = stored.UserMetadata["Sha256"]
	}
	if stored.Size != info.Size() || !strings.EqualFold(metadataChecksum, checksumSHA256) {
		_ = storage.Delete(context.WithoutCancel(ctx), objectKey)
		return 0, fmt.Errorf("uploaded media worker object %q failed validation", objectKey)
	}
	return uploaded.Size, nil
}

func decodeSHA256Hex(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("media worker upload checksum must be a SHA-256 hex digest")
	}
	return decoded, nil
}

func (storage *MinIOObjectStorage) StatObject(
	ctx context.Context, objectKey string,
) (sizeBytes int64, checksumSHA256 string, exists bool, err error) {
	info, err := storage.client.StatObject(ctx, storage.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.StatusCode == http.StatusNotFound || response.Code == "NoSuchKey" ||
			response.Code == "NoSuchObject" || response.Code == "NotFound" {
			return 0, "", false, nil
		}
		return 0, "", false, fmt.Errorf("inspect media worker object %q: %w", objectKey, err)
	}
	checksum := info.UserMetadata["sha256"]
	if checksum == "" {
		checksum = info.UserMetadata["Sha256"]
	}
	if checksum == "" {
		checksum = info.Metadata.Get("X-Amz-Meta-Sha256")
	}
	return info.Size, strings.ToLower(strings.TrimSpace(checksum)), true, nil
}

func (storage *MinIOObjectStorage) Delete(ctx context.Context, objectKey string) error {
	if err := storage.client.RemoveObject(ctx, storage.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete media worker object %q: %w", objectKey, err)
	}
	return nil
}

func normalizeStorageEndpoint(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "s3.amazonaws.com", true, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false, errors.New("S3_ENDPOINT must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false, errors.New("S3_ENDPOINT must not contain a path")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}
