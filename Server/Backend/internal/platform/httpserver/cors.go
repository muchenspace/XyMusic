package httpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSConfig controls browser cross-origin access. An empty origin allow-list
// denies cross-origin access while leaving same-origin requests unaffected.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowAllOrigins  bool
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

var corsToken = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

// DefaultCORSConfig returns the API's supported methods and headers. Callers
// must still opt into explicit origins or AllowAllOrigins.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Idempotency-Key",
			"X-CSRF-Token",
			TraceIDHeader,
		},
		ExposedHeaders: []string{
			TraceIDHeader,
			"X-Idempotent-Replay",
			"Retry-After",
		},
		MaxAge: 10 * time.Minute,
	}
}

// UnrestrictedCORSConfig accepts every valid browser origin. Credentialed
// requests reflect the request Origin because browsers reject credentials
// when Access-Control-Allow-Origin is the literal wildcard.
func UnrestrictedCORSConfig() CORSConfig {
	config := DefaultCORSConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AllowedHeaders = []string{"*"}
	config.ExposedHeaders = append(config.ExposedHeaders, "X-CSRF-Token")
	return config
}

type compiledCORS struct {
	allowAll        bool
	origins         map[string]struct{}
	methods         map[string]struct{}
	methodList      string
	headers         map[string]struct{}
	headerList      string
	exposedList     string
	allowAnyHeader  bool
	credentials     bool
	reflectOrigin   bool
	maxAgeInSeconds int64
}

// CORS validates configuration once and returns a Gin CORS middleware.
func CORS(config CORSConfig) (gin.HandlerFunc, error) {
	compiled, err := compileCORS(config)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}

		preflight := c.Request.Method == http.MethodOptions &&
			strings.TrimSpace(c.GetHeader("Access-Control-Request-Method")) != ""
		allowed := compiled.allowAll
		if compiled.reflectOrigin && !validOrigin(origin) {
			allowed = false
		}
		if !allowed {
			_, allowed = compiled.origins[origin]
		}
		if !compiled.allowAll || compiled.reflectOrigin {
			addVary(c.Writer.Header(), "Origin")
		}
		if !allowed {
			if preflight {
				writeProblem(c, newHTTPProblem(
					http.StatusForbidden,
					"CORS_ORIGIN_FORBIDDEN",
					"跨域来源无效",
					"请求中的 Origin 不是有效的浏览器来源",
					TraceID(c),
					requestInstance(c),
				))
				return
			}
			c.Next()
			return
		}

		if compiled.allowAll && !compiled.reflectOrigin {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		if compiled.credentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if !preflight {
			if compiled.exposedList != "" {
				c.Header("Access-Control-Expose-Headers", compiled.exposedList)
			}
			c.Next()
			return
		}

		addVary(c.Writer.Header(), "Access-Control-Request-Method")
		addVary(c.Writer.Header(), "Access-Control-Request-Headers")
		requestedMethod := strings.ToUpper(strings.TrimSpace(c.GetHeader("Access-Control-Request-Method")))
		if _, ok := compiled.methods[requestedMethod]; !ok || !compiled.requestHeadersAllowed(c.GetHeader("Access-Control-Request-Headers")) {
			writeProblem(c, newHTTPProblem(
				http.StatusForbidden,
				"CORS_PREFLIGHT_FORBIDDEN",
				"跨域预检未通过",
				"请求方法或请求头不符合服务端跨域协议",
				TraceID(c),
				requestInstance(c),
			))
			return
		}

		c.Header("Access-Control-Allow-Methods", compiled.methodList)
		if compiled.allowAnyHeader {
			requestedHeaders := strings.TrimSpace(c.GetHeader("Access-Control-Request-Headers"))
			if requestedHeaders == "" {
				c.Header("Access-Control-Allow-Headers", "*")
			} else {
				c.Header("Access-Control-Allow-Headers", requestedHeaders)
			}
		} else if compiled.headerList != "" {
			c.Header("Access-Control-Allow-Headers", compiled.headerList)
		}
		if compiled.maxAgeInSeconds > 0 {
			c.Header("Access-Control-Max-Age", fmt.Sprintf("%d", compiled.maxAgeInSeconds))
		}
		c.AbortWithStatus(http.StatusNoContent)
	}, nil
}

func compileCORS(input CORSConfig) (compiledCORS, error) {
	defaults := DefaultCORSConfig()
	if input.AllowAllOrigins && len(input.AllowedOrigins) > 0 {
		return compiledCORS{}, fmt.Errorf("CORS wildcard and explicit origins cannot be combined")
	}
	if input.MaxAge < 0 {
		return compiledCORS{}, fmt.Errorf("CORS max age cannot be negative")
	}

	origins := make(map[string]struct{}, len(input.AllowedOrigins))
	for _, rawOrigin := range input.AllowedOrigins {
		origin := strings.TrimSpace(rawOrigin)
		if !validOrigin(origin) {
			return compiledCORS{}, fmt.Errorf("invalid CORS origin %q", rawOrigin)
		}
		origins[origin] = struct{}{}
	}

	methodsInput := input.AllowedMethods
	if len(methodsInput) == 0 {
		methodsInput = defaults.AllowedMethods
	}
	methods := make(map[string]struct{}, len(methodsInput))
	for _, rawMethod := range methodsInput {
		method := strings.ToUpper(strings.TrimSpace(rawMethod))
		if !corsToken.MatchString(method) {
			return compiledCORS{}, fmt.Errorf("invalid CORS method %q", rawMethod)
		}
		methods[method] = struct{}{}
	}

	headersInput := input.AllowedHeaders
	if len(headersInput) == 0 {
		headersInput = defaults.AllowedHeaders
	}
	headers := make(map[string]struct{}, len(headersInput))
	allowAnyHeader := false
	canonicalHeaders := make([]string, 0, len(headersInput))
	for _, rawHeader := range headersInput {
		header := strings.TrimSpace(rawHeader)
		if header == "*" {
			allowAnyHeader = true
			continue
		}
		if !corsToken.MatchString(header) {
			return compiledCORS{}, fmt.Errorf("invalid CORS header %q", rawHeader)
		}
		lower := strings.ToLower(header)
		if _, exists := headers[lower]; !exists {
			headers[lower] = struct{}{}
			canonicalHeaders = append(canonicalHeaders, http.CanonicalHeaderKey(header))
		}
	}
	if allowAnyHeader && len(headers) > 0 {
		return compiledCORS{}, fmt.Errorf("CORS wildcard and explicit request headers cannot be combined")
	}

	exposedInput := input.ExposedHeaders
	if len(exposedInput) == 0 {
		exposedInput = defaults.ExposedHeaders
	}
	exposed := make([]string, 0, len(exposedInput))
	seenExposed := make(map[string]struct{}, len(exposedInput))
	for _, rawHeader := range exposedInput {
		header := strings.TrimSpace(rawHeader)
		if header != "*" && !corsToken.MatchString(header) {
			return compiledCORS{}, fmt.Errorf("invalid exposed CORS header %q", rawHeader)
		}
		lower := strings.ToLower(header)
		if _, exists := seenExposed[lower]; !exists {
			seenExposed[lower] = struct{}{}
			if header == "*" {
				exposed = append(exposed, header)
			} else {
				exposed = append(exposed, http.CanonicalHeaderKey(header))
			}
		}
	}

	methodList := sortedSet(methods)
	sort.Strings(canonicalHeaders)
	sort.Strings(exposed)
	maxAge := input.MaxAge
	if maxAge == 0 {
		maxAge = defaults.MaxAge
	}
	return compiledCORS{
		allowAll:        input.AllowAllOrigins,
		origins:         origins,
		methods:         methods,
		methodList:      strings.Join(methodList, ", "),
		headers:         headers,
		headerList:      strings.Join(canonicalHeaders, ", "),
		exposedList:     strings.Join(exposed, ", "),
		allowAnyHeader:  allowAnyHeader,
		credentials:     input.AllowCredentials,
		reflectOrigin:   input.AllowAllOrigins && input.AllowCredentials,
		maxAgeInSeconds: int64(maxAge / time.Second),
	}, nil
}

func (c compiledCORS) requestHeadersAllowed(raw string) bool {
	if strings.TrimSpace(raw) == "" || c.allowAnyHeader {
		return true
	}
	for _, entry := range strings.Split(raw, ",") {
		header := strings.TrimSpace(entry)
		if !corsToken.MatchString(header) {
			return false
		}
		if _, ok := c.headers[strings.ToLower(header)]; !ok {
			return false
		}
	}
	return true
}

func validOrigin(origin string) bool {
	if origin == "null" {
		return true
	}
	if origin == "" || strings.ContainsAny(origin, "\r\n") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func addVary(header http.Header, value string) {
	for _, current := range header.Values("Vary") {
		for _, token := range strings.Split(current, ",") {
			if strings.EqualFold(strings.TrimSpace(token), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
