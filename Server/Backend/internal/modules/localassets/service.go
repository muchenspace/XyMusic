package localassets

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Presenter builds artwork URLs and cache keys for other modules.
type Presenter struct{}

func NewPresenter() *Presenter {
	return &Presenter{}
}

func (p *Presenter) PresentArtwork(assetID string, checksum *string, updatedAt time.Time) (string, string, error) {
	if assetID == "" {
		return "", "", errors.New("asset ID is required")
	}
	version := AssetVersion(checksum, updatedAt)
	return fmt.Sprintf("/api/v1/assets/%s/%s", assetID, version), fmt.Sprintf("%s:%s", assetID, version), nil
}

// AssetVersion is the single source of truth for the immutable artwork
// version. The checksum wins when present; otherwise the asset's updated
// timestamp in milliseconds is used. PresentArtwork and asset serving share
// it so the generated URL and the accepted request version cannot drift.
func AssetVersion(checksum *string, updatedAt time.Time) string {
	if checksum != nil && *checksum != "" {
		return *checksum
	}
	return strconv.FormatInt(updatedAt.UnixMilli(), 10)
}

// MatchAssetVersion reports whether a requested URL version addresses the
// current asset state and returns the canonical version for ETag/cache
// headers. A request may use either the checksum or the timestamp fallback,
// matching the version scheme PresentArtwork emits.
func MatchAssetVersion(asset *AssetRecord, requested string) (string, bool) {
	if asset == nil {
		return "", false
	}
	expected := AssetVersion(asset.ChecksumSHA256, asset.UpdatedAt)
	if requested != expected && requested != AssetVersion(nil, asset.UpdatedAt) {
		return "", false
	}
	return expected, true
}
