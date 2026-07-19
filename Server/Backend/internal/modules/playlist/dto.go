package playlist

import (
	"bytes"
	"encoding/json"
	"errors"

	"xymusic/server/internal/modules/catalog"
)

type ArtworkDTO = catalog.ArtworkDTO

type UserSummaryDTO struct {
	ID          string      `json:"id"`
	Username    string      `json:"username"`
	DisplayName string      `json:"displayName"`
	Avatar      *ArtworkDTO `json:"avatar"`
}

type SummaryDTO struct {
	ID          string         `json:"id"`
	Owner       UserSummaryDTO `json:"owner"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	Visibility  Visibility     `json:"visibility"`
	Cover       *ArtworkDTO    `json:"cover"`
	TrackCount  int            `json:"trackCount"`
	Version     int            `json:"version"`
	CreatedAt   string         `json:"createdAt"`
	UpdatedAt   string         `json:"updatedAt"`
}

type EntryDTO struct {
	ID       string                  `json:"id"`
	Position int                     `json:"position"`
	Track    catalog.TrackSummaryDTO `json:"track"`
	AddedBy  UserSummaryDTO          `json:"addedBy"`
	AddedAt  string                  `json:"addedAt"`
}

type DetailDTO struct {
	SummaryDTO
	Entries    []EntryDTO `json:"entries"`
	NextCursor *string    `json:"nextCursor"`
}

type PageDTO struct {
	Items      []SummaryDTO `json:"items"`
	NextCursor *string      `json:"nextCursor"`
}

type AddTrackDTO struct {
	PlaylistID string   `json:"playlistId"`
	Version    int      `json:"version"`
	UpdatedAt  string   `json:"updatedAt"`
	Entry      EntryDTO `json:"entry"`
}

type VersionDTO struct {
	PlaylistID string `json:"playlistId"`
	Version    int    `json:"version"`
	UpdatedAt  string `json:"updatedAt"`
}

type ListOwnedInput struct {
	Sort   Sort
	Cursor string
	Limit  *int
}

type GetInput struct {
	Cursor string
	Limit  *int
}

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

type OptionalVisibility struct {
	Set   bool
	Value Visibility
}

func (value *OptionalVisibility) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be a visibility string")
	}
	return json.Unmarshal(raw, &value.Value)
}

type OptionalUUID struct {
	Set   bool
	Value *string
}

func (value *OptionalUUID) UnmarshalJSON(raw []byte) error {
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

type CreateInput struct {
	Name        string                 `json:"name"`
	Description OptionalNullableString `json:"description"`
	Visibility  Visibility             `json:"visibility"`
}

func (input CreateInput) Payload() map[string]any {
	payload := map[string]any{"name": input.Name, "visibility": input.Visibility}
	if input.Description.Set {
		payload["description"] = input.Description.Value
	}
	return payload
}

type UpdateInput struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	Name            OptionalString         `json:"name"`
	Description     OptionalNullableString `json:"description"`
	Visibility      OptionalVisibility     `json:"visibility"`
}

func (input UpdateInput) Payload() map[string]any {
	payload := map[string]any{"expectedVersion": input.ExpectedVersion}
	if input.Name.Set {
		payload["name"] = input.Name.Value
	}
	if input.Description.Set {
		payload["description"] = input.Description.Value
	}
	if input.Visibility.Set {
		payload["visibility"] = input.Visibility.Value
	}
	return payload
}

type AddTrackInput struct {
	ExpectedVersion    int          `json:"expectedVersion"`
	TrackID            string       `json:"trackId"`
	InsertAfterEntryID OptionalUUID `json:"insertAfterEntryId"`
}

func (input AddTrackInput) Payload() map[string]any {
	payload := map[string]any{"expectedVersion": input.ExpectedVersion, "trackId": input.TrackID}
	if input.InsertAfterEntryID.Set {
		payload["insertAfterEntryId"] = input.InsertAfterEntryID.Value
	}
	return payload
}

type ReorderInput struct {
	ExpectedVersion int                 `json:"expectedVersion"`
	OrderedEntryIDs RequiredStringSlice `json:"orderedEntryIds"`
}

func (input ReorderInput) Payload() map[string]any {
	return map[string]any{"expectedVersion": input.ExpectedVersion, "orderedEntryIds": input.OrderedEntryIDs.Values}
}

type RequiredStringSlice struct {
	Set    bool
	Values []string
}

func (value *RequiredStringSlice) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be an array")
	}
	return json.Unmarshal(raw, &value.Values)
}
