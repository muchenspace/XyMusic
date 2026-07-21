package ossproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const artworkAuthority = "artwork"

var ErrArtworkNotFound = errors.New("artwork resource not found")

type ArtworkResource struct {
	ObjectKey      string
	MimeType       string
	SizeBytes      int64
	ChecksumSHA256 *string
	UpdatedAt      time.Time
}

type ArtworkResolver interface {
	ResolveArtwork(context.Context, string) (ArtworkResource, error)
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type ObjectMetadata struct {
	Size         int64
	LastModified time.Time
}

type ArtworkObjectStore interface {
	OpenForProxy(context.Context, string) (ReadSeekCloser, ObjectMetadata, error)
}

type artworkGateway struct {
	resolver ArtworkResolver
	objects  ArtworkObjectStore
}

func newArtworkGateway(resolver ArtworkResolver, objects ArtworkObjectStore) (*artworkGateway, error) {
	if resolver == nil {
		return nil, errors.New("artwork resolver is required")
	}
	if objects == nil {
		return nil, errors.New("artwork object store is required")
	}
	return &artworkGateway{resolver: resolver, objects: objects}, nil
}

func ArtworkVersion(checksumSHA256 *string, updatedAt time.Time) string {
	if checksumSHA256 != nil && strings.TrimSpace(*checksumSHA256) != "" {
		return strings.ToLower(strings.TrimSpace(*checksumSHA256))
	}
	return strconv.FormatInt(updatedAt.UTC().UnixMilli(), 10)
}

func ArtworkCacheKey(assetID, version string) string {
	return assetID + ":" + version
}

func (proxy *Proxy) ArtworkURL(assetID, version string) (string, error) {
	if proxy == nil || proxy.artworks == nil {
		return "", errors.New("artwork gateway is unavailable")
	}
	if _, err := uuid.Parse(assetID); err != nil {
		return "", errors.New("artwork asset ID must be a UUID")
	}
	if !validArtworkVersion(version) {
		return "", errors.New("artwork version is invalid")
	}
	return Prefix + "/" + artworkAuthority + "/" + url.PathEscape(assetID) + "/" + url.PathEscape(version), nil
}

func (proxy *Proxy) PresentArtwork(
	assetID string,
	checksumSHA256 *string,
	updatedAt time.Time,
) (url string, cacheKey string, err error) {
	version := ArtworkVersion(checksumSHA256, updatedAt)
	resourceURL, err := proxy.ArtworkURL(assetID, version)
	if err != nil {
		return "", "", err
	}
	return resourceURL, ArtworkCacheKey(assetID, version), nil
}

func (gateway *artworkGateway) handle(c *gin.Context, rawPath string) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.AbortWithStatus(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) != 2 {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	assetID, version := parts[0], parts[1]
	if _, err := uuid.Parse(assetID); err != nil || !validArtworkVersion(version) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	resource, err := gateway.resolver.ResolveArtwork(c.Request.Context(), assetID)
	if err != nil {
		if errors.Is(err, ErrArtworkNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if ArtworkVersion(resource.ChecksumSHA256, resource.UpdatedAt) != version {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	object, metadata, err := gateway.objects.OpenForProxy(c.Request.Context(), resource.ObjectKey)
	if err != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	defer object.Close()
	if metadata.Size != resource.SizeBytes {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Type", resource.MimeType)
	c.Header("ETag", `"`+version+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, assetID, metadata.LastModified, object)
}

func validArtworkVersion(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, candidate := range value {
		if (candidate < 'a' || candidate > 'z') && (candidate < '0' || candidate > '9') && candidate != '-' && candidate != '_' {
			return false
		}
	}
	return true
}
