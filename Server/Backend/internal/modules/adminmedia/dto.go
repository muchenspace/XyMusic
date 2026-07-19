package adminmedia

import (
	"bytes"
	"encoding/json"
	"errors"
)

// OptionalString preserves whether an optional JSON property was supplied.
// JSON null is intentionally rejected to match the legacy TypeBox contract.
type OptionalString struct {
	Set   bool
	Value string
}

func (value *OptionalString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be a string")
	}
	return json.Unmarshal(raw, &value.Value)
}

type CreateUploadInput struct {
	Purpose        UploadPurpose `json:"purpose"`
	TargetID       string        `json:"targetId"`
	FileName       string        `json:"fileName"`
	ContentType    string        `json:"contentType"`
	SizeBytes      int64         `json:"sizeBytes"`
	ChecksumSHA256 string        `json:"checksumSha256"`
}

type CompleteUploadInput struct {
	ObservedETag    OptionalString  `json:"observedEtag"`
	CompletionFence CompletionFence `json:"-"`
}

type RetryJobInput struct {
	ExpectedVersion int            `json:"expectedVersion"`
	Reason          OptionalString `json:"reason"`
}

type UploadReservationDTO struct {
	ID              string            `json:"id"`
	Purpose         UploadPurpose     `json:"purpose"`
	TargetID        string            `json:"targetId"`
	Status          string            `json:"status"`
	Method          string            `json:"method"`
	UploadURL       string            `json:"uploadUrl"`
	RequiredHeaders map[string]string `json:"requiredHeaders"`
	ExpiresAt       string            `json:"expiresAt"`
}

type UploadCompletionDTO struct {
	UploadID string  `json:"uploadId"`
	Status   string  `json:"status"`
	AssetID  string  `json:"assetId"`
	JobID    *string `json:"jobId"`
}

type MediaJobDTO struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	Status           string  `json:"status"`
	Attempts         int     `json:"attempts"`
	MaxAttempts      int     `json:"maxAttempts"`
	CancelRequested  bool    `json:"cancelRequested"`
	LastErrorCode    *string `json:"lastErrorCode"`
	LastErrorMessage *string `json:"lastErrorMessage"`
	NextAttemptAt    *string `json:"nextAttemptAt"`
	Version          int     `json:"version"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}
