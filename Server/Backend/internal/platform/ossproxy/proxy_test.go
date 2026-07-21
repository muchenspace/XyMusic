package ossproxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestArtworkGatewayServesImmutableVersionedResource(t *testing.T) {
	assetID := "00000000-0000-4000-8000-000000000001"
	checksum := strings.Repeat("a", 64)
	payload := []byte("image-bytes")
	resolver := &artworkResolverStub{resource: ArtworkResource{
		ObjectKey: "media/artwork/cover.webp", MimeType: "image/webp", SizeBytes: int64(len(payload)),
		ChecksumSHA256: &checksum, UpdatedAt: time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC),
	}}
	objects := &artworkObjectStoreStub{payload: payload, modifiedAt: time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)}
	proxy, err := New(
		"http://objects.example.test:9000",
		"",
		WithArtworkGateway(resolver, objects),
	)
	if err != nil {
		t.Fatal(err)
	}
	resourceURL, err := proxy.ArtworkURL(assetID, checksum)
	if err != nil {
		t.Fatal(err)
	}
	gatewayURL, cacheKey, err := proxy.PresentArtwork(assetID, &checksum, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if gatewayURL != resourceURL || cacheKey != assetID+":"+checksum {
		t.Fatalf("presented artwork = %q %q", gatewayURL, cacheKey)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	proxy.Register(engine)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, resourceURL, nil))

	if response.Code != http.StatusOK || response.Body.String() != string(payload) {
		t.Fatalf("artwork response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "image/webp" ||
		response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		response.Header().Get("ETag") != `"`+checksum+`"` {
		t.Fatalf("artwork headers = %#v", response.Header())
	}
	if resolver.assetID != assetID || objects.objectKey != "media/artwork/cover.webp" {
		t.Fatalf("resolved artwork = %q %q", resolver.assetID, objects.objectKey)
	}
}

func TestArtworkGatewayRejectsStaleVersionWithoutOpeningStorage(t *testing.T) {
	assetID := "00000000-0000-4000-8000-000000000001"
	checksum := strings.Repeat("b", 64)
	resolver := &artworkResolverStub{resource: ArtworkResource{
		ObjectKey: "media/artwork/cover.webp", MimeType: "image/webp", SizeBytes: 1,
		ChecksumSHA256: &checksum, UpdatedAt: time.Now(),
	}}
	objects := &artworkObjectStoreStub{payload: []byte("x")}
	proxy, err := New(
		"http://objects.example.test:9000",
		"",
		WithArtworkGateway(resolver, objects),
	)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	proxy.Register(engine)
	response := httptest.NewRecorder()
	engine.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, Prefix+"/artwork/"+assetID+"/"+strings.Repeat("c", 64), nil),
	)
	if response.Code != http.StatusNotFound || objects.openCalls != 0 {
		t.Fatalf("stale artwork response = %d, opens = %d", response.Code, objects.openCalls)
	}
}

type artworkResolverStub struct {
	resource ArtworkResource
	assetID  string
	err      error
}

func (resolver *artworkResolverStub) ResolveArtwork(_ context.Context, assetID string) (ArtworkResource, error) {
	resolver.assetID = assetID
	return resolver.resource, resolver.err
}

type artworkObjectStoreStub struct {
	payload    []byte
	modifiedAt time.Time
	objectKey  string
	openCalls  int
	err        error
}

func (store *artworkObjectStoreStub) OpenForProxy(
	_ context.Context,
	objectKey string,
) (ReadSeekCloser, ObjectMetadata, error) {
	store.objectKey = objectKey
	store.openCalls++
	if store.err != nil {
		return nil, ObjectMetadata{}, store.err
	}
	return &readSeekCloser{Reader: bytes.NewReader(store.payload)}, ObjectMetadata{
		Size: int64(len(store.payload)), LastModified: store.modifiedAt,
	}, nil
}

type readSeekCloser struct {
	*bytes.Reader
}

func (*readSeekCloser) Close() error { return nil }

func TestClientURLPreservesSignedPathAndQuery(t *testing.T) {
	got, err := ClientURL("https://objects.example.test:9443/music/folder%20name/song.flac?X-Amz-Date=1&X-Amz-Signature=a%2Bb")
	if err != nil {
		t.Fatal(err)
	}
	target := base64.RawURLEncoding.EncodeToString([]byte("https://objects.example.test:9443"))
	want := Prefix + "/" + target + "/music/folder%20name/song.flac?X-Amz-Date=1&X-Amz-Signature=a%2Bb"
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

	proxy, err := New(storage.URL, "")
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
	proxy, err := New("https://objects.example.test", "")
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

func TestProxyAllowsConfiguredIPv6Endpoint(t *testing.T) {
	proxy, err := New("http://[2001:db8::10]:9000", "")
	if err != nil {
		t.Fatal(err)
	}
	target, err := decodeTarget(base64.RawURLEncoding.EncodeToString([]byte("http://[2001:db8::10]:9000")))
	if err != nil {
		t.Fatal(err)
	}
	scheme, allowed := proxy.upstreamScheme(target, "/music/song.flac")
	if !allowed || scheme != "http" {
		t.Fatalf("configured IPv6 endpoint = scheme %q allowed %v", scheme, allowed)
	}

	wrongPort, err := decodeTarget(base64.RawURLEncoding.EncodeToString([]byte("http://[2001:db8::10]:9001")))
	if err != nil {
		t.Fatal(err)
	}
	if _, allowed := proxy.upstreamScheme(wrongPort, "/music/song.flac"); allowed {
		t.Fatal("IPv6 endpoint with a different port must be rejected")
	}
}

func TestProxyForwardsConfiguredPublicBaseURLUsingItsOwnScheme(t *testing.T) {
	publicStorage := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/cdn/music/song.flac" {
			t.Fatalf("upstream path = %q", request.URL.EscapedPath())
		}
		_, _ = writer.Write([]byte("public object"))
	}))
	defer publicStorage.Close()

	proxy, err := New("http://objects.example.test:9000", publicStorage.URL+"/cdn")
	if err != nil {
		t.Fatal(err)
	}
	proxy.proxy.Transport = publicStorage.Client().Transport
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	proxy.Register(engine)
	backend := httptest.NewServer(engine)
	defer backend.Close()
	clientURL, err := ClientURL(publicStorage.URL + "/cdn/music/song.flac")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(backend.URL + clientURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "public object" {
		t.Fatalf("proxy response = %d %q", response.StatusCode, string(body))
	}

	outsideURL, err := ClientURL(publicStorage.URL + "/private/secret")
	if err != nil {
		t.Fatal(err)
	}
	outsideResponse := httptest.NewRecorder()
	engine.ServeHTTP(outsideResponse, httptest.NewRequest(http.MethodGet, outsideURL, nil))
	if outsideResponse.Code != http.StatusBadRequest {
		t.Fatalf("outside public base status = %d, want %d", outsideResponse.Code, http.StatusBadRequest)
	}
}
