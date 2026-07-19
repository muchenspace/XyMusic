package adminmutation

import (
	"errors"
	"time"
)

var ErrPermanentDeleteLeaseLost = errors.New("permanent delete batch item lease was lost")

type BatchTrackItemInput struct {
	TrackID         string `json:"trackId"`
	ExpectedVersion int    `json:"expectedVersion"`
}

type BatchTrackMutationInput struct {
	Items []BatchTrackItemInput `json:"items"`
}

type BatchRestoreItemRecord struct {
	TrackID string
	Status  string
	Version int
}

type BatchRestoreItemDTO struct {
	TrackID string `json:"trackId"`
	Status  string `json:"status"`
	Version int    `json:"version"`
}

type BatchRestoreDTO struct {
	Restored int                   `json:"restored"`
	Items    []BatchRestoreItemDTO `json:"items"`
}

type DeleteBatchStatus string

const (
	DeleteBatchPending   DeleteBatchStatus = "PENDING"
	DeleteBatchRunning   DeleteBatchStatus = "RUNNING"
	DeleteBatchCompleted DeleteBatchStatus = "COMPLETED"
	DeleteBatchFailed    DeleteBatchStatus = "FAILED"
)

type DeleteBatchItemStatus string

const (
	DeleteBatchItemPending   DeleteBatchItemStatus = "PENDING"
	DeleteBatchItemRunning   DeleteBatchItemStatus = "RUNNING"
	DeleteBatchItemSucceeded DeleteBatchItemStatus = "SUCCEEDED"
	DeleteBatchItemFailed    DeleteBatchItemStatus = "FAILED"
)

type PermanentDeleteBatchRecord struct {
	ID          string
	RequestedBy *string
	TraceID     string
	Status      DeleteBatchStatus
	Total       int
	Processed   int
	Succeeded   int
	Failed      int
	StartedAt   *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PermanentDeleteBatchItemRecord struct {
	ID               string
	JobID            string
	TrackID          string
	ExpectedVersion  int
	Position         int
	Status           DeleteBatchItemStatus
	Attempts         int
	NextAttemptAt    time.Time
	AttemptID        *string
	LockedBy         *string
	LockedUntil      *time.Time
	HeartbeatAt      *time.Time
	DeletedFiles     int
	QuarantinedFiles int
	ScheduledObjects int
	ErrorCode        *string
	Message          *string
	StartedAt        *time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ClaimedPermanentDeleteItem struct {
	Job  PermanentDeleteBatchRecord
	Item PermanentDeleteBatchItemRecord
}

type PermanentDeleteBatchItemDTO struct {
	ID               string                `json:"id"`
	TrackID          string                `json:"trackId"`
	ExpectedVersion  int                   `json:"expectedVersion"`
	Position         int                   `json:"position"`
	Status           DeleteBatchItemStatus `json:"status"`
	Attempts         int                   `json:"attempts"`
	DeletedFiles     int                   `json:"deletedFiles"`
	QuarantinedFiles int                   `json:"quarantinedFiles"`
	ScheduledObjects int                   `json:"scheduledObjects"`
	ErrorCode        *string               `json:"errorCode"`
	Message          *string               `json:"message"`
	StartedAt        *string               `json:"startedAt"`
	CompletedAt      *string               `json:"completedAt"`
	CreatedAt        string                `json:"createdAt"`
	UpdatedAt        string                `json:"updatedAt"`
}

type PermanentDeleteBatchDTO struct {
	ID          string                        `json:"id"`
	Status      DeleteBatchStatus             `json:"status"`
	Total       int                           `json:"total"`
	Processed   int                           `json:"processed"`
	Succeeded   int                           `json:"succeeded"`
	Failed      int                           `json:"failed"`
	CreatedAt   string                        `json:"createdAt"`
	UpdatedAt   string                        `json:"updatedAt"`
	StartedAt   *string                       `json:"startedAt"`
	CompletedAt *string                       `json:"completedAt"`
	Items       []PermanentDeleteBatchItemDTO `json:"items"`
}
