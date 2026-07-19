package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type API interface {
	ListTracks(ctx context.Context, userID string, input ListTracksInput) (TrackPageDTO, error)
	RandomTracks(ctx context.Context, userID string, requestedLimit int) (RandomTracksDTO, error)
	GetTrack(ctx context.Context, userID, trackID string, input GetTrackInput) (TrackDetailDTO, error)
	ListArtists(ctx context.Context, input ListArtistsInput) (ArtistPageDTO, error)
	GetArtist(ctx context.Context, artistID string) (ArtistDetailDTO, error)
	ListAlbums(ctx context.Context, input ListAlbumsInput) (AlbumPageDTO, error)
	RandomAlbums(ctx context.Context, requestedLimit int) (RandomAlbumsDTO, error)
	GetAlbum(ctx context.Context, albumID string) (AlbumDetailDTO, error)
	Search(ctx context.Context, userID string, input SearchInput) (SearchResultDTO, error)
}

// Authenticator intentionally returns only the user ID needed by catalog.
// The identity module can be connected with a one-line adapter without making
// catalog depend on identity's actor model.
type Authenticator interface {
	Authenticate(ctx context.Context, authorization string) (userID string, err error)
}

type AuthenticateFunc func(ctx context.Context, authorization string) (userID string, err error)

func (function AuthenticateFunc) Authenticate(ctx context.Context, authorization string) (string, error) {
	return function(ctx, authorization)
}

type Routes struct {
	service       API
	authenticator Authenticator
}

func NewRoutes(service API, authenticator Authenticator) (*Routes, error) {
	if service == nil {
		return nil, errors.New("catalog API service is required")
	}
	if authenticator == nil {
		return nil, errors.New("catalog authenticator is required")
	}
	return &Routes{service: service, authenticator: authenticator}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	api := router.Group("/api/v1")
	api.GET("/tracks", httpserver.Handle(routes.listTracks))
	api.POST("/tracks/random", httpserver.Handle(routes.randomTracks))
	api.GET("/tracks/:id", httpserver.Handle(routes.getTrack))
	api.GET("/artists", httpserver.Handle(routes.listArtists))
	api.GET("/artists/:id", httpserver.Handle(routes.getArtist))
	api.GET("/albums", httpserver.Handle(routes.listAlbums))
	api.POST("/albums/random", httpserver.Handle(routes.randomAlbums))
	api.GET("/albums/:id", httpserver.Handle(routes.getAlbum))
	api.GET("/search", httpserver.Handle(routes.search))
}

func (routes *Routes) listTracks(c *gin.Context) error {
	input, err := bindListTracks(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	input.Limit, err = optionalPageLimit(c)
	if err != nil {
		return err
	}
	result, err := routes.service.ListTracks(c.Request.Context(), userID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) randomTracks(c *gin.Context) error {
	limit, err := bindRandomLimit(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.RandomTracks(c.Request.Context(), userID, limit)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) getTrack(c *gin.Context) error {
	id, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	input, err := bindGetTrack(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.GetTrack(c.Request.Context(), userID, id, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func bindGetTrack(c *gin.Context) (GetTrackInput, error) {
	page, err := optionalInteger(c, "lyricPage", 1, pagination.MaxPage)
	if err != nil {
		return GetTrackInput{}, err
	}
	pageSize, err := optionalInteger(c, "lyricPageSize", 1, maximumPageLimit)
	if err != nil {
		return GetTrackInput{}, err
	}
	return GetTrackInput{LyricPage: page, LyricPageSize: pageSize}, nil
}

func (routes *Routes) listArtists(c *gin.Context) error {
	input, err := bindListArtists(c)
	if err != nil {
		return err
	}
	if _, err := routes.authenticate(c); err != nil {
		return err
	}
	input.Limit, err = optionalPageLimit(c)
	if err != nil {
		return err
	}
	result, err := routes.service.ListArtists(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) getArtist(c *gin.Context) error {
	id, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	if _, err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.GetArtist(c.Request.Context(), id)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) listAlbums(c *gin.Context) error {
	input, err := bindListAlbums(c)
	if err != nil {
		return err
	}
	if _, err := routes.authenticate(c); err != nil {
		return err
	}
	input.Limit, err = optionalPageLimit(c)
	if err != nil {
		return err
	}
	result, err := routes.service.ListAlbums(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) randomAlbums(c *gin.Context) error {
	limit, err := bindRandomLimit(c)
	if err != nil {
		return err
	}
	if _, err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.RandomAlbums(c.Request.Context(), limit)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) getAlbum(c *gin.Context) error {
	id, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	if _, err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.GetAlbum(c.Request.Context(), id)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) search(c *gin.Context) error {
	input, err := bindSearch(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	if input.Scope != SearchScopeAll {
		input.Limit, err = optionalPageLimit(c)
		if err != nil {
			return err
		}
	}
	result, err := routes.service.Search(c.Request.Context(), userID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) authenticate(c *gin.Context) (string, error) {
	return routes.authenticator.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
}

func bindListTracks(c *gin.Context) (ListTracksInput, error) {
	sort := TrackSortPublishedDesc
	if raw, exists := httpserver.LastQueryValue(c, "sort"); exists {
		sort = TrackSort(raw)
		if !validTrackSort(sort) {
			return ListTracksInput{}, routeValidationError()
		}
	}
	cursor, err := optionalCursor(c)
	if err != nil {
		return ListTracksInput{}, err
	}
	artistID, err := optionalUUID(c, "artistId")
	if err != nil {
		return ListTracksInput{}, err
	}
	albumID, err := optionalUUID(c, "albumId")
	if err != nil {
		return ListTracksInput{}, err
	}
	return ListTracksInput{Sort: sort, Cursor: cursor, ArtistID: artistID, AlbumID: albumID}, nil
}

func bindListArtists(c *gin.Context) (ListArtistsInput, error) {
	sort := ArtistSortNameAsc
	if raw, exists := httpserver.LastQueryValue(c, "sort"); exists {
		sort = ArtistSort(raw)
		if !validArtistSort(sort) {
			return ListArtistsInput{}, routeValidationError()
		}
	}
	cursor, err := optionalCursor(c)
	if err != nil {
		return ListArtistsInput{}, err
	}
	return ListArtistsInput{Sort: sort, Cursor: cursor}, nil
}

func bindListAlbums(c *gin.Context) (ListAlbumsInput, error) {
	sort := AlbumSortReleaseDateDesc
	if raw, exists := httpserver.LastQueryValue(c, "sort"); exists {
		sort = AlbumSort(raw)
		if !validAlbumSort(sort) {
			return ListAlbumsInput{}, routeValidationError()
		}
	}
	cursor, err := optionalCursor(c)
	if err != nil {
		return ListAlbumsInput{}, err
	}
	artistID, err := optionalUUID(c, "artistId")
	if err != nil {
		return ListAlbumsInput{}, err
	}
	return ListAlbumsInput{Sort: sort, Cursor: cursor, ArtistID: artistID}, nil
}

func bindSearch(c *gin.Context) (SearchInput, error) {
	query, exists := httpserver.LastQueryValue(c, "q")
	if !exists || query == "" || javascriptStringLength(query) > 200 {
		return SearchInput{}, routeValidationError()
	}
	rawScope, exists := httpserver.LastQueryValue(c, "scope")
	if !exists || !validSearchScope(SearchScope(rawScope)) {
		return SearchInput{}, routeValidationError()
	}
	cursor, err := optionalCursor(c)
	if err != nil {
		return SearchInput{}, err
	}
	return SearchInput{Query: query, Scope: SearchScope(rawScope), Cursor: cursor}, nil
}

type randomLimitRequest struct {
	Limit int `json:"limit"`
}

func bindRandomLimit(c *gin.Context) (int, error) {
	var body randomLimitRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&body); err != nil {
		return 0, routeValidationError()
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return 0, routeValidationError()
	}
	if _, err := randomLimit(body.Limit); err != nil {
		return 0, routeValidationError()
	}
	return body.Limit, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func optionalCursor(c *gin.Context) (string, error) {
	value, exists := httpserver.LastQueryValue(c, "cursor")
	if !exists {
		return "", nil
	}
	length := javascriptStringLength(value)
	if length < 1 || length > 512 {
		return "", routeValidationError()
	}
	return value, nil
}

func optionalPageLimit(c *gin.Context) (*int, error) {
	raw, exists := httpserver.LastQueryValue(c, "limit")
	if !exists || raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1 || value > maximumPageLimit {
		return nil, apperror.Validation("limit is invalid")
	}
	converted := int(value)
	return &converted, nil
}

func optionalInteger(c *gin.Context, name string, minimum, maximum int) (int, error) {
	raw, exists := httpserver.LastQueryValue(c, name)
	if !exists || raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value < float64(minimum) || value > float64(maximum) {
		return 0, apperror.Validation(name + " is invalid")
	}
	return int(value), nil
}

func optionalUUID(c *gin.Context, name string) (string, error) {
	value, exists := httpserver.LastQueryValue(c, name)
	if !exists {
		return "", nil
	}
	return bindUUID(value, name)
}

var canonicalUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func bindUUID(value, field string) (string, error) {
	if !canonicalUUID.MatchString(value) {
		return "", routeValidationError()
	}
	return value, nil
}

func routeValidationError() error {
	return apperror.Validation("请求参数不符合接口要求")
}
