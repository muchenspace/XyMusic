package adminsources

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
	"time"
	"unicode/utf16"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
	"xymusic/server/internal/shared/sse"
)

type API interface {
	Browse(context.Context, string, PageQuery) (BrowseDTO, error)
	ListRoots(context.Context, PageQuery) (RootListDTO, error)
	CreateRoot(context.Context, string, string, CreateRootInput) (RootDTO, error)
	Root(context.Context, string) (RootDTO, error)
	UpdateRoot(context.Context, string, string, string, UpdateRootInput) (RootDTO, error)
	DeleteRoot(context.Context, string, string, string, DeleteRootInput) (DeletedDTO, error)
	ListFiles(context.Context, string, FileQuery) (SourceFilePageDTO, error)
	ProcessingSummary(context.Context, string) (ProcessingSummaryDTO, error)
	ListRuns(context.Context, string, PageQuery) (ScanRunPageDTO, error)
	EnqueueScan(context.Context, string, string, string) (ScanRunDTO, error)
	ScanRun(context.Context, string, string) (ScanRunDTO, error)
	CancelScan(context.Context, string, string, string, string) (CancelledDTO, error)
}

type Routes struct {
	service     API
	identity    adminauth.Identity
	idempotency Idempotency
	events      *sse.Broadcaster
}

func NewRoutes(
	service API,
	identity adminauth.Identity,
	idempotency Idempotency,
	events *sse.Broadcaster,
) (*Routes, error) {
	if service == nil {
		return nil, errors.New("administrator source API service is required")
	}
	if identity == nil {
		return nil, errors.New("administrator source identity service is required")
	}
	if idempotency == nil {
		return nil, errors.New("administrator source idempotency service is required")
	}
	if events == nil {
		return nil, errors.New("administrator source SSE broadcaster is required")
	}
	return &Routes{service: service, identity: identity, idempotency: idempotency, events: events}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	sources := router.Group("/api/v1/admin/sources")
	sources.GET("/browse", httpserver.Handle(routes.browse))
	sources.GET("", httpserver.Handle(routes.listRoots))
	sources.POST("", httpserver.Handle(routes.createRoot))
	sources.GET("/:id", httpserver.Handle(routes.root))
	sources.PATCH("/:id", httpserver.Handle(routes.updateRoot))
	sources.DELETE("/:id", httpserver.Handle(routes.deleteRoot))
	sources.GET("/:id/files", httpserver.Handle(routes.listFiles))
	sources.GET("/:id/processing", httpserver.Handle(routes.processingSummary))
	sources.GET("/:id/scans", httpserver.Handle(routes.listRuns))
	sources.POST("/:id/scans", httpserver.Handle(routes.enqueueScan))
	sources.GET("/:id/scans/:scanId", httpserver.Handle(routes.scanRun))
	sources.POST("/:id/scans/:scanId/cancel", httpserver.Handle(routes.cancelScan))
	sources.GET("/:id/scans/:scanId/events", httpserver.Handle(routes.scanEvents))
}

func (routes *Routes) browse(c *gin.Context) error {
	path, present := lastQuery(c, "path")
	if present && javascriptLength(path) > 4000 {
		return routeContractError()
	}
	page, err := bindPageQuery(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.Browse(c.Request.Context(), path, page)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) listRoots(c *gin.Context) error {
	page, err := bindPageQuery(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ListRoots(c.Request.Context(), page)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) createRoot(c *gin.Context) error {
	var input CreateRootInput
	shape, err := decodeContractJSON(c, &input)
	if err != nil || validateCreateRoot(input, shape) != nil {
		return routeContractError()
	}
	payload := createRootPayload(input, shape)
	return mutate(routes, c, "admin.library-source.create", payload, http.StatusCreated,
		func(actorID, traceID string) (RootDTO, error) {
			return routes.service.CreateRoot(c.Request.Context(), actorID, traceID, input)
		})
}

func (routes *Routes) root(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.Root(c.Request.Context(), rootID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) updateRoot(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input UpdateRootInput
	shape, err := decodeContractJSON(c, &input)
	if err != nil || validateUpdateRoot(input, shape) != nil {
		return routeContractError()
	}
	payload := updateRootPayload(input, shape)
	return mutate(routes, c, "admin.library-source.update:"+rootID, payload, http.StatusOK,
		func(actorID, traceID string) (RootDTO, error) {
			return routes.service.UpdateRoot(c.Request.Context(), actorID, traceID, rootID, input)
		})
}

func (routes *Routes) deleteRoot(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input DeleteRootInput
	shape, err := decodeContractJSON(c, &input)
	if err != nil || validateDeleteRoot(input, shape) != nil {
		return routeContractError()
	}
	payload := map[string]any{"expectedVersion": int(input.ExpectedVersion), "archiveCatalog": *input.ArchiveCatalog}
	return mutate(routes, c, "admin.library-source.delete:"+rootID, payload, http.StatusOK,
		func(actorID, traceID string) (DeletedDTO, error) {
			return routes.service.DeleteRoot(c.Request.Context(), actorID, traceID, rootID, input)
		})
}

func (routes *Routes) listFiles(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	query, err := bindFileQuery(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ListFiles(c.Request.Context(), rootID, query)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) processingSummary(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ProcessingSummary(c.Request.Context(), rootID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) listRuns(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	query, err := bindPageQuery(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ListRuns(c.Request.Context(), rootID, query)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) enqueueScan(c *gin.Context) error {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	payload := struct {
		SourceID string `json:"sourceId"`
	}{SourceID: rootID}
	return mutate(routes, c, "admin.library-source.scan:"+rootID, payload, http.StatusAccepted,
		func(actorID, traceID string) (ScanRunDTO, error) {
			return routes.service.EnqueueScan(c.Request.Context(), rootID, actorID, traceID)
		})
}

func (routes *Routes) scanRun(c *gin.Context) error {
	rootID, runID, err := scanRouteIDs(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ScanRun(c.Request.Context(), rootID, runID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) cancelScan(c *gin.Context) error {
	rootID, runID, err := scanRouteIDs(c)
	if err != nil {
		return err
	}
	payload := struct {
		SourceID string `json:"sourceId"`
		ScanID   string `json:"scanId"`
	}{SourceID: rootID, ScanID: runID}
	return mutate(routes, c, "admin.library-source.scan.cancel:"+runID, payload, http.StatusAccepted,
		func(actorID, traceID string) (CancelledDTO, error) {
			return routes.service.CancelScan(c.Request.Context(), rootID, runID, actorID, traceID)
		})
}

func (routes *Routes) scanEvents(c *gin.Context) error {
	rootID, runID, err := scanRouteIDs(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	flusher, supported := c.Writer.(http.Flusher)
	if !supported {
		return errors.New("HTTP response writer does not support streaming")
	}
	subscription, err := routes.events.Subscribe(c.Request.Context(), rootID+":"+runID, sse.TopicOptions{
		Load: func(ctx context.Context) (any, error) {
			return routes.service.ScanRun(ctx, rootID, runID)
		},
		Fingerprint: func(value any) string {
			encoded, _ := json.Marshal(value.(ScanRunDTO))
			return string(encoded)
		},
		Payload: func(value any) any { return value },
		Event:   "progress",
		Terminal: func(value any) bool {
			status := value.(ScanRunDTO).Status
			return status == ScanStatusCompleted || status == ScanStatusFailed || status == ScanStatusCancelled
		},
		PollInterval: 2 * time.Second,
	})
	if err != nil {
		return err
	}
	defer subscription.Close()
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(routes.events.RetryFrame()); err != nil {
		return nil
	}
	flusher.Flush()
	for {
		select {
		case <-c.Request.Context().Done():
			return nil
		case frame, open := <-subscription.Frames():
			if !open {
				return nil
			}
			if _, err := c.Writer.Write(frame); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}

func mutate[T any](
	routes *Routes,
	c *gin.Context,
	scope string,
	payload any,
	status int,
	operation func(string, string) (T, error),
) error {
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		return apperror.Validation("Idempotency-Key is invalid")
	}
	traceID := httpserver.TraceID(c)
	result, err := routes.idempotency.Execute(c.Request.Context(), IdempotencyInput{
		ActorID: actor.UserID, Scope: scope, Key: key, Payload: payload,
	}, func() (IdempotencyResponse, error) {
		body, err := operation(actor.UserID, traceID)
		if err != nil {
			return IdempotencyResponse{}, err
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return IdempotencyResponse{}, errors.New("encode administrator source response: " + err.Error())
		}
		return IdempotencyResponse{Status: status, Body: encoded}, nil
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.Data(status, "application/json; charset=utf-8", result.Body)
	return nil
}

func decodeContractJSON(c *gin.Context, destination any) (map[string]json.RawMessage, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, routeContractError()
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil, routeContractError()
	}
	var shape map[string]json.RawMessage
	shapeDecoder := json.NewDecoder(bytes.NewReader(raw))
	if err := shapeDecoder.Decode(&shape); err != nil || shape == nil || !decoderAtEOF(shapeDecoder) {
		return nil, routeContractError()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(destination); err != nil || !decoderAtEOF(decoder) {
		return nil, routeContractError()
	}
	return shape, nil
}

func decoderAtEOF(decoder *json.Decoder) bool {
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func createRootPayload(input CreateRootInput, shape map[string]json.RawMessage) map[string]any {
	payload := map[string]any{
		"name": input.Name, "path": input.Path, "mode": input.Mode,
		"enabled": *input.Enabled, "scanOnStartup": *input.ScanOnStartup,
		"includePatterns": input.IncludePatterns, "excludePatterns": input.ExcludePatterns,
	}
	if _, exists := shape["scanIntervalMinutes"]; exists {
		if input.ScanIntervalMinutes == nil {
			payload["scanIntervalMinutes"] = nil
		} else {
			payload["scanIntervalMinutes"] = int(*input.ScanIntervalMinutes)
		}
	}
	return payload
}

func updateRootPayload(input UpdateRootInput, shape map[string]json.RawMessage) map[string]any {
	payload := map[string]any{"expectedVersion": int(input.ExpectedVersion)}
	if input.Name.Set {
		payload["name"] = input.Name.Value
	}
	if input.Path.Set {
		payload["path"] = input.Path.Value
	}
	if input.Mode.Set {
		payload["mode"] = input.Mode.Value
	}
	if input.Enabled.Set {
		payload["enabled"] = input.Enabled.Value
	}
	if input.ScanOnStartup.Set {
		payload["scanOnStartup"] = input.ScanOnStartup.Value
	}
	if _, exists := shape["scanIntervalMinutes"]; exists {
		if input.ScanIntervalMinutes.Value == nil {
			payload["scanIntervalMinutes"] = nil
		} else {
			payload["scanIntervalMinutes"] = *input.ScanIntervalMinutes.Value
		}
	}
	if input.IncludePatterns.Set {
		payload["includePatterns"] = input.IncludePatterns.Value
	}
	if input.ExcludePatterns.Set {
		payload["excludePatterns"] = input.ExcludePatterns.Value
	}
	return payload
}

func validateCreateRoot(input CreateRootInput, shape map[string]json.RawMessage) error {
	for _, name := range []string{"name", "path", "mode", "enabled", "scanOnStartup", "includePatterns", "excludePatterns"} {
		if _, exists := shape[name]; !exists {
			return routeContractError()
		}
	}
	if javascriptLength(input.Name) < 1 || javascriptLength(input.Name) > 120 ||
		javascriptLength(input.Path) < 1 || javascriptLength(input.Path) > 4000 ||
		!validMode(input.Mode) || input.Enabled == nil || input.ScanOnStartup == nil ||
		!validJSONInterval(input.ScanIntervalMinutes) || !validPatterns(input.IncludePatterns) || !validPatterns(input.ExcludePatterns) {
		return routeContractError()
	}
	return nil
}

func validateUpdateRoot(input UpdateRootInput, shape map[string]json.RawMessage) error {
	if _, exists := shape["expectedVersion"]; !exists || int(input.ExpectedVersion) < 1 {
		return routeContractError()
	}
	if input.Name.Set && (javascriptLength(input.Name.Value) < 1 || javascriptLength(input.Name.Value) > 120) {
		return routeContractError()
	}
	if input.Path.Set && (javascriptLength(input.Path.Value) < 1 || javascriptLength(input.Path.Value) > 4000) {
		return routeContractError()
	}
	if input.Mode.Set && !validMode(input.Mode.Value) {
		return routeContractError()
	}
	if input.ScanIntervalMinutes.Set && !validInterval(input.ScanIntervalMinutes.Value) {
		return routeContractError()
	}
	if input.IncludePatterns.Set && !validPatterns(input.IncludePatterns.Value) {
		return routeContractError()
	}
	if input.ExcludePatterns.Set && !validPatterns(input.ExcludePatterns.Value) {
		return routeContractError()
	}
	return nil
}

func validateDeleteRoot(input DeleteRootInput, shape map[string]json.RawMessage) error {
	_, version := shape["expectedVersion"]
	_, archive := shape["archiveCatalog"]
	if !version || !archive || int(input.ExpectedVersion) < 1 || input.ArchiveCatalog == nil {
		return routeContractError()
	}
	return nil
}

func bindFileQuery(c *gin.Context) (FileQuery, error) {
	page, err := optionalInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return FileQuery{}, err
	}
	pageSize, err := optionalInteger(c, "pageSize", 1, 100)
	if err != nil {
		return FileQuery{}, err
	}
	query, queryPresent := lastQuery(c, "query")
	if queryPresent && javascriptLength(query) > 500 {
		return FileQuery{}, routeContractError()
	}
	statusValue, statusPresent := lastQuery(c, "status")
	status := SourceFileStatus(statusValue)
	if statusPresent && !validFileStatus(status) {
		return FileQuery{}, routeContractError()
	}
	return FileQuery{Page: page, PageSize: pageSize, Query: query, Status: status}, nil
}

func bindPageQuery(c *gin.Context) (PageQuery, error) {
	page, err := optionalInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return PageQuery{}, err
	}
	pageSize, err := optionalInteger(c, "pageSize", 1, 100)
	if err != nil {
		return PageQuery{}, err
	}
	return PageQuery{Page: page, PageSize: pageSize}, nil
}

func optionalInteger(c *gin.Context, name string, minimum, maximum int) (int, error) {
	raw, present := lastQuery(c, name)
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

func lastQuery(c *gin.Context, name string) (string, bool) {
	values, present := c.Request.URL.Query()[name]
	if !present || len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

func scanRouteIDs(c *gin.Context) (string, string, error) {
	rootID, err := routeUUID(c.Param("id"))
	if err != nil {
		return "", "", err
	}
	runID, err := routeUUID(c.Param("scanId"))
	return rootID, runID, err
}

func routeUUID(value string) (string, error) {
	if !uuidPattern.MatchString(value) {
		return "", routeContractError()
	}
	return value, nil
}

func validMode(mode RootMode) bool { return mode == RootModeReadOnly || mode == RootModeReadWrite }

func validInterval(value *int) bool { return value == nil || (*value >= 5 && *value <= 10080) }

func validJSONInterval(value *JSONInteger) bool {
	return value == nil || (int(*value) >= 5 && int(*value) <= 10080)
}

func validPatterns(patterns []string) bool {
	if patterns == nil || len(patterns) > 100 {
		return false
	}
	for _, pattern := range patterns {
		if javascriptLength(pattern) < 1 || javascriptLength(pattern) > 500 {
			return false
		}
	}
	return true
}

func validFileStatus(status SourceFileStatus) bool {
	return status == SourceFilePending || status == SourceFileProcessing || status == SourceFileReady ||
		status == SourceFileFailed || status == SourceFileMissing
}

func javascriptLength(value string) int { return len(utf16.Encode([]rune(value))) }

func routeContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

var (
	uuidPattern           = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)
