package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

const (
	defaultPageLimit       = 20
	maximumPageLimit       = 100
	maximumSafeJSONInteger = int64(9_007_199_254_740_991)
)

type ServiceDependencies struct {
	Repository  Store
	Cursors     *pagination.CursorCodec
	Tracks      TrackPresenter
	Idempotency Idempotency
	Clock       Clock
}

type Service struct {
	repository  Store
	cursors     *pagination.CursorCodec
	tracks      TrackPresenter
	idempotency Idempotency
	clock       Clock
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil {
		return nil, errors.New("library repository is required")
	}
	if dependencies.Cursors == nil {
		return nil, errors.New("library cursor codec is required")
	}
	if dependencies.Tracks == nil {
		return nil, errors.New("library track presenter is required")
	}
	if dependencies.Idempotency == nil {
		return nil, errors.New("library idempotency service is required")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = SystemClock{}
	}
	return &Service{
		repository:  dependencies.Repository,
		cursors:     dependencies.Cursors,
		tracks:      dependencies.Tracks,
		idempotency: dependencies.Idempotency,
		clock:       dependencies.Clock,
	}, nil
}

func (service *Service) ListFavorites(
	ctx context.Context,
	userID string,
	input ListFavoritesInput,
) (FavoritePageDTO, error) {
	if !validFavoriteSort(input.Sort) {
		return FavoritePageDTO{}, apperror.Validation("sort is invalid")
	}
	limit, err := libraryPageLimit(input.Limit)
	if err != nil {
		return FavoritePageDTO{}, err
	}
	scope := fmt.Sprintf("favorites:%s:%s", userID, input.Sort)
	after, err := service.decodeFavoriteCursor(scope, input.Sort, input.Cursor)
	if err != nil {
		return FavoritePageDTO{}, err
	}
	rows, err := service.repository.ListFavorites(ctx, ListFavoritesQuery{
		UserID: userID,
		Sort:   input.Sort,
		After:  after,
		Limit:  limit + 1,
	})
	if err != nil {
		return FavoritePageDTO{}, err
	}
	pageRows, hasNext := libraryPage(rows, limit)
	tracks, err := service.tracks.TrackSummaries(ctx, userID, favoriteTrackIDs(pageRows))
	if err != nil {
		return FavoritePageDTO{}, fmt.Errorf("present favorite tracks: %w", err)
	}
	byID := trackDTOsByID(tracks)
	items := make([]FavoriteItemDTO, 0, len(pageRows))
	for _, row := range pageRows {
		track, found := byID[row.TrackID]
		if !found {
			continue
		}
		items = append(items, FavoriteItemDTO{
			Track:       track,
			FavoritedAt: formatLibraryTimestamp(row.FavoritedAt),
		})
	}
	result := FavoritePageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		encoded, err := service.encodeFavoriteCursor(scope, input.Sort, pageRows[len(pageRows)-1])
		if err != nil {
			return FavoritePageDTO{}, fmt.Errorf("encode favorite cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (service *Service) AddFavorite(
	ctx context.Context,
	userID, trackID string,
) (FavoriteItemDTO, error) {
	exists, err := service.repository.PlayableTrackExists(ctx, trackID)
	if err != nil {
		return FavoriteItemDTO{}, err
	}
	if !exists {
		return FavoriteItemDTO{}, apperror.NotFound("Track was not found")
	}
	favoritedAt, err := service.repository.AddFavorite(ctx, userID, trackID)
	if err != nil {
		return FavoriteItemDTO{}, err
	}
	track, err := service.requirePresentedTrack(ctx, userID, trackID)
	if err != nil {
		return FavoriteItemDTO{}, err
	}
	return FavoriteItemDTO{Track: track, FavoritedAt: formatLibraryTimestamp(favoritedAt)}, nil
}

func (service *Service) RemoveFavorite(ctx context.Context, userID, trackID string) error {
	exists, err := service.repository.TrackExists(ctx, trackID)
	if err != nil {
		return err
	}
	if !exists {
		return apperror.NotFound("Track was not found")
	}
	return service.repository.RemoveFavorite(ctx, userID, trackID)
}

func (service *Service) ListHistory(
	ctx context.Context,
	userID string,
	input ListHistoryInput,
) (HistoryPageDTO, error) {
	limit, err := libraryPageLimit(input.Limit)
	if err != nil {
		return HistoryPageDTO{}, err
	}
	scope := fmt.Sprintf("history:%s", userID)
	after, err := service.decodeHistoryCursor(scope, input.Cursor)
	if err != nil {
		return HistoryPageDTO{}, err
	}
	rows, err := service.repository.ListHistory(ctx, ListHistoryQuery{
		UserID: userID,
		After:  after,
		Limit:  limit + 1,
	})
	if err != nil {
		return HistoryPageDTO{}, err
	}
	pageRows, hasNext := libraryPage(rows, limit)
	tracks, err := service.tracks.TrackSummaries(ctx, userID, historyTrackIDs(pageRows))
	if err != nil {
		return HistoryPageDTO{}, fmt.Errorf("present play history tracks: %w", err)
	}
	byID := trackDTOsByID(tracks)
	items := make([]HistoryItemDTO, 0, len(pageRows))
	for _, row := range pageRows {
		track, found := byID[row.TrackID]
		if !found {
			continue
		}
		items = append(items, presentHistory(row, track))
	}
	result := HistoryPageDTO{Items: items}
	if hasNext && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		encoded, err := pagination.EncodeCursor(service.cursors, scope, historyCursorValue{
			LastPlayedAt: formatLibraryTimestamp(last.LastPlayedAt),
			TrackID:      last.TrackID,
		})
		if err != nil {
			return HistoryPageDTO{}, fmt.Errorf("encode play history cursor: %w", err)
		}
		result.NextCursor = &encoded
	}
	return result, nil
}

func (service *Service) RecordPlayback(
	ctx context.Context,
	userID, trackID, idempotencyKey string,
	input RecordPlaybackInput,
) (MutationResult[HistoryItemDTO], error) {
	return service.idempotency.ExecutePlayback(ctx, IdempotencyInput{
		ActorID: userID,
		Scope:   "library.history:" + trackID,
		Key:     idempotencyKey,
		Payload: input,
	}, func() (HistoryItemDTO, error) {
		return service.recordPlayback(ctx, userID, trackID, input)
	})
}

func (service *Service) recordPlayback(
	ctx context.Context,
	userID, trackID string,
	input RecordPlaybackInput,
) (HistoryItemDTO, error) {
	if !validLibraryUUID(input.PlaybackSessionID) {
		return HistoryItemDTO{}, apperror.Validation("playbackSessionId must be a UUID")
	}
	if input.PositionMS < 0 || input.PositionMS > maximumSafeJSONInteger {
		return HistoryItemDTO{}, apperror.Validation("positionMs must be a non-negative integer")
	}
	if !validPlaybackEvent(input.Event) {
		return HistoryItemDTO{}, apperror.Validation("event is invalid")
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, input.OccurredAt)
	if err != nil {
		return HistoryItemDTO{}, apperror.Validation("occurredAt must be an ISO date-time")
	}
	if occurredAt.After(service.clock.Now().Add(5 * time.Minute)) {
		return HistoryItemDTO{}, apperror.Validation("occurredAt is too far in the future")
	}
	track, err := service.requirePresentedTrack(ctx, userID, trackID)
	if err != nil {
		return HistoryItemDTO{}, err
	}
	history, err := service.repository.UpsertPlayback(ctx, PlaybackWrite{
		UserID:            userID,
		TrackID:           trackID,
		PlaybackSessionID: input.PlaybackSessionID,
		PositionMS:        input.PositionMS,
		OccurredAt:        occurredAt,
		Event:             input.Event,
		UpdatedAt:         service.clock.Now(),
	})
	if err != nil {
		return HistoryItemDTO{}, err
	}
	return presentHistory(history, track), nil
}

func (service *Service) requirePresentedTrack(
	ctx context.Context,
	userID, trackID string,
) (catalog.TrackSummaryDTO, error) {
	tracks, err := service.tracks.TrackSummaries(ctx, userID, []string{trackID})
	if err != nil {
		return catalog.TrackSummaryDTO{}, fmt.Errorf("present library track: %w", err)
	}
	if len(tracks) == 0 {
		return catalog.TrackSummaryDTO{}, apperror.NotFound("Track was not found")
	}
	for _, track := range tracks {
		if track.ID == trackID {
			return track, nil
		}
	}
	return catalog.TrackSummaryDTO{}, apperror.NotFound("Track was not found")
}

type favoriteCursorValue struct {
	CreatedAt *string `json:"createdAt,omitempty"`
	Title     *string `json:"title,omitempty"`
	TrackID   string  `json:"trackId"`
}

type historyCursorValue struct {
	LastPlayedAt string `json:"lastPlayedAt"`
	TrackID      string `json:"trackId"`
}

func (service *Service) decodeFavoriteCursor(
	scope string,
	sort FavoriteSort,
	encoded string,
) (*FavoriteCursor, error) {
	value, err := pagination.DecodeCursor[favoriteCursorValue](service.cursors, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.TrackID == "" {
		return nil, invalidLibraryCursor()
	}
	result := &FavoriteCursor{TrackID: value.TrackID}
	switch sort {
	case FavoriteSortFavoritedDesc:
		if value.CreatedAt == nil {
			return nil, invalidLibraryCursor()
		}
		createdAt, err := time.Parse(time.RFC3339Nano, *value.CreatedAt)
		if err != nil {
			return nil, invalidLibraryCursor()
		}
		result.CreatedAt = &createdAt
	case FavoriteSortTitleAsc:
		if value.Title == nil {
			return nil, invalidLibraryCursor()
		}
		result.Title = value.Title
	default:
		return nil, invalidLibraryCursor()
	}
	return result, nil
}

func (service *Service) encodeFavoriteCursor(
	scope string,
	sort FavoriteSort,
	record FavoriteRecord,
) (string, error) {
	value := favoriteCursorValue{TrackID: record.TrackID}
	switch sort {
	case FavoriteSortFavoritedDesc:
		createdAt := formatLibraryTimestamp(record.FavoritedAt)
		value.CreatedAt = &createdAt
	case FavoriteSortTitleAsc:
		title := record.NormalizedTitle
		value.Title = &title
	default:
		return "", invalidLibraryCursor()
	}
	return pagination.EncodeCursor(service.cursors, scope, value)
}

func (service *Service) decodeHistoryCursor(scope, encoded string) (*HistoryCursor, error) {
	value, err := pagination.DecodeCursor[historyCursorValue](service.cursors, scope, encoded)
	if err != nil || value == nil {
		return nil, err
	}
	if value.TrackID == "" || value.LastPlayedAt == "" {
		return nil, invalidLibraryCursor()
	}
	lastPlayedAt, err := time.Parse(time.RFC3339Nano, value.LastPlayedAt)
	if err != nil {
		return nil, invalidLibraryCursor()
	}
	return &HistoryCursor{LastPlayedAt: lastPlayedAt, TrackID: value.TrackID}, nil
}

func validFavoriteSort(value FavoriteSort) bool {
	return value == FavoriteSortFavoritedDesc || value == FavoriteSortTitleAsc
}

func validPlaybackEvent(value PlaybackEvent) bool {
	switch value {
	case PlaybackEventStarted, PlaybackEventProgress, PlaybackEventPaused, PlaybackEventCompleted:
		return true
	default:
		return false
	}
}

func libraryPageLimit(value *int) (int, error) {
	if value == nil {
		return defaultPageLimit, nil
	}
	if *value < 1 || *value > maximumPageLimit {
		return 0, apperror.Validation("limit must be from 1 to 100")
	}
	return *value, nil
}

func libraryPage[T any](rows []T, limit int) ([]T, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

func favoriteTrackIDs(rows []FavoriteRecord) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.TrackID)
	}
	return result
}

func historyTrackIDs(rows []HistoryRecord) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.TrackID)
	}
	return result
}

func trackDTOsByID(tracks []catalog.TrackSummaryDTO) map[string]catalog.TrackSummaryDTO {
	result := make(map[string]catalog.TrackSummaryDTO, len(tracks))
	for _, track := range tracks {
		result[track.ID] = track
	}
	return result
}

func presentHistory(record HistoryRecord, track catalog.TrackSummaryDTO) HistoryItemDTO {
	return HistoryItemDTO{
		Track:          track,
		LastPositionMS: record.LastPositionMS,
		PlayCount:      record.PlayCount,
		LastPlayedAt:   formatLibraryTimestamp(record.LastPlayedAt),
		Completed:      record.Completed,
		UpdatedAt:      formatLibraryTimestamp(record.UpdatedAt),
	}
}

func invalidLibraryCursor() error {
	return apperror.InvalidCursor("Cursor is invalid")
}

func formatLibraryTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
