package localassets

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/platform/localmedia"
	"xymusic/server/internal/shared/apperror"
)

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

	expectedVersion, matched := MatchAssetVersion(asset, version)
	if !matched {
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
