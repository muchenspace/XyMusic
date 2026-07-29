package adminsettings

import (
	"reflect"
	"runtime"
	"testing"

	"xymusic/server/internal/config"
	"xymusic/server/internal/shared/apperror"
)

func TestMergeSettingsPreservesLegacyConfigurationContract(t *testing.T) {
	current := settingsTestConfig(t)
	host := "db.internal"
	port := 5544
	databaseName := "music_next"
	maximumConnections := 20
	storageEndpoint := "https://objects.example.test/"
	mediaDirectory := "new-tools"
	libraryName := "Archive"
	libraryDirectory := "archive"
	mode := "READ_WRITE"
	enabled := false
	interval := 60
	include := []string{"**/*.flac"}
	exclude := []string{"**/tmp/**"}
	registration := true
	accessTTL := 1200
	httpPort := 3100
	proxies := []string{"127.0.0.1", "::1"}
	candidate, err := mergeSettings(current, UpdateInput{
		ExpectedVersion: 1,
		Database: &DatabaseInput{
			Host: &host, Port: &port, Database: &databaseName, MaximumConnections: &maximumConnections,
		},
		Storage:    &StorageInput{Endpoint: OptionalNullableString{Set: true, Value: &storageEndpoint}},
		MediaTools: &MediaToolsInput{Directory: &mediaDirectory},
		LocalLibrary: &LocalLibraryInput{
			Name: &libraryName, Directory: &libraryDirectory, Mode: &mode, Enabled: &enabled,
			ScanIntervalMinutes: OptionalNullableInt{Set: true, Value: &interval},
			IncludePatterns:     &include, ExcludePatterns: &exclude,
		},
		Registration: &RegistrationInput{Enabled: &registration},
		Security:     &SecurityInput{AccessTokenTTLSeconds: &accessTTL},
		HTTP:         &HTTPInput{IPv4Port: &httpPort, TrustedProxyAddresses: &proxies},
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Database.MaxConnections != 20 || candidate.Storage.Endpoint != "https://objects.example.test" {
		t.Fatalf("database/storage = %#v/%#v", candidate.Database, candidate.Storage)
	}
	if candidate.Media.Mode != "DIRECTORY" || candidate.Paths.MediaToolsDirectory != "new-tools" {
		t.Fatalf("media = %#v paths=%#v", candidate.Media, candidate.Paths)
	}
	wantFFmpeg := "new-tools/ffmpeg"
	if runtime.GOOS == "windows" {
		wantFFmpeg = `new-tools\ffmpeg.exe`
	}
	if candidate.Media.FFmpegPath != wantFFmpeg {
		t.Fatalf("ffmpeg path = %q, want %q", candidate.Media.FFmpegPath, wantFFmpeg)
	}
	if candidate.LocalLibrary.Name != "Archive" || candidate.LocalLibrary.Mode != "READ_WRITE" || candidate.LocalLibrary.Enabled || candidate.LocalLibrary.ScanIntervalMinutes == nil || *candidate.LocalLibrary.ScanIntervalMinutes != 60 {
		t.Fatalf("local library = %#v", candidate.LocalLibrary)
	}
	if !candidate.Registration.Enabled || candidate.Security.AccessTokenTTLSeconds != 1200 || candidate.HTTP.Port != 3100 {
		t.Fatalf("registration/security/http = %#v/%#v/%#v", candidate.Registration, candidate.Security, candidate.HTTP)
	}
	if !reflect.DeepEqual(candidate.HTTP.TrustedProxyAddresses, proxies) {
		t.Fatalf("trusted proxies = %#v", candidate.HTTP.TrustedProxyAddresses)
	}
	fields := changedFields(current, candidate)
	for _, required := range []string{"database.url", "storage.endpoint", "media.ffmpegPath", "localLibrary.name", "registration.enabled", "http.ipv4Port"} {
		if !contains(fields, required) {
			t.Errorf("changed fields missing %q: %v", required, fields)
		}
	}
}

func TestMergeSettingsValidatesBoundsAndNullableFields(t *testing.T) {
	current := settingsTestConfig(t)
	invalidPort := 0
	_, err := mergeSettings(current, UpdateInput{HTTP: &HTTPInput{IPv4Port: &invalidPort}})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("invalid port error = %v", err)
	}
	_, err = mergeSettings(current, UpdateInput{Registration: &RegistrationInput{}})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("missing registration error = %v", err)
	}
	candidate, err := mergeStorage(current, StorageInput{
		Endpoint: OptionalNullableString{Set: true}, PublicBaseURL: OptionalNullableString{Set: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Storage.Endpoint != "" || candidate.Storage.PublicBaseURL != "" {
		t.Fatalf("nullable storage values = %#v", candidate.Storage)
	}
}

func TestMergeMediaToolsAllowsBlankPathsForSystemPath(t *testing.T) {
	current := settingsTestConfig(t)
	empty := ""
	candidate, err := mergeMediaTools(current, MediaToolsInput{FFmpegPath: &empty, FFprobePath: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Media.Mode != "ADVANCED" || candidate.Media.FFmpegPath != "ffmpeg" || candidate.Media.FFprobePath != "ffprobe" {
		t.Fatalf("blank media paths did not select PATH commands: %#v", candidate.Media)
	}
}

func TestMergeMediaToolsAllowsBlankDirectoryForSystemPath(t *testing.T) {
	current := settingsTestConfig(t)
	empty := ""
	candidate, err := mergeMediaTools(current, MediaToolsInput{Directory: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Media.Mode != "ADVANCED" || candidate.Media.FFmpegPath != "ffmpeg" || candidate.Media.FFprobePath != "ffprobe" {
		t.Fatalf("blank automatic directory did not select PATH commands: %#v", candidate.Media)
	}
}

func TestPresentSettingsHidesSecretsAndReportsRestartFields(t *testing.T) {
	cfg := settingsTestConfig(t)
	result, err := presentSettings(cfg, 7, "managed", ListenerDTO{
		IPv4: ListenerAddressDTO{Host: "127.0.0.1", Port: 9999},
		IPv6: ListenerAddressDTO{Host: "::1", Port: 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Database.PasswordConfigured || !result.Storage.SecretAccessKeyConfigured {
		t.Fatalf("secret flags = %#v/%#v", result.Database, result.Storage)
	}
	if result.Database.Username != "xymusic" || result.Database.Database != "xymusic" {
		t.Fatalf("database presentation = %#v", result.Database)
	}
	if !reflect.DeepEqual(result.RestartRequiredFields, []string{"http.ipv4Host", "http.ipv4Port", "http.ipv6Host", "http.ipv6Port"}) {
		t.Fatalf("restart fields = %#v", result.RestartRequiredFields)
	}
}

func settingsTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Parse(map[string]string{
		"NODE_ENV": "production", "DATABASE_URL": "postgres://xymusic:password@127.0.0.1:5432/xymusic?sslmode=disable",
		"DATABASE_MAX_CONNECTIONS": "10", "ACCESS_TOKEN_SECRET": "12345678901234567890123456789012",
		"IDEMPOTENCY_ENCRYPTION_SECRET": "22345678901234567890123456789012",
		"CURSOR_SIGNING_SECRET":         "32345678901234567890123456789012", "S3_ENDPOINT": "http://127.0.0.1:9000",
		"S3_BUCKET": "xymusic", "S3_ACCESS_KEY_ID": "access", "S3_SECRET_ACCESS_KEY": "secret",
		"HTTP_HOST": "0.0.0.0", "HTTP_PORT": "3000", "LOCAL_MUSIC_DIRECTORY": "music",
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
