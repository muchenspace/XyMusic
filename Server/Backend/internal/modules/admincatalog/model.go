package admincatalog

import (
	"time"

	"xymusic/server/internal/shared/audiostatus"
)

type TrackStatus string

const (
	TrackStatusReady    TrackStatus = "READY"
	TrackStatusError    TrackStatus = "ERROR"
	TrackStatusArchived TrackStatus = "ARCHIVED"
)

type AudioStatus = audiostatus.Status

const (
	AudioStatusProcessing = audiostatus.Processing
	AudioStatusReady      = audiostatus.Ready
	AudioStatusError      = audiostatus.Error
	AudioStatusArchived   = audiostatus.Archived
)

type MetadataStatus string

const (
	MetadataNormal       MetadataStatus = "NORMAL"
	MetadataPendingWrite MetadataStatus = "PENDING_WRITE"
	MetadataWriteFailed  MetadataStatus = "WRITE_FAILED"
)

type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

type ListInput struct {
	Page       int
	PageSize   int
	Search     string
	Sort       string
	Order      SortOrder
	Cursor     string
	CursorMode bool
}

type PageInput struct {
	Page       int
	PageSize   int
	Cursor     string
	CursorMode bool
}

type DuplicateAlbumInput struct {
	PageInput
	AlbumID         string
	AlbumPage       int
	AlbumPageSize   int
	AlbumCursor     string
	AlbumCursorMode bool
}

type TrackListInput struct {
	ListInput
	Status         AudioStatus
	MetadataStatus MetadataStatus
	SourceID       string
}

type CreditRecord struct {
	ArtistID   string
	ArtistName string
	Role       string
	SortOrder  int
}

type ArtistRecord struct {
	ID             string
	Name           string
	NormalizedName string
	ArtworkAssetID *string
	Description    *string
	AlbumCount     int
	TrackCount     int
	Version        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AlbumRecord struct {
	ID              string
	Title           string
	NormalizedTitle string
	Description     *string
	CoverAssetID    *string
	ReleaseDate     *string
	Credits         []CreditRecord
	TrackCount      int
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SourceRecord struct {
	ID             string
	RootID         *string
	RootName       *string
	RelativePath   string
	Status         string
	ChecksumSHA256 string
	Mode           *string
	RootEnabled    *bool
	ScanActive     bool
	MappingCount   int
	Cue            bool
}

type LyricRecord struct {
	ID        string
	Language  string
	Format    string
	Timing    string
	Content   *string
	IsDefault bool
	Version   int
	UpdatedAt time.Time
}

type TrackRecord struct {
	ID                       string
	AlbumID                  *string
	AlbumTitle               *string
	AlbumCoverAssetID        *string
	Title                    string
	NormalizedTitle          string
	TrackNumber              *int
	DiscNumber               *int
	DurationMS               int64
	Status                   TrackStatus
	AudioStatus              AudioStatus
	Version                  int
	PublishedAt              *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Credits                  []CreditRecord
	Source                   *SourceRecord
	MetadataStatus           MetadataStatus
	MetadataVersion          *int
	ActiveWritebackJobID     *string
	LatestWritebackErrorCode *string
	LatestWritebackError     *string
	Lyrics                   []LyricRecord
}

type ArtistQuery struct {
	Search       string
	Sort         string
	Order        SortOrder
	Limit        int
	Offset       int
	After        *ListCursor
	CursorMode   bool
	HasNextProbe bool
	TotalHint    *int
}

type AlbumQuery = ArtistQuery

type DuplicateAlbumQuery struct {
	AlbumID         string
	Limit           int
	Offset          int
	After           *DuplicateGroupCursor
	CursorMode      bool
	TotalHint       *int
	AlbumLimit      int
	AlbumOffset     int
	AlbumAfter      *DuplicateAlbumCursor
	AlbumCursorMode bool
}

type DuplicateGroupCursor struct {
	Key   string
	Total *int
}

type DuplicateAlbumCursor struct {
	ID    string
	Total *int
}

type DuplicateAlbumGroupPage struct {
	Key        string
	Title      string
	Albums     []AlbumRecord
	AlbumTotal int
}

type DuplicateAlbumPage struct {
	Groups              []DuplicateAlbumGroupPage
	Total               int
	GroupCount          int
	DuplicateAlbumCount int
}

type TrackQuery struct {
	Search         string
	Sort           string
	Order          SortOrder
	Status         AudioStatus
	MetadataStatus MetadataStatus
	SourceID       string
	Limit          int
	Offset         int
	After          *ListCursor
	CursorMode     bool
	HasNextProbe   bool
	TotalHint      *int
}

// ListCursor is the decoded seek position for one admin catalog ordering.
// Value contains either a normalized text/status key or an RFC3339/date key;
// Null is used for nullable release dates.
type ListCursor struct {
	Value string
	ID    string
	Null  bool
	Total *int
}

type AlbumTrackCursor struct {
	DiscNumber      *int
	TrackNumber     *int
	NormalizedTitle string
	ID              string
	Total           *int
}

type TrackLyricCursor struct {
	IsDefault bool
	Language  string
	ID        string
	Total     *int
}
