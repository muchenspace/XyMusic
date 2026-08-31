package adminjobs

import "time"

type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusSucceeded JobStatus = "SUCCEEDED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusCanceled  JobStatus = "CANCELED"
)

type JobType string

const (
	JobTypeSourceScan JobType = "SOURCE_SCAN"
	JobTypeTagWrite   JobType = "TAG_WRITE"
)

type JobSource string

const (
	JobSourceScan JobSource = "SCAN"
	JobSourceTag  JobSource = "TAG"
)

type SortField string

const (
	SortCreatedAt SortField = "createdAt"
	SortUpdatedAt SortField = "updatedAt"
	SortStatus    SortField = "status"
	SortType      SortField = "type"
	SortTitle     SortField = "title"
)

type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

type ListInput struct {
	Page       int
	PageSize   int
	Search     string
	Status     JobStatus
	Type       JobType
	Sort       SortField
	Order      SortOrder
	Cursor     string
	CursorMode bool
}

type ListQuery struct {
	Search     string
	Status     JobStatus
	Type       JobType
	Sort       SortField
	Order      SortOrder
	Limit      int
	Offset     int
	After      *JobCursor
	CursorMode bool
	TotalHint  *int
}

type JobCursor struct {
	Value string
	ID    string
	Total *int
}

type JobRecord struct {
	ID              string
	Type            JobType
	Status          JobStatus
	Source          JobSource
	Title           string
	Progress        int
	Processed       int
	Total           int
	Attempts        int
	MaxAttempts     int
	Version         *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	ErrorCode       *string
	ErrorMessage    *string
	TrackID         *string
	SourceID        *string
	SourceAssetID   *string
	CancelRequested bool
	NextAttemptAt   *time.Time
	LockedUntil     *time.Time
	HeartbeatAt     *time.Time
}

type EventRecord struct {
	UpdatedAt *time.Time
	Active    int
}

type MetadataMutationInput struct {
	ExpectedVersion int
	Reason          string
}

type MetadataCancelInput struct {
	ExpectedVersion int
}
