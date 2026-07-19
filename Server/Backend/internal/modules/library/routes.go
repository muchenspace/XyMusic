package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
)

type API interface {
	ListFavorites(context.Context, string, ListFavoritesInput) (FavoritePageDTO, error)
	AddFavorite(context.Context, string, string) (FavoriteItemDTO, error)
	RemoveFavorite(context.Context, string, string) error
	ListHistory(context.Context, string, ListHistoryInput) (HistoryPageDTO, error)
	RecordPlayback(context.Context, string, string, string, RecordPlaybackInput) (MutationResult[HistoryItemDTO], error)
}

type Authenticator interface {
	Authenticate(context.Context, string) (string, error)
}

type AuthenticateFunc func(context.Context, string) (string, error)

func (function AuthenticateFunc) Authenticate(ctx context.Context, authorization string) (string, error) {
	return function(ctx, authorization)
}

type Routes struct {
	service       API
	authenticator Authenticator
}

func NewRoutes(service API, authenticator Authenticator) (*Routes, error) {
	if service == nil {
		return nil, errors.New("library API service is required")
	}
	if authenticator == nil {
		return nil, errors.New("library authenticator is required")
	}
	return &Routes{service: service, authenticator: authenticator}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	library := router.Group("/api/v1/library")
	library.GET("/favorites", httpserver.Handle(routes.listFavorites))
	library.PUT("/favorites/:trackId", httpserver.Handle(routes.addFavorite))
	library.DELETE("/favorites/:trackId", httpserver.Handle(routes.removeFavorite))
	library.GET("/history", httpserver.Handle(routes.listHistory))
	library.PUT("/history/:trackId", httpserver.Handle(routes.recordPlayback))
}

func (routes *Routes) listFavorites(c *gin.Context) error {
	input, err := bindListFavorites(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.ListFavorites(c.Request.Context(), userID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) addFavorite(c *gin.Context) error {
	trackID, err := bindLibraryUUID(c.Param("trackId"), "trackId")
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.AddFavorite(c.Request.Context(), userID, trackID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) removeFavorite(c *gin.Context) error {
	trackID, err := bindLibraryUUID(c.Param("trackId"), "trackId")
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	if err := routes.service.RemoveFavorite(c.Request.Context(), userID, trackID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (routes *Routes) listHistory(c *gin.Context) error {
	input, err := bindListHistory(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.ListHistory(c.Request.Context(), userID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) recordPlayback(c *gin.Context) error {
	trackID, err := bindLibraryUUID(c.Param("trackId"), "trackId")
	if err != nil {
		return err
	}
	input, err := decodePlaybackJSON(c)
	if err != nil {
		return err
	}
	userID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.RecordPlayback(
		c.Request.Context(),
		userID,
		trackID,
		c.GetHeader("Idempotency-Key"),
		input,
	)
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.JSON(http.StatusOK, result.Body)
	return nil
}

func (routes *Routes) authenticate(c *gin.Context) (string, error) {
	return routes.authenticator.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
}

func bindListFavorites(c *gin.Context) (ListFavoritesInput, error) {
	rawSort, exists := httpserver.LastQueryValue(c, "sort")
	if !exists || !validFavoriteSort(FavoriteSort(rawSort)) {
		return ListFavoritesInput{}, routeLibraryValidationError()
	}
	cursor, err := optionalLibraryCursor(c)
	if err != nil {
		return ListFavoritesInput{}, err
	}
	limit, err := optionalLibraryLimit(c)
	if err != nil {
		return ListFavoritesInput{}, err
	}
	return ListFavoritesInput{Cursor: cursor, Limit: limit, Sort: FavoriteSort(rawSort)}, nil
}

func bindListHistory(c *gin.Context) (ListHistoryInput, error) {
	cursor, err := optionalLibraryCursor(c)
	if err != nil {
		return ListHistoryInput{}, err
	}
	limit, err := optionalLibraryLimit(c)
	if err != nil {
		return ListHistoryInput{}, err
	}
	return ListHistoryInput{Cursor: cursor, Limit: limit}, nil
}

func optionalLibraryCursor(c *gin.Context) (string, error) {
	value, exists := httpserver.LastQueryValue(c, "cursor")
	if !exists {
		return "", nil
	}
	length := javascriptStringLength(value)
	if length < 1 || length > 512 {
		return "", routeLibraryValidationError()
	}
	return value, nil
}

func optionalLibraryLimit(c *gin.Context) (*int, error) {
	raw, exists := httpserver.LastQueryValue(c, "limit")
	if !exists || raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1 || value > maximumPageLimit {
		return nil, routeLibraryValidationError()
	}
	converted := int(value)
	return &converted, nil
}

type playbackJSON struct {
	PlaybackSessionID *string        `json:"playbackSessionId"`
	PositionMS        *json.Number   `json:"positionMs"`
	OccurredAt        *string        `json:"occurredAt"`
	Event             *PlaybackEvent `json:"event"`
}

func decodePlaybackJSON(c *gin.Context) (RecordPlaybackInput, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return RecordPlaybackInput{}, routeLibraryValidationError()
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return RecordPlaybackInput{}, apperror.PayloadTooLarge("Request body exceeds the permitted size")
		}
		return RecordPlaybackInput{}, routeLibraryParseError()
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return RecordPlaybackInput{}, routeLibraryValidationError()
	}
	if !json.Valid(raw) {
		return RecordPlaybackInput{}, routeLibraryParseError()
	}
	var body playbackJSON
	if err := json.Unmarshal(raw, &body); err != nil {
		return RecordPlaybackInput{}, routeLibraryValidationError()
	}
	if body.PlaybackSessionID == nil || body.PositionMS == nil || body.OccurredAt == nil || body.Event == nil {
		return RecordPlaybackInput{}, routeLibraryValidationError()
	}
	if _, err := bindLibraryUUID(*body.PlaybackSessionID, "playbackSessionId"); err != nil {
		return RecordPlaybackInput{}, err
	}
	positionMS, err := parsePlaybackPosition(*body.PositionMS)
	if err != nil || !validPlaybackEvent(*body.Event) {
		return RecordPlaybackInput{}, routeLibraryValidationError()
	}
	if _, err := time.Parse(time.RFC3339Nano, *body.OccurredAt); err != nil {
		return RecordPlaybackInput{}, routeLibraryValidationError()
	}
	return RecordPlaybackInput{
		PlaybackSessionID: *body.PlaybackSessionID,
		PositionMS:        positionMS,
		OccurredAt:        *body.OccurredAt,
		Event:             *body.Event,
	}, nil
}

func bindLibraryUUID(value, _ string) (string, error) {
	if !validLibraryUUID(value) {
		return "", routeLibraryValidationError()
	}
	return value, nil
}

func routeLibraryValidationError() error {
	return apperror.Validation("请求参数不符合接口要求")
}

func routeLibraryParseError() error {
	return apperror.Validation("请求内容无法解析")
}

func parsePlaybackPosition(value json.Number) (int64, error) {
	parsed, err := strconv.ParseFloat(value.String(), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Trunc(parsed) != parsed || parsed < 0 || parsed > float64(maximumSafeJSONInteger) {
		return 0, errors.New("positionMs is not a safe non-negative integer")
	}
	return int64(parsed), nil
}
