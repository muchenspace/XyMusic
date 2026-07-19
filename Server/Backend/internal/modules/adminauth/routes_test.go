package adminauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
)

func TestCookiesAndCSRF(t *testing.T) {
	cookies := ParseCookies("first=one; first=two; xymusic_admin_csrf=abcdefghijklmnop")
	if cookies["first"] != "one" {
		t.Fatalf("first cookie = %q", cookies["first"])
	}
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	context.Request.Header.Set("X-CSRF-Token", "abcdefghijklmnop")
	if err := RequireCSRF(context, cookies); err != nil {
		t.Fatal(err)
	}
	context.Request.Header.Set("X-CSRF-Token", "wrong")
	if err := RequireCSRF(context, cookies); err == nil {
		t.Fatal("expected CSRF failure")
	}
}

func TestRequireAdminPrefersBearerAndRequiresCookieCSRF(t *testing.T) {
	service := &adminIdentityStub{actor: identity.AuthenticatedActor{Role: identity.RoleAdmin}}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	context.Request.Header.Set("Authorization", "Bearer direct")
	context.Request.Header.Set("Cookie", AccessCookie+"=cookie-token")
	if _, err := RequireAdmin(context, service, true); err != nil {
		t.Fatal(err)
	}
	if service.authorization != "Bearer direct" {
		t.Fatalf("authorization = %q", service.authorization)
	}

	context.Request.Header.Del("Authorization")
	context.Request.Header.Set("Cookie", AccessCookie+"=cookie-token; "+CSRFCookie+"=abcdefghijklmnop")
	context.Request.Header.Set("X-CSRF-Token", "abcdefghijklmnop")
	if _, err := RequireAdmin(context, service, true); err != nil {
		t.Fatal(err)
	}
	if service.authorization != "Bearer cookie-token" {
		t.Fatalf("cookie authorization = %q", service.authorization)
	}
}

type adminIdentityStub struct {
	actor         identity.AuthenticatedActor
	authorization string
}

func (stub *adminIdentityStub) Authenticate(_ context.Context, authorization string) (identity.AuthenticatedActor, error) {
	stub.authorization = authorization
	return stub.actor, nil
}
func (stub *adminIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, nil
}
func (stub *adminIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, nil
}
func (stub *adminIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error { return nil }
func (stub *adminIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, nil
}
