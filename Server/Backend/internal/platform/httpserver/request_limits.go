package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/shared/apperror"
)

const (
	// MaxStructuredRequestBodyBytes limits JSON, forms, and other ordinary API
	// request bodies to 2 MiB.
	MaxStructuredRequestBodyBytes int64 = 2 * 1024 * 1024
	// MaxMediaUploadRequestBodyBytes is the hard server ceiling for the
	// designated streaming media content endpoint.
	MaxMediaUploadRequestBodyBytes int64 = 1024 * 1024 * 1024
)

var mediaContentUploadPath = regexp.MustCompile(
	`(?i)^/api/v1/admin/media/uploads/[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}/content$`,
)

var objectStorageProxyUploadPath = regexp.MustCompile(
	`^/api/v1/oss/[A-Za-z0-9_-]+/.+$`,
)

// MediaUploadMatcher identifies endpoints allowed to stream up to the media
// upload ceiling. Keeping this explicit prevents arbitrary routes from opting
// into a 1 GiB request body merely by changing Content-Type.
type MediaUploadMatcher func(*http.Request) bool

// RequestLimits configures the body-size middleware. Zero byte limits use the
// production defaults.
type RequestLimits struct {
	StructuredBytes    int64
	MediaUploadBytes   int64
	MediaUploadMatcher MediaUploadMatcher
}

// DefaultRequestLimits returns the production server ceilings.
func DefaultRequestLimits() RequestLimits {
	return RequestLimits{
		StructuredBytes:    MaxStructuredRequestBodyBytes,
		MediaUploadBytes:   MaxMediaUploadRequestBodyBytes,
		MediaUploadMatcher: IsMediaContentUpload,
	}
}

// IsMediaContentUpload matches PUT requests that stream media either through
// the UUID-scoped application endpoint or the pre-signed object proxy.
func IsMediaContentUpload(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPut || request.URL == nil {
		return false
	}
	return mediaContentUploadPath.MatchString(request.URL.Path) || objectStorageProxyUploadPath.MatchString(request.URL.Path)
}

// RequestSizeLimiter rejects declared oversize bodies before routing and wraps
// streaming bodies so chunked requests cannot bypass the same limit.
func RequestSizeLimiter(input RequestLimits) (gin.HandlerFunc, error) {
	limits, err := normalizeRequestLimits(input)
	if err != nil {
		return nil, err
	}
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}

		maximumBytes := limits.StructuredBytes
		mediaUpload := limits.MediaUploadMatcher(c.Request)
		if mediaUpload {
			maximumBytes = limits.MediaUploadBytes
		}
		if err := enforceContentLength(c.Request, maximumBytes); err != nil {
			if c.Request.Body != nil {
				_ = c.Request.Body.Close()
			}
			WriteError(c, err)
			return
		}
		if c.Request.Body != nil && c.Request.Body != http.NoBody {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumBytes)
		}
		c.Next()
	}, nil
}

// DecodeJSON decodes exactly one JSON document and translates syntax or body
// limit failures into application errors suitable for WriteError.
func DecodeJSON(c *gin.Context, destination any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return apperror.Validation("请求内容不能为空")
	}
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(destination); err != nil {
		return normalizeBodyError(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return apperror.Validation("请求内容只能包含一个 JSON 文档")
		}
		return normalizeBodyError(err)
	}
	return nil
}

func normalizeRequestLimits(input RequestLimits) (RequestLimits, error) {
	defaults := DefaultRequestLimits()
	if input.StructuredBytes == 0 {
		input.StructuredBytes = defaults.StructuredBytes
	}
	if input.MediaUploadBytes == 0 {
		input.MediaUploadBytes = defaults.MediaUploadBytes
	}
	if input.MediaUploadMatcher == nil {
		input.MediaUploadMatcher = defaults.MediaUploadMatcher
	}
	if input.StructuredBytes < 1 {
		return RequestLimits{}, fmt.Errorf("structured request limit must be positive")
	}
	if input.MediaUploadBytes < input.StructuredBytes {
		return RequestLimits{}, fmt.Errorf("media upload limit cannot be smaller than structured request limit")
	}
	return input, nil
}

func enforceContentLength(request *http.Request, maximumBytes int64) error {
	values := request.Header.Values("Content-Length")
	if len(values) > 1 {
		return apperror.Validation("Content-Length 请求头无效")
	}
	if len(values) == 1 {
		raw := values[0]
		if raw == "" || strings.IndexFunc(raw, func(character rune) bool {
			return character < '0' || character > '9'
		}) >= 0 {
			return apperror.Validation("Content-Length 请求头无效")
		}
		declared, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || declared > uint64(maximumBytes) {
			return apperror.PayloadTooLarge(bodyLimitDetail(maximumBytes))
		}
		if request.ContentLength >= 0 && uint64(request.ContentLength) != declared {
			return apperror.Validation("Content-Length 与请求体长度不一致")
		}
		return nil
	}
	if request.ContentLength > maximumBytes {
		return apperror.PayloadTooLarge(bodyLimitDetail(maximumBytes))
	}
	return nil
}

func normalizeBodyError(err error) error {
	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		return apperror.PayloadTooLarge(bodyLimitDetail(maximumBytesError.Limit))
	}
	return apperror.Validation("请求内容无法解析")
}

func bodyLimitDetail(maximumBytes int64) string {
	switch maximumBytes {
	case MaxStructuredRequestBodyBytes:
		return "请求内容超过 2 MiB，请缩小后重试"
	case MaxMediaUploadRequestBodyBytes:
		return "上传内容超过 1 GiB，请缩小后重试"
	default:
		return fmt.Sprintf("请求内容超过允许的 %d 字节", maximumBytes)
	}
}
