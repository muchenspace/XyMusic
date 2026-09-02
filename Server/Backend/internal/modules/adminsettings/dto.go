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
	AssetDirectory           *string `json:"assetDirectory,omitempty"`
	TranscodeDirectory       *string `json:"transcodeDirectory,omitempty"`
	UploadTTLSeconds         *int    `json:"uploadTtlSeconds,omitempty"`
	StreamTTLSeconds         *int    `json:"streamTtlSeconds,omitempty"`
	StreamMaxConcurrent      *int    `json:"streamMaxConcurrent,omitempty"`
	StreamIdleTimeoutSeconds *int    `json:"streamIdleTimeoutSeconds,omitempty"`
	TranscodeTimeoutSeconds  *int    `json:"transcodeTimeoutSeconds,omitempty"`
	TranscodeCacheMaxBytes   *int64  `json:"transcodeCacheMaxBytes,omitempty"`
	MaxUploadBytes           *int64  `json:"maxUploadBytes,omitempty"`
}

type MediaToolsInput struct {
	Directory   *string `json:"directory,omitempty"`
	FFmpegPath  *string `json:"ffmpegPath,omitempty"`
	FFprobePath *string `json:"ffprobePath,omitempty"`
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
	OK                       bool   `json:"ok"`
	Message                  string `json:"message"`
	AssetDirectoryExists     bool   `json:"assetDirectoryExists"`
	TranscodeDirectoryExists bool   `json:"transcodeDirectoryExists"`
	LatencyMS                int64  `json:"latencyMs"`
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
	AssetDirectory           string   `json:"assetDirectory"`
	TranscodeDirectory       string   `json:"transcodeDirectory"`
	UploadTTLSeconds         int      `json:"uploadTtlSeconds"`
	StreamTTLSeconds         int      `json:"streamTtlSeconds"`
	StreamMaxConcurrent      int      `json:"streamMaxConcurrent"`
	StreamIdleTimeoutSeconds int      `json:"streamIdleTimeoutSeconds"`
	TranscodeTimeoutSeconds  int      `json:"transcodeTimeoutSeconds"`
	TranscodeCacheMaxBytes   int64    `json:"transcodeCacheMaxBytes"`
	MaxUploadBytes           int64    `json:"maxUploadBytes"`
	LockedFields             []string `json:"lockedFields"`
}

type MediaToolsDTO struct {
	Directory    *string  `json:"directory"`
	FFmpegPath   string   `json:"ffmpegPath"`
	FFprobePath  string   `json:"ffprobePath"`
	LockedFields []string `json:"lockedFields"`
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
