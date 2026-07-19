package playlist

import (
	"bytes"
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
)

type API interface {
	ListOwned(ctx context.Context, ownerID string, input ListOwnedInput) (PageDTO, error)
	Create(ctx context.Context, ownerID string, input CreateInput) (SummaryDTO, error)
	Get(ctx context.Context, requesterID, playlistID string, input GetInput) (DetailDTO, error)
	Update(ctx context.Context, ownerID, playlistID string, input UpdateInput) (SummaryDTO, error)
	Delete(ctx context.Context, ownerID, playlistID string, expectedVersion int) error
	AddTrack(ctx context.Context, ownerID, playlistID string, input AddTrackInput) (AddTrackDTO, error)
	RemoveTrack(ctx context.Context, ownerID, playlistID, entryID string, expectedVersion int) (VersionDTO, error)
	Reorder(ctx context.Context, ownerID, playlistID string, input ReorderInput) (VersionDTO, error)
}

type Routes struct {
	service       API
	authenticator Authenticator
	idempotency   Idempotency
}

func NewRoutes(service API, authenticator Authenticator, idempotency Idempotency) (*Routes, error) {
	if service == nil {
		return nil, errors.New("playlist API service is required")
	}
	if authenticator == nil {
		return nil, errors.New("playlist authenticator is required")
	}
	if idempotency == nil {
		return nil, errors.New("playlist idempotency service is required")
	}
	return &Routes{service: service, authenticator: authenticator, idempotency: idempotency}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	playlists := router.Group("/api/v1/playlists")
	playlists.GET("", httpserver.Handle(routes.listOwned))
	playlists.POST("", httpserver.Handle(routes.create))
	playlists.GET("/:id", httpserver.Handle(routes.get))
	playlists.PATCH("/:id", httpserver.Handle(routes.update))
	playlists.DELETE("/:id", httpserver.Handle(routes.delete))
	playlists.POST("/:id/tracks", httpserver.Handle(routes.addTrack))
	playlists.DELETE("/:id/tracks/:entryId", httpserver.Handle(routes.removeTrack))
	playlists.PATCH("/:id/tracks/order", httpserver.Handle(routes.reorder))
}

func (routes *Routes) listOwned(c *gin.Context) error {
	input, err := bindListOwned(c)
	if err != nil {
		return err
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.ListOwned(c.Request.Context(), ownerID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) create(c *gin.Context) error {
	var input CreateInput
	if err := decodePlaylistJSON(c, &input); err != nil {
		return err
	}
	if err := validateCreateRoute(input); err != nil {
		return err
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	return routes.executeMutation(c, ownerID, "playlist.create", input.Payload(), http.StatusCreated, func() (any, error) {
		return routes.service.Create(c.Request.Context(), ownerID, input)
	})
}

func (routes *Routes) get(c *gin.Context) error {
	playlistID, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	input, err := bindPageQuery(c)
	if err != nil {
		return err
	}
	requesterID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	result, err := routes.service.Get(c.Request.Context(), requesterID, playlistID, input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) update(c *gin.Context) error {
	playlistID, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	var input UpdateInput
	if err := decodePlaylistJSON(c, &input); err != nil {
		return err
	}
	if err := validateUpdateRoute(input); err != nil {
		return err
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	return routes.executeMutation(
		c,
		ownerID,
		"playlist.update:"+playlistID,
		input.Payload(),
		http.StatusOK,
		func() (any, error) { return routes.service.Update(c.Request.Context(), ownerID, playlistID, input) },
	)
}

func (routes *Routes) delete(c *gin.Context) error {
	playlistID, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	expectedVersion, err := bindExpectedVersion(c)
	if err != nil {
		return err
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	return routes.executeMutation(
		c,
		ownerID,
		"playlist.delete:"+playlistID,
		map[string]any{"expectedVersion": expectedVersion},
		http.StatusNoContent,
		func() (any, error) {
			if err := routes.service.Delete(c.Request.Context(), ownerID, playlistID, expectedVersion); err != nil {
				return nil, err
			}
			return struct{}{}, nil
		},
	)
}

func (routes *Routes) addTrack(c *gin.Context) error {
	playlistID, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	var input AddTrackInput
	if err := decodePlaylistJSON(c, &input); err != nil {
		return err
	}
	if input.ExpectedVersion < 1 {
		return routePlaylistValidationError()
	}
	if _, err := bindUUID(input.TrackID, "trackId"); err != nil {
		return err
	}
	if input.InsertAfterEntryID.Value != nil {
		if _, err := bindUUID(*input.InsertAfterEntryID.Value, "insertAfterEntryId"); err != nil {
			return err
		}
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	return routes.executeMutation(
		c,
		ownerID,
		"playlist.add-track:"+playlistID,
		input.Payload(),
		http.StatusCreated,
		func() (any, error) { return routes.service.AddTrack(c.Request.Context(), ownerID, playlistID, input) },
	)
}

func (routes *Routes) removeTrack(c *gin.Context) error {
	playlistID, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	entryID, err := bindUUID(c.Param("entryId"), "entryId")
	if err != nil {
		return err
	}
	expectedVersion, err := bindExpectedVersion(c)
	if err != nil {
		return err
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	return routes.executeMutation(
		c,
		ownerID,
		"playlist.remove-track:"+playlistID+":"+entryID,
		map[string]any{"expectedVersion": expectedVersion},
		http.StatusOK,
		func() (any, error) {
			return routes.service.RemoveTrack(c.Request.Context(), ownerID, playlistID, entryID, expectedVersion)
		},
	)
}

func (routes *Routes) reorder(c *gin.Context) error {
	playlistID, err := bindUUID(c.Param("id"), "id")
	if err != nil {
		return err
	}
	var input ReorderInput
	if err := decodePlaylistJSON(c, &input); err != nil {
		return err
	}
	if err := validateReorderRoute(input); err != nil {
		return err
	}
	for _, entryID := range input.OrderedEntryIDs.Values {
		if _, err := bindUUID(entryID, "orderedEntryIds"); err != nil {
			return err
		}
	}
	ownerID, err := routes.authenticate(c)
	if err != nil {
		return err
	}
	return routes.executeMutation(
		c,
		ownerID,
		"playlist.reorder:"+playlistID,
		input.Payload(),
		http.StatusOK,
		func() (any, error) { return routes.service.Reorder(c.Request.Context(), ownerID, playlistID, input) },
	)
}

func (routes *Routes) executeMutation(
	c *gin.Context,
	actorID, scope string,
	payload any,
	status int,
	operation func() (any, error),
) error {
	key := c.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	result, err := routes.idempotency.Execute(c.Request.Context(), IdempotencyInput{
		ActorID: actorID,
		Scope:   scope,
		Key:     key,
		Payload: payload,
	}, func() (IdempotencyResponse, error) {
		body, err := operation()
		if err != nil {
			return IdempotencyResponse{}, err
		}
		encoded, err := marshalBody(body)
		return IdempotencyResponse{Status: status, Body: encoded}, err
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	if status == http.StatusNoContent {
		c.Status(http.StatusNoContent)
		return nil
	}
	c.Data(status, "application/json; charset=utf-8", result.Body)
	return nil
}

func (routes *Routes) authenticate(c *gin.Context) (string, error) {
	return routes.authenticator.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
}

func bindListOwned(c *gin.Context) (ListOwnedInput, error) {
	rawSort, exists := httpserver.LastQueryValue(c, "sort")
	if !exists || !validSort(Sort(rawSort)) {
		return ListOwnedInput{}, routePlaylistValidationError()
	}
	cursor, err := optionalCursor(c)
	if err != nil {
		return ListOwnedInput{}, err
	}
	limit, err := optionalPageLimit(c)
	if err != nil {
		return ListOwnedInput{}, err
	}
	return ListOwnedInput{Sort: Sort(rawSort), Cursor: cursor, Limit: limit}, nil
}

func bindPageQuery(c *gin.Context) (GetInput, error) {
	cursor, err := optionalCursor(c)
	if err != nil {
		return GetInput{}, err
	}
	limit, err := optionalPageLimit(c)
	if err != nil {
		return GetInput{}, err
	}
	return GetInput{Cursor: cursor, Limit: limit}, nil
}

func bindExpectedVersion(c *gin.Context) (int, error) {
	raw, exists := httpserver.LastQueryValue(c, "expectedVersion")
	if !exists {
		return 0, routePlaylistValidationError()
	}
	value, err := parseJavaScriptInteger(raw)
	if err != nil || value < 1 {
		return 0, routePlaylistValidationError()
	}
	return value, nil
}

func optionalCursor(c *gin.Context) (string, error) {
	value, exists := httpserver.LastQueryValue(c, "cursor")
	if !exists {
		return "", nil
	}
	length := javascriptStringLength(value)
	if length < 1 || length > 512 {
		return "", routePlaylistValidationError()
	}
	return value, nil
}

func optionalPageLimit(c *gin.Context) (*int, error) {
	raw, exists := httpserver.LastQueryValue(c, "limit")
	if !exists || raw == "" {
		return nil, nil
	}
	value, err := parseJavaScriptInteger(raw)
	if err != nil || value < 1 || value > maximumPageLimit {
		return nil, routePlaylistValidationError()
	}
	return &value, nil
}

func parseJavaScriptInteger(raw string) (int, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value > float64(1<<53-1) || value < -float64(1<<53-1) {
		return 0, errors.New("not a safe integer")
	}
	return int(value), nil
}

var (
	canonicalUUID         = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)

func bindUUID(value, _ string) (string, error) {
	if !canonicalUUID.MatchString(value) {
		return "", routePlaylistValidationError()
	}
	return value, nil
}

func validateCreateRoute(input CreateInput) error {
	length := javascriptStringLength(input.Name)
	if length < 1 || length > 100 {
		return routePlaylistValidationError()
	}
	if input.Description.Set && input.Description.Value != nil && javascriptStringLength(*input.Description.Value) > 1000 {
		return routePlaylistValidationError()
	}
	if !validVisibility(input.Visibility) {
		return routePlaylistValidationError()
	}
	return nil
}

func validateUpdateRoute(input UpdateInput) error {
	if input.ExpectedVersion < 1 {
		return routePlaylistValidationError()
	}
	if !input.Name.Set && !input.Description.Set && !input.Visibility.Set {
		return routePlaylistValidationError()
	}
	if input.Name.Set {
		length := javascriptStringLength(input.Name.Value)
		if length < 1 || length > 100 {
			return routePlaylistValidationError()
		}
	}
	if input.Description.Set && input.Description.Value != nil && javascriptStringLength(*input.Description.Value) > 1000 {
		return routePlaylistValidationError()
	}
	if input.Visibility.Set && !validVisibility(input.Visibility.Value) {
		return routePlaylistValidationError()
	}
	return nil
}

func validateReorderRoute(input ReorderInput) error {
	if input.ExpectedVersion < 1 {
		return routePlaylistValidationError()
	}
	if !input.OrderedEntryIDs.Set {
		return routePlaylistValidationError()
	}
	if len(input.OrderedEntryIDs.Values) > MaxPlaylistEntries {
		return routePlaylistValidationError()
	}
	seen := make(map[string]struct{}, len(input.OrderedEntryIDs.Values))
	for _, id := range input.OrderedEntryIDs.Values {
		if _, duplicate := seen[id]; duplicate {
			return routePlaylistValidationError()
		}
		seen[id] = struct{}{}
	}
	return nil
}

func routePlaylistValidationError() error {
	return apperror.Validation("请求参数不符合接口要求")
}

func decodePlaylistJSON(c *gin.Context, destination any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return routePlaylistValidationError()
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return apperror.PayloadTooLarge("请求内容超过 2 MiB，请缩小后重试")
		}
		return apperror.Validation("请求内容无法解析")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return routePlaylistValidationError()
	}
	if !json.Valid(raw) {
		return apperror.Validation("请求内容无法解析")
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return routePlaylistValidationError()
	}
	return nil
}
