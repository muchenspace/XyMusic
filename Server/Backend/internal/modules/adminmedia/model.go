package adminmedia

import "time"

type UploadPurpose string

const (
	PurposeTrackSource   UploadPurpose = "TRACK_SOURCE"
	PurposeArtistArtwork UploadPurpose = "ARTIST_ARTWORK"
	PurposeAlbumArtwork  UploadPurpose = "ALBUM_ARTWORK"
	PurposeUserAvatar    UploadPurpose = "USER_AVATAR"

	UploadStatusCreated    = "CREATED"
	UploadStatusCompleting = "COMPLETING"
	UploadStatusCompleted  = "COMPLETED"
	UploadStatusExpired    = "EXPIRED"
	UploadStatusFailed     = "FAILED"

	JobStatusPending    = "PENDING"
	JobStatusProcessing = "PROCESSING"
	JobStatusReady      = "READY"
	JobStatusFailed     = "FAILED"
	JobStatusCancelled  = "CANCELLED"
)

type CreateUploadParams struct {
	ID             string
	ActorID        string
	Purpose        UploadPurpose
	TargetID       string
	ObjectKey      string
	FileName       string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	ExpiresAt      time.Time
	Now            time.Time
	MaximumBytes   int64
}

type MediaUpload struct {
	ID                     string
	Purpose                UploadPurpose
	TargetID               string
	TrackID                *string
	UploaderID             string
	ObjectKey              string
	ExpectedSize           int64
	ExpectedChecksumSHA256 string
	ExpectedMIMEType       string
	OriginalFileName       string
	Status                 string
	CompletionToken        *string
	CompletionStartedAt    *time.Time
	AssetID                *string
	JobID                  *string
	ExpiresAt              time.Time
	CreatedAt              time.Time
	CompletedAt            *time.Time
}

type CompletionOutcome string

const (
	CompletionClaimed    CompletionOutcome = "CLAIMED"
	CompletionInProgress CompletionOutcome = "IN_PROGRESS"
	CompletionFinished   CompletionOutcome = "COMPLETED"
	CompletionExpired    CompletionOutcome = "EXPIRED"
)

type CompletionClaim struct {
	Outcome CompletionOutcome
	Upload  MediaUpload
	Token   string
}

type StoredObject struct {
	SizeBytes      int64
	ContentType    string
	ETag           string
	ChecksumSHA256 string
	MetadataSHA256 string
}

type InspectedUpload struct {
	ObjectKey      string
	MIMEType       string
	SizeBytes      int64
	ChecksumSHA256 string
	Width          *int
	Height         *int
	CleanupKeys    []string
}

type FinalizeCompletionParams struct {
	ActorID         string
	TraceID         string
	UploadID        string
	CompletionToken string
	AssetID         string
	JobID           string
	Inspected       InspectedUpload
	CompletionFence CompletionFence
	Now             time.Time
}

type CompletedUpload struct {
	UploadID string
	AssetID  string
	JobID    *string
}

type MediaJob struct {
	ID              string
	Type            string
	Status          string
	TrackID         string
	Generation      int
	Attempts        int
	MaxAttempts     int
	CancelRequested bool
	LastError       *string
	LastErrorCode   *string
	NextAttemptAt   time.Time
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RetryJobParams struct {
	ActorID         string
	TraceID         string
	JobID           string
	ExpectedVersion int
	Reason          *string
	Now             time.Time
}

type AuditWrite struct {
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	TraceID    string
	Details    map[string]any
}
