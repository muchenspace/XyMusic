package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// TraceIDHeader is accepted from trusted callers when it contains a safe
	// opaque identifier and is returned on every response.
	TraceIDHeader = "X-Trace-Id"
	traceIDKey    = "http.trace_id"
)

var (
	safeTraceID     = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
	fallbackCounter atomic.Uint64
)

type requestTraceIDKey struct{}

// TraceIDGenerator supplies trace identifiers when a caller did not provide a
// valid one. Invalid generated values are ignored in favor of a UUID.
type TraceIDGenerator func() string

// TraceIDMiddleware establishes one trace identifier for the full request and
// places it in both the Gin and standard request contexts.
func TraceIDMiddleware(generator TraceIDGenerator) gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := strings.TrimSpace(c.GetHeader(TraceIDHeader))
		if !safeTraceID.MatchString(traceID) {
			traceID = ""
			if generator != nil {
				candidate := strings.TrimSpace(generator())
				if safeTraceID.MatchString(candidate) {
					traceID = candidate
				}
			}
			if traceID == "" {
				traceID = newTraceID()
			}
		}

		c.Set(traceIDKey, traceID)
		requestContext := context.WithValue(c.Request.Context(), requestTraceIDKey{}, traceID)
		c.Request = c.Request.WithContext(requestContext)
		c.Header(TraceIDHeader, traceID)
		c.Next()
	}
}

// TraceID returns the current Gin request trace identifier.
func TraceID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, exists := c.Get(traceIDKey)
	if !exists {
		return ""
	}
	traceID, _ := value.(string)
	return traceID
}

// TraceIDFromContext returns a trace identifier from a standard request
// context, allowing application services and outbound clients to correlate
// work without importing Gin.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(requestTraceIDKey{}).(string)
	return traceID
}

// SecurityHeaders adds conservative API response hardening headers.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Cross-Origin-Resource-Policy", "same-site")
		c.Next()
	}
}

// RequestMetricsMiddleware records the final response status and elapsed time
// after inner recovery and problem handlers have completed.
func RequestMetricsMiddleware(metrics RequestMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		if metrics == nil {
			c.Next()
			return
		}
		startedAt := time.Now()
		metrics.RequestStarted()
		defer func() {
			metrics.RequestFinished(c.Writer.Status(), time.Since(startedAt))
		}()
		c.Next()
	}
}

// PanicHandler receives recovered panic values for structured logging.
type PanicHandler func(*gin.Context, any)

// Recovery converts unhandled panics into a safe RFC 7807 response. If a
// handler already committed bytes, it only aborts the remaining chain because
// the response can no longer be replaced safely.
func Recovery(onPanic PanicHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				c.Abort()
				if onPanic != nil {
					onPanic(c, recovered)
				}
				if !c.Writer.Written() {
					WriteError(c, fmt.Errorf("panic recovered: %T", recovered))
				}
			}
		}()
		c.Next()
	}
}

func newTraceID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		value[6] = (value[6] & 0x0f) | 0x40
		value[8] = (value[8] & 0x3f) | 0x80
		encoded := hex.EncodeToString(value[:])
		return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
	}

	seed := fmt.Sprintf("%d-%d", time.Now().UnixNano(), fallbackCounter.Add(1))
	digest := sha256.Sum256([]byte(seed))
	encoded := hex.EncodeToString(digest[:16])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
