package catalog

import "time"

type TrackSort string

const (
	TrackSortPublishedDesc TrackSort = "PUBLISHED_DESC"
	TrackSortTitleAsc      TrackSort = "TITLE_ASC"
	TrackSortTitleDesc     TrackSort = "TITLE_DESC"
	TrackSortAlbumOrderAsc TrackSort = "ALBUM_ORDER_ASC"
)

type ArtistSort string

const (
	ArtistSortNameAsc  ArtistSort = "NAME_ASC"
	ArtistSortNameDesc ArtistSort = "NAME_DESC"
)

type AlbumSort string

const (
	AlbumSortReleaseDateDesc AlbumSort = "RELEASE_DATE_DESC"
	AlbumSortTitleAsc        AlbumSort = "TITLE_ASC"
	AlbumSortTitleDesc       AlbumSort = "TITLE_DESC"
)

type SearchScope string

const (
	SearchScopeAll     SearchScope = "ALL"
	SearchScopeTracks  SearchScope = "TRACKS"
	SearchScopeArtists SearchScope = "ARTISTS"
	SearchScopeAlbums  SearchScope = "ALBUMS"
)

type ArtistReferenceRecord struct {
	ID   string
	Name string
}

type AlbumReferenceRecord struct {
	ID    string
	Title string
}

type ArtworkAsset struct {
	ID             string
	MimeType       string
	ChecksumSHA256 *string
	Width          *int
	Height         *int
	UpdatedAt      time.Time
}

type TrackRecord struct {
	ID              string
	Title           string
	NormalizedTitle string
	Artists         []ArtistReferenceRecord
	Album           *AlbumReferenceRecord
	Artwork         *ArtworkAsset
	DurationMS      int64
	TrackNumber     *int
	DiscNumber      *int
	Favorite        bool
	PublishedAt     time.Time
	Version         int
}

type ArtistRecord struct {
	ID             string
	Name           string
	NormalizedName string
	Artwork        *ArtworkAsset
	Description    *string
}

type AlbumRecord struct {
	ID              string
	Title           string
	NormalizedTitle string
	Artists         []ArtistReferenceRecord
	Cover           *ArtworkAsset
	ReleaseDate     *string
	TrackCount      int
	Description     *string
}

type LyricRecord struct {
	ID        string
	TrackID   string
	Language  string
	Format    string
	Content   string
	IsDefault bool
	UpdatedAt time.Time
}

type ListLyricsQuery struct {
	TrackID string
	Limit   int
	Offset  int
}

type TrackCursor struct {
	PublishedAt *time.Time
	Title       *string
	DiscNumber  *int
	TrackNumber *int
	ID          string
}

type AlbumCursor struct {
	Title       *string
	ReleaseDate *string
	NullRelease bool
	ID          string
}

type SearchCursor struct {
	Value string
	ID    string
}

type ListTracksQuery struct {
	UserID   string
	Sort     TrackSort
	ArtistID string
	AlbumID  string
	After    *TrackCursor
	Limit    int
}

type ListArtistsQuery struct {
	Sort  ArtistSort
	After *SearchCursor
	Limit int
}

type ListAlbumsQuery struct {
	Sort     AlbumSort
	ArtistID string
	After    *AlbumCursor
	Limit    int
}

type SearchQuery struct {
	UserID          string
	NormalizedQuery string
	Pattern         string
	UseTrigram      bool
	After           *SearchCursor
	Limit           int
}
