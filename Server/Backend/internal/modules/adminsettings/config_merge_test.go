package adminsettings

import (
	"encoding/json"
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
	assetDirectory := "new-assets"
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
		Storage:    &StorageInput{AssetDirectory: &assetDirectory},
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
	if candidate.Database.MaxConnections != 20 || candidate.MediaStorage.AssetDirectory != "new-assets" {
		t.Fatalf("database/storage = %#v/%#v", candidate.Database, candidate.MediaStorage)
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
	for _, required := range []string{"database.url", "storage.assetDirectory", "media.ffmpegPath", "localLibrary.name", "registration.enabled", "http.ipv4Port"} {
		if !contains(fields, required) {
			t.Errorf("changed fields missing %q: %v", required, fields)
		}
	}
}

// Redacted settings responses must survive the actual JSON contract. A client
// that updates only the library section must not manufacture nil/empty
// database credentials while the request is decoded.
func TestMergeSettingsPreservesCredentialsWhenRedactedFieldsAreOmittedFromJSON(t *testing.T) {
	current := settingsTestConfig(t)
	encoded := []byte(`{"expectedVersion":1,"localLibrary":{"name":"Updated library"}}`)
	var decoded UpdateInput
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Database != nil || decoded.Storage != nil {
		t.Fatalf("redacted sections unexpectedly appeared in payload: %#v", decoded)
	}
	candidate, err := mergeSettings(current, decoded)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Database.URL != current.Database.URL {
		t.Fatalf("database credentials changed after JSON decode: before=%q after=%q", current.Database.URL, candidate.Database.URL)
	}
}

func TestMergeSettingsLocalLibraryOnlyPreservesExternalDependencies(t *testing.T) {
	current := settingsTestConfig(t)
	name := "Second library"
	directory := "second-library"
	mode := "READ_WRITE"
	enabled := true
	syncOnStartup := false
	include := []string{"**/*.flac"}
	exclude := []string{"**/tmp/**"}
	candidate, err := mergeSettings(current, UpdateInput{
		ExpectedVersion: 1,
		LocalLibrary: &LocalLibraryInput{
			Name: &name, Directory: &directory, Mode: &mode, Enabled: &enabled,
			SyncOnStartup: &syncOnStartup, IncludePatterns: &include, ExcludePatterns: &exclude,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Database != current.Database {
		t.Fatalf("database changed during local-library-only update: before=%#v after=%#v", current.Database, candidate.Database)
	}
	if candidate.MediaStorage != current.MediaStorage {
		t.Fatalf("storage changed during local-library-only update: before=%#v after=%#v", current.MediaStorage, candidate.MediaStorage)
	}
	if candidate.LocalLibrary.Name != name || candidate.LocalLibrary.Directory != directory ||
		candidate.LocalLibrary.Mode != mode || candidate.LocalLibrary.Enabled != enabled ||
		candidate.LocalLibrary.SyncOnStartup != syncOnStartup {
		t.Fatalf("local library was not updated: %#v", candidate.LocalLibrary)
	}
}

func TestMergeSettingsEverySectionPreservesExternalCredentials(t *testing.T) {
	current := settingsTestConfig(t)
	cases := []struct {
		name  string
		input func() UpdateInput
	}{
		{
			name: "database",
			input: func() UpdateInput {
				maximumConnections := 11
				return UpdateInput{Database: &DatabaseInput{MaximumConnections: &maximumConnections}}
			},
		},
		{
			name: "storage",
			input: func() UpdateInput {
				assetDirectory := "custom-assets"
				return UpdateInput{Storage: &StorageInput{AssetDirectory: &assetDirectory}}
			},
		},
		{
			name: "media tools",
			input: func() UpdateInput {
				directory := "media-tools"
				return UpdateInput{MediaTools: &MediaToolsInput{Directory: &directory}}
			},
		},
		{
			name: "local library",
			input: func() UpdateInput {
				name := "Updated library"
				return UpdateInput{LocalLibrary: &LocalLibraryInput{Name: &name}}
			},
		},
		{
			name: "registration",
			input: func() UpdateInput {
				enabled := true
				return UpdateInput{Registration: &RegistrationInput{Enabled: &enabled}}
			},
		},
		{
			name: "security",
			input: func() UpdateInput {
				ttl := 901
				return UpdateInput{Security: &SecurityInput{AccessTokenTTLSeconds: &ttl}}
			},
		},
		{
			name: "http",
			input: func() UpdateInput {
				port := 3001
				return UpdateInput{HTTP: &HTTPInput{IPv4Port: &port}}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			candidate, err := mergeSettings(current, testCase.input())
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Database.URL != current.Database.URL {
				t.Fatalf("database credentials changed: before=%q after=%q", current.Database.URL, candidate.Database.URL)
			}
		})
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
	invalidTTL := 10
	_, err = mergeStorage(current, StorageInput{UploadTTLSeconds: &invalidTTL})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("invalid upload TTL error = %v", err)
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
	if !result.Database.PasswordConfigured {
		t.Fatalf("secret flags = %#v", result.Database)
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
		"CURSOR_SIGNING_SECRET":         "32345678901234567890123456789012",
		"PLAYBACK_TICKET_SECRET":        "42345678901234567890123456789012",
		"MEDIA_ASSET_DIRECTORY":         "assets",
		"MEDIA_TRANSCODE_DIRECTORY":     "transcode",
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
