package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
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

func TestNeteaseVerbatimLyricUsesEAPIAndRendersYRC(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "interface.music.163.com" || request.URL.Path != "/eapi/song/lyric/v1" || request.Method != http.MethodPost {
			t.Fatalf("unexpected Netease lyric request: %s", request.URL.String())
		}
		if !strings.HasPrefix(request.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			t.Fatalf("content type = %q", request.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "params=") {
			t.Fatalf("encrypted request body = %q", body)
		}
		response, err := encryptECB([]byte(neteaseEAPIKey), []byte(`{"code":200,"yrc":{"lyric":"[0,1000](0,500,0)你(500,500,0)好"}}`))
		if err != nil {
			t.Fatal(err)
		}
		return responseFor(request, http.StatusOK, "application/octet-stream", response), nil
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Lyric(context.Background(), SourceNetease, Candidate{ID: "123"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result != "[00:00.00]<00:00.00>你<00:00.50>好" {
		t.Fatalf("verbatim lyrics = %q", result)
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
	if result != "[00:00.00]<00:00.00>你<00:00.50>好" {
		t.Fatalf("verbatim lyrics = %q", result)
	}
}

func TestKugouVerbatimLyricSearchesAndDownloadsKRC(t *testing.T) {
	krc := "[0,1000]<0,500,0>你<500,500,0>好"
	encrypted, err := encryptKRCForTest([]byte(krc))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v1/search":
			query := request.URL.Query()
			for _, key := range []string{"album_audio_id", "duration", "hash", "keyword", "lrctxt", "man", "appid", "clientver", "signature"} {
				if query.Get(key) == "" {
					t.Fatalf("missing Kugou lyric search parameter %q: %s", key, request.URL.String())
				}
			}
			if query.Get("appid") != "3116" || query.Get("clientver") != "11070" {
				t.Fatalf("unexpected Kugou lyric search client parameters: %s", request.URL.String())
			}
			return responseFor(request, http.StatusOK, "application/json", []byte(`{"candidates":[{"id":"lyric-id","accesskey":"access-key"}]} `)), nil
		case "/download":
			query := request.URL.Query()
			if query.Get("id") != "lyric-id" || query.Get("accesskey") != "access-key" || query.Get("fmt") != "krc" ||
				query.Get("appid") != "3116" || query.Get("clientver") != "11070" || query.Get("signature") == "" {
				t.Fatalf("unexpected Kugou download query: %s", request.URL.String())
			}
			body, _ := json.Marshal(map[string]any{"contenttype": 1, "content": base64.StdEncoding.EncodeToString(encrypted)})
			return responseFor(request, http.StatusOK, "application/json", body), nil
		default:
			t.Fatalf("unexpected Kugou lyric request: %s", request.URL.String())
			return nil, nil
		}
	})}
	platform := NewMusicPlatformClient(client, "")
	result, err := platform.Lyric(context.Background(), SourceKugou, Candidate{
		ID: "file-hash", LyricID: "audio-id", Name: "歌曲", Artist: "歌手", DurationMS: 1_000,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result != "[00:00.00]<00:00.00>你<00:00.50>好" {
		t.Fatalf("verbatim lyrics = %q", result)
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

func encryptKRCForTest(content []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	encrypted := append([]byte("krc1"), compressed.Bytes()...)
	for index := 4; index < len(encrypted); index++ {
		encrypted[index] ^= krcKey[(index-4)%len(krcKey)]
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
