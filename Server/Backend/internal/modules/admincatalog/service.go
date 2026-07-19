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
	"xymusic/server/internal/shared/pagination"
	"xymusic/server/internal/shared/tagwriteback"
)

type Service struct {
	store    Store
	artworks ArtworkPresenter
}

func NewService(store Store, artworks ArtworkPresenter) (*Service, error) {
	if store == nil {
		return nil, errors.New("admin catalog store is required")
	}
	if artworks == nil {
		return nil, errors.New("admin catalog artwork presenter is required")
	}
	return &Service{store: store, artworks: artworks}, nil
}

func (service *Service) ListArtists(ctx context.Context, input ListInput) (ArtistPageDTO, error) {
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	if input.Sort == "" {
		input.Sort = "name"
	}
	if input.Order == "" {
		input.Order = SortAscending
	}
	if !oneOf(input.Sort, "name", "createdAt", "updatedAt") || !validOrder(input.Order) {
		return ArtistPageDTO{}, apperror.Validation("Artist query is invalid")
	}
	records, total, err := service.store.ListArtists(ctx, ArtistQuery{
		Search: strings.TrimSpace(input.Search), Sort: input.Sort, Order: input.Order,
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return ArtistPageDTO{}, err
	}
	items, err := service.presentArtists(ctx, records)
	if err != nil {
		return ArtistPageDTO{}, err
	}
	return ArtistPageDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
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
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	if input.Sort == "" {
		input.Sort = "updatedAt"
	}
	if input.Order == "" {
		input.Order = SortDescending
	}
	if !oneOf(input.Sort, "title", "createdAt", "updatedAt", "releaseDate") || !validOrder(input.Order) {
		return AlbumPageDTO{}, apperror.Validation("Album query is invalid")
	}
	records, total, err := service.store.ListAlbums(ctx, AlbumQuery{
		Search: strings.TrimSpace(input.Search), Sort: input.Sort, Order: input.Order,
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return AlbumPageDTO{}, err
	}
	items, err := service.presentAlbums(ctx, records)
	if err != nil {
		return AlbumPageDTO{}, err
	}
	return AlbumPageDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func (service *Service) DuplicateAlbums(ctx context.Context, input DuplicateAlbumInput) (DuplicateAlbumsDTO, error) {
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return DuplicateAlbumsDTO{}, err
	}
	albumPage, err := pagination.ParseOffset(input.AlbumPage, input.AlbumPageSize, 100)
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
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
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
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return TrackPageDTO{}, err
	}
	if input.Sort == "" {
		input.Sort = "updatedAt"
	}
	if input.Order == "" {
		input.Order = SortDescending
	}
	if !oneOf(input.Sort, "title", "createdAt", "updatedAt", "status") || !validOrder(input.Order) ||
		!validAudioStatusFilter(input.Status) || !validMetadataStatusFilter(input.MetadataStatus) {
		return TrackPageDTO{}, apperror.Validation("Track query is invalid")
	}
	records, total, err := service.store.ListTracks(ctx, TrackQuery{
		Search: strings.TrimSpace(input.Search), Sort: input.Sort, Order: input.Order,
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
		lyrics = append(lyrics, LyricDTO{
			ID: lyric.ID, Language: lyric.Language, Format: lyric.Format, Content: lyric.Content,
			IsDefault: lyric.IsDefault, Version: lyric.Version, UpdatedAt: formatTimestamp(lyric.UpdatedAt),
		})
	}
	variants := append([]VariantRecord(nil), record.Variants...)
	sort.SliceStable(variants, func(left, right int) bool {
		if variants[left].Bitrate != variants[right].Bitrate {
			return variants[left].Bitrate > variants[right].Bitrate
		}
		return variants[left].ID < variants[right].ID
	})
	variantDTOs := make([]VariantDTO, 0, len(variants))
	for _, variant := range variants {
		variantDTOs = append(variantDTOs, VariantDTO{
			ID: variant.ID, Quality: variant.Quality, MimeType: variant.MimeType, Codec: variant.Codec,
			Container: variant.Container, Bitrate: variant.Bitrate, SampleRate: variant.SampleRate,
			Status: variant.Status, UpdatedAt: formatTimestamp(variant.UpdatedAt),
		})
	}
	return TrackDetailDTO{
		TrackDTO:        items[0],
		Lyrics:          lyrics,
		LyricPage:       page.Page,
		LyricPageSize:   page.PageSize,
		LyricTotal:      lyricTotal,
		LyricTotalPages: exactTotalPages(lyricTotal, page.PageSize),
		Variants:        variantDTOs,
	}, nil
}

func exactTotalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func (service *Service) presentArtists(ctx context.Context, records []ArtistRecord) ([]ArtistDTO, error) {
	assetIDs := make([]string, 0, len(records))
	for _, record := range records {
		if record.ArtworkAssetID != nil {
			assetIDs = append(assetIDs, *record.ArtworkAssetID)
		}
	}
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
	assetIDs := make([]string, 0, len(records))
	for _, record := range records {
		if record.CoverAssetID != nil {
			assetIDs = append(assetIDs, *record.CoverAssetID)
		}
	}
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
	assetIDs := make([]string, 0, len(records))
	for _, record := range records {
		if record.AlbumCoverAssetID != nil {
			assetIDs = append(assetIDs, *record.AlbumCoverAssetID)
		}
	}
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
			rootStatus := ""
			if record.Source.RootStatus != nil {
				rootStatus = *record.Source.RootStatus
			}
			eligibility := tagwriteback.Evaluate(tagwriteback.SourceContext{
				HasSource: true, TrackStatus: string(record.Status), RootMode: rootMode,
				RootEnabled: rootEnabled, RootStatus: rootStatus, SourceStatus: record.Source.Status,
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
		var processing *MediaProcessingDTO
		if record.MediaProcessing != nil {
			processing = &MediaProcessingDTO{
				Status: record.MediaProcessing.Status, Attempts: record.MediaProcessing.Attempts,
				MaxAttempts: record.MediaProcessing.MaxAttempts,
				LastError:   userFacingOperationalError(record.MediaProcessing.LastError, record.MediaProcessing.LastErrorCode),
				UpdatedAt:   formatTimestamp(record.MediaProcessing.UpdatedAt),
			}
		}
		variantSummary := make([]VariantSummaryDTO, 0, len(record.Variants))
		for _, variant := range record.Variants {
			variantSummary = append(variantSummary, VariantSummaryDTO{
				Quality: variant.Quality, Codec: variant.Codec, Container: variant.Container,
				Bitrate: variant.Bitrate, SampleRate: variant.SampleRate, Status: variant.Status,
			})
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
			MetadataVersion: record.MetadataVersion, Source: source, MediaProcessing: processing,
			VariantSummary: variantSummary, ActiveWritebackJobID: record.ActiveWritebackJobID,
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
	return value == "" || value == MetadataOriginal || value == MetadataOverridden ||
		value == MetadataPendingWrite || value == MetadataWriteFailed
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
