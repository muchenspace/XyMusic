package workerstatus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"xymusic/server/internal/config"
)

// ConfigurationFingerprint matches the legacy worker fingerprint document so
// a Go control process can evaluate a Bun worker during a rolling cutover.
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
			MigrationsDirectory: cfg.Paths.MigrationsDirectory,
			MediaToolsDirectory: cfg.Paths.MediaToolsDirectory,
			LocalMusicDirectory: cfg.Paths.LocalMusicDirectory,
		},
		Database: fingerprintDatabase{
			URL: cfg.Database.URL, MaxConnections: cfg.Database.MaxConnections,
		},
		Storage: fingerprintStorage{
			Endpoint: cfg.Storage.Endpoint, Region: cfg.Storage.Region,
			Bucket: cfg.Storage.Bucket, AccessKeyID: cfg.Storage.AccessKeyID,
			SecretAccessKey: cfg.Storage.SecretAccessKey, ForcePathStyle: cfg.Storage.ForcePathStyle,
			PublicBaseURL: cfg.Storage.PublicBaseURL, SignedURLTTLSeconds: cfg.Storage.SignedURLTTLSeconds,
			MaxUploadBytes: cfg.Storage.MaxUploadBytes,
		},
		Media: fingerprintMedia{
			Mode: cfg.Media.Mode, FFmpegPath: cfg.Media.FFmpegPath, FFprobePath: cfg.Media.FFprobePath,
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
	}
}

type fingerprintDocument struct {
	Environment                 config.Environment      `json:"environment"`
	Paths                       fingerprintPaths        `json:"paths"`
	Database                    fingerprintDatabase     `json:"database"`
	Storage                     fingerprintStorage      `json:"storage"`
	Media                       fingerprintMedia        `json:"media"`
	LocalLibrary                fingerprintLocalLibrary `json:"localLibrary"`
	IdempotencyEncryptionSecret string                  `json:"idempotencyEncryptionSecret"`
}

type fingerprintPaths struct {
	MigrationsDirectory string `json:"migrationsDirectory"`
	MediaToolsDirectory string `json:"mediaToolsDirectory"`
	LocalMusicDirectory string `json:"localMusicDirectory"`
}

type fingerprintDatabase struct {
	URL            string `json:"url"`
	MaxConnections int32  `json:"maxConnections"`
}

type fingerprintStorage struct {
	Endpoint            string `json:"endpoint,omitempty"`
	Region              string `json:"region"`
	Bucket              string `json:"bucket"`
	AccessKeyID         string `json:"accessKeyId"`
	SecretAccessKey     string `json:"secretAccessKey"`
	ForcePathStyle      bool   `json:"forcePathStyle"`
	PublicBaseURL       string `json:"publicBaseUrl,omitempty"`
	SignedURLTTLSeconds int    `json:"signedUrlTtlSeconds"`
	MaxUploadBytes      int64  `json:"maxUploadBytes"`
}

type fingerprintMedia struct {
	Mode        string `json:"mode"`
	FFmpegPath  string `json:"ffmpegPath"`
	FFprobePath string `json:"ffprobePath"`
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
