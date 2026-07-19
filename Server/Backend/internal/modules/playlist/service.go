package playlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

const (
	defaultPageLimit = 20
	maximumPageLimit = 100
)

type ServiceDependencies struct {
	Repository Store
	Cursors    *pagination.CursorCodec
	Catalog    CatalogPresenter
	Users      UserPresenter
	Clock      Clock
}

type Service struct {
	repository Store
	cursors    *pagination.CursorCodec
	catalog    CatalogPresenter
	users      UserPresenter
	clock      Clock
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil {
		return nil, errors.New("playlist repository is required")
	}
	if dependencies.Cursors == nil {
		return nil, errors.New("playlist cursor codec is required")
	}
	if dependencies.Catalog == nil {
		return nil, errors.New("playlist catalog presenter is required")
	}
	if dependencies.Users == nil {
		return nil, errors.New("playlist user presenter is required")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	return &Service{
		repository: dependencies.Repository,
		cursors:    dependencies.Cursors,
		catalog:    dependencies.Catalog,
		users:      dependencies.Users,
		clock:      dependencies.Clock,
	}, nil
}

func (service *Service) ListOwned(ctx context.Context, ownerID string, input ListOwnedInput) (PageDTO, error) {
	if !validSort(input.Sort) {
		return PageDTO{}, apperror.Validation("sort is invalid")
	}
	limit, err := pageLimit(input.Limit)
	if err != nil {
		return PageDTO{}, err
	}
	scope := fmt.Sprintf("playlists:%s:%s", ownerID, input.Sort)
	cursor, err := service.decodePlaylistCursor(scope, input.Sort, input.Cursor)
	if err != nil {
		return PageDTO{}, err
	}
	rows, err := service.repository.ListOwned(ctx, ListOwnedQuery{
		OwnerID: ownerID,
		Sort:    input.Sort,
		After:   cursor,
		Limit:   limit + 1,
	})
	if err != nil {
		return PageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := service.presentSummaries(ctx, pageRows)
	if err != nil {
		return PageDTO{}, err
	}
	result := PageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		encoded, err := service.encodePlaylistCursor(scope, input.Sort, pageRows[len(pageRows)-1])
		if err != nil {
			return PageDTO{}, fmt.Errorf("encode playlist cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (service *Service) Create(ctx context.Context, ownerID string, input CreateInput) (SummaryDTO, error) {
	name, description, visibility, err := validateCreate(input)
	if err != nil {
		return SummaryDTO{}, err
	}
	record, err := service.repository.CreatePlaylist(ctx, CreatePlaylistParams{
		OwnerID: ownerID, Name: name, Description: description, Visibility: visibility,
	})
	if err != nil {
		return SummaryDTO{}, err
	}
	return service.presentSummary(ctx, record)
}

func (service *Service) Get(ctx context.Context, requesterID, playlistID string, input GetInput) (DetailDTO, error) {
	playlist, err := service.repository.FindPlaylist(ctx, playlistID)
	if errors.Is(err, ErrNotFound) || (err == nil && playlist.OwnerID != requesterID && playlist.Visibility == VisibilityPrivate) {
		return DetailDTO{}, apperror.NotFound("Playlist was not found")
	}
	if err != nil {
		return DetailDTO{}, err
	}
	limit, err := pageLimit(input.Limit)
	if err != nil {
		return DetailDTO{}, err
	}
	scope := fmt.Sprintf("playlist-entries:%s:%d", playlistID, playlist.Version)
	cursor, err := decodeEntryCursor(service.cursors, scope, input.Cursor)
	if err != nil {
		return DetailDTO{}, err
	}
	rows, err := service.repository.ListEntries(ctx, ListEntriesQuery{
		PlaylistID: playlistID,
		After:      cursor,
		Limit:      limit + 1,
	})
	if err != nil {
		return DetailDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	trackIDs := make([]string, len(pageRows))
	userIDs := make([]string, len(pageRows))
	for index, entry := range pageRows {
		trackIDs[index] = entry.TrackID
		userIDs[index] = entry.AddedBy
	}
	tracks, err := service.catalog.TrackSummaries(ctx, requesterID, trackIDs)
	if err != nil {
		return DetailDTO{}, err
	}
	users, err := service.users.UserSummaries(ctx, userIDs)
	if err != nil {
		return DetailDTO{}, err
	}
	trackByID := make(map[string]catalog.TrackSummaryDTO, len(tracks))
	for _, track := range tracks {
		trackByID[track.ID] = track
	}
	entries := make([]EntryDTO, 0, len(pageRows))
	for _, entry := range pageRows {
		track, hasTrack := trackByID[entry.TrackID]
		user, hasUser := users[entry.AddedBy]
		if !hasTrack || !hasUser {
			continue
		}
		entries = append(entries, EntryDTO{
			ID:       entry.ID,
			Position: entry.Position,
			Track:    track,
			AddedBy:  user,
			AddedAt:  formatTimestamp(entry.AddedAt),
		})
	}
	summary, err := service.presentSummary(ctx, playlist)
	if err != nil {
		return DetailDTO{}, err
	}
	result := DetailDTO{SummaryDTO: summary, Entries: entries}
	if hasNext && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		position := last.Position
		encoded, err := pagination.EncodeCursor(service.cursors, scope, entryCursorValue{Position: &position, ID: last.ID})
		if err != nil {
			return DetailDTO{}, fmt.Errorf("encode playlist entry cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (service *Service) Update(ctx context.Context, ownerID, playlistID string, input UpdateInput) (SummaryDTO, error) {
	if input.ExpectedVersion < 1 {
		return SummaryDTO{}, apperror.Validation("expectedVersion must be a positive integer")
	}
	if !input.Name.Set && !input.Description.Set && !input.Visibility.Set {
		return SummaryDTO{}, apperror.Validation("At least one playlist field must be supplied")
	}
	params := UpdatePlaylistParams{
		OwnerID:         ownerID,
		PlaylistID:      playlistID,
		ExpectedVersion: input.ExpectedVersion,
		SetDescription:  input.Description.Set,
	}
	if input.Name.Set {
		name, err := validateName(input.Name.Value)
		if err != nil {
			return SummaryDTO{}, err
		}
		params.Name = &name
	}
	if input.Description.Set {
		description, err := validateDescription(input.Description.Value)
		if err != nil {
			return SummaryDTO{}, err
		}
		params.Description = description
	}
	if input.Visibility.Set {
		if !validVisibility(input.Visibility.Value) {
			return SummaryDTO{}, apperror.Validation("visibility is invalid")
		}
		visibility := input.Visibility.Value
		params.Visibility = &visibility
	}
	record, err := service.repository.UpdatePlaylist(ctx, params)
	if err != nil {
		return SummaryDTO{}, mapStoreError(err)
	}
	return service.presentSummary(ctx, record)
}

func (service *Service) Delete(ctx context.Context, ownerID, playlistID string, expectedVersion int) error {
	if expectedVersion < 1 {
		return apperror.Validation("expectedVersion must be a positive integer")
	}
	return mapStoreError(service.repository.DeletePlaylist(ctx, ownerID, playlistID, expectedVersion))
}

func (service *Service) AddTrack(ctx context.Context, ownerID, playlistID string, input AddTrackInput) (AddTrackDTO, error) {
	if input.ExpectedVersion < 1 {
		return AddTrackDTO{}, apperror.Validation("expectedVersion must be a positive integer")
	}
	exists, err := service.repository.ReadyTrackExists(ctx, input.TrackID)
	if err != nil {
		return AddTrackDTO{}, err
	}
	if !exists {
		return AddTrackDTO{}, apperror.NotFound("Track was not found")
	}
	mutation, err := service.repository.AddTrack(ctx, AddTrackParams{
		OwnerID:            ownerID,
		PlaylistID:         playlistID,
		ExpectedVersion:    input.ExpectedVersion,
		TrackID:            input.TrackID,
		InsertAfterEntryID: input.InsertAfterEntryID.Value,
		Now:                service.clock.Now(),
	})
	if err != nil {
		return AddTrackDTO{}, mapStoreError(err)
	}
	tracks, err := service.catalog.TrackSummaries(ctx, ownerID, []string{mutation.Entry.TrackID})
	if err != nil {
		return AddTrackDTO{}, err
	}
	if len(tracks) == 0 {
		return AddTrackDTO{}, errors.New("catalog did not present the inserted playlist track")
	}
	addedBy, err := service.users.UserSummary(ctx, ownerID)
	if err != nil {
		return AddTrackDTO{}, err
	}
	return AddTrackDTO{
		PlaylistID: mutation.PlaylistID,
		Version:    mutation.Version,
		UpdatedAt:  formatTimestamp(mutation.UpdatedAt),
		Entry: EntryDTO{
			ID:       mutation.Entry.ID,
			Position: mutation.Entry.Position,
			Track:    tracks[0],
			AddedBy:  addedBy,
			AddedAt:  formatTimestamp(mutation.Entry.AddedAt),
		},
	}, nil
}

func (service *Service) RemoveTrack(
	ctx context.Context,
	ownerID, playlistID, entryID string,
	expectedVersion int,
) (VersionDTO, error) {
	if expectedVersion < 1 {
		return VersionDTO{}, apperror.Validation("expectedVersion must be a positive integer")
	}
	mutation, err := service.repository.RemoveTrack(ctx, RemoveTrackParams{
		OwnerID: ownerID, PlaylistID: playlistID, EntryID: entryID,
		ExpectedVersion: expectedVersion, Now: service.clock.Now(),
	})
	if err != nil {
		return VersionDTO{}, mapStoreError(err)
	}
	return presentVersion(mutation), nil
}

func (service *Service) Reorder(ctx context.Context, ownerID, playlistID string, input ReorderInput) (VersionDTO, error) {
	if input.ExpectedVersion < 1 {
		return VersionDTO{}, apperror.Validation("expectedVersion must be a positive integer")
	}
	if !input.OrderedEntryIDs.Set {
		return VersionDTO{}, apperror.Validation("orderedEntryIds is required")
	}
	orderedEntryIDs := input.OrderedEntryIDs.Values
	if len(orderedEntryIDs) > MaxPlaylistEntries {
		return VersionDTO{}, apperror.Validation(fmt.Sprintf("orderedEntryIds cannot exceed %d entries", MaxPlaylistEntries))
	}
	seen := make(map[string]struct{}, len(orderedEntryIDs))
	for _, id := range orderedEntryIDs {
		if _, duplicate := seen[id]; duplicate {
			return VersionDTO{}, apperror.Validation("orderedEntryIds must be unique")
		}
		seen[id] = struct{}{}
	}
	mutation, err := service.repository.Reorder(ctx, ReorderParams{
		OwnerID: ownerID, PlaylistID: playlistID, ExpectedVersion: input.ExpectedVersion,
		OrderedEntryIDs: append([]string(nil), orderedEntryIDs...), Now: service.clock.Now(),
	})
	if err != nil {
		return VersionDTO{}, mapStoreError(err)
	}
	return presentVersion(mutation), nil
}

func (service *Service) presentSummary(ctx context.Context, record PlaylistRecord) (SummaryDTO, error) {
	items, err := service.presentSummaries(ctx, []PlaylistRecord{record})
	if err != nil {
		return SummaryDTO{}, err
	}
	if len(items) == 0 {
		return SummaryDTO{}, errors.New("playlist summary was not created")
	}
	return items[0], nil
}

func (service *Service) presentSummaries(ctx context.Context, records []PlaylistRecord) ([]SummaryDTO, error) {
	if len(records) == 0 {
		return []SummaryDTO{}, nil
	}
	ownerIDs := make([]string, 0, len(records))
	assetIDs := make([]string, 0, len(records))
	ownerSeen := make(map[string]struct{})
	assetSeen := make(map[string]struct{})
	for _, record := range records {
		if _, exists := ownerSeen[record.OwnerID]; !exists {
			ownerSeen[record.OwnerID] = struct{}{}
			ownerIDs = append(ownerIDs, record.OwnerID)
		}
		if record.CoverAssetID != nil {
			if _, exists := assetSeen[*record.CoverAssetID]; !exists {
				assetSeen[*record.CoverAssetID] = struct{}{}
				assetIDs = append(assetIDs, *record.CoverAssetID)
			}
		}
	}
	owners, err := service.users.UserSummaries(ctx, ownerIDs)
	if err != nil {
		return nil, err
	}
	artworks, err := service.users.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make([]SummaryDTO, 0, len(records))
	for _, record := range records {
		owner, exists := owners[record.OwnerID]
		if !exists {
			return nil, fmt.Errorf("playlist owner %s was not presented", record.OwnerID)
		}
		var cover *ArtworkDTO
		if record.CoverAssetID != nil {
			if artwork, exists := artworks[*record.CoverAssetID]; exists {
				copy := artwork
				cover = &copy
			}
		}
		result = append(result, SummaryDTO{
			ID:          record.ID,
			Owner:       owner,
			Name:        record.Name,
			Description: record.Description,
			Visibility:  record.Visibility,
			Cover:       cover,
			TrackCount:  record.TrackCount,
			Version:     record.Version,
			CreatedAt:   formatTimestamp(record.CreatedAt),
			UpdatedAt:   formatTimestamp(record.UpdatedAt),
		})
	}
	return result, nil
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	var conflict *VersionConflictError
	switch {
	case errors.As(err, &conflict):
		return apperror.Conflict(apperror.CodeVersionConflict, "Playlist was modified by another client", map[string]any{
			"expectedVersion":      conflict.ExpectedVersion,
			"currentVersion":       conflict.CurrentVersion,
			"conflictResourceType": "PLAYLIST",
			"conflictResourceId":   conflict.PlaylistID,
		})
	case errors.Is(err, ErrNotFound):
		return apperror.NotFound("Playlist was not found")
	case errors.Is(err, ErrTrackNotFound):
		return apperror.NotFound("Track was not found")
	case errors.Is(err, ErrDuplicateTrack):
		return apperror.Conflict(apperror.CodeTrackAlreadyInPlaylist, "Track is already in this playlist", nil)
	case errors.Is(err, ErrPlaylistFull):
		return apperror.Unprocessable(apperror.CodeResourceConflict, "Playlist is full", nil)
	case errors.Is(err, ErrInsertAfterMissing):
		return apperror.NotFound("insertAfterEntryId was not found in the playlist")
	case errors.Is(err, ErrEntryNotFound):
		return apperror.NotFound("Playlist entry was not found")
	case errors.Is(err, ErrIncompleteOrder):
		return apperror.Unprocessable(apperror.CodeResourceConflict, "orderedEntryIds must contain every playlist entry", nil)
	case errors.Is(err, ErrUnknownOrderEntry):
		return apperror.Unprocessable(apperror.CodeResourceConflict, "orderedEntryIds contains an unknown entry", nil)
	default:
		return err
	}
}

func validateCreate(input CreateInput) (string, *string, Visibility, error) {
	name, err := validateName(input.Name)
	if err != nil {
		return "", nil, "", err
	}
	description, err := validateDescription(input.Description.Value)
	if err != nil {
		return "", nil, "", err
	}
	if !validVisibility(input.Visibility) {
		return "", nil, "", apperror.Validation("visibility is invalid")
	}
	return name, description, input.Visibility, nil
}

func validateName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	length := javascriptStringLength(trimmed)
	if length < 1 || length > 100 {
		return "", apperror.Validation("name must contain 1 to 100 characters")
	}
	return trimmed, nil
}

func validateDescription(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if javascriptStringLength(trimmed) > 1000 {
		return nil, apperror.Validation("description cannot exceed 1000 characters")
	}
	return &trimmed, nil
}

func validVisibility(value Visibility) bool {
	switch value {
	case VisibilityPrivate, VisibilityUnlisted, VisibilityPublic:
		return true
	default:
		return false
	}
}

func validSort(value Sort) bool {
	switch value {
	case SortUpdatedDesc, SortNameAsc, SortNameDesc:
		return true
	default:
		return false
	}
}

func pageLimit(value *int) (int, error) {
	if value == nil {
		return defaultPageLimit, nil
	}
	if *value < 1 || *value > maximumPageLimit {
		return 0, apperror.Validation("limit must be from 1 to 100")
	}
	return *value, nil
}

type playlistCursorValue struct {
	UpdatedAt *string `json:"updatedAt,omitempty"`
	Name      *string `json:"name,omitempty"`
	ID        string  `json:"id"`
}

type entryCursorValue struct {
	Position *int   `json:"position"`
	ID       string `json:"id"`
}

func (service *Service) decodePlaylistCursor(scope string, sort Sort, encoded string) (*PlaylistCursor, error) {
	value, err := pagination.DecodeCursor[playlistCursorValue](service.cursors, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.ID == "" {
		return nil, invalidCursor()
	}
	result := &PlaylistCursor{ID: value.ID, Name: value.Name}
	if sort == SortUpdatedDesc {
		if value.UpdatedAt == nil {
			return nil, invalidCursor()
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, *value.UpdatedAt)
		if err != nil {
			return nil, invalidCursor()
		}
		result.UpdatedAt = &updatedAt
	} else if value.Name == nil {
		return nil, invalidCursor()
	}
	return result, nil
}

func (service *Service) encodePlaylistCursor(scope string, sort Sort, record PlaylistRecord) (string, error) {
	value := playlistCursorValue{ID: record.ID}
	if sort == SortUpdatedDesc {
		updatedAt := formatTimestamp(record.UpdatedAt)
		value.UpdatedAt = &updatedAt
	} else {
		name := record.Name
		value.Name = &name
	}
	return pagination.EncodeCursor(service.cursors, scope, value)
}

func decodeEntryCursor(codec *pagination.CursorCodec, scope, encoded string) (*EntryCursor, error) {
	value, err := pagination.DecodeCursor[entryCursorValue](codec, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.Position == nil || *value.Position < 0 || value.ID == "" {
		return nil, invalidCursor()
	}
	return &EntryCursor{Position: *value.Position, ID: value.ID}, nil
}

func invalidCursor() error {
	return apperror.InvalidCursor("Cursor is invalid")
}

func presentVersion(mutation VersionMutation) VersionDTO {
	return VersionDTO{
		PlaylistID: mutation.PlaylistID,
		Version:    mutation.Version,
		UpdatedAt:  formatTimestamp(mutation.UpdatedAt),
	}
}

func firstPage[T any](items []T, limit int) ([]T, bool) {
	if len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}

func javascriptStringLength(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func marshalBody(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
