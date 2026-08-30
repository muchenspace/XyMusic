package adminmedia

import "time"

type UploadPurpose string

const (
	PurposeTrackSource   UploadPurpose = "TRACK_SOURCE"
	PurposeArtistArtwork UploadPurpose = "ARTIST_ARTWORK"
	PurposeAlbumArtwork  UploadPurpose = "ALBUM_ARTWORK"
)

func (purpose UploadPurpose) Valid() bool {
	return purpose == PurposeTrackSource || purpose == PurposeArtistArtwork || purpose == PurposeAlbumArtwork
}

const (
	UploadStatusCreated    = "CREATED"
	UploadStatusCompleting = "COMPLETING"
	UploadStatusCompleted  = "COMPLETED"
	UploadStatusExpired    = "EXPIRED"
	UploadStatusFailed     = "FAILED"
)

type UploadReservation struct {
	ID                     string
	Purpose                UploadPurpose
	TargetID               string
	TrackID                *string
	UploaderID             string
	StoragePath            string
	ExpectedSize           int64
	ExpectedChecksumSHA256 string
	ExpectedMIMEType       string
	OriginalFileName       string
	Status                 string
	AssetID                *string
	ExpiresAt              time.Time
	CreatedAt              time.Time
	CompletedAt            *time.Time
	CompletionToken        *string
	CompletionStartedAt    *time.Time
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
	Upload  UploadReservation
	Token   string
}

type InspectedMedia struct {
	StoragePath    string
	Kind           string
	MIMEType       string
	SizeBytes      int64
	ChecksumSHA256 string
	Width          *int
	Height         *int
	DurationMs     *int64
	Bitrate        *int
	SampleRate     *int
	Channels       *int
}

type FinalizeUploadParams struct {
	UploadID        string
	CompletionToken string
	AssetID         string
	Inspected       InspectedMedia
	CompletionFence CompletionFence
	Now             time.Time
}
