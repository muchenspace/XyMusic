// Package httpserver provides the standalone Gin HTTP transport used by the
// Go backend. It owns transport concerns only; application services remain
// independent from Gin and net/http response details.
package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/shared/apperror"
)

// ReadinessChecker verifies that dependencies required to serve application
// traffic are available.
type ReadinessChecker interface {
	Check(context.Context) error
}

// ReadinessReporter supplies a protocol-specific readiness response. It is
// used by the process-lifetime control plane, whose public contract includes
// runtime and worker state rather than only a dependency error.
type ReadinessReporter interface {
	Report(context.Context) (int, any)
}

// RequestMetrics records one completed observation for each request handled by
// this engine. The control forwarding engine intentionally does not share the
// managed runtime collector, so forwarded API requests are counted once.
type RequestMetrics interface {
	RequestStarted()
	RequestFinished(status int, duration time.Duration)
}

// ReadinessFunc adapts a function to ReadinessChecker.
type ReadinessFunc func(context.Context) error

// Check implements ReadinessChecker.
func (check ReadinessFunc) Check(ctx context.Context) error {
	if check == nil {
		return nil
	}
	return check(ctx)
}

// RouteRegistrar installs feature routes after infrastructure middleware and
// health endpoints have been configured.
type RouteRegistrar func(*gin.Engine)

// Options configures a standalone Gin engine.
type Options struct {
	CORS             CORSConfig
	RequestLimits    RequestLimits
	AllowedHosts     []string
	Readiness        ReadinessChecker
	ReadinessReport  ReadinessReporter
	TrustedProxies   []string
	TraceIDGenerator TraceIDGenerator
	PanicHandler     PanicHandler
	Metrics          RequestMetrics
	RegisterRoutes   RouteRegistrar
}

// NewEngine builds the production HTTP pipeline without starting a listener.
func NewEngine(options Options) (*gin.Engine, error) {
	limits, err := normalizeRequestLimits(options.RequestLimits)
	if err != nil {
		return nil, fmt.Errorf("configure request limits: %w", err)
	}
	limitMiddleware, err := RequestSizeLimiter(limits)
	if err != nil {
		return nil, fmt.Errorf("configure request limit middleware: %w", err)
	}
	corsMiddleware, err := CORS(options.CORS)
	if err != nil {
		return nil, fmt.Errorf("configure CORS: %w", err)
	}
	hostMiddleware, err := HostGuard(options.AllowedHosts)
	if err != nil {
		return nil, fmt.Errorf("configure HTTP host guard: %w", err)
	}

	engine := gin.New()
	engine.ContextWithFallback = true
	engine.HandleMethodNotAllowed = true
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.MaxMultipartMemory = limits.StructuredBytes
	if err := engine.SetTrustedProxies(options.TrustedProxies); err != nil {
		return nil, fmt.Errorf("configure trusted proxies: %w", err)
	}
	engine.Use(
		TraceIDMiddleware(options.TraceIDGenerator),
		RequestMetricsMiddleware(options.Metrics),
		SecurityHeaders(),
		hostMiddleware,
		corsMiddleware,
		limitMiddleware,
		ProblemHandler(),
		Recovery(options.PanicHandler),
	)

	registerHealthRoutes(engine, options.Readiness, options.ReadinessReport)
	if options.RegisterRoutes != nil {
		options.RegisterRoutes(engine)
	}
	registerFallbackHandlers(engine)
	return engine, nil
}

// New is a concise alias for NewEngine.
func New(options Options) (*gin.Engine, error) {
	return NewEngine(options)
}

func registerHealthRoutes(engine *gin.Engine, readiness ReadinessChecker, report ReadinessReporter) {
	engine.GET("/health/live", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	engine.GET("/health/ready", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if report != nil {
			status, body := report.Report(c.Request.Context())
			c.JSON(status, body)
			return
		}
		if readiness != nil {
			if err := readiness.Check(c.Request.Context()); err != nil {
				WriteError(c, apperror.DependencyUnavailable("必要的运行依赖暂不可用"))
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
}

func registerFallbackHandlers(engine *gin.Engine) {
	engine.NoRoute(func(c *gin.Context) {
		spec := applicationProblemSpecs[apperror.CodeResourceNotFound]
		writeProblem(c, newHTTPProblem(
			http.StatusNotFound,
			string(apperror.CodeResourceNotFound),
			spec.title,
			"请求的接口不存在",
			TraceID(c),
			requestInstance(c),
		))
	})
	engine.NoMethod(func(c *gin.Context) {
		if allowed := allowedMethods(engine, c.Request.URL.Path, c.Request.Method); len(allowed) > 0 {
			c.Header("Allow", strings.Join(allowed, ", "))
		}
		writeProblem(c, newHTTPProblem(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"请求方法不受支持",
			"该接口不支持当前请求方法",
			TraceID(c),
			requestInstance(c),
		))
	})
}

func allowedMethods(engine *gin.Engine, path, currentMethod string) []string {
	methods := make(map[string]struct{})
	for _, route := range engine.Routes() {
		if route.Method == currentMethod || !routePatternMatches(route.Path, path) {
			continue
		}
		methods[route.Method] = struct{}{}
	}
	result := make([]string, 0, len(methods))
	for method := range methods {
		result = append(result, method)
	}
	sort.Strings(result)
	return result
}

func routePatternMatches(pattern, path string) bool {
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	for index, patternPart := range patternParts {
		if strings.HasPrefix(patternPart, "*") {
			return index == len(patternParts)-1
		}
		if index >= len(pathParts) {
			return false
		}
		if strings.HasPrefix(patternPart, ":") {
			if pathParts[index] == "" {
				return false
			}
			continue
		}
		if patternPart != pathParts[index] {
			return false
		}
	}
	return len(patternParts) == len(pathParts)
}
