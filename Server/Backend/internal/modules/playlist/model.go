package playlist

import (
	"errors"
	"time"
)

const (
	MaxPlaylistEntries = 100_000
	positionShift      = 1_000_000
)

type Visibility string

const (
	VisibilityPrivate  Visibility = "PRIVATE"
	VisibilityUnlisted Visibility = "UNLISTED"
	VisibilityPublic   Visibility = "PUBLIC"
)

type Sort string

const (
	SortUpdatedDesc Sort = "UPDATED_DESC"
	SortNameAsc     Sort = "NAME_ASC"
	SortNameDesc    Sort = "NAME_DESC"
)

type PlaylistRecord struct {
	ID           string
	OwnerID      string
	Name         string
	Description  *string
	Visibility   Visibility
	CoverAssetID *string
	TrackCount   int
	Version      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type EntryRecord struct {
	ID         string
	PlaylistID string
	TrackID    string
	Position   int
	AddedBy    string
	AddedAt    time.Time
}

type PlaylistCursor struct {
	UpdatedAt *time.Time
	Name      *string
	ID        string
}

type EntryCursor struct {
	Position int
	ID       string
}

type ListOwnedQuery struct {
	OwnerID string
	Sort    Sort
	After   *PlaylistCursor
	Limit   int
}

type ListEntriesQuery struct {
	PlaylistID string
	After      *EntryCursor
	Limit      int
}

type CreatePlaylistParams struct {
	OwnerID     string
	Name        string
	Description *string
	Visibility  Visibility
}

type UpdatePlaylistParams struct {
	OwnerID         string
	PlaylistID      string
	ExpectedVersion int
	Name            *string
	SetDescription  bool
	Description     *string
	Visibility      *Visibility
}

type AddTrackParams struct {
	OwnerID            string
	PlaylistID         string
	ExpectedVersion    int
	TrackID            string
	InsertAfterEntryID *string
	Now                time.Time
}

type RemoveTrackParams struct {
	OwnerID         string
	PlaylistID      string
	EntryID         string
	ExpectedVersion int
	Now             time.Time
}

type ReorderParams struct {
	OwnerID         string
	PlaylistID      string
	ExpectedVersion int
	OrderedEntryIDs []string
	Now             time.Time
}

type AddTrackMutation struct {
	PlaylistID string
	Version    int
	UpdatedAt  time.Time
	Entry      EntryRecord
}

type VersionMutation struct {
	PlaylistID string
	Version    int
	UpdatedAt  time.Time
}

var (
	ErrNotFound           = errors.New("playlist record not found")
	ErrTrackNotFound      = errors.New("playlist track not found")
	ErrDuplicateTrack     = errors.New("track already in playlist")
	ErrPlaylistFull       = errors.New("playlist is full")
	ErrInsertAfterMissing = errors.New("insert-after entry not found")
	ErrEntryNotFound      = errors.New("playlist entry not found")
	ErrIncompleteOrder    = errors.New("playlist order is incomplete")
	ErrUnknownOrderEntry  = errors.New("playlist order contains unknown entry")
)

type VersionConflictError struct {
	PlaylistID      string
	ExpectedVersion int
	CurrentVersion  int
}

func (conflict *VersionConflictError) Error() string {
	return "playlist version conflict"
}
