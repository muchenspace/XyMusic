package adminmetadata

type MetadataSourceDTO struct {
	ID                   string  `json:"id"`
	RootID               *string `json:"rootId"`
	RelativePath         string  `json:"relativePath"`
	Status               string  `json:"status"`
	ChecksumSHA256       string  `json:"checksumSha256"`
	Mode                 *string `json:"mode"`
	CanWriteBack         bool    `json:"canWriteBack"`
	WritebackBlockReason *string `json:"writebackBlockReason"`
}

type MetadataDTO struct {
	TrackID          string             `json:"trackId"`
	Raw              MetadataSnapshot   `json:"raw"`
	Overrides        MetadataOverrides  `json:"overrides"`
	Effective        MetadataSnapshot   `json:"effective"`
	OverriddenFields []string           `json:"overriddenFields"`
	Source           *MetadataSourceDTO `json:"source"`
	Version          int                `json:"version"`
	LastScannedAt    *string            `json:"lastScannedAt"`
	UpdatedBy        *string            `json:"updatedBy"`
	CreatedAt        string             `json:"createdAt"`
	UpdatedAt        string             `json:"updatedAt"`
}

type BatchUpdateItemDTO struct {
	TrackID       string   `json:"trackId"`
	Version       int      `json:"version"`
	ChangedFields []string `json:"changedFields"`
}

type BatchUpdateDTO struct {
	Items []BatchUpdateItemDTO `json:"items"`
}

type WritebackJobDTO struct {
	ID                   string          `json:"id"`
	TrackID              string          `json:"trackId"`
	SourceID             string          `json:"sourceId"`
	Status               WritebackStatus `json:"status"`
	Stage                WritebackStage  `json:"stage"`
	Attempts             int             `json:"attempts"`
	MaxAttempts          int             `json:"maxAttempts"`
	CancelRequested      bool            `json:"cancelRequested"`
	MetadataVersion      int             `json:"metadataVersion"`
	Reason               string          `json:"reason"`
	OutputChecksumSHA256 *string         `json:"outputChecksumSha256"`
	LastErrorCode        *string         `json:"lastErrorCode"`
	LastError            *string         `json:"lastError"`
	Version              int             `json:"version"`
	NextAttemptAt        string          `json:"nextAttemptAt"`
	StartedAt            *string         `json:"startedAt"`
	CompletedAt          *string         `json:"completedAt"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
}

type WritebackJobPageDTO struct {
	Items      []WritebackJobDTO `json:"items"`
	Page       int               `json:"page"`
	PageSize   int               `json:"pageSize"`
	Total      int               `json:"total"`
	TotalPages int               `json:"totalPages"`
	NextCursor *string           `json:"nextCursor,omitempty"`
}
