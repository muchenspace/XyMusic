package adminsettings

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xymusic/server/internal/config"
	"xymusic/server/internal/shared/apperror"
)

type ProductionStorageFactory struct{}

func (ProductionStorageFactory) Open(cfg config.Storage) (StorageProbe, error) {
	endpoint, secure, err := storageEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: secure, Region: cfg.Region, BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create object storage client: %w", err)
	}
	return &productionStorage{client: client, bucket: cfg.Bucket, region: cfg.Region}, nil
}

type productionStorage struct {
	client *minio.Client
	bucket string
	region string
}

func (storage *productionStorage) Probe(ctx context.Context) (bool, error) {
	exists, err := storage.client.BucketExists(ctx, storage.bucket)
	if err != nil {
		return false, storageDependencyError("Object storage endpoint or bucket is unavailable", err)
	}
	return exists, nil
}

func (storage *productionStorage) EnsureBucket(ctx context.Context) error {
	exists, err := storage.client.BucketExists(ctx, storage.bucket)
	if err != nil {
		return storageDependencyError("Object storage endpoint or bucket is unavailable", err)
	}
	if exists {
		return nil
	}
	if err := storage.client.MakeBucket(ctx, storage.bucket, minio.MakeBucketOptions{Region: storage.region}); err != nil {
		if exists, checkErr := storage.client.BucketExists(ctx, storage.bucket); checkErr == nil && exists {
			return nil
		}
		return storageDependencyError("Object storage bucket could not be created", err)
	}
	return nil
}

func (*productionStorage) Close() {}

func storageEndpoint(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "s3.amazonaws.com", true, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false, validation("storage.endpoint is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false, validation("storage.endpoint must not contain a path")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

func storageDependencyError(detail string, cause error) error {
	if cause == nil {
		cause = errors.New(detail)
	}
	return apperror.New(apperror.CodeDependencyUnavailable, detail, apperror.WithCause(cause))
}
