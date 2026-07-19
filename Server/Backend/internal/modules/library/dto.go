package library

import "xymusic/server/internal/modules/catalog"

type ListFavoritesInput struct {
	Cursor string
	Limit  *int
	Sort   FavoriteSort
}

type FavoriteItemDTO struct {
	Track       catalog.TrackSummaryDTO `json:"track"`
	FavoritedAt string                  `json:"favoritedAt"`
}

type FavoritePageDTO struct {
	Items      []FavoriteItemDTO `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type ListHistoryInput struct {
	Cursor string
	Limit  *int
}

type RecordPlaybackInput struct {
	PlaybackSessionID string        `json:"playbackSessionId"`
	PositionMS        int64         `json:"positionMs"`
	OccurredAt        string        `json:"occurredAt"`
	Event             PlaybackEvent `json:"event"`
}

type HistoryItemDTO struct {
	Track          catalog.TrackSummaryDTO `json:"track"`
	LastPositionMS int64                   `json:"lastPositionMs"`
	PlayCount      int64                   `json:"playCount"`
	LastPlayedAt   string                  `json:"lastPlayedAt"`
	Completed      bool                    `json:"completed"`
	UpdatedAt      string                  `json:"updatedAt"`
}

type HistoryPageDTO struct {
	Items      []HistoryItemDTO `json:"items"`
	NextCursor *string          `json:"nextCursor"`
}

type MutationResult[T any] struct {
	Body     T
	Replayed bool
}
