package profile

import "time"

const (
	AvatarUploadPurpose = "USER_AVATAR"

	UploadStatusCreated    = "CREATED"
	UploadStatusCompleting = "COMPLETING"
	UploadStatusCompleted  = "COMPLETED"
	UploadStatusExpired    = "EXPIRED"
	UploadStatusFailed     = "FAILED"
)

type ProfileChanges struct {
	DisplayNameSet bool
	DisplayName    string
	BioSet         bool
	Bio            *string
}

type CreateUploadParams struct {
	ID             string
	ActorID        string
	StoragePath    string
	FileName       string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	ExpiresAt      time.Time
	Now            time.Time
}

type AvatarUpload struct {
	ID                     string
	Purpose                string
	TargetID               string
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
	Upload  AvatarUpload
	Token   string
}

type InspectedAvatar struct {
	StoragePath    string
	MIMEType       string
	SizeBytes      int64
	ChecksumSHA256 string
	Width          int
	Height         int
}

type FinalizeAvatarParams struct {
	ActorID         string
	UploadID        string
	CompletionToken string
	AssetID         string
	Inspected       InspectedAvatar
	Now             time.Time
}
