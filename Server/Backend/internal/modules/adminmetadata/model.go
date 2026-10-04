package adminmetadata

import (
	"encoding/json"
	"time"

	"xymusic/server/internal/platform/mediafile"
	"xymusic/server/internal/shared/requestdto"
)

type CreditRole = mediafile.CreditRole

const (
	CreditPrimary  = mediafile.CreditPrimary
	CreditFeatured = mediafile.CreditFeatured
	CreditComposer = mediafile.CreditComposer
	CreditLyricist = mediafile.CreditLyricist
	CreditProducer = mediafile.CreditProducer
)

type EditableField = mediafile.EditableField

const (
	FieldTitle        = mediafile.FieldTitle
	FieldCredits      = mediafile.FieldCredits
	FieldAlbumArtists = mediafile.FieldAlbumArtists
	FieldAlbum        = mediafile.FieldAlbum
	FieldReleaseDate  = mediafile.FieldReleaseDate
	FieldTrackNumber  = mediafile.FieldTrackNumber
	FieldTrackTotal   = mediafile.FieldTrackTotal
	FieldDiscNumber   = mediafile.FieldDiscNumber
	FieldDiscTotal    = mediafile.FieldDiscTotal
	FieldGenres       = mediafile.FieldGenres
	FieldBPM          = mediafile.FieldBPM
	FieldISRC         = mediafile.FieldISRC
	FieldComment      = mediafile.FieldComment
	FieldCopyright    = mediafile.FieldCopyright
	FieldLyrics       = mediafile.FieldLyrics
)

var editableFields = mediafile.EditableFields

type MetadataCredit = mediafile.MetadataCredit

type MetadataLyrics = mediafile.MetadataLyrics

type MetadataSnapshot = mediafile.MetadataSnapshot

type MetadataOverrides = mediafile.MetadataOverrides

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
	ScanActive     bool
	TrackStatus    *string
	MappingCount   int
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
	ScanRunning bool
	TrackStatus string
}

type ArtworkReference struct {
	ObjectKey string
	MIMEType  string
}

type MetadataMutationInput struct {
	ExpectedVersion int
	Patch           map[string]any
}

type BatchMutationItem struct {
	TrackID         string `json:"trackId"`
	ExpectedVersion int    `json:"expectedVersion"`
}

type BatchMetadataMutationInput struct {
	Items []BatchMutationItem
	Patch map[string]any
}

type VersionReasonInput struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

type VersionInput = requestdto.VersionInput

type BatchUpdateRecord struct {
	TrackID       string
	Version       int
	ChangedFields []string
}

type WritebackListInput struct {
	Page       int
	PageSize   int
	Status     WritebackStatus
	TrackID    string
	Cursor     string
	CursorMode bool
}

type WritebackListQuery struct {
	Limit      int
	Offset     int
	Status     WritebackStatus
	TrackID    string
	After      *WritebackCursor
	CursorMode bool
	TotalHint  *int
}

type WritebackCursor struct {
	CreatedAt string
	ID        string
	Total     *int
}
