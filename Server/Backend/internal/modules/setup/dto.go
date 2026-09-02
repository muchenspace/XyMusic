package setup

const (
	DatabaseStateEmpty    = "EMPTY"
	DatabaseStatePartial  = "PARTIAL"
	DatabaseStateComplete = "COMPLETE"
)

// SetupInput is the complete first-run installation document accepted by
// POST /api/setup/complete. JSON member names intentionally match the legacy
// Bun service so the existing AdminWeb setup wizard can be reused unchanged.
type SetupInput struct {
	HTTP           HTTPInput          `json:"http"`
	Paths          PathsInput         `json:"paths"`
	Database       DatabaseInput      `json:"database"`
	Storage        StorageInput       `json:"storage"`
	Media          MediaInput         `json:"media"`
	Source         SourceInput        `json:"source"`
	Registration   RegistrationInput  `json:"registration"`
	Administrator  AdministratorInput `json:"administrator"`
	DatabaseAction string             `json:"databaseAction,omitempty"`
	StorageAction  string             `json:"storageAction,omitempty"`
}

type HTTPInput struct {
	IPv4Host              string   `json:"ipv4Host"`
	IPv4Port              int      `json:"ipv4Port"`
	IPv6Host              string   `json:"ipv6Host"`
	IPv6Port              int      `json:"ipv6Port"`
	Host                  string   `json:"host,omitempty"`
	Port                  int      `json:"port,omitempty"`
	TrustedProxyAddresses []string `json:"trustedProxyAddresses"`
}

type PathsInput struct {
	MigrationsDirectory string `json:"migrationsDirectory"`
	AdminWebDirectory   string `json:"adminWebDirectory"`
}

type DatabaseInput struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Database       string `json:"database"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	SSLMode        string `json:"sslMode"`
	MaxConnections int    `json:"maxConnections"`
}

type DatabaseTestInput struct {
	Database            DatabaseInput `json:"database"`
	MigrationsDirectory string        `json:"migrationsDirectory"`
}

type StorageInput struct {
	AssetDirectory           string `json:"assetDirectory,omitempty"`
	TranscodeDirectory       string `json:"transcodeDirectory,omitempty"`
	UploadTTLSeconds         *int   `json:"uploadTtlSeconds,omitempty"`
	StreamTTLSeconds         *int   `json:"streamTtlSeconds,omitempty"`
	StreamMaxConcurrent      *int   `json:"streamMaxConcurrent,omitempty"`
	StreamIdleTimeoutSeconds *int   `json:"streamIdleTimeoutSeconds,omitempty"`
	TranscodeTimeoutSeconds  *int   `json:"transcodeTimeoutSeconds,omitempty"`
	TranscodeCacheMaxBytes   *int64 `json:"transcodeCacheMaxBytes,omitempty"`
	MaxUploadBytes           *int64 `json:"maxUploadBytes,omitempty"`
}

type MediaInput struct {
	Directory   *string `json:"directory,omitempty"`
	FFmpegPath  *string `json:"ffmpegPath,omitempty"`
	FFprobePath *string `json:"ffprobePath,omitempty"`
}

type SourceInput struct {
	Name                string   `json:"name"`
	Directory           string   `json:"directory"`
	Mode                string   `json:"mode"`
	Enabled             *bool    `json:"enabled"`
	SyncOnStartup       *bool    `json:"syncOnStartup"`
	ScanIntervalMinutes *int     `json:"scanIntervalMinutes,omitempty"`
	IncludePatterns     []string `json:"includePatterns"`
	ExcludePatterns     []string `json:"excludePatterns"`
}

type RegistrationInput struct {
	Enabled *bool `json:"enabled"`
}

type AdministratorInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

type StatusResponse struct {
	SetupRequired       bool                  `json:"setupRequired"`
	Configured          bool                  `json:"configured"`
	ConfigurationSource string                `json:"configurationSource"`
	Runtime             RuntimeStatusResponse `json:"runtime"`
	Platform            string                `json:"platform"`
}

type RuntimeStatusResponse struct {
	Phase      string  `json:"phase"`
	Source     string  `json:"source"`
	Generation int     `json:"generation"`
	StartedAt  *string `json:"startedAt"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type PathsTestResponse struct {
	OK            bool          `json:"ok"`
	ResolvedPaths ResolvedPaths `json:"resolvedPaths"`
}

type ResolvedPaths struct {
	MigrationsDirectory string `json:"migrationsDirectory"`
	AdminWebDirectory   string `json:"adminWebDirectory"`
}

type DatabaseTestResponse struct {
	OK                 bool                   `json:"ok"`
	ServerTimeMS       int64                  `json:"serverTimeMs"`
	DatabaseInspection InstallationInspection `json:"databaseInspection"`
}

type InstallationInspection struct {
	State                  string   `json:"state"`
	MigrationRequired      bool     `json:"migrationRequired"`
	HasData                bool     `json:"hasData"`
	HasAdministrator       bool     `json:"hasAdministrator"`
	HasActiveAdministrator bool     `json:"hasActiveAdministrator"`
	Reusable               []string `json:"reusable"`
	Missing                []string `json:"missing"`
}

type StorageTestResponse struct {
	OK                bool              `json:"ok"`
	StorageInspection StorageInspection `json:"storageInspection"`
}

type StorageInspection struct {
	AssetDirectoryExists     bool  `json:"assetDirectoryExists"`
	TranscodeDirectoryExists bool  `json:"transcodeDirectoryExists"`
	HasAssets                bool  `json:"hasAssets"`
	AssetCount               int64 `json:"assetCount"`
	CountLimited             bool  `json:"countLimited"`
}

type MediaTestResponse struct {
	OK      bool               `json:"ok"`
	FFmpeg  string             `json:"ffmpeg"`
	FFprobe string             `json:"ffprobe"`
	Paths   ResolvedMediaPaths `json:"paths"`
}

type ResolvedMediaPaths struct {
	FFmpegPath  string `json:"ffmpegPath"`
	FFprobePath string `json:"ffprobePath"`
}

type SourceTestResponse struct {
	OK        bool   `json:"ok"`
	Directory string `json:"directory"`
}

type CompletionResponse struct {
	Configured            bool           `json:"configured"`
	RuntimeGeneration     int            `json:"runtimeGeneration"`
	ActualListener        ActualListener `json:"actualListener"`
	RestartRequiredFields []string       `json:"restartRequiredFields"`
}

type ActualListener struct {
	IPv4 ListenerAddress `json:"ipv4"`
	IPv6 ListenerAddress `json:"ipv6"`
}

type ListenerAddress struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
