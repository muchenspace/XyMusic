package adminmetadata

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

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/httpx"
	"xymusic/server/internal/shared/pagination"
)

type API interface {
	Metadata(context.Context, string) (MetadataDTO, error)
	Update(context.Context, string, string, MetadataMutationInput) (MetadataDTO, error)
	BatchUpdate(context.Context, string, BatchMetadataMutationInput) (BatchUpdateDTO, error)
	EnqueueWriteback(context.Context, string, string, VersionReasonInput) (WritebackJobDTO, error)
	ListWritebacks(context.Context, WritebackListInput) (WritebackJobPageDTO, error)
	WritebackJob(context.Context, string) (WritebackJobDTO, error)
	RetryWriteback(context.Context, string, string, VersionReasonInput) (WritebackJobDTO, error)
	CancelWriteback(context.Context, string, VersionInput) (WritebackJobDTO, error)
}

type Routes struct {
	service     API
	identity    adminauth.Identity
	idempotency Idempotency
}

func NewRoutes(service API, identity adminauth.Identity, idempotency Idempotency) (*Routes, error) {
	if service == nil {
		return nil, errors.New("admin metadata API service is required")
	}
	if identity == nil {
		return nil, errors.New("admin metadata identity service is required")
	}
	if idempotency == nil {
		return nil, errors.New("admin metadata idempotency service is required")
	}
	return &Routes{service: service, identity: identity, idempotency: idempotency}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.GET("/tracks/:id/metadata", httpserver.Handle(routes.metadata))
	admin.PATCH("/tracks/:id/metadata", httpserver.Handle(routes.update))
	admin.POST("/metadata/batch", httpserver.Handle(routes.batchUpdate))
	admin.POST("/tracks/:id/metadata/writeback", httpserver.Handle(routes.enqueueWriteback))
	admin.GET("/metadata/writeback-jobs", httpserver.Handle(routes.listWritebacks))
	admin.GET("/metadata/writeback-jobs/:id", httpserver.Handle(routes.writebackJob))
	admin.POST("/metadata/writeback-jobs/:id/retry", httpserver.Handle(routes.retryWriteback))
	admin.POST("/metadata/writeback-jobs/:id/cancel", httpserver.Handle(routes.cancelWriteback))
}

func (routes *Routes) metadata(c *gin.Context) error {
	trackID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.Metadata(c.Request.Context(), trackID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) update(c *gin.Context) error {
	trackID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	request, err := decodeMetadataMutation(c.Request.Body)
	if err != nil {
		return err
	}
	return mutateJSON(routes, c, "admin.track.metadata.update:"+trackID, request, http.StatusOK,
		func(actorID string) (MetadataDTO, error) {
			return routes.service.Update(
				c.Request.Context(), actorID, trackID, MetadataMutationInput(request),
			)
		})
}

func (routes *Routes) batchUpdate(c *gin.Context) error {
	request, err := decodeBatchMutation(c.Request.Body)
	if err != nil {
		return err
	}
	return mutateJSON(routes, c, "admin.track.metadata.batch", request, http.StatusOK,
		func(actorID string) (BatchUpdateDTO, error) {
			return routes.service.BatchUpdate(
				c.Request.Context(), actorID, BatchMetadataMutationInput(request),
			)
		})
}

func (routes *Routes) enqueueWriteback(c *gin.Context) error {
	trackID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	request, err := decodeVersionReason(c.Request.Body)
	if err != nil {
		return err
	}
	return mutateJSON(routes, c, "admin.track.metadata.writeback:"+trackID, request, http.StatusAccepted,
		func(actorID string) (WritebackJobDTO, error) {
			return routes.service.EnqueueWriteback(
				c.Request.Context(), actorID, trackID, VersionReasonInput(request),
			)
		})
}

func (routes *Routes) listWritebacks(c *gin.Context) error {
	input, err := bindWritebackList(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ListWritebacks(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) writebackJob(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.WritebackJob(c.Request.Context(), jobID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) retryWriteback(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	request, err := decodeVersionReason(c.Request.Body)
	if err != nil {
		return err
	}
	return mutateJSON(routes, c, "admin.track.metadata.writeback.retry:"+jobID, request, http.StatusAccepted,
		func(actorID string) (WritebackJobDTO, error) {
			return routes.service.RetryWriteback(
				c.Request.Context(), actorID, jobID, VersionReasonInput(request),
			)
		})
}

func (routes *Routes) cancelWriteback(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	request, err := decodeVersion(c.Request.Body)
	if err != nil {
		return err
	}
	return mutateJSON(routes, c, "admin.track.metadata.writeback.cancel:"+jobID, request, http.StatusAccepted,
		func(string) (WritebackJobDTO, error) {
			return routes.service.CancelWriteback(
				c.Request.Context(), jobID, VersionInput(request),
			)
		})
}

func mutateJSON[T any, P any](
	routes *Routes,
	c *gin.Context,
	scope string,
	payload P,
	status int,
	operation func(string) (T, error),
) error {
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	result, err := routes.idempotency.Execute(c.Request.Context(), IdempotencyInput{
		ActorID: actor.UserID, Scope: scope, Key: key, Payload: payload,
	}, func() (IdempotencyResponse, error) {
		body, err := operation(actor.UserID)
		if err != nil {
			return IdempotencyResponse{}, err
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return IdempotencyResponse{}, errors.New("encode admin metadata response: " + err.Error())
		}
		return IdempotencyResponse{Status: status, Body: encoded}, nil
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.Data(result.Status, "application/json; charset=utf-8", result.Body)
	return nil
}

type metadataMutationRequest struct {
	ExpectedVersion int            `json:"expectedVersion"`
	Patch           map[string]any `json:"patch"`
}

type batchMutationRequest struct {
	Items []BatchMutationItem `json:"items"`
	Patch map[string]any      `json:"patch"`
}

type versionReasonRequest struct {
	ExpectedVersion int    `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

type versionRequest struct {
	ExpectedVersion int `json:"expectedVersion"`
}

func decodeMetadataMutation(body io.Reader) (metadataMutationRequest, error) {
	object, err := decodeBodyObject(body)
	if err != nil {
		return metadataMutationRequest{}, err
	}
	expectedVersion, err := requiredIntegerField(object, "expectedVersion", 1, math.MaxInt)
	if err != nil {
		return metadataMutationRequest{}, err
	}
	patchObject, err := requiredObjectField(object, "patch")
	if err != nil {
		return metadataMutationRequest{}, err
	}
	patch, err := filterMetadataPatch(patchObject)
	if err != nil {
		return metadataMutationRequest{}, err
	}
	return metadataMutationRequest{
		ExpectedVersion: expectedVersion, Patch: patch,
	}, nil
}

func decodeBatchMutation(body io.Reader) (batchMutationRequest, error) {
	object, err := decodeBodyObject(body)
	if err != nil {
		return batchMutationRequest{}, err
	}
	rawItems, ok := object["items"].([]any)
	if !ok || len(rawItems) < 1 || len(rawItems) > 200 {
		return batchMutationRequest{}, routeContractError()
	}
	items := make([]BatchMutationItem, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			return batchMutationRequest{}, routeContractError()
		}
		trackID, err := requiredRouteString(item, "trackId", 1, 100, routeUUIDPattern)
		if err != nil {
			return batchMutationRequest{}, err
		}
		expectedVersion, err := requiredIntegerField(item, "expectedVersion", 1, math.MaxInt)
		if err != nil {
			return batchMutationRequest{}, err
		}
		items = append(items, BatchMutationItem{TrackID: trackID, ExpectedVersion: expectedVersion})
	}
	patchObject, err := requiredObjectField(object, "patch")
	if err != nil {
		return batchMutationRequest{}, err
	}
	patch, err := filterMetadataPatch(patchObject)
	if err != nil {
		return batchMutationRequest{}, err
	}
	return batchMutationRequest{
		Items: items, Patch: patch,
	}, nil
}

func decodeVersionReason(body io.Reader) (versionReasonRequest, error) {
	object, err := decodeBodyObject(body)
	if err != nil {
		return versionReasonRequest{}, err
	}
	expectedVersion, err := requiredIntegerField(object, "expectedVersion", 1, math.MaxInt)
	if err != nil {
		return versionReasonRequest{}, err
	}
	reason := ""
	if raw, exists := object["reason"]; exists && raw != nil {
		if text, ok := raw.(string); ok {
			reason = text
		}
	}
	return versionReasonRequest{ExpectedVersion: expectedVersion, Reason: reason}, nil
}

func decodeVersion(body io.Reader) (versionRequest, error) {
	object, err := decodeBodyObject(body)
	if err != nil {
		return versionRequest{}, err
	}
	expectedVersion, err := requiredIntegerField(object, "expectedVersion", 1, math.MaxInt)
	if err != nil {
		return versionRequest{}, err
	}
	return versionRequest{ExpectedVersion: expectedVersion}, nil
}

func decodeBodyObject(body io.Reader) (map[string]any, error) {
	if body == nil {
		return nil, routeContractError()
	}
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, routeContractError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, routeContractError()
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, routeContractError()
	}
	return object, nil
}

func requiredObjectField(object map[string]any, name string) (map[string]any, error) {
	value, exists := object[name]
	if !exists {
		return nil, routeContractError()
	}
	result, ok := value.(map[string]any)
	if !ok || result == nil {
		return nil, routeContractError()
	}
	return result, nil
}

func requiredIntegerField(object map[string]any, name string, minimum, maximum int) (int, error) {
	value, exists := object[name]
	if !exists {
		return 0, routeContractError()
	}
	result, ok := routeInteger(value, minimum, maximum)
	if !ok {
		return 0, routeContractError()
	}
	return result, nil
}

func requiredRouteString(
	object map[string]any,
	name string,
	minimum, maximum int,
	pattern *regexp.Regexp,
) (string, error) {
	value, exists := object[name]
	if !exists {
		return "", routeContractError()
	}
	result, ok := routeString(value, minimum, maximum, pattern)
	if !ok {
		return "", routeContractError()
	}
	return result, nil
}

func routeString(value any, minimum, maximum int, pattern *regexp.Regexp) (string, bool) {
	text, ok := value.(string)
	if !ok || routeJavascriptLength(text) < minimum || routeJavascriptLength(text) > maximum {
		return "", false
	}
	if pattern != nil && !pattern.MatchString(text) {
		return "", false
	}
	return text, true
}

func routeJavascriptLength(value string) int {
	return httpx.JavascriptStringLength(value)
}

func routeInteger(value any, minimum, maximum int) (int, bool) {
	number, ok := exactInteger(value)
	if !ok || number < int64(minimum) || number > int64(maximum) {
		return 0, false
	}
	return int(number), true
}

func bindPaginationQuery(c *gin.Context) (int, int, error) {
	page, err := routeOptionalQueryInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := routeOptionalQueryInteger(c, "pageSize", 1, pagination.MaxPageSize)
	if err != nil {
		return 0, 0, err
	}
	return page, pageSize, nil
}

func bindWritebackList(c *gin.Context) (WritebackListInput, error) {
	page, err := routeOptionalQueryInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return WritebackListInput{}, err
	}
	pageSize, err := routeOptionalQueryInteger(c, "pageSize", 1, pagination.MaxPageSize)
	if err != nil {
		return WritebackListInput{}, err
	}
	statusValue, statusPresent := routeLastQuery(c, "status")
	status := WritebackStatus(statusValue)
	if statusPresent && !validWritebackStatus(status, false) {
		return WritebackListInput{}, routeContractError()
	}
	trackID, trackPresent := routeLastQuery(c, "trackId")
	if trackPresent && !routeUUIDPattern.MatchString(trackID) {
		return WritebackListInput{}, routeContractError()
	}
	cursor, cursorPresent := routeLastQuery(c, "cursor")
	if cursorPresent && len(cursor) > 4096 {
		return WritebackListInput{}, routeContractError()
	}
	cursorModeValue, cursorModePresent := routeLastQuery(c, "cursorMode")
	cursorMode := !cursorModePresent
	if cursorModePresent {
		if cursorModeValue != "cursor" && cursorModeValue != "offset" {
			return WritebackListInput{}, routeContractError()
		}
		cursorMode = cursorModeValue == "cursor"
	}
	if cursor != "" && !cursorModePresent {
		cursorMode = true
	}
	return WritebackListInput{
		Page: page, PageSize: pageSize, Status: status, TrackID: trackID,
		Cursor: cursor, CursorMode: cursorMode,
	}, nil
}

func routeOptionalQueryInteger(c *gin.Context, name string, minimum, maximum int) (int, error) {
	raw, present := routeLastQuery(c, name)
	if !present {
		return 0, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value < float64(minimum) || value > float64(maximum) || math.Abs(value) > float64(1<<53-1) {
		return 0, routeContractError()
	}
	return int(value), nil
}

func routeLastQuery(c *gin.Context, name string) (string, bool) {
	values, present := c.Request.URL.Query()[name]
	if !present || len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

func routeUUID(value string) (string, error) {
	if !routeUUIDPattern.MatchString(value) {
		return "", routeContractError()
	}
	return value, nil
}

func routeContractError() error {
	return apperror.Validation("请求参数不符合接口要求")
}

var (
	routeUUIDPattern      = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)
