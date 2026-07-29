package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"encoding/json"
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

func TestSearchCacheAvoidsRepeatedUpstreamRequests(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := []byte(`{"data":{"song":{"list":[{"songmid":"mid","songid":1,"songname":"Song","singer":[],"albummid":"album","albumname":"Album"}]}}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	for index := 0; index < 2; index++ {
		result, err := platform.Search(context.Background(), SourceQMusic, "Song")
		if err != nil || len(result) != 1 {
			t.Fatalf("search %d result/error = %#v / %v", index, result, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
}

func TestConcurrentSearchesAreCoalescedIntoOneUpstreamRequest(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		body := []byte(`{"data":{"song":{"list":[]}}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := platform.Search(context.Background(), SourceQMusic, "Concurrent Song")
			results <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream search did not start")
	}
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("upstream calls while coalesced = %d, want 1", calls.Load())
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourceCircuitIsIndependentPerChannel(t *testing.T) {
	var qmusicCalls, neteaseCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "c.y.qq.com":
			qmusicCalls.Add(1)
			return responseFor(request, http.StatusServiceUnavailable, "text/plain", nil), nil
		case "music.163.com":
			neteaseCalls.Add(1)
			return responseFor(request, http.StatusOK, "application/json", []byte(`{"result":{"songs":[]}}`)), nil
		default:
			t.Fatalf("unexpected host: %s", request.URL.Host)
			return nil, nil
		}
	})}
	platform := NewMusicPlatformClient(client, "")
	for index := 0; index < maximumSourceFailures; index++ {
		platform.recordSourceFailure(SourceQMusic, &upstreamHTTPError{status: http.StatusInternalServerError})
	}
	if health := platform.ChannelHealth(SourceQMusic); health.State != "OPEN" || health.RetryAfterSeconds <= 0 {
		t.Fatalf("QQ health = %#v", health)
	}
	if _, err := platform.Search(context.Background(), SourceQMusic, "blocked"); !apperror.IsCode(err, apperror.CodeDependencyUnavailable) {
		t.Fatalf("QQ circuit error = %v", err)
	}
	if qmusicCalls.Load() != 0 {
		t.Fatalf("QQ requests while circuit open = %d", qmusicCalls.Load())
	}
	if _, err := platform.Search(context.Background(), SourceNetease, "available"); err != nil {
		t.Fatal(err)
	}
	if neteaseCalls.Load() != 1 {
		t.Fatalf("Netease requests = %d, want 1", neteaseCalls.Load())
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

func TestSearchArtistsCacheAvoidsRepeatedUpstreamRequests(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := []byte(`{"data":{"singer":{"itemlist":[{"mid":"artist","name":"Artist"}]}}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	for _, query := range []string{"Artist", " Artist "} {
		result, err := platform.SearchArtists(context.Background(), SourceQMusic, query)
		if err != nil || len(result) != 1 {
			t.Fatalf("artist search result/error = %#v / %v", result, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream artist calls = %d, want 1", calls.Load())
	}
}

func TestSearchArtistsRejectsRemovedNeteaseProvider(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "music.163.com" || request.URL.Path != "/api/linux/forward" {
			t.Fatalf("unexpected Netease artist request: %s", request.URL.String())
		}
		body := []byte(`{"result":{"artists":[{"id":123,"name":"Artist","picUrl":"http://p1.music.126.net/artist.jpg","alias":["Alias"],"transNames":["Alias","Translated"]}]}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.SearchArtists(context.Background(), Source("netease"), "Artist")
	if !apperror.IsCode(err, apperror.CodeValidationError) || result != nil {
		t.Fatalf("result/error = %#v / %v", result, err)
	}
}

func TestSearchQQKeepsSongmidAndNumericSongIDForLyrics(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "c.y.qq.com" || request.URL.Path != "/soso/fcgi-bin/client_search_cp" {
			t.Fatalf("unexpected QQ song request: %s", request.URL.String())
		}
		body := []byte(`{"data":{"song":{"list":[{"songmid":"qq-mid","songid":12345,"songname":"Song","singer":[{"mid":"artist","name":"Artist"}],"albummid":"album","albumname":"Album","pubtime":0,"interval":183}]}}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Search(context.Background(), SourceQMusic, "Song")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "qq-mid" || result[0].LyricID != "12345" || result[0].DurationMS != 183_000 {
		t.Fatalf("QQ candidates = %#v", result)
	}
}

func TestSearchKugouUsesResultIDForLyricLookup(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "complexsearch.kugou.com" || request.URL.Path != "/v2/search/song" {
			t.Fatalf("unexpected Kugou song request: %s", request.URL.String())
		}
		body := []byte(`{"data":{"lists":[{"ID":123,"Audioid":456,"FileHash":"hash","SongName":"Song","SingerName":"Artist","SingerId":"artist","AlbumName":"Album","AlbumID":"album","Duration":183}]}}`)
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Search(context.Background(), SourceKugou, "Song")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].LyricID != "123" || result[0].DurationMS != 183_000 {
		t.Fatalf("Kugou candidates = %#v", result)
	}
}

func TestOrdinaryLyricProvidersReturnLineTiming(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Host == "music.163.com" && request.URL.Path == "/api/linux/forward":
			return responseFor(request, http.StatusOK, "application/json", []byte(`{"lrc":{"lyric":"[00:01.00]line"}}`)), nil
		case request.URL.Host == "m.kugou.com" && request.URL.Path == "/app/i/krc.php":
			return responseFor(request, http.StatusOK, "text/plain", []byte("[00:01.00]line")), nil
		default:
			t.Fatalf("unexpected lyric request: %s", request.URL.String())
			return nil, nil
		}
	})}
	platform := NewMusicPlatformClient(client, "")
	for _, test := range []struct {
		name      string
		source    Source
		candidate Candidate
	}{
		{name: "netease", source: SourceNetease, candidate: Candidate{ID: "163", Name: "Song"}},
		{name: "kugou", source: SourceKugou, candidate: Candidate{ID: "hash", Name: "Song"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := platform.Lyric(context.Background(), test.source, test.candidate, false)
			if err != nil {
				t.Fatal(err)
			}
			if result.Content != "[00:01.00]line" || string(result.Timing) != "LINE" {
				t.Fatalf("lyric result = %#v", result)
			}
		})
	}
}

func TestNeteaseEnhancedLyricReturnsWordTimingWithoutVerbatim(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return responseFor(request, http.StatusOK, "application/json", []byte(`{"lrc":{"lyric":"[00:01.00]<00:01.00>word"}}`)), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Lyric(context.Background(), SourceNetease, Candidate{ID: "163", Name: "Song"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "[00:01.00]<00:01.00>word" || result.Timing != "WORD" {
		t.Fatalf("enhanced lyric result = %#v", result)
	}
}

func TestQQVerbatimLyricWithoutWordMarkersStaysLineTiming(t *testing.T) {
	qrc := `<Lyric_1 LyricType="1" LyricContent="[0,1000]ordinary line"/>`
	encrypted, err := encryptQRCForTest([]byte(qrc))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{
			"code": 0,
			"request": map[string]any{
				"code": 0,
				"data": map[string]any{"lyric": hex.EncodeToString(encrypted), "qrc_t": "1"},
			},
		})
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Lyric(context.Background(), SourceQMusic, Candidate{
		ID: "123", Name: "Song", Artist: "Artist", Album: "Album", DurationMS: 1_000,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "[00:00.000]ordinary line" || string(result.Timing) != "LINE" {
		t.Fatalf("ordinary QRC result = %#v", result)
	}
}

func TestQQVerbatimLyricUsesMusicUAndDecryptsQRC(t *testing.T) {
	qrc := `<Lyric_1 LyricType="1" LyricContent="[0,1000]你(0,500)好(500,500)"/>`
	encrypted, err := encryptQRCForTest([]byte(qrc))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "u.y.qq.com" || request.URL.Path != "/cgi-bin/musicu.fcg" || request.Method != http.MethodPost {
			t.Fatalf("unexpected QQ lyric request: %s", request.URL.String())
		}
		var payload struct {
			Request struct {
				Method string         `json:"method"`
				Module string         `json:"module"`
				Param  map[string]any `json:"param"`
			} `json:"request"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Request.Method != "GetPlayLyricInfo" || payload.Request.Module != "music.musichallSong.PlayLyricInfo" ||
			payload.Request.Param["qrc"] != float64(1) {
			t.Fatalf("QQ request payload = %#v", payload)
		}
		body, _ := json.Marshal(map[string]any{
			"code": 0,
			"request": map[string]any{
				"code": 0,
				"data": map[string]any{"lyric": hex.EncodeToString(encrypted), "qrc_t": "1"},
			},
		})
		return responseFor(request, http.StatusOK, "application/json", body), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Lyric(context.Background(), SourceQMusic, Candidate{
		ID: "123", Name: "歌曲", Artist: "歌手", Album: "专辑", DurationMS: 1_000,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "[00:00.000]<00:00.000>你<00:00.500>好<00:01.000>" || result.Timing != "WORD" {
		t.Fatalf("verbatim lyrics = %#v", result)
	}
}

func encryptQRCForTest(content []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	plain := compressed.Bytes()
	if remainder := len(plain) % 8; remainder != 0 {
		plain = append(plain, bytes.Repeat([]byte{0}, 8-remainder)...)
	}
	schedule := qrcTripleKeySchedule(qrcKey, qrcCipherEncrypt)
	encrypted := make([]byte, 0, len(plain))
	for offset := 0; offset < len(plain); offset += 8 {
		encrypted = append(encrypted, qrcTripleCryptBlock(plain[offset:offset+8], schedule)...)
	}
	return encrypted, nil
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
