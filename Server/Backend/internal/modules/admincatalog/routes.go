package admincatalog

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type API interface {
	ListArtists(context.Context, ListInput) (ArtistPageDTO, error)
	Artist(context.Context, string) (ArtistDTO, error)
	ListAlbums(context.Context, ListInput) (AlbumPageDTO, error)
	DuplicateAlbums(context.Context, DuplicateAlbumInput) (DuplicateAlbumsDTO, error)
	Album(context.Context, string, PageInput) (AlbumDetailDTO, error)
	ListTracks(context.Context, TrackListInput) (TrackPageDTO, error)
	Track(context.Context, string, PageInput) (TrackDetailDTO, error)
}

type Routes struct {
	service  API
	identity adminauth.Identity
}

func NewRoutes(service API, identity adminauth.Identity) (*Routes, error) {
	if service == nil {
		return nil, errors.New("admin catalog query API is required")
	}
	if identity == nil {
		return nil, errors.New("admin catalog query identity service is required")
	}
	return &Routes{service: service, identity: identity}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.GET("/artists", httpserver.Handle(routes.listArtists))
	admin.GET("/artists/:id", httpserver.Handle(routes.artist))
	admin.GET("/albums", httpserver.Handle(routes.listAlbums))
	admin.GET("/albums/duplicates", httpserver.Handle(routes.duplicateAlbums))
	admin.GET("/albums/:id", httpserver.Handle(routes.album))
	admin.GET("/tracks", httpserver.Handle(routes.listTracks))
	admin.GET("/tracks/:id", httpserver.Handle(routes.track))
}

func (routes *Routes) listArtists(c *gin.Context) error {
	input, err := bindList(c, []string{"name", "createdAt", "updatedAt"})
	if err != nil {
		return err
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.ListArtists(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) artist(c *gin.Context) error {
	id, err := queryUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.Artist(c.Request.Context(), id)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) listAlbums(c *gin.Context) error {
	input, err := bindList(c, []string{"title", "createdAt", "updatedAt", "releaseDate"})
	if err != nil {
		return err
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.ListAlbums(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) duplicateAlbums(c *gin.Context) error {
	input, err := bindDuplicateAlbums(c)
	if err != nil {
		return err
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.DuplicateAlbums(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) album(c *gin.Context) error {
	id, err := queryUUID(c.Param("id"))
	if err != nil {
		return err
	}
	page, err := bindPage(c)
	if err != nil {
		return err
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.Album(c.Request.Context(), id, page)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func bindPage(c *gin.Context) (PageInput, error) {
	page, err := queryOptionalInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return PageInput{}, err
	}
	pageSize, err := queryOptionalInteger(c, "pageSize", 1, 100)
	if err != nil {
		return PageInput{}, err
	}
	return PageInput{Page: page, PageSize: pageSize}, nil
}

func bindDuplicateAlbums(c *gin.Context) (DuplicateAlbumInput, error) {
	page, err := bindPage(c)
	if err != nil {
		return DuplicateAlbumInput{}, err
	}
	albumPage, err := queryOptionalInteger(c, "albumPage", 1, pagination.MaxPage)
	if err != nil {
		return DuplicateAlbumInput{}, err
	}
	albumPageSize, err := queryOptionalInteger(c, "albumPageSize", 1, 100)
	if err != nil {
		return DuplicateAlbumInput{}, err
	}
	albumID, present := httpserver.LastQueryValue(c, "albumId")
	if present {
		albumID, err = queryUUID(albumID)
		if err != nil {
			return DuplicateAlbumInput{}, err
		}
	}
	return DuplicateAlbumInput{
		PageInput: page, AlbumID: albumID, AlbumPage: albumPage, AlbumPageSize: albumPageSize,
	}, nil
}

func (routes *Routes) listTracks(c *gin.Context) error {
	base, err := bindList(c, []string{"title", "createdAt", "updatedAt", "status"})
	if err != nil {
		return err
	}
	statusValue, statusPresent := httpserver.LastQueryValue(c, "status")
	status := AudioStatus(statusValue)
	if statusPresent && !validAudioStatusFilter(status) {
		return queryContractError()
	}
	metadataValue, metadataPresent := httpserver.LastQueryValue(c, "metadataStatus")
	metadataStatus := MetadataStatus(metadataValue)
	if metadataPresent && !validMetadataStatusFilter(metadataStatus) {
		return queryContractError()
	}
	sourceID, sourcePresent := httpserver.LastQueryValue(c, "sourceId")
	if sourcePresent {
		if _, err := queryUUID(sourceID); err != nil {
			return err
		}
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.ListTracks(c.Request.Context(), TrackListInput{
		ListInput: base, Status: status, MetadataStatus: metadataStatus, SourceID: sourceID,
	})
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) track(c *gin.Context) error {
	id, err := queryUUID(c.Param("id"))
	if err != nil {
		return err
	}
	page, err := bindLyricPage(c)
	if err != nil {
		return err
	}
	if err := routes.authenticate(c); err != nil {
		return err
	}
	result, err := routes.service.Track(c.Request.Context(), id, page)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func bindLyricPage(c *gin.Context) (PageInput, error) {
	page, err := queryOptionalInteger(c, "lyricPage", 1, pagination.MaxPage)
	if err != nil {
		return PageInput{}, err
	}
	pageSize, err := queryOptionalInteger(c, "lyricPageSize", 1, 100)
	if err != nil {
		return PageInput{}, err
	}
	return PageInput{Page: page, PageSize: pageSize}, nil
}

func (routes *Routes) authenticate(c *gin.Context) error {
	_, err := adminauth.RequireAdmin(c, routes.identity, false)
	return err
}

func bindList(c *gin.Context, allowedSorts []string) (ListInput, error) {
	page, err := queryOptionalInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return ListInput{}, err
	}
	pageSize, err := queryOptionalInteger(c, "pageSize", 1, 100)
	if err != nil {
		return ListInput{}, err
	}
	search, searchPresent := httpserver.LastQueryValue(c, "search")
	if searchPresent && javascriptLength(search) > 300 {
		return ListInput{}, queryContractError()
	}
	sortValue, sortPresent := httpserver.LastQueryValue(c, "sort")
	if sortPresent && !containsString(allowedSorts, sortValue) {
		return ListInput{}, queryContractError()
	}
	orderValue, orderPresent := httpserver.LastQueryValue(c, "order")
	order := SortOrder(orderValue)
	if orderPresent && !validOrder(order) {
		return ListInput{}, queryContractError()
	}
	return ListInput{Page: page, PageSize: pageSize, Search: search, Sort: sortValue, Order: order}, nil
}

func queryOptionalInteger(c *gin.Context, name string, minimum, maximum int) (int, error) {
	raw, present := httpserver.LastQueryValue(c, name)
	if !present {
		return 0, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value < float64(minimum) || value > float64(maximum) || math.Abs(value) > float64(1<<53-1) {
		return 0, queryContractError()
	}
	return int(value), nil
}

func queryUUID(value string) (string, error) {
	if !queryUUIDPattern.MatchString(value) {
		return "", queryContractError()
	}
	return value, nil
}

func queryContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func javascriptLength(value string) int { return len(utf16.Encode([]rune(value))) }

var queryUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
