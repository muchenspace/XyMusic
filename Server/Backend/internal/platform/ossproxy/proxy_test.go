package ossproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestClientURLPreservesSignedPathAndQuery(t *testing.T) {
	got, err := ClientURL("https://objects.example.test:9443/music/folder%20name/song.flac?X-Amz-Date=1&X-Amz-Signature=a%2Bb")
	if err != nil {
		t.Fatal(err)
	}
	want := Prefix + "/b2JqZWN0cy5leGFtcGxlLnRlc3Q6OTQ0Mw/music/folder%20name/song.flac?X-Amz-Date=1&X-Amz-Signature=a%2Bb"
	if got != want {
		t.Fatalf("client URL = %q, want %q", got, want)
	}
}

func TestProxyForwardsSignedRequestsToConfiguredStorage(t *testing.T) {
	var observedMethod, observedPath, observedQuery, observedHost, observedRange, observedBody string
	storage := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedMethod = request.Method
		observedPath = request.URL.EscapedPath()
		observedQuery = request.URL.RawQuery
		observedHost = request.Host
		observedRange = request.Header.Get("Range")
		body, _ := io.ReadAll(request.Body)
		observedBody = string(body)
		writer.Header().Set("ETag", `"asset-etag"`)
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("proxied"))
	}))
	defer storage.Close()

	proxy, err := New(storage.URL)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	proxy.Register(engine)
	backend := httptest.NewServer(engine)
	defer backend.Close()

	signedURL := storage.URL + "/music/folder%20name/song.flac?X-Amz-Date=1&X-Amz-Signature=a%2Bb"
	clientURL, err := ClientURL(signedURL)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPut, backend.URL+clientURL, strings.NewReader("audio"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=10-20")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusPartialContent || string(responseBody) != "proxied" || response.Header.Get("ETag") != `"asset-etag"` {
		t.Fatalf("proxy response = %d %q %#v", response.StatusCode, string(responseBody), response.Header)
	}
	if observedMethod != http.MethodPut || observedPath != "/music/folder%20name/song.flac" ||
		observedQuery != "X-Amz-Date=1&X-Amz-Signature=a%2Bb" || observedRange != "bytes=10-20" || observedBody != "audio" {
		t.Fatalf("upstream request = method %q path %q query %q range %q body %q", observedMethod, observedPath, observedQuery, observedRange, observedBody)
	}
	if observedHost != strings.TrimPrefix(storage.URL, "http://") {
		t.Fatalf("upstream Host = %q, want %q", observedHost, strings.TrimPrefix(storage.URL, "http://"))
	}
}

func TestProxyRejectsAuthorityOutsideConfiguredEndpoint(t *testing.T) {
	proxy, err := New("https://objects.example.test")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	proxy.Register(engine)
	clientURL, err := ClientURL("https://attacker.example/secret")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, clientURL, nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
