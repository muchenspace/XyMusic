package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

const (
	defaultPageLimit     = 20
	defaultLyricPageSize = 20
	maximumPageLimit     = 100
	maximumRandom        = 50
	artworkCacheSize     = 1_000
)

type ArtworkURLSigner interface {
	PresignedGet(ctx context.Context, objectKey string, expires time.Duration) (string, error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type ServiceDependencies struct {
	Repository    Store
	Cursors       *pagination.CursorCodec
	ArtworkURLs   ArtworkURLSigner
	ArtworkURLTTL time.Duration
	Clock         Clock
	RandomFloat64 func() float64
}

type artworkCacheEntry struct {
	value         ArtworkDTO
	cacheKey      string
	reusableUntil time.Time
}

type Service struct {
	repository  Store
	cursors     *pagination.CursorCodec
	artworkURLs ArtworkURLSigner
	artworkTTL  time.Duration
	clock       Clock
	random      func() float64

	cacheMu      sync.Mutex
	artworkCache map[string]artworkCacheEntry
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil {
		return nil, errors.New("catalog repository is required")
	}
	if dependencies.Cursors == nil {
		return nil, errors.New("catalog cursor codec is required")
	}
	if dependencies.ArtworkURLs == nil {
		return nil, errors.New("catalog artwork URL signer is required")
	}
	if dependencies.ArtworkURLTTL <= 0 {
		return nil, errors.New("catalog artwork URL TTL must be positive")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	if dependencies.RandomFloat64 == nil {
		dependencies.RandomFloat64 = rand.Float64
	}
	return &Service{
		repository:   dependencies.Repository,
		cursors:      dependencies.Cursors,
		artworkURLs:  dependencies.ArtworkURLs,
		artworkTTL:   dependencies.ArtworkURLTTL,
		clock:        dependencies.Clock,
		random:       dependencies.RandomFloat64,
		artworkCache: make(map[string]artworkCacheEntry),
	}, nil
}

func (s *Service) ListTracks(ctx context.Context, userID string, input ListTracksInput) (TrackPageDTO, error) {
	limit, err := pageLimit(input.Limit)
	if err != nil {
		return TrackPageDTO{}, err
	}
	if !validTrackSort(input.Sort) {
		return TrackPageDTO{}, apperror.Validation("sort is invalid")
	}
	if input.Sort == TrackSortAlbumOrderAsc && input.AlbumID == "" {
		return TrackPageDTO{}, apperror.Validation("ALBUM_ORDER_ASC requires albumId")
	}
	scope := fmt.Sprintf("tracks:%s:%s:%s", input.Sort, input.ArtistID, input.AlbumID)
	cursor, err := s.decodeTrackCursor(scope, input.Sort, input.Cursor)
	if err != nil {
		return TrackPageDTO{}, err
	}
	rows, err := s.repository.ListTracks(ctx, ListTracksQuery{
		UserID:   userID,
		Sort:     input.Sort,
		ArtistID: input.ArtistID,
		AlbumID:  input.AlbumID,
		After:    cursor,
		Limit:    limit + 1,
	})
	if err != nil {
		return TrackPageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := s.presentTracks(ctx, pageRows)
	if err != nil {
		return TrackPageDTO{}, err
	}
	result := TrackPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		encoded, err := s.encodeTrackCursor(scope, input.Sort, pageRows[len(pageRows)-1])
		if err != nil {
			return TrackPageDTO{}, fmt.Errorf("encode track cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (s *Service) RandomTracks(ctx context.Context, userID string, requestedLimit int) (RandomTracksDTO, error) {
	limit, err := randomLimit(requestedLimit)
	if err != nil {
		return RandomTracksDTO{}, err
	}
	anchor := s.random()
	first, err := s.repository.RandomTracks(ctx, userID, anchor, true, limit)
	if err != nil {
		return RandomTracksDTO{}, err
	}
	rows := append(make([]TrackRecord, 0, limit), first...)
	if len(rows) < limit {
		wrapped, err := s.repository.RandomTracks(ctx, userID, anchor, false, limit-len(rows))
		if err != nil {
			return RandomTracksDTO{}, err
		}
		rows = append(rows, wrapped...)
	}
	items, err := s.presentTracks(ctx, rows)
	if err != nil {
		return RandomTracksDTO{}, err
	}
	return RandomTracksDTO{Items: items}, nil
}

func (s *Service) GetTrack(ctx context.Context, userID, trackID string, input GetTrackInput) (TrackDetailDTO, error) {
	page, err := pagination.ParseOffset(input.LyricPage, input.LyricPageSize, defaultLyricPageSize)
	if err != nil {
		return TrackDetailDTO{}, err
	}
	record, err := s.repository.FindTrack(ctx, userID, trackID)
	if errors.Is(err, ErrNotFound) {
		return TrackDetailDTO{}, apperror.NotFound("Track was not found")
	}
	if err != nil {
		return TrackDetailDTO{}, err
	}
	lyrics, lyricTotal, err := s.repository.ListLyrics(ctx, ListLyricsQuery{
		TrackID: trackID,
		Limit:   page.PageSize,
		Offset:  page.Offset,
	})
	if err != nil {
		return TrackDetailDTO{}, err
	}
	summaries, err := s.presentTracks(ctx, []TrackRecord{record})
	if err != nil {
		return TrackDetailDTO{}, err
	}
	lyricDTOs := make([]LyricDTO, 0, len(lyrics))
	for _, item := range lyrics {
		lyricDTOs = append(lyricDTOs, LyricDTO{
			ID:           item.ID,
			TrackID:      item.TrackID,
			Language:     item.Language,
			Format:       item.Format,
			Content:      item.Content,
			IsDefault:    item.IsDefault,
			TrackVersion: record.Version,
			UpdatedAt:    formatTimestamp(item.UpdatedAt),
		})
	}
	return TrackDetailDTO{
		TrackSummaryDTO: summaries[0],
		Lyrics:          lyricDTOs,
		LyricPage:       page.Page,
		LyricPageSize:   page.PageSize,
		LyricTotal:      lyricTotal,
		LyricTotalPages: totalPages(lyricTotal, page.PageSize),
	}, nil
}

func totalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

// TrackSummaries returns playable tracks in the caller's requested order.
// Library and playlist projections use this same catalog contract so artwork
// signing and favorite state remain consistent across endpoints.
func (s *Service) TrackSummaries(ctx context.Context, userID string, trackIDs []string) ([]TrackSummaryDTO, error) {
	if len(trackIDs) == 0 {
		return []TrackSummaryDTO{}, nil
	}
	unique := make([]string, 0, len(trackIDs))
	seen := make(map[string]struct{}, len(trackIDs))
	for _, trackID := range trackIDs {
		if _, exists := seen[trackID]; exists {
			continue
		}
		seen[trackID] = struct{}{}
		unique = append(unique, trackID)
	}
	records, err := s.repository.FindTracks(ctx, userID, unique)
	if err != nil {
		return nil, err
	}
	presented, err := s.presentTracks(ctx, records)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]TrackSummaryDTO, len(presented))
	for _, track := range presented {
		byID[track.ID] = track
	}
	result := make([]TrackSummaryDTO, 0, len(trackIDs))
	for _, trackID := range trackIDs {
		if track, exists := byID[trackID]; exists {
			result = append(result, track)
		}
	}
	return result, nil
}

func (s *Service) ListArtists(ctx context.Context, input ListArtistsInput) (ArtistPageDTO, error) {
	limit, err := pageLimit(input.Limit)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	if !validArtistSort(input.Sort) {
		return ArtistPageDTO{}, apperror.Validation("sort is invalid")
	}
	scope := fmt.Sprintf("artists:%s", input.Sort)
	cursor, err := decodeArtistCursor(s.cursors, scope, input.Cursor)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	rows, err := s.repository.ListArtists(ctx, ListArtistsQuery{Sort: input.Sort, After: cursor, Limit: limit + 1})
	if err != nil {
		return ArtistPageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := s.presentArtists(ctx, pageRows)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	result := ArtistPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		encoded, err := pagination.EncodeCursor(s.cursors, scope, artistCursorValue{Name: last.NormalizedName, ID: last.ID})
		if err != nil {
			return ArtistPageDTO{}, fmt.Errorf("encode artist cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (s *Service) GetArtist(ctx context.Context, artistID string) (ArtistDetailDTO, error) {
	record, err := s.repository.FindArtist(ctx, artistID)
	if errors.Is(err, ErrNotFound) {
		return ArtistDetailDTO{}, apperror.NotFound("Artist was not found")
	}
	if err != nil {
		return ArtistDetailDTO{}, err
	}
	artwork, err := s.presentArtwork(ctx, record.Artwork)
	if err != nil {
		return ArtistDetailDTO{}, err
	}
	return ArtistDetailDTO{
		ID:          record.ID,
		Name:        record.Name,
		Artwork:     artwork,
		Description: record.Description,
	}, nil
}

func (s *Service) ListAlbums(ctx context.Context, input ListAlbumsInput) (AlbumPageDTO, error) {
	limit, err := pageLimit(input.Limit)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	if !validAlbumSort(input.Sort) {
		return AlbumPageDTO{}, apperror.Validation("sort is invalid")
	}
	scope := fmt.Sprintf("albums:%s:%s", input.Sort, input.ArtistID)
	cursor, err := s.decodeAlbumCursor(scope, input.Sort, input.Cursor)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	rows, err := s.repository.ListAlbums(ctx, ListAlbumsQuery{
		Sort:     input.Sort,
		ArtistID: input.ArtistID,
		After:    cursor,
		Limit:    limit + 1,
	})
	if err != nil {
		return AlbumPageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := s.presentAlbums(ctx, pageRows)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	result := AlbumPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		encoded, err := s.encodeAlbumCursor(scope, input.Sort, pageRows[len(pageRows)-1])
		if err != nil {
			return AlbumPageDTO{}, fmt.Errorf("encode album cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (s *Service) RandomAlbums(ctx context.Context, requestedLimit int) (RandomAlbumsDTO, error) {
	limit, err := randomLimit(requestedLimit)
	if err != nil {
		return RandomAlbumsDTO{}, err
	}
	anchor := s.random()
	first, err := s.repository.RandomAlbums(ctx, anchor, true, limit)
	if err != nil {
		return RandomAlbumsDTO{}, err
	}
	rows := append(make([]AlbumRecord, 0, limit), first...)
	if len(rows) < limit {
		wrapped, err := s.repository.RandomAlbums(ctx, anchor, false, limit-len(rows))
		if err != nil {
			return RandomAlbumsDTO{}, err
		}
		rows = append(rows, wrapped...)
	}
	items, err := s.presentAlbums(ctx, rows)
	if err != nil {
		return RandomAlbumsDTO{}, err
	}
	return RandomAlbumsDTO{Items: items}, nil
}

func (s *Service) GetAlbum(ctx context.Context, albumID string) (AlbumDetailDTO, error) {
	record, err := s.repository.FindAlbum(ctx, albumID)
	if errors.Is(err, ErrNotFound) {
		return AlbumDetailDTO{}, apperror.NotFound("Album was not found")
	}
	if err != nil {
		return AlbumDetailDTO{}, err
	}
	summaries, err := s.presentAlbums(ctx, []AlbumRecord{record})
	if err != nil {
		return AlbumDetailDTO{}, err
	}
	return AlbumDetailDTO{AlbumSummaryDTO: summaries[0], Description: record.Description}, nil
}

func (s *Service) Search(ctx context.Context, userID string, input SearchInput) (SearchResultDTO, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" || javascriptStringLength(query) > 200 {
		return SearchResultDTO{}, apperror.Validation("q must contain 1 to 200 characters")
	}
	if !validSearchScope(input.Scope) {
		return SearchResultDTO{}, apperror.Validation("scope is invalid")
	}
	if input.Scope == SearchScopeAll && input.Cursor != "" {
		return SearchResultDTO{}, apperror.Validation("cursor is not valid for ALL search")
	}
	if input.Scope == SearchScopeAll {
		tracks, err := s.searchTracks(ctx, userID, query, "", 5)
		if err != nil {
			return SearchResultDTO{}, err
		}
		artists, err := s.searchArtists(ctx, query, "", 5)
		if err != nil {
			return SearchResultDTO{}, err
		}
		albums, err := s.searchAlbums(ctx, query, "", 5)
		if err != nil {
			return SearchResultDTO{}, err
		}
		return SearchResultDTO{
			Query:   query,
			Scope:   SearchScopeAll,
			Tracks:  &tracks,
			Artists: &artists,
			Albums:  &albums,
		}, nil
	}
	limit, err := pageLimit(input.Limit)
	if err != nil {
		return SearchResultDTO{}, err
	}
	result := SearchResultDTO{Query: query, Scope: input.Scope}
	switch input.Scope {
	case SearchScopeTracks:
		page, err := s.searchTracks(ctx, userID, query, input.Cursor, limit)
		if err != nil {
			return SearchResultDTO{}, err
		}
		result.Tracks = &page
	case SearchScopeArtists:
		page, err := s.searchArtists(ctx, query, input.Cursor, limit)
		if err != nil {
			return SearchResultDTO{}, err
		}
		result.Artists = &page
	case SearchScopeAlbums:
		page, err := s.searchAlbums(ctx, query, input.Cursor, limit)
		if err != nil {
			return SearchResultDTO{}, err
		}
		result.Albums = &page
	}
	return result, nil
}

func (s *Service) searchTracks(ctx context.Context, userID, query, encoded string, limit int) (TrackPageDTO, error) {
	normalized, pattern := normalizedSearch(query)
	scope := searchCursorScope(SearchScopeTracks, normalized)
	cursor, err := decodeSimpleCursor(s.cursors, scope, encoded)
	if err != nil {
		return TrackPageDTO{}, err
	}
	rows, err := s.repository.SearchTracks(ctx, SearchQuery{
		UserID:          userID,
		NormalizedQuery: normalized,
		Pattern:         pattern,
		UseTrigram:      javascriptStringLength(normalized) >= 3,
		After:           cursor,
		Limit:           limit + 1,
	})
	if err != nil {
		return TrackPageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := s.presentTracks(ctx, pageRows)
	if err != nil {
		return TrackPageDTO{}, err
	}
	result := TrackPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		value, err := pagination.EncodeCursor(s.cursors, scope, searchCursorValue{Value: last.NormalizedTitle, ID: last.ID})
		if err != nil {
			return TrackPageDTO{}, fmt.Errorf("encode track search cursor: %w", err)
		}
		result.NextCursor = &value
	}
	return result, nil
}

func (s *Service) searchArtists(ctx context.Context, query, encoded string, limit int) (ArtistPageDTO, error) {
	normalized, pattern := normalizedSearch(query)
	scope := searchCursorScope(SearchScopeArtists, normalized)
	cursor, err := decodeSimpleCursor(s.cursors, scope, encoded)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	rows, err := s.repository.SearchArtists(ctx, SearchQuery{
		NormalizedQuery: normalized,
		Pattern:         pattern,
		UseTrigram:      javascriptStringLength(normalized) >= 3,
		After:           cursor,
		Limit:           limit + 1,
	})
	if err != nil {
		return ArtistPageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := s.presentArtists(ctx, pageRows)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	result := ArtistPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		value, err := pagination.EncodeCursor(s.cursors, scope, searchCursorValue{Value: last.NormalizedName, ID: last.ID})
		if err != nil {
			return ArtistPageDTO{}, fmt.Errorf("encode artist search cursor: %w", err)
		}
		result.NextCursor = &value
	}
	return result, nil
}

func (s *Service) searchAlbums(ctx context.Context, query, encoded string, limit int) (AlbumPageDTO, error) {
	normalized, pattern := normalizedSearch(query)
	scope := searchCursorScope(SearchScopeAlbums, normalized)
	cursor, err := decodeSimpleCursor(s.cursors, scope, encoded)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	rows, err := s.repository.SearchAlbums(ctx, SearchQuery{
		NormalizedQuery: normalized,
		Pattern:         pattern,
		UseTrigram:      javascriptStringLength(normalized) >= 3,
		After:           cursor,
		Limit:           limit + 1,
	})
	if err != nil {
		return AlbumPageDTO{}, err
	}
	pageRows, hasNext := firstPage(rows, limit)
	items, err := s.presentAlbums(ctx, pageRows)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	result := AlbumPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		value, err := pagination.EncodeCursor(s.cursors, scope, searchCursorValue{Value: last.NormalizedTitle, ID: last.ID})
		if err != nil {
			return AlbumPageDTO{}, fmt.Errorf("encode album search cursor: %w", err)
		}
		result.NextCursor = &value
	}
	return result, nil
}

func (s *Service) presentTracks(ctx context.Context, records []TrackRecord) ([]TrackSummaryDTO, error) {
	result := make([]TrackSummaryDTO, 0, len(records))
	for _, record := range records {
		artwork, err := s.presentArtwork(ctx, record.Artwork)
		if err != nil {
			return nil, fmt.Errorf("present track artwork: %w", err)
		}
		artists := presentCredits(record.Artists)
		var album *AlbumReferenceDTO
		if record.Album != nil {
			album = &AlbumReferenceDTO{ID: record.Album.ID, Title: record.Album.Title}
		}
		discNumber := 1
		if record.DiscNumber != nil {
			discNumber = *record.DiscNumber
		}
		result = append(result, TrackSummaryDTO{
			ID:          record.ID,
			Title:       record.Title,
			Artists:     artists,
			Album:       album,
			Artwork:     artwork,
			DurationMS:  record.DurationMS,
			TrackNumber: record.TrackNumber,
			DiscNumber:  discNumber,
			IsFavorite:  record.Favorite,
			PublishedAt: formatTimestamp(record.PublishedAt),
		})
	}
	return result, nil
}

func (s *Service) presentArtists(ctx context.Context, records []ArtistRecord) ([]ArtistSummaryDTO, error) {
	result := make([]ArtistSummaryDTO, 0, len(records))
	for _, record := range records {
		artwork, err := s.presentArtwork(ctx, record.Artwork)
		if err != nil {
			return nil, fmt.Errorf("present artist artwork: %w", err)
		}
		result = append(result, ArtistSummaryDTO{ID: record.ID, Name: record.Name, Artwork: artwork})
	}
	return result, nil
}

func (s *Service) presentAlbums(ctx context.Context, records []AlbumRecord) ([]AlbumSummaryDTO, error) {
	result := make([]AlbumSummaryDTO, 0, len(records))
	for _, record := range records {
		cover, err := s.presentArtwork(ctx, record.Cover)
		if err != nil {
			return nil, fmt.Errorf("present album artwork: %w", err)
		}
		result = append(result, AlbumSummaryDTO{
			ID:          record.ID,
			Title:       record.Title,
			Artists:     presentCredits(record.Artists),
			Cover:       cover,
			ReleaseDate: record.ReleaseDate,
			TrackCount:  record.TrackCount,
		})
	}
	return result, nil
}

func (s *Service) presentArtwork(ctx context.Context, asset *ArtworkAsset) (*ArtworkDTO, error) {
	if asset == nil {
		return nil, nil
	}
	now := s.clock.Now()
	cacheValue := strconv.FormatInt(asset.UpdatedAt.UnixMilli(), 10)
	if asset.ChecksumSHA256 != nil {
		cacheValue = *asset.ChecksumSHA256
	}
	cacheKey := asset.ID + ":" + cacheValue
	s.cacheMu.Lock()
	cached, found := s.artworkCache[asset.ID]
	s.cacheMu.Unlock()
	if found && cached.cacheKey == cacheKey && cached.reusableUntil.After(now) {
		value := cached.value
		return &value, nil
	}
	url, err := s.artworkURLs.PresignedGet(ctx, asset.ObjectKey, s.artworkTTL)
	if err != nil {
		return nil, err
	}
	expiresAtTime := now.Add(s.artworkTTL)
	expiresAt := formatTimestamp(expiresAtTime)
	value := ArtworkDTO{
		AssetID:   asset.ID,
		URL:       url,
		CacheKey:  cacheKey,
		MimeType:  asset.MimeType,
		ExpiresAt: &expiresAt,
		Width:     asset.Width,
		Height:    asset.Height,
	}
	reuseMargin := s.artworkTTL / 4
	if reuseMargin > 30*time.Second {
		reuseMargin = 30 * time.Second
	}
	s.cacheMu.Lock()
	if len(s.artworkCache) >= artworkCacheSize {
		for key := range s.artworkCache {
			delete(s.artworkCache, key)
			break
		}
	}
	s.artworkCache[asset.ID] = artworkCacheEntry{
		value:         value,
		cacheKey:      cacheKey,
		reusableUntil: expiresAtTime.Add(-reuseMargin),
	}
	s.cacheMu.Unlock()
	return &value, nil
}

type trackCursorValue struct {
	PublishedAt *string `json:"publishedAt,omitempty"`
	Title       *string `json:"title,omitempty"`
	DiscNumber  *int    `json:"discNumber,omitempty"`
	TrackNumber *int    `json:"trackNumber,omitempty"`
	ID          string  `json:"id"`
}

type albumCursorValue struct {
	Title       *string         `json:"title,omitempty"`
	ReleaseDate json.RawMessage `json:"releaseDate"`
	ID          string          `json:"id"`
}

type albumTitleCursorValue struct {
	Title string `json:"title"`
	ID    string `json:"id"`
}

type albumReleaseCursorValue struct {
	ReleaseDate *string `json:"releaseDate"`
	ID          string  `json:"id"`
}

type searchCursorValue struct {
	Value string `json:"value"`
	ID    string `json:"id"`
}

type artistCursorValue struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

func (s *Service) decodeTrackCursor(scope string, sort TrackSort, encoded string) (*TrackCursor, error) {
	value, err := pagination.DecodeCursor[trackCursorValue](s.cursors, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.ID == "" {
		return nil, invalidCursor()
	}
	result := &TrackCursor{ID: value.ID, Title: value.Title, DiscNumber: value.DiscNumber, TrackNumber: value.TrackNumber}
	switch sort {
	case TrackSortPublishedDesc:
		if value.PublishedAt == nil {
			return nil, invalidCursor()
		}
		parsed, err := time.Parse(time.RFC3339Nano, *value.PublishedAt)
		if err != nil {
			return nil, invalidCursor()
		}
		result.PublishedAt = &parsed
	case TrackSortTitleAsc, TrackSortTitleDesc:
		if value.Title == nil {
			return nil, invalidCursor()
		}
	case TrackSortAlbumOrderAsc:
		if value.DiscNumber == nil || value.TrackNumber == nil {
			return nil, invalidCursor()
		}
	default:
		return nil, invalidCursor()
	}
	return result, nil
}

func (s *Service) encodeTrackCursor(scope string, sort TrackSort, record TrackRecord) (string, error) {
	value := trackCursorValue{ID: record.ID}
	switch sort {
	case TrackSortPublishedDesc:
		publishedAt := formatTimestamp(record.PublishedAt)
		value.PublishedAt = &publishedAt
	case TrackSortTitleAsc, TrackSortTitleDesc:
		title := record.NormalizedTitle
		value.Title = &title
	case TrackSortAlbumOrderAsc:
		discNumber := 1
		if record.DiscNumber != nil {
			discNumber = *record.DiscNumber
		}
		trackNumber := 0
		if record.TrackNumber != nil {
			trackNumber = *record.TrackNumber
		}
		value.DiscNumber = &discNumber
		value.TrackNumber = &trackNumber
	default:
		return "", invalidCursor()
	}
	return pagination.EncodeCursor(s.cursors, scope, value)
}

func (s *Service) decodeAlbumCursor(scope string, sort AlbumSort, encoded string) (*AlbumCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	if sort == AlbumSortTitleAsc || sort == AlbumSortTitleDesc {
		value, err := pagination.DecodeCursor[albumTitleCursorValue](s.cursors, scope, encoded)
		if err != nil {
			return nil, err
		}
		if value == nil || value.ID == "" {
			return nil, invalidCursor()
		}
		return &AlbumCursor{Title: &value.Title, ID: value.ID}, nil
	}
	value, err := pagination.DecodeCursor[albumCursorValue](s.cursors, scope, encoded)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ID == "" || len(value.ReleaseDate) == 0 {
		return nil, invalidCursor()
	}
	result := &AlbumCursor{ID: value.ID}
	if string(value.ReleaseDate) == "null" {
		result.NullRelease = true
		return result, nil
	}
	var releaseDate string
	if err := json.Unmarshal(value.ReleaseDate, &releaseDate); err != nil || !validDate(releaseDate) {
		return nil, invalidCursor()
	}
	result.ReleaseDate = &releaseDate
	return result, nil
}

func (s *Service) encodeAlbumCursor(scope string, sort AlbumSort, record AlbumRecord) (string, error) {
	if sort == AlbumSortReleaseDateDesc {
		return pagination.EncodeCursor(s.cursors, scope, albumReleaseCursorValue{ReleaseDate: record.ReleaseDate, ID: record.ID})
	}
	return pagination.EncodeCursor(s.cursors, scope, albumTitleCursorValue{Title: record.NormalizedTitle, ID: record.ID})
}

func decodeSimpleCursor(codec *pagination.CursorCodec, scope, encoded string) (*SearchCursor, error) {
	value, err := pagination.DecodeCursor[searchCursorValue](codec, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.ID == "" {
		return nil, invalidCursor()
	}
	return &SearchCursor{Value: value.Value, ID: value.ID}, nil
}

func decodeArtistCursor(codec *pagination.CursorCodec, scope, encoded string) (*SearchCursor, error) {
	value, err := pagination.DecodeCursor[artistCursorValue](codec, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.ID == "" {
		return nil, invalidCursor()
	}
	return &SearchCursor{Value: value.Name, ID: value.ID}, nil
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

func randomLimit(value int) (int, error) {
	if value < 1 || value > maximumRandom {
		return 0, apperror.Validation("limit must be from 1 to 50")
	}
	return value, nil
}

func validTrackSort(value TrackSort) bool {
	switch value {
	case TrackSortPublishedDesc, TrackSortTitleAsc, TrackSortTitleDesc, TrackSortAlbumOrderAsc:
		return true
	default:
		return false
	}
}

func validArtistSort(value ArtistSort) bool {
	return value == ArtistSortNameAsc || value == ArtistSortNameDesc
}

func validAlbumSort(value AlbumSort) bool {
	switch value {
	case AlbumSortReleaseDateDesc, AlbumSortTitleAsc, AlbumSortTitleDesc:
		return true
	default:
		return false
	}
}

func validSearchScope(value SearchScope) bool {
	switch value {
	case SearchScopeAll, SearchScopeTracks, SearchScopeArtists, SearchScopeAlbums:
		return true
	default:
		return false
	}
}

func presentCredits(records []ArtistReferenceRecord) []ArtistReferenceDTO {
	result := make([]ArtistReferenceDTO, 0, len(records))
	for _, record := range records {
		result = append(result, ArtistReferenceDTO(record))
	}
	return result
}

func firstPage[T any](records []T, limit int) ([]T, bool) {
	if len(records) <= limit {
		return records, false
	}
	return records[:limit], true
}

func normalizedSearch(value string) (string, string) {
	normalized := strings.ToLower(norm.NFKC.String(value))
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return normalized, "%" + replacer.Replace(normalized) + "%"
}

func javascriptStringLength(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

func searchCursorScope(scope SearchScope, normalizedQuery string) string {
	digest := sha256.Sum256([]byte(normalizedQuery))
	return fmt.Sprintf("search:%s:%s", scope, hex.EncodeToString(digest[:]))
}

func invalidCursor() error {
	return apperror.InvalidCursor("Cursor is invalid")
}

func validDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
