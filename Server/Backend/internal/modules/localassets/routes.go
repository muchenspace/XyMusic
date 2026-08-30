package localassets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

type AssetRecord struct {
	ID             string
	StoragePath    string
	Kind           string
	MimeType       string
	SizeBytes      int64
	ChecksumSHA256 *string
	Status         string
	UpdatedAt      time.Time
}

type Store interface {
	FindReadyAsset(ctx context.Context, assetID string) (*AssetRecord, error)
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindReadyAsset(ctx context.Context, assetID string) (*AssetRecord, error) {
	var record AssetRecord
	err := r.pool.QueryRow(ctx, `
		SELECT id, storage_path, kind::text, mime_type, size_bytes, checksum_sha256, status::text, updated_at
		FROM media_assets
		WHERE id = $1 AND status = 'READY'`, assetID).Scan(
		&record.ID,
		&record.StoragePath,
		&record.Kind,
		&record.MimeType,
		&record.SizeBytes,
		&record.ChecksumSHA256,
		&record.Status,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query ready media asset: %w", err)
	}
	return &record, nil
}

type Presenter struct{}

func NewPresenter() *Presenter {
	return &Presenter{}
}

func (p *Presenter) PresentArtwork(assetID string, checksum *string, updatedAt time.Time) (string, string, error) {
	if strings := assetID; strings == "" {
		return "", "", errors.New("asset ID is required")
	}
	version := strconv.FormatInt(updatedAt.UnixMilli(), 10)
	if checksum != nil && *checksum != "" {
		version = *checksum
	}
	url := fmt.Sprintf("/api/v1/assets/%s/%s", assetID, version)
	cacheKey := fmt.Sprintf("%s:%s", assetID, version)
	return url, cacheKey, nil
}

func (p *Presenter) ArtworkURL(assetID, version string) (string, error) {
	if assetID == "" {
		return "", errors.New("asset ID is required")
	}
	if version == "" {
		version = "1"
	}
	return fmt.Sprintf("/api/v1/assets/%s/%s", assetID, version), nil
}

type Routes struct {
	store Store
	media *localmedia.Store
}

func NewRoutes(store Store, media *localmedia.Store) (*Routes, error) {
	if store == nil || media == nil {
		return nil, errors.New("local assets routes require store and media")
	}
	return &Routes{store: store, media: media}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	router.GET("/api/v1/assets/:assetId/:version", httpserver.Handle(routes.serveAsset))
	router.HEAD("/api/v1/assets/:assetId/:version", httpserver.Handle(routes.serveAsset))
}

func (routes *Routes) serveAsset(c *gin.Context) error {
	assetID := c.Param("assetId")
	if _, err := uuid.Parse(assetID); err != nil {
		return apperror.NotFound("Asset was not found")
	}
	version := c.Param("version")
	if version == "" {
		return apperror.NotFound("Asset version is required")
	}

	asset, err := routes.store.FindReadyAsset(c.Request.Context(), assetID)
	if err != nil {
		return err
	}
	if asset == nil {
		return apperror.NotFound("Asset was not found")
	}

	expectedVersion := strconv.FormatInt(asset.UpdatedAt.UnixMilli(), 10)
	if asset.ChecksumSHA256 != nil && *asset.ChecksumSHA256 != "" {
		expectedVersion = *asset.ChecksumSHA256
	}

	if version != expectedVersion && version != strconv.FormatInt(asset.UpdatedAt.UnixMilli(), 10) {
		return apperror.NotFound("Asset version mismatch")
	}

	file, err := routes.media.OpenAsset(asset.StoragePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apperror.NotFound("Asset file was not found")
		}
		return fmt.Errorf("open asset file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat asset file: %w", err)
	}

	etag := fmt.Sprintf(`"%s"`, expectedVersion)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	if asset.MimeType != "" {
		c.Header("Content-Type", asset.MimeType)
	}

	http.ServeContent(c.Writer, c.Request, asset.ID, stat.ModTime(), file)
	return nil
}
