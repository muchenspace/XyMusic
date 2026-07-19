package workerstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xymusic/server/internal/config"
)

func TestConfigurationFingerprintMatchesLegacyDocument(t *testing.T) {
	interval := 30
	cfg := config.Config{
		Environment: config.Production,
		Paths: config.Paths{
			MigrationsDirectory: "migrations", MediaToolsDirectory: "tools", LocalMusicDirectory: "music",
		},
		Database: config.Database{URL: "postgres://user:pass@db/xymusic", MaxConnections: 12},
		Storage: config.Storage{
			Endpoint: "http://minio:9000", Region: "us-east-1", Bucket: "music",
			AccessKeyID: "access", SecretAccessKey: "secret", ForcePathStyle: true,
			SignedURLTTLSeconds: 300, MaxUploadBytes: 1024,
		},
		Media: config.Media{Mode: "DIRECTORY", FFmpegPath: "tools/ffmpeg", FFprobePath: "tools/ffprobe"},
		LocalLibrary: config.LocalLibrary{
			Name: "Local", Directory: "music", Mode: "READ_ONLY", Enabled: true,
			SyncOnStartup: false, ScanIntervalMinutes: &interval,
			IncludePatterns: []string{"**/*.flac"}, ExcludePatterns: []string{},
		},
		Security: config.Security{IdempotencyEncryptionSecret: "idempotency-secret"},
	}
	legacyJSON := `{"environment":"production","paths":{"migrationsDirectory":"migrations","mediaToolsDirectory":"tools","localMusicDirectory":"music"},"database":{"url":"postgres://user:pass@db/xymusic","maxConnections":12},"storage":{"endpoint":"http://minio:9000","region":"us-east-1","bucket":"music","accessKeyId":"access","secretAccessKey":"secret","forcePathStyle":true,"signedUrlTtlSeconds":300,"maxUploadBytes":1024},"media":{"mode":"DIRECTORY","ffmpegPath":"tools/ffmpeg","ffprobePath":"tools/ffprobe"},"localLibrary":{"name":"Local","directory":"music","mode":"READ_ONLY","enabled":true,"syncOnStartup":false,"scanIntervalMinutes":30,"includePatterns":["**/*.flac"],"excludePatterns":[]},"idempotencyEncryptionSecret":"idempotency-secret"}`
	digest := sha256.Sum256([]byte(legacyJSON))
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
	mismatched := Evaluate(document, "other", now, 45*time.Second, func(int) bool { return true })
	if mismatched.Available || !mismatched.Responsive || mismatched.Synchronized {
		t.Fatalf("mismatched status = %#v", mismatched)
	}
	future := document
	future.UpdatedAt = now.Add(time.Second).Format(time.RFC3339Nano)
	if status := Evaluate(future, "current", now, 45*time.Second, func(int) bool { return true }); status.Responsive {
		t.Fatalf("future status = %#v", status)
	}
}

func TestMonitorCachesAndCoalescesReads(t *testing.T) {
	now := time.Date(2026, 7, 16, 4, 0, 0, 0, time.UTC)
	content := []byte(`{"pid":7,"state":"RUNNING","fingerprint":"same","updatedAt":"2026-07-16T03:59:50Z"}`)
	var mu sync.Mutex
	reads := 0
	monitor, err := New(Options{
		Path: "status.json", Now: func() time.Time { return now },
		ReadFile: func(string) ([]byte, error) {
			mu.Lock()
			reads++
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return content, nil
		},
		ProcessAlive: func(int) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if status := monitor.Status(context.Background(), "same"); !status.Available {
				t.Errorf("status = %#v", status)
			}
		}()
	}
	wait.Wait()
	if status := monitor.Status(context.Background(), "same"); !status.Available {
		t.Fatalf("cached status = %#v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	if reads != 1 {
		t.Fatalf("reads = %d, want 1", reads)
	}
}

func TestWriteDocumentAtomicallyReplacesStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.worker-status")
	first := Document{PID: 1, State: "STARTING", UpdatedAt: "2026-07-16T04:00:00Z"}
	second := Document{PID: 1, State: "RUNNING", Fingerprint: "ready", UpdatedAt: "2026-07-16T04:00:01Z"}
	if err := WriteDocument(context.Background(), path, first); err != nil {
		t.Fatal(err)
	}
	if err := WriteDocument(context.Background(), path, second); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "{\"pid\":1,\"state\":\"RUNNING\",\"fingerprint\":\"ready\",\"updatedAt\":\"2026-07-16T04:00:01Z\"}\n" {
		t.Fatalf("status content = %q", content)
	}
	files, err := filepath.Glob(path + ".*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("temporary status files remain: %v", files)
	}
}

func TestWriteDocumentHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "status.json")
	if err := WriteDocument(ctx, path, Document{}); err == nil {
		t.Fatal("expected cancellation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("status file exists after cancellation: %v", err)
	}
}
