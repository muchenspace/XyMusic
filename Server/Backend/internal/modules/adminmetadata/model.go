package adminmetadata

import (
	"encoding/json"
	"time"
)

type CreditRole string

const (
	CreditPrimary  CreditRole = "PRIMARY"
	CreditFeatured CreditRole = "FEATURED"
	CreditComposer CreditRole = "COMPOSER"
	CreditLyricist CreditRole = "LYRICIST"
	CreditProducer CreditRole = "PRODUCER"
)

type EditableField string

const (
	FieldTitle        EditableField = "title"
	FieldCredits      EditableField = "credits"
	FieldAlbumArtists EditableField = "albumArtists"
	FieldAlbum        EditableField = "album"
	FieldReleaseDate  EditableField = "releaseDate"
	FieldTrackNumber  EditableField = "trackNumber"
	FieldTrackTotal   EditableField = "trackTotal"
	FieldDiscNumber   EditableField = "discNumber"
	FieldDiscTotal    EditableField = "discTotal"
	FieldGenres       EditableField = "genres"
	FieldBPM          EditableField = "bpm"
	FieldISRC         EditableField = "isrc"
	FieldComment      EditableField = "comment"
	FieldCopyright    EditableField = "copyright"
	FieldLyrics       EditableField = "lyrics"
)

var editableFields = []EditableField{
	FieldTitle, FieldCredits, FieldAlbumArtists, FieldAlbum, FieldReleaseDate,
	FieldTrackNumber, FieldTrackTotal, FieldDiscNumber, FieldDiscTotal,
	FieldGenres, FieldBPM, FieldISRC, FieldComment, FieldCopyright, FieldLyrics,
}

type MetadataCredit struct {
	Name string     `json:"name"`
	Role CreditRole `json:"role"`
}

type MetadataLyrics struct {
	Content  string `json:"content"`
	Format   string `json:"format"`
	Language string `json:"language"`
}

type MetadataSnapshot struct {
	Title        string           `json:"title"`
	Credits      []MetadataCredit `json:"credits"`
	AlbumArtists []string         `json:"albumArtists"`
	Album        *string          `json:"album"`
	ReleaseDate  *string          `json:"releaseDate"`
	TrackNumber  *int             `json:"trackNumber"`
	TrackTotal   *int             `json:"trackTotal"`
	DiscNumber   *int             `json:"discNumber"`
	DiscTotal    *int             `json:"discTotal"`
	Genres       []string         `json:"genres"`
	BPM          *float64         `json:"bpm"`
	ISRC         *string          `json:"isrc"`
	Comment      *string          `json:"comment"`
	Copyright    *string          `json:"copyright"`
	Lyrics       *MetadataLyrics  `json:"lyrics"`
	HasArtwork   bool             `json:"hasArtwork"`
}

type MetadataOverrides map[string]any

type MetadataRecord struct {
	TrackID       string
	SourceID      *string
	Raw           json.RawMessage
	Overrides     json.RawMessage
	RawChecksum   *string
	LastScannedAt *time.Time
	UpdatedBy     *string
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Source        *MetadataSourceRecord
}

type MetadataSourceRecord struct {
	ID             string
	RootID         *string
	SourcePath     string
	Status         string
	ChecksumSHA256 string
	RootPath       *string
	RootMode       *string
	RootEnabled    *bool
	RootStatus     *string
	TrackStatus    *string
	MappingCount   int
	Cue            bool
}

type RevisionRecord struct {
	ID              string
	TrackID         string
	MetadataVersion int
	Action          string
	Raw             json.RawMessage
	Overrides       json.RawMessage
	Effective       json.RawMessage
	ActorID         *string
	Reason          *string
	CreatedAt       time.Time
}

type WritebackStatus string

const (
	WritebackPending    WritebackStatus = "PENDING"
	WritebackProcessing WritebackStatus = "PROCESSING"
	WritebackReady      WritebackStatus = "READY"
	WritebackFailed     WritebackStatus = "FAILED"
	WritebackCancelled  WritebackStatus = "CANCELLED"
)

type WritebackStage string

const (
	StageQueued       WritebackStage = "QUEUED"
	StagePreparing    WritebackStage = "PREPARING"
	StagePrepared     WritebackStage = "PREPARED"
	StageFileReplaced WritebackStage = "FILE_REPLACED"
	StageCommitted    WritebackStage = "COMMITTED"
)

type WritebackJob struct {
	ID                     string
	TrackID                string
	SourceID               string
	RevisionID             *string
	RequestedBy            *string
	Reason                 string
	MetadataSnapshot       json.RawMessage
	MetadataVersion        int
	ExpectedSourceChecksum string
	RootPathSnapshot       string
	SourcePathSnapshot     string
	Status                 WritebackStatus
	Attempts               int
	MaxAttempts            int
	Version                int
	CancelRequested        bool
	AttemptID              *string
	Stage                  WritebackStage
	LockedBy               *string
	LockedUntil            *time.Time
	NextAttemptAt          time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
	BackupPath             *string
	BackupExpiresAt        *time.Time
	OutputChecksumSHA256   *string
	LastErrorCode          *string
	LastError              *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type WritebackContext struct {
	Job         WritebackJob
	Metadata    MetadataRecord
	Source      MetadataSourceRecord
	Artwork     *ArtworkReference
	RootPath    string
	RootMode    string
	Enabled     bool
	Status      string
	TrackStatus string
}

type ArtworkReference struct {
	ObjectKey string
	MIMEType  string
}

type MetadataMutationInput struct {
	ExpectedVersion int
	Patch           map[string]any
	ResetFields     []string
	Reason          string
}

type BatchMutationItem struct {
	TrackID         string `json:"trackId"`
	ExpectedVersion int    `json:"expectedVersion"`
}

type BatchMetadataMutationInput struct {
	Items       []BatchMutationItem
	Patch       map[string]any
	ResetFields []string
	Reason      string
}

type VersionReasonInput struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

type BatchUpdateRecord struct {
	TrackID       string
	Version       int
	ChangedFields []string
}

type WritebackListInput struct {
	Page     int
	PageSize int
	Status   WritebackStatus
	TrackID  string
}

type WritebackListQuery struct {
	Limit   int
	Offset  int
	Status  WritebackStatus
	TrackID string
}
