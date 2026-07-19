package adminaudit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
)

func TestAuditQueryIgnoresUnknownFieldsAndUsesLastRepeatedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resourceID := "00000000-0000-0000-0000-000000000099"
	api := &auditAPIStub{result: PageDTO{
		Items: []ItemDTO{{
			ID:           "audit-1",
			Action:       "TRACK_UPDATED",
			ResourceType: "TRACK",
			ResourceID:   &resourceID,
			Result:       "SUCCESS",
			TraceID:      "trace-audit-1",
			Metadata:     map[string]any{"field": "title"},
			CreatedAt:    "2026-07-16T08:00:00.000Z",
		}},
		Page: 2, PageSize: 20, Total: 21, TotalPages: 2,
	}}
	identityService := &auditIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, err := NewRoutes(api, identityService)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-0000-0000-000000000001"
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/audit?page=bad&page=2&pageSize=bad&pageSize=20&actorId=bad&actorId="+id+
			"&result=INVALID&result=SUCCESS&sort=invalid&sort=action&order=invalid&order=asc&unknown=true",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if api.input.Page != 2 || api.input.PageSize != 20 || api.input.ActorID != id ||
		api.input.Result != "SUCCESS" || api.input.Sort != "action" || api.input.Order != "asc" {
		t.Fatalf("input=%+v", api.input)
	}
	if identityService.calls != 1 {
		t.Fatalf("identity calls=%d", identityService.calls)
	}
	if identityService.authorization != "Bearer admin" {
		t.Fatalf("authorization=%q", identityService.authorization)
	}
	var body PageDTO
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Page != 2 || body.PageSize != 20 || body.Total != 21 || body.TotalPages != 2 || len(body.Items) != 1 ||
		body.Items[0].ID != "audit-1" || body.Items[0].Action != "TRACK_UPDATED" || body.Items[0].ResourceID == nil || *body.Items[0].ResourceID != resourceID {
		t.Fatalf("body=%+v", body)
	}
}

type auditAPIStub struct {
	input  ListInput
	result PageDTO
}

func (stub *auditAPIStub) List(_ context.Context, input ListInput) (PageDTO, error) {
	stub.input = input
	return stub.result, nil
}

type auditIdentityStub struct {
	actor         identity.AuthenticatedActor
	calls         int
	authorization string
}

func (stub *auditIdentityStub) Authenticate(_ context.Context, authorization string) (identity.AuthenticatedActor, error) {
	stub.calls++
	stub.authorization = authorization
	return stub.actor, nil
}
func (*auditIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected Login call")
}
func (*auditIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected Refresh call")
}
func (*auditIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected Logout call")
}
func (*auditIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected GetAuthenticatedUser call")
}
