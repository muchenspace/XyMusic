package admincatalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
)

func TestRoutesExposeSevenAdminCatalogQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &catalogAPIStub{calls: make(map[string]int)}
	identityService := &catalogIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, err := NewRoutes(api, identityService)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-4000-8000-000000000001"
	paths := []string{
		"/api/v1/admin/artists?page=bad&page=1&pageSize=bad&pageSize=25&sort=invalid&sort=name&order=invalid&order=asc&unknown=true",
		"/api/v1/admin/artists/" + id,
		"/api/v1/admin/albums?sort=invalid&sort=releaseDate&order=invalid&order=desc&unknown=true",
		"/api/v1/admin/albums/duplicates?page=bad&page=2&pageSize=bad&pageSize=10&albumPage=bad&albumPage=3&albumPageSize=bad&albumPageSize=50&albumId=bad&albumId=" + id,
		"/api/v1/admin/albums/" + id + "?page=bad&page=3&pageSize=bad&pageSize=15",
		"/api/v1/admin/tracks?status=INVALID&status=PROCESSING&metadataStatus=INVALID&metadataStatus=ORIGINAL&sourceId=bad&sourceId=" + id + "&sort=invalid&sort=status&order=invalid&order=asc&unknown=true",
		"/api/v1/admin/tracks/" + id + "?lyricPage=bad&lyricPage=2&lyricPageSize=bad&lyricPageSize=50",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer admin")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	for _, name := range []string{"artists", "artist", "albums", "duplicates", "album", "tracks", "track"} {
		if api.calls[name] != 1 {
			t.Fatalf("%s calls=%d", name, api.calls[name])
		}
	}
	if identityService.calls != 7 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
	if api.duplicateInput.Page != 2 || api.duplicateInput.PageSize != 10 || api.duplicateInput.AlbumID != id ||
		api.duplicateInput.AlbumPage != 3 || api.duplicateInput.AlbumPageSize != 50 {
		t.Fatalf("duplicate input=%#v", api.duplicateInput)
	}
	if api.albumInput.Page != 3 || api.albumInput.PageSize != 15 {
		t.Fatalf("album input=%#v", api.albumInput)
	}
	if api.trackInput.Page != 2 || api.trackInput.PageSize != 50 {
		t.Fatalf("track lyric input=%#v", api.trackInput)
	}
}

func TestRouteSchemaValidationPrecedesAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &catalogAPIStub{calls: make(map[string]int)}
	identityService := &catalogIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(api, identityService)
	engine := gin.New()
	routes.Register(engine)
	paths := []string{
		"/api/v1/admin/artists?page=0",
		"/api/v1/admin/albums?sort=invalid",
		"/api/v1/admin/tracks?status=PENDING",
		"/api/v1/admin/tracks?metadataStatus=INVALID",
		"/api/v1/admin/tracks?sourceId=bad",
		"/api/v1/admin/artists/not-a-uuid",
		"/api/v1/admin/albums/duplicates?albumId=bad",
		"/api/v1/admin/albums/duplicates?albumPage=0",
		"/api/v1/admin/albums/duplicates?albumPageSize=101",
		"/api/v1/admin/albums/00000000-0000-4000-8000-000000000001?page=0",
		"/api/v1/admin/tracks/00000000-0000-4000-8000-000000000001?lyricPage=0",
		"/api/v1/admin/tracks/00000000-0000-4000-8000-000000000001?lyricPageSize=101",
	}
	for _, path := range paths {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	if identityService.calls != 0 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
}

type catalogAPIStub struct {
	calls          map[string]int
	duplicateInput DuplicateAlbumInput
	albumInput     PageInput
	trackInput     PageInput
}

func (stub *catalogAPIStub) ListArtists(context.Context, ListInput) (ArtistPageDTO, error) {
	stub.calls["artists"]++
	return ArtistPageDTO{Items: []ArtistDTO{}}, nil
}
func (stub *catalogAPIStub) Artist(context.Context, string) (ArtistDTO, error) {
	stub.calls["artist"]++
	return ArtistDTO{}, nil
}
func (stub *catalogAPIStub) ListAlbums(context.Context, ListInput) (AlbumPageDTO, error) {
	stub.calls["albums"]++
	return AlbumPageDTO{Items: []AlbumDTO{}}, nil
}
func (stub *catalogAPIStub) DuplicateAlbums(_ context.Context, input DuplicateAlbumInput) (DuplicateAlbumsDTO, error) {
	stub.calls["duplicates"]++
	stub.duplicateInput = input
	return DuplicateAlbumsDTO{Groups: []DuplicateAlbumGroupDTO{}}, nil
}
func (stub *catalogAPIStub) Album(_ context.Context, _ string, input PageInput) (AlbumDetailDTO, error) {
	stub.calls["album"]++
	stub.albumInput = input
	return AlbumDetailDTO{Tracks: []TrackDTO{}}, nil
}
func (stub *catalogAPIStub) ListTracks(context.Context, TrackListInput) (TrackPageDTO, error) {
	stub.calls["tracks"]++
	return TrackPageDTO{Items: []TrackDTO{}}, nil
}
func (stub *catalogAPIStub) Track(_ context.Context, _ string, input PageInput) (TrackDetailDTO, error) {
	stub.calls["track"]++
	stub.trackInput = input
	return TrackDetailDTO{Lyrics: []LyricDTO{}, Variants: []VariantDTO{}}, nil
}

type catalogIdentityStub struct {
	actor identity.AuthenticatedActor
	err   error
	calls int
}

func (stub *catalogIdentityStub) Authenticate(context.Context, string) (identity.AuthenticatedActor, error) {
	stub.calls++
	return stub.actor, stub.err
}
func (*catalogIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*catalogIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*catalogIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*catalogIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}
