package catalog

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

func TestListTracksUsesScopedCursorAndPresentsLegacyShape(t *testing.T) {
	published := time.Date(2026, 7, 15, 12, 34, 56, 789_000_000, time.UTC)
	updated := time.Date(2026, 7, 1, 1, 2, 3, 456_000_000, time.UTC)
	checksum := "abc123"
	trackNumber := 4
	discNumber := 2
	calls := 0
	store := &storeStub{
		listTracks: func(_ context.Context, query ListTracksQuery) ([]TrackRecord, error) {
			calls++
			if query.UserID != "user-1" || query.Sort != TrackSortPublishedDesc || query.ArtistID != "artist-1" || query.AlbumID != "album-1" || query.Limit != 2 {
				t.Fatalf("unexpected track query: %#v", query)
			}
			if calls == 2 {
				if query.After == nil || query.After.PublishedAt == nil || !query.After.PublishedAt.Equal(published) || query.After.ID != "track-1" {
					t.Fatalf("decoded cursor = %#v", query.After)
				}
				return []TrackRecord{}, nil
			}
			if query.After != nil {
				t.Fatalf("first query unexpectedly had cursor: %#v", query.After)
			}
			return []TrackRecord{
				{
					ID:              "track-1",
					Title:           "Song",
					NormalizedTitle: "song",
					Artists:         []ArtistReferenceRecord{{ID: "artist-1", Name: "Artist"}},
					Album:           &AlbumReferenceRecord{ID: "album-1", Title: "Album"},
					Artwork: &ArtworkAsset{
						ID:             "asset-1",
						ObjectKey:      "covers/one.jpg",
						MimeType:       "image/jpeg",
						ChecksumSHA256: &checksum,
						Width:          intPointer(600),
						Height:         intPointer(600),
						UpdatedAt:      updated,
					},
					DurationMS:  123456,
					TrackNumber: &trackNumber,
					DiscNumber:  &discNumber,
					Favorite:    true,
					PublishedAt: published,
				},
				{ID: "track-2", NormalizedTitle: "track 2", PublishedAt: published.Add(-time.Hour)},
			}, nil
		},
	}
	signer := &signerStub{}
	service := newTestService(t, store, signer)
	limit := 1
	input := ListTracksInput{
		Sort: TrackSortPublishedDesc, ArtistID: "artist-1", AlbumID: "album-1", Limit: &limit,
	}
	result, err := service.ListTracks(context.Background(), "user-1", input)
	if err != nil {
		t.Fatalf("ListTracks() error = %v", err)
	}
	if len(result.Items) != 1 || result.NextCursor == nil {
		t.Fatalf("ListTracks() = %#v", result)
	}
	item := result.Items[0]
	if item.ID != "track-1" || item.Album == nil || item.Album.ID != "album-1" || !item.IsFavorite || item.DiscNumber != 2 {
		t.Fatalf("track item = %#v", item)
	}
	if item.PublishedAt != "2026-07-15T12:34:56.789Z" || item.Artwork == nil || item.Artwork.CacheKey != "asset-1:abc123" {
		t.Fatalf("presented track = %#v", item)
	}
	if item.Artwork.ExpiresAt == nil || *item.Artwork.ExpiresAt != "2026-07-16T08:05:00.000Z" {
		t.Fatalf("artwork expiry = %#v", item.Artwork)
	}
	input.Cursor = *result.NextCursor
	if _, err := service.ListTracks(context.Background(), "user-1", input); err != nil {
		t.Fatalf("second ListTracks() error = %v", err)
	}
	input.AlbumID = "different-album"
	if _, err := service.ListTracks(context.Background(), "user-1", input); !apperror.IsCode(err, apperror.CodeInvalidCursor) {
		t.Fatalf("cross-scope cursor error = %v", err)
	}
	if signer.calls != 1 {
		t.Fatalf("artwork signer calls = %d", signer.calls)
	}
}

func TestListAlbumsRoundTripsNullReleaseDateCursor(t *testing.T) {
	calls := 0
	store := &storeStub{
		listAlbums: func(_ context.Context, query ListAlbumsQuery) ([]AlbumRecord, error) {
			calls++
			if calls == 1 {
				return []AlbumRecord{
					{ID: "album-null", Title: "Unknown", NormalizedTitle: "unknown", Artists: []ArtistReferenceRecord{}},
					{ID: "album-next", Title: "Next", NormalizedTitle: "next", Artists: []ArtistReferenceRecord{}},
				}, nil
			}
			if query.After == nil || !query.After.NullRelease || query.After.ReleaseDate != nil || query.After.ID != "album-null" {
				t.Fatalf("null release cursor = %#v", query.After)
			}
			return []AlbumRecord{}, nil
		},
	}
	service := newTestService(t, store, &signerStub{})
	limit := 1
	first, err := service.ListAlbums(context.Background(), ListAlbumsInput{Sort: AlbumSortReleaseDateDesc, Limit: &limit})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first ListAlbums() = %#v, %v", first, err)
	}
	_, err = service.ListAlbums(context.Background(), ListAlbumsInput{
		Sort: AlbumSortReleaseDateDesc, Limit: &limit, Cursor: *first.NextCursor,
	})
	if err != nil {
		t.Fatalf("second ListAlbums() error = %v", err)
	}
}

func TestListArtistsCursorUsesLegacyNameField(t *testing.T) {
	calls := 0
	store := &storeStub{
		listArtists: func(_ context.Context, query ListArtistsQuery) ([]ArtistRecord, error) {
			calls++
			if calls == 1 {
				return []ArtistRecord{
					{ID: "artist-1", Name: "Artist", NormalizedName: "artist"},
					{ID: "artist-2", Name: "Other", NormalizedName: "other"},
				}, nil
			}
			if query.After == nil || query.After.Value != "artist" || query.After.ID != "artist-1" {
				t.Fatalf("artist cursor = %#v", query.After)
			}
			return []ArtistRecord{}, nil
		},
	}
	service := newTestService(t, store, &signerStub{})
	limit := 1
	first, err := service.ListArtists(context.Background(), ListArtistsInput{Sort: ArtistSortNameAsc, Limit: &limit})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first ListArtists() = %#v, %v", first, err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(*first.NextCursor, ".")[0])
	if err != nil || !strings.Contains(string(payload), `"value":{"name":"artist","id":"artist-1"}`) {
		t.Fatalf("artist cursor payload = %s, error = %v", payload, err)
	}
	if _, err := service.ListArtists(context.Background(), ListArtistsInput{
		Sort: ArtistSortNameAsc, Limit: &limit, Cursor: *first.NextCursor,
	}); err != nil {
		t.Fatalf("second ListArtists() error = %v", err)
	}
}

func TestRandomTracksWrapsAtAnchorWithRemainingLimit(t *testing.T) {
	queries := make([]struct {
		atOrAfter bool
		limit     int
	}, 0, 2)
	store := &storeStub{
		randomTracks: func(_ context.Context, userID string, anchor float64, atOrAfter bool, limit int) ([]TrackRecord, error) {
			if userID != "user-1" || anchor != 0.75 {
				t.Fatalf("random query user/anchor = %q/%v", userID, anchor)
			}
			queries = append(queries, struct {
				atOrAfter bool
				limit     int
			}{atOrAfter, limit})
			if atOrAfter {
				return []TrackRecord{{ID: "high", PublishedAt: time.Now()}}, nil
			}
			return []TrackRecord{{ID: "low-1", PublishedAt: time.Now()}, {ID: "low-2", PublishedAt: time.Now()}}, nil
		},
	}
	service := newTestServiceWithRandom(t, store, &signerStub{}, func() float64 { return 0.75 })
	result, err := service.RandomTracks(context.Background(), "user-1", 3)
	if err != nil {
		t.Fatalf("RandomTracks() error = %v", err)
	}
	if len(result.Items) != 3 || !reflect.DeepEqual(queries, []struct {
		atOrAfter bool
		limit     int
	}{{true, 3}, {false, 2}}) {
		t.Fatalf("RandomTracks() = %#v, queries = %#v", result, queries)
	}
}

func TestGetTrackReturnsLyricsWithTrackVersion(t *testing.T) {
	updated := time.Date(2026, 1, 2, 3, 4, 5, 678_000_000, time.UTC)
	store := &storeStub{
		findTrack: func(_ context.Context, userID, trackID string) (TrackRecord, error) {
			if userID != "user-1" || trackID != "track-1" {
				t.Fatalf("find track args = %q/%q", userID, trackID)
			}
			return TrackRecord{ID: trackID, Title: "Song", PublishedAt: updated, Version: 7, Artists: []ArtistReferenceRecord{}}, nil
		},
		listLyrics: func(_ context.Context, query ListLyricsQuery) ([]LyricRecord, int, error) {
			if query.TrackID != "track-1" || query.Limit != 20 || query.Offset != 0 {
				t.Fatalf("lyric query = %#v", query)
			}
			return []LyricRecord{{
				ID: "lyric-1", TrackID: query.TrackID, Language: "zh-CN", Format: "LRC",
				Content: "[00:00]hello", IsDefault: true, UpdatedAt: updated,
			}}, 41, nil
		},
	}
	service := newTestService(t, store, &signerStub{})
	result, err := service.GetTrack(context.Background(), "user-1", "track-1", GetTrackInput{})
	if err != nil {
		t.Fatalf("GetTrack() error = %v", err)
	}
	if len(result.Lyrics) != 1 || result.Lyrics[0].TrackVersion != 7 || result.Lyrics[0].UpdatedAt != "2026-01-02T03:04:05.678Z" ||
		result.LyricPage != 1 || result.LyricPageSize != 20 || result.LyricTotal != 41 || result.LyricTotalPages != 3 {
		t.Fatalf("GetTrack() = %#v", result)
	}
}

func TestSearchAllNormalizesQueryAndIgnoresRequestedLimit(t *testing.T) {
	assertQuery := func(query SearchQuery) {
		if query.NormalizedQuery != `a%_\` || query.Pattern != `%a\%\_\\%` || !query.UseTrigram || query.Limit != 6 {
			t.Fatalf("search query = %#v", query)
		}
	}
	store := &storeStub{
		searchTracks: func(_ context.Context, query SearchQuery) ([]TrackRecord, error) {
			if query.UserID != "user-1" {
				t.Fatalf("track search user = %q", query.UserID)
			}
			assertQuery(query)
			return []TrackRecord{}, nil
		},
		searchArtists: func(_ context.Context, query SearchQuery) ([]ArtistRecord, error) {
			assertQuery(query)
			return []ArtistRecord{}, nil
		},
		searchAlbums: func(_ context.Context, query SearchQuery) ([]AlbumRecord, error) {
			assertQuery(query)
			return []AlbumRecord{}, nil
		},
	}
	service := newTestService(t, store, &signerStub{})
	ignoredLimit := 101
	result, err := service.Search(context.Background(), "user-1", SearchInput{
		Query: " Ａ%_\\ ", Scope: SearchScopeAll, Limit: &ignoredLimit,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Query != `Ａ%_\` || result.Tracks == nil || result.Artists == nil || result.Albums == nil {
		t.Fatalf("Search() = %#v", result)
	}
}

func TestGetCatalogDetailsMapMissingRowsToNotFound(t *testing.T) {
	store := &storeStub{
		findTrack:  func(context.Context, string, string) (TrackRecord, error) { return TrackRecord{}, ErrNotFound },
		findArtist: func(context.Context, string) (ArtistRecord, error) { return ArtistRecord{}, ErrNotFound },
		findAlbum:  func(context.Context, string) (AlbumRecord, error) { return AlbumRecord{}, ErrNotFound },
	}
	service := newTestService(t, store, &signerStub{})
	if _, err := service.GetTrack(context.Background(), "user", "track", GetTrackInput{}); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("track error = %v", err)
	}
	if _, err := service.GetArtist(context.Background(), "artist"); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("artist error = %v", err)
	}
	if _, err := service.GetAlbum(context.Background(), "album"); !apperror.IsCode(err, apperror.CodeResourceNotFound) {
		t.Fatalf("album error = %v", err)
	}
}

func newTestService(t *testing.T, store Store, signer ArtworkURLSigner) *Service {
	return newTestServiceWithRandom(t, store, signer, func() float64 { return 0.5 })
}

func newTestServiceWithRandom(t *testing.T, store Store, signer ArtworkURLSigner, random func() float64) *Service {
	t.Helper()
	service, err := NewService(ServiceDependencies{
		Repository:    store,
		Cursors:       pagination.NewCursorCodec("catalog-test-secret"),
		ArtworkURLs:   signer,
		ArtworkURLTTL: 5 * time.Minute,
		Clock:         fixedClock{value: time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)},
		RandomFloat64: random,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type signerStub struct {
	calls int
	err   error
}

func (signer *signerStub) PresignedGet(_ context.Context, objectKey string, _ time.Duration) (string, error) {
	signer.calls++
	if signer.err != nil {
		return "", signer.err
	}
	return "https://media.example/" + objectKey, nil
}

type storeStub struct {
	listTracks    func(context.Context, ListTracksQuery) ([]TrackRecord, error)
	randomTracks  func(context.Context, string, float64, bool, int) ([]TrackRecord, error)
	findTracks    func(context.Context, string, []string) ([]TrackRecord, error)
	findTrack     func(context.Context, string, string) (TrackRecord, error)
	listLyrics    func(context.Context, ListLyricsQuery) ([]LyricRecord, int, error)
	listArtists   func(context.Context, ListArtistsQuery) ([]ArtistRecord, error)
	findArtist    func(context.Context, string) (ArtistRecord, error)
	listAlbums    func(context.Context, ListAlbumsQuery) ([]AlbumRecord, error)
	randomAlbums  func(context.Context, float64, bool, int) ([]AlbumRecord, error)
	findAlbum     func(context.Context, string) (AlbumRecord, error)
	searchTracks  func(context.Context, SearchQuery) ([]TrackRecord, error)
	searchArtists func(context.Context, SearchQuery) ([]ArtistRecord, error)
	searchAlbums  func(context.Context, SearchQuery) ([]AlbumRecord, error)
}

func (store *storeStub) ListTracks(ctx context.Context, query ListTracksQuery) ([]TrackRecord, error) {
	if store.listTracks == nil {
		return nil, errors.New("unexpected ListTracks call")
	}
	return store.listTracks(ctx, query)
}

func (store *storeStub) RandomTracks(ctx context.Context, userID string, anchor float64, atOrAfter bool, limit int) ([]TrackRecord, error) {
	if store.randomTracks == nil {
		return nil, errors.New("unexpected RandomTracks call")
	}
	return store.randomTracks(ctx, userID, anchor, atOrAfter, limit)
}

func (store *storeStub) FindTracks(ctx context.Context, userID string, trackIDs []string) ([]TrackRecord, error) {
	if store.findTracks == nil {
		return nil, errors.New("unexpected FindTracks call")
	}
	return store.findTracks(ctx, userID, trackIDs)
}

func (store *storeStub) FindTrack(ctx context.Context, userID, trackID string) (TrackRecord, error) {
	if store.findTrack == nil {
		return TrackRecord{}, errors.New("unexpected FindTrack call")
	}
	return store.findTrack(ctx, userID, trackID)
}

func (store *storeStub) ListLyrics(ctx context.Context, query ListLyricsQuery) ([]LyricRecord, int, error) {
	if store.listLyrics == nil {
		return nil, 0, errors.New("unexpected ListLyrics call")
	}
	return store.listLyrics(ctx, query)
}

func (store *storeStub) ListArtists(ctx context.Context, query ListArtistsQuery) ([]ArtistRecord, error) {
	if store.listArtists == nil {
		return nil, errors.New("unexpected ListArtists call")
	}
	return store.listArtists(ctx, query)
}

func (store *storeStub) FindArtist(ctx context.Context, artistID string) (ArtistRecord, error) {
	if store.findArtist == nil {
		return ArtistRecord{}, errors.New("unexpected FindArtist call")
	}
	return store.findArtist(ctx, artistID)
}

func (store *storeStub) ListAlbums(ctx context.Context, query ListAlbumsQuery) ([]AlbumRecord, error) {
	if store.listAlbums == nil {
		return nil, errors.New("unexpected ListAlbums call")
	}
	return store.listAlbums(ctx, query)
}

func (store *storeStub) RandomAlbums(ctx context.Context, anchor float64, atOrAfter bool, limit int) ([]AlbumRecord, error) {
	if store.randomAlbums == nil {
		return nil, errors.New("unexpected RandomAlbums call")
	}
	return store.randomAlbums(ctx, anchor, atOrAfter, limit)
}

func (store *storeStub) FindAlbum(ctx context.Context, albumID string) (AlbumRecord, error) {
	if store.findAlbum == nil {
		return AlbumRecord{}, errors.New("unexpected FindAlbum call")
	}
	return store.findAlbum(ctx, albumID)
}

func (store *storeStub) SearchTracks(ctx context.Context, query SearchQuery) ([]TrackRecord, error) {
	if store.searchTracks == nil {
		return nil, errors.New("unexpected SearchTracks call")
	}
	return store.searchTracks(ctx, query)
}

func (store *storeStub) SearchArtists(ctx context.Context, query SearchQuery) ([]ArtistRecord, error) {
	if store.searchArtists == nil {
		return nil, errors.New("unexpected SearchArtists call")
	}
	return store.searchArtists(ctx, query)
}

func (store *storeStub) SearchAlbums(ctx context.Context, query SearchQuery) ([]AlbumRecord, error) {
	if store.searchAlbums == nil {
		return nil, errors.New("unexpected SearchAlbums call")
	}
	return store.searchAlbums(ctx, query)
}

func intPointer(value int) *int { return &value }
