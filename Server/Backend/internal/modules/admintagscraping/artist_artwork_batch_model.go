package admintagscraping

import "time"

type ArtistArtworkBatchItemInput struct {
	ArtistID        string `json:"artistId"`
	ExpectedVersion int    `json:"expectedVersion"`
}

type ArtistArtworkBatchOptions struct {
	Sources   []Source `json:"sources"`
	Overwrite bool     `json:"overwrite"`
	Reason    string   `json:"reason"`
}

type CreateArtistArtworkBatchInput struct {
	Items   []ArtistArtworkBatchItemInput `json:"items"`
	Options ArtistArtworkBatchOptions     `json:"options"`
}

type ArtistArtworkBatchCreateResult struct {
	Job               *ArtistArtworkBatchJobDTO `json:"job"`
	Selected          int                       `json:"selected"`
	ConditionExcluded int                       `json:"conditionExcluded"`
}

type ArtistArtworkBatchJobRecord struct {
	ID              string
	RequestedBy     *string
	Options         ArtistArtworkBatchOptions
	Status          JobStatus
	Total           int
	Processed       int
	Succeeded       int
	Failed          int
	CancelRequested bool
	StartedAt       *time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ArtistArtworkBatchItemRecord struct {
	ID              string
	JobID           string
	ArtistID        string
	ExpectedVersion int
	Position        int
	Status          ItemStatus
	Attempts        int
	MaxAttempts     int
	NextAttemptAt   time.Time
	AttemptID       *string
	LockedBy        *string
	LockedUntil     *time.Time
	Candidate       *ArtistCandidate
	Source          *Source
	Message         *string
	StartedAt       *time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ArtistArtworkBatchTarget struct {
	ID             string
	Name           string
	NormalizedName string
	Version        int
	HasArtwork     bool
	PerformerRole  bool
}

type ClaimedArtistArtworkBatchItem struct {
	Job       ArtistArtworkBatchJobRecord
	Item      ArtistArtworkBatchItemRecord
	Target    ArtistArtworkBatchTarget
	AttemptID string
}

type ArtistArtworkBatchClaimResult struct {
	Item        *ClaimedArtistArtworkBatchItem
	FinishJobID string
}

type ArtistArtworkBatchJobDTO struct {
	ID              string                      `json:"id"`
	RequestedBy     *string                     `json:"requestedBy"`
	Options         ArtistArtworkBatchOptions   `json:"options"`
	Status          JobStatus                   `json:"status"`
	Total           int                         `json:"total"`
	Processed       int                         `json:"processed"`
	Succeeded       int                         `json:"succeeded"`
	Failed          int                         `json:"failed"`
	Skipped         int                         `json:"skipped"`
	CancelRequested bool                        `json:"cancelRequested"`
	StartedAt       *string                     `json:"startedAt"`
	CompletedAt     *string                     `json:"completedAt"`
	CreatedAt       string                      `json:"createdAt"`
	UpdatedAt       string                      `json:"updatedAt"`
	PartialItems    bool                        `json:"partialItems"`
	Items           []ArtistArtworkBatchItemDTO `json:"items"`
}

type ArtistArtworkBatchItemDTO struct {
	ID              string           `json:"id"`
	JobID           string           `json:"jobId"`
	ArtistID        string           `json:"artistId"`
	ExpectedVersion int              `json:"expectedVersion"`
	Position        int              `json:"position"`
	Status          ItemStatus       `json:"status"`
	Attempts        int              `json:"attempts"`
	MaxAttempts     int              `json:"maxAttempts"`
	NextAttemptAt   string           `json:"nextAttemptAt"`
	Candidate       *ArtistCandidate `json:"candidate"`
	Source          *Source          `json:"source"`
	Message         *string          `json:"message"`
	StartedAt       *string          `json:"startedAt"`
	CompletedAt     *string          `json:"completedAt"`
	CreatedAt       string           `json:"createdAt"`
	UpdatedAt       string           `json:"updatedAt"`
}
