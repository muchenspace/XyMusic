package adminsettings

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"

	"xymusic/server/internal/config"
	"xymusic/server/internal/shared/apperror"
)

func mergeSettings(current config.Config, input UpdateInput) (config.Config, error) {
	candidate := current
	var err error
	if input.Database != nil {
		candidate, err = mergeDatabase(candidate, *input.Database)
	}
	if err == nil && input.Storage != nil {
		candidate, err = mergeStorage(candidate, *input.Storage)
	}
	if err == nil && input.MediaTools != nil {
		candidate, err = mergeMediaTools(candidate, *input.MediaTools)
	}
	if err == nil && input.Scraping != nil {
		candidate, err = mergeScraping(candidate, *input.Scraping)
	}
	if err != nil {
		return config.Config{}, err
	}
	environment := config.ToEnvironment(candidate)
	if input.LocalLibrary != nil {
		library := input.LocalLibrary
		if library.Name != nil {
			environment["LOCAL_MUSIC_SOURCE_NAME"], err = requiredText(*library.Name, 120, "localLibrary.name")
		}
		if err == nil && library.Directory != nil {
			environment["LOCAL_MUSIC_DIRECTORY"], err = requiredText(*library.Directory, 4000, "localLibrary.directory")
		}
		if err == nil && library.Mode != nil {
			if *library.Mode != "READ_ONLY" && *library.Mode != "READ_WRITE" {
				err = validation("localLibrary.mode is invalid")
			} else {
				environment["LOCAL_MUSIC_SOURCE_MODE"] = *library.Mode
			}
		}
		if library.Enabled != nil {
			environment["LOCAL_MUSIC_SOURCE_ENABLED"] = strconv.FormatBool(*library.Enabled)
		}
		if library.SyncOnStartup != nil {
			environment["LOCAL_MUSIC_SYNC_ON_STARTUP"] = strconv.FormatBool(*library.SyncOnStartup)
		}
		if err == nil && library.ScanIntervalMinutes.Set {
			if library.ScanIntervalMinutes.Value == nil {
				environment["LOCAL_MUSIC_SCAN_INTERVAL_MINUTES"] = ""
			} else if value := *library.ScanIntervalMinutes.Value; value < 5 || value > 10080 {
				err = validation("localLibrary.scanIntervalMinutes is invalid")
			} else {
				environment["LOCAL_MUSIC_SCAN_INTERVAL_MINUTES"] = strconv.Itoa(value)
			}
		}
		if err == nil && library.IncludePatterns != nil {
			environment["LOCAL_MUSIC_INCLUDE_PATTERNS"], err = encodePatterns(*library.IncludePatterns, "localLibrary.includePatterns")
		}
		if err == nil && library.ExcludePatterns != nil {
			environment["LOCAL_MUSIC_EXCLUDE_PATTERNS"], err = encodePatterns(*library.ExcludePatterns, "localLibrary.excludePatterns")
		}
	}
	if input.Registration != nil {
		if input.Registration.Enabled == nil {
			return config.Config{}, validation("registration.enabled is required")
		}
		environment["REGISTRATION_ENABLED"] = strconv.FormatBool(*input.Registration.Enabled)
	}
	if err == nil && input.Security != nil {
		if value := input.Security.AccessTokenTTLSeconds; value != nil {
			if *value < 60 || *value > 86400 {
				err = validation("security.accessTokenTtlSeconds is invalid")
			} else {
				environment["ACCESS_TOKEN_TTL_SECONDS"] = strconv.Itoa(*value)
			}
		}
		if value := input.Security.RefreshTokenTTLSeconds; err == nil && value != nil {
			if *value < 3600 || *value > 31536000 {
				err = validation("security.refreshTokenTtlSeconds is invalid")
			} else {
				environment["REFRESH_TOKEN_TTL_SECONDS"] = strconv.Itoa(*value)
			}
		}
	}
	if err == nil && input.HTTP != nil {
		ipv4Host := input.HTTP.IPv4Host
		if ipv4Host == nil {
			ipv4Host = input.HTTP.Host
		}
		ipv4Port := input.HTTP.IPv4Port
		if ipv4Port == nil {
			ipv4Port = input.HTTP.Port
		}
		if ipv4Host != nil {
			host, hostErr := listenerHost(*ipv4Host, true, "http.ipv4Host")
			if hostErr != nil {
				err = hostErr
			} else {
				environment["HTTP_IPV4_HOST"] = host
				environment["HTTP_HOST"] = host
			}
		}
		if err == nil && ipv4Port != nil {
			if *ipv4Port < 1 || *ipv4Port > 65535 {
				err = validation("http.ipv4Port is invalid")
			} else {
				environment["HTTP_IPV4_PORT"] = strconv.Itoa(*ipv4Port)
				environment["HTTP_PORT"] = strconv.Itoa(*ipv4Port)
			}
		}
		if err == nil && input.HTTP.IPv6Host != nil {
			host, hostErr := listenerHost(*input.HTTP.IPv6Host, false, "http.ipv6Host")
			if hostErr != nil {
				err = hostErr
			} else {
				environment["HTTP_IPV6_HOST"] = host
			}
		}
		if err == nil && input.HTTP.IPv6Port != nil {
			if *input.HTTP.IPv6Port < 1 || *input.HTTP.IPv6Port > 65535 {
				err = validation("http.ipv6Port is invalid")
			} else {
				environment["HTTP_IPV6_PORT"] = strconv.Itoa(*input.HTTP.IPv6Port)
			}
		}
		if err == nil && input.HTTP.TrustedProxyAddresses != nil {
			values, valuesErr := uniqueTexts(*input.HTTP.TrustedProxyAddresses, 100, 2, 64, "http.trustedProxyAddresses")
			if valuesErr != nil {
				err = valuesErr
			} else {
				for _, address := range values {
					if net.ParseIP(address) == nil {
						err = validation("trustedProxyAddresses must contain IP addresses")
						break
					}
				}
				environment["HTTP_TRUSTED_PROXY_ADDRESSES"] = strings.Join(values, ",")
			}
		}
	}
	if err != nil {
		return config.Config{}, err
	}
	return parseCandidate(environment)
}

func mergeDatabase(current config.Config, input DatabaseInput) (config.Config, error) {
	parsed, err := url.Parse(current.Database.URL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return config.Config{}, validation("Database configuration is invalid")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if input.Host != nil {
		host, err = requiredText(strings.Trim(*input.Host, "[]"), 255, "database.host")
		if err != nil || strings.ContainsAny(host, " \t\r\n/@?#") {
			return config.Config{}, validation("database.host is invalid")
		}
	}
	if input.Port != nil {
		if *input.Port < 1 || *input.Port > 65535 {
			return config.Config{}, validation("database.port is invalid")
		}
		port = strconv.Itoa(*input.Port)
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
	if input.Database != nil {
		value, valueErr := requiredText(*input.Database, 255, "database.database")
		if valueErr != nil {
			return config.Config{}, valueErr
		}
		parsed.Path = "/" + value
		parsed.RawPath = ""
	}
	username := ""
	password := ""
	passwordSet := false
	if parsed.User != nil {
		username = parsed.User.Username()
		password, passwordSet = parsed.User.Password()
	}
	if input.Username != nil {
		username, err = requiredText(*input.Username, 255, "database.username")
		if err != nil {
			return config.Config{}, err
		}
	}
	if input.Password != nil {
		if textLength(*input.Password) > 2000 {
			return config.Config{}, validation("database.password is invalid")
		}
		password = *input.Password
		passwordSet = true
	}
	if passwordSet {
		parsed.User = url.UserPassword(username, password)
	} else {
		parsed.User = url.User(username)
	}
	if input.SSLMode != nil {
		switch *input.SSLMode {
		case "disable", "prefer", "require", "verify-full":
			query := parsed.Query()
			query.Set("sslmode", *input.SSLMode)
			parsed.RawQuery = query.Encode()
		default:
			return config.Config{}, validation("database.sslMode is invalid")
		}
	}
	environment := config.ToEnvironment(current)
	environment["DATABASE_URL"] = parsed.String()
	if input.MaximumConnections != nil {
		if *input.MaximumConnections < 1 || *input.MaximumConnections > 100 {
			return config.Config{}, validation("database.maximumConnections is invalid")
		}
		environment["DATABASE_MAX_CONNECTIONS"] = strconv.Itoa(*input.MaximumConnections)
	}
	return parseCandidate(environment)
}

func mergeStorage(current config.Config, input StorageInput) (config.Config, error) {
	environment := config.ToEnvironment(current)
	if input.Endpoint.Set {
		value, err := nullableHTTPURL(input.Endpoint.Value, "storage.endpoint")
		if err != nil {
			return config.Config{}, err
		}
		environment["S3_ENDPOINT"] = value
	}
	if input.PublicBaseURL.Set {
		value, err := nullableHTTPURL(input.PublicBaseURL.Value, "storage.publicBaseUrl")
		if err != nil {
			return config.Config{}, err
		}
		environment["S3_PUBLIC_BASE_URL"] = value
	}
	fields := []struct {
		value *string
		key   string
		max   int
		name  string
	}{{input.Region, "S3_REGION", 100, "storage.region"}, {input.Bucket, "S3_BUCKET", 255, "storage.bucket"}, {input.AccessKeyID, "S3_ACCESS_KEY_ID", 500, "storage.accessKeyId"}, {input.SecretAccessKey, "S3_SECRET_ACCESS_KEY", 2000, "storage.secretAccessKey"}}
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		value, err := requiredText(*field.value, field.max, field.name)
		if err != nil {
			return config.Config{}, err
		}
		environment[field.key] = value
	}
	if input.ForcePathStyle != nil {
		environment["S3_FORCE_PATH_STYLE"] = strconv.FormatBool(*input.ForcePathStyle)
	}
	if input.SignedURLTTLSeconds != nil {
		if *input.SignedURLTTLSeconds < 30 || *input.SignedURLTTLSeconds > 3600 {
			return config.Config{}, validation("storage.signedUrlTtlSeconds is invalid")
		}
		environment["MEDIA_SIGNED_URL_TTL_SECONDS"] = strconv.Itoa(*input.SignedURLTTLSeconds)
	}
	if input.MaxUploadBytes != nil {
		if *input.MaxUploadBytes < 1 || *input.MaxUploadBytes > config.MaxServerRequestBodyBytes {
			return config.Config{}, validation("storage.maxUploadBytes is invalid")
		}
		environment["MEDIA_MAX_UPLOAD_BYTES"] = strconv.FormatInt(*input.MaxUploadBytes, 10)
	}
	return parseCandidate(environment)
}

func mergeMediaTools(current config.Config, input MediaToolsInput) (config.Config, error) {
	environment := config.ToEnvironment(current)
	if input.Directory != nil {
		if strings.TrimSpace(*input.Directory) == "" {
			environment["MEDIA_TOOLS_MODE"] = "ADVANCED"
			environment["FFMPEG_PATH"] = ""
			environment["FFPROBE_PATH"] = ""
			return parseCandidate(environment)
		}
		directory, err := requiredText(*input.Directory, 2000, "mediaTools.directory")
		if err != nil {
			return config.Config{}, err
		}
		environment["MEDIA_TOOLS_DIRECTORY"] = directory
		environment["MEDIA_TOOLS_MODE"] = "DIRECTORY"
		environment["FFMPEG_PATH"] = filepath.Join(directory, executableName("ffmpeg"))
		environment["FFPROBE_PATH"] = filepath.Join(directory, executableName("ffprobe"))
		return parseCandidate(environment)
	}
	if input.FFmpegPath != nil {
		value, err := optionalText(*input.FFmpegPath, 2000, "mediaTools.ffmpegPath")
		if err != nil {
			return config.Config{}, err
		}
		environment["FFMPEG_PATH"] = value
	}
	if input.FFprobePath != nil {
		value, err := optionalText(*input.FFprobePath, 2000, "mediaTools.ffprobePath")
		if err != nil {
			return config.Config{}, err
		}
		environment["FFPROBE_PATH"] = value
	}
	if input.FFmpegPath != nil || input.FFprobePath != nil {
		environment["MEDIA_TOOLS_MODE"] = "ADVANCED"
	}
	return parseCandidate(environment)
}

func mergeScraping(current config.Config, input ScrapingInput) (config.Config, error) {
	environment := config.ToEnvironment(current)
	if input.FPcalcPath != nil {
		value, err := optionalText(*input.FPcalcPath, 2000, "scraping.fpcalcPath")
		if err != nil {
			return config.Config{}, err
		}
		environment["FPCALC_PATH"] = value
	}
	if input.AcoustIDClient != nil {
		value, err := optionalText(*input.AcoustIDClient, 500, "scraping.acoustIdClient")
		if err != nil {
			return config.Config{}, err
		}
		environment["ACOUSTID_CLIENT"] = value
	}
	return parseCandidate(environment)
}

func changedFields(previous, candidate config.Config) []string {
	pairs := []struct {
		name        string
		left, right any
	}{
		{"database.url", previous.Database.URL, candidate.Database.URL},
		{"database.maximumConnections", previous.Database.MaxConnections, candidate.Database.MaxConnections},
		{"storage.endpoint", previous.Storage.Endpoint, candidate.Storage.Endpoint},
		{"storage.publicBaseUrl", previous.Storage.PublicBaseURL, candidate.Storage.PublicBaseURL},
		{"storage.region", previous.Storage.Region, candidate.Storage.Region},
		{"storage.bucket", previous.Storage.Bucket, candidate.Storage.Bucket},
		{"storage.accessKeyId", previous.Storage.AccessKeyID, candidate.Storage.AccessKeyID},
		{"storage.secretAccessKey", previous.Storage.SecretAccessKey, candidate.Storage.SecretAccessKey},
		{"storage.forcePathStyle", previous.Storage.ForcePathStyle, candidate.Storage.ForcePathStyle},
		{"storage.signedUrlTtlSeconds", previous.Storage.SignedURLTTLSeconds, candidate.Storage.SignedURLTTLSeconds},
		{"storage.maxUploadBytes", previous.Storage.MaxUploadBytes, candidate.Storage.MaxUploadBytes},
		{"media.ffmpegPath", previous.Media.FFmpegPath, candidate.Media.FFmpegPath},
		{"media.mode", previous.Media.Mode, candidate.Media.Mode},
		{"media.ffprobePath", previous.Media.FFprobePath, candidate.Media.FFprobePath},
		{"scraping.fpcalcPath", previous.Scraping.FPcalcPath, candidate.Scraping.FPcalcPath},
		{"scraping.acoustIdClient", previous.Scraping.AcoustIDClient, candidate.Scraping.AcoustIDClient},
		{"localLibrary.name", previous.LocalLibrary.Name, candidate.LocalLibrary.Name},
		{"localLibrary.directory", previous.LocalLibrary.Directory, candidate.LocalLibrary.Directory},
		{"localLibrary.mode", previous.LocalLibrary.Mode, candidate.LocalLibrary.Mode},
		{"localLibrary.enabled", previous.LocalLibrary.Enabled, candidate.LocalLibrary.Enabled},
		{"localLibrary.syncOnStartup", previous.LocalLibrary.SyncOnStartup, candidate.LocalLibrary.SyncOnStartup},
		{"localLibrary.scanIntervalMinutes", previous.LocalLibrary.ScanIntervalMinutes, candidate.LocalLibrary.ScanIntervalMinutes},
		{"localLibrary.includePatterns", previous.LocalLibrary.IncludePatterns, candidate.LocalLibrary.IncludePatterns},
		{"localLibrary.excludePatterns", previous.LocalLibrary.ExcludePatterns, candidate.LocalLibrary.ExcludePatterns},
		{"registration.enabled", previous.Registration.Enabled, candidate.Registration.Enabled},
		{"security.accessTokenTtlSeconds", previous.Security.AccessTokenTTLSeconds, candidate.Security.AccessTokenTTLSeconds},
		{"security.refreshTokenTtlSeconds", previous.Security.RefreshTokenTTLSeconds, candidate.Security.RefreshTokenTTLSeconds},
		{"http.ipv4Host", previous.HTTP.IPv4Host, candidate.HTTP.IPv4Host},
		{"http.ipv4Port", previous.HTTP.IPv4Port, candidate.HTTP.IPv4Port},
		{"http.ipv6Host", previous.HTTP.IPv6Host, candidate.HTTP.IPv6Host},
		{"http.ipv6Port", previous.HTTP.IPv6Port, candidate.HTTP.IPv6Port},
		{"http.trustedProxyAddresses", previous.HTTP.TrustedProxyAddresses, candidate.HTTP.TrustedProxyAddresses},
	}
	result := make([]string, 0)
	for _, pair := range pairs {
		if !reflect.DeepEqual(pair.left, pair.right) {
			result = append(result, pair.name)
		}
	}
	return result
}

func listenerHost(value string, ipv4 bool, field string) (string, error) {
	host, err := requiredText(strings.Trim(strings.TrimSpace(value), "[]"), 255, field)
	if err != nil {
		return "", err
	}
	address := net.ParseIP(host)
	if address == nil || ipv4 != (address.To4() != nil) {
		return "", validation(field + " is invalid")
	}
	return host, nil
}

func parseCandidate(environment map[string]string) (config.Config, error) {
	candidate, err := config.Parse(environment)
	if err != nil {
		return config.Config{}, validation(err.Error())
	}
	return candidate, nil
}

func nullableHTTPURL(value *string, field string) (string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "", nil
	}
	candidate := strings.TrimSpace(*value)
	parsed, err := url.Parse(candidate)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", validation(field + " is invalid")
	}
	if parsed.User != nil {
		return "", validation(field + " cannot contain credentials")
	}
	return strings.TrimRight(candidate, "/"), nil
}

func encodePatterns(values []string, field string) (string, error) {
	if len(values) > 100 {
		return "", validation(field + " is invalid")
	}
	for _, value := range values {
		if textLength(value) < 1 || textLength(value) > 500 {
			return "", validation(field + " is invalid")
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", field, err)
	}
	return string(encoded), nil
}

func uniqueTexts(values []string, maximumItems, minimum, maximum int, field string) ([]string, error) {
	if len(values) > maximumItems {
		return nil, validation(field + " is invalid")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if textLength(value) < minimum || textLength(value) > maximum {
			return nil, validation(field + " is invalid")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func requiredText(value string, maximum int, field string) (string, error) {
	result := strings.TrimSpace(value)
	if textLength(result) < 1 || textLength(result) > maximum {
		return "", validation(field + " is invalid")
	}
	return result, nil
}

func optionalText(value string, maximum int, field string) (string, error) {
	result := strings.TrimSpace(value)
	if textLength(result) > maximum {
		return "", validation(field + " is invalid")
	}
	return result, nil
}

func textLength(value string) int    { return len(utf16.Encode([]rune(value))) }
func validation(detail string) error { return apperror.Validation(detail) }
