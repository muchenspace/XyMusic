package adminmutation

import "xymusic/server/internal/modules/catalog"

type CreditDTO struct {
	Artist    catalog.ArtistReferenceDTO `json:"artist"`
	Role      CreditRole                 `json:"role"`
	SortOrder int                        `json:"sortOrder"`
}

type ArtistDTO struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description *string             `json:"description"`
	Artwork     *catalog.ArtworkDTO `json:"artwork"`
	Version     int                 `json:"version"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt"`
}

type AlbumDTO struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	ArtistCredits []CreditDTO         `json:"artistCredits"`
	ReleaseDate   *string             `json:"releaseDate"`
	Description   *string             `json:"description"`
	Cover         *catalog.ArtworkDTO `json:"cover"`
	Version       int                 `json:"version"`
	CreatedAt     string              `json:"createdAt"`
	UpdatedAt     string              `json:"updatedAt"`
}

type AlbumReferenceDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type TrackDTO struct {
	ID               string              `json:"id"`
	Title            string              `json:"title"`
	Album            *AlbumReferenceDTO  `json:"album"`
	ArtistCredits    []CreditDTO         `json:"artistCredits"`
	Artwork          *catalog.ArtworkDTO `json:"artwork"`
	DurationMS       *int64              `json:"durationMs"`
	TrackNumber      *int                `json:"trackNumber"`
	DiscNumber       int                 `json:"discNumber"`
	Status           string              `json:"status"`
	ActiveMediaJobID *string             `json:"activeMediaJobId"`
	Version          int                 `json:"version"`
	CreatedAt        string              `json:"createdAt"`
	UpdatedAt        string              `json:"updatedAt"`
}

type MergeResultDTO struct {
	TargetAlbumID string `json:"targetAlbumId"`
	MergedAlbums  int    `json:"mergedAlbums"`
	MovedTracks   int    `json:"movedTracks"`
	TargetVersion int    `json:"targetVersion"`
}

type DeleteTrackDTO struct {
	Deleted          bool `json:"deleted"`
	DeletedFiles     int  `json:"deletedFiles"`
	QuarantinedFiles int  `json:"quarantinedFiles"`
	ScheduledObjects int  `json:"scheduledObjects"`
}

type LyricDTO struct {
	ID           string `json:"id"`
	TrackID      string `json:"trackId"`
	Language     string `json:"language"`
	Format       string `json:"format"`
	Content      string `json:"content"`
	IsDefault    bool   `json:"isDefault"`
	TrackVersion int    `json:"trackVersion"`
	UpdatedAt    string `json:"updatedAt"`
}

type UserStatusDTO struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        string     `json:"role"`
	Status      UserStatus `json:"status"`
	Version     int        `json:"version"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
}
