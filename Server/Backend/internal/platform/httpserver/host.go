package httpserver

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// HostGuard rejects DNS-rebinding and proxy-host confusion when callers opt
// into an explicit listener host allow-list. An empty allow-list disables the
// guard for configured deployments that intentionally serve named hosts.
func HostGuard(allowedHosts []string) (gin.HandlerFunc, error) {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, raw := range allowedHosts {
		host, ok := normalizeHost(raw, false)
		if !ok {
			return nil, &hostConfigurationError{host: raw}
		}
		allowed[host] = struct{}{}
	}
	return func(c *gin.Context) {
		if len(allowed) == 0 {
			c.Next()
			return
		}
		host, ok := normalizeHost(c.Request.Host, true)
		if ok {
			_, ok = allowed[host]
		}
		if !ok {
			writeProblem(c, newHTTPProblem(
				http.StatusMisdirectedRequest,
				"HOST_NOT_ALLOWED",
				"Request host is not allowed",
				"Use the local XyMusic address shown by the application.",
				TraceID(c),
				requestInstance(c),
			))
			return
		}
		c.Next()
	}, nil
}

type hostConfigurationError struct{ host string }

func (err *hostConfigurationError) Error() string {
	return "invalid allowed HTTP host " + strconv.Quote(err.host)
}

func normalizeHost(raw string, allowPort bool) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "\x00\r\n/\\") {
		return "", false
	}

	host := value
	if strings.HasPrefix(value, "[") {
		closing := strings.IndexByte(value, ']')
		if closing < 0 {
			return "", false
		}
		host = value[1:closing]
		remainder := value[closing+1:]
		if remainder != "" {
			if !allowPort || !validHostPort(strings.TrimPrefix(remainder, ":")) || !strings.HasPrefix(remainder, ":") {
				return "", false
			}
		}
	} else if parsedHost, port, err := net.SplitHostPort(value); err == nil {
		if !allowPort || !validHostPort(port) {
			return "", false
		}
		host = parsedHost
	} else if strings.Contains(value, ":") && net.ParseIP(value) == nil {
		return "", false
	}

	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return "", false
	}
	if address := net.ParseIP(host); address != nil {
		return address.String(), true
	}
	for _, character := range host {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '-' && character != '.' {
			return "", false
		}
	}
	return host, true
}

func validHostPort(raw string) bool {
	if raw == "" || strings.IndexFunc(raw, func(character rune) bool {
		return character < '0' || character > '9'
	}) >= 0 {
		return false
	}
	port, err := strconv.ParseUint(raw, 10, 16)
	return err == nil && port > 0
}
