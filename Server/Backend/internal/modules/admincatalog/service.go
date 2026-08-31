package admincatalog

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/audiostatus"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
	"xymusic/server/internal/shared/pagination"
	"xymusic/server/internal/shared/tagwriteback"
)

const defaultCatalogPageSize = 100

type Service struct {
	store    Store
	artworks ArtworkPresenter
	cursors  *pagination.CursorCodec
}

func NewService(store Store, artworks ArtworkPresenter) (*Service, error) {
	return NewServiceWithOptions(store, artworks, nil)
}

// NewServiceWithOptions enables signed keyset cursors for the high-volume
// admin catalog endpoints. NewService remains available for small/local
// callers and keeps the legacy offset behavior when no codec is supplied.
func NewServiceWithOptions(
	store Store,
	artworks ArtworkPresenter,
	cursors *pagination.CursorCodec,
) (*Service, error) {
	if store == nil {
		return nil, errors.New("admin catalog store is required")
	}
	if artworks == nil {
		return nil, errors.New("admin catalog artwork presenter is required")
	}
	return &Service{store: store, artworks: artworks, cursors: cursors}, nil
}

func (service *Service) ListArtists(ctx context.Context, input ListInput) (ArtistPageDTO, error) {
	if input.Sort == "" {
		input.Sort = "name"
	}
	if input.Order == "" {
		input.Order = SortAscending
	}
	input.Search = strings.TrimSpace(input.Search)
	if !oneOf(input.Sort, "name", "createdAt", "updatedAt") || !validOrder(input.Order) {
		return ArtistPageDTO{}, apperror.Validation("Artist query is invalid")
	}
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, defaultCatalogPageSize)
		if err != nil {
			return ArtistPageDTO{}, err
		}
		if service.cursors == nil {
			return ArtistPageDTO{}, errors.New("admin catalog cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return ArtistPageDTO{}, apperror.Validation("cursor is required for deep catalog pages")
		}
		scope := artistCursorScope(input)
		after, err := decodeCatalogCursor(service.cursors, scope, input.Cursor, input.Sort, false)
		if err != nil {
			return ArtistPageDTO{}, err
		}
		records, total, err := service.store.ListArtists(ctx, ArtistQuery{
			Search: input.Search, Sort: input.Sort, Order: input.Order,
			Limit: pagination.CursorLimit(page.Page, page.PageSize), After: after, CursorMode: true, HasNextProbe: true, TotalHint: listCursorTotalHint(after),
		})
		if err != nil {
			return ArtistPageDTO{}, err
		}
		hasNext := len(records) > page.PageSize
		if len(records) > page.PageSize {
			records = records[:page.PageSize]
		}
		items, err := service.presentArtists(ctx, records)
		if err != nil {
			return ArtistPageDTO{}, err
		}
		result := ArtistPageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total, TotalPages: exactTotalPages(total, page.PageSize)}
		if hasNext && len(records) > 0 {
			encoded, err := encodeArtistCursor(service.cursors, scope, input.Sort, records[len(records)-1], total)
			if err != nil {
				return ArtistPageDTO{}, err
			}
			result.NextCursor = &encoded
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, defaultCatalogPageSize)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	records, total, err := service.store.ListArtists(ctx, ArtistQuery{
		Search: input.Search, Sort: input.Sort, Order: input.Order,
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return ArtistPageDTO{}, err
	}
	items, err := service.presentArtists(ctx, records)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	return ArtistPageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total, TotalPages: pagination.BoundedTotalPages(total, page.PageSize)}, nil
}

func (service *Service) Artist(ctx context.Context, id string) (ArtistDTO, error) {
	record, err := service.store.FindArtist(ctx, id)
	if err != nil {
		return ArtistDTO{}, err
	}
	items, err := service.presentArtists(ctx, []ArtistRecord{record})
	if err != nil {
		return ArtistDTO{}, err
	}
	return items[0], nil
}

func (service *Service) ListAlbums(ctx context.Context, input ListInput) (AlbumPageDTO, error) {
	if input.Sort == "" {
		input.Sort = "updatedAt"
	}
	if input.Order == "" {
		input.Order = SortDescending
	}
	input.Search = strings.TrimSpace(input.Search)
	if !oneOf(input.Sort, "title", "createdAt", "updatedAt", "releaseDate") || !validOrder(input.Order) {
		return AlbumPageDTO{}, apperror.Validation("Album query is invalid")
	}
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, defaultCatalogPageSize)
		if err != nil {
			return AlbumPageDTO{}, err
		}
		if service.cursors == nil {
			return AlbumPageDTO{}, errors.New("admin catalog cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return AlbumPageDTO{}, apperror.Validation("cursor is required for deep catalog pages")
		}
		scope := albumCursorScope(input)
		after, err := decodeCatalogCursor(service.cursors, scope, input.Cursor, input.Sort, input.Sort == "releaseDate")
		if err != nil {
			return AlbumPageDTO{}, err
		}
		records, total, err := service.store.ListAlbums(ctx, AlbumQuery{
			Search: input.Search, Sort: input.Sort, Order: input.Order,
			Limit: pagination.CursorLimit(page.Page, page.PageSize), After: after, CursorMode: true, HasNextProbe: true, TotalHint: listCursorTotalHint(after),
		})
		if err != nil {
			return AlbumPageDTO{}, err
		}
		hasNext := len(records) > page.PageSize
		if len(records) > page.PageSize {
			records = records[:page.PageSize]
		}
		items, err := service.presentAlbums(ctx, records)
		if err != nil {
			return AlbumPageDTO{}, err
		}
		result := AlbumPageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total, TotalPages: exactTotalPages(total, page.PageSize)}
		if hasNext && len(records) > 0 {
			encoded, err := encodeAlbumCursor(service.cursors, scope, input.Sort, records[len(records)-1], total)
			if err != nil {
				return AlbumPageDTO{}, err
			}
			result.NextCursor = &encoded
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, defaultCatalogPageSize)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	records, total, err := service.store.ListAlbums(ctx, AlbumQuery{
		Search: input.Search, Sort: input.Sort, Order: input.Order,
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return AlbumPageDTO{}, err
	}
	items, err := service.presentAlbums(ctx, records)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	return AlbumPageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total, TotalPages: pagination.BoundedTotalPages(total, page.PageSize)}, nil
}

func (service *Service) DuplicateAlbums(ctx context.Context, input DuplicateAlbumInput) (DuplicateAlbumsDTO, error) {
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, defaultCatalogPageSize)
		if err != nil {
			return DuplicateAlbumsDTO{}, err
		}
		albumPage, err := pagination.ParseCursor(input.AlbumPage, input.AlbumPageSize, defaultCatalogPageSize)
		if err != nil {
			return DuplicateAlbumsDTO{}, err
		}
		if service.cursors == nil {
			return DuplicateAlbumsDTO{}, errors.New("admin catalog cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return DuplicateAlbumsDTO{}, apperror.Validation("cursor is required for deep duplicate pages")
		}
		if input.AlbumID != "" && albumPage.Page > 1 && input.AlbumCursor == "" {
			return DuplicateAlbumsDTO{}, apperror.Validation("album cursor is required for deep duplicate members")
		}
		if input.AlbumCursor != "" && input.AlbumID == "" {
			return DuplicateAlbumsDTO{}, invalidCursor()
		}
		groupScope := duplicateGroupCursorScope(input)
		groupAfter, err := decodeDuplicateGroupCursor(service.cursors, groupScope, input.Cursor)
		if err != nil {
			return DuplicateAlbumsDTO{}, err
		}
		albumScope := duplicateAlbumCursorScope(input)
		albumAfter, err := decodeDuplicateAlbumCursor(service.cursors, albumScope, input.AlbumCursor)
		if err != nil {
			return DuplicateAlbumsDTO{}, err
		}
		stored, err := service.store.FindDuplicateAlbums(ctx, DuplicateAlbumQuery{
			AlbumID: input.AlbumID, Limit: pagination.CursorLimit(page.Page, page.PageSize), After: groupAfter, CursorMode: true,
			TotalHint:  duplicateGroupCursorTotalHint(groupAfter),
			AlbumLimit: pagination.CursorLimit(albumPage.Page, albumPage.PageSize), AlbumAfter: albumAfter, AlbumCursorMode: true,
		})
		if err != nil {
			return DuplicateAlbumsDTO{}, err
		}
		groupHasNext := len(stored.Groups) > page.PageSize
		if len(stored.Groups) > page.PageSize {
			stored.Groups = stored.Groups[:page.PageSize]
		}
		albumNext := make(map[string]*string, len(stored.Groups))
		for index := range stored.Groups {
			group := &stored.Groups[index]
			if len(group.Albums) > albumPage.PageSize {
				group.Albums = group.Albums[:albumPage.PageSize]
				if input.AlbumID != "" {
					next, err := encodeDuplicateAlbumCursor(service.cursors, albumScope, group.Albums[albumPage.PageSize-1], group.AlbumTotal)
					if err != nil {
						return DuplicateAlbumsDTO{}, err
					}
					albumNext[group.Key] = &next
				}
			}
		}
		records := make([]AlbumRecord, 0)
		for _, group := range stored.Groups {
			records = append(records, group.Albums...)
		}
		items, err := service.presentAlbums(ctx, records)
		if err != nil {
			return DuplicateAlbumsDTO{}, err
		}
		itemsByID := make(map[string]AlbumDTO, len(items))
		for _, item := range items {
			itemsByID[item.ID] = item
		}
		result := DuplicateAlbumsDTO{
			Groups:     make([]DuplicateAlbumGroupDTO, 0, len(stored.Groups)),
			GroupCount: stored.GroupCount, DuplicateAlbumCount: stored.DuplicateAlbumCount,
			Page: page.Page, PageSize: page.PageSize, Total: stored.Total,
			TotalPages: pagination.BoundedTotalPages(stored.Total, page.PageSize),
		}
		for _, group := range stored.Groups {
			albums := make([]AlbumDTO, 0, len(group.Albums))
			for _, record := range group.Albums {
				if item, exists := itemsByID[record.ID]; exists {
					albums = append(albums, item)
				}
			}
			sort.SliceStable(albums, func(left, right int) bool {
				if albums[left].TrackCount != albums[right].TrackCount {
					return albums[left].TrackCount > albums[right].TrackCount
				}
				return albums[left].CreatedAt < albums[right].CreatedAt
			})
			primaryArtists := make([]catalog.ArtistReferenceDTO, 0)
			seenArtists := make(map[string]struct{})
			for _, album := range albums {
				for _, credit := range album.ArtistCredits {
					if credit.Role != "PRIMARY" {
						continue
					}
					if _, exists := seenArtists[credit.Artist.ID]; exists {
						continue
					}
					seenArtists[credit.Artist.ID] = struct{}{}
					primaryArtists = append(primaryArtists, credit.Artist)
				}
			}
			title := group.Title
			if len(albums) > 0 {
				title = albums[0].Title
			}
			resultGroup := DuplicateAlbumGroupDTO{
				Key: group.Key, Title: title, PrimaryArtists: primaryArtists, Albums: albums,
				AlbumPage: albumPage.Page, AlbumPageSize: albumPage.PageSize,
				AlbumTotal: group.AlbumTotal, AlbumTotalPages: pagination.BoundedTotalPages(group.AlbumTotal, albumPage.PageSize),
			}
			if next := albumNext[group.Key]; next != nil {
				resultGroup.AlbumNextCursor = next
			}
			result.Groups = append(result.Groups, resultGroup)
		}
		if groupHasNext && len(stored.Groups) > 0 {
			next, err := encodeDuplicateGroupCursor(service.cursors, groupScope, stored.Groups[len(stored.Groups)-1], stored.Total)
			if err != nil {
				return DuplicateAlbumsDTO{}, err
			}
			result.NextCursor = &next
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, defaultCatalogPageSize)
	if err != nil {
		return DuplicateAlbumsDTO{}, err
	}
	albumPage, err := pagination.ParseOffset(input.AlbumPage, input.AlbumPageSize, defaultCatalogPageSize)
	if err != nil {
		return DuplicateAlbumsDTO{}, err
	}
	stored, err := service.store.FindDuplicateAlbums(ctx, DuplicateAlbumQuery{
		AlbumID: input.AlbumID, Limit: page.PageSize, Offset: page.Offset,
		AlbumLimit: albumPage.PageSize, AlbumOffset: albumPage.Offset,
	})
	if err != nil {
		return DuplicateAlbumsDTO{}, err
	}
	if len(stored.Groups) == 0 {
		return DuplicateAlbumsDTO{
			Groups: []DuplicateAlbumGroupDTO{}, GroupCount: stored.GroupCount,
			DuplicateAlbumCount: stored.DuplicateAlbumCount, Page: page.Page,
			PageSize: page.PageSize, Total: stored.Total,
			TotalPages: pagination.BoundedTotalPages(stored.Total, page.PageSize),
		}, nil
	}
	records := make([]AlbumRecord, 0)
	for _, group := range stored.Groups {
		records = append(records, group.Albums...)
	}
	items, err := service.presentAlbums(ctx, records)
	if err != nil {
		return DuplicateAlbumsDTO{}, err
	}
	itemsByID := make(map[string]AlbumDTO, len(items))
	for _, item := range items {
		itemsByID[item.ID] = item
	}
	result := DuplicateAlbumsDTO{
		Groups:     make([]DuplicateAlbumGroupDTO, 0, len(stored.Groups)),
		GroupCount: stored.GroupCount, DuplicateAlbumCount: stored.DuplicateAlbumCount,
		Page: page.Page, PageSize: page.PageSize, Total: stored.Total,
		TotalPages: pagination.BoundedTotalPages(stored.Total, page.PageSize),
	}
	for _, group := range stored.Groups {
		albums := make([]AlbumDTO, 0, len(group.Albums))
		for _, record := range group.Albums {
			if item, exists := itemsByID[record.ID]; exists {
				albums = append(albums, item)
			}
		}
		sort.SliceStable(albums, func(left, right int) bool {
			if albums[left].TrackCount != albums[right].TrackCount {
				return albums[left].TrackCount > albums[right].TrackCount
			}
			return albums[left].CreatedAt < albums[right].CreatedAt
		})
		primaryArtists := make([]catalog.ArtistReferenceDTO, 0)
		seenArtists := make(map[string]struct{})
		for _, album := range albums {
			for _, credit := range album.ArtistCredits {
				if credit.Role != "PRIMARY" {
					continue
				}
				if _, exists := seenArtists[credit.Artist.ID]; exists {
					continue
				}
				seenArtists[credit.Artist.ID] = struct{}{}
				primaryArtists = append(primaryArtists, credit.Artist)
			}
		}
		title := group.Title
		if len(albums) > 0 {
			title = albums[0].Title
		}
		result.Groups = append(result.Groups, DuplicateAlbumGroupDTO{
			Key: group.Key, Title: title, PrimaryArtists: primaryArtists, Albums: albums,
			AlbumPage: albumPage.Page, AlbumPageSize: albumPage.PageSize, AlbumTotal: group.AlbumTotal,
			AlbumTotalPages: pagination.BoundedTotalPages(group.AlbumTotal, albumPage.PageSize),
		})
	}
	return result, nil
}

func (service *Service) Album(ctx context.Context, id string, input PageInput) (AlbumDetailDTO, error) {
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, defaultCatalogPageSize)
		if err != nil {
			return AlbumDetailDTO{}, err
		}
		if service.cursors == nil {
			return AlbumDetailDTO{}, errors.New("admin catalog cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return AlbumDetailDTO{}, apperror.Validation("cursor is required for deep album-track pages")
		}
		cursorStore, ok := service.store.(CursorStore)
		if !ok {
			return AlbumDetailDTO{}, errors.New("admin catalog cursor store is required")
		}
		scope := albumTrackCursorScope(id)
		after, err := decodeAlbumTrackCursor(service.cursors, scope, input.Cursor)
		if err != nil {
			return AlbumDetailDTO{}, err
		}
		album, tracks, total, err := cursorStore.FindAlbumCursor(ctx, id, page.PageSize+1, after, albumTrackCursorTotalHint(after))
		if err != nil {
			return AlbumDetailDTO{}, err
		}
		hasNext := len(tracks) > page.PageSize
		if len(tracks) > page.PageSize {
			tracks = tracks[:page.PageSize]
		}
		albums, err := service.presentAlbums(ctx, []AlbumRecord{album})
		if err != nil {
			return AlbumDetailDTO{}, err
		}
		trackItems, err := service.presentTracks(ctx, tracks)
		if err != nil {
			return AlbumDetailDTO{}, err
		}
		result := AlbumDetailDTO{
			AlbumDTO: albums[0], Tracks: trackItems, TrackPage: page.Page, TrackPageSize: page.PageSize,
			TrackTotal: total, TrackTotalPages: pagination.BoundedTotalPages(total, page.PageSize),
		}
		if hasNext && len(tracks) > 0 {
			next, err := encodeAlbumTrackCursor(service.cursors, scope, tracks[len(tracks)-1], total)
			if err != nil {
				return AlbumDetailDTO{}, err
			}
			result.NextCursor = &next
		}
		return result, nil
	}
	page, err := pagination.ParseOffset(input.Page, input.PageSize, defaultCatalogPageSize)
	if err != nil {
		return AlbumDetailDTO{}, err
	}
	album, tracks, total, err := service.store.FindAlbum(ctx, id, page.PageSize, page.Offset)
	if err != nil {
		return AlbumDetailDTO{}, err
	}
	albums, err := service.presentAlbums(ctx, []AlbumRecord{album})
	if err != nil {
		return AlbumDetailDTO{}, err
	}
	trackItems, err := service.presentTracks(ctx, tracks)
	if err != nil {
		return AlbumDetailDTO{}, err
	}
	return AlbumDetailDTO{
		AlbumDTO: albums[0], Tracks: trackItems, TrackPage: page.Page, TrackPageSize: page.PageSize,
		TrackTotal: total, TrackTotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func (service *Service) ListTracks(ctx context.Context, input TrackListInput) (TrackPageDTO, error) {
	if input.Sort == "" {
		input.Sort = "updatedAt"
	}
	if input.Order == "" {
		input.Order = SortDescending
	}
	input.Search = strings.TrimSpace(input.Search)
	if !oneOf(input.Sort, "title", "createdAt", "updatedAt", "status") || !validOrder(input.Order) ||
		!validAudioStatusFilter(input.Status) || !validMetadataStatusFilter(input.MetadataStatus) {
		return TrackPageDTO{}, apperror.Validation("Track query is invalid")
	}

	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, defaultCatalogPageSize)
		if err != nil {
			return TrackPageDTO{}, err
		}
		if service.cursors == nil {
			return TrackPageDTO{}, errors.New("admin catalog cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return TrackPageDTO{}, apperror.Validation("cursor is required for deep catalog pages")
		}
		scope := trackCursorScope(input)
		after, err := decodeTrackCursor(service.cursors, scope, input.Cursor, input.Sort)
		if err != nil {
			return TrackPageDTO{}, err
		}
		records, total, err := service.store.ListTracks(ctx, TrackQuery{
			Search: input.Search, Sort: input.Sort, Order: input.Order,
			Status: input.Status, MetadataStatus: input.MetadataStatus, SourceID: input.SourceID,
			Limit: pagination.CursorLimit(page.Page, page.PageSize), After: after, CursorMode: true, HasNextProbe: true, TotalHint: listCursorTotalHint(after),
		})
		if err != nil {
			return TrackPageDTO{}, err
		}
		hasNext := len(records) > page.PageSize
		if len(records) > page.PageSize {
			records = records[:page.PageSize]
		}
		items, err := service.presentTracks(ctx, records)
		if err != nil {
			return TrackPageDTO{}, err
		}
		result := TrackPageDTO{
			Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
			TotalPages: exactTotalPages(total, page.PageSize),
		}
		if hasNext && len(records) > 0 {
			encoded, err := encodeTrackCursor(service.cursors, scope, input.Sort, records[len(records)-1], total)
			if err != nil {
				return TrackPageDTO{}, err
			}
			result.NextCursor = &encoded
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, defaultCatalogPageSize)
	if err != nil {
		return TrackPageDTO{}, err
	}
	records, total, err := service.store.ListTracks(ctx, TrackQuery{
		Search: input.Search, Sort: input.Sort, Order: input.Order,
		Status: input.Status, MetadataStatus: input.MetadataStatus, SourceID: input.SourceID,
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return TrackPageDTO{}, err
	}
	items, err := service.presentTracks(ctx, records)
	if err != nil {
		return TrackPageDTO{}, err
	}
	return TrackPageDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func (service *Service) Track(ctx context.Context, id string, input PageInput) (TrackDetailDTO, error) {
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, 20)
		if err != nil {
			return TrackDetailDTO{}, err
		}
		if service.cursors == nil {
			return TrackDetailDTO{}, errors.New("admin catalog cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return TrackDetailDTO{}, apperror.Validation("cursor is required for deep lyric pages")
		}
		cursorStore, ok := service.store.(CursorStore)
		if !ok {
			return TrackDetailDTO{}, errors.New("admin catalog cursor store is required")
		}
		scope := trackLyricCursorScope(id)
		after, err := decodeTrackLyricCursor(service.cursors, scope, input.Cursor)
		if err != nil {
			return TrackDetailDTO{}, err
		}
		record, lyricTotal, err := cursorStore.FindTrackCursor(ctx, id, page.PageSize+1, after, trackLyricCursorTotalHint(after))
		if err != nil {
			return TrackDetailDTO{}, err
		}
		hasNext := len(record.Lyrics) > page.PageSize
		if len(record.Lyrics) > page.PageSize {
			record.Lyrics = record.Lyrics[:page.PageSize]
		}
		items, err := service.presentTracks(ctx, []TrackRecord{record})
		if err != nil {
			return TrackDetailDTO{}, err
		}
		lyrics := make([]LyricDTO, 0, len(record.Lyrics))
		for _, lyric := range record.Lyrics {
			content := ""
			if lyric.Content != nil {
				content = *lyric.Content
			}
			if !sharedlyrics.ValidTiming(lyric.Timing) {
				return TrackDetailDTO{}, apperror.Internal("Stored lyrics timing is invalid", nil)
			}
			if err := sharedlyrics.ValidateDocument(lyric.Format, sharedlyrics.Timing(lyric.Timing), content); err != nil {
				return TrackDetailDTO{}, apperror.Internal("Stored lyrics violate the timing contract", err)
			}
			lyrics = append(lyrics, LyricDTO{
				ID: lyric.ID, Language: lyric.Language, Format: lyric.Format, Timing: lyric.Timing, Content: lyric.Content,
				IsDefault: lyric.IsDefault, Version: lyric.Version, UpdatedAt: formatTimestamp(lyric.UpdatedAt),
			})
		}
		result := TrackDetailDTO{
			TrackDTO: items[0], Lyrics: lyrics, LyricPage: page.Page, LyricPageSize: page.PageSize,
			LyricTotal: lyricTotal, LyricTotalPages: pagination.BoundedTotalPages(lyricTotal, page.PageSize),
		}
		if hasNext && len(record.Lyrics) > 0 {
			next, err := encodeTrackLyricCursor(service.cursors, scope, record.Lyrics[len(record.Lyrics)-1], lyricTotal)
			if err != nil {
				return TrackDetailDTO{}, err
			}
			result.NextCursor = &next
		}
		return result, nil
	}
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 20)
	if err != nil {
		return TrackDetailDTO{}, err
	}
	record, lyricTotal, err := service.store.FindTrack(ctx, id, page.PageSize, page.Offset)
	if err != nil {
		return TrackDetailDTO{}, err
	}
	items, err := service.presentTracks(ctx, []TrackRecord{record})
	if err != nil {
		return TrackDetailDTO{}, err
	}
	lyrics := make([]LyricDTO, 0, len(record.Lyrics))
	for _, lyric := range record.Lyrics {
		content := ""
		if lyric.Content != nil {
			content = *lyric.Content
		}
		if !sharedlyrics.ValidTiming(lyric.Timing) {
			return TrackDetailDTO{}, apperror.Internal("Stored lyrics timing is invalid", nil)
		}
		if err := sharedlyrics.ValidateDocument(lyric.Format, sharedlyrics.Timing(lyric.Timing), content); err != nil {
			return TrackDetailDTO{}, apperror.Internal("Stored lyrics violate the timing contract", err)
		}
		lyrics = append(lyrics, LyricDTO{
			ID: lyric.ID, Language: lyric.Language, Format: lyric.Format, Timing: lyric.Timing, Content: lyric.Content,
			IsDefault: lyric.IsDefault, Version: lyric.Version, UpdatedAt: formatTimestamp(lyric.UpdatedAt),
		})
	}
	return TrackDetailDTO{
		TrackDTO:        items[0],
		Lyrics:          lyrics,
		LyricPage:       page.Page,
		LyricPageSize:   page.PageSize,
		LyricTotal:      lyricTotal,
		LyricTotalPages: pagination.BoundedTotalPages(lyricTotal, page.PageSize),
	}, nil
}

func exactTotalPages(total, pageSize int) int {
	return pagination.BoundedTotalPages(total, pageSize)
}

func (service *Service) presentArtists(ctx context.Context, records []ArtistRecord) ([]ArtistDTO, error) {
	assetIDs := uniqueAssetIDs(len(records), func(index int) *string { return records[index].ArtworkAssetID })
	artworks, err := service.artworks.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make([]ArtistDTO, 0, len(records))
	for _, record := range records {
		result = append(result, ArtistDTO{
			ID: record.ID, Name: record.Name, Description: record.Description,
			Artwork:    artworkPointer(artworks, record.ArtworkAssetID),
			AlbumCount: record.AlbumCount, TrackCount: record.TrackCount, Version: record.Version,
			CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(record.UpdatedAt),
		})
	}
	return result, nil
}

func (service *Service) presentAlbums(ctx context.Context, records []AlbumRecord) ([]AlbumDTO, error) {
	assetIDs := uniqueAssetIDs(len(records), func(index int) *string { return records[index].CoverAssetID })
	artworks, err := service.artworks.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make([]AlbumDTO, 0, len(records))
	for _, record := range records {
		result = append(result, AlbumDTO{
			ID: record.ID, Title: record.Title, ArtistCredits: presentCredits(record.Credits),
			Description: record.Description, ReleaseDate: record.ReleaseDate,
			Artwork: artworkPointer(artworks, record.CoverAssetID), TrackCount: record.TrackCount,
			Version: record.Version, CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(record.UpdatedAt),
		})
	}
	return result, nil
}

func (service *Service) presentTracks(ctx context.Context, records []TrackRecord) ([]TrackDTO, error) {
	assetIDs := uniqueAssetIDs(len(records), func(index int) *string { return records[index].AlbumCoverAssetID })
	artworks, err := service.artworks.Artworks(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make([]TrackDTO, 0, len(records))
	for _, record := range records {
		credits := presentCredits(record.Credits)
		artists := make([]string, 0, len(credits))
		for _, credit := range credits {
			if credit.Role == "PRIMARY" || credit.Role == "FEATURED" {
				artists = append(artists, credit.Artist.Name)
			}
		}
		var album *AlbumReferenceDTO
		if record.AlbumID != nil && record.AlbumTitle != nil {
			album = &AlbumReferenceDTO{ID: *record.AlbumID, Title: *record.AlbumTitle}
		}
		var source *SourceDTO
		if record.Source != nil {
			var format *string
			extension := strings.TrimPrefix(filepath.Ext(record.Source.RelativePath), ".")
			if extension != "" {
				value := strings.ToUpper(extension)
				format = &value
			}
			rootMode := ""
			if record.Source.Mode != nil {
				rootMode = *record.Source.Mode
			}
			rootEnabled := false
			if record.Source.RootEnabled != nil {
				rootEnabled = *record.Source.RootEnabled
			}
			eligibility := tagwriteback.Evaluate(tagwriteback.SourceContext{
				HasSource: true, TrackStatus: string(record.Status), RootMode: rootMode,
				RootEnabled: rootEnabled, ScanActive: record.Source.ScanActive, SourceStatus: record.Source.Status,
				SourcePath: record.Source.RelativePath, MappingCount: record.Source.MappingCount,
				Cue: record.Source.Cue,
			})
			source = &SourceDTO{
				ID: record.Source.ID, RootID: record.Source.RootID, RootName: record.Source.RootName,
				RelativePath: record.Source.RelativePath, Format: format, Status: record.Source.Status,
				ChecksumSHA256: record.Source.ChecksumSHA256, Mode: record.Source.Mode,
				CanWriteBack: eligibility.CanWriteBack, WritebackBlockReason: eligibility.MessagePointer(),
			}
		}
		var publishedAt *string
		if record.PublishedAt != nil {
			value := formatTimestamp(*record.PublishedAt)
			publishedAt = &value
		}
		result = append(result, TrackDTO{
			ID: record.ID, Title: record.Title, ArtistCredits: credits, Artists: artists,
			Album: album, Artwork: artworkPointer(artworks, record.AlbumCoverAssetID),
			DurationMS: record.DurationMS, TrackNumber: record.TrackNumber, DiscNumber: record.DiscNumber,
			Status: string(record.Status), AudioStatus: record.AudioStatus, MetadataStatus: record.MetadataStatus,
			MetadataVersion: record.MetadataVersion, Source: source, ActiveWritebackJobID: record.ActiveWritebackJobID,
			LatestWritebackErrorCode: record.LatestWritebackErrorCode,
			LatestWritebackError:     record.LatestWritebackError,
			PublishedAt:              publishedAt, Version: record.Version,
			CreatedAt: formatTimestamp(record.CreatedAt), UpdatedAt: formatTimestamp(record.UpdatedAt),
		})
	}
	return result, nil
}

func presentCredits(records []CreditRecord) []CreditDTO {
	result := make([]CreditDTO, 0, len(records))
	for _, record := range records {
		result = append(result, CreditDTO{
			Artist: catalog.ArtistReferenceDTO{ID: record.ArtistID, Name: record.ArtistName},
			Role:   record.Role, SortOrder: record.SortOrder,
		})
	}
	return result
}

func uniqueAssetIDs(capacity int, assetID func(index int) *string) []string {
	result := make([]string, 0, capacity)
	seen := make(map[string]struct{}, capacity)
	for index := 0; index < capacity; index++ {
		value := assetID(index)
		if value == nil || *value == "" {
			continue
		}
		if _, exists := seen[*value]; exists {
			continue
		}
		seen[*value] = struct{}{}
		result = append(result, *value)
	}
	return result
}

func artworkPointer(artworks map[string]catalog.ArtworkDTO, assetID *string) *catalog.ArtworkDTO {
	if assetID == nil {
		return nil
	}
	value, exists := artworks[*assetID]
	if !exists {
		return nil
	}
	return &value
}

func userFacingOperationalError(message, code *string) *string {
	if message == nil || strings.TrimSpace(*message) == "" {
		return nil
	}
	normalized := strings.TrimSpace(*message)
	known := map[string]string{
		"Cancelled by an administrator":                                   "\u4efb\u52a1\u5df2\u7531\u7ba1\u7406\u5458\u53d6\u6d88\u3002",
		"Music source was disabled":                                       "\u97f3\u4e50\u6e90\u5df2\u505c\u7528\uff0c\u4efb\u52a1\u5df2\u53d6\u6d88\u3002",
		"The scan worker lease expired before completion":                 "\u626b\u63cf\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
		"The previous scan stopped before completion":                     "\u4e0a\u4e00\u6b21\u626b\u63cf\u672a\u5b8c\u6210\uff0c\u8bf7\u91cd\u65b0\u626b\u63cf\u3002",
		"The final worker lease expired before completion":                "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
		"Media job lease expired after all retry attempts were used":      "\u5a92\u4f53\u5904\u7406\u591a\u6b21\u91cd\u8bd5\u540e\u4ecd\u672a\u5b8c\u6210\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u72b6\u6001\u3002",
		"Object cleanup lease expired after all retry attempts were used": "\u8d44\u6e90\u6e05\u7406\u591a\u6b21\u91cd\u8bd5\u540e\u4ecd\u672a\u5b8c\u6210\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u72b6\u6001\u3002",
		"A newer upload superseded this media job":                        "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684\u4e0a\u4f20\u66ff\u4ee3\u3002",
		"A newer source generation superseded this media job":             "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684\u97f3\u4e50\u6e90\u7248\u672c\u66ff\u4ee3\u3002",
		"A newer CUE definition superseded this media job":                "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684 CUE \u5b9a\u4e49\u66ff\u4ee3\u3002",
	}
	if value, exists := known[normalized]; exists {
		return &value
	}
	if code != nil {
		byCode := map[string]string{
			"MEDIA_UPLOAD_MISMATCH":    "\u5a92\u4f53\u6587\u4ef6\u6821\u9a8c\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u6587\u4ef6\u683c\u5f0f\u540e\u91cd\u8bd5\u3002",
			"DEPENDENCY_UNAVAILABLE":   "\u76f8\u5173\u5904\u7406\u670d\u52a1\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u914d\u7f6e\u540e\u91cd\u8bd5\u3002",
			"SOURCE_SIZE_MISMATCH":     "\u4ece\u5bf9\u8c61\u5b58\u50a8\u8bfb\u53d6\u7684\u6e90\u97f3\u9891\u4e0d\u5b8c\u6574\uff0c\u5c1a\u672a\u5f00\u59cb\u8f6c\u7801\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"SOURCE_CHECKSUM_MISMATCH": "\u4ece\u5bf9\u8c61\u5b58\u50a8\u8bfb\u53d6\u7684\u6e90\u97f3\u9891\u6821\u9a8c\u5931\u8d25\uff0c\u5c1a\u672a\u5f00\u59cb\u8f6c\u7801\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WORKER_LEASE_EXPIRED":     "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WRITEBACK_LEASE_LOST":     "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WRITEBACK_INTERRUPTED":    "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"SOURCE_CHANGED":           "\u6e90\u6587\u4ef6\u5df2\u53d1\u751f\u53d8\u5316\uff0c\u8bf7\u91cd\u65b0\u626b\u63cf\u540e\u518d\u8bd5\u3002",
			"METADATA_CHANGED":         "\u66f2\u76ee\u4fe1\u606f\u5df2\u53d1\u751f\u53d8\u5316\uff0c\u8bf7\u5237\u65b0\u540e\u91cd\u8bd5\u3002",
			"LIBRARY_SCAN_FAILED":      "\u97f3\u4e50\u6e90\u626b\u63cf\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u76ee\u5f55\u8bbf\u95ee\u6743\u9650\u540e\u91cd\u8bd5\u3002",
		}
		if value, exists := byCode[*code]; exists {
			return &value
		}
	}
	if containsHan(normalized) && !sensitiveOperationalDetail(normalized) {
		value := strings.Join(strings.Fields(normalized), " ")
		value = truncateRunes(value, 1_000)
		return &value
	}
	value := "\u4efb\u52a1\u6267\u884c\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\uff1b\u5982\u95ee\u9898\u6301\u7eed\u51fa\u73b0\uff0c\u8bf7\u67e5\u770b\u670d\u52a1\u7aef\u65e5\u5fd7\u3002"
	return &value
}

func containsHan(value string) bool {
	for _, character := range value {
		if character >= '\u3400' && character <= '\u9fff' {
			return true
		}
	}
	return false
}

func sensitiveOperationalDetail(value string) bool {
	for _, pattern := range operationalSensitivePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	return string([]rune(value)[:maximum])
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func validOrder(value SortOrder) bool { return value == SortAscending || value == SortDescending }

func validAudioStatusFilter(value AudioStatus) bool {
	return value == "" || audiostatus.Valid(value)
}

func validMetadataStatusFilter(value MetadataStatus) bool {
	return value == "" || value == MetadataNormal || value == MetadataPendingWrite ||
		value == MetadataWriteFailed
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

var operationalSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`[A-Za-z]:[\\/]`),
	regexp.MustCompile(`(?i)(?:postgres|postgresql)://`),
	regexp.MustCompile(`(?i)\bBearer\s+`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.`),
	regexp.MustCompile(`\b(?:EACCES|EEXIST|EINVAL|EIO|ENOENT|ENOTDIR|EPERM|ETIMEDOUT|ECONNREFUSED|ECONNRESET|SQLSTATE)\b`),
}
