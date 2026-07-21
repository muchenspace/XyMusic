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

// Proxy relays client-facing object requests to the configured S3 endpoint or
// public base URL. The upstream origin is carried in the proxy path so the Host
// value used by S3 signatures is preserved.
type Proxy struct {
	endpoint  *url.URL
	publicURL *url.URL
	proxy     *httputil.ReverseProxy
}

func New(rawEndpoint, rawPublicBaseURL string) (*Proxy, error) {
	endpoint, err := parseEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}
	publicBaseURL, err := parsePublicBaseURL(rawPublicBaseURL)
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
	return &Proxy{endpoint: endpoint, publicURL: publicBaseURL, proxy: reverse}, nil
}

func (proxy *Proxy) Register(router gin.IRouter) {
	path := Prefix + "/:authority/*path"
	router.GET(path, proxy.handle)
	router.HEAD(path, proxy.handle)
	router.PUT(path, proxy.handle)
}

// ClientURL converts an absolute pre-signed S3 URL into a relative URL served
// by the XyMusic backend. The encoded target contains both scheme and authority
// so endpoint and public-base URLs can safely use different protocols. Query
// text is preserved byte-for-byte because it is part of the S3 signature.
func ClientURL(rawSignedURL string) (string, error) {
	signed, err := url.Parse(rawSignedURL)
	if err != nil || signed.Host == "" || (signed.Scheme != "http" && signed.Scheme != "https") || signed.User != nil || signed.Fragment != "" {
		return "", errors.New("signed object URL must be an absolute HTTP or HTTPS URL")
	}
	target := base64.RawURLEncoding.EncodeToString([]byte(signed.Scheme + "://" + signed.Host))
	path := signed.EscapedPath()
	if path == "" {
		path = "/"
	}
	result := Prefix + "/" + target + path
	if signed.ForceQuery || signed.RawQuery != "" {
		result += "?" + signed.RawQuery
	}
	return result, nil
}

func (proxy *Proxy) handle(c *gin.Context) {
	target, err := decodeTarget(c.Param("authority"))
	if err != nil {
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
	upstreamScheme, allowed := proxy.upstreamScheme(target, upstreamEscapedPath)
	if !allowed {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	upstreamPath, err := url.PathUnescape(upstreamEscapedPath)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	c.Request.URL.Scheme = upstreamScheme
	c.Request.URL.Host = target.authority
	c.Request.URL.Path = upstreamPath
	c.Request.URL.RawPath = upstreamEscapedPath
	c.Request.Host = target.authority
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

func parsePublicBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	publicBaseURL, err := url.Parse(raw)
	if err != nil || publicBaseURL.Host == "" || (publicBaseURL.Scheme != "http" && publicBaseURL.Scheme != "https") {
		return nil, errors.New("S3_PUBLIC_BASE_URL must be an absolute HTTP or HTTPS URL")
	}
	if publicBaseURL.User != nil || publicBaseURL.RawQuery != "" || publicBaseURL.Fragment != "" {
		return nil, errors.New("S3_PUBLIC_BASE_URL must not contain credentials, a query, or fragment")
	}
	return publicBaseURL, nil
}

type upstreamTarget struct {
	scheme    string
	authority string
}

func decodeTarget(encoded string) (upstreamTarget, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return upstreamTarget{}, errors.New("invalid object storage target")
	}
	value := string(decoded)
	if strings.Contains(value, "://") {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return upstreamTarget{}, errors.New("invalid object storage target")
		}
		return upstreamTarget{scheme: parsed.Scheme, authority: parsed.Host}, nil
	}
	authority := value
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.Host != authority || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" {
		return upstreamTarget{}, errors.New("invalid object storage target")
	}
	return upstreamTarget{authority: authority}, nil
}

func (proxy *Proxy) upstreamScheme(target upstreamTarget, escapedPath string) (string, bool) {
	if target.scheme != "" {
		if proxy.allowsPublicTarget(target, escapedPath) {
			return target.scheme, true
		}
		if target.scheme != proxy.endpoint.Scheme {
			return "", false
		}
	} else if !proxy.allowsEndpointAuthority(target.authority) {
		if proxy.allowsPublicTarget(target, escapedPath) {
			return proxy.publicURL.Scheme, true
		}
		return "", false
	}
	if !proxy.allowsEndpointAuthority(target.authority) {
		return "", false
	}
	return proxy.endpoint.Scheme, true
}

func (proxy *Proxy) allowsPublicTarget(target upstreamTarget, escapedPath string) bool {
	if proxy.publicURL == nil || !strings.EqualFold(target.authority, proxy.publicURL.Host) ||
		(target.scheme != "" && target.scheme != proxy.publicURL.Scheme) {
		return false
	}
	basePath := strings.TrimRight(proxy.publicURL.EscapedPath(), "/")
	return basePath == "" || escapedPath == basePath || strings.HasPrefix(escapedPath, basePath+"/")
}

func (proxy *Proxy) allowsEndpointAuthority(authority string) bool {
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
