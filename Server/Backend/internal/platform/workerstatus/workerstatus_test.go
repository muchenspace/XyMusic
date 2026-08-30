package workerstatus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"xymusic/server/internal/config"
)

func TestConfigurationFingerprintMatchesDocument(t *testing.T) {
	interval := 30
	cfg := config.Config{
		Environment: config.Production,
		Paths: config.Paths{
			MigrationsDirectory:     "migrations",
			AdminWebDirectory:       "admin",
			MediaToolsDirectory:     "tools",
			LocalMusicDirectory:     "music",
			MediaAssetDirectory:     "assets",
			MediaTranscodeDirectory: "transcode",
		},
		Database: config.Database{URL: "postgres://user:pass@db/xymusic", MaxConnections: 12},
		MediaStorage: config.MediaStorage{
			AssetDirectory:           "assets",
			TranscodeDirectory:       "transcode",
			UploadTTLSeconds:         1800,
			StreamTTLSeconds:         1800,
			StreamMaxConcurrent:      8,
			StreamIdleTimeoutSeconds: 300,
			TranscodeTimeoutSeconds:  120,
			MaxUploadBytes:           1024,
		},
		Media: config.Media{Mode: "DIRECTORY", FFmpegPath: "tools/ffmpeg", FFprobePath: "tools/ffprobe", FFmpegThreads: 4},
		LocalLibrary: config.LocalLibrary{
			Name: "Local", Directory: "music", Mode: "READ_ONLY", Enabled: true,
			SyncOnStartup: false, ScanIntervalMinutes: &interval,
			IncludePatterns: []string{"**/*.flac"}, ExcludePatterns: []string{},
		},
		Security: config.Security{
			IdempotencyEncryptionSecret: "idempotency-secret",
			PlaybackTicketSecret:        "ticket-secret",
		},
	}
	doc := configurationDocument(cfg)
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	want := hex.EncodeToString(digest[:])
	if got := ConfigurationFingerprint(cfg); got != want {
		t.Fatalf("fingerprint = %s, want %s", got, want)
	}
}

func TestEvaluateExternalWorkerStatus(t *testing.T) {
	now := time.Date(2026, 7, 16, 4, 0, 0, 0, time.UTC)
	document := Document{
		PID: 42, State: "RUNNING", Fingerprint: "current",
		UpdatedAt: now.Add(-15 * time.Second).Format(time.RFC3339Nano),
	}
	status := Evaluate(document, "current", now, 45*time.Second, func(pid int) bool { return pid == 42 })
	if !status.Available || !status.Responsive || !status.Synchronized || status.UpdatedAt == nil {
		t.Fatalf("available status = %#v", status)
	}

	stale := Evaluate(document, "current", now.Add(time.Minute), 45*time.Second, func(int) bool { return true })
	if stale.Available || stale.Responsive {
		t.Fatalf("stale status = %#v", stale)
	}
}
