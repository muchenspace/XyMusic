package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/ossproxy"
)

type Client struct {
	client        *minio.Client
	bucket        string
	publicBaseURL string
}

func Open(cfg config.Storage) (*Client, error) {
	endpoint, secure, err := normalizeEndpoint(cfg.Endpoint)
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
		return nil, fmt.Errorf("create object storage client: %w", err)
	}
	return &Client{client: client, bucket: cfg.Bucket, publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/")}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	exists, err := c.client.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("probe object storage bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("object storage bucket %q does not exist", c.bucket)
	}
	return nil
}

func (c *Client) Put(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	_, err := c.client.PutObject(ctx, c.bucket, objectKey, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("upload object %q: %w", objectKey, err)
	}
	return nil
}

func (c *Client) Stat(ctx context.Context, objectKey string) (minio.ObjectInfo, error) {
	info, err := c.client.StatObject(ctx, c.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return minio.ObjectInfo{}, fmt.Errorf("inspect object %q: %w", objectKey, err)
	}
	return info, nil
}

func (c *Client) Delete(ctx context.Context, objectKey string) error {
	if err := c.client.RemoveObject(ctx, c.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %q: %w", objectKey, err)
	}
	return nil
}

func (c *Client) DownloadToFile(ctx context.Context, objectKey, destination string, maximumBytes int64) error {
	if maximumBytes < 1 {
		return errors.New("maximum download size must be positive")
	}
	object, err := c.client.GetObject(ctx, c.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("open object %q: %w", objectKey, err)
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		return fmt.Errorf("inspect object %q: %w", objectKey, err)
	}
	if info.Size > maximumBytes {
		return fmt.Errorf("object %q exceeds the permitted download size", objectKey)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("prepare download directory: %w", err)
	}
	temporary := destination + ".partial"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(object, maximumBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > maximumBytes || (info.Size >= 0 && written != info.Size) {
		_ = os.Remove(temporary)
		switch {
		case copyErr != nil:
			return fmt.Errorf("download object %q: %w", objectKey, copyErr)
		case closeErr != nil:
			return fmt.Errorf("close downloaded object %q: %w", objectKey, closeErr)
		case written > maximumBytes:
			return fmt.Errorf("object %q exceeds the permitted download size", objectKey)
		default:
			return fmt.Errorf("object %q ended before its advertised content length", objectKey)
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit downloaded object: %w", err)
	}
	return nil
}

func (c *Client) PresignedGet(ctx context.Context, objectKey string, expires time.Duration) (string, error) {
	if c.publicBaseURL != "" {
		return c.publicBaseURL + "/" + strings.TrimLeft(objectKey, "/"), nil
	}
	presigned, err := c.client.PresignedGetObject(ctx, c.bucket, objectKey, expires, nil)
	if err != nil {
		return "", fmt.Errorf("sign object URL %q: %w", objectKey, err)
	}
	clientURL, err := ossproxy.ClientURL(presigned.String())
	if err != nil {
		return "", fmt.Errorf("create proxied object URL %q: %w", objectKey, err)
	}
	return clientURL, nil
}

func normalizeEndpoint(raw string) (endpoint string, secure bool, err error) {
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
