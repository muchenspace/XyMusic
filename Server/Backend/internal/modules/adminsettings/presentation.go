package adminsettings

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"xymusic/server/internal/config"
)

func presentSettings(cfg config.Config, version int, source string, listener ListenerDTO) (SettingsDTO, error) {
	database, err := presentDatabase(cfg.Database)
	if err != nil {
		return SettingsDTO{}, err
	}
	restart := []string{}
	if cfg.HTTP.IPv4Host != listener.IPv4.Host {
		restart = append(restart, "http.ipv4Host")
	}
	if cfg.HTTP.IPv4Port != listener.IPv4.Port {
		restart = append(restart, "http.ipv4Port")
	}
	if cfg.HTTP.IPv6Host != listener.IPv6.Host {
		restart = append(restart, "http.ipv6Host")
	}
	if cfg.HTTP.IPv6Port != listener.IPv6.Port {
		restart = append(restart, "http.ipv6Port")
	}
	var endpoint *string
	if cfg.Storage.Endpoint != "" {
		value := cfg.Storage.Endpoint
		endpoint = &value
	}
	var publicBaseURL *string
	if cfg.Storage.PublicBaseURL != "" {
		value := cfg.Storage.PublicBaseURL
		publicBaseURL = &value
	}
	var mediaDirectory *string
	if cfg.Media.Mode == "DIRECTORY" {
		value := cfg.Paths.MediaToolsDirectory
		mediaDirectory = &value
	}
	empty := func() []string { return []string{} }
	return SettingsDTO{
		Version: version, Environment: string(cfg.Environment), ConfigurationSource: source,
		ActualListener: listener, RestartRequiredFields: restart,
		Database: database,
		Storage: StorageDTO{
			Endpoint: endpoint, PublicBaseURL: publicBaseURL, Region: cfg.Storage.Region,
			Bucket: cfg.Storage.Bucket, AccessKeyID: cfg.Storage.AccessKeyID,
			SecretAccessKeyConfigured: cfg.Storage.SecretAccessKey != "",
			ForcePathStyle:            cfg.Storage.ForcePathStyle, SignedURLTTLSeconds: cfg.Storage.SignedURLTTLSeconds,
			MaxUploadBytes: cfg.Storage.MaxUploadBytes, LockedFields: empty(),
		},
		MediaTools: MediaToolsDTO{
			Directory: mediaDirectory, FFmpegPath: cfg.Media.FFmpegPath,
			FFprobePath: cfg.Media.FFprobePath, LockedFields: empty(),
		},
		Scraping: ScrapingDTO{
			FPcalcPath: cfg.Scraping.FPcalcPath, AcoustIDClient: cfg.Scraping.AcoustIDClient,
			LockedFields: empty(),
		},
		LocalLibrary: LocalLibraryDTO{
			Name: cfg.LocalLibrary.Name, Directory: cfg.LocalLibrary.Directory,
			Mode: cfg.LocalLibrary.Mode, Enabled: cfg.LocalLibrary.Enabled,
			SyncOnStartup:       cfg.LocalLibrary.SyncOnStartup,
			ScanIntervalMinutes: cfg.LocalLibrary.ScanIntervalMinutes,
			IncludePatterns:     normalizedStrings(cfg.LocalLibrary.IncludePatterns),
			ExcludePatterns:     normalizedStrings(cfg.LocalLibrary.ExcludePatterns), LockedFields: empty(),
		},
		Registration: RegistrationDTO{Enabled: cfg.Registration.Enabled, LockedFields: empty()},
		Security: SecurityDTO{
			AccessTokenTTLSeconds:  cfg.Security.AccessTokenTTLSeconds,
			RefreshTokenTTLSeconds: cfg.Security.RefreshTokenTTLSeconds, LockedFields: empty(),
		},
		HTTP: HTTPDTO{
			IPv4Host: cfg.HTTP.IPv4Host, IPv4Port: cfg.HTTP.IPv4Port,
			IPv6Host: cfg.HTTP.IPv6Host, IPv6Port: cfg.HTTP.IPv6Port,
			TrustedProxyAddresses: normalizedStrings(cfg.HTTP.TrustedProxyAddresses), LockedFields: empty(),
		},
	}, nil
}

func presentDatabase(database config.Database) (DatabaseDTO, error) {
	parsed, err := url.Parse(database.URL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return DatabaseDTO{}, validation("Database configuration is invalid")
	}
	port := 5432
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return DatabaseDTO{}, validation("Database configuration is invalid")
		}
	}
	username := ""
	passwordConfigured := false
	if parsed.User != nil {
		username = parsed.User.Username()
		_, passwordConfigured = parsed.User.Password()
	}
	sslMode := parsed.Query().Get("sslmode")
	if sslMode != "disable" && sslMode != "require" && sslMode != "verify-full" {
		sslMode = "prefer"
	}
	name, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil {
		name = strings.TrimPrefix(parsed.Path, "/")
	}
	return DatabaseDTO{
		Host: parsed.Hostname(), Port: port, Database: name, Username: username,
		SSLMode: sslMode, MaximumConnections: database.MaxConnections,
		PasswordConfigured: passwordConfigured, LockedFields: []string{},
	}, nil
}

func dataDirectory(configurationPath string) string {
	return filepath.Dir(configurationPath)
}

func normalizedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}
