package profile

import (
	"bytes"
	"encoding/json"
	"errors"
)

const AvatarMaximumBytes int64 = 5 * 1024 * 1024

// OptionalString distinguishes an omitted JSON property from a supplied
// string. The legacy API rejects null for these properties.
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

// OptionalNullableString distinguishes an omitted property from an explicit
// null, which is required for clearing a profile biography.
type OptionalNullableString struct {
	Set   bool
	Value *string
}

func (value *OptionalNullableString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type UpdateProfileInput struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	DisplayName     OptionalString         `json:"displayName"`
	Bio             OptionalNullableString `json:"bio"`
}

type CreateAvatarUploadInput struct {
	FileName       string `json:"fileName"`
	ContentType    string `json:"contentType"`
	SizeBytes      int64  `json:"sizeBytes"`
	ChecksumSHA256 string `json:"checksumSha256"`
}

type CompleteAvatarUploadInput struct {
	ObservedETag OptionalString `json:"observedEtag"`
}

type AvatarUploadDTO struct {
	ID              string            `json:"id"`
	Purpose         string            `json:"purpose"`
	TargetID        string            `json:"targetId"`
	Status          string            `json:"status"`
	Method          string            `json:"method"`
	UploadURL       string            `json:"uploadUrl"`
	RequiredHeaders map[string]string `json:"requiredHeaders"`
	ExpiresAt       string            `json:"expiresAt"`
}

// MutationResult carries HTTP replay metadata without coupling the service to
// Gin. Routes expose Replayed through X-Idempotent-Replay.
type MutationResult[T any] struct {
	Body     T
	Replayed bool
}
