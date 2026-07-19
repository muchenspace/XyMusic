package admincatalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"xymusic/server/internal/modules/catalog"
)

func TestListArtistsAppliesLegacyDefaultsAndPresentsArtwork(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 456000000, time.UTC)
	assetID := "asset-1"
	var query ArtistQuery
	store := &catalogStoreStub{listArtists: func(_ context.Context, input ArtistQuery) ([]ArtistRecord, int, error) {
		query = input
		return []ArtistRecord{{
			ID: "artist-1", Name: "Artist", ArtworkAssetID: &assetID,
			AlbumCount: 2, TrackCount: 3, Version: 4, CreatedAt: now, UpdatedAt: now,
		}}, 1, nil
	}}
	service := newCatalogService(t, store, catalogArtworkStub{items: map[string]catalog.ArtworkDTO{
		assetID: {AssetID: assetID, URL: "signed", CacheKey: "key", MimeType: "image/jpeg"},
	}})
	result, err := service.ListArtists(context.Background(), ListInput{})
	if err != nil {
		t.Fatal(err)
	}
	if query.Sort != "name" || query.Order != SortAscending || query.Limit != 25 || query.Offset != 0 {
		t.Fatalf("query = %#v", query)
	}
	if len(result.Items) != 1 || result.Items[0].Artwork == nil || result.Items[0].Artwork.URL != "signed" ||
		result.Items[0].CreatedAt != "2026-07-16T01:02:03.456Z" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDuplicateAlbumsGroupsSortsAndDeduplicatesPrimaryArtists(t *testing.T) {
	now := time.Now().UTC()
	records := []AlbumRecord{
		{ID: "album-1", Title: "Same", NormalizedTitle: "same", TrackCount: 1, Version: 1, CreatedAt: now.Add(time.Hour), UpdatedAt: now,
			Credits: []CreditRecord{{ArtistID: "artist-1", ArtistName: "One", Role: "PRIMARY"}}},
		{ID: "album-2", Title: "Same", NormalizedTitle: "same", TrackCount: 4, Version: 1, CreatedAt: now, UpdatedAt: now,
			Credits: []CreditRecord{{ArtistID: "artist-1", ArtistName: "One", Role: "PRIMARY"}, {ArtistID: "artist-2", ArtistName: "Two", Role: "PRIMARY"}}},
	}
	var query DuplicateAlbumQuery
	store := &catalogStoreStub{duplicates: func(_ context.Context, input DuplicateAlbumQuery) (DuplicateAlbumPage, error) {
		query = input
		return DuplicateAlbumPage{
			Groups: []DuplicateAlbumGroupPage{{Key: "same", Title: "Same", Albums: records, AlbumTotal: 202}},
			Total:  1, GroupCount: 3, DuplicateAlbumCount: 5,
		}, nil
	}}
	service := newCatalogService(t, store, catalogArtworkStub{})
	result, err := service.DuplicateAlbums(context.Background(), DuplicateAlbumInput{
		AlbumID: "album-1", AlbumPage: 2, AlbumPageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GroupCount != 3 || result.DuplicateAlbumCount != 5 || result.Groups[0].Albums[0].ID != "album-2" ||
		result.Page != 1 || result.PageSize != 25 || result.Total != 1 || result.TotalPages != 1 ||
		result.Groups[0].AlbumPage != 2 || result.Groups[0].AlbumPageSize != 100 ||
		result.Groups[0].AlbumTotal != 202 || result.Groups[0].AlbumTotalPages != 3 {
		t.Fatalf("result = %#v", result)
	}
	if query.AlbumID != "album-1" || query.Limit != 25 || query.Offset != 0 ||
		query.AlbumLimit != 100 || query.AlbumOffset != 100 {
		t.Fatalf("query = %#v", query)
	}
	artists := result.Groups[0].PrimaryArtists
	if len(artists) != 2 || artists[0].ID != "artist-1" || artists[1].ID != "artist-2" {
		t.Fatalf("primary artists = %#v", artists)
	}
}

func TestAlbumPaginatesTracksAndReturnsTrackTotals(t *testing.T) {
	now := time.Now().UTC()
	var limit, offset int
	store := &catalogStoreStub{findAlbum: func(_ context.Context, _ string, pageLimit, pageOffset int) (AlbumRecord, []TrackRecord, int, error) {
		limit, offset = pageLimit, pageOffset
		return AlbumRecord{ID: "album-1", Title: "Album", Version: 1, CreatedAt: now, UpdatedAt: now},
			[]TrackRecord{{ID: "track-1", Title: "Track", Status: TrackStatusReady, AudioStatus: AudioStatusReady, Version: 1, CreatedAt: now, UpdatedAt: now}}, 52, nil
	}}
	service := newCatalogService(t, store, catalogArtworkStub{})
	result, err := service.Album(context.Background(), "album-1", PageInput{Page: 2, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if limit != 20 || offset != 20 || result.TrackPage != 2 || result.TrackPageSize != 20 ||
		result.TrackTotal != 52 || result.TrackTotalPages != 3 || len(result.Tracks) != 1 {
		t.Fatalf("query/result=%d/%d %#v", limit, offset, result)
	}
}

func TestTrackPresentsAdminOperationalProjection(t *testing.T) {
	now := time.Date(2026, 7, 16, 3, 4, 5, 0, time.UTC)
	albumID, albumTitle, coverID := "album-1", "Album", "cover-1"
	rootID, rootName, mode, rootStatus := "root-1", "Library", "READ_ONLY", "READY"
	rootEnabled := true
	errorMessage, errorCode := "worker failed with SQLSTATE 08000", "DEPENDENCY_UNAVAILABLE"
	metadataVersion := 2
	writebackID := "writeback-1"
	writebackErrorCode, writebackError := "WRITE_FAILED", "Tag writeback failed"
	content := "[00:00] line"
	record := TrackRecord{
		ID: "track-1", AlbumID: &albumID, AlbumTitle: &albumTitle, AlbumCoverAssetID: &coverID,
		Title: "Track", DurationMS: 123, Status: TrackStatusReady, AudioStatus: AudioStatusError, Version: 3,
		CreatedAt: now, UpdatedAt: now,
		Credits: []CreditRecord{
			{ArtistID: "artist-1", ArtistName: "Primary", Role: "PRIMARY"},
			{ArtistID: "artist-2", ArtistName: "Writer", Role: "COMPOSER"},
		},
		Source: &SourceRecord{
			ID: "source-1", RootID: &rootID, RootName: &rootName, RelativePath: `disc\song.flac`,
			Status: "READY", ChecksumSHA256: "sum", Mode: &mode, RootEnabled: &rootEnabled,
			RootStatus: &rootStatus, MappingCount: 1,
		},
		MetadataStatus: MetadataPendingWrite, MetadataVersion: &metadataVersion,
		MediaProcessing: &MediaProcessingRecord{Status: "FAILED", Attempts: 2, MaxAttempts: 5, LastError: &errorMessage, LastErrorCode: &errorCode, UpdatedAt: now},
		Variants: []VariantRecord{
			{ID: "variant-low", Quality: "LOW", MimeType: "audio/aac", Codec: "aac", Container: "m4a", Bitrate: 128, Status: "READY", UpdatedAt: now},
			{ID: "variant-high", Quality: "HIGH", MimeType: "audio/flac", Codec: "flac", Container: "flac", Bitrate: 900, Status: "READY", UpdatedAt: now},
		},
		ActiveWritebackJobID:     &writebackID,
		LatestWritebackErrorCode: &writebackErrorCode,
		LatestWritebackError:     &writebackError,
		Lyrics:                   []LyricRecord{{ID: "lyric-1", Language: "zh", Format: "LRC", Content: &content, IsDefault: true, Version: 1, UpdatedAt: now}},
	}
	store := &catalogStoreStub{findTrack: func(_ context.Context, id string, limit, offset int) (TrackRecord, int, error) {
		if id != "track-1" || limit != 1 || offset != 1 {
			t.Fatalf("track lyric query=%q/%d/%d", id, limit, offset)
		}
		return record, 3, nil
	}}
	service := newCatalogService(t, store, catalogArtworkStub{})
	result, err := service.Track(context.Background(), "track-1", PageInput{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Artists, []string{"Primary"}) || result.Source == nil || result.Source.Format == nil || *result.Source.Format != "FLAC" {
		t.Fatalf("track = %#v", result)
	}
	if result.Source.CanWriteBack || result.Source.WritebackBlockReason == nil || *result.Source.WritebackBlockReason != "The music source is read-only" {
		t.Fatalf("source writeback eligibility = %#v", result.Source)
	}
	if result.MediaProcessing == nil || result.MediaProcessing.LastError == nil || *result.MediaProcessing.LastError != "\u76f8\u5173\u5904\u7406\u670d\u52a1\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u914d\u7f6e\u540e\u91cd\u8bd5\u3002" {
		t.Fatalf("media processing = %#v", result.MediaProcessing)
	}
	if result.Status != string(TrackStatusReady) || result.AudioStatus != AudioStatusError {
		t.Fatalf("track states = raw %q audio %q", result.Status, result.AudioStatus)
	}
	if result.LatestWritebackErrorCode == nil || *result.LatestWritebackErrorCode != writebackErrorCode ||
		result.LatestWritebackError == nil || *result.LatestWritebackError != writebackError {
		t.Fatalf("latest writeback error = %v / %v", result.LatestWritebackErrorCode, result.LatestWritebackError)
	}
	if len(result.Variants) != 2 || result.Variants[0].ID != "variant-high" || len(result.VariantSummary) != 2 {
		t.Fatalf("variants = %#v / %#v", result.Variants, result.VariantSummary)
	}
	if result.LyricPage != 2 || result.LyricPageSize != 1 || result.LyricTotal != 3 || result.LyricTotalPages != 3 || len(result.Lyrics) != 1 {
		t.Fatalf("lyric page = %#v", result)
	}
}

func TestListTracksFiltersByAudioStatus(t *testing.T) {
	var query TrackQuery
	store := &catalogStoreStub{listTracks: func(_ context.Context, input TrackQuery) ([]TrackRecord, int, error) {
		query = input
		return []TrackRecord{}, 0, nil
	}}
	service := newCatalogService(t, store, catalogArtworkStub{})
	result, err := service.ListTracks(context.Background(), TrackListInput{Status: AudioStatusProcessing})
	if err != nil {
		t.Fatal(err)
	}
	if query.Status != AudioStatusProcessing || result.Items == nil {
		t.Fatalf("query/result = %#v / %#v", query, result)
	}
	if _, err := service.ListTracks(context.Background(), TrackListInput{Status: AudioStatus("PENDING")}); err == nil {
		t.Fatal("PENDING must not be accepted as a public audio status")
	}
}

func newCatalogService(t *testing.T, store Store, artworks ArtworkPresenter) *Service {
	t.Helper()
	service, err := NewService(store, artworks)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type catalogArtworkStub struct {
	items map[string]catalog.ArtworkDTO
	err   error
}

func (stub catalogArtworkStub) Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error) {
	if stub.items == nil {
		stub.items = map[string]catalog.ArtworkDTO{}
	}
	return stub.items, stub.err
}

type catalogStoreStub struct {
	listArtists func(context.Context, ArtistQuery) ([]ArtistRecord, int, error)
	findArtist  func(context.Context, string) (ArtistRecord, error)
	listAlbums  func(context.Context, AlbumQuery) ([]AlbumRecord, int, error)
	duplicates  func(context.Context, DuplicateAlbumQuery) (DuplicateAlbumPage, error)
	findAlbum   func(context.Context, string, int, int) (AlbumRecord, []TrackRecord, int, error)
	listTracks  func(context.Context, TrackQuery) ([]TrackRecord, int, error)
	findTrack   func(context.Context, string, int, int) (TrackRecord, int, error)
}

func (stub *catalogStoreStub) ListArtists(ctx context.Context, query ArtistQuery) ([]ArtistRecord, int, error) {
	if stub.listArtists == nil {
		return nil, 0, errors.New("unexpected ListArtists call")
	}
	return stub.listArtists(ctx, query)
}
func (stub *catalogStoreStub) FindArtist(ctx context.Context, id string) (ArtistRecord, error) {
	if stub.findArtist == nil {
		return ArtistRecord{}, errors.New("unexpected FindArtist call")
	}
	return stub.findArtist(ctx, id)
}
func (stub *catalogStoreStub) ListAlbums(ctx context.Context, query AlbumQuery) ([]AlbumRecord, int, error) {
	if stub.listAlbums == nil {
		return nil, 0, errors.New("unexpected ListAlbums call")
	}
	return stub.listAlbums(ctx, query)
}
func (stub *catalogStoreStub) FindDuplicateAlbums(ctx context.Context, query DuplicateAlbumQuery) (DuplicateAlbumPage, error) {
	if stub.duplicates == nil {
		return DuplicateAlbumPage{}, errors.New("unexpected FindDuplicateAlbums call")
	}
	return stub.duplicates(ctx, query)
}
func (stub *catalogStoreStub) FindAlbum(ctx context.Context, id string, limit, offset int) (AlbumRecord, []TrackRecord, int, error) {
	if stub.findAlbum == nil {
		return AlbumRecord{}, nil, 0, errors.New("unexpected FindAlbum call")
	}
	return stub.findAlbum(ctx, id, limit, offset)
}
func (stub *catalogStoreStub) ListTracks(ctx context.Context, query TrackQuery) ([]TrackRecord, int, error) {
	if stub.listTracks == nil {
		return nil, 0, errors.New("unexpected ListTracks call")
	}
	return stub.listTracks(ctx, query)
}
func (stub *catalogStoreStub) FindTrack(ctx context.Context, id string, limit, offset int) (TrackRecord, int, error) {
	if stub.findTrack == nil {
		return TrackRecord{}, 0, errors.New("unexpected FindTrack call")
	}
	return stub.findTrack(ctx, id, limit, offset)
}
