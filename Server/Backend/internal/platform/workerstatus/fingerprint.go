package workerstatus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"xymusic/server/internal/config"
)

func ConfigurationFingerprint(cfg config.Config) string {
	document := configurationDocument(cfg)
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func configurationDocument(cfg config.Config) fingerprintDocument {
	return fingerprintDocument{
		Environment: cfg.Environment,
		Paths: fingerprintPaths{
			MigrationsDirectory:     cfg.Paths.MigrationsDirectory,
			AdminWebDirectory:       cfg.Paths.AdminWebDirectory,
			MediaToolsDirectory:     cfg.Paths.MediaToolsDirectory,
			LocalMusicDirectory:     cfg.Paths.LocalMusicDirectory,
			MediaAssetDirectory:     cfg.Paths.MediaAssetDirectory,
			MediaTranscodeDirectory: cfg.Paths.MediaTranscodeDirectory,
		},
		Database: fingerprintDatabase{
			URL: cfg.Database.URL, MaxConnections: cfg.Database.MaxConnections,
		},
		MediaStorage: fingerprintMediaStorage{
			AssetDirectory:           cfg.MediaStorage.AssetDirectory,
			TranscodeDirectory:       cfg.MediaStorage.TranscodeDirectory,
			UploadTTLSeconds:         cfg.MediaStorage.UploadTTLSeconds,
			StreamTTLSeconds:         cfg.MediaStorage.StreamTTLSeconds,
			StreamMaxConcurrent:      cfg.MediaStorage.StreamMaxConcurrent,
			StreamIdleTimeoutSeconds: cfg.MediaStorage.StreamIdleTimeoutSeconds,
			TranscodeTimeoutSeconds:  cfg.MediaStorage.TranscodeTimeoutSeconds,
			TranscodeCacheMaxBytes:   cfg.MediaStorage.TranscodeCacheMaxBytes,
			MaxUploadBytes:           cfg.MediaStorage.MaxUploadBytes,
		},
		Media: fingerprintMedia{
			Mode: cfg.Media.Mode, FFmpegPath: cfg.Media.FFmpegPath, FFprobePath: cfg.Media.FFprobePath,
			FFmpegThreads: cfg.Media.FFmpegThreads,
		},
		LocalLibrary: fingerprintLocalLibrary{
			Name: cfg.LocalLibrary.Name, Directory: cfg.LocalLibrary.Directory,
			Mode: cfg.LocalLibrary.Mode, Enabled: cfg.LocalLibrary.Enabled,
			SyncOnStartup:       cfg.LocalLibrary.SyncOnStartup,
			ScanIntervalMinutes: cfg.LocalLibrary.ScanIntervalMinutes,
			IncludePatterns:     normalizedStrings(cfg.LocalLibrary.IncludePatterns),
			ExcludePatterns:     normalizedStrings(cfg.LocalLibrary.ExcludePatterns),
		},
		IdempotencyEncryptionSecret: cfg.Security.IdempotencyEncryptionSecret,
		PlaybackTicketSecret:        cfg.Security.PlaybackTicketSecret,
	}
}

type fingerprintDocument struct {
	Environment                 config.Environment      `json:"environment"`
	Paths                       fingerprintPaths        `json:"paths"`
	Database                    fingerprintDatabase     `json:"database"`
	MediaStorage                fingerprintMediaStorage `json:"mediaStorage"`
	Media                       fingerprintMedia        `json:"media"`
	LocalLibrary                fingerprintLocalLibrary `json:"localLibrary"`
	IdempotencyEncryptionSecret string                  `json:"idempotencyEncryptionSecret"`
	PlaybackTicketSecret        string                  `json:"playbackTicketSecret"`
}

type fingerprintPaths struct {
	MigrationsDirectory     string `json:"migrationsDirectory"`
	AdminWebDirectory       string `json:"adminWebDirectory"`
	MediaToolsDirectory     string `json:"mediaToolsDirectory"`
	LocalMusicDirectory     string `json:"localMusicDirectory"`
	MediaAssetDirectory     string `json:"mediaAssetDirectory"`
	MediaTranscodeDirectory string `json:"mediaTranscodeDirectory"`
}

type fingerprintDatabase struct {
	URL            string `json:"url"`
	MaxConnections int32  `json:"maxConnections"`
}

type fingerprintMediaStorage struct {
	AssetDirectory           string `json:"assetDirectory"`
	TranscodeDirectory       string `json:"transcodeDirectory"`
	UploadTTLSeconds         int    `json:"uploadTTLSeconds"`
	StreamTTLSeconds         int    `json:"streamTTLSeconds"`
	StreamMaxConcurrent      int    `json:"streamMaxConcurrent"`
	StreamIdleTimeoutSeconds int    `json:"streamIdleTimeoutSeconds"`
	TranscodeTimeoutSeconds  int    `json:"transcodeTimeoutSeconds"`
	TranscodeCacheMaxBytes   int64  `json:"transcodeCacheMaxBytes"`
	MaxUploadBytes           int64  `json:"maxUploadBytes"`
}

type fingerprintMedia struct {
	Mode          string `json:"mode"`
	FFmpegPath    string `json:"ffmpegPath"`
	FFprobePath   string `json:"ffprobePath"`
	FFmpegThreads int    `json:"ffmpegThreads"`
}

type fingerprintLocalLibrary struct {
	Name                string   `json:"name"`
	Directory           string   `json:"directory"`
	Mode                string   `json:"mode"`
	Enabled             bool     `json:"enabled"`
	SyncOnStartup       bool     `json:"syncOnStartup"`
	ScanIntervalMinutes *int     `json:"scanIntervalMinutes"`
	IncludePatterns     []string `json:"includePatterns"`
	ExcludePatterns     []string `json:"excludePatterns"`
}

func normalizedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}
