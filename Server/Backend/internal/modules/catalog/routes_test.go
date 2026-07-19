package catalog

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/shared/apperror"
)

func TestRoutesRegisterAllNineCatalogEndpointsAndAuthenticate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &apiStub{calls: make(map[string]int)}
	auth := &authStub{userID: "user-1"}
	engine := gin.New()
	routes, err := NewRoutes(api, auth)
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	routes.Register(engine)

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/tracks?sort=INVALID&sort=PUBLISHED_DESC&cursor=&cursor=track-cursor&unknown=true", ""},
		{http.MethodPost, "/api/v1/tracks/random", `{"limit":3}`},
		{http.MethodGet, "/api/v1/tracks/00000000-0000-0000-0000-000000000001?lyricPage=bad&lyricPage=2&lyricPageSize=bad&lyricPageSize=50", ""},
		{http.MethodGet, "/api/v1/artists?sort=INVALID&sort=NAME_ASC&cursor=&cursor=artist-cursor&unknown=true", ""},
		{http.MethodGet, "/api/v1/artists/00000000-0000-0000-0000-000000000002", ""},
		{http.MethodGet, "/api/v1/albums?sort=INVALID&sort=RELEASE_DATE_DESC&cursor=&cursor=album-cursor&unknown=true", ""},
		{http.MethodPost, "/api/v1/albums/random", `{"limit":4}`},
		{http.MethodGet, "/api/v1/albums/00000000-0000-0000-0000-000000000003", ""},
		{http.MethodGet, "/api/v1/search?q=&q=hello&scope=INVALID&scope=ALL&cursor=&cursor=search-cursor&limit=not-a-number&unknown=true", ""},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Authorization", "Bearer test-token")
		if item.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, body = %s", item.method, item.path, response.Code, response.Body.String())
		}
	}
	if auth.calls != 9 || auth.lastAuthorization != "Bearer test-token" {
		t.Fatalf("auth calls/header = %d/%q", auth.calls, auth.lastAuthorization)
	}
	for _, name := range []string{
		"listTracks", "randomTracks", "getTrack", "listArtists", "getArtist",
		"listAlbums", "randomAlbums", "getAlbum", "search",
	} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls = %d", name, api.calls[name])
		}
	}
	if api.trackInput.Sort != TrackSortPublishedDesc || api.artistInput.Sort != ArtistSortNameAsc || api.albumInput.Sort != AlbumSortReleaseDateDesc {
		t.Fatalf("browse defaults = %#v / %#v / %#v", api.trackInput, api.artistInput, api.albumInput)
	}
	if api.trackUserID != "user-1" || api.searchUserID != "user-1" || api.randomTrackLimit != 3 || api.randomAlbumLimit != 4 {
		t.Fatalf("route arguments = %#v", api)
	}
	if api.searchInput.Scope != SearchScopeAll || api.searchInput.Limit != nil {
		t.Fatalf("ALL search input = %#v", api.searchInput)
	}
	if api.getTrackInput.LyricPage != 2 || api.getTrackInput.LyricPageSize != 50 {
		t.Fatalf("track detail input = %#v", api.getTrackInput)
	}
}

func TestTrackRouteRejectsInvalidLyricPaginationBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &apiStub{calls: make(map[string]int)}
	auth := &authStub{userID: "user-1"}
	engine := gin.New()
	routes, _ := NewRoutes(api, auth)
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/00000000-0000-0000-0000-000000000001?lyricPageSize=101", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || auth.calls != 0 || api.calls["getTrack"] != 0 {
		t.Fatalf("status/auth/service = %d/%d/%d, body = %s", response.Code, auth.calls, api.calls["getTrack"], response.Body.String())
	}
}

func TestRoutesIgnoreUnknownRandomBodyFieldsLikeLegacyRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &apiStub{calls: make(map[string]int)}
	auth := &authStub{userID: "user-1"}
	engine := gin.New()
	routes, _ := NewRoutes(api, auth)
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/random", bytes.NewBufferString(`{"limit":2,"extra":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if auth.calls != 1 || api.calls["randomTracks"] != 1 {
		t.Fatalf("auth/service calls = %d/%d", auth.calls, api.calls["randomTracks"])
	}
}

func TestRoutesPropagateAuthenticationFailureAsProblem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &apiStub{calls: make(map[string]int)}
	auth := &authStub{err: apperror.Unauthorized(apperror.CodeAuthenticationRequired, "Authentication is required")}
	engine := gin.New()
	routes, _ := NewRoutes(api, auth)
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tracks", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("status/header = %d/%q, body = %s", response.Code, response.Header().Get("WWW-Authenticate"), response.Body.String())
	}
	if api.calls["listTracks"] != 0 {
		t.Fatalf("service calls = %d", api.calls["listTracks"])
	}
}

type authStub struct {
	userID            string
	err               error
	calls             int
	lastAuthorization string
}

func (auth *authStub) Authenticate(_ context.Context, authorization string) (string, error) {
	auth.calls++
	auth.lastAuthorization = authorization
	return auth.userID, auth.err
}

type apiStub struct {
	calls map[string]int

	trackUserID      string
	searchUserID     string
	trackInput       ListTracksInput
	getTrackInput    GetTrackInput
	artistInput      ListArtistsInput
	albumInput       ListAlbumsInput
	searchInput      SearchInput
	randomTrackLimit int
	randomAlbumLimit int
}

func (api *apiStub) ListTracks(_ context.Context, userID string, input ListTracksInput) (TrackPageDTO, error) {
	api.calls["listTracks"]++
	api.trackUserID = userID
	api.trackInput = input
	return TrackPageDTO{Items: []TrackSummaryDTO{}}, nil
}

func (api *apiStub) RandomTracks(_ context.Context, userID string, requestedLimit int) (RandomTracksDTO, error) {
	api.calls["randomTracks"]++
	api.trackUserID = userID
	api.randomTrackLimit = requestedLimit
	return RandomTracksDTO{Items: []TrackSummaryDTO{}}, nil
}

func (api *apiStub) GetTrack(_ context.Context, userID, _ string, input GetTrackInput) (TrackDetailDTO, error) {
	api.calls["getTrack"]++
	api.trackUserID = userID
	api.getTrackInput = input
	return TrackDetailDTO{Lyrics: []LyricDTO{}}, nil
}

func (api *apiStub) ListArtists(_ context.Context, input ListArtistsInput) (ArtistPageDTO, error) {
	api.calls["listArtists"]++
	api.artistInput = input
	return ArtistPageDTO{Items: []ArtistSummaryDTO{}}, nil
}

func (api *apiStub) GetArtist(context.Context, string) (ArtistDetailDTO, error) {
	api.calls["getArtist"]++
	return ArtistDetailDTO{}, nil
}

func (api *apiStub) ListAlbums(_ context.Context, input ListAlbumsInput) (AlbumPageDTO, error) {
	api.calls["listAlbums"]++
	api.albumInput = input
	return AlbumPageDTO{Items: []AlbumSummaryDTO{}}, nil
}

func (api *apiStub) RandomAlbums(_ context.Context, requestedLimit int) (RandomAlbumsDTO, error) {
	api.calls["randomAlbums"]++
	api.randomAlbumLimit = requestedLimit
	return RandomAlbumsDTO{Items: []AlbumSummaryDTO{}}, nil
}

func (api *apiStub) GetAlbum(context.Context, string) (AlbumDetailDTO, error) {
	api.calls["getAlbum"]++
	return AlbumDetailDTO{}, nil
}

func (api *apiStub) Search(_ context.Context, userID string, input SearchInput) (SearchResultDTO, error) {
	api.calls["search"]++
	api.searchUserID = userID
	api.searchInput = input
	return SearchResultDTO{Query: input.Query, Scope: input.Scope}, nil
}
