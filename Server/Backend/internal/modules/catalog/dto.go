package catalog

type ArtworkDTO struct {
	AssetID   string  `json:"assetId"`
	URL       string  `json:"url"`
	CacheKey  string  `json:"cacheKey"`
	MimeType  string  `json:"mimeType"`
	ExpiresAt *string `json:"expiresAt"`
	Width     *int    `json:"width,omitempty"`
	Height    *int    `json:"height,omitempty"`
}

type ArtistReferenceDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AlbumReferenceDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type TrackSummaryDTO struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Artists     []ArtistReferenceDTO `json:"artists"`
	Album       *AlbumReferenceDTO   `json:"album"`
	Artwork     *ArtworkDTO          `json:"artwork"`
	DurationMS  int64                `json:"durationMs"`
	TrackNumber *int                 `json:"trackNumber"`
	DiscNumber  int                  `json:"discNumber"`
	IsFavorite  bool                 `json:"isFavorite"`
	PublishedAt string               `json:"publishedAt"`
}

type LyricDTO struct {
	ID           string `json:"id"`
	TrackID      string `json:"trackId"`
	Language     string `json:"language"`
	Format       string `json:"format"`
	Content      string `json:"content"`
	IsDefault    bool   `json:"isDefault"`
	TrackVersion int    `json:"trackVersion"`
	UpdatedAt    string `json:"updatedAt"`
}

type TrackDetailDTO struct {
	TrackSummaryDTO
	Lyrics          []LyricDTO `json:"lyrics"`
	LyricPage       int        `json:"lyricPage"`
	LyricPageSize   int        `json:"lyricPageSize"`
	LyricTotal      int        `json:"lyricTotal"`
	LyricTotalPages int        `json:"lyricTotalPages"`
}

type ArtistSummaryDTO struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Artwork *ArtworkDTO `json:"artwork"`
}

type ArtistDetailDTO struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Artwork     *ArtworkDTO `json:"artwork"`
	Description *string     `json:"description"`
}

type AlbumSummaryDTO struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Artists     []ArtistReferenceDTO `json:"artists"`
	Cover       *ArtworkDTO          `json:"cover"`
	ReleaseDate *string              `json:"releaseDate"`
	TrackCount  int                  `json:"trackCount"`
}

type AlbumDetailDTO struct {
	AlbumSummaryDTO
	Description *string `json:"description"`
}

type TrackPageDTO struct {
	Items      []TrackSummaryDTO `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type ArtistPageDTO struct {
	Items      []ArtistSummaryDTO `json:"items"`
	NextCursor *string            `json:"nextCursor"`
}

type AlbumPageDTO struct {
	Items      []AlbumSummaryDTO `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type RandomTracksDTO struct {
	Items []TrackSummaryDTO `json:"items"`
}

type RandomAlbumsDTO struct {
	Items []AlbumSummaryDTO `json:"items"`
}

type SearchResultDTO struct {
	Query   string         `json:"query"`
	Scope   SearchScope    `json:"scope"`
	Tracks  *TrackPageDTO  `json:"tracks,omitempty"`
	Artists *ArtistPageDTO `json:"artists,omitempty"`
	Albums  *AlbumPageDTO  `json:"albums,omitempty"`
}

type ListTracksInput struct {
	Cursor   string
	Limit    *int
	ArtistID string
	AlbumID  string
	Sort     TrackSort
}

type GetTrackInput struct {
	LyricPage     int
	LyricPageSize int
}

type ListArtistsInput struct {
	Cursor string
	Limit  *int
	Sort   ArtistSort
}

type ListAlbumsInput struct {
	Cursor   string
	Limit    *int
	ArtistID string
	Sort     AlbumSort
}

type SearchInput struct {
	Query  string
	Scope  SearchScope
	Cursor string
	Limit  *int
}
