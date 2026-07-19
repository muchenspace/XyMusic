package adminmetadata

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"xymusic/server/internal/shared/apperror"
)

type WritebackError struct {
	Code    string
	Message string
	Cause   error
}

func NewWritebackError(code, message string) *WritebackError {
	return &WritebackError{Code: code, Message: message}
}

func wrapWritebackError(code, message string, cause error) *WritebackError {
	return &WritebackError{Code: code, Message: message, Cause: cause}
}

func (failure *WritebackError) Error() string {
	if failure == nil {
		return "<nil>"
	}
	if failure.Message == "" {
		return failure.Code
	}
	return failure.Message
}

func (failure *WritebackError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func writebackErrorCode(err error) string {
	var writeback *WritebackError
	if errors.As(err, &writeback) && writeback.Code != "" {
		return truncateASCII(writeback.Code, 100)
	}
	if application, ok := apperror.As(err); ok {
		return string(application.Code)
	}
	type coded interface{ ErrorCode() string }
	var value coded
	if errors.As(err, &value) && value.ErrorCode() != "" {
		return truncateASCII(value.ErrorCode(), 100)
	}
	return "WRITEBACK_FAILED"
}

func safeWritebackError(err error) string {
	if err == nil {
		return "Unknown metadata writeback failure"
	}
	message := strings.TrimSpace(lineBreakPattern.ReplaceAllString(err.Error(), " "))
	if message == "" {
		return "Unknown metadata writeback failure"
	}
	if len(message) > 4_000 {
		message = message[:4_000]
	}
	return message
}

func filesystemWritebackError(err error, message string) error {
	code := "UNKNOWN"
	if err != nil {
		code = filesystemCode(err)
	}
	return wrapWritebackError(
		truncateASCII("FILESYSTEM_"+code, 100),
		fmt.Sprintf("%s: %s", message, safeWritebackError(err)),
		err,
	)
}

func filesystemCode(err error) string {
	text := strings.ToUpper(err.Error())
	for _, code := range []string{
		"ENOENT", "EACCES", "EPERM", "EEXIST", "EINVAL", "EISDIR", "ENOSPC", "EROFS",
	} {
		if strings.Contains(text, code) {
			return code
		}
	}
	return "UNKNOWN"
}

func truncateASCII(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

var lineBreakPattern = regexp.MustCompile(`[\r\n]+`)
