package adminmutation

import (
	"bytes"
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

type UserStatus string

const (
	UserActive    UserStatus = "ACTIVE"
	UserSuspended UserStatus = "SUSPENDED"
)

type CreditInput struct {
	ArtistID     string     `json:"artistId"`
	Role         CreditRole `json:"role"`
	SortOrder    int        `json:"sortOrder"`
	SortOrderSet bool       `json:"-"`
}

func (input *CreditInput) UnmarshalJSON(raw []byte) error {
	var decoded struct {
		ArtistID  string     `json:"artistId"`
		Role      CreditRole `json:"role"`
		SortOrder *int       `json:"sortOrder"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	input.ArtistID, input.Role = decoded.ArtistID, decoded.Role
	if decoded.SortOrder != nil {
		input.SortOrder, input.SortOrderSet = *decoded.SortOrder, true
	}
	return nil
}

type OptionalString struct {
	Set   bool
	Value string
}

func (value *OptionalString) UnmarshalJSON(raw []byte) error {
	value.Set = true
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

type OptionalNullableInt struct {
	Set   bool
	Value *int
}

func (value *OptionalNullableInt) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.Value = nil
		return nil
	}
	var decoded int
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type OptionalInt struct {
	Set   bool
	Value int
}

func (value *OptionalInt) UnmarshalJSON(raw []byte) error {
	value.Set = true
	return json.Unmarshal(raw, &value.Value)
}

type OptionalBool struct {
	Set   bool
	Value bool
}

func (value *OptionalBool) UnmarshalJSON(raw []byte) error {
	value.Set = true
	return json.Unmarshal(raw, &value.Value)
}

type OptionalCredits struct {
	Set    bool
	Values []CreditInput
}

func (value *OptionalCredits) UnmarshalJSON(raw []byte) error {
	value.Set = true
	return json.Unmarshal(raw, &value.Values)
}

type CreateArtistInput struct {
	Name        string                 `json:"name"`
	Description OptionalNullableString `json:"description"`
}
type UpdateArtistInput struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	Name            OptionalString         `json:"name"`
	Description     OptionalNullableString `json:"description"`
}

type CreateAlbumInput struct {
	Title         string                 `json:"title"`
	ArtistCredits []CreditInput          `json:"artistCredits"`
	ReleaseDate   OptionalNullableString `json:"releaseDate"`
	Description   OptionalNullableString `json:"description"`
}
type UpdateAlbumInput struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	Title           OptionalString         `json:"title"`
	ArtistCredits   OptionalCredits        `json:"artistCredits"`
	ReleaseDate     OptionalNullableString `json:"releaseDate"`
	Description     OptionalNullableString `json:"description"`
}

type AlbumVersionInput struct {
	AlbumID         string `json:"albumId"`
	ExpectedVersion int    `json:"expectedVersion"`
}
type AlbumMergeFieldSources struct {
	Title         string                 `json:"title"`
	Cover         OptionalNullableString `json:"cover"`
	ArtistCredits string                 `json:"artistCredits"`
	ReleaseDate   OptionalNullableString `json:"releaseDate"`
	Description   OptionalNullableString `json:"description"`
}
type MergeAlbumsInput struct {
	Target       AlbumVersionInput      `json:"target"`
	Sources      []AlbumVersionInput    `json:"sources"`
	FieldSources AlbumMergeFieldSources `json:"fieldSources"`
}

type CreateTrackInput struct {
	Title         string                 `json:"title"`
	AlbumID       OptionalNullableString `json:"albumId"`
	ArtistCredits []CreditInput          `json:"artistCredits"`
	TrackNumber   OptionalNullableInt    `json:"trackNumber"`
	DiscNumber    OptionalInt            `json:"discNumber"`
}
type UpdateTrackInput struct {
	ExpectedVersion int                    `json:"expectedVersion"`
	Title           OptionalString         `json:"title"`
	AlbumID         OptionalNullableString `json:"albumId"`
	ArtistCredits   OptionalCredits        `json:"artistCredits"`
	TrackNumber     OptionalNullableInt    `json:"trackNumber"`
	DiscNumber      OptionalInt            `json:"discNumber"`
}
type VersionInput struct {
	ExpectedVersion int `json:"expectedVersion"`
}
type LyricsInput struct {
	ExpectedVersion int            `json:"expectedVersion"`
	Language        string         `json:"language"`
	Format          string         `json:"format"`
	Content         OptionalString `json:"content"`
	IsDefault       OptionalBool   `json:"isDefault"`
}
type UserStatusInput struct {
	ExpectedVersion int        `json:"expectedVersion"`
	Status          UserStatus `json:"status"`
	Reason          string     `json:"reason"`
}

type ArtistRecord struct {
	ID, Name                    string
	Description, ArtworkAssetID *string
	Version                     int
	CreatedAt, UpdatedAt        time.Time
}
type AlbumRecord struct {
	ID, Title                              string
	Description, CoverAssetID, ReleaseDate *string
	Credits                                []CreditRecord
	Version                                int
	CreatedAt, UpdatedAt                   time.Time
}
type CreditRecord struct {
	ArtistID, ArtistName string
	Role                 CreditRole
	SortOrder            int
}
type TrackRecord struct {
	ID, Title, Status                      string
	AlbumID, AlbumTitle, AlbumCoverAssetID *string
	Credits                                []CreditRecord
	DurationMS                             int64
	TrackNumber, DiscNumber                *int
	ActiveMediaJobID                       *string
	Version                                int
	CreatedAt, UpdatedAt                   time.Time
}
type UserRecord struct {
	ID, Username, DisplayName, Role            string
	Status                                     UserStatus
	Version                                    int
	CreatedAt, UserUpdatedAt, ProfileUpdatedAt time.Time
}

type CreateArtistParams struct {
	Name, NormalizedName string
	SetDescription       bool
	Description          *string
}
type UpdateArtistParams struct {
	ID                   string
	ExpectedVersion      int
	Name, NormalizedName *string
	SetDescription       bool
	Description          *string
}
type CreateAlbumParams struct {
	Title, NormalizedTitle string
	Credits                []CreditInput
	SetReleaseDate         bool
	ReleaseDate            *string
	SetDescription         bool
	Description            *string
}
type UpdateAlbumParams struct {
	ID                     string
	ExpectedVersion        int
	Title, NormalizedTitle *string
	SetCredits             bool
	Credits                []CreditInput
	SetReleaseDate         bool
	ReleaseDate            *string
	SetDescription         bool
	Description            *string
}
type CreateTrackParams struct {
	Title, NormalizedTitle string
	AlbumID                *string
	Credits                []CreditInput
	TrackNumber            *int
	DiscNumber             int
}
type UpdateTrackParams struct {
	ID                     string
	ExpectedVersion        int
	Title, NormalizedTitle *string
	SetAlbum               bool
	AlbumID                *string
	SetCredits             bool
	Credits                []CreditInput
	SetTrackNumber         bool
	TrackNumber            *int
	DiscNumber             *int
}
type DeleteResult struct{ DeletedFiles, QuarantinedFiles, ScheduledObjects int }
type StoredLyric struct {
	ID, Language, Format, Content string
	IsDefault                     bool
	TrackVersion                  int
	UpdatedAt                     time.Time
}
