package adminmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/adminauth"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type API interface {
	Dashboard(context.Context) (DashboardDTO, error)
	ListUsers(context.Context, ListUsersInput) (UserPageDTO, error)
	User(context.Context, string, SessionPageInput) (UserDetailDTO, error)
	CreateUser(context.Context, string, string, CreateUserInput) (UserDetailDTO, error)
	UpdateUser(context.Context, string, string, string, UpdateUserInput) (UserDetailDTO, error)
	ResetPassword(context.Context, string, string, string, PasswordInput) (UpdatedDTO, error)
	RevokeSession(context.Context, string, string, string, string, string) (RevokedDTO, error)
}

type Routes struct {
	service     API
	identity    adminauth.Identity
	idempotency Idempotency
}

func NewRoutes(service API, identity adminauth.Identity, idempotency Idempotency) (*Routes, error) {
	if service == nil {
		return nil, errors.New("admin management API service is required")
	}
	if identity == nil {
		return nil, errors.New("admin management identity service is required")
	}
	if idempotency == nil {
		return nil, errors.New("admin management idempotency service is required")
	}
	return &Routes{service: service, identity: identity, idempotency: idempotency}, nil
}

func (routes *Routes) Register(router gin.IRouter) {
	admin := router.Group("/api/v1/admin")
	admin.GET("/dashboard", httpserver.Handle(routes.dashboard))
	admin.GET("/users", httpserver.Handle(routes.listUsers))
	admin.POST("/users", httpserver.Handle(routes.createUser))
	admin.GET("/users/:id", httpserver.Handle(routes.user))
	admin.PATCH("/users/:id", httpserver.Handle(routes.updateUser))
	admin.POST("/users/:id/password", httpserver.Handle(routes.resetPassword))
	admin.POST("/users/:id/sessions/:sessionId/revoke", httpserver.Handle(routes.revokeSession))
	admin.DELETE("/users/:id", httpserver.Handle(routes.deleteUser))
	admin.POST("/users/:id/restore", httpserver.Handle(routes.restoreUser))
}

func (routes *Routes) dashboard(c *gin.Context) error {
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.Dashboard(c.Request.Context())
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) listUsers(c *gin.Context) error {
	input, err := bindUserList(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.ListUsers(c.Request.Context(), input)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func (routes *Routes) createUser(c *gin.Context) error {
	var input CreateUserInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validateCreateUserRoute(input); err != nil {
		return err
	}
	return routes.mutate(c, "admin.user.create", input, http.StatusCreated, func(actorID, traceID string) (any, error) {
		return routes.service.CreateUser(c.Request.Context(), actorID, traceID, input)
	})
}

func (routes *Routes) user(c *gin.Context) error {
	userID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	page, err := bindSessionPage(c)
	if err != nil {
		return err
	}
	if _, err := adminauth.RequireAdmin(c, routes.identity, false); err != nil {
		return err
	}
	result, err := routes.service.User(c.Request.Context(), userID, page)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, result)
	return nil
}

func bindSessionPage(c *gin.Context) (SessionPageInput, error) {
	page, err := routeOptionalInteger(c, "page", 1, pagination.MaxPage, 1)
	if err != nil {
		return SessionPageInput{}, err
	}
	pageSize, err := routeOptionalInteger(c, "pageSize", 1, 100, 25)
	if err != nil {
		return SessionPageInput{}, err
	}
	return SessionPageInput{Page: page, PageSize: pageSize}, nil
}

func (routes *Routes) updateUser(c *gin.Context) error {
	userID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input UpdateUserInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validateUpdateUserRoute(input); err != nil {
		return err
	}
	return routes.mutate(c, "admin.user.update:"+userID, input, http.StatusOK, func(actorID, traceID string) (any, error) {
		return routes.service.UpdateUser(c.Request.Context(), actorID, traceID, userID, input)
	})
}

func (routes *Routes) resetPassword(c *gin.Context) error {
	userID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	var input PasswordInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validatePasswordRoute(input); err != nil {
		return err
	}
	return routes.mutate(c, "admin.user.password:"+userID, input, http.StatusOK, func(actorID, traceID string) (any, error) {
		return routes.service.ResetPassword(c.Request.Context(), actorID, traceID, userID, input)
	})
}

func (routes *Routes) revokeSession(c *gin.Context) error {
	userID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	sessionID, err := routeUUID(c.Param("sessionId"))
	if err != nil {
		return err
	}
	var input ReasonInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return err
	}
	if err := validateReasonRoute(input.Reason); err != nil {
		return err
	}
	return routes.mutate(
		c, "admin.user.session.revoke:"+sessionID, input, http.StatusOK,
		func(actorID, traceID string) (any, error) {
			return routes.service.RevokeSession(c.Request.Context(), actorID, traceID, userID, sessionID, input.Reason)
		},
	)
}

func (routes *Routes) deleteUser(c *gin.Context) error {
	userID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	input, err := decodeVersionReason(c)
	if err != nil {
		return err
	}
	payload := input
	return routes.mutate(c, "admin.user.delete:"+userID, payload, http.StatusOK, func(actorID, traceID string) (any, error) {
		return routes.service.UpdateUser(c.Request.Context(), actorID, traceID, userID, UpdateUserInput{
			ExpectedVersion: input.ExpectedVersion,
			Status:          OptionalStatus{Set: true, Value: StatusDeleted},
			Reason:          input.Reason,
		})
	})
}

func (routes *Routes) restoreUser(c *gin.Context) error {
	userID, err := routeUUID(c.Param("id"))
	if err != nil {
		return err
	}
	input, err := decodeVersionReason(c)
	if err != nil {
		return err
	}
	payload := input
	return routes.mutate(c, "admin.user.restore:"+userID, payload, http.StatusOK, func(actorID, traceID string) (any, error) {
		return routes.service.UpdateUser(c.Request.Context(), actorID, traceID, userID, UpdateUserInput{
			ExpectedVersion: input.ExpectedVersion,
			Status:          OptionalStatus{Set: true, Value: StatusActive},
			Reason:          input.Reason,
		})
	})
}

func (routes *Routes) mutate(
	c *gin.Context,
	scope string,
	payload any,
	status int,
	operation func(actorID, traceID string) (any, error),
) error {
	actor, err := adminauth.RequireAdmin(c, routes.identity, true)
	if err != nil {
		return err
	}
	key := c.GetHeader("Idempotency-Key")
	if !routeIdempotencyKey.MatchString(key) {
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
			return IdempotencyResponse{}, fmtError("encode admin management response", err)
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

func bindUserList(c *gin.Context) (ListUsersInput, error) {
	page, err := routeOptionalInteger(c, "page", 1, pagination.MaxPage, 1)
	if err != nil {
		return ListUsersInput{}, err
	}
	pageSize, err := routeOptionalInteger(c, "pageSize", 1, 100, 25)
	if err != nil {
		return ListUsersInput{}, err
	}
	query, _ := httpserver.LastQueryValue(c, "query")
	if javascriptStringLength(query) > 100 {
		return ListUsersInput{}, routeContractError()
	}
	roleValue, rolePresent := httpserver.LastQueryValue(c, "role")
	role := UserRole(roleValue)
	if rolePresent && !validRole(role) {
		return ListUsersInput{}, routeContractError()
	}
	statusValue, statusPresent := httpserver.LastQueryValue(c, "status")
	status := UserStatus(statusValue)
	if statusPresent && !validStatus(status) {
		return ListUsersInput{}, routeContractError()
	}
	return ListUsersInput{Page: page, PageSize: pageSize, Query: query, Role: role, Status: status}, nil
}

func routeOptionalInteger(c *gin.Context, name string, minimum, maximum, fallback int) (int, error) {
	raw, present := httpserver.LastQueryValue(c, name)
	if !present {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value < float64(minimum) || value > float64(maximum) || math.Abs(value) > float64(1<<53-1) {
		return 0, routeContractError()
	}
	return int(value), nil
}

func routeUUID(value string) (string, error) {
	if !routeUUIDPattern.MatchString(value) {
		return "", routeContractError()
	}
	return value, nil
}

func validateCreateUserRoute(input CreateUserInput) error {
	if !usernamePattern.MatchString(input.Username) || !routeStringLength(input.Password, 6, 128) ||
		!routeStringLength(input.DisplayName, 1, 100) || !validRole(input.Role) {
		return routeContractError()
	}
	return nil
}

func validateUpdateUserRoute(input UpdateUserInput) error {
	if input.ExpectedVersion < 1 || !routeStringLength(input.Reason, 1, 500) {
		return routeContractError()
	}
	if input.Username.Set && !usernamePattern.MatchString(input.Username.Value) {
		return routeContractError()
	}
	if input.DisplayName.Set && !routeStringLength(input.DisplayName.Value, 1, 100) {
		return routeContractError()
	}
	if input.Bio.Set && input.Bio.Value != nil && javascriptStringLength(*input.Bio.Value) > 500 {
		return routeContractError()
	}
	if input.Role.Set && !validRole(input.Role.Value) {
		return routeContractError()
	}
	if input.Status.Set && !validStatus(input.Status.Value) {
		return routeContractError()
	}
	return nil
}

func validatePasswordRoute(input PasswordInput) error {
	if input.ExpectedVersion < 1 || !routeStringLength(input.Password, 6, 128) ||
		!routeStringLength(input.Reason, 1, 500) {
		return routeContractError()
	}
	return nil
}

func decodeVersionReason(c *gin.Context) (VersionReasonInput, error) {
	var input VersionReasonInput
	if err := httpserver.DecodeJSON(c, &input); err != nil {
		return VersionReasonInput{}, err
	}
	if input.ExpectedVersion < 1 || !routeStringLength(input.Reason, 1, 500) {
		return VersionReasonInput{}, routeContractError()
	}
	return input, nil
}

func validateReasonRoute(reason string) error {
	if !routeStringLength(reason, 1, 500) {
		return routeContractError()
	}
	return nil
}

func routeStringLength(value string, minimum, maximum int) bool {
	length := javascriptStringLength(value)
	return length >= minimum && length <= maximum
}

func routeContractError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

func fmtError(operation string, err error) error {
	return errors.New(operation + ": " + err.Error())
}

var (
	routeUUIDPattern    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	routeIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._~-]{8,128}$`)
)
