package playlist

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

func TestListOwnedPresentsSummaryAndUsesOwnerScopedCursor(t *testing.T) {
	now := time.Date(2026, 7, 16, 9, 10, 11, 123_000_000, time.UTC)
	coverID := "asset-1"
	calls := 0
	store := &storeStub{
		listOwned: func(_ context.Context, query ListOwnedQuery) ([]PlaylistRecord, error) {
			calls++
			if query.OwnerID != "owner-1" || query.Sort != SortUpdatedDesc || query.Limit != 2 {
				t.Fatalf("list query = %#v", query)
			}
			if calls == 2 {
				if query.After == nil || query.After.UpdatedAt == nil || !query.After.UpdatedAt.Equal(now) || query.After.ID != "playlist-1" {
					t.Fatalf("decoded cursor = %#v", query.After)
				}
				return []PlaylistRecord{}, nil
			}
			return []PlaylistRecord{
				{ID: "playlist-1", OwnerID: "owner-1", Name: "One", Visibility: VisibilityPrivate, CoverAssetID: &coverID, TrackCount: 3, Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
				{ID: "playlist-2", OwnerID: "owner-1", Name: "Two", Visibility: VisibilityPublic, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)},
			}, nil
		},
	}
	service := newPlaylistService(t, store)
	limit := 1
	input := ListOwnedInput{Sort: SortUpdatedDesc, Limit: &limit}
	first, err := service.ListOwned(context.Background(), "owner-1", input)
	if err != nil {
		t.Fatalf("ListOwned() error = %v", err)
	}
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("ListOwned() = %#v", first)
	}
	item := first.Items[0]
	if item.Owner.ID != "owner-1" || item.Cover == nil || item.Cover.AssetID != coverID || item.TrackCount != 3 || item.UpdatedAt != "2026-07-16T09:10:11.123Z" {
		t.Fatalf("summary = %#v", item)
	}
	input.Cursor = *first.NextCursor
	if _, err := service.ListOwned(context.Background(), "owner-1", input); err != nil {
		t.Fatalf("cursor ListOwned() error = %v", err)
	}
	if _, err := service.ListOwned(context.Background(), "another-owner", input); !apperror.IsCode(err, apperror.CodeInvalidCursor) {
		t.Fatalf("cross-owner cursor error = %v", err)
	}
}

func TestGetEnforcesPrivateVisibilityAndMapsOnlyPresentableEntries(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	privateStore := &storeStub{
		findPlaylist: func(context.Context, string) (PlaylistRecord, error) {
			return PlaylistRecord{ID: "playlist-1", OwnerID: "owner-1", Visibility: VisibilityPrivate}, nil
		},
	}
	privateService := newPlaylistService(t, privateStore)
	if _, err := privateService.Get(context.Background(), "viewer-1", "playlist-1", GetInput{}); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("private playlist error = %v", err)
	}

	store := &storeStub{
		findPlaylist: func(context.Context, string) (PlaylistRecord, error) {
			return PlaylistRecord{
				ID: "playlist-1", OwnerID: "owner-1", Name: "Public", Visibility: VisibilityPublic,
				Version: 4, CreatedAt: now, UpdatedAt: now,
			}, nil
		},
		listEntries: func(_ context.Context, query ListEntriesQuery) ([]EntryRecord, error) {
			if query.Limit != 2 {
				t.Fatalf("entry query = %#v", query)
			}
			return []EntryRecord{
				{ID: "entry-1", PlaylistID: "playlist-1", TrackID: "track-1", Position: 0, AddedBy: "owner-1", AddedAt: now},
				{ID: "entry-2", PlaylistID: "playlist-1", TrackID: "track-hidden", Position: 1, AddedBy: "owner-1", AddedAt: now},
			}, nil
		},
	}
	service := newPlaylistService(t, store)
	limit := 1
	result, err := service.Get(context.Background(), "viewer-1", "playlist-1", GetInput{Limit: &limit})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Track.ID != "track-1" || result.NextCursor == nil {
		t.Fatalf("Get() = %#v", result)
	}
}

func TestCreateAndUpdateNormalizeMetadataAndPreserveExplicitNull(t *testing.T) {
	now := time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	store := &storeStub{
		createPlaylist: func(_ context.Context, params CreatePlaylistParams) (PlaylistRecord, error) {
			if params.Name != "Road Trip" || params.Description == nil || *params.Description != "songs" || params.Visibility != VisibilityUnlisted {
				t.Fatalf("create params = %#v", params)
			}
			return PlaylistRecord{ID: "playlist-1", OwnerID: params.OwnerID, Name: params.Name, Description: params.Description, Visibility: params.Visibility, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
		},
		updatePlaylist: func(_ context.Context, params UpdatePlaylistParams) (PlaylistRecord, error) {
			if params.Name == nil || *params.Name != "Renamed" || !params.SetDescription || params.Description != nil || params.Visibility == nil || *params.Visibility != VisibilityPublic {
				t.Fatalf("update params = %#v", params)
			}
			return PlaylistRecord{ID: params.PlaylistID, OwnerID: params.OwnerID, Name: *params.Name, Visibility: *params.Visibility, Version: 3, CreatedAt: now, UpdatedAt: now}, nil
		},
	}
	service := newPlaylistService(t, store)
	description := " songs "
	created, err := service.Create(context.Background(), "owner-1", CreateInput{
		Name: " Road Trip ", Description: OptionalNullableString{Set: true, Value: &description}, Visibility: VisibilityUnlisted,
	})
	if err != nil || created.Name != "Road Trip" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	updated, err := service.Update(context.Background(), "owner-1", "playlist-1", UpdateInput{
		ExpectedVersion: 2,
		Name:            OptionalString{Set: true, Value: " Renamed "},
		Description:     OptionalNullableString{Set: true, Value: nil},
		Visibility:      OptionalVisibility{Set: true, Value: VisibilityPublic},
	})
	if err != nil || updated.Version != 3 || updated.Description != nil {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
}

func TestAddTrackPresentsMutationAndMapsDomainErrors(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &storeStub{
		readyTrackExists: func(context.Context, string) (bool, error) { return true, nil },
		addTrack: func(_ context.Context, params AddTrackParams) (AddTrackMutation, error) {
			if params.InsertAfterEntryID == nil || *params.InsertAfterEntryID != "entry-before" || params.ExpectedVersion != 2 {
				t.Fatalf("add params = %#v", params)
			}
			return AddTrackMutation{
				PlaylistID: params.PlaylistID, Version: 3, UpdatedAt: now,
				Entry: EntryRecord{ID: "entry-2", PlaylistID: params.PlaylistID, TrackID: params.TrackID, Position: 1, AddedBy: params.OwnerID, AddedAt: now},
			}, nil
		},
	}
	service := newPlaylistService(t, store)
	after := "entry-before"
	result, err := service.AddTrack(context.Background(), "owner-1", "playlist-1", AddTrackInput{
		ExpectedVersion: 2, TrackID: "track-1", InsertAfterEntryID: OptionalUUID{Set: true, Value: &after},
	})
	if err != nil || result.Version != 3 || result.Entry.Track.ID != "track-1" || result.Entry.AddedBy.ID != "owner-1" {
		t.Fatalf("AddTrack() = %#v, %v", result, err)
	}

	for _, test := range []struct {
		err  error
		code apperror.Code
	}{
		{ErrDuplicateTrack, apperror.CodeTrackAlreadyInPlaylist},
		{ErrPlaylistFull, apperror.CodeResourceConflict},
		{ErrInsertAfterMissing, apperror.CodeResourceNotFound},
		{versionConflict("playlist-1", 2, 3), apperror.CodeVersionConflict},
	} {
		if mapped := mapStoreError(test.err); !apperror.IsCode(mapped, test.code) {
			t.Fatalf("mapStoreError(%v) = %v", test.err, mapped)
		}
	}
}

func TestReorderRequiresUniqueCompleteListAndPassesExactOrder(t *testing.T) {
	store := &storeStub{
		reorder: func(_ context.Context, params ReorderParams) (VersionMutation, error) {
			if !reflect.DeepEqual(params.OrderedEntryIDs, []string{"entry-2", "entry-1"}) {
				t.Fatalf("ordered ids = %#v", params.OrderedEntryIDs)
			}
			return VersionMutation{PlaylistID: params.PlaylistID, Version: 5, UpdatedAt: params.Now}, nil
		},
	}
	service := newPlaylistService(t, store)
	result, err := service.Reorder(context.Background(), "owner-1", "playlist-1", ReorderInput{
		ExpectedVersion: 4,
		OrderedEntryIDs: RequiredStringSlice{Set: true, Values: []string{"entry-2", "entry-1"}},
	})
	if err != nil || result.Version != 5 {
		t.Fatalf("Reorder() = %#v, %v", result, err)
	}
	_, err = service.Reorder(context.Background(), "owner-1", "playlist-1", ReorderInput{
		ExpectedVersion: 4,
		OrderedEntryIDs: RequiredStringSlice{Set: true, Values: []string{"entry-1", "entry-1"}},
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("duplicate reorder error = %v", err)
	}
}

func newPlaylistService(t *testing.T, store Store) *Service {
	t.Helper()
	service, err := NewService(ServiceDependencies{
		Repository: store,
		Cursors:    pagination.NewCursorCodec("playlist-test-secret"),
		Catalog:    catalogPresenterStub{},
		Users:      userPresenterStub{},
		Clock:      playlistFixedClock{value: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

type playlistFixedClock struct{ value time.Time }

func (clock playlistFixedClock) Now() time.Time { return clock.value }

type catalogPresenterStub struct{}

func (catalogPresenterStub) TrackSummaries(_ context.Context, _ string, trackIDs []string) ([]catalog.TrackSummaryDTO, error) {
	result := make([]catalog.TrackSummaryDTO, 0, len(trackIDs))
	for _, id := range trackIDs {
		if id == "track-hidden" {
			continue
		}
		result = append(result, catalog.TrackSummaryDTO{ID: id, Title: "Track " + id, Artists: []catalog.ArtistReferenceDTO{}})
	}
	return result, nil
}

type userPresenterStub struct{}

func (userPresenterStub) UserSummary(_ context.Context, userID string) (UserSummaryDTO, error) {
	return UserSummaryDTO{ID: userID, Username: "user", DisplayName: "User"}, nil
}

func (presenter userPresenterStub) UserSummaries(ctx context.Context, userIDs []string) (map[string]UserSummaryDTO, error) {
	result := make(map[string]UserSummaryDTO)
	for _, id := range userIDs {
		user, _ := presenter.UserSummary(ctx, id)
		result[id] = user
	}
	return result, nil
}

func (userPresenterStub) Artworks(_ context.Context, assetIDs []string) (map[string]ArtworkDTO, error) {
	result := make(map[string]ArtworkDTO)
	for _, id := range assetIDs {
		result[id] = ArtworkDTO{AssetID: id, URL: "https://media.example/" + id, CacheKey: id + ":hash", MimeType: "image/jpeg"}
	}
	return result, nil
}

type storeStub struct {
	listOwned        func(context.Context, ListOwnedQuery) ([]PlaylistRecord, error)
	createPlaylist   func(context.Context, CreatePlaylistParams) (PlaylistRecord, error)
	findPlaylist     func(context.Context, string) (PlaylistRecord, error)
	listEntries      func(context.Context, ListEntriesQuery) ([]EntryRecord, error)
	updatePlaylist   func(context.Context, UpdatePlaylistParams) (PlaylistRecord, error)
	deletePlaylist   func(context.Context, string, string, int) error
	readyTrackExists func(context.Context, string) (bool, error)
	addTrack         func(context.Context, AddTrackParams) (AddTrackMutation, error)
	removeTrack      func(context.Context, RemoveTrackParams) (VersionMutation, error)
	reorder          func(context.Context, ReorderParams) (VersionMutation, error)
}

func (store *storeStub) ListOwned(ctx context.Context, query ListOwnedQuery) ([]PlaylistRecord, error) {
	if store.listOwned == nil {
		return nil, errors.New("unexpected ListOwned call")
	}
	return store.listOwned(ctx, query)
}

func (store *storeStub) CreatePlaylist(ctx context.Context, params CreatePlaylistParams) (PlaylistRecord, error) {
	if store.createPlaylist == nil {
		return PlaylistRecord{}, errors.New("unexpected CreatePlaylist call")
	}
	return store.createPlaylist(ctx, params)
}

func (store *storeStub) FindPlaylist(ctx context.Context, id string) (PlaylistRecord, error) {
	if store.findPlaylist == nil {
		return PlaylistRecord{}, errors.New("unexpected FindPlaylist call")
	}
	return store.findPlaylist(ctx, id)
}

func (store *storeStub) ListEntries(ctx context.Context, query ListEntriesQuery) ([]EntryRecord, error) {
	if store.listEntries == nil {
		return nil, errors.New("unexpected ListEntries call")
	}
	return store.listEntries(ctx, query)
}

func (store *storeStub) UpdatePlaylist(ctx context.Context, params UpdatePlaylistParams) (PlaylistRecord, error) {
	if store.updatePlaylist == nil {
		return PlaylistRecord{}, errors.New("unexpected UpdatePlaylist call")
	}
	return store.updatePlaylist(ctx, params)
}

func (store *storeStub) DeletePlaylist(ctx context.Context, ownerID, playlistID string, version int) error {
	if store.deletePlaylist == nil {
		return errors.New("unexpected DeletePlaylist call")
	}
	return store.deletePlaylist(ctx, ownerID, playlistID, version)
}

func (store *storeStub) ReadyTrackExists(ctx context.Context, trackID string) (bool, error) {
	if store.readyTrackExists == nil {
		return false, errors.New("unexpected ReadyTrackExists call")
	}
	return store.readyTrackExists(ctx, trackID)
}

func (store *storeStub) AddTrack(ctx context.Context, params AddTrackParams) (AddTrackMutation, error) {
	if store.addTrack == nil {
		return AddTrackMutation{}, errors.New("unexpected AddTrack call")
	}
	return store.addTrack(ctx, params)
}

func (store *storeStub) RemoveTrack(ctx context.Context, params RemoveTrackParams) (VersionMutation, error) {
	if store.removeTrack == nil {
		return VersionMutation{}, errors.New("unexpected RemoveTrack call")
	}
	return store.removeTrack(ctx, params)
}

func (store *storeStub) Reorder(ctx context.Context, params ReorderParams) (VersionMutation, error) {
	if store.reorder == nil {
		return VersionMutation{}, errors.New("unexpected Reorder call")
	}
	return store.reorder(ctx, params)
}
