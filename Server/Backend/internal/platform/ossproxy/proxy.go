package ossproxy

import (
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const Prefix = "/api/v1/oss"

// Proxy relays client-facing, pre-signed S3 requests to the configured object
// storage endpoint. The signed authority is carried in the proxy path so the
// upstream receives the same Host value that was included in the signature.
type Proxy struct {
	endpoint *url.URL
	proxy    *httputil.ReverseProxy
}

func New(rawEndpoint string) (*Proxy, error) {
	endpoint, err := parseEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}
	reverse := &httputil.ReverseProxy{
		Director:      func(*http.Request) {},
		FlushInterval: -1 * time.Second,
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "object storage is unavailable", http.StatusBadGateway)
		},
	}
	return &Proxy{endpoint: endpoint, proxy: reverse}, nil
}

func (proxy *Proxy) Register(router gin.IRouter) {
	path := Prefix + "/:authority/*path"
	router.GET(path, proxy.handle)
	router.HEAD(path, proxy.handle)
	router.PUT(path, proxy.handle)
}

// ClientURL converts an absolute pre-signed S3 URL into a relative URL served
// by the XyMusic backend. Query text is preserved byte-for-byte because it is
// part of the S3 signature.
func ClientURL(rawSignedURL string) (string, error) {
	signed, err := url.Parse(rawSignedURL)
	if err != nil || signed.Host == "" || (signed.Scheme != "http" && signed.Scheme != "https") {
		return "", errors.New("signed object URL must be an absolute HTTP or HTTPS URL")
	}
	authority := base64.RawURLEncoding.EncodeToString([]byte(signed.Host))
	path := signed.EscapedPath()
	if path == "" {
		path = "/"
	}
	result := Prefix + "/" + authority + path
	if signed.ForceQuery || signed.RawQuery != "" {
		result += "?" + signed.RawQuery
	}
	return result, nil
}

func (proxy *Proxy) handle(c *gin.Context) {
	authority, err := decodeAuthority(c.Param("authority"))
	if err != nil || !proxy.allowsAuthority(authority) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	escapedPath := c.Request.URL.EscapedPath()
	escapedPrefix := Prefix + "/" + c.Param("authority")
	if !strings.HasPrefix(escapedPath, escapedPrefix+"/") {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	upstreamEscapedPath := strings.TrimPrefix(escapedPath, escapedPrefix)
	upstreamPath, err := url.PathUnescape(upstreamEscapedPath)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	c.Request.URL.Scheme = proxy.endpoint.Scheme
	c.Request.URL.Host = authority
	c.Request.URL.Path = upstreamPath
	c.Request.URL.RawPath = upstreamEscapedPath
	c.Request.Host = authority
	proxy.proxy.ServeHTTP(c.Writer, c.Request)
}

func parseEndpoint(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return url.Parse("https://s3.amazonaws.com")
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errors.New("S3_ENDPOINT must be an absolute HTTP or HTTPS URL")
	}
	if endpoint.User != nil || (endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("S3_ENDPOINT must not contain credentials, a path, query, or fragment")
	}
	return endpoint, nil
}

func decodeAuthority(encoded string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return "", errors.New("invalid object storage authority")
	}
	authority := string(decoded)
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.Host != authority || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" {
		return "", errors.New("invalid object storage authority")
	}
	return authority, nil
}

func (proxy *Proxy) allowsAuthority(authority string) bool {
	candidate, err := url.Parse("//" + authority)
	if err != nil || candidate.Hostname() == "" || candidate.Port() != proxy.endpoint.Port() {
		return false
	}
	configuredHost := strings.ToLower(proxy.endpoint.Hostname())
	candidateHost := strings.ToLower(candidate.Hostname())
	if candidateHost == configuredHost {
		return true
	}
	if net.ParseIP(configuredHost) != nil {
		return false
	}
	return strings.HasSuffix(candidateHost, "."+configuredHost)
}
