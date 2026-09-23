package users

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

type fakeAuthService struct{}

func (fakeAuthService) Login(context.Context, identity.LoginInput, identity.RequestMeta) (identity.LoginResult, error) {
	return identity.LoginResult{}, identity.ErrInvalidCredentials
}

func (fakeAuthService) Authenticate(context.Context, string) (identity.SessionIdentity, error) {
	return identity.SessionIdentity{UserID: 1, TenantID: 7, Email: "admin@example.test"}, nil
}

func (fakeAuthService) VerifyCSRF(identity.SessionIdentity, string) bool {
	return true
}

func (fakeAuthService) Logout(context.Context, identity.SessionIdentity, identity.RequestMeta) error {
	return nil
}

func (fakeAuthService) Permissions(context.Context, identity.SessionIdentity) ([]string, error) {
	return []string{"admin.users.manage"}, nil
}

func (fakeAuthService) HasPermission(context.Context, identity.SessionIdentity, string) (bool, error) {
	return true, nil
}

type fakeUsersService struct {
	listTenantID uint64
	listAfterID  uint64
	listLimit    int
	listQuery    string
	listCalls    int
	addActor     Actor
	addInput     AddExistingInput
	addID        uint64
	addErr       error
}

func (f *fakeUsersService) List(
	_ context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (Page, error) {
	f.listCalls++
	f.listTenantID = tenantID
	f.listAfterID = afterID
	f.listLimit = limit
	f.listQuery = query
	return Page{
		Users: []User{{
			ID: 88, Email: "operator@example.test", DisplayName: "Operator",
			UserActive: true, MembershipActive: true,
		}},
		NextAfterID: 88,
	}, nil
}

func (*fakeUsersService) Create(context.Context, Actor, CreateInput) (uint64, error) {
	return 0, nil
}

func (f *fakeUsersService) AddExistingMembership(
	_ context.Context,
	actor Actor,
	input AddExistingInput,
) (uint64, error) {
	f.addActor = actor
	f.addInput = input
	return f.addID, f.addErr
}

func (*fakeUsersService) SetMembershipActive(context.Context, Actor, uint64, bool) error {
	return nil
}

func usersTestRouter(service APIService) http.Handler {
	router := chi.NewRouter()
	auth := identity.NewMiddleware(fakeAuthService{}, config.SessionConfig{
		TTL:            time.Hour,
		CookieName:     "session",
		CSRFCookieName: "csrf",
	})
	RegisterRoutes(router, NewHandler(service), auth)
	return router
}

func authenticatedUsersRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})
	return request
}

func TestListRejectsOversizedLimit(t *testing.T) {
	service := &fakeUsersService{}
	recorder := httptest.NewRecorder()
	usersTestRouter(service).ServeHTTP(
		recorder,
		authenticatedUsersRequest("/api/v1/admin/users/?limit=201"),
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.listCalls != 0 {
		t.Fatalf("list calls = %d", service.listCalls)
	}
}

func TestListPassesCursorLimitAndSearch(t *testing.T) {
	service := &fakeUsersService{}
	recorder := httptest.NewRecorder()
	usersTestRouter(service).ServeHTTP(
		recorder,
		authenticatedUsersRequest("/api/v1/admin/users/?after_id=40&limit=25&q=operator"),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.listTenantID != 7 || service.listAfterID != 40 || service.listLimit != 25 || service.listQuery != "operator" {
		t.Fatalf(
			"list args tenant=%d after=%d limit=%d query=%q",
			service.listTenantID,
			service.listAfterID,
			service.listLimit,
			service.listQuery,
		)
	}
	if !strings.Contains(recorder.Body.String(), `"next_after_id":"88"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestListRejectsInvalidCursor(t *testing.T) {
	service := &fakeUsersService{}
	recorder := httptest.NewRecorder()
	usersTestRouter(service).ServeHTTP(
		recorder,
		authenticatedUsersRequest("/api/v1/admin/users/?after_id=0"),
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.listCalls != 0 {
		t.Fatalf("list calls = %d", service.listCalls)
	}
}

func TestAddExistingMembershipUsesAuthenticatedTenantAndStringID(t *testing.T) {
	const largeID uint64 = 9007199254740993
	service := &fakeUsersService{addID: largeID}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/users/memberships",
		strings.NewReader(`{"email":"existing@example.test"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})
	request.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf-token"})

	recorder := httptest.NewRecorder()
	usersTestRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.addActor.TenantID != 7 || service.addActor.UserID != 1 {
		t.Fatalf("actor = %+v", service.addActor)
	}
	if service.addInput.Email != "existing@example.test" {
		t.Fatalf("input = %+v", service.addInput)
	}
	if !strings.Contains(recorder.Body.String(), `"id":"9007199254740993"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAddExistingMembershipRequiresCSRF(t *testing.T) {
	service := &fakeUsersService{addID: 1}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/users/memberships",
		strings.NewReader(`{"email":"existing@example.test"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque-token"})

	recorder := httptest.NewRecorder()
	usersTestRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
