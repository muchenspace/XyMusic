// Package apperror defines application-level failures shared by services and
// delivery adapters. It intentionally has no dependency on HTTP or Gin.
package apperror

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable application error identifier.
type Code string

const (
	CodeValidationError               Code = "VALIDATION_ERROR"
	CodeInvalidCursor                 Code = "INVALID_CURSOR"
	CodeAuthenticationRequired        Code = "AUTHENTICATION_REQUIRED"
	CodeAccessTokenExpired            Code = "ACCESS_TOKEN_EXPIRED"
	CodeSessionRevoked                Code = "SESSION_REVOKED"
	CodeInvalidCredentials            Code = "INVALID_CREDENTIALS"
	CodeAccountSuspended              Code = "ACCOUNT_SUSPENDED"
	CodeForbidden                     Code = "FORBIDDEN"
	CodeResourceNotFound              Code = "RESOURCE_NOT_FOUND"
	CodeDuplicateUsername             Code = "DUPLICATE_USERNAME"
	CodeIdempotencyKeyReused          Code = "IDEMPOTENCY_KEY_REUSED"
	CodeVersionConflict               Code = "VERSION_CONFLICT"
	CodeResourceConflict              Code = "RESOURCE_CONFLICT"
	CodeInvalidStateTransition        Code = "INVALID_STATE_TRANSITION"
	CodeTrackNotPlayable              Code = "TRACK_NOT_PLAYABLE"
	CodeTrackAlreadyInPlaylist        Code = "TRACK_ALREADY_IN_PLAYLIST"
	CodeSourceFileDeleteFailed        Code = "SOURCE_FILE_DELETE_FAILED"
	CodeSourceFileRestoreFailed       Code = "SOURCE_FILE_RESTORE_FAILED"
	CodeMediaUploadMismatch           Code = "MEDIA_UPLOAD_MISMATCH"
	CodePayloadTooLarge               Code = "PAYLOAD_TOO_LARGE"
	CodeRateLimited                   Code = "RATE_LIMITED"
	CodeDependencyUnavailable         Code = "DEPENDENCY_UNAVAILABLE"
	CodeDatabaseHostUnresolved        Code = "DATABASE_HOST_UNRESOLVED"
	CodeDatabaseEndpointUnreachable   Code = "DATABASE_ENDPOINT_UNREACHABLE"
	CodeDatabaseConnectionTimeout     Code = "DATABASE_CONNECTION_TIMEOUT"
	CodeDatabaseNotFound              Code = "DATABASE_NOT_FOUND"
	CodeDatabaseAuthenticationFailed  Code = "DATABASE_AUTHENTICATION_FAILED"
	CodeDatabaseTLSFailed             Code = "DATABASE_TLS_FAILED"
	CodeDatabasePermissionDenied      Code = "DATABASE_PERMISSION_DENIED"
	CodeDatabaseConnectionLimit       Code = "DATABASE_CONNECTION_LIMIT"
	CodeDatabaseConnectionFailed      Code = "DATABASE_CONNECTION_FAILED"
	CodeDatabaseMigrationFailed       Code = "DATABASE_MIGRATION_FAILED"
	CodeDatabaseMigrationIncompatible Code = "DATABASE_MIGRATION_INCOMPATIBLE"
	CodeSetupDecisionRequired         Code = "SETUP_DECISION_REQUIRED"
	CodeSetupFailed                   Code = "SETUP_FAILED"
	CodeInternalError                 Code = "INTERNAL_ERROR"
)

// Error is a transport-neutral application failure. Detail is safe to show to
// an API consumer; implementation diagnostics belong in Cause instead.
type Error struct {
	Code     Code
	Detail   string
	Metadata map[string]any
	cause    error
}

// Option customizes an application error.
type Option func(*Error)

// New constructs an application error. Metadata is always initialized so
// callers can inspect it without nil checks.
func New(code Code, detail string, options ...Option) *Error {
	err := &Error{
		Code:     code,
		Detail:   detail,
		Metadata: make(map[string]any),
	}
	for _, option := range options {
		if option != nil {
			option(err)
		}
	}
	return err
}

// WithCause attaches a diagnostic cause without exposing it as public detail.
func WithCause(cause error) Option {
	return func(target *Error) {
		target.cause = cause
	}
}

// WithMetadata copies extension metadata onto an error.
func WithMetadata(metadata map[string]any) Option {
	return func(target *Error) {
		for key, value := range metadata {
			target.Metadata[key] = value
		}
	}
}

// Error implements the standard error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Detail == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

// Unwrap exposes the diagnostic cause to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// As returns the first application error in an error chain.
func As(err error) (*Error, bool) {
	var target *Error
	if !errors.As(err, &target) {
		return nil, false
	}
	return target, true
}

// IsCode reports whether an error chain contains the requested application
// error code.
func IsCode(err error, code Code) bool {
	target, ok := As(err)
	return ok && target.Code == code
}

// Validation creates a request validation failure. Field errors are copied so
// callers can safely reuse their input map.
func Validation(detail string, fieldErrors ...map[string][]string) *Error {
	metadata := make(map[string]any)
	if len(fieldErrors) > 0 && len(fieldErrors[0]) > 0 {
		copied := make(map[string][]string, len(fieldErrors[0]))
		for field, messages := range fieldErrors[0] {
			copied[field] = append([]string(nil), messages...)
		}
		metadata["fieldErrors"] = copied
	}
	return New(CodeValidationError, detail, WithMetadata(metadata))
}

// InvalidCursor creates an invalid pagination cursor failure.
func InvalidCursor(detail string) *Error {
	return New(CodeInvalidCursor, detail)
}

// Unauthorized creates an authentication failure with a specific auth code.
func Unauthorized(code Code, detail string) *Error {
	return New(code, detail)
}

// Forbidden creates an authorization failure.
func Forbidden(detail string) *Error {
	return New(CodeForbidden, detail)
}

// AccountState creates an account-state authorization failure.
func AccountState(code Code, detail string) *Error {
	return New(code, detail)
}

// NotFound creates a missing-resource failure.
func NotFound(detail string) *Error {
	return New(CodeResourceNotFound, detail)
}

// Conflict creates a state conflict failure.
func Conflict(code Code, detail string, metadata map[string]any) *Error {
	return New(code, detail, WithMetadata(metadata))
}

// Unprocessable creates a domain validation failure for a well-formed request.
func Unprocessable(code Code, detail string, metadata map[string]any) *Error {
	return New(code, detail, WithMetadata(metadata))
}

// PayloadTooLarge creates a request size failure.
func PayloadTooLarge(detail string) *Error {
	return New(CodePayloadTooLarge, detail)
}

// RateLimited creates a throttling failure.
func RateLimited(retryAfterSeconds int) *Error {
	return New(
		CodeRateLimited,
		"请求过于频繁，请稍后重试",
		WithMetadata(map[string]any{"retryAfterSeconds": retryAfterSeconds}),
	)
}

// DependencyUnavailable creates a dependency health failure.
func DependencyUnavailable(detail string) *Error {
	return New(CodeDependencyUnavailable, detail)
}

// Internal creates an explicitly classified internal failure.
func Internal(detail string, cause error) *Error {
	return New(CodeInternalError, detail, WithCause(cause))
}
