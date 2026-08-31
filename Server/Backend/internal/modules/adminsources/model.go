package adminsources

import "time"

type RootMode string

const (
	RootModeReadOnly  RootMode = "READ_ONLY"
	RootModeReadWrite RootMode = "READ_WRITE"
)

type RootStatus string

const (
	RootStatusUnknown  RootStatus = "UNKNOWN"
	RootStatusReady    RootStatus = "READY"
	RootStatusScanning RootStatus = "SCANNING"
	RootStatusError    RootStatus = "ERROR"
	RootStatusDisabled RootStatus = "DISABLED"
)

type ScanStatus string

const (
	ScanStatusPending   ScanStatus = "PENDING"
	ScanStatusRunning   ScanStatus = "RUNNING"
	ScanStatusCompleted ScanStatus = "COMPLETED"
	ScanStatusFailed    ScanStatus = "FAILED"
	ScanStatusCancelled ScanStatus = "CANCELLED"
)

type SourceFileStatus string

const (
	SourceFilePending    SourceFileStatus = "PENDING"
	SourceFileProcessing SourceFileStatus = "PROCESSING"
	SourceFileReady      SourceFileStatus = "READY"
	SourceFileFailed     SourceFileStatus = "FAILED"
	SourceFileMissing    SourceFileStatus = "MISSING"
)

type Root struct {
	ID             string
	Name           string
	Path           string
	NormalizedPath string
	Mode           RootMode
	// ConfigurationManaged is true for the legacy/default root that is owned by
	// LOCAL_MUSIC_* settings. Roots created or edited through the admin source
	// API are database-owned and must not be overwritten when the runtime is
	// rebuilt for an unrelated settings change.
	ConfigurationManaged bool
	Enabled              bool
	ScanOnStartup        bool
	ScanIntervalMinutes  *int
	IncludePatterns      []string
	ExcludePatterns      []string
	Status               RootStatus
	LastScanAt           *time.Time
	LastError            *string
	Version              int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type RootCounts struct {
	FileCount       int
	FailedFileCount int
	TrackCount      int
	CueFileCount    int
}

type RootView struct {
	Root       Root
	Counts     RootCounts
	LatestRun  *ScanRun
	ScanActive bool
}

type ScanRun struct {
	ID              string
	RootID          string
	RootVersion     int
	TriggeredBy     *string
	Status          ScanStatus
	DiscoveredFiles int
	ProcessedFiles  int
	FailedFiles     int
	CancelRequested bool
	AttemptID       *string
	LockedBy        *string
	LockedUntil     *time.Time
	HeartbeatAt     *time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	LastError       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SourceFile struct {
	ID          string
	Path        string
	Status      SourceFileStatus
	LastError   *string
	SizeBytes   int64
	ModifiedAt  time.Time
	TrackID     string
	TrackTitle  string
	TrackStatus string
	TrackCount  int
	Cue         bool
}

type ProcessingJob struct {
	ID            string
	Status        string
	Title         string
	Attempts      int
	MaxAttempts   int
	LastError     *string
	LastErrorCode *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ProcessingSummary struct {
	Queued     int
	Processing int
	Completed  int
	Failed     int
	Cancelled  int
	UpdatedAt  *time.Time
	Jobs       []ProcessingJob
}

type RootMutation struct {
	Name                string
	Path                string
	NormalizedPath      string
	Mode                RootMode
	Enabled             bool
	ScanOnStartup       bool
	ScanIntervalMinutes *int
	IncludePatterns     []string
	ExcludePatterns     []string
	Status              RootStatus
}

type UpdateRootCommand struct {
	RootID          string
	ExpectedVersion int
	Mutation        RootMutation
}

type DeleteRootCommand struct {
	RootID          string
	ExpectedVersion int
	ArchiveCatalog  bool
}

type EnqueueScanCommand struct {
	RootID      string
	ActorID     *string
	Deduplicate bool
}

type CancelScanCommand struct {
	RootID string
	RunID  string
}

type FileQuery struct {
	Page       int
	PageSize   int
	Limit      int
	Offset     int
	Query      string
	Status     SourceFileStatus
	Cursor     string
	CursorMode bool
	After      *SourceFileCursor
	TotalHint  *int
}

type PageQuery struct {
	Page           int
	PageSize       int
	Limit          int
	Offset         int
	Cursor         string
	CursorMode     bool
	After          *ScanCursor
	DirectoryAfter *DirectoryCursor
	TotalHint      *int
}

type RootQuery struct {
	Limit      int
	Offset     int
	After      *RootCursor
	CursorMode bool
	TotalHint  *int
}

type RootCursor struct {
	Name  string
	ID    string
	Total *int
}

type SourceFileCursor struct {
	Path  string
	ID    string
	Total *int
}

type ScanCursor struct {
	CreatedAt string
	ID        string
	Total     *int
}

type DirectoryCursor struct {
	Name  string
	Total *int
}

type ClaimedScan struct {
	Run  ScanRun
	Root Root
}

type ScanProgress struct {
	DiscoveredFiles int
	ProcessedFiles  int
	FailedFiles     int
}

type ScanResult struct {
	DiscoveredFiles int
	ProcessedFiles  int
	FailedFiles     int
	DeletedFiles    int
	// ArchivedFiles is retained only for legacy test synchronizers and older
	// integrations. Production scans populate DeletedFiles instead.
	ArchivedFiles int
}
