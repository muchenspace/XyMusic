package admincatalog

import "xymusic/server/internal/modules/catalog"

type CreditDTO struct {
	Artist    catalog.ArtistReferenceDTO `json:"artist"`
	Role      string                     `json:"role"`
	SortOrder int                        `json:"sortOrder"`
}

type ArtistDTO struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description *string             `json:"description"`
	Artwork     *catalog.ArtworkDTO `json:"artwork"`
	AlbumCount  int                 `json:"albumCount"`
	TrackCount  int                 `json:"trackCount"`
	Version     int                 `json:"version"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt"`
}

type ArtistPageDTO struct {
	Items      []ArtistDTO `json:"items"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
	Total      int         `json:"total"`
	TotalPages int         `json:"totalPages"`
}

type AlbumDTO struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	ArtistCredits []CreditDTO         `json:"artistCredits"`
	Description   *string             `json:"description"`
	ReleaseDate   *string             `json:"releaseDate"`
	Artwork       *catalog.ArtworkDTO `json:"artwork"`
	TrackCount    int                 `json:"trackCount"`
	Version       int                 `json:"version"`
	CreatedAt     string              `json:"createdAt"`
	UpdatedAt     string              `json:"updatedAt"`
}

type AlbumPageDTO struct {
	Items      []AlbumDTO `json:"items"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
	Total      int        `json:"total"`
	TotalPages int        `json:"totalPages"`
}

type DuplicateAlbumGroupDTO struct {
	Key             string                       `json:"key"`
	Title           string                       `json:"title"`
	PrimaryArtists  []catalog.ArtistReferenceDTO `json:"primaryArtists"`
	Albums          []AlbumDTO                   `json:"albums"`
	AlbumPage       int                          `json:"albumPage"`
	AlbumPageSize   int                          `json:"albumPageSize"`
	AlbumTotal      int                          `json:"albumTotal"`
	AlbumTotalPages int                          `json:"albumTotalPages"`
}

type DuplicateAlbumsDTO struct {
	GroupCount          int                      `json:"groupCount"`
	DuplicateAlbumCount int                      `json:"duplicateAlbumCount"`
	Groups              []DuplicateAlbumGroupDTO `json:"groups"`
	Page                int                      `json:"page"`
	PageSize            int                      `json:"pageSize"`
	Total               int                      `json:"total"`
	TotalPages          int                      `json:"totalPages"`
}

type AlbumReferenceDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type SourceDTO struct {
	ID                   string  `json:"id"`
	RootID               *string `json:"rootId"`
	RootName             *string `json:"rootName"`
	RelativePath         string  `json:"relativePath"`
	Format               *string `json:"format"`
	Status               string  `json:"status"`
	ChecksumSHA256       string  `json:"checksumSha256"`
	Mode                 *string `json:"mode"`
	CanWriteBack         bool    `json:"canWriteBack"`
	WritebackBlockReason *string `json:"writebackBlockReason"`
}

type TrackDTO struct {
	ID                       string              `json:"id"`
	Title                    string              `json:"title"`
	ArtistCredits            []CreditDTO         `json:"artistCredits"`
	Artists                  []string            `json:"artists"`
	Album                    *AlbumReferenceDTO  `json:"album"`
	Artwork                  *catalog.ArtworkDTO `json:"artwork"`
	DurationMS               int64               `json:"durationMs"`
	TrackNumber              *int                `json:"trackNumber"`
	DiscNumber               *int                `json:"discNumber"`
	Status                   string              `json:"status"`
	AudioStatus              AudioStatus         `json:"audioStatus"`
	MetadataStatus           MetadataStatus      `json:"metadataStatus"`
	MetadataVersion          *int                `json:"metadataVersion"`
	Source                   *SourceDTO          `json:"source"`
	ActiveWritebackJobID     *string             `json:"activeWritebackJobId"`
	LatestWritebackErrorCode *string             `json:"latestWritebackErrorCode"`
	LatestWritebackError     *string             `json:"latestWritebackError"`
	PublishedAt              *string             `json:"publishedAt"`
	Version                  int                 `json:"version"`
	CreatedAt                string              `json:"createdAt"`
	UpdatedAt                string              `json:"updatedAt"`
}

type TrackPageDTO struct {
	Items      []TrackDTO `json:"items"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
	Total      int        `json:"total"`
	TotalPages int        `json:"totalPages"`
}

type LyricDTO struct {
	ID        string  `json:"id"`
	Language  string  `json:"language"`
	Format    string  `json:"format"`
	Timing    string  `json:"timing"`
	Content   *string `json:"content"`
	IsDefault bool    `json:"isDefault"`
	Version   int     `json:"version"`
	UpdatedAt string  `json:"updatedAt"`
}

type TrackDetailDTO struct {
	TrackDTO
	Lyrics          []LyricDTO `json:"lyrics"`
	LyricPage       int        `json:"lyricPage"`
	LyricPageSize   int        `json:"lyricPageSize"`
	LyricTotal      int        `json:"lyricTotal"`
	LyricTotalPages int        `json:"lyricTotalPages"`
}

type AlbumDetailDTO struct {
	AlbumDTO
	Tracks          []TrackDTO `json:"tracks"`
	TrackPage       int        `json:"trackPage"`
	TrackPageSize   int        `json:"trackPageSize"`
	TrackTotal      int        `json:"trackTotal"`
	TrackTotalPages int        `json:"trackTotalPages"`
}
