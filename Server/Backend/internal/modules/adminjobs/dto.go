package adminjobs

type JobErrorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type JobDTO struct {
	ID          string       `json:"id"`
	Type        JobType      `json:"type"`
	Status      JobStatus    `json:"status"`
	Title       string       `json:"title"`
	Progress    int          `json:"progress"`
	Processed   int          `json:"processed"`
	Total       int          `json:"total"`
	Attempts    int          `json:"attempts"`
	CreatedAt   string       `json:"createdAt"`
	StartedAt   *string      `json:"startedAt"`
	CompletedAt *string      `json:"completedAt"`
	Error       *JobErrorDTO `json:"error"`
}

type JobDetailDTO struct {
	JobDTO
	UpdatedAt       string    `json:"updatedAt"`
	MaxAttempts     int       `json:"maxAttempts"`
	Version         *int      `json:"version"`
	Source          JobSource `json:"source"`
	TrackID         *string   `json:"trackId"`
	SourceID        *string   `json:"sourceId"`
	SourceAssetID   *string   `json:"sourceAssetId"`
	CancelRequested bool      `json:"cancelRequested"`
	NextAttemptAt   *string   `json:"nextAttemptAt"`
	LockedUntil     *string   `json:"lockedUntil"`
	HeartbeatAt     *string   `json:"heartbeatAt"`
}

type JobPageDTO struct {
	Items      []JobDTO `json:"items"`
	Page       int      `json:"page"`
	PageSize   int      `json:"pageSize"`
	Total      int      `json:"total"`
	TotalPages int      `json:"totalPages"`
}

type EventStateDTO struct {
	Fingerprint string  `json:"-"`
	UpdatedAt   *string `json:"updatedAt"`
	Active      int     `json:"active"`
}

type ReasonInput struct {
	Reason *string `json:"reason,omitempty"`
}
