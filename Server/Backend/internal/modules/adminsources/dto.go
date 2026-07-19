package adminsources

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

type JSONInteger int

func (value *JSONInteger) UnmarshalJSON(raw []byte) error {
	parsed, err := parseJSONInteger(raw)
	if err != nil {
		return err
	}
	*value = JSONInteger(parsed)
	return nil
}

type OptionalString struct {
	Set   bool
	Value string
}

func (value *OptionalString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be a string")
	}
	return json.Unmarshal(raw, &value.Value)
}

type OptionalBool struct {
	Set   bool
	Value bool
}

func (value *OptionalBool) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be a boolean")
	}
	return json.Unmarshal(raw, &value.Value)
}

type OptionalMode struct {
	Set   bool
	Value RootMode
}

func (value *OptionalMode) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be a string")
	}
	return json.Unmarshal(raw, &value.Value)
}

type OptionalPatterns struct {
	Set   bool
	Value []string
}

func (value *OptionalPatterns) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("value must be an array")
	}
	return json.Unmarshal(raw, &value.Value)
}

type OptionalNullableInt struct {
	Set   bool
	Value *int
}

func (value *OptionalNullableInt) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.Value = nil
		return nil
	}
	parsed, err := parseJSONInteger(raw)
	if err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type CreateRootInput struct {
	Name                string       `json:"name"`
	Path                string       `json:"path"`
	Mode                RootMode     `json:"mode"`
	Enabled             *bool        `json:"enabled"`
	ScanOnStartup       *bool        `json:"scanOnStartup"`
	ScanIntervalMinutes *JSONInteger `json:"scanIntervalMinutes,omitempty"`
	IncludePatterns     []string     `json:"includePatterns"`
	ExcludePatterns     []string     `json:"excludePatterns"`
}

type UpdateRootInput struct {
	ExpectedVersion     JSONInteger         `json:"expectedVersion"`
	Name                OptionalString      `json:"name"`
	Path                OptionalString      `json:"path"`
	Mode                OptionalMode        `json:"mode"`
	Enabled             OptionalBool        `json:"enabled"`
	ScanOnStartup       OptionalBool        `json:"scanOnStartup"`
	ScanIntervalMinutes OptionalNullableInt `json:"scanIntervalMinutes"`
	IncludePatterns     OptionalPatterns    `json:"includePatterns"`
	ExcludePatterns     OptionalPatterns    `json:"excludePatterns"`
}

type DeleteRootInput struct {
	ExpectedVersion JSONInteger `json:"expectedVersion"`
	ArchiveCatalog  *bool       `json:"archiveCatalog"`
}

func parseJSONInteger(raw []byte) (int, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" || strings.HasPrefix(text, `"`) {
		return 0, errors.New("value must be an integer")
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Trunc(parsed) != parsed {
		return 0, errors.New("value must be an integer")
	}
	maximum := float64(^uint(0) >> 1)
	if parsed < -maximum-1 || parsed > maximum {
		return 0, errors.New("integer is outside the supported range")
	}
	return int(parsed), nil
}

type DirectoryDTO struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type BrowseDTO struct {
	Path        string         `json:"path"`
	Directories []DirectoryDTO `json:"directories"`
	Page        int            `json:"page"`
	PageSize    int            `json:"pageSize"`
	Total       int            `json:"total"`
	TotalPages  int            `json:"totalPages"`
}

type ScanRunDTO struct {
	ID              string     `json:"id"`
	RootID          string     `json:"rootId"`
	Status          ScanStatus `json:"status"`
	DiscoveredFiles int        `json:"discoveredFiles"`
	ProcessedFiles  int        `json:"processedFiles"`
	FailedFiles     int        `json:"failedFiles"`
	CancelRequested bool       `json:"cancelRequested"`
	StartedAt       *string    `json:"startedAt"`
	CompletedAt     *string    `json:"completedAt"`
	LastError       *string    `json:"lastError"`
	CreatedAt       string     `json:"createdAt"`
	UpdatedAt       string     `json:"updatedAt"`
}

type RootDTO struct {
	ID                  string      `json:"id"`
	Name                string      `json:"name"`
	Path                string      `json:"path"`
	Mode                RootMode    `json:"mode"`
	Enabled             bool        `json:"enabled"`
	ScanOnStartup       bool        `json:"scanOnStartup"`
	ScanIntervalMinutes *int        `json:"scanIntervalMinutes"`
	IncludePatterns     []string    `json:"includePatterns"`
	ExcludePatterns     []string    `json:"excludePatterns"`
	Status              RootStatus  `json:"status"`
	LastScanAt          *string     `json:"lastScanAt"`
	LastError           *string     `json:"lastError"`
	FileCount           int         `json:"fileCount"`
	FailedFileCount     int         `json:"failedFileCount"`
	TrackCount          int         `json:"trackCount"`
	CueFileCount        int         `json:"cueFileCount"`
	LatestRun           *ScanRunDTO `json:"latestRun"`
	Version             int         `json:"version"`
	CreatedAt           string      `json:"createdAt"`
	UpdatedAt           string      `json:"updatedAt"`
}

type RootListDTO struct {
	Items      []RootDTO `json:"items"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	Total      int       `json:"total"`
	TotalPages int       `json:"totalPages"`
}

type TrackSummaryDTO struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type SourceFileDTO struct {
	ID         string           `json:"id"`
	Path       string           `json:"path"`
	Status     SourceFileStatus `json:"status"`
	LastError  *string          `json:"lastError"`
	SizeBytes  int64            `json:"sizeBytes"`
	ModifiedAt string           `json:"modifiedAt"`
	Track      TrackSummaryDTO  `json:"track"`
	TrackCount int              `json:"trackCount"`
	Cue        bool             `json:"cue"`
}

type SourceFilePageDTO struct {
	Items      []SourceFileDTO `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"pageSize"`
	Total      int             `json:"total"`
	TotalPages int             `json:"totalPages"`
}

type ProcessingJobDTO struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	Title         string  `json:"title"`
	Attempts      int     `json:"attempts"`
	MaxAttempts   int     `json:"maxAttempts"`
	LastError     *string `json:"lastError"`
	LastErrorCode *string `json:"lastErrorCode"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

type ProcessingSummaryDTO struct {
	Queued     int                `json:"queued"`
	Processing int                `json:"processing"`
	Completed  int                `json:"completed"`
	Failed     int                `json:"failed"`
	Cancelled  int                `json:"cancelled"`
	Active     int                `json:"active"`
	Total      int                `json:"total"`
	UpdatedAt  *string            `json:"updatedAt"`
	Jobs       []ProcessingJobDTO `json:"jobs"`
}

type ScanRunPageDTO struct {
	Items      []ScanRunDTO `json:"items"`
	Page       int          `json:"page"`
	PageSize   int          `json:"pageSize"`
	Total      int          `json:"total"`
	TotalPages int          `json:"totalPages"`
}

type DeletedDTO struct {
	Deleted bool `json:"deleted"`
}

type CancelledDTO struct {
	Cancelled bool `json:"cancelled"`
}
