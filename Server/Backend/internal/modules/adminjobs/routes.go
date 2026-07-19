package adminjobs

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
	List(context.Context, ListInput) (JobPageDTO, error)
	Job(context.Context, string) (JobDetailDTO, error)
	Retry(context.Context, string, string, string, *string) (JobDetailDTO, error)
	Cancel(context.Context, string, string, string, *string) (JobDetailDTO, error)
	EventState(context.Context) (EventStateDTO, error)
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
		return nil, errors.New("admin jobs API service is required")
	}
	if identity == nil {
		return nil, errors.New("admin jobs identity service is required")
	}
	if idempotency == nil {
		return nil, errors.New("admin jobs idempotency service is required")
	}
	if events == nil {
		return nil, errors.New("admin jobs SSE broadcaster is required")
	}
	return &Routes{service: service, identity: identity, idempotency: idempotency, events: events}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.GET("/jobs", httpserver.Handle(routes.list))
	admin.GET("/jobs/events", httpserver.Handle(routes.eventsStream))
	admin.GET("/jobs/:id", httpserver.Handle(routes.job))
	admin.POST("/jobs/:id/retry", httpserver.Handle(routes.retry))
	admin.POST("/jobs/:id/cancel", httpserver.Handle(routes.cancel))
}

func (routes *Routes) list(c *gin.Context) error {
	input, err := bindJobList(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.List(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) eventsStream(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	flusher, supported := c.Writer.(http.Flusher)
	if !supported {
		return errors.New("HTTP response writer does not support streaming")
	}
	subscription, err := routes.events.Subscribe(c.Request.Context(), "jobs", sse.TopicOptions{
		Load: func(ctx context.Context) (any, error) {
			return routes.service.EventState(ctx)
		},
		Fingerprint: func(value any) string {
			return value.(EventStateDTO).Fingerprint
		},
		Payload: func(value any) any {
			return value.(EventStateDTO)
		},
		PollInterval: 5 * time.Second,
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

func (routes *Routes) job(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.Job(c.Request.Context(), jobID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) retry(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	input, err := decodeOptionalReason(c)
	if err != nil {
		return err
	}
	return routes.mutate(c, "admin.job.retry:"+jobID, input, func(actorID, traceID string) (JobDetailDTO, error) {
		return routes.service.Retry(c.Request.Context(), actorID, traceID, jobID, input.Reason)
	})
}

func (routes *Routes) cancel(c *gin.Context) error {
	jobID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	input, err := decodeOptionalReason(c)
	if err != nil {
		return err
	}
	return routes.mutate(c, "admin.job.cancel:"+jobID, input, func(actorID, traceID string) (JobDetailDTO, error) {
		return routes.service.Cancel(c.Request.Context(), actorID, traceID, jobID, input.Reason)
	})
}

func (routes *Routes) mutate(
	c *gin.Context,
	scope string,
	payload ReasonInput,
	operation func(actorID, traceID string) (JobDetailDTO, error),
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
			return IdempotencyResponse{}, errors.New("encode administrator job response: " + err.Error())
		}
		return IdempotencyResponse{Status: http.StatusOK, Body: encoded}, nil
	})
	if err != nil {
		return err
	}
	c.Header("X-Idempotent-Replay", strconv.FormatBool(result.Replayed))
	c.Data(result.Status, "application/json; charset=utf-8", result.Body)
	return nil
}

func bindJobList(c *gin.Context) (ListInput, error) {
	page, err := routeOptionalInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return ListInput{}, err
	}
	pageSize, err := routeOptionalInteger(c, "pageSize", 1, 100)
	if err != nil {
		return ListInput{}, err
	}
	search, searchPresent := routeLastQuery(c, "search")
	if searchPresent && routeJavascriptLength(search) > 200 {
		return ListInput{}, routeContractError()
	}
	statusValue, statusPresent := routeLastQuery(c, "status")
	status := JobStatus(statusValue)
	if statusPresent && !validStatusFilter(status) {
		return ListInput{}, routeContractError()
	}
	typeValue, typePresent := routeLastQuery(c, "type")
	jobType := JobType(typeValue)
	if typePresent && !validTypeFilter(jobType) {
		return ListInput{}, routeContractError()
	}
	sortValue, sortPresent := routeLastQuery(c, "sort")
	sort := SortField(sortValue)
	if sortPresent && !validSort(sort) {
		return ListInput{}, routeContractError()
	}
	orderValue, orderPresent := routeLastQuery(c, "order")
	order := SortOrder(orderValue)
	if orderPresent && !validOrder(order) {
		return ListInput{}, routeContractError()
	}
	return ListInput{
		Page: page, PageSize: pageSize, Search: search, Status: status, Type: jobType, Sort: sort, Order: order,
	}, nil
}

func routeOptionalInteger(c *gin.Context, name string, minimum, maximum int) (int, error) {
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

func decodeOptionalReason(c *gin.Context) (ReasonInput, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return ReasonInput{}, nil
	}
	decoder := json.NewDecoder(c.Request.Body)
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); errors.Is(err, io.EOF) {
		return ReasonInput{}, nil
	} else if err != nil || fields == nil {
		return ReasonInput{}, routeContractError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ReasonInput{}, routeContractError()
	}
	raw, exists := fields["reason"]
	if !exists {
		return ReasonInput{}, nil
	}
	var reason string
	if err := json.Unmarshal(raw, &reason); err != nil || routeJavascriptLength(reason) < 1 || routeJavascriptLength(reason) > 500 {
		return ReasonInput{}, routeContractError()
	}
	return ReasonInput{Reason: &reason}, nil
}

func routeUUID(value string) (string, error) {
	if !routeUUIDPattern.MatchString(value) {
		return "", routeContractError()
	}
	return value, nil
}

func routeJavascriptLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func routeContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

var (
	routeUUIDPattern      = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)
