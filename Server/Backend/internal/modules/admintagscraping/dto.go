package admintagscraping

import "context"

type SearchInput struct {
	Source   Source   `json:"source"`
	Verbatim bool     `json:"verbatim"`
	Query    *string  `json:"query,omitempty"`
	Title    *string  `json:"title,omitempty"`
	Artist   *string  `json:"artist,omitempty"`
	Album    *string  `json:"album,omitempty"`
	Sources  []Source `json:"sources,omitempty"`
}

type CandidateDetailsInput struct {
	Candidate Candidate `json:"candidate"`
	Verbatim  bool      `json:"verbatim"`
}

type CandidateDetailsDTO struct {
	Candidate Candidate       `json:"candidate"`
	Lyrics    *MetadataLyrics `json:"lyrics"`
}

type ApplyInput struct {
	ExpectedVersion   int         `json:"expectedVersion"`
	Candidate         Candidate   `json:"candidate"`
	Verbatim          bool        `json:"verbatim"`
	Fields            ApplyFields `json:"fields"`
	WriteBack         bool        `json:"writeBack"`
	Reason            string      `json:"reason"`
	cancellationCheck func(context.Context) error
	// Batch workers use this to avoid replaying a partially applied version.
	retryTransientOptional bool
}

type BatchItemInput struct {
	TrackID         string `json:"trackId"`
	ExpectedVersion int    `json:"expectedVersion"`
}

type CreateBatchInput struct {
	Items   []BatchItemInput `json:"items"`
	Options BatchOptions     `json:"options"`
}

type ApplyResult struct {
	Metadata      TrackMetadata `json:"metadata"`
	AppliedFields []string      `json:"appliedFields"`
	CoverApplied  bool          `json:"coverApplied"`
	Warnings      []string      `json:"warnings"`
	WritebackJob  *WritebackJob `json:"writebackJob,omitempty"`
}

type BatchItemDTO struct {
	ID              string     `json:"id"`
	JobID           string     `json:"jobId"`
	TrackID         string     `json:"trackId"`
	ExpectedVersion int        `json:"expectedVersion"`
	Position        int        `json:"position"`
	Status          ItemStatus `json:"status"`
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"maxAttempts"`
	NextAttemptAt   string     `json:"nextAttemptAt"`
	Candidate       *Candidate `json:"candidate"`
	Source          *Source    `json:"source"`
	Message         *string    `json:"message"`
	CreatedAt       string     `json:"createdAt"`
	UpdatedAt       string     `json:"updatedAt"`
	StartedAt       *string    `json:"startedAt"`
	CompletedAt     *string    `json:"completedAt"`
}

type BatchJobDTO struct {
	ID              string         `json:"id"`
	RequestedBy     *string        `json:"requestedBy"`
	Options         BatchOptions   `json:"options"`
	Status          JobStatus      `json:"status"`
	Total           int            `json:"total"`
	Processed       int            `json:"processed"`
	Succeeded       int            `json:"succeeded"`
	Failed          int            `json:"failed"`
	Skipped         int            `json:"skipped"`
	CancelRequested bool           `json:"cancelRequested"`
	StartedAt       *string        `json:"startedAt"`
	CompletedAt     *string        `json:"completedAt"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
	Unsuccessful    int            `json:"unsuccessful"`
	PartialItems    bool           `json:"partialItems"`
	Items           []BatchItemDTO `json:"items"`
}
