package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionRequiresSecurityMaterial(t *testing.T) {
	_, err := Parse(map[string]string{
		"NODE_ENV":     "production",
		"DATABASE_URL": "postgres://user:pass@localhost/xymusic",
	})
	if err == nil || !strings.Contains(err.Error(), "ACCESS_TOKEN_SECRET") {
		t.Fatalf("expected production secret error, got %v", err)
	}
}

func TestDevelopmentDefaultsAreCompatible(t *testing.T) {
	cfg, err := Parse(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.IPv4Host != "0.0.0.0" || cfg.HTTP.IPv4Port != 3000 || cfg.HTTP.IPv6Host != "::" || cfg.HTTP.IPv6Port != 3000 {
		t.Fatalf("unexpected listener defaults: %+v", cfg.HTTP)
	}
	wantDatabaseConnections := defaultDatabaseConnections(
		cfg.LocalLibrary.ScanCommitWorkers,
		cfg.Scraping.BatchWorkers, cfg.Scraping.ArtworkWorkers, cfg.Scraping.WritebackWorkers,
	)
	if cfg.HTTP.Port != 3000 || int(cfg.Database.MaxConnections) != wantDatabaseConnections {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.Security.AccessTokenSecret) < 32 {
		t.Fatal("development secret is too short")
	}
	if len(cfg.Security.PlaybackTicketSecret) < 32 {
		t.Fatal("playback ticket secret is too short")
	}
	if cfg.MediaStorage.MaxUploadBytes != MaxServerRequestBodyBytes {
		t.Fatal("upload limit mismatch")
	}
	if cfg.Media.Mode != "ADVANCED" || cfg.Media.FFmpegPath != "ffmpeg" || cfg.Media.FFprobePath != "ffprobe" {
		t.Fatalf("expected PATH-based media defaults: %#v", cfg.Media)
	}
	if cfg.Media.FFmpegThreads != 0 {
		t.Fatalf("unexpected automatic FFmpeg thread setting: %#v", cfg.Media)
	}
	if cfg.LocalLibrary.ScanCommitWorkers < 1 || cfg.LocalLibrary.ScanCommitBatchSize < 1 || cfg.LocalLibrary.ScanProbeWorkers < 1 {
		t.Fatalf("unexpected scan stage defaults: %#v", cfg.LocalLibrary)
	}
	if cfg.Paths.MediaAssetDirectory != DefaultMediaAssetDirectory || cfg.Paths.MediaTranscodeDirectory != DefaultMediaTranscodeDirectory {
		t.Fatalf("unexpected media directory defaults: %#v", cfg.Paths)
	}
}

func TestPerformanceWorkerLimitsAreConfigurable(t *testing.T) {
	cfg, err := Parse(map[string]string{
		"LOCAL_MUSIC_SCAN_COMMIT_WORKERS":    "3",
		"LOCAL_MUSIC_SCAN_COMMIT_BATCH_SIZE": "11",
		"LOCAL_MUSIC_SCAN_PROBE_WORKERS":     "5",
		"MEDIA_FFMPEG_THREADS":               "2",
		"TAG_SCRAPING_WORKERS":               "4",
		"TAG_SCRAPING_CLAIM_WINDOW":          "17",
		"TAG_SCRAPING_ARTWORK_WORKERS":       "3",
		"TAG_SCRAPING_ARTWORK_CLAIM_WINDOW":  "13",
		"TAG_WRITEBACK_WORKERS":              "4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalLibrary.ScanCommitWorkers != 3 || cfg.LocalLibrary.ScanCommitBatchSize != 11 || cfg.LocalLibrary.ScanProbeWorkers != 5 ||
		cfg.Media.FFmpegThreads != 2 ||
		cfg.Scraping.BatchWorkers != 4 || cfg.Scraping.BatchClaimWindow != 17 || cfg.Scraping.ArtworkWorkers != 3 || cfg.Scraping.ArtworkClaimWindow != 13 || cfg.Scraping.WritebackWorkers != 4 {
		t.Fatalf("performance limits = %#v/%#v", cfg.LocalLibrary, cfg.Media)
	}
	environment := ToEnvironment(cfg)
	if environment["LOCAL_MUSIC_SCAN_COMMIT_WORKERS"] != "3" || environment["LOCAL_MUSIC_SCAN_COMMIT_BATCH_SIZE"] != "11" ||
		environment["LOCAL_MUSIC_SCAN_PROBE_WORKERS"] != "5" || environment["MEDIA_FFMPEG_THREADS"] != "2" ||
		environment["TAG_SCRAPING_WORKERS"] != "4" || environment["TAG_SCRAPING_CLAIM_WINDOW"] != "17" ||
		environment["TAG_SCRAPING_ARTWORK_WORKERS"] != "3" || environment["TAG_SCRAPING_ARTWORK_CLAIM_WINDOW"] != "13" || environment["TAG_WRITEBACK_WORKERS"] != "4" {
		t.Fatalf("performance limits were not persisted: %#v", environment)
	}
}

func TestDatabaseConnectionDefaultFollowsConfiguredWorkerBudget(t *testing.T) {
	cfg, err := Parse(map[string]string{
		"LOCAL_MUSIC_SCAN_COMMIT_WORKERS": "12",
		"TAG_SCRAPING_WORKERS":            "20",
		"TAG_SCRAPING_ARTWORK_WORKERS":    "4",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := defaultDatabaseConnections(12, 20, 4, 2)
	if int(cfg.Database.MaxConnections) != want {
		t.Fatalf("default database connections=%d want %d", cfg.Database.MaxConnections, want)
	}

	cfg, err = Parse(map[string]string{
		"DATABASE_MAX_CONNECTIONS":        "9",
		"LOCAL_MUSIC_SCAN_COMMIT_WORKERS": "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.MaxConnections != 9 {
		t.Fatalf("explicit database connection limit=%d want 9", cfg.Database.MaxConnections)
	}
}

func TestHTTPListenersSupportSeparateAddressFamiliesAndLegacyFallback(t *testing.T) {
	cfg, err := Parse(map[string]string{
		"HTTP_IPV4_HOST": "192.0.2.10",
		"HTTP_IPV4_PORT": "3100",
		"HTTP_IPV6_HOST": "2001:db8::10",
		"HTTP_IPV6_PORT": "3200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.IPv4Host != "192.0.2.10" || cfg.HTTP.IPv4Port != 3100 || cfg.HTTP.IPv6Host != "2001:db8::10" || cfg.HTTP.IPv6Port != 3200 {
		t.Fatalf("separate listeners were not preserved: %+v", cfg.HTTP)
	}

	legacy, err := Parse(map[string]string{"HTTP_HOST": "127.0.0.1", "HTTP_PORT": "3300"})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.HTTP.IPv4Host != "127.0.0.1" || legacy.HTTP.IPv4Port != 3300 || legacy.HTTP.IPv6Host != "::" || legacy.HTTP.IPv6Port != 3300 {
		t.Fatalf("legacy listener fallback is invalid: %+v", legacy.HTTP)
	}
}

func TestHTTPListenersRejectMismatchedAddressFamilies(t *testing.T) {
	for name, environment := range map[string]map[string]string{
		"IPv6 in IPv4 field": {"HTTP_IPV4_HOST": "::"},
		"IPv4 in IPv6 field": {"HTTP_IPV6_HOST": "0.0.0.0"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(environment); err == nil {
				t.Fatal("expected listener address family validation error")
			}
		})
	}
}

func TestLegacyCORSOriginSettingIsIgnoredAndRemovedFromManagedOutput(t *testing.T) {
	cfg, err := Parse(map[string]string{"HTTP_CORS_ORIGINS": "https://legacy.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := ToEnvironment(cfg)["HTTP_CORS_ORIGINS"]; exists {
		t.Fatal("deprecated HTTP_CORS_ORIGINS was written back to managed configuration")
	}
}

func TestUploadLimitIsCapped(t *testing.T) {
	_, err := Parse(map[string]string{"MEDIA_MAX_UPLOAD_BYTES": "1073741825"})
	if err == nil || !strings.Contains(err.Error(), "MEDIA_MAX_UPLOAD_BYTES") {
		t.Fatalf("expected cap error, got %v", err)
	}
}

func TestRelativePathsAreResolvedAtRuntime(t *testing.T) {
	cfg, err := Parse(map[string]string{"LOCAL_MUSIC_DIRECTORY": "library"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.LocalMusicDirectory != "library" {
		t.Fatalf("parse changed relative path: %s", cfg.Paths.LocalMusicDirectory)
	}
	root := t.TempDir()
	resolved, err := ResolveRuntime(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Paths.LocalMusicDirectory != filepath.Join(root, "library") {
		t.Fatalf("unexpected resolved path: %s", resolved.Paths.LocalMusicDirectory)
	}
}

func TestMediaExecutablesSupportPathAbsoluteAndRelativeValues(t *testing.T) {
	root := t.TempDir()
	cfg, err := Parse(map[string]string{
		"MEDIA_TOOLS_MODE": "ADVANCED",
		"FFMPEG_PATH":      "",
		"FFPROBE_PATH":     filepath.Join("tools", executableName("ffprobe")),
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveRuntime(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Media.FFmpegPath != "ffmpeg" {
		t.Fatalf("blank FFmpeg path did not use PATH: %q", resolved.Media.FFmpegPath)
	}
	if resolved.Media.FFprobePath != filepath.Join(root, "tools", executableName("ffprobe")) {
		t.Fatalf("relative FFprobe path was not rooted: %q", resolved.Media.FFprobePath)
	}
}

func TestStoreDistinguishesMissingAndMalformedFiles(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), ".env"))
	if _, err := store.Load(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected first-run state, got %v", err)
	}
	if err := os.WriteFile(store.Path, []byte("this is invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil || errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected malformed configuration, got %v", err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	cfg, err := Parse(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(t.TempDir(), ".env"))
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Database.URL != cfg.Database.URL || loaded.Paths.MediaAssetDirectory != cfg.Paths.MediaAssetDirectory ||
		loaded.Paths.MediaTranscodeDirectory != cfg.Paths.MediaTranscodeDirectory {
		t.Fatalf("round trip mismatch: %#v", loaded)
	}
}

func TestStoreRecoverPromotesCompletedNextFile(t *testing.T) {
	cfg, err := Parse(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	store := NewStore(filepath.Join(directory, ".env"))
	nextStore := NewStore(store.Path + ".next")
	if err := nextStore.Save(cfg); err != nil {
		t.Fatal(err)
	}
	// Save writes atomically to the requested .next path itself.
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("recovered configuration could not be loaded: %v", err)
	}
}

func TestStoreClearRemovesRecoveryArtifacts(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), ".env"))
	for _, path := range []string{store.Path, store.Path + ".next", store.Path + ".backup"} {
		if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{store.Path, store.Path + ".next", store.Path + ".backup"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("artifact still exists: %s", path)
		}
	}
}
