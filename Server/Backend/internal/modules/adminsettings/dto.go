package adminsettings

import (
	"bytes"
	"encoding/json"

	"xymusic/server/internal/platform/runtimemetrics"
	"xymusic/server/internal/platform/workerstatus"
)

type OptionalNullableString struct {
	Set   bool
	Value *string
}

func (value *OptionalNullableString) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
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
	var decoded int
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type DatabaseInput struct {
	Host               *string `json:"host,omitempty"`
	Port               *int    `json:"port,omitempty"`
	Database           *string `json:"database,omitempty"`
	Username           *string `json:"username,omitempty"`
	Password           *string `json:"password,omitempty"`
	SSLMode            *string `json:"sslMode,omitempty"`
	MaximumConnections *int    `json:"maximumConnections,omitempty"`
}

type StorageInput struct {
	Endpoint            OptionalNullableString `json:"endpoint,omitempty"`
	PublicBaseURL       OptionalNullableString `json:"publicBaseUrl,omitempty"`
	Region              *string                `json:"region,omitempty"`
	Bucket              *string                `json:"bucket,omitempty"`
	AccessKeyID         *string                `json:"accessKeyId,omitempty"`
	SecretAccessKey     *string                `json:"secretAccessKey,omitempty"`
	ForcePathStyle      *bool                  `json:"forcePathStyle,omitempty"`
	SignedURLTTLSeconds *int                   `json:"signedUrlTtlSeconds,omitempty"`
	MaxUploadBytes      *int64                 `json:"maxUploadBytes,omitempty"`
}

type MediaToolsInput struct {
	Directory   *string `json:"directory,omitempty"`
	FFmpegPath  *string `json:"ffmpegPath,omitempty"`
	FFprobePath *string `json:"ffprobePath,omitempty"`
}

type ScrapingInput struct {
	FPcalcPath     *string `json:"fpcalcPath,omitempty"`
	AcoustIDClient *string `json:"acoustIdClient,omitempty"`
}

type LocalLibraryInput struct {
	Name                *string             `json:"name,omitempty"`
	Directory           *string             `json:"directory,omitempty"`
	Mode                *string             `json:"mode,omitempty"`
	Enabled             *bool               `json:"enabled,omitempty"`
	SyncOnStartup       *bool               `json:"syncOnStartup,omitempty"`
	ScanIntervalMinutes OptionalNullableInt `json:"scanIntervalMinutes,omitempty"`
	IncludePatterns     *[]string           `json:"includePatterns,omitempty"`
	ExcludePatterns     *[]string           `json:"excludePatterns,omitempty"`
}

type RegistrationInput struct {
	Enabled *bool `json:"enabled"`
}

type SecurityInput struct {
	AccessTokenTTLSeconds  *int `json:"accessTokenTtlSeconds,omitempty"`
	RefreshTokenTTLSeconds *int `json:"refreshTokenTtlSeconds,omitempty"`
}

type HTTPInput struct {
	IPv4Host              *string   `json:"ipv4Host,omitempty"`
	IPv4Port              *int      `json:"ipv4Port,omitempty"`
	IPv6Host              *string   `json:"ipv6Host,omitempty"`
	IPv6Port              *int      `json:"ipv6Port,omitempty"`
	Host                  *string   `json:"host,omitempty"`
	Port                  *int      `json:"port,omitempty"`
	TrustedProxyAddresses *[]string `json:"trustedProxyAddresses,omitempty"`
}

type UpdateInput struct {
	ExpectedVersion int                `json:"expectedVersion"`
	Database        *DatabaseInput     `json:"database,omitempty"`
	Storage         *StorageInput      `json:"storage,omitempty"`
	MediaTools      *MediaToolsInput   `json:"mediaTools,omitempty"`
	Scraping        *ScrapingInput     `json:"scraping,omitempty"`
	LocalLibrary    *LocalLibraryInput `json:"localLibrary,omitempty"`
	Registration    *RegistrationInput `json:"registration,omitempty"`
	Security        *SecurityInput     `json:"security,omitempty"`
	HTTP            *HTTPInput         `json:"http,omitempty"`
}

type TestResponse struct {
	OK        bool     `json:"ok"`
	Message   string   `json:"message"`
	LatencyMS *int64   `json:"latencyMs,omitempty"`
	Details   []string `json:"details,omitempty"`
	Paths     any      `json:"paths,omitempty"`
}

type StorageTestResponse struct {
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	BucketExists bool   `json:"bucketExists"`
	LatencyMS    int64  `json:"latencyMs"`
}

type LocalLibraryTestResponse struct {
	OK             bool   `json:"ok"`
	Message        string `json:"message"`
	NormalizedPath string `json:"normalizedPath"`
}

type SettingsDTO struct {
	Version               int             `json:"version"`
	Environment           string          `json:"environment"`
	ConfigurationSource   string          `json:"configurationSource"`
	ActualListener        ListenerDTO     `json:"actualListener"`
	RestartRequiredFields []string        `json:"restartRequiredFields"`
	Database              DatabaseDTO     `json:"database"`
	Storage               StorageDTO      `json:"storage"`
	MediaTools            MediaToolsDTO   `json:"mediaTools"`
	Scraping              ScrapingDTO     `json:"scraping"`
	LocalLibrary          LocalLibraryDTO `json:"localLibrary"`
	Registration          RegistrationDTO `json:"registration"`
	Security              SecurityDTO     `json:"security"`
	HTTP                  HTTPDTO         `json:"http"`
	AppliedFields         []string        `json:"appliedFields,omitempty"`
}

type ListenerDTO struct {
	IPv4 ListenerAddressDTO `json:"ipv4"`
	IPv6 ListenerAddressDTO `json:"ipv6"`
}

type ListenerAddressDTO struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type DatabaseDTO struct {
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	Database           string   `json:"database"`
	Username           string   `json:"username"`
	SSLMode            string   `json:"sslMode"`
	MaximumConnections int32    `json:"maximumConnections"`
	PasswordConfigured bool     `json:"passwordConfigured"`
	LockedFields       []string `json:"lockedFields"`
}

type StorageDTO struct {
	Endpoint                  *string  `json:"endpoint"`
	PublicBaseURL             *string  `json:"publicBaseUrl"`
	Region                    string   `json:"region"`
	Bucket                    string   `json:"bucket"`
	AccessKeyID               string   `json:"accessKeyId"`
	SecretAccessKeyConfigured bool     `json:"secretAccessKeyConfigured"`
	ForcePathStyle            bool     `json:"forcePathStyle"`
	SignedURLTTLSeconds       int      `json:"signedUrlTtlSeconds"`
	MaxUploadBytes            int64    `json:"maxUploadBytes"`
	LockedFields              []string `json:"lockedFields"`
}

type MediaToolsDTO struct {
	Directory    *string  `json:"directory"`
	FFmpegPath   string   `json:"ffmpegPath"`
	FFprobePath  string   `json:"ffprobePath"`
	LockedFields []string `json:"lockedFields"`
}

type ScrapingDTO struct {
	FPcalcPath     string   `json:"fpcalcPath"`
	AcoustIDClient string   `json:"acoustIdClient"`
	LockedFields   []string `json:"lockedFields"`
}

type LocalLibraryDTO struct {
	Name                string   `json:"name"`
	Directory           string   `json:"directory"`
	Mode                string   `json:"mode"`
	Enabled             bool     `json:"enabled"`
	SyncOnStartup       bool     `json:"syncOnStartup"`
	ScanIntervalMinutes *int     `json:"scanIntervalMinutes"`
	IncludePatterns     []string `json:"includePatterns"`
	ExcludePatterns     []string `json:"excludePatterns"`
	LockedFields        []string `json:"lockedFields"`
}

type RegistrationDTO struct {
	Enabled      bool     `json:"enabled"`
	LockedFields []string `json:"lockedFields"`
}

type SecurityDTO struct {
	AccessTokenTTLSeconds  int      `json:"accessTokenTtlSeconds"`
	RefreshTokenTTLSeconds int      `json:"refreshTokenTtlSeconds"`
	LockedFields           []string `json:"lockedFields"`
}

type HTTPDTO struct {
	IPv4Host              string   `json:"ipv4Host"`
	IPv4Port              int      `json:"ipv4Port"`
	IPv6Host              string   `json:"ipv6Host"`
	IPv6Port              int      `json:"ipv6Port"`
	TrustedProxyAddresses []string `json:"trustedProxyAddresses"`
	LockedFields          []string `json:"lockedFields"`
}

type IdempotentSettingsResult struct {
	Status   int
	Body     SettingsDTO
	Replayed bool
}

type QueueDTO struct {
	Media     int `json:"media"`
	Scans     int `json:"scans"`
	Cleanup   int `json:"cleanup"`
	Writeback int `json:"writeback"`
	Scraping  int `json:"scraping"`
	Total     int `json:"total"`
}

type SystemInformationDTO struct {
	ApplicationVersion  string                  `json:"applicationVersion"`
	RuntimeVersion      string                  `json:"runtimeVersion"`
	Platform            string                  `json:"platform"`
	Architecture        string                  `json:"architecture"`
	UptimeSeconds       int64                   `json:"uptimeSeconds"`
	DatabaseVersion     string                  `json:"databaseVersion"`
	MigrationVersion    string                  `json:"migrationVersion"`
	FFmpegVersion       *string                 `json:"ffmpegVersion"`
	DataDirectory       string                  `json:"dataDirectory"`
	ConfigurationFile   string                  `json:"configurationFile"`
	ConfigurationSource string                  `json:"configurationSource"`
	Worker              workerstatus.Snapshot   `json:"worker"`
	Metrics             runtimemetrics.Snapshot `json:"metrics"`
	Queues              QueueDTO                `json:"queues"`
}
