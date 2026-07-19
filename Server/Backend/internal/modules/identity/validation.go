package identity

import (
	"regexp"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/shared/apperror"
)

var (
	registrationUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)
	installationIDPattern       = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	idempotencyKeyPattern       = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
	bearerTokenPattern          = regexp.MustCompile(`(?i)^Bearer\s+(.+)$`)
)

func NormalizeUsername(value string) string {
	return strings.ToLower(norm.NFKC.String(trimSpace(value)))
}

func IsDevicePlatform(value DevicePlatform) bool {
	switch value {
	case DevicePlatformAndroid, DevicePlatformWindows, DevicePlatformWeb:
		return true
	default:
		return false
	}
}

func BearerToken(authorization string) (string, error) {
	match := bearerTokenPattern.FindStringSubmatch(authorization)
	if len(match) != 2 || match[1] == "" {
		return "", apperror.Unauthorized(
			apperror.CodeAuthenticationRequired,
			"Authentication is required",
		)
	}
	return match[1], nil
}

func validateRegistration(username, password string) error {
	if !registrationUsernamePattern.MatchString(username) {
		return apperror.Validation("username must match ^[A-Za-z0-9_]{3,32}$")
	}
	return validatePassword(password, 8)
}

func validateLogin(input LoginInput) error {
	usernameLength := javascriptStringLength(input.Username)
	if usernameLength < 3 || usernameLength > 32 {
		return apperror.Validation("username must contain 3 to 32 characters")
	}
	if err := validatePassword(input.Password, 1); err != nil {
		return err
	}
	return validateDevice(input.Device)
}

func validatePassword(password string, minimum int) error {
	length := javascriptStringLength(password)
	if length < minimum || length > 128 {
		if minimum == 8 {
			return apperror.Validation("password must contain 8 to 128 characters")
		}
		return apperror.Validation("password must contain 1 to 128 characters")
	}
	return nil
}

func validateDevice(device DeviceInfoInput) error {
	if !installationIDPattern.MatchString(device.InstallationID) {
		return apperror.Validation("device.installationId must be a UUID")
	}
	if !IsDevicePlatform(device.Platform) {
		return apperror.Validation("device.platform must be ANDROID, WINDOWS or WEB")
	}
	nameLength := javascriptStringLength(trimSpace(device.Name))
	if nameLength < 1 || nameLength > 100 {
		return apperror.Validation("device.name is invalid")
	}
	appVersionLength := javascriptStringLength(trimSpace(device.AppVersion))
	if appVersionLength < 1 || appVersionLength > 40 {
		return apperror.Validation("device.appVersion is invalid")
	}
	return nil
}

func validateRefreshInput(refreshToken, idempotencyKey string) error {
	length := javascriptStringLength(refreshToken)
	if length < 32 || length > 4096 {
		return apperror.Validation("refreshToken is invalid")
	}
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	return nil
}

func requireActiveUser(status UserStatus) error {
	if status == UserStatusSuspended {
		return apperror.AccountState(
			apperror.CodeAccountSuspended,
			"Account is suspended",
		)
	}
	if status != UserStatusActive {
		return sessionRevoked("Account is unavailable")
	}
	return nil
}

func duplicateUsernameError() error {
	return apperror.Conflict(
		apperror.CodeDuplicateUsername,
		"Username is already registered",
		nil,
	)
}

func sessionRevoked(detail string) error {
	return apperror.Unauthorized(apperror.CodeSessionRevoked, detail)
}

func javascriptStringLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func trimSpace(value string) string {
	return strings.TrimSpace(value)
}
