package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrNotConfigured = errors.New("xymusic is not configured")

type Store struct {
	Path string
}

func NewStore(path string) *Store { return &Store{Path: path} }

// Recover completes or rolls back an interrupted atomic configuration write
// and re-applies sensitive-file permissions to an existing valid file.
func (s *Store) Recover() error {
	next := s.Path + ".next"
	backup := s.Path + ".backup"
	if validConfigurationFile(s.Path) {
		_ = os.Remove(next)
		_ = os.Remove(backup)
		return protectSensitiveFile(s.Path)
	}
	replacement := ""
	if validConfigurationFile(next) {
		replacement = next
	} else if validConfigurationFile(backup) {
		replacement = backup
	}
	if replacement == "" {
		return nil
	}
	if err := protectSensitiveFile(replacement); err != nil {
		return err
	}
	_ = os.Remove(s.Path)
	if err := os.Rename(replacement, s.Path); err != nil {
		return err
	}
	_ = os.Remove(next)
	_ = os.Remove(backup)
	return protectSensitiveFile(s.Path)
}

func (s *Store) Clear() error {
	var failures []error
	for _, path := range []string{s.Path, s.Path + ".next", s.Path + ".backup"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Store) Load() (Config, error) {
	values, err := ReadEnvironmentFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, ErrNotConfigured
		}
		return Config{}, fmt.Errorf("read backend configuration: %w", err)
	}
	if len(values) == 0 {
		return Config{}, ErrNotConfigured
	}
	if strings.TrimSpace(values["DATABASE_URL"]) == "" || strings.TrimSpace(values["S3_BUCKET"]) == "" {
		return Config{}, errors.New("backend .env must contain DATABASE_URL and S3_BUCKET")
	}
	cfg, err := Parse(values)
	if err != nil {
		return Config{}, fmt.Errorf("backend .env is invalid: %w", err)
	}
	return cfg, nil
}

func ReadEnvironmentFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		separator := strings.IndexByte(line, '=')
		if separator <= 0 {
			return nil, fmt.Errorf("backend .env line %d is invalid", lineNumber)
		}
		key := strings.TrimSpace(line[:separator])
		if !validEnvironmentKey(key) {
			return nil, fmt.Errorf("backend .env line %d is invalid", lineNumber)
		}
		parsed, err := parseEnvironmentValue(strings.TrimSpace(line[separator+1:]), lineNumber)
		if err != nil {
			return nil, err
		}
		result[key] = parsed
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) Save(cfg Config) error {
	values := ToEnvironment(cfg)
	keys := []string{
		"NODE_ENV", "MIGRATIONS_DIRECTORY", "ADMIN_WEB_DIRECTORY", "MEDIA_TOOLS_DIRECTORY",
		"MEDIA_TOOLS_MODE", "LOCAL_MUSIC_DIRECTORY", "HTTP_HOST", "HTTP_PORT",
		"HTTP_IPV4_HOST", "HTTP_IPV4_PORT", "HTTP_IPV6_HOST", "HTTP_IPV6_PORT",
		"HTTP_TRUSTED_PROXY_ADDRESSES", "DATABASE_URL",
		"DATABASE_MAX_CONNECTIONS", "ACCESS_TOKEN_SECRET", "IDEMPOTENCY_ENCRYPTION_SECRET",
		"CURSOR_SIGNING_SECRET", "ACCESS_TOKEN_TTL_SECONDS", "REFRESH_TOKEN_TTL_SECONDS",
		"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY",
		"S3_FORCE_PATH_STYLE", "S3_PUBLIC_BASE_URL", "MEDIA_SIGNED_URL_TTL_SECONDS",
		"MEDIA_MAX_UPLOAD_BYTES", "FFMPEG_PATH", "FFPROBE_PATH", "FPCALC_PATH",
		"ACOUSTID_CLIENT", "LOCAL_MUSIC_SOURCE_NAME", "LOCAL_MUSIC_SOURCE_MODE",
		"LOCAL_MUSIC_SOURCE_ENABLED", "LOCAL_MUSIC_SYNC_ON_STARTUP",
		"LOCAL_MUSIC_SCAN_INTERVAL_MINUTES", "LOCAL_MUSIC_INCLUDE_PATTERNS",
		"LOCAL_MUSIC_EXCLUDE_PATTERNS", "LOCAL_MUSIC_SCAN_WORKERS", "LOCAL_MUSIC_SCAN_COMMIT_WORKERS",
		"LOCAL_MUSIC_SCAN_COMMIT_BATCH_SIZE", "LOCAL_MUSIC_SCAN_PROBE_WORKERS", "LOCAL_MUSIC_READY_OBJECT_STAT_TTL_SECONDS",
		"MEDIA_VARIANT_PROFILE_VERSION",
		"MEDIA_WORKERS", "MEDIA_PROBE_WORKERS", "MEDIA_STORAGE_WORKERS", "MEDIA_FFMPEG_THREADS", "TAG_SCRAPING_WORKERS", "TAG_SCRAPING_REQUEST_WORKERS",
		"TAG_SCRAPING_CLAIM_WINDOW", "TAG_SCRAPING_ARTWORK_WORKERS",
		"TAG_SCRAPING_ARTWORK_CLAIM_WINDOW", "REGISTRATION_ENABLED",
	}
	var content strings.Builder
	for _, key := range keys {
		content.WriteString(key)
		content.WriteByte('=')
		content.WriteString(strconv.Quote(values[key]))
		content.WriteByte('\n')
	}
	return atomicWrite(s.Path, []byte(content.String()))
}

func ToEnvironment(cfg Config) map[string]string {
	scanInterval := ""
	if cfg.LocalLibrary.ScanIntervalMinutes != nil {
		scanInterval = strconv.Itoa(*cfg.LocalLibrary.ScanIntervalMinutes)
	}
	scanWorkers := cfg.LocalLibrary.ScanWorkers
	if scanWorkers <= 0 {
		scanWorkers = defaultScanWorkers()
	}
	scanCommitWorkers := cfg.LocalLibrary.ScanCommitWorkers
	if scanCommitWorkers <= 0 {
		scanCommitWorkers = defaultScanCommitWorkers()
	}
	scanCommitBatchSize := cfg.LocalLibrary.ScanCommitBatchSize
	if scanCommitBatchSize <= 0 {
		scanCommitBatchSize = 8
	}
	scanProbeWorkers := cfg.LocalLibrary.ScanProbeWorkers
	if scanProbeWorkers <= 0 {
		scanProbeWorkers = defaultScanProbeWorkers()
	}
	readySourceObjectStatTTLSeconds := cfg.LocalLibrary.ReadySourceObjectStatTTLSeconds
	if readySourceObjectStatTTLSeconds < 0 {
		readySourceObjectStatTTLSeconds = 300
	}
	mediaWorkers := cfg.Media.Workers
	if mediaWorkers <= 0 {
		mediaWorkers = defaultMediaWorkers()
	}
	mediaProbeWorkers := cfg.Media.ProbeWorkers
	if mediaProbeWorkers <= 0 {
		mediaProbeWorkers = defaultMediaProbeWorkers(mediaWorkers)
	}
	mediaStorageWorkers := cfg.Media.StorageWorkers
	if mediaStorageWorkers <= 0 {
		mediaStorageWorkers = defaultMediaStorageWorkers(mediaWorkers)
	}
	mediaFFmpegThreads := cfg.Media.FFmpegThreads
	if mediaFFmpegThreads < 0 {
		mediaFFmpegThreads = 0
	}
	batchWorkers := cfg.Scraping.BatchWorkers
	if batchWorkers <= 0 {
		batchWorkers = 8
	}
	batchClaimWindow := cfg.Scraping.BatchClaimWindow
	if batchClaimWindow < batchWorkers {
		batchClaimWindow = defaultScrapingClaimWindow(batchWorkers)
	}
	requestWorkers := cfg.Scraping.RequestWorkers
	if requestWorkers <= 0 {
		requestWorkers = 6
	}
	artworkWorkers := cfg.Scraping.ArtworkWorkers
	if artworkWorkers <= 0 {
		artworkWorkers = 2
	}
	artworkClaimWindow := cfg.Scraping.ArtworkClaimWindow
	if artworkClaimWindow < artworkWorkers {
		artworkClaimWindow = defaultScrapingClaimWindow(artworkWorkers)
	}
	databaseMaxConnections := int(cfg.Database.MaxConnections)
	if databaseMaxConnections <= 0 {
		databaseMaxConnections = defaultDatabaseConnections(
			scanCommitWorkers, mediaWorkers, batchWorkers, artworkWorkers,
		)
	}
	include, _ := jsonStringArray(cfg.LocalLibrary.IncludePatterns)
	exclude, _ := jsonStringArray(cfg.LocalLibrary.ExcludePatterns)
	return map[string]string{
		"NODE_ENV":                                  string(cfg.Environment),
		"MIGRATIONS_DIRECTORY":                      cfg.Paths.MigrationsDirectory,
		"ADMIN_WEB_DIRECTORY":                       cfg.Paths.AdminWebDirectory,
		"MEDIA_TOOLS_DIRECTORY":                     cfg.Paths.MediaToolsDirectory,
		"MEDIA_TOOLS_MODE":                          cfg.Media.Mode,
		"LOCAL_MUSIC_DIRECTORY":                     cfg.Paths.LocalMusicDirectory,
		"HTTP_HOST":                                 cfg.HTTP.IPv4Host,
		"HTTP_PORT":                                 strconv.Itoa(cfg.HTTP.IPv4Port),
		"HTTP_IPV4_HOST":                            cfg.HTTP.IPv4Host,
		"HTTP_IPV4_PORT":                            strconv.Itoa(cfg.HTTP.IPv4Port),
		"HTTP_IPV6_HOST":                            cfg.HTTP.IPv6Host,
		"HTTP_IPV6_PORT":                            strconv.Itoa(cfg.HTTP.IPv6Port),
		"HTTP_TRUSTED_PROXY_ADDRESSES":              strings.Join(cfg.HTTP.TrustedProxyAddresses, ","),
		"DATABASE_URL":                              cfg.Database.URL,
		"DATABASE_MAX_CONNECTIONS":                  strconv.Itoa(databaseMaxConnections),
		"ACCESS_TOKEN_SECRET":                       cfg.Security.AccessTokenSecret,
		"IDEMPOTENCY_ENCRYPTION_SECRET":             cfg.Security.IdempotencyEncryptionSecret,
		"CURSOR_SIGNING_SECRET":                     cfg.Security.CursorSigningSecret,
		"ACCESS_TOKEN_TTL_SECONDS":                  strconv.Itoa(cfg.Security.AccessTokenTTLSeconds),
		"REFRESH_TOKEN_TTL_SECONDS":                 strconv.Itoa(cfg.Security.RefreshTokenTTLSeconds),
		"S3_ENDPOINT":                               cfg.Storage.Endpoint,
		"S3_REGION":                                 cfg.Storage.Region,
		"S3_BUCKET":                                 cfg.Storage.Bucket,
		"S3_ACCESS_KEY_ID":                          cfg.Storage.AccessKeyID,
		"S3_SECRET_ACCESS_KEY":                      cfg.Storage.SecretAccessKey,
		"S3_FORCE_PATH_STYLE":                       strconv.FormatBool(cfg.Storage.ForcePathStyle),
		"S3_PUBLIC_BASE_URL":                        cfg.Storage.PublicBaseURL,
		"MEDIA_SIGNED_URL_TTL_SECONDS":              strconv.Itoa(cfg.Storage.SignedURLTTLSeconds),
		"MEDIA_MAX_UPLOAD_BYTES":                    strconv.FormatInt(cfg.Storage.MaxUploadBytes, 10),
		"FFMPEG_PATH":                               cfg.Media.FFmpegPath,
		"FFPROBE_PATH":                              cfg.Media.FFprobePath,
		"MEDIA_VARIANT_PROFILE_VERSION":             cfg.Media.ProfileVersion,
		"FPCALC_PATH":                               cfg.Scraping.FPcalcPath,
		"ACOUSTID_CLIENT":                           cfg.Scraping.AcoustIDClient,
		"LOCAL_MUSIC_SOURCE_NAME":                   cfg.LocalLibrary.Name,
		"LOCAL_MUSIC_SOURCE_MODE":                   cfg.LocalLibrary.Mode,
		"LOCAL_MUSIC_SOURCE_ENABLED":                strconv.FormatBool(cfg.LocalLibrary.Enabled),
		"LOCAL_MUSIC_SYNC_ON_STARTUP":               strconv.FormatBool(cfg.LocalLibrary.SyncOnStartup),
		"LOCAL_MUSIC_SCAN_INTERVAL_MINUTES":         scanInterval,
		"LOCAL_MUSIC_INCLUDE_PATTERNS":              include,
		"LOCAL_MUSIC_EXCLUDE_PATTERNS":              exclude,
		"LOCAL_MUSIC_SCAN_WORKERS":                  strconv.Itoa(scanWorkers),
		"LOCAL_MUSIC_SCAN_COMMIT_WORKERS":           strconv.Itoa(scanCommitWorkers),
		"LOCAL_MUSIC_SCAN_COMMIT_BATCH_SIZE":        strconv.Itoa(scanCommitBatchSize),
		"LOCAL_MUSIC_SCAN_PROBE_WORKERS":            strconv.Itoa(scanProbeWorkers),
		"LOCAL_MUSIC_READY_OBJECT_STAT_TTL_SECONDS": strconv.Itoa(readySourceObjectStatTTLSeconds),
		"MEDIA_WORKERS":                             strconv.Itoa(mediaWorkers),
		"MEDIA_PROBE_WORKERS":                       strconv.Itoa(mediaProbeWorkers),
		"MEDIA_STORAGE_WORKERS":                     strconv.Itoa(mediaStorageWorkers),
		"MEDIA_FFMPEG_THREADS":                      strconv.Itoa(mediaFFmpegThreads),
		"TAG_SCRAPING_WORKERS":                      strconv.Itoa(batchWorkers),
		"TAG_SCRAPING_CLAIM_WINDOW":                 strconv.Itoa(batchClaimWindow),
		"TAG_SCRAPING_REQUEST_WORKERS":              strconv.Itoa(requestWorkers),
		"TAG_SCRAPING_ARTWORK_WORKERS":              strconv.Itoa(artworkWorkers),
		"TAG_SCRAPING_ARTWORK_CLAIM_WINDOW":         strconv.Itoa(artworkClaimWindow),
		"REGISTRATION_ENABLED":                      strconv.FormatBool(cfg.Registration.Enabled),
	}
}

func parseEnvironmentValue(value string, line int) (string, error) {
	if strings.HasPrefix(value, "\"") {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("backend .env line %d contains an invalid quoted value", line)
		}
		return parsed, nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", fmt.Errorf("backend .env line %d contains an invalid quoted value", line)
		}
		return value[1 : len(value)-1], nil
	}
	if comment := strings.Index(value, " #"); comment >= 0 {
		value = value[:comment]
	}
	return strings.TrimSpace(value), nil
}

func validEnvironmentKey(key string) bool {
	if key == "" || !((key[0] >= 'A' && key[0] <= 'Z') || (key[0] >= 'a' && key[0] <= 'z') || key[0] == '_') {
		return false
	}
	for index := 1; index < len(key); index++ {
		character := key[index]
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_') {
			return false
		}
	}
	return true
}

func atomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	next := path + ".next"
	backup := path + ".backup"
	_ = os.Remove(next)
	file, err := os.OpenFile(next, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(next)
		return err
	}
	if err := protectSensitiveFile(next); err != nil {
		_ = os.Remove(next)
		return err
	}
	_, statErr := os.Stat(path)
	exists := statErr == nil
	if exists {
		_ = os.Remove(backup)
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(next)
			return err
		}
	}
	if err := os.Rename(next, path); err != nil {
		if exists {
			_ = os.Rename(backup, path)
		}
		_ = os.Remove(next)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func jsonStringArray(values []string) (string, error) {
	bytes, err := json.Marshal(values)
	return string(bytes), err
}

func validConfigurationFile(path string) bool {
	values, err := ReadEnvironmentFile(path)
	if err != nil || len(values) == 0 || strings.TrimSpace(values["DATABASE_URL"]) == "" || strings.TrimSpace(values["S3_BUCKET"]) == "" {
		return false
	}
	_, err = Parse(values)
	return err == nil
}
