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
	MetadataOriginal     MetadataStatus = "ORIGINAL"
	MetadataOverridden   MetadataStatus = "OVERRIDDEN"
	MetadataPendingWrite MetadataStatus = "PENDING_WRITE"
	MetadataWriteFailed  MetadataStatus = "WRITE_FAILED"
)

type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

type ListInput struct {
	Page     int
	PageSize int
	Search   string
	Sort     string
	Order    SortOrder
}

type PageInput struct {
	Page     int
	PageSize int
}

type DuplicateAlbumInput struct {
	PageInput
	AlbumID       string
	AlbumPage     int
	AlbumPageSize int
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
	RootStatus     *string
	MappingCount   int
	Cue            bool
}

type MediaProcessingRecord struct {
	Status        string
	Attempts      int
	MaxAttempts   int
	LastError     *string
	LastErrorCode *string
	UpdatedAt     time.Time
}

type VariantRecord struct {
	ID         string
	Quality    string
	MimeType   string
	Codec      string
	Container  string
	Bitrate    int
	SampleRate *int
	Status     string
	UpdatedAt  time.Time
}

type LyricRecord struct {
	ID        string
	Language  string
	Format    string
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
	MediaProcessing          *MediaProcessingRecord
	Variants                 []VariantRecord
	ActiveWritebackJobID     *string
	LatestWritebackErrorCode *string
	LatestWritebackError     *string
	Lyrics                   []LyricRecord
}

type ArtistQuery struct {
	Search string
	Sort   string
	Order  SortOrder
	Limit  int
	Offset int
}

type AlbumQuery = ArtistQuery

type DuplicateAlbumQuery struct {
	AlbumID     string
	Limit       int
	Offset      int
	AlbumLimit  int
	AlbumOffset int
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
}
