package admintagscraping

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestArtworkDownloadsAreCoalescedAndContentValidated(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return responseFor(request, http.StatusOK, "image/jpeg", []byte{0xff, 0xd8, 0xff, 0x00}), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	type result struct {
		artwork DownloadedArtwork
		err     error
	}
	results := make(chan result, 2)
	go func() {
		artwork, err := platform.DownloadArtwork(context.Background(), "https://y.qq.com/cover/same.jpg")
		results <- result{artwork: artwork, err: err}
	}()
	<-started
	go func() {
		artwork, err := platform.DownloadArtwork(context.Background(), "https://y.qq.com/cover/same.jpg")
		results <- result{artwork: artwork, err: err}
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("download errors = %v / %v", first.err, second.err)
	}
	if calls.Load() != 1 || first.artwork.ContentType != "image/jpeg" || second.artwork.Extension != "jpg" {
		t.Fatalf("calls/artwork = %d / %#v / %#v", calls.Load(), first.artwork, second.artwork)
	}
}

func TestArtworkFailureOpensShortHostCircuit(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseFor(request, http.StatusServiceUnavailable, "text/plain", nil), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	_, firstErr := platform.DownloadArtwork(context.Background(), "https://y.qq.com/cover/one.jpg")
	firstCalls := calls.Load()
	_, secondErr := platform.DownloadArtwork(context.Background(), "https://y.qq.com/cover/two.jpg")
	if firstErr == nil || secondErr == nil || firstCalls != 3 || calls.Load() != firstCalls {
		t.Fatalf("errors/calls = %v / %v / %d / %d", firstErr, secondErr, firstCalls, calls.Load())
	}
	if !apperror.IsCode(secondErr, apperror.CodeDependencyUnavailable) {
		t.Fatalf("circuit error = %v", secondErr)
	}
}

func TestArtworkRejectsUntrustedHostsWithoutNetworkAccess(t *testing.T) {
	var calls atomic.Int32
	platform := NewMusicPlatformClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseFor(request, http.StatusOK, "image/jpeg", []byte{0xff, 0xd8, 0xff}), nil
	})}, "")
	_, err := platform.DownloadArtwork(context.Background(), "https://example.com/private.jpg")
	if !apperror.IsCode(err, apperror.CodeValidationError) || calls.Load() != 0 {
		t.Fatalf("error/calls = %v/%d", err, calls.Load())
	}
}

func TestMissingAcoustIDConfigurationDoesNotMakeARequest(t *testing.T) {
	var calls atomic.Int32
	platform := NewMusicPlatformClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, io.EOF
	})}, "")
	_, err := platform.AcoustID(context.Background(), 120, "fingerprint")
	if !apperror.IsCode(err, apperror.CodeDependencyUnavailable) || calls.Load() != 0 || !strings.Contains(err.Error(), "AcoustID") {
		t.Fatalf("error/calls = %v/%d", err, calls.Load())
	}
}

func TestSearchArtistsParsesQQSmartboxCandidates(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "c.y.qq.com" || request.URL.Path != "/splcloud/fcgi-bin/smartbox_new.fcg" ||
			request.URL.Query().Get("key") != "Artist" {
			t.Fatalf("unexpected QQ artist request: %s", request.URL.String())
		}
		body := []byte(`{"data":{"singer":{"itemlist":[{"mid":"qq-mid","name":"Artist"},{"mid":"qq-mid","name":"Artist"}]}}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.SearchArtists(context.Background(), SourceQMusic, "Artist")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "qq-mid" || result[0].Name != "Artist" ||
		result[0].ImageURL != "https://y.qq.com/music/photo_new/T001R500x500M000qq-mid.jpg" ||
		result[0].Aliases == nil {
		t.Fatalf("QQ artist results = %#v", result)
	}
}

func TestSearchArtistsParsesNeteaseCandidatesAndAliases(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "music.163.com" || request.URL.Path != "/api/linux/forward" {
			t.Fatalf("unexpected Netease artist request: %s", request.URL.String())
		}
		body := []byte(`{"result":{"artists":[{"id":123,"name":"Artist","picUrl":"http://p1.music.126.net/artist.jpg","alias":["Alias"],"transNames":["Alias","Translated"]}]}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.SearchArtists(context.Background(), SourceNetease, "Artist")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "123" || result[0].Name != "Artist" ||
		result[0].ImageURL != "https://p1.music.126.net/artist.jpg" ||
		len(result[0].Aliases) != 2 || result[0].Aliases[0] != "Alias" || result[0].Aliases[1] != "Translated" {
		t.Fatalf("Netease artist results = %#v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func responseFor(request *http.Request, status int, contentType string, body []byte) *http.Response {
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}
