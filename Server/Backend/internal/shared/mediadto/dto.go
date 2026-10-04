// Package mediadto holds artwork and album reference DTOs shared by catalog,
// identity, playlist and admin modules. JSON tags are part of the API v1
// contract and must not change.
package mediadto

type ArtworkDTO struct {
	AssetID  string `json:"assetId"`
	URL      string `json:"url"`
	CacheKey string `json:"cacheKey"`
	MimeType string `json:"mimeType"`
	// ExpiresAt remains in API v1 for client compatibility. Stable artwork resources set it to null.
	ExpiresAt *string `json:"expiresAt"`
	Width     *int    `json:"width,omitempty"`
	Height    *int    `json:"height,omitempty"`
}

type AlbumReferenceDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
