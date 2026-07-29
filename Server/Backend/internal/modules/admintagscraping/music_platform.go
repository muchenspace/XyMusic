package admintagscraping

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/lyrics"
)

const (
	platformUserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126 Safari/537.36"
	defaultUpstreamTimeout = 12 * time.Second
	searchCacheTTL         = 24 * time.Hour
	lyricsCacheTTL         = 7 * 24 * time.Hour
	artworkCacheTTL        = 7 * 24 * time.Hour
	maximumArtworkBytes    = int64(20 * 1024 * 1024)
	maximumJSONBytes       = int64(2 * 1024 * 1024)
	maximumTextBytes       = int64(2 * 1024 * 1024)
	maximumRequestAttempts = 3
	maximumRedirects       = 5
	maximumSourceFailures  = 3
	maximumSourceCooldown  = 2 * time.Second
	defaultArtworkWorkers  = 8
	neteaseLinuxForwardKey = "rFgB&h#%2?^eDg:Q"
	kugouSignatureSalt     = "NVPh5oo715z5DIWAeQlhMDsWXXQV4hwt"
)

var allowedArtworkHosts = []string{"music.126.net", "y.qq.com", "kugou.com"}

var sourceBaseIntervals = map[Source]time.Duration{
	SourceQMusic:   10 * time.Millisecond,
	SourceNetease:  5 * time.Millisecond,
	SourceKugou:    10 * time.Millisecond,
	SourceAcoustID: 50 * time.Millisecond,
}

type ProductionMusicPlatform struct {
	client         *http.Client
	acoustIDClient string
	gate           *requestGate
	sourceGates    map[Source]*requestGate
	artworkGate    *requestGate
	artworkMu      sync.Mutex
	artworkCalls   map[string]*artworkCall
	artworkCache   map[string]cachedArtwork
	metrics        PerformanceMetrics
	searchMu       sync.Mutex
	searchCache    map[string]cachedSearch
	searchCalls    map[string]*searchCall
	artistMu       sync.Mutex
	artistCache    map[string]cachedArtists
	artistCalls    map[string]*artistSearchCall
	lyricsMu       sync.Mutex
	lyricsCache    map[string]cachedLyric
	lyricsCalls    map[string]*lyricLookupCall
	circuitMu      sync.Mutex
	circuitOpen    map[string]time.Time
	sourceStates   map[Source]*sourceCircuitState
}

var _ MusicPlatform = (*ProductionMusicPlatform)(nil)

type artworkCall struct {
	done   chan struct{}
	result DownloadedArtwork
	err    error
}

type cachedArtwork struct {
	result    DownloadedArtwork
	expiresAt time.Time
}

type cachedSearch struct {
	result    []Candidate
	expiresAt time.Time
}

type searchCall struct {
	done   chan struct{}
	result []Candidate
	err    error
}

type cachedArtists struct {
	result    []ArtistCandidate
	expiresAt time.Time
}

type artistSearchCall struct {
	done   chan struct{}
	result []ArtistCandidate
	err    error
}

type cachedLyric struct {
	result    LyricResult
	expiresAt time.Time
}

type lyricLookupCall struct {
	done   chan struct{}
	result LyricResult
	err    error
}

type sourceCircuitState struct {
	consecutiveFailures int
	openUntil           time.Time
	nextRequestAt       time.Time
	cooldown            time.Duration
	total               uint64
	errors              uint64
}

func NewMusicPlatformClient(client *http.Client, acoustIDClient string, metrics ...PerformanceMetrics) *ProductionMusicPlatform {
	return NewMusicPlatformClientWithArtworkWorkers(client, acoustIDClient, defaultArtworkWorkers, metrics...)
}

func NewMusicPlatformClientWithArtworkWorkers(
	client *http.Client,
	acoustIDClient string,
	artworkWorkers int,
	metrics ...PerformanceMetrics,
) *ProductionMusicPlatform {
	if client == nil {
		client = &http.Client{}
	}
	if artworkWorkers < 1 || artworkWorkers > 64 {
		artworkWorkers = defaultArtworkWorkers
	}
	var observer PerformanceMetrics
	if len(metrics) > 0 {
		observer = metrics[0]
	}
	return &ProductionMusicPlatform{
		client: client, acoustIDClient: strings.TrimSpace(acoustIDClient),
		metrics: observer,
		gate:    newRequestGate(6, 128), sourceGates: map[Source]*requestGate{
			SourceQMusic: newRequestGate(512, 512), SourceNetease: newRequestGate(1024, 1024),
			SourceKugou: newRequestGate(128, 512), SourceAcoustID: newRequestGate(64, 64),
		}, artworkGate: newRequestGate(artworkWorkers, 0),
		artworkCalls: make(map[string]*artworkCall), artworkCache: make(map[string]cachedArtwork),
		searchCache: make(map[string]cachedSearch), searchCalls: make(map[string]*searchCall),
		artistCache: make(map[string]cachedArtists), artistCalls: make(map[string]*artistSearchCall),
		lyricsCache: make(map[string]cachedLyric), lyricsCalls: make(map[string]*lyricLookupCall),
		circuitOpen: make(map[string]time.Time), sourceStates: make(map[Source]*sourceCircuitState),
	}
}

func (platform *ProductionMusicPlatform) Search(ctx context.Context, source Source, query string) ([]Candidate, error) {
	key := string(source) + "\x00" + strings.TrimSpace(query)
	platform.searchMu.Lock()
	if cached, ok := platform.searchCache[key]; ok {
		if time.Now().Before(cached.expiresAt) {
			result := cloneCandidates(cached.result)
			platform.observeCache("search", true)
			platform.searchMu.Unlock()
			return result, nil
		}
		delete(platform.searchCache, key)
	}
	call := platform.searchCalls[key]
	if call == nil {
		platform.observeCache("search", false)
		call = &searchCall{done: make(chan struct{})}
		platform.searchCalls[key] = call
		platform.searchMu.Unlock()
		call.result, call.err = platform.searchUncached(withPlatformSource(ctx, source), source, query)
		if call.err == nil {
			platform.searchMu.Lock()
			platform.searchCache[key] = cachedSearch{result: cloneCandidates(call.result), expiresAt: time.Now().Add(searchCacheTTL)}
			platform.searchMu.Unlock()
		}
		close(call.done)
		platform.searchMu.Lock()
		delete(platform.searchCalls, key)
		platform.searchMu.Unlock()
		return cloneCandidates(call.result), call.err
	}
	platform.searchMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-call.done:
		return cloneCandidates(call.result), call.err
	}
}

func (platform *ProductionMusicPlatform) searchUncached(ctx context.Context, source Source, query string) ([]Candidate, error) {
	var result []Candidate
	var err error
	switch source {
	case SourceNetease:
		result, err = platform.searchNetease(ctx, query)
	case SourceQMusic:
		result, err = platform.searchQQ(ctx, query)
	case SourceKugou:
		result, err = platform.searchKugou(ctx, query)
	default:
		return nil, apperror.Validation("The music platform source is invalid")
	}
	if err != nil {
		return nil, normalizeUpstreamError(err, ctx)
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) Lyric(ctx context.Context, source Source, candidate Candidate, verbatim bool) (LyricResult, error) {
	key := strings.Join([]string{
		string(source), candidate.ID, candidate.LyricID,
		normalizeForTagMatch(candidate.Artist), tagVersionSignature(candidate.Name),
		strconv.FormatBool(verbatim),
	}, "\x00")
	platform.lyricsMu.Lock()
	if cached, ok := platform.lyricsCache[key]; ok {
		if time.Now().Before(cached.expiresAt) {
			platform.observeCache("lyrics", true)
			platform.lyricsMu.Unlock()
			return cached.result, nil
		}
		delete(platform.lyricsCache, key)
	}
	call := platform.lyricsCalls[key]
	if call == nil {
		platform.observeCache("lyrics", false)
		call = &lyricLookupCall{done: make(chan struct{})}
		platform.lyricsCalls[key] = call
		platform.lyricsMu.Unlock()
		call.result, call.err = platform.lyricUncached(withPlatformSource(ctx, source), source, candidate, verbatim)
		if call.err == nil {
			platform.lyricsMu.Lock()
			platform.lyricsCache[key] = cachedLyric{result: call.result, expiresAt: time.Now().Add(lyricsCacheTTL)}
			platform.lyricsMu.Unlock()
		}
		close(call.done)
		platform.lyricsMu.Lock()
		delete(platform.lyricsCalls, key)
		platform.lyricsMu.Unlock()
		return call.result, call.err
	}
	platform.lyricsMu.Unlock()
	select {
	case <-ctx.Done():
		return LyricResult{}, ctx.Err()
	case <-call.done:
		return call.result, call.err
	}
}

func (platform *ProductionMusicPlatform) lyricUncached(ctx context.Context, source Source, candidate Candidate, verbatim bool) (LyricResult, error) {
	var result string
	var err error
	switch source {
	case SourceNetease:
		result, err = platform.lyricNetease(ctx, candidate.ID)
	case SourceQMusic:
		result, err = platform.lyricQQ(ctx, candidate, verbatim)
	case SourceKugou:
		result, err = platform.lyricKugou(ctx, candidate)
	default:
		return LyricResult{}, apperror.Validation("The music platform source is invalid")
	}
	if err != nil {
		return LyricResult{}, normalizeUpstreamError(err, ctx)
	}
	timing := lyrics.DetectTiming("LRC", result)
	return LyricResult{Content: result, Timing: timing}, nil
}

func (platform *ProductionMusicPlatform) AcoustID(
	ctx context.Context,
	duration float64,
	fingerprint string,
) ([]Candidate, error) {
	if platform == nil || strings.TrimSpace(platform.acoustIDClient) == "" {
		return nil, apperror.DependencyUnavailable("Audio fingerprinting is not configured: set the AcoustID Client ID")
	}
	parameters := url.Values{
		"format":      {"json"},
		"client":      {platform.acoustIDClient},
		"duration":    {strconv.FormatInt(int64(math.Floor(duration)), 10)},
		"fingerprint": {fingerprint},
		"meta":        {"recordings releasegroups"},
	}
	data, err := platform.requestJSON(withPlatformSource(ctx, SourceAcoustID), "https://api.acoustid.org/v2/lookup?"+parameters.Encode(), requestOptions{Timeout: 20 * time.Second})
	if err != nil {
		return nil, normalizeUpstreamError(err, ctx)
	}
	result := make([]Candidate, 0)
	for _, match := range sliceValue(data["results"]) {
		for _, recordingValue := range sliceValue(mapValue(match)["recordings"]) {
			recording := mapValue(recordingValue)
			group := map[string]any{}
			if groups := sliceValue(recording["releasegroups"]); len(groups) > 0 {
				group = mapValue(groups[0])
			}
			artists := strings.Builder{}
			for _, artistValue := range sliceValue(recording["artists"]) {
				artist := mapValue(artistValue)
				artists.WriteString(stringValue(artist["name"]))
				artists.WriteString(stringValue(artist["joinphrase"]))
			}
			result = append(result, normalizeCandidate(map[string]any{
				"id": recording["id"], "name": recording["title"], "artist": artists.String(),
				"album": group["title"], "albumId": group["id"],
			}, SourceAcoustID))
		}
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) DownloadArtwork(ctx context.Context, rawURL string) (DownloadedArtwork, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || validateArtworkURL(parsed) != nil {
		return DownloadedArtwork{}, apperror.Validation("The artwork URL is invalid or untrusted")
	}
	normalized := parsed.String()
	host := strings.ToLower(parsed.Hostname())
	if retryAfter := platform.circuitRetryAfter(host); retryAfter > 0 {
		return DownloadedArtwork{}, dependencyUnavailable("The artwork host is temporarily unavailable", retryAfter)
	}

	platform.artworkMu.Lock()
	if cached, ok := platform.artworkCache[normalized]; ok {
		if time.Now().Before(cached.expiresAt) {
			platform.observeCache("artwork", true)
			result := cached.result
			result.Bytes = append([]byte(nil), result.Bytes...)
			platform.artworkMu.Unlock()
			return result, nil
		}
		delete(platform.artworkCache, normalized)
	}
	call := platform.artworkCalls[normalized]
	if call == nil {
		platform.observeCache("artwork", false)
		call = &artworkCall{done: make(chan struct{})}
		platform.artworkCalls[normalized] = call
		go platform.performArtwork(normalized, host, call)
	}
	platform.artworkMu.Unlock()
	select {
	case <-ctx.Done():
		return DownloadedArtwork{}, ctx.Err()
	case <-call.done:
		if call.err != nil {
			return DownloadedArtwork{}, normalizeUpstreamError(call.err, ctx)
		}
		result := call.result
		result.Bytes = append([]byte(nil), result.Bytes...)
		return result, nil
	}
}

func (platform *ProductionMusicPlatform) performArtwork(rawURL, host string, call *artworkCall) {
	defer func() {
		close(call.done)
		platform.artworkMu.Lock()
		if platform.artworkCalls[rawURL] == call {
			delete(platform.artworkCalls, rawURL)
		}
		platform.artworkMu.Unlock()
	}()
	ctx := context.Background()
	if err := platform.artworkGate.acquire(ctx); err != nil {
		call.err = err
		return
	}
	defer platform.artworkGate.release()
	result, err := platform.downloadArtworkRequest(ctx, rawURL)
	if err != nil {
		call.err = err
		if transientUpstreamFailure(err) {
			platform.openCircuit(host, 30*time.Second)
		}
		return
	}
	platform.closeCircuit(host)
	call.result = result
	platform.artworkMu.Lock()
	platform.artworkCache[rawURL] = cachedArtwork{result: result, expiresAt: time.Now().Add(artworkCacheTTL)}
	platform.artworkMu.Unlock()
}

func (platform *ProductionMusicPlatform) downloadArtworkRequest(ctx context.Context, rawURL string) (DownloadedArtwork, error) {
	parsed, _ := url.Parse(rawURL)
	response, err := platform.requestBytes(ctx, rawURL, requestOptions{
		Headers:     map[string]string{"Referer": parsed.Scheme + "://" + parsed.Host + "/", "Accept": "image/*"},
		ValidateURL: validateArtworkURL,
	}, maximumArtworkBytes)
	if err != nil {
		return DownloadedArtwork{}, err
	}
	if len(response.Bytes) == 0 {
		return DownloadedArtwork{}, errors.New("artwork response is empty")
	}
	contentType, extension, err := detectArtwork(response.Bytes)
	if err != nil {
		return DownloadedArtwork{}, err
	}
	declared := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if declared != "" && declared != "image/jpeg" && declared != "image/png" && declared != "image/webp" {
		return DownloadedArtwork{}, errors.New("artwork MIME type is unsupported")
	}
	if declared != "" && declared != contentType {
		return DownloadedArtwork{}, errors.New("artwork content does not match its MIME type")
	}
	return DownloadedArtwork{Bytes: response.Bytes, ContentType: contentType, Extension: extension}, nil
}

func (platform *ProductionMusicPlatform) searchNetease(ctx context.Context, query string) ([]Candidate, error) {
	data, err := platform.neteaseForward(ctx, "https://music.163.com/api/cloudsearch/pc", map[string]any{
		"s": query, "type": 1, "limit": 10, "offset": 0,
	})
	if err != nil {
		return nil, err
	}
	result := make([]Candidate, 0)
	for _, songValue := range sliceValue(mapValue(data["result"])["songs"]) {
		song := mapValue(songValue)
		artists := make([]string, 0)
		artistID := ""
		for index, artistValue := range sliceValue(song["ar"]) {
			artist := mapValue(artistValue)
			artists = append(artists, stringValue(artist["name"]))
			if index == 0 {
				artistID = stringValue(artist["id"])
			}
		}
		album := mapValue(song["al"])
		year := ""
		if milliseconds := numberValue(song["publishTime"]); milliseconds > 0 {
			year = strconv.Itoa(time.UnixMilli(int64(milliseconds)).UTC().Year())
		}
		result = append(result, normalizeCandidate(map[string]any{
			"id": song["id"], "name": song["name"], "artist": strings.Join(artists, ","), "artistId": artistID,
			"album": album["name"], "albumId": album["id"], "albumImg": album["picUrl"], "year": year,
			"durationMs": numberValue(song["dt"]),
		}, SourceNetease))
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) lyricNetease(ctx context.Context, id string) (string, error) {
	data, err := platform.neteaseForward(ctx, "https://music.163.com/api/song/lyric?lv=-1&kv=-1&tv=-1", map[string]any{"id": id})
	if err != nil {
		return "", err
	}
	return cleanScrapedText(mapValue(data["lrc"])["lyric"]), nil
}

func (platform *ProductionMusicPlatform) neteaseForward(ctx context.Context, endpoint string, parameters map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(struct {
		Method string         `json:"method"`
		URL    string         `json:"url"`
		Params map[string]any `json:"params"`
	}{Method: http.MethodPost, URL: endpoint, Params: parameters})
	if err != nil {
		return nil, err
	}
	encrypted, err := encryptECB([]byte(neteaseLinuxForwardKey), payload)
	if err != nil {
		return nil, err
	}
	body := []byte(url.Values{"eparams": {strings.ToUpper(hex.EncodeToString(encrypted))}}.Encode())
	return platform.requestJSON(ctx, "https://music.163.com/api/linux/forward", requestOptions{
		Method:  http.MethodPost,
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": "https://music.163.com/"},
		Body:    body,
	})
}

func (platform *ProductionMusicPlatform) searchQQ(ctx context.Context, query string) ([]Candidate, error) {
	parameters := url.Values{"format": {"json"}, "p": {"1"}, "n": {"10"}, "w": {query}}
	data, err := platform.requestJSON(ctx, "https://c.y.qq.com/soso/fcgi-bin/client_search_cp?"+parameters.Encode(), requestOptions{Headers: map[string]string{"Referer": "https://y.qq.com/"}})
	if err != nil {
		return nil, err
	}
	result := make([]Candidate, 0)
	songs := sliceValue(mapValue(mapValue(data["data"])["song"])["list"])
	for _, songValue := range songs {
		song := mapValue(songValue)
		artists := make([]string, 0)
		artistID := ""
		for index, artistValue := range sliceValue(song["singer"]) {
			artist := mapValue(artistValue)
			artists = append(artists, stringValue(artist["name"]))
			if index == 0 {
				artistID = stringValue(artist["mid"])
			}
		}
		albumID := stringValue(song["albummid"])
		image := ""
		if albumID != "" {
			image = "https://y.qq.com/music/photo_new/T002R300x300M000" + albumID + ".jpg"
		}
		year := ""
		if seconds := numberValue(song["pubtime"]); seconds > 0 {
			year = time.Unix(int64(seconds), 0).UTC().Format("2006-01-02")
		}
		result = append(result, normalizeCandidate(map[string]any{
			"id": song["songmid"], "name": song["songname"], "artist": strings.Join(artists, ","), "artistId": artistID,
			"album": song["albumname"], "albumId": albumID, "albumImg": image, "year": year,
			"lyricId": song["songid"], "durationMs": numberValue(song["interval"]) * 1_000,
		}, SourceQMusic))
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) lyricQQ(ctx context.Context, candidate Candidate, verbatim bool) (string, error) {
	if verbatim {
		return platform.lyricQQVerbatim(ctx, candidate)
	}
	id := candidate.ID
	endpoint := "https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg?g_tk=5381&format=json&inCharset=utf-8&outCharset=utf-8&notice=0&platform=h5&needNewCode=1&ct=121&cv=0&songmid=" + url.QueryEscape(id)
	data, err := platform.requestJSON(ctx, endpoint, requestOptions{Headers: map[string]string{"Referer": "https://y.qq.com/"}})
	if err != nil {
		return "", err
	}
	encoded := stringValue(data["lyric"])
	if encoded == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (platform *ProductionMusicPlatform) lyricQQVerbatim(ctx context.Context, candidate Candidate) (string, error) {
	seconds := max(0, int(math.Round(float64(candidate.DurationMS)/1_000)))
	songID := candidate.LyricID
	if songID == "" {
		songID = candidate.ID
	}
	parameters := map[string]any{
		"albumName": base64.StdEncoding.EncodeToString([]byte(candidate.Album)),
		"crypt":     1, "ct": 19, "cv": 2111, "interval": seconds,
		"lrc_t": 0, "qrc": 1, "qrc_t": 0, "roma": 1, "roma_t": 0,
		"singerName": base64.StdEncoding.EncodeToString([]byte(candidate.Artist)),
		"songID":     numberOrString(songID),
		"songName":   base64.StdEncoding.EncodeToString([]byte(candidate.Name)),
		"trans":      1, "trans_t": 0, "type": 0,
	}
	body, err := json.Marshal(map[string]any{
		"comm": map[string]any{
			"ct": 11, "cv": "1003006", "v": "1003006", "os_ver": "15",
			"phonetype": "24122RKC7C", "tmeAppID": "qqmusiclight", "nettype": "NETWORK_WIFI", "udid": "0",
		},
		"request": map[string]any{
			"method": "GetPlayLyricInfo", "module": "music.musichallSong.PlayLyricInfo", "param": parameters,
		},
	})
	if err != nil {
		return "", err
	}
	data, err := platform.requestJSON(ctx, "https://u.y.qq.com/cgi-bin/musicu.fcg", requestOptions{
		Method:  http.MethodPost,
		Headers: map[string]string{"Content-Type": "application/json", "Referer": "https://y.qq.com/"},
		Body:    body,
	})
	if err != nil {
		return "", err
	}
	requestData := mapValue(data["request"])
	if numberValue(requestData["code"]) != 0 {
		return "", fmt.Errorf("QQ lyric request failed with code %s", stringValue(requestData["code"]))
	}
	lyricData := mapValue(requestData["data"])
	encoded := stringValue(lyricData["lyric"])
	if encoded == "" {
		return "", nil
	}
	decrypted, err := decryptQRC(encoded)
	if err != nil {
		return "", err
	}
	document, err := parseQRC(string(decrypted))
	if err != nil {
		return "", err
	}
	return renderEnhancedLRC(document), nil
}

func (platform *ProductionMusicPlatform) searchKugou(ctx context.Context, query string) ([]Candidate, error) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signed := kugouSignatureSalt + "bitrate=0clienttime=" + timestamp + "clientver=2000dfid=-inputtype=0iscorrection=1isfuzzy=0keyword=" + query + "mid=" + timestamp + "page=1pagesize=10platform=WebFilterprivilege_filter=0srcappid=2919tag=emuserid=-1uuid=" + timestamp + kugouSignatureSalt
	digest := md5.Sum([]byte(signed))
	parameters := url.Values{
		"keyword": {query}, "page": {"1"}, "pagesize": {"10"}, "bitrate": {"0"}, "isfuzzy": {"0"},
		"tag": {"em"}, "inputtype": {"0"}, "platform": {"WebFilter"}, "userid": {"-1"}, "clientver": {"2000"},
		"iscorrection": {"1"}, "privilege_filter": {"0"}, "srcappid": {"2919"}, "clienttime": {timestamp},
		"mid": {timestamp}, "uuid": {timestamp}, "dfid": {"-"}, "signature": {strings.ToUpper(hex.EncodeToString(digest[:]))},
	}
	data, err := platform.requestJSON(ctx, "https://complexsearch.kugou.com/v2/search/song?"+parameters.Encode(), requestOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]Candidate, 0)
	for _, songValue := range sliceValue(mapValue(data["data"])["lists"]) {
		song := mapValue(songValue)
		result = append(result, normalizeCandidate(map[string]any{
			"id": song["FileHash"], "name": song["SongName"], "artist": strings.Join(splitArtists(cleanScrapedText(song["SingerName"])), ","),
			"artistId": song["SingerId"], "album": song["AlbumName"], "albumId": song["AlbumID"],
			"albumImg": strings.ReplaceAll(stringValue(song["Image"]), "{size}", "400"), "year": song["PublishTime"],
			"lyricId":    firstNonEmpty(song["ID"], song["Audioid"], song["AudioID"], song["MixSongID"]),
			"durationMs": numberValue(song["Duration"]) * 1_000,
		}, SourceKugou))
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) lyricKugou(ctx context.Context, candidate Candidate) (string, error) {
	response, err := platform.requestBytes(ctx, "https://m.kugou.com/app/i/krc.php?cmd=100&timelength=999999&hash="+url.QueryEscape(candidate.ID), requestOptions{}, maximumTextBytes)
	return string(response.Bytes), err
}

type requestOptions struct {
	Method      string
	Headers     map[string]string
	Body        []byte
	Timeout     time.Duration
	ValidateURL func(*url.URL) error
}

type byteResponse struct {
	Bytes  []byte
	Header http.Header
	URL    string
}

func (platform *ProductionMusicPlatform) requestJSON(ctx context.Context, rawURL string, options requestOptions) (map[string]any, error) {
	response, err := platform.requestBytes(ctx, rawURL, options, maximumJSONBytes)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(response.Bytes, &result); err != nil {
		platform.recordSourceFailure(platformSource(ctx), &upstreamEmptyResultError{message: "upstream JSON is malformed"})
		return nil, fmt.Errorf("decode upstream JSON: %w", err)
	}
	if result == nil || len(result) == 0 {
		err := &upstreamEmptyResultError{message: "upstream JSON result is empty"}
		platform.recordSourceFailure(platformSource(ctx), err)
		return nil, err
	}
	return result, nil
}

func (platform *ProductionMusicPlatform) requestBytes(
	ctx context.Context,
	rawURL string,
	options requestOptions,
	maximumBytes int64,
) (byteResponse, error) {
	gate := platform.gate
	source := platformSource(ctx)
	metricName := string(source)
	if metricName == "" && options.ValidateURL != nil {
		metricName = "artwork"
	}
	if source != "" {
		if sourceGate := platform.sourceGates[source]; sourceGate != nil {
			gate = sourceGate
		}
		if err := platform.acquireSource(ctx, source); err != nil {
			platform.observePlatform(metricName, true)
			return byteResponse{}, err
		}
	}
	if err := gate.acquire(ctx); err != nil {
		platform.observePlatform(metricName, true)
		return byteResponse{}, err
	}
	defer gate.release()
	method := options.Method
	if method == "" {
		method = http.MethodGet
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultUpstreamTimeout
	}
	for attempt := 1; attempt <= maximumRequestAttempts; attempt++ {
		attemptContext, cancel := context.WithTimeout(ctx, timeout)
		request, err := http.NewRequestWithContext(attemptContext, method, rawURL, bytes.NewReader(options.Body))
		if err != nil {
			cancel()
			return byteResponse{}, &upstreamPolicyError{message: "upstream URL is invalid"}
		}
		request.Header.Set("User-Agent", platformUserAgent)
		request.Header.Set("Accept", "application/json,text/plain,*/*")
		for name, value := range options.Headers {
			request.Header.Set(name, value)
		}
		client := platform.client
		if options.ValidateURL != nil {
			clone := *platform.client
			clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
				if len(via) > maximumRedirects {
					return &upstreamPolicyError{message: "too many upstream redirects"}
				}
				return options.ValidateURL(request.URL)
			}
			client = &clone
			if err := options.ValidateURL(request.URL); err != nil {
				cancel()
				return byteResponse{}, err
			}
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			cancel()
			if ctx.Err() != nil {
				platform.observePlatform(metricName, true)
				return byteResponse{}, ctx.Err()
			}
			if attemptContext.Err() == context.DeadlineExceeded {
				requestErr = &upstreamTimeoutError{}
			}
			if attempt < maximumRequestAttempts && retryableRequestError(requestErr) {
				if err := sleepContext(ctx, retryDelay(attempt, "")); err != nil {
					return byteResponse{}, err
				}
				continue
			}
			platform.recordSourceFailure(source, requestErr)
			platform.observePlatform(metricName, true)
			return byteResponse{}, requestErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			retryAfter := response.Header.Get("Retry-After")
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4_096))
			response.Body.Close()
			cancel()
			requestErr = &upstreamHTTPError{status: response.StatusCode, retryAfter: retryAfter}
			if attempt < maximumRequestAttempts && retryableStatus(response.StatusCode) {
				if err := sleepContext(ctx, retryDelay(attempt, retryAfter)); err != nil {
					return byteResponse{}, err
				}
				continue
			}
			platform.recordSourceFailure(source, requestErr)
			platform.observePlatform(metricName, true)
			return byteResponse{}, requestErr
		}
		body, readErr := readBoundedBody(response, maximumBytes)
		finalURL := response.Request.URL.String()
		header := response.Header.Clone()
		cancel()
		if readErr != nil {
			platform.recordSourceFailure(source, readErr)
			platform.observePlatform(metricName, true)
			return byteResponse{}, readErr
		}
		platform.recordSourceSuccess(source)
		platform.observePlatform(metricName, false)
		return byteResponse{Bytes: body, Header: header, URL: finalURL}, nil
	}
	return byteResponse{}, errors.New("upstream request attempts exhausted")
}

func readBoundedBody(response *http.Response, maximumBytes int64) ([]byte, error) {
	defer response.Body.Close()
	if response.ContentLength > maximumBytes {
		return nil, &upstreamBodyTooLargeError{maximum: maximumBytes}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maximumBytes {
		return nil, &upstreamBodyTooLargeError{maximum: maximumBytes}
	}
	return body, nil
}

type requestGate struct {
	semaphore chan struct{}
	mu        sync.Mutex
	waiting   int
	maximum   int
}

type platformSourceContextKey struct{}

func withPlatformSource(ctx context.Context, source Source) context.Context {
	return context.WithValue(ctx, platformSourceContextKey{}, source)
}

func platformSource(ctx context.Context) Source {
	if ctx == nil {
		return ""
	}
	if source, ok := ctx.Value(platformSourceContextKey{}).(Source); ok {
		return source
	}
	return ""
}

func cloneCandidates(values []Candidate) []Candidate {
	if values == nil {
		return nil
	}
	return append([]Candidate(nil), values...)
}

func cloneArtistCandidates(values []ArtistCandidate) []ArtistCandidate {
	if values == nil {
		return nil
	}
	result := make([]ArtistCandidate, len(values))
	for index, value := range values {
		result[index] = value
		if value.Aliases != nil {
			result[index].Aliases = make([]string, len(value.Aliases))
			copy(result[index].Aliases, value.Aliases)
		}
	}
	return result
}

func newRequestGate(limit, maximumWaiting int) *requestGate {
	return &requestGate{semaphore: make(chan struct{}, limit), maximum: maximumWaiting}
}

func (gate *requestGate) acquire(ctx context.Context) error {
	select {
	case gate.semaphore <- struct{}{}:
		return nil
	default:
	}
	gate.mu.Lock()
	if gate.maximum > 0 && gate.waiting >= gate.maximum {
		gate.mu.Unlock()
		return &upstreamQueueFullError{}
	}
	gate.waiting++
	gate.mu.Unlock()
	defer func() {
		gate.mu.Lock()
		gate.waiting--
		gate.mu.Unlock()
	}()
	select {
	case gate.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (gate *requestGate) release() { <-gate.semaphore }

func (gate *requestGate) snapshot() (actual, waiting, limit int) {
	if gate == nil {
		return 0, 0, 0
	}
	gate.mu.Lock()
	waiting = gate.waiting
	gate.mu.Unlock()
	return len(gate.semaphore), waiting, cap(gate.semaphore)
}

func (platform *ProductionMusicPlatform) ArtworkHealth() ArtworkHealth {
	if platform == nil {
		return ArtworkHealth{}
	}
	actual, waiting, limit := platform.artworkGate.snapshot()
	return ArtworkHealth{Limit: limit, Actual: actual, Waiting: waiting}
}

func (platform *ProductionMusicPlatform) circuitRetryAfter(host string) int {
	platform.circuitMu.Lock()
	defer platform.circuitMu.Unlock()
	until := platform.circuitOpen[host]
	if until.IsZero() || !until.After(time.Now()) {
		delete(platform.circuitOpen, host)
		return 0
	}
	return max(1, int(math.Ceil(time.Until(until).Seconds())))
}

func (platform *ProductionMusicPlatform) openCircuit(host string, duration time.Duration) {
	platform.circuitMu.Lock()
	platform.circuitOpen[host] = time.Now().Add(duration)
	platform.circuitMu.Unlock()
}

func (platform *ProductionMusicPlatform) closeCircuit(host string) {
	platform.circuitMu.Lock()
	delete(platform.circuitOpen, host)
	platform.circuitMu.Unlock()
}

type upstreamHTTPError struct {
	status     int
	retryAfter string
}

func (err *upstreamHTTPError) Error() string {
	return fmt.Sprintf("upstream returned HTTP %d", err.status)
}

type upstreamBodyTooLargeError struct{ maximum int64 }

func (err *upstreamBodyTooLargeError) Error() string {
	return fmt.Sprintf("upstream response exceeded %d bytes", err.maximum)
}

type upstreamPolicyError struct{ message string }

func (err *upstreamPolicyError) Error() string { return err.message }

type upstreamTimeoutError struct{}

func (err *upstreamTimeoutError) Error() string { return "upstream request timed out" }

type upstreamQueueFullError struct{}

func (err *upstreamQueueFullError) Error() string { return "upstream request queue is full" }

type upstreamEmptyResultError struct{ message string }

func (err *upstreamEmptyResultError) Error() string { return err.message }

type sourceCircuitError struct {
	source     Source
	retryAfter int
}

func (err *sourceCircuitError) Error() string {
	return fmt.Sprintf("music platform %s circuit is open", err.source)
}

func (platform *ProductionMusicPlatform) acquireSource(ctx context.Context, source Source) error {
	for {
		platform.circuitMu.Lock()
		state := platform.sourceStates[source]
		if state == nil {
			state = &sourceCircuitState{}
			platform.sourceStates[source] = state
		}
		now := time.Now()
		if state.openUntil.After(now) {
			retryAfter := max(1, int(math.Ceil(time.Until(state.openUntil).Seconds())))
			platform.circuitMu.Unlock()
			return &sourceCircuitError{source: source, retryAfter: retryAfter}
		}
		if !state.openUntil.IsZero() {
			state.openUntil = time.Time{}
		}
		readyAt := state.nextRequestAt
		if readyAt.Before(now) {
			readyAt = now
		}
		interval := sourceBaseIntervals[source]
		state.nextRequestAt = readyAt.Add(interval)
		wait := readyAt.Sub(now)
		platform.circuitMu.Unlock()
		if wait <= 0 {
			return nil
		}
		if err := sleepContext(ctx, wait); err != nil {
			return err
		}
	}
}

func (platform *ProductionMusicPlatform) recordSourceSuccess(source Source) {
	if source == "" {
		return
	}
	platform.circuitMu.Lock()
	defer platform.circuitMu.Unlock()
	state := platform.sourceStates[source]
	if state == nil {
		state = &sourceCircuitState{}
		platform.sourceStates[source] = state
	}
	state.total++
	state.consecutiveFailures = 0
	state.cooldown = 0
	state.openUntil = time.Time{}
}

func (platform *ProductionMusicPlatform) recordSourceFailure(source Source, err error) {
	if source == "" || err == nil || !sourceFailure(err) {
		return
	}
	platform.circuitMu.Lock()
	defer platform.circuitMu.Unlock()
	state := platform.sourceStates[source]
	if state == nil {
		state = &sourceCircuitState{}
		platform.sourceStates[source] = state
	}
	state.total++
	state.errors++
	state.consecutiveFailures++
	if state.cooldown <= 0 {
		state.cooldown = 100 * time.Millisecond
	} else {
		state.cooldown *= 2
		if state.cooldown > maximumSourceCooldown {
			state.cooldown = maximumSourceCooldown
		}
	}
	if httpError := new(upstreamHTTPError); errors.As(err, &httpError) && httpError.retryAfter != "" {
		state.cooldown = maxDuration(state.cooldown, retryDelay(1, httpError.retryAfter))
	}
	next := time.Now().Add(state.cooldown)
	if state.nextRequestAt.Before(next) {
		state.nextRequestAt = next
	}
	if state.consecutiveFailures >= maximumSourceFailures {
		state.openUntil = time.Now().Add(30 * time.Second)
	}
}

func sourceFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var policy *upstreamPolicyError
	var tooLarge *upstreamBodyTooLargeError
	var queueFull *upstreamQueueFullError
	var circuit *sourceCircuitError
	if errors.As(err, &policy) || errors.As(err, &tooLarge) || errors.As(err, &queueFull) || errors.As(err, &circuit) {
		return false
	}
	return true
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func (platform *ProductionMusicPlatform) ChannelHealth(source Source) ChannelHealth {
	platform.circuitMu.Lock()
	state := platform.sourceStates[source]
	var snapshot sourceCircuitState
	if state != nil {
		snapshot = *state
	}
	platform.circuitMu.Unlock()
	status := "READY"
	retryAfter := 0
	if snapshot.openUntil.After(time.Now()) {
		status = "OPEN"
		retryAfter = max(1, int(math.Ceil(time.Until(snapshot.openUntil).Seconds())))
	} else if snapshot.consecutiveFailures > 0 {
		status = "DEGRADED"
	}
	if gate := platform.sourceGates[source]; gate != nil {
		return ChannelHealth{
			State: status, Actual: len(gate.semaphore), RetryAfterSeconds: retryAfter,
			ConsecutiveFailures: snapshot.consecutiveFailures,
			ErrorRate:           ratioFloat(snapshot.errors, snapshot.total),
		}
	}
	return ChannelHealth{State: status, RetryAfterSeconds: retryAfter,
		ConsecutiveFailures: snapshot.consecutiveFailures, ErrorRate: ratioFloat(snapshot.errors, snapshot.total)}
}

func (platform *ProductionMusicPlatform) observeCache(name string, hit bool) {
	if platform.metrics != nil {
		platform.metrics.ObserveCache(name, hit)
	}
}

func (platform *ProductionMusicPlatform) observePlatform(source string, failed bool) {
	if platform.metrics != nil && source != "" {
		platform.metrics.ObservePlatform(source, failed)
	}
}

func ratioFloat(numerator, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func normalizeUpstreamError(err error, ctx context.Context) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if _, ok := apperror.As(err); ok {
		return err
	}
	var policy *upstreamPolicyError
	if errors.As(err, &policy) {
		return apperror.Validation("The music platform URL is invalid or untrusted")
	}
	var tooLarge *upstreamBodyTooLargeError
	if errors.As(err, &tooLarge) {
		return apperror.PayloadTooLarge("The music platform response exceeds the permitted size")
	}
	var httpError *upstreamHTTPError
	if errors.As(err, &httpError) && httpError.status == http.StatusTooManyRequests {
		return apperror.RateLimited(retryAfterSeconds(httpError.retryAfter))
	}
	var circuit *sourceCircuitError
	if errors.As(err, &circuit) {
		return dependencyUnavailable("The music platform channel is temporarily paused", circuit.retryAfter)
	}
	var empty *upstreamEmptyResultError
	if errors.As(err, &empty) {
		return dependencyUnavailable("The music platform returned an invalid empty response", 1)
	}
	var queueFull *upstreamQueueFullError
	if errors.As(err, &queueFull) {
		return dependencyUnavailable("The music platform is busy; retry later", 1)
	}
	var timeout *upstreamTimeoutError
	if errors.As(err, &timeout) {
		return dependencyUnavailable("The music platform request timed out", 1)
	}
	return dependencyUnavailable("The music platform is temporarily unavailable", 1)
}

func dependencyUnavailable(detail string, retryAfter int) error {
	return apperror.New(apperror.CodeDependencyUnavailable, detail, apperror.WithMetadata(map[string]any{"retryAfterSeconds": max(1, retryAfter)}))
}

func transientUpstreamFailure(err error) bool {
	var timeout *upstreamTimeoutError
	if errors.As(err, &timeout) {
		return true
	}
	var httpError *upstreamHTTPError
	return errors.As(err, &httpError) && retryableStatus(httpError.status)
}

func retryableRequestError(err error) bool {
	var policy *upstreamPolicyError
	var tooLarge *upstreamBodyTooLargeError
	return !errors.As(err, &policy) && !errors.As(err, &tooLarge)
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds > 0 {
			return min(1_500*time.Millisecond, time.Duration(seconds*float64(time.Second)))
		}
		if timestamp, err := http.ParseTime(retryAfter); err == nil && timestamp.After(time.Now()) {
			return min(1_500*time.Millisecond, time.Until(timestamp))
		}
	}
	return min(1_500*time.Millisecond, 200*time.Millisecond*time.Duration(1<<(attempt-1)))
}

func retryAfterSeconds(value string) int {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return max(1, int(math.Ceil(seconds)))
	}
	if timestamp, err := http.ParseTime(value); err == nil {
		return max(1, int(math.Ceil(time.Until(timestamp).Seconds())))
	}
	return 1
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validateArtworkURL(parsed *url.URL) error {
	if parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !allowedHost(parsed.Hostname(), allowedArtworkHosts) {
		return &upstreamPolicyError{message: "artwork URL is untrusted"}
	}
	return nil
}

func allowedHost(hostname string, allowed []string) bool {
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, domain := range allowed {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}
	return false
}

func detectArtwork(bytes []byte) (string, string, error) {
	if len(bytes) >= 3 && bytes[0] == 0xff && bytes[1] == 0xd8 && bytes[2] == 0xff {
		return "image/jpeg", "jpg", nil
	}
	if len(bytes) >= 4 && bytes[0] == 0x89 && bytes[1] == 0x50 && bytes[2] == 0x4e && bytes[3] == 0x47 {
		return "image/png", "png", nil
	}
	if len(bytes) >= 12 && string(bytes[:4]) == "RIFF" && string(bytes[8:12]) == "WEBP" {
		return "image/webp", "webp", nil
	}
	return "", "", errors.New("artwork content is not a supported image")
}

func encryptECB(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := block.BlockSize() - len(plaintext)%block.BlockSize()
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	result := make([]byte, len(padded))
	for offset := 0; offset < len(padded); offset += block.BlockSize() {
		block.Encrypt(result[offset:offset+block.BlockSize()], padded[offset:offset+block.BlockSize()])
	}
	return result, nil
}

func normalizeCandidate(value map[string]any, source Source) Candidate {
	return Candidate{
		ID: stringValue(value["id"]), Name: cleanScrapedText(value["name"]), Artist: cleanScrapedText(value["artist"]),
		ArtistID: stringValue(value["artistId"]), Album: cleanScrapedText(value["album"]), AlbumID: stringValue(value["albumId"]),
		AlbumImg: stringValue(value["albumImg"]), Year: cleanScrapedText(value["year"]), Track: cleanScrapedText(value["track"]),
		Disc: cleanScrapedText(value["disc"]), Genre: cleanScrapedText(value["genre"]), Source: source,
		LyricID:    stringValue(value["lyricId"]),
		DurationMS: int(math.Max(0, math.Round(numberValue(value["durationMs"])))),
	}
}

func firstNonEmpty(values ...any) string {
	for _, value := range values {
		if result := stringValue(value); result != "" {
			return result
		}
	}
	return ""
}

func numberOrString(value string) any {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return parsed
	}
	return value
}

func mapValue(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func sliceValue(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		result, _ := typed.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(typed, 64)
		return result
	default:
		return 0
	}
}
