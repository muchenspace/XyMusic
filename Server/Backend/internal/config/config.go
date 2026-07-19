package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	MaxServerRequestBodyBytes     int64 = 1024 * 1024 * 1024
	MaxStructuredRequestBodyBytes int64 = 2 * 1024 * 1024

	DefaultMigrationsDirectory = "migrations"
	DefaultAdminWebDirectory   = "admin"
	DefaultMediaToolsDirectory = "tools"
	DefaultLocalMusicDirectory = "music"
)

type Environment string

const (
	Development Environment = "development"
	Test        Environment = "test"
	Production  Environment = "production"
)

type Config struct {
	Environment  Environment
	Paths        Paths
	HTTP         HTTP
	Database     Database
	Security     Security
	Storage      Storage
	Media        Media
	Scraping     Scraping
	LocalLibrary LocalLibrary
	Registration Registration
}

type Paths struct {
	MigrationsDirectory string
	AdminWebDirectory   string
	MediaToolsDirectory string
	LocalMusicDirectory string
}

type HTTP struct {
	IPv4Host              string
	IPv4Port              int
	IPv6Host              string
	IPv6Port              int
	Host                  string
	Port                  int
	TrustedProxyAddresses []string
}

type Database struct {
	URL            string
	MaxConnections int32
}

type Security struct {
	AccessTokenSecret           string
	IdempotencyEncryptionSecret string
	CursorSigningSecret         string
	AccessTokenTTLSeconds       int
	RefreshTokenTTLSeconds      int
}

type Storage struct {
	Endpoint            string
	Region              string
	Bucket              string
	AccessKeyID         string
	SecretAccessKey     string
	ForcePathStyle      bool
	PublicBaseURL       string
	SignedURLTTLSeconds int
	MaxUploadBytes      int64
}

type Media struct {
	Mode        string
	FFmpegPath  string
	FFprobePath string
}

type Scraping struct {
	FPcalcPath     string
	AcoustIDClient string
}

type LocalLibrary struct {
	Name                string
	Directory           string
	Mode                string
	Enabled             bool
	SyncOnStartup       bool
	ScanIntervalMinutes *int
	IncludePatterns     []string
	ExcludePatterns     []string
}

type Registration struct {
	Enabled bool
}

// Parse preserves the legacy backend's configuration contract. Relative paths
// remain relative here and are resolved only by ResolveRuntime.
func Parse(env map[string]string) (Config, error) {
	environment, err := parseEnvironment(value(env, "NODE_ENV", "development"))
	if err != nil {
		return Config{}, err
	}
	production := environment == Production

	paths := Paths{
		MigrationsDirectory: value(env, "MIGRATIONS_DIRECTORY", DefaultMigrationsDirectory),
		AdminWebDirectory:   value(env, "ADMIN_WEB_DIRECTORY", DefaultAdminWebDirectory),
		MediaToolsDirectory: value(env, "MEDIA_TOOLS_DIRECTORY", DefaultMediaToolsDirectory),
		LocalMusicDirectory: value(env, "LOCAL_MUSIC_DIRECTORY", DefaultLocalMusicDirectory),
	}
	for name, candidate := range map[string]string{
		"MIGRATIONS_DIRECTORY":  paths.MigrationsDirectory,
		"ADMIN_WEB_DIRECTORY":   paths.AdminWebDirectory,
		"MEDIA_TOOLS_DIRECTORY": paths.MediaToolsDirectory,
		"LOCAL_MUSIC_DIRECTORY": paths.LocalMusicDirectory,
	} {
		if err := validatePath(candidate, name); err != nil {
			return Config{}, err
		}
	}

	fpcalcPath := strings.TrimSpace(env["FPCALC_PATH"])
	if err := validateOptionalPath(fpcalcPath, "FPCALC_PATH"); err != nil {
		return Config{}, err
	}
	acoustIDClient := strings.TrimSpace(env["ACOUSTID_CLIENT"])
	if (fpcalcPath != "") != (acoustIDClient != "") {
		return Config{}, errors.New("FPCALC_PATH and ACOUSTID_CLIENT must be configured together")
	}

	databaseURL := strings.TrimSpace(env["DATABASE_URL"])
	if databaseURL == "" && !production {
		databaseURL = "postgres://xymusic:xymusic@127.0.0.1:5432/xymusic"
	}
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if err := validatePostgresURL(databaseURL); err != nil {
		return Config{}, err
	}

	databaseConnections, err := integer(env, "DATABASE_MAX_CONNECTIONS", 10, 1, 100)
	if err != nil {
		return Config{}, err
	}
	legacyHTTPPort, err := integer(env, "HTTP_PORT", 3000, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	httpIPv4Port, err := integer(env, "HTTP_IPV4_PORT", legacyHTTPPort, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	httpIPv6Port, err := integer(env, "HTTP_IPV6_PORT", legacyHTTPPort, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	accessTTL, err := integer(env, "ACCESS_TOKEN_TTL_SECONDS", 900, 60, 86400)
	if err != nil {
		return Config{}, err
	}
	refreshTTL, err := integer(env, "REFRESH_TOKEN_TTL_SECONDS", 2592000, 3600, 31536000)
	if err != nil {
		return Config{}, err
	}
	signedURLTTL, err := integer(env, "MEDIA_SIGNED_URL_TTL_SECONDS", 300, 30, 3600)
	if err != nil {
		return Config{}, err
	}
	maxUploadBytes, err := integer64(env, "MEDIA_MAX_UPLOAD_BYTES", MaxServerRequestBodyBytes, 1, MaxServerRequestBodyBytes)
	if err != nil {
		return Config{}, err
	}

	accessSecret, err := secret(env, "ACCESS_TOKEN_SECRET", production)
	if err != nil {
		return Config{}, err
	}
	idempotencySecret, err := secret(env, "IDEMPOTENCY_ENCRYPTION_SECRET", production)
	if err != nil {
		return Config{}, err
	}
	cursorSecret, err := secret(env, "CURSOR_SIGNING_SECRET", production)
	if err != nil {
		return Config{}, err
	}

	bucket := strings.TrimSpace(env["S3_BUCKET"])
	accessKeyID := strings.TrimSpace(env["S3_ACCESS_KEY_ID"])
	secretAccessKey := strings.TrimSpace(env["S3_SECRET_ACCESS_KEY"])
	if !production {
		if bucket == "" {
			bucket = "xymusic"
		}
		if accessKeyID == "" {
			accessKeyID = "minioadmin"
		}
		if secretAccessKey == "" {
			secretAccessKey = "minioadmin"
		}
	}
	if bucket == "" {
		return Config{}, errors.New("S3_BUCKET is required")
	}
	if accessKeyID == "" {
		return Config{}, errors.New("S3_ACCESS_KEY_ID is required")
	}
	if secretAccessKey == "" {
		return Config{}, errors.New("S3_SECRET_ACCESS_KEY is required")
	}

	forcePathStyle, err := boolean(env, "S3_FORCE_PATH_STYLE", true)
	if err != nil {
		return Config{}, err
	}
	registrationEnabled, err := boolean(env, "REGISTRATION_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	localEnabled, err := boolean(env, "LOCAL_MUSIC_SOURCE_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	syncOnStartup, err := boolean(env, "LOCAL_MUSIC_SYNC_ON_STARTUP", true)
	if err != nil {
		return Config{}, err
	}
	scanInterval, err := nullableInteger(env, "LOCAL_MUSIC_SCAN_INTERVAL_MINUTES", 5, 10080)
	if err != nil {
		return Config{}, err
	}
	includePatterns, err := stringArray(env["LOCAL_MUSIC_INCLUDE_PATTERNS"])
	if err != nil {
		return Config{}, fmt.Errorf("LOCAL_MUSIC_INCLUDE_PATTERNS: %w", err)
	}
	excludePatterns, err := stringArray(env["LOCAL_MUSIC_EXCLUDE_PATTERNS"])
	if err != nil {
		return Config{}, fmt.Errorf("LOCAL_MUSIC_EXCLUDE_PATTERNS: %w", err)
	}

	mediaMode := strings.TrimSpace(env["MEDIA_TOOLS_MODE"])
	if mediaMode == "" {
		if strings.TrimSpace(env["FFMPEG_PATH"]) != "" || strings.TrimSpace(env["FFPROBE_PATH"]) != "" {
			mediaMode = "ADVANCED"
		} else if strings.TrimSpace(env["MEDIA_TOOLS_DIRECTORY"]) != "" {
			mediaMode = "DIRECTORY"
		} else {
			mediaMode = "ADVANCED"
		}
	}
	if mediaMode != "DIRECTORY" && mediaMode != "ADVANCED" {
		return Config{}, errors.New("MEDIA_TOOLS_MODE has an unsupported value")
	}
	ffmpegPath := strings.TrimSpace(env["FFMPEG_PATH"])
	ffprobePath := strings.TrimSpace(env["FFPROBE_PATH"])
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	if err := validatePath(ffmpegPath, "FFMPEG_PATH"); err != nil {
		return Config{}, err
	}
	if err := validatePath(ffprobePath, "FFPROBE_PATH"); err != nil {
		return Config{}, err
	}

	localMode := value(env, "LOCAL_MUSIC_SOURCE_MODE", "READ_ONLY")
	if localMode != "READ_ONLY" && localMode != "READ_WRITE" {
		return Config{}, errors.New("LOCAL_MUSIC_SOURCE_MODE has an unsupported value")
	}

	legacyHost := strings.Trim(strings.TrimSpace(env["HTTP_HOST"]), "[]")
	ipv4Fallback := "0.0.0.0"
	ipv6Fallback := "::"
	if address := net.ParseIP(legacyHost); address != nil {
		if address.To4() != nil {
			ipv4Fallback = legacyHost
		} else {
			ipv6Fallback = legacyHost
		}
	}
	ipv4Host := strings.Trim(value(env, "HTTP_IPV4_HOST", ipv4Fallback), "[]")
	ipv6Host := strings.Trim(value(env, "HTTP_IPV6_HOST", ipv6Fallback), "[]")
	if err := validateListenerHost(ipv4Host, true, "HTTP_IPV4_HOST"); err != nil {
		return Config{}, err
	}
	if err := validateListenerHost(ipv6Host, false, "HTTP_IPV6_HOST"); err != nil {
		return Config{}, err
	}

	return Config{
		Environment: environment,
		Paths:       paths,
		HTTP: HTTP{
			IPv4Host:              ipv4Host,
			IPv4Port:              httpIPv4Port,
			IPv6Host:              ipv6Host,
			IPv6Port:              httpIPv6Port,
			Host:                  ipv4Host,
			Port:                  httpIPv4Port,
			TrustedProxyAddresses: commaSeparated(env["HTTP_TRUSTED_PROXY_ADDRESSES"]),
		},
		Database: Database{URL: databaseURL, MaxConnections: int32(databaseConnections)},
		Security: Security{
			AccessTokenSecret:           accessSecret,
			IdempotencyEncryptionSecret: idempotencySecret,
			CursorSigningSecret:         cursorSecret,
			AccessTokenTTLSeconds:       accessTTL,
			RefreshTokenTTLSeconds:      refreshTTL,
		},
		Storage: Storage{
			Endpoint:            strings.TrimRight(strings.TrimSpace(env["S3_ENDPOINT"]), "/"),
			Region:              value(env, "S3_REGION", "us-east-1"),
			Bucket:              bucket,
			AccessKeyID:         accessKeyID,
			SecretAccessKey:     secretAccessKey,
			ForcePathStyle:      forcePathStyle,
			PublicBaseURL:       strings.TrimRight(strings.TrimSpace(env["S3_PUBLIC_BASE_URL"]), "/"),
			SignedURLTTLSeconds: signedURLTTL,
			MaxUploadBytes:      maxUploadBytes,
		},
		Media:    Media{Mode: mediaMode, FFmpegPath: ffmpegPath, FFprobePath: ffprobePath},
		Scraping: Scraping{FPcalcPath: fpcalcPath, AcoustIDClient: acoustIDClient},
		LocalLibrary: LocalLibrary{
			Name:                value(env, "LOCAL_MUSIC_SOURCE_NAME", "Music"),
			Directory:           paths.LocalMusicDirectory,
			Mode:                localMode,
			Enabled:             localEnabled,
			SyncOnStartup:       syncOnStartup,
			ScanIntervalMinutes: scanInterval,
			IncludePatterns:     includePatterns,
			ExcludePatterns:     excludePatterns,
		},
		Registration: Registration{Enabled: registrationEnabled},
	}, nil
}

func ResolveRuntime(cfg Config, root string) (Config, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Config{}, fmt.Errorf("resolve executable root: %w", err)
	}
	resolve := func(candidate, name string) (string, error) {
		candidate = strings.TrimSpace(candidate)
		if err := validatePath(candidate, name); err != nil {
			return "", err
		}
		if filepath.IsAbs(candidate) {
			return filepath.Clean(candidate), nil
		}
		return filepath.Join(absRoot, candidate), nil
	}
	resolveExecutable := func(candidate, name string) (string, error) {
		candidate = strings.TrimSpace(candidate)
		if err := validatePath(candidate, name); err != nil {
			return "", err
		}
		if filepath.IsAbs(candidate) {
			return filepath.Clean(candidate), nil
		}
		if filepath.Base(candidate) == candidate && filepath.VolumeName(candidate) == "" {
			return candidate, nil
		}
		return filepath.Join(absRoot, candidate), nil
	}
	if cfg.Paths.MigrationsDirectory, err = resolve(cfg.Paths.MigrationsDirectory, "MIGRATIONS_DIRECTORY"); err != nil {
		return Config{}, err
	}
	if cfg.Paths.AdminWebDirectory, err = resolve(cfg.Paths.AdminWebDirectory, "ADMIN_WEB_DIRECTORY"); err != nil {
		return Config{}, err
	}
	if cfg.Paths.MediaToolsDirectory, err = resolve(cfg.Paths.MediaToolsDirectory, "MEDIA_TOOLS_DIRECTORY"); err != nil {
		return Config{}, err
	}
	if cfg.Paths.LocalMusicDirectory, err = resolve(cfg.Paths.LocalMusicDirectory, "LOCAL_MUSIC_DIRECTORY"); err != nil {
		return Config{}, err
	}
	if cfg.Media.FFmpegPath, err = resolveExecutable(cfg.Media.FFmpegPath, "FFMPEG_PATH"); err != nil {
		return Config{}, err
	}
	if cfg.Media.FFprobePath, err = resolveExecutable(cfg.Media.FFprobePath, "FFPROBE_PATH"); err != nil {
		return Config{}, err
	}
	if cfg.Scraping.FPcalcPath != "" {
		if cfg.Scraping.FPcalcPath, err = resolve(cfg.Scraping.FPcalcPath, "FPCALC_PATH"); err != nil {
			return Config{}, err
		}
	}
	cfg.LocalLibrary.Directory = cfg.Paths.LocalMusicDirectory
	return cfg, nil
}

func parseEnvironment(raw string) (Environment, error) {
	value := Environment(raw)
	switch value {
	case Development, Test, Production:
		return value, nil
	default:
		return "", errors.New("NODE_ENV has an unsupported value")
	}
}

func secret(env map[string]string, name string, production bool) (string, error) {
	value := env[name]
	if value == "" && !production {
		digest := sha256.Sum256([]byte("xymusic-development-only-secret-seed\x00" + name))
		value = base64.RawURLEncoding.EncodeToString(digest[:])
	}
	if len(value) < 32 {
		return "", fmt.Errorf("%s must contain at least 32 characters", name)
	}
	return value, nil
}

func integer(env map[string]string, name string, fallback, minimum, maximum int) (int, error) {
	raw, ok := env[name]
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, minimum, maximum)
	}
	return parsed, nil
}

func integer64(env map[string]string, name string, fallback, minimum, maximum int64) (int64, error) {
	raw, ok := env[name]
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, minimum, maximum)
	}
	return parsed, nil
}

func nullableInteger(env map[string]string, name string, minimum, maximum int) (*int, error) {
	raw, ok := env[name]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := integer(env, name, minimum, minimum, maximum)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func boolean(env map[string]string, name string, fallback bool) (bool, error) {
	raw, ok := env[name]
	if !ok {
		return fallback, nil
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func stringArray(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, errors.New("must contain a valid JSON string array")
	}
	cleaned := make([]string, 0, len(values))
	for _, candidate := range values {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			cleaned = append(cleaned, candidate)
		}
	}
	return unique(cleaned), nil
}

func commaSeparated(raw string) []string {
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return unique(result)
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, item := range values {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func validatePath(candidate, name string) error {
	if strings.TrimSpace(candidate) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if len(candidate) > 4000 {
		return fmt.Errorf("%s is too long", name)
	}
	return nil
}

func validateOptionalPath(candidate, name string) error {
	if candidate == "" {
		return nil
	}
	return validatePath(candidate, name)
}

func validateListenerHost(host string, ipv4 bool, name string) error {
	address := net.ParseIP(strings.Trim(host, "[]"))
	if address == nil || (ipv4 && address.To4() == nil) || (!ipv4 && address.To4() != nil) {
		family := "IPv6"
		if ipv4 {
			family = "IPv4"
		}
		return fmt.Errorf("%s must be an %s address", name, family)
	}
	return nil
}

func validatePostgresURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		return errors.New("DATABASE_URL must be a valid PostgreSQL URL")
	}
	return nil
}

func value(env map[string]string, name, fallback string) string {
	if candidate, ok := env[name]; ok && strings.TrimSpace(candidate) != "" {
		return candidate
	}
	return fallback
}

func executableName(name string) string {
	if filepath.Separator == '\\' {
		return name + ".exe"
	}
	return name
}
