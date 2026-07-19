package admintagscraping

import (
	"encoding/json"
	"time"
)

type Source string

const (
	SourceSmart    Source = "smart"
	SourceNetease  Source = "netease"
	SourceMigu     Source = "migu"
	SourceQMusic   Source = "qmusic"
	SourceKugou    Source = "kugou"
	SourceKuwo     Source = "kuwo"
	SourceAcoustID Source = "acoustid"
)

var searchableSources = []Source{SourceNetease, SourceMigu, SourceQMusic, SourceKugou, SourceKuwo}

type MatchMode string

const (
	MatchStrict MatchMode = "strict"
	MatchSimple MatchMode = "simple"
)

type MissingField string

const (
	MissingArtist MissingField = "artist"
	MissingAlbum  MissingField = "album"
	MissingYear   MissingField = "year"
	MissingGenre  MissingField = "genre"
	MissingLyrics MissingField = "lyrics"
	MissingCover  MissingField = "cover"
)

type Candidate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Artist      string   `json:"artist"`
	ArtistID    string   `json:"artistId"`
	Album       string   `json:"album"`
	AlbumID     string   `json:"albumId"`
	AlbumImg    string   `json:"albumImg"`
	Year        string   `json:"year"`
	Track       string   `json:"track"`
	Disc        string   `json:"disc"`
	Genre       string   `json:"genre"`
	Source      Source   `json:"source"`
	TitleScore  *float64 `json:"titleScore,omitempty"`
	ArtistScore *float64 `json:"artistScore,omitempty"`
	AlbumScore  *float64 `json:"albumScore,omitempty"`
	Score       *float64 `json:"score,omitempty"`
}

type ApplyFields struct {
	Title     bool `json:"title"`
	Artist    bool `json:"artist"`
	Album     bool `json:"album"`
	Year      bool `json:"year"`
	Genre     bool `json:"genre"`
	Lyrics    bool `json:"lyrics"`
	Cover     bool `json:"cover"`
	Overwrite bool `json:"overwrite"`
}

type BatchOptions struct {
	Sources       []Source       `json:"sources"`
	MatchMode     MatchMode      `json:"matchMode"`
	MissingFields []MissingField `json:"missingFields"`
	Fields        ApplyFields    `json:"fields"`
	WriteBack     bool           `json:"writeBack"`
	Reason        string         `json:"reason"`
}

type MetadataCredit struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type MetadataLyrics struct {
	Content  string `json:"content"`
	Format   string `json:"format"`
	Language string `json:"language"`
}

type MetadataSnapshot struct {
	Title        string           `json:"title"`
	Credits      []MetadataCredit `json:"credits"`
	AlbumArtists []string         `json:"albumArtists"`
	Album        *string          `json:"album"`
	ReleaseDate  *string          `json:"releaseDate"`
	TrackNumber  *int             `json:"trackNumber"`
	TrackTotal   *int             `json:"trackTotal"`
	DiscNumber   *int             `json:"discNumber"`
	DiscTotal    *int             `json:"discTotal"`
	Genres       []string         `json:"genres"`
	BPM          *float64         `json:"bpm"`
	ISRC         *string          `json:"isrc"`
	Comment      *string          `json:"comment"`
	Copyright    *string          `json:"copyright"`
	Lyrics       *MetadataLyrics  `json:"lyrics"`
	HasArtwork   bool             `json:"hasArtwork"`
}

type MetadataSource struct {
	ID                   string  `json:"id"`
	RootID               *string `json:"rootId"`
	RelativePath         string  `json:"relativePath"`
	Status               string  `json:"status"`
	ChecksumSHA256       string  `json:"checksumSha256"`
	Mode                 *string `json:"mode"`
	CanWriteBack         bool    `json:"canWriteBack"`
	WritebackBlockReason *string `json:"writebackBlockReason"`
}

type TrackMetadata struct {
	TrackID          string           `json:"trackId"`
	TrackStatus      string           `json:"trackStatus"`
	Raw              MetadataSnapshot `json:"raw"`
	Overrides        map[string]any   `json:"overrides"`
	Effective        MetadataSnapshot `json:"effective"`
	OverriddenFields []string         `json:"overriddenFields"`
	Source           *MetadataSource  `json:"source"`
	Version          int              `json:"version"`
	LastScannedAt    *string          `json:"lastScannedAt"`
	UpdatedBy        *string          `json:"updatedBy"`
	CreatedAt        string           `json:"createdAt"`
	UpdatedAt        string           `json:"updatedAt"`
}

type WritebackJob struct {
	ID                   string  `json:"id"`
	TrackID              string  `json:"trackId"`
	SourceID             string  `json:"sourceId"`
	RevisionID           *string `json:"revisionId"`
	Status               string  `json:"status"`
	Stage                string  `json:"stage"`
	Attempts             int     `json:"attempts"`
	MaxAttempts          int     `json:"maxAttempts"`
	CancelRequested      bool    `json:"cancelRequested"`
	MetadataVersion      int     `json:"metadataVersion"`
	Reason               string  `json:"reason"`
	OutputChecksumSHA256 *string `json:"outputChecksumSha256"`
	LastErrorCode        *string `json:"lastErrorCode"`
	LastError            *string `json:"lastError"`
	Version              int     `json:"version"`
	NextAttemptAt        string  `json:"nextAttemptAt"`
	StartedAt            *string `json:"startedAt"`
	CompletedAt          *string `json:"completedAt"`
	CreatedAt            string  `json:"createdAt"`
	UpdatedAt            string  `json:"updatedAt"`
}

type FingerprintSource struct {
	SourcePath string
	RootPath   string
	StartMS    int
	EndMS      *int
}

type FingerprintResult struct {
	DurationSeconds float64
	Fingerprint     string
}

type DownloadedArtwork struct {
	Bytes       []byte
	ContentType string
	Extension   string
}

type JobStatus string

const (
	JobPending   JobStatus = "PENDING"
	JobRunning   JobStatus = "RUNNING"
	JobCompleted JobStatus = "COMPLETED"
	JobCancelled JobStatus = "CANCELLED"
	JobFailed    JobStatus = "FAILED"
)

type ItemStatus string

const (
	ItemPending   ItemStatus = "PENDING"
	ItemRunning   ItemStatus = "RUNNING"
	ItemSucceeded ItemStatus = "SUCCEEDED"
	ItemFailed    ItemStatus = "FAILED"
	ItemSkipped   ItemStatus = "SKIPPED"
)

type BatchJobRecord struct {
	ID              string
	RequestedBy     *string
	Options         BatchOptions
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

type BatchItemRecord struct {
	ID              string
	JobID           string
	TrackID         string
	ExpectedVersion int
	Position        int
	Status          ItemStatus
	AttemptID       *string
	LockedBy        *string
	LockedUntil     *time.Time
	Candidate       *Candidate
	Source          *Source
	Message         *string
	StartedAt       *time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ClaimedBatchItem struct {
	Job       BatchJobRecord
	Item      BatchItemRecord
	AttemptID string
}

type ClaimResult struct {
	Item        *ClaimedBatchItem
	FinishJobID string
}

type BatchLeaseControl struct {
	Owned           bool
	CancelRequested bool
}

type MetadataPatch map[string]any

type IdempotencyInput struct {
	ActorID string
	Scope   string
	Key     string
	Payload any
}

type IdempotencyResponse struct {
	Status int
	Body   json.RawMessage
}

type IdempotencyResult struct {
	Status   int
	Body     json.RawMessage
	Replayed bool
}
