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
	permission  bool
	permissions map[string]bool
	csrf        bool
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

func (f fakeAuth) HasPermission(_ context.Context, _ identity.SessionIdentity, permission string) (bool, error) {
	if f.permissions != nil {
		return f.permissions[permission], nil
	}
	return f.permission, nil
}

type fakeRBAC struct {
	createRoleCalls       int
	listRoleCalls         int
	listRoleAfterID       uint64
	listRoleLimit         int
	listRoleQuery         string
	setRoleVersion        uint64
	listGroupMemberCalls  int
	listGroupMemberTenant uint64
	listGroupMemberGroup  uint64
	listGroupMemberAfter  uint64
	listGroupMemberLimit  int
	addGroupMemberCalls   int
	assignUserRoleCalls   int
}

func (f *fakeRBAC) ListPermissions(context.Context) ([]Permission, error) {
	return []Permission{{ID: 1, Key: "admin.rbac.manage"}}, nil
}

func (f *fakeRBAC) ListRoles(
	_ context.Context,
	_ uint64,
	afterID uint64,
	limit int,
	query string,
) (RolePage, error) {
	f.listRoleCalls++
	f.listRoleAfterID = afterID
	f.listRoleLimit = limit
	f.listRoleQuery = query
	return RolePage{Roles: []Role{}, NextAfterID: 99}, nil
}

func (f *fakeRBAC) CreateRole(context.Context, Actor, CreateRoleInput) (uint64, error) {
	f.createRoleCalls++
	return 9, nil
}

func (f *fakeRBAC) SetRolePermissions(_ context.Context, _ Actor, _ uint64, version uint64, _ []string) error {
	f.setRoleVersion = version
	return nil
}

func (f *fakeRBAC) ListGroups(
	context.Context,
	uint64,
	uint64,
	int,
	string,
) (GroupPage, error) {
	return GroupPage{Groups: []Group{}}, nil
}

func (f *fakeRBAC) ListGroupMembers(
	_ context.Context,
	tenantID, groupID, afterID uint64,
	limit int,
) (GroupMemberPage, error) {
	f.listGroupMemberCalls++
	f.listGroupMemberTenant = tenantID
	f.listGroupMemberGroup = groupID
	f.listGroupMemberAfter = afterID
	f.listGroupMemberLimit = limit
	return GroupMemberPage{Members: []GroupMember{}, NextAfterID: 22}, nil
}

func (f *fakeRBAC) ListUserRoles(context.Context, uint64, uint64, uint64, int) (AssignedRolePage, error) {
	return AssignedRolePage{Roles: []AssignedRole{}}, nil
}

func (f *fakeRBAC) ListGroupRoles(context.Context, uint64, uint64, uint64, int) (AssignedRolePage, error) {
	return AssignedRolePage{Roles: []AssignedRole{}}, nil
}

func (f *fakeRBAC) CreateGroup(context.Context, Actor, CreateGroupInput) (uint64, error) {
	return 7, nil
}

func (f *fakeRBAC) AddGroupMember(context.Context, Actor, uint64, uint64) error {
	f.addGroupMemberCalls++
	return nil
}

func (f *fakeRBAC) RemoveGroupMember(context.Context, Actor, uint64, uint64) error {
	return nil
}

func (f *fakeRBAC) AssignRoleToUser(context.Context, Actor, uint64, uint64) error {
	f.assignUserRoleCalls++
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
	if !strings.Contains(rr.Body.String(), `"id":"9"`) {
		t.Fatalf("created role ID is not string encoded: %s", rr.Body.String())
	}
}

func TestRBACRoleListRejectsOversizedLimit(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(
		rr,
		authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/roles?limit=201", ""),
	)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.listRoleCalls != 0 {
		t.Fatalf("list role calls = %d", service.listRoleCalls)
	}
}

func TestRBACRoleListPassesCursorAndSearch(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(
		rr,
		authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/roles?after_id=12&limit=25&q=operator", ""),
	)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.listRoleAfterID != 12 || service.listRoleLimit != 25 || service.listRoleQuery != "operator" {
		t.Fatalf(
			"list args after=%d limit=%d query=%q",
			service.listRoleAfterID,
			service.listRoleLimit,
			service.listRoleQuery,
		)
	}
	if !strings.Contains(rr.Body.String(), `"next_after_id":"99"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestSetRolePermissionsRequiresVersion(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
		csrf:       true,
	}, service)

	request := authenticatedRequest(
		http.MethodPut,
		"/api/v1/admin/rbac/roles/9/permissions",
		`{"permissions":["tenant.read"]}`,
	)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf-token"})
	request.Header.Set("X-CSRF-Token", "csrf-token")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSetRolePermissionsAcceptsQuotedVersion(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 2},
		permission: true,
		csrf:       true,
	}, service)

	request := authenticatedRequest(
		http.MethodPut,
		"/api/v1/admin/rbac/roles/9/permissions",
		`{"version":"7","permissions":["tenant.read"]}`,
	)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf-token"})
	request.Header.Set("X-CSRF-Token", "csrf-token")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.setRoleVersion != 7 {
		t.Fatalf("role version = %d", service.setRoleVersion)
	}
}

func TestListGroupMembersUsesTenantScopedCursor(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 42},
		permission: true,
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(
		rr,
		authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/groups/7/members?after_id=11&limit=10", ""),
	)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.listGroupMemberCalls != 1 ||
		service.listGroupMemberTenant != 42 ||
		service.listGroupMemberGroup != 7 ||
		service.listGroupMemberAfter != 11 ||
		service.listGroupMemberLimit != 10 {
		t.Fatalf(
			"list group members args calls=%d tenant=%d group=%d after=%d limit=%d",
			service.listGroupMemberCalls,
			service.listGroupMemberTenant,
			service.listGroupMemberGroup,
			service.listGroupMemberAfter,
			service.listGroupMemberLimit,
		)
	}
	if !strings.Contains(rr.Body.String(), `"next_after_id":"22"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestListAssignmentsRejectsOversizedLimit(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity:    identity.SessionIdentity{UserID: 1, TenantID: 42},
		permission: true,
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(
		rr,
		authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/groups/7/members?limit=201", ""),
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.listGroupMemberCalls != 0 {
		t.Fatalf("list group member calls = %d", service.listGroupMemberCalls)
	}
}

func TestAssignmentUserReadsRequireUsersPermission(t *testing.T) {
	service := &fakeRBAC{}
	router := newRouter(fakeAuth{
		identity: identity.SessionIdentity{UserID: 1, TenantID: 42},
		permissions: map[string]bool{
			"admin.rbac.manage":  true,
			"admin.users.manage": false,
		},
	}, service)

	rr := httptest.NewRecorder()
	router.ServeHTTP(
		rr,
		authenticatedRequest(http.MethodGet, "/api/v1/admin/rbac/groups/7/members", ""),
	)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if service.listGroupMemberCalls != 0 {
		t.Fatalf("list group member calls = %d", service.listGroupMemberCalls)
	}
}

func TestUserBearingRBACWritesRequireUsersPermission(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		calls func(*fakeRBAC) int
	}{
		{
			name:  "group membership",
			path:  "/api/v1/admin/rbac/groups/7/members/9",
			calls: func(service *fakeRBAC) int { return service.addGroupMemberCalls },
		},
		{
			name:  "direct user role",
			path:  "/api/v1/admin/rbac/users/9/roles/11",
			calls: func(service *fakeRBAC) int { return service.assignUserRoleCalls },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeRBAC{}
			router := newRouter(fakeAuth{
				identity: identity.SessionIdentity{UserID: 1, TenantID: 42},
				permissions: map[string]bool{
					"admin.rbac.manage":  true,
					"admin.users.manage": false,
				},
				csrf: true,
			}, service)

			request := authenticatedRequest(http.MethodPost, test.path, "")
			request.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf-token"})
			request.Header.Set("X-CSRF-Token", "csrf-token")

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, request)

			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
			}
			if calls := test.calls(service); calls != 0 {
				t.Fatalf("service calls = %d", calls)
			}
		})
	}
}

func TestRBACPlatformAdminRequirementReturnsForbidden(t *testing.T) {
	rr := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/rbac/groups/7/roles/11", nil)

	writeServiceError(rr, request, identity.ErrPlatformAdminRequired)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "PLATFORM_ADMIN_REQUIRED") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}
