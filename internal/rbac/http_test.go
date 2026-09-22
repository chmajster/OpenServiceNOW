package rbac

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

type fakeAuth struct {
	identity    identity.SessionIdentity
	permission bool
	csrf       bool
}

func (f fakeAuth) Login(context.Context, identity.LoginInput, identity.RequestMeta) (identity.LoginResult, error) {
	return identity.LoginResult{}, identity.ErrInvalidCredentials
}

func (f fakeAuth) Authenticate(context.Context, string) (identity.SessionIdentity, error) {
	return f.identity, nil
}

func (f fakeAuth) VerifyCSRF(identity.SessionIdentity, string) bool {
	return f.csrf
}

func (f fakeAuth) Logout(context.Context, identity.SessionIdentity, identity.RequestMeta) error {
	return nil
}

func (f fakeAuth) Permissions(context.Context, identity.SessionIdentity) ([]string, error) {
	return nil, nil
}

func (f fakeAuth) HasPermission(context.Context, identity.SessionIdentity, string) (bool, error) {
	return f.permission, nil
}

type fakeRBAC struct {
	createRoleCalls int
}

func (f *fakeRBAC) ListPermissions(context.Context) ([]Permission, error) {
	return []Permission{{ID: 1, Key: "admin.rbac.manage"}}, nil
}

func (f *fakeRBAC) ListRoles(context.Context, uint64) ([]Role, error) {
	return []Role{}, nil
}

func (f *fakeRBAC) CreateRole(context.Context, Actor, CreateRoleInput) (uint64, error) {
	f.createRoleCalls++
	return 9, nil
}

func (f *fakeRBAC) SetRolePermissions(context.Context, Actor, uint64, []string) error {
	return nil
}

func (f *fakeRBAC) ListGroups(context.Context, uint64) ([]Group, error) {
	return []Group{}, nil
}

func (f *fakeRBAC) CreateGroup(context.Context, Actor, CreateGroupInput) (uint64, error) {
	return 7, nil
}

func (f *fakeRBAC) AddGroupMember(context.Context, Actor, uint64, uint64) error {
	return nil
}

func (f *fakeRBAC) RemoveGroupMember(context.Context, Actor, uint64, uint64) error {
	return nil
}

func (f *fakeRBAC) AssignRoleToUser(context.Context, Actor, uint64, uint64) error {
	return nil
}

func (f *fakeRBAC) RemoveRoleFromUser(context.Context, Actor, uint64, uint64) error {
	return nil
}

func (f *fakeRBAC) AssignRoleToGroup(context.Context, Actor, uint64, uint64) error {
	return nil
}

func (f *fakeRBAC) RemoveRoleFromGroup(context.Context, Actor, uint64, uint64) error {
	return nil
}

func sessionConfig() config.SessionConfig {
	return config.SessionConfig{
		TTL:            time.Hour,
		CookieName:     "session",
		CSRFCookieName: "csrf",
	}
}

func newRouter(authService identity.AuthService, rbacService APIService) http.Handler {
	router := chi.NewRouter()
	auth := identity.NewMiddleware(authService, sessionConfig())
	RegisterRoutes(router, NewHandler(rbacService), auth)
	return router
}

func authenticatedRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})
	return request
}

func TestRBACRequiresAuthentication(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{}, service)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/rbac/permissions", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestRBACRequiresBackendPermission(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity: identity.SessionIdentity{UserID: 1, TenantID: 2},
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/permissions", ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestRBACReadAllowedWithPermission(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/permissions", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "admin.rbac.manage") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestRBACWriteRequiresCSRF(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
		csrf:       true,
	}, service)

	request := authenticatedRequest(
		http.MethodPost,
		"/api/v1/admin/rbac/roles",
		`{"name":"operator","permissions":["tenant.read"]}`,
	)
	request.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.createRoleCalls != 0 {
		t.Fatalf("create role calls = %d", service.createRoleCalls)
	}
}

func TestRBACWriteAllowedWithCSRF(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
		csrf:       true,
	}, service)

	request := authenticatedRequest(
		http.MethodPost,
		"/api/v1/admin/rbac/roles",
		`{"name":"operator","permissions":["tenant.read"]}`,
	)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf-token"})
	request.Header.Set("X-CSRF-Token", "csrf-token")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.createRoleCalls != 1 {
		t.Fatalf("create role calls = %d", service.createRoleCalls)
	}
}
