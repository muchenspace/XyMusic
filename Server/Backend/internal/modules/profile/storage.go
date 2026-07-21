package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/ossproxy"
)

// MinIOObjectStorage is the production avatar-upload adapter. Presigned PUT
// URLs bind every header returned to clients; completion still downloads and
// independently hashes the object instead of trusting S3 metadata.
type MinIOObjectStorage struct {
	client *minio.Client
	bucket string
}

var _ AvatarObjectStorage = (*MinIOObjectStorage)(nil)

func NewMinIOObjectStorage(cfg config.Storage) (*MinIOObjectStorage, error) {
	endpoint, secure, err := normalizeStorageEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	bucketLookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		bucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: bucketLookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create profile object storage client: %w", err)
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("profile object storage bucket is required")
	}
	return &MinIOObjectStorage{client: client, bucket: cfg.Bucket}, nil
}

func (storage *MinIOObjectStorage) CreateUploadURL(
	ctx context.Context,
	request UploadURLRequest,
) (string, error) {
	if request.Expires < time.Second || request.Expires > 7*24*time.Hour {
		return "", errors.New("avatar upload URL expiry must be between one second and seven days")
	}
	headers := make(http.Header)
	headers.Set("Content-Type", request.ContentType)
	headers.Set("Content-Length", strconv.FormatInt(request.ContentLength, 10))
	headers.Set("X-Amz-Checksum-Sha256", checksumBase64(request.ChecksumSHA256))
	headers.Set("X-Amz-Meta-Sha256", request.ChecksumSHA256)
	signed, err := storage.client.PresignHeader(
		ctx,
		http.MethodPut,
		storage.bucket,
		request.ObjectKey,
		request.Expires,
		nil,
		headers,
	)
	if err != nil {
		return "", fmt.Errorf("sign avatar upload URL: %w", err)
	}
	clientURL, err := ossproxy.ClientURL(signed.String())
	if err != nil {
		return "", fmt.Errorf("create proxied avatar upload URL: %w", err)
	}
	return clientURL, nil
}

func (storage *MinIOObjectStorage) DownloadToFile(
	ctx context.Context,
	objectKey string,
	destination string,
	maximumBytes int64,
) (StoredObject, error) {
	if maximumBytes < 1 {
		return StoredObject{}, errors.New("avatar download maximum must be positive")
	}
	object, err := storage.client.GetObject(ctx, storage.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return StoredObject{}, fmt.Errorf("open avatar object %q: %w", objectKey, err)
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		return StoredObject{}, fmt.Errorf("inspect avatar object %q: %w", objectKey, err)
	}
	if info.Size < 0 || info.Size > maximumBytes {
		return StoredObject{}, fmt.Errorf("avatar object %q exceeds its permitted size", objectKey)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return StoredObject{}, fmt.Errorf("prepare avatar download directory: %w", err)
	}
	temporary := destination + ".partial"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return StoredObject{}, fmt.Errorf("create avatar download file: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(object, maximumBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > maximumBytes || written != info.Size {
		_ = os.Remove(temporary)
		switch {
		case copyErr != nil:
			return StoredObject{}, fmt.Errorf("download avatar object %q: %w", objectKey, copyErr)
		case closeErr != nil:
			return StoredObject{}, fmt.Errorf("close avatar object %q: %w", objectKey, closeErr)
		case written > maximumBytes:
			return StoredObject{}, fmt.Errorf("avatar object %q exceeds its permitted size", objectKey)
		default:
			return StoredObject{}, fmt.Errorf("avatar object %q size changed while downloading", objectKey)
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return StoredObject{}, fmt.Errorf("commit avatar download: %w", err)
	}
	metadataChecksum := info.UserMetadata["sha256"]
	if metadataChecksum == "" {
		metadataChecksum = info.UserMetadata["Sha256"]
	}
	if metadataChecksum == "" {
		metadataChecksum = info.Metadata.Get("X-Amz-Meta-Sha256")
	}
	return StoredObject{
		SizeBytes:      written,
		ContentType:    info.ContentType,
		ETag:           normalizeETag(info.ETag),
		ChecksumSHA256: hex.EncodeToString(hasher.Sum(nil)),
		MetadataSHA256: strings.ToLower(strings.TrimSpace(metadataChecksum)),
	}, nil
}

func (storage *MinIOObjectStorage) UploadFile(
	ctx context.Context,
	objectKey string,
	path string,
	contentType string,
	checksumSHA256 string,
) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open normalized avatar: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("inspect normalized avatar: %w", err)
	}
	uploaded, err := storage.client.PutObject(
		ctx,
		storage.bucket,
		objectKey,
		file,
		info.Size(),
		minio.PutObjectOptions{
			ContentType:      contentType,
			UserMetadata:     map[string]string{"sha256": checksumSHA256},
			SendContentMd5:   true,
			DisableMultipart: true,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("upload normalized avatar %q: %w", objectKey, err)
	}
	return uploaded.Size, nil
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
