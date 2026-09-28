package tenants

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/go-chi/chi/v5"
)

type fakeTenantAuth struct {
	identity identity.SessionIdentity
}

func (f fakeTenantAuth) Login(context.Context, identity.LoginInput, identity.RequestMeta) (identity.LoginResult, error) {
	return identity.LoginResult{}, identity.ErrInvalidCredentials
}

func (f fakeTenantAuth) Authenticate(context.Context, string) (identity.SessionIdentity, error) {
	return f.identity, nil
}

func (f fakeTenantAuth) VerifyCSRF(identity.SessionIdentity, string) bool {
	return true
}

func (f fakeTenantAuth) Logout(context.Context, identity.SessionIdentity, identity.RequestMeta) error {
	return nil
}

func (f fakeTenantAuth) Permissions(context.Context, identity.SessionIdentity) ([]string, error) {
	return []string{"tenant.read", "tenant.manage"}, nil
}

func (f fakeTenantAuth) HasPermission(_ context.Context, _ identity.SessionIdentity, permission string) (bool, error) {
	return permission == "tenant.read" || permission == "tenant.manage", nil
}

type fakeTenantService struct {
	version uint64
	name    string
}

func (f *fakeTenantService) Get(context.Context, uint64) (Tenant, error) {
	return Tenant{ID: 2, Slug: "default", Name: "Default", Status: "ACTIVE", Version: 7}, nil
}

func (f *fakeTenantService) UpdateName(
	_ context.Context,
	_ Actor,
	version uint64,
	name string,
) (Tenant, error) {
	f.version = version
	f.name = name
	return Tenant{ID: 2, Slug: "default", Name: name, Status: "ACTIVE", Version: version + 1}, nil
}

func TestUpdateAcceptsQuotedBIGINTVersion(t *testing.T) {
	service := &fakeTenantService{}
	auth := identity.NewMiddleware(
		fakeTenantAuth{identity: identity.SessionIdentity{UserID: 1, TenantID: 2}},
		config.SessionConfig{
			TTL:            time.Hour,
			CookieName:     "session",
			CSRFCookieName: "csrf",
		},
	)
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(service), auth)

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/tenant/",
		strings.NewReader(`{"name":"Updated","version":"7"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})
	request.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf-token"})
	request.Header.Set("X-CSRF-Token", "csrf-token")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.version != 7 {
		t.Fatalf("decoded version = %d", service.version)
	}
	if service.name != "Updated" {
		t.Fatalf("decoded name = %q", service.name)
	}
	if !strings.Contains(rr.Body.String(), `"id":"2"`) {
		t.Fatalf("tenant ID is not string encoded: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"version":"8"`) {
		t.Fatalf("tenant version is not string encoded: %s", rr.Body.String())
	}
}
