package adminaudit

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
	List(context.Context, ListInput) (PageDTO, error)
}
type Routes struct {
	service  API
	identity adminauth.Identity
}

func NewRoutes(service API, identity adminauth.Identity) (*Routes, error) {
	if service == nil {
		return nil, errors.New("admin audit API is required")
	}
	if identity == nil {
		return nil, errors.New("admin audit identity is required")
	}
	return &Routes{service: service, identity: identity}, nil
}
func (routes *Routes) Register(router gin.IRouter) {
	router.GET("/api/v1/admin/audit", httpserver.Handle(routes.list))
}
func (routes *Routes) list(c *gin.Context) error {
	input, err := bindAudit(c)
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
func bindAudit(c *gin.Context) (ListInput, error) {
	page, err := auditInteger(c, "page", 1, pagination.MaxPage)
	if err != nil {
		return ListInput{}, err
	}
	pageSize, err := auditInteger(c, "pageSize", 1, 100)
	if err != nil {
		return ListInput{}, err
	}
	input := ListInput{Page: page, PageSize: pageSize}
	fields := []struct {
		name   string
		max    int
		target *string
	}{{"search", 200, &input.Search}, {"action", 120, &input.Action}, {"targetType", 80, &input.TargetType}}
	for _, field := range fields {
		if value, present := httpserver.LastQueryValue(c, field.name); present {
			if auditLength(value) > field.max {
				return ListInput{}, auditContractError()
			}
			*field.target = value
		}
	}
	for _, field := range []struct {
		name   string
		target *string
	}{{"actorId", &input.ActorID}, {"targetId", &input.TargetID}} {
		if value, present := httpserver.LastQueryValue(c, field.name); present {
			if !auditUUID.MatchString(value) {
				return ListInput{}, auditContractError()
			}
			*field.target = value
		}
	}
	if value, present := httpserver.LastQueryValue(c, "result"); present {
		if value != "SUCCESS" && value != "FAILURE" {
			return ListInput{}, auditContractError()
		}
		input.Result = value
	}
	for _, field := range []struct {
		name   string
		target *string
	}{{"from", &input.From}, {"to", &input.To}} {
		if value, present := httpserver.LastQueryValue(c, field.name); present {
			length := auditLength(value)
			if length < 10 || length > 40 {
				return ListInput{}, auditContractError()
			}
			*field.target = value
		}
	}
	if value, present := httpserver.LastQueryValue(c, "sort"); present {
		if value != "createdAt" && value != "action" && value != "result" && value != "targetType" {
			return ListInput{}, auditContractError()
		}
		input.Sort = value
	}
	if value, present := httpserver.LastQueryValue(c, "order"); present {
		if value != "asc" && value != "desc" {
			return ListInput{}, auditContractError()
		}
		input.Order = value
	}
	return input, nil
}
func auditInteger(c *gin.Context, name string, min, max int) (int, error) {
	raw, present := httpserver.LastQueryValue(c, name)
	if !present {
		return 0, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < float64(min) || value > float64(max) {
		return 0, auditContractError()
	}
	return int(value), nil
}
func auditLength(value string) int { return len(utf16.Encode([]rune(value))) }
func auditContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

var auditUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
