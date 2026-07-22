package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/shared/apperror"
)

const generatedTestTraceID = "trace-test-0001"

func TestMain(main *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(main.Run())
}

func TestHealthEndpointsAndResponseHardening(t *testing.T) {
	var readinessTraceID string
	engine := mustEngine(t, Options{
		TraceIDGenerator: func() string { return generatedTestTraceID },
		Readiness: ReadinessFunc(func(ctx context.Context) error {
			readinessTraceID = TraceIDFromContext(ctx)
			return nil
		}),
	})

	ready := execute(engine, request(t, http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("ready status = %d, body = %s", ready.Code, ready.Body.String())
	}
	assertJSONField(t, ready, "status", "ready")
	if readinessTraceID != generatedTestTraceID {
		t.Fatalf("readiness trace ID = %q", readinessTraceID)
	}
	assertHeader(t, ready, TraceIDHeader, generatedTestTraceID)
	assertHeader(t, ready, "Cache-Control", "no-store")
	assertHeader(t, ready, "X-Content-Type-Options", "nosniff")
	assertHeader(t, ready, "X-Frame-Options", "DENY")
	assertHeader(t, ready, "Referrer-Policy", "no-referrer")
	assertHeader(t, ready, "Cross-Origin-Resource-Policy", "cross-origin")
	if got := ready.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("unexpected Content-Security-Policy: %q", got)
	}

	live := execute(engine, request(t, http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("live status = %d, body = %s", live.Code, live.Body.String())
	}
	assertJSONField(t, live, "status", "ok")
}

func TestReadinessFailureReturnsSafeProblem(t *testing.T) {
	engine := mustEngine(t, Options{
		Readiness: ReadinessFunc(func(context.Context) error {
			return errors.New("postgres password=do-not-leak")
		}),
	})
	req := request(t, http.MethodGet, "/health/ready", nil)
	req.Header.Set(TraceIDHeader, "client.trace-123")
	response := execute(engine, req)

	assertProblem(t, response, http.StatusServiceUnavailable, string(apperror.CodeDependencyUnavailable))
	assertHeader(t, response, TraceIDHeader, "client.trace-123")
	if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "postgres") {
		t.Fatalf("readiness diagnostics leaked: %s", response.Body.String())
	}
}

func TestTraceIDAcceptsOnlySafeCallerValues(t *testing.T) {
	engine := mustEngine(t, Options{TraceIDGenerator: func() string { return generatedTestTraceID }})

	valid := request(t, http.MethodGet, "/health/live", nil)
	valid.Header.Set(TraceIDHeader, "caller.trace:1234")
	validResponse := execute(engine, valid)
	assertHeader(t, validResponse, TraceIDHeader, "caller.trace:1234")

	invalid := request(t, http.MethodGet, "/health/live", nil)
	invalid.Header.Set(TraceIDHeader, "bad\r\ntrace")
	invalidResponse := execute(engine, invalid)
	assertHeader(t, invalidResponse, TraceIDHeader, generatedTestTraceID)
}

func TestCORSActualAndPreflightRequests(t *testing.T) {
	engine := mustEngine(t, Options{
		CORS: CORSConfig{AllowedOrigins: []string{"https://client.example"}},
		RegisterRoutes: func(engine *gin.Engine) {
			engine.POST("/api/resource", func(c *gin.Context) { c.Status(http.StatusCreated) })
		},
	})

	actual := request(t, http.MethodGet, "/health/live", nil)
	actual.Header.Set("Origin", "https://client.example")
	actualResponse := execute(engine, actual)
	assertHeader(t, actualResponse, "Access-Control-Allow-Origin", "https://client.example")
	if exposed := actualResponse.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(exposed, TraceIDHeader) {
		t.Fatalf("trace header is not exposed: %q", exposed)
	}
	if vary := strings.Join(actualResponse.Header().Values("Vary"), ","); !strings.Contains(vary, "Origin") {
		t.Fatalf("Origin is missing from Vary: %q", vary)
	}

	preflight := request(t, http.MethodOptions, "/api/resource", nil)
	preflight.Header.Set("Origin", "https://client.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "Authorization, X-Trace-Id")
	preflightResponse := execute(engine, preflight)
	if preflightResponse.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, body = %s", preflightResponse.Code, preflightResponse.Body.String())
	}
	assertHeader(t, preflightResponse, "Access-Control-Allow-Origin", "https://client.example")
	if allowed := preflightResponse.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(allowed, http.MethodPost) {
		t.Fatalf("POST is missing from allowed methods: %q", allowed)
	}
	if allowed := strings.ToLower(preflightResponse.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(allowed, "authorization") {
		t.Fatalf("Authorization is missing from allowed headers: %q", allowed)
	}
}

func TestCORSWildcardAllowsAnyOriginAndRequestHeader(t *testing.T) {
	engine := mustEngine(t, Options{
		CORS: CORSConfig{
			AllowAllOrigins: true,
			AllowedHeaders:  []string{"*"},
			ExposedHeaders:  []string{"*"},
		},
		RegisterRoutes: func(engine *gin.Engine) {
			engine.POST("/api/resource", func(c *gin.Context) { c.Status(http.StatusCreated) })
		},
	})

	preflight := request(t, http.MethodOptions, "/api/resource", nil)
	preflight.Header.Set("Origin", "https://public-client.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "Authorization, X-Public-Client")
	response := execute(engine, preflight)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, body = %s", response.Code, response.Body.String())
	}
	assertHeader(t, response, "Access-Control-Allow-Origin", "*")
	if allowed := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(allowed, "x-public-client") {
		t.Fatalf("custom request header is not allowed: %q", allowed)
	}
}

func TestCORSUnrestrictedCredentialsReflectAnyValidOrigin(t *testing.T) {
	engine := mustEngine(t, Options{
		CORS: UnrestrictedCORSConfig(),
		RegisterRoutes: func(engine *gin.Engine) {
			engine.POST("/api/resource", func(c *gin.Context) { c.Status(http.StatusCreated) })
		},
	})

	for _, origin := range []string{"https://admin.example", "http://192.0.2.10:4173", "tauri://localhost"} {
		preflight := request(t, http.MethodOptions, "/api/resource", nil)
		preflight.Header.Set("Origin", origin)
		preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
		preflight.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Custom-Client")
		response := execute(engine, preflight)

		if response.Code != http.StatusNoContent {
			t.Fatalf("origin %q preflight status = %d, body = %s", origin, response.Code, response.Body.String())
		}
		assertHeader(t, response, "Access-Control-Allow-Origin", origin)
		assertHeader(t, response, "Access-Control-Allow-Credentials", "true")
		if vary := strings.Join(response.Header().Values("Vary"), ","); !strings.Contains(vary, "Origin") {
			t.Fatalf("origin %q is missing Origin from Vary: %q", origin, vary)
		}
		if allowed := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(allowed, "x-custom-client") {
			t.Fatalf("origin %q custom header is not allowed: %q", origin, allowed)
		}
	}

	invalid := request(t, http.MethodOptions, "/api/resource", nil)
	invalid.Header.Set("Origin", "https://invalid.example/path")
	invalid.Header.Set("Access-Control-Request-Method", http.MethodPost)
	invalidResponse := execute(engine, invalid)
	assertProblem(t, invalidResponse, http.StatusForbidden, "CORS_ORIGIN_FORBIDDEN")
}

func TestCORSRejectsUntrustedOrUnsupportedPreflight(t *testing.T) {
	engine := mustEngine(t, Options{
		CORS: CORSConfig{AllowedOrigins: []string{"https://client.example"}},
		RegisterRoutes: func(engine *gin.Engine) {
			engine.POST("/api/resource", func(c *gin.Context) { c.Status(http.StatusCreated) })
		},
	})

	untrusted := request(t, http.MethodOptions, "/api/resource", nil)
	untrusted.Header.Set("Origin", "https://attacker.example")
	untrusted.Header.Set("Access-Control-Request-Method", http.MethodPost)
	untrustedResponse := execute(engine, untrusted)
	assertProblem(t, untrustedResponse, http.StatusForbidden, "CORS_ORIGIN_FORBIDDEN")
	if origin := untrustedResponse.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("untrusted origin was reflected: %q", origin)
	}

	unsupported := request(t, http.MethodOptions, "/api/resource", nil)
	unsupported.Header.Set("Origin", "https://client.example")
	unsupported.Header.Set("Access-Control-Request-Method", http.MethodPost)
	unsupported.Header.Set("Access-Control-Request-Headers", "X-Not-Allowed")
	unsupportedResponse := execute(engine, unsupported)
	assertProblem(t, unsupportedResponse, http.StatusForbidden, "CORS_PREFLIGHT_FORBIDDEN")
	assertHeader(t, unsupportedResponse, "Access-Control-Allow-Origin", "https://client.example")
}

func TestHostGuardAcceptsLoopbackFormsAndRejectsRebindingHosts(t *testing.T) {
	engine := mustEngine(t, Options{AllowedHosts: []string{"localhost", "127.0.0.1", "::1"}})

	for _, host := range []string{
		"localhost",
		"LOCALHOST:3000",
		"localhost.:3000",
		"127.0.0.1:3000",
		"[::1]",
		"[::1]:3000",
	} {
		req := request(t, http.MethodGet, "/health/live", nil)
		req.Host = host
		response := execute(engine, req)
		if response.Code != http.StatusOK {
			t.Fatalf("host %q status = %d, body = %s", host, response.Code, response.Body.String())
		}
	}

	for _, host := range []string{
		"attacker.example",
		"attacker.example:3000",
		"127.0.0.2:3000",
		"localhost:invalid",
		"[::1",
	} {
		req := request(t, http.MethodGet, "/health/live", nil)
		req.Host = host
		response := execute(engine, req)
		assertProblem(t, response, http.StatusMisdirectedRequest, "HOST_NOT_ALLOWED")
	}
}

func TestEmptyHostAllowListAcceptsPublicHosts(t *testing.T) {
	engine := mustEngine(t, Options{})
	for _, host := range []string{"203.0.113.10:3000", "music.example.com:3000"} {
		req := request(t, http.MethodGet, "/health/live", nil)
		req.Host = host
		response := execute(engine, req)
		if response.Code != http.StatusOK {
			t.Fatalf("host %q status = %d, body = %s", host, response.Code, response.Body.String())
		}
	}
}

func TestHostGuardRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewEngine(Options{AllowedHosts: []string{"localhost:3000"}}); err == nil ||
		!strings.Contains(err.Error(), "configure HTTP host guard") {
		t.Fatalf("NewEngine() error = %v", err)
	}
}

func TestDeclaredRequestBodyLimits(t *testing.T) {
	var mediaHandlerCalls int
	engine := mustEngine(t, Options{
		RegisterRoutes: func(engine *gin.Engine) {
			engine.POST("/structured", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			engine.PUT("/api/v1/admin/media/uploads/:uploadID/content", func(c *gin.Context) {
				mediaHandlerCalls++
				c.Status(http.StatusNoContent)
			})
		},
	})

	structured := request(t, http.MethodPost, "/structured", nil)
	structured.ContentLength = MaxStructuredRequestBodyBytes + 1
	structuredResponse := execute(engine, structured)
	assertProblem(t, structuredResponse, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge))

	mediaPath := "/api/v1/admin/media/uploads/01234567-89ab-cdef-0123-456789abcdef/content"
	allowedMedia := request(t, http.MethodPut, mediaPath, nil)
	allowedMedia.ContentLength = MaxStructuredRequestBodyBytes + 1
	allowedResponse := execute(engine, allowedMedia)
	if allowedResponse.Code != http.StatusNoContent {
		t.Fatalf("media request within 1 GiB status = %d, body = %s", allowedResponse.Code, allowedResponse.Body.String())
	}
	if mediaHandlerCalls != 1 {
		t.Fatalf("media handler calls = %d", mediaHandlerCalls)
	}

	oversizeMedia := request(t, http.MethodPut, mediaPath, nil)
	oversizeMedia.ContentLength = MaxMediaUploadRequestBodyBytes + 1
	oversizeResponse := execute(engine, oversizeMedia)
	assertProblem(t, oversizeResponse, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge))
	if mediaHandlerCalls != 1 {
		t.Fatalf("oversize request reached media handler; calls = %d", mediaHandlerCalls)
	}

	lookalike := request(t, http.MethodPut, "/api/v1/admin/media/uploads/not-a-uuid/content", nil)
	lookalike.ContentLength = MaxStructuredRequestBodyBytes + 1
	lookalikeResponse := execute(engine, lookalike)
	assertProblem(t, lookalikeResponse, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge))
}

func TestChunkedRequestCannotBypassBodyLimit(t *testing.T) {
	engine := mustEngine(t, Options{
		RequestLimits: RequestLimits{StructuredBytes: 8, MediaUploadBytes: 16},
		RegisterRoutes: func(engine *gin.Engine) {
			engine.POST("/consume", Handle(func(c *gin.Context) error {
				_, err := io.ReadAll(c.Request.Body)
				if err != nil {
					return err
				}
				c.Status(http.StatusNoContent)
				return nil
			}))
		},
	})

	oversize := request(t, http.MethodPost, "/consume", strings.NewReader("123456789"))
	oversize.ContentLength = -1
	oversize.Header.Del("Content-Length")
	oversizeResponse := execute(engine, oversize)
	assertProblem(t, oversizeResponse, http.StatusRequestEntityTooLarge, string(apperror.CodePayloadTooLarge))

	exact := request(t, http.MethodPost, "/consume", strings.NewReader("12345678"))
	exact.ContentLength = -1
	exact.Header.Del("Content-Length")
	exactResponse := execute(engine, exact)
	if exactResponse.Code != http.StatusNoContent {
		t.Fatalf("exact-size request status = %d, body = %s", exactResponse.Code, exactResponse.Body.String())
	}
}

func TestNotFoundAndMethodNotAllowedUseProblems(t *testing.T) {
	engine := mustEngine(t, Options{
		RegisterRoutes: func(engine *gin.Engine) {
			engine.GET("/items/:itemID", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			engine.POST("/items/:itemID", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		},
	})

	notFound := execute(engine, request(t, http.MethodGet, "/missing", nil))
	assertProblem(t, notFound, http.StatusNotFound, string(apperror.CodeResourceNotFound))
	trailingSlash := execute(engine, request(t, http.MethodGet, "/items/42/", nil))
	assertProblem(t, trailingSlash, http.StatusNotFound, string(apperror.CodeResourceNotFound))
	assertHeader(t, trailingSlash, "X-Content-Type-Options", "nosniff")

	methodNotAllowed := execute(engine, request(t, http.MethodDelete, "/items/42", nil))
	assertProblem(t, methodNotAllowed, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	assertHeader(t, methodNotAllowed, "Allow", "GET, POST")
}

func TestApplicationErrorsContextErrorsAndPanicsUseProblemPipeline(t *testing.T) {
	engine := mustEngine(t, Options{
		RegisterRoutes: func(engine *gin.Engine) {
			engine.GET("/domain-error", Handle(func(*gin.Context) error {
				return fmt.Errorf("lookup failed: %w", apperror.NotFound("指定曲目不存在"))
			}))
			engine.GET("/context-error", func(c *gin.Context) {
				_ = c.Error(apperror.Forbidden("当前用户不能执行此操作"))
				c.Abort()
			})
			engine.GET("/rate-limited", Handle(func(*gin.Context) error {
				return apperror.RateLimited(17)
			}))
			engine.GET("/panic", func(*gin.Context) { panic("sensitive panic value") })
		},
	})

	domain := execute(engine, request(t, http.MethodGet, "/domain-error", nil))
	assertProblem(t, domain, http.StatusNotFound, string(apperror.CodeResourceNotFound))
	assertJSONField(t, domain, "detail", "指定曲目不存在")

	contextError := execute(engine, request(t, http.MethodGet, "/context-error", nil))
	assertProblem(t, contextError, http.StatusForbidden, string(apperror.CodeForbidden))

	rateLimited := execute(engine, request(t, http.MethodGet, "/rate-limited", nil))
	assertProblem(t, rateLimited, http.StatusTooManyRequests, string(apperror.CodeRateLimited))
	assertHeader(t, rateLimited, "Retry-After", "17")
	assertJSONField(t, rateLimited, "retryAfterSeconds", float64(17))

	panicResponse := execute(engine, request(t, http.MethodGet, "/panic", nil))
	assertProblem(t, panicResponse, http.StatusInternalServerError, string(apperror.CodeInternalError))
	if strings.Contains(panicResponse.Body.String(), "sensitive") {
		t.Fatalf("panic value leaked: %s", panicResponse.Body.String())
	}
}

func TestHeadProblemHasNoResponseBody(t *testing.T) {
	engine := mustEngine(t, Options{})
	response := execute(engine, request(t, http.MethodHead, "/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("HEAD status = %d", response.Code)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("HEAD problem wrote a body: %q", response.Body.String())
	}
	assertHeader(t, response, "Content-Type", ProblemMediaType)
}

func TestRequestMetricsRecordsEachHandledRequestOnce(t *testing.T) {
	metrics := &requestMetricsRecorder{}
	engine := mustEngine(t, Options{
		Metrics: metrics,
		RegisterRoutes: func(engine *gin.Engine) {
			engine.GET("/measured", func(c *gin.Context) {
				c.Status(http.StatusCreated)
			})
		},
	})
	response := execute(engine, request(t, http.MethodGet, "/measured", nil))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d", response.Code)
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if metrics.started != 1 || metrics.finished != 1 || metrics.status != http.StatusCreated || metrics.duration < 0 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestRequestMetricsCoverPanicAndRequestLimitFailures(t *testing.T) {
	metrics := &requestMetricsRecorder{}
	engine := mustEngine(t, Options{
		Metrics: metrics,
		RegisterRoutes: func(engine *gin.Engine) {
			engine.GET("/panic-metric", func(*gin.Context) { panic("metric panic") })
		},
	})
	panicResponse := execute(engine, request(t, http.MethodGet, "/panic-metric", nil))
	if panicResponse.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d", panicResponse.Code)
	}
	oversizedRequest := request(
		t,
		http.MethodPost,
		"/panic-metric",
		strings.NewReader(strings.Repeat("x", int(MaxStructuredRequestBodyBytes)+1)),
	)
	oversizedRequest.Header.Set("Content-Type", "application/json")
	oversizedResponse := execute(engine, oversizedRequest)
	if oversizedResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d", oversizedResponse.Code)
	}
	missingResponse := execute(engine, request(t, http.MethodGet, "/missing-metric", nil))
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", missingResponse.Code)
	}
	methodResponse := execute(engine, request(t, http.MethodPost, "/panic-metric", nil))
	if methodResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d", methodResponse.Code)
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	wantStatuses := []int{
		http.StatusInternalServerError,
		http.StatusRequestEntityTooLarge,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
	}
	if metrics.started != len(wantStatuses) || metrics.finished != len(wantStatuses) ||
		!reflect.DeepEqual(metrics.statuses, wantStatuses) {
		t.Fatalf("metrics = %+v, want statuses=%v", metrics, wantStatuses)
	}
}

type requestMetricsRecorder struct {
	mu       sync.Mutex
	started  int
	finished int
	status   int
	statuses []int
	duration time.Duration
}

func (metrics *requestMetricsRecorder) RequestStarted() {
	metrics.mu.Lock()
	metrics.started++
	metrics.mu.Unlock()
}

func (metrics *requestMetricsRecorder) RequestFinished(status int, duration time.Duration) {
	metrics.mu.Lock()
	metrics.finished++
	metrics.status = status
	metrics.statuses = append(metrics.statuses, status)
	metrics.duration = duration
	metrics.mu.Unlock()
}

func mustEngine(t *testing.T, options Options) *gin.Engine {
	t.Helper()
	if options.TraceIDGenerator == nil {
		options.TraceIDGenerator = func() string { return generatedTestTraceID }
	}
	engine, err := NewEngine(options)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	return engine
}

func request(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, body)
	return request
}

func execute(engine http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func assertProblem(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	assertHeader(t, response, "Content-Type", ProblemMediaType)
	assertHeader(t, response, "Cache-Control", "no-store")
	document := decodeJSON(t, response)
	if got := document["status"]; got != float64(status) {
		t.Fatalf("problem status = %#v, want %d", got, status)
	}
	if got := document["code"]; got != code {
		t.Fatalf("problem code = %#v, want %q", got, code)
	}
	if got := document["traceId"]; got == "" || got == nil {
		t.Fatalf("problem traceId is missing: %#v", got)
	}
	if got := document["type"]; !strings.HasPrefix(fmt.Sprint(got), problemTypeBase) {
		t.Fatalf("problem type = %#v", got)
	}
}

func assertJSONField(t *testing.T, response *httptest.ResponseRecorder, field string, want any) {
	t.Helper()
	document := decodeJSON(t, response)
	if got := document[field]; got != want {
		t.Fatalf("JSON field %s = %#v, want %#v", field, got, want)
	}
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode JSON response: %v; body = %s", err, response.Body.String())
	}
	return document
}

func assertHeader(t *testing.T, response *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := response.Header().Get(name); got != want {
		t.Fatalf("header %s = %q, want %q", name, got, want)
	}
}
