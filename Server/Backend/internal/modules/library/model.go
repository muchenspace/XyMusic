package library

import "time"

type FavoriteSort string

const (
	FavoriteSortFavoritedDesc FavoriteSort = "FAVORITED_DESC"
	FavoriteSortTitleAsc      FavoriteSort = "TITLE_ASC"
)

type PlaybackEvent string

const (
	PlaybackEventStarted   PlaybackEvent = "STARTED"
	PlaybackEventProgress  PlaybackEvent = "PROGRESS"
	PlaybackEventPaused    PlaybackEvent = "PAUSED"
	PlaybackEventCompleted PlaybackEvent = "COMPLETED"
)

type FavoriteCursor struct {
	CreatedAt *time.Time
	Title     *string
	TrackID   string
}

type HistoryCursor struct {
	LastPlayedAt time.Time
	TrackID      string
}

type FavoriteRecord struct {
	TrackID         string
	FavoritedAt     time.Time
	NormalizedTitle string
}

type HistoryRecord struct {
	TrackID               string
	LastPositionMS        int64
	PlayCount             int64
	LastPlayedAt          time.Time
	Completed             bool
	LastPlaybackSessionID string
	UpdatedAt             time.Time
}

type ListFavoritesQuery struct {
	UserID string
	Sort   FavoriteSort
	After  *FavoriteCursor
	Limit  int
}

type ListHistoryQuery struct {
	UserID string
	After  *HistoryCursor
	Limit  int
}

type PlaybackWrite struct {
	UserID            string
	TrackID           string
	PlaybackSessionID string
	PositionMS        int64
	OccurredAt        time.Time
	Event             PlaybackEvent
	UpdatedAt         time.Time
}
