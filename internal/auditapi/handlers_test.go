package auditapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/go-chi/chi/v5"
)

type testAuth struct {
	identity.AuthService
	allowed bool
}

func (a testAuth) Authenticate(context.Context, string) (identity.SessionIdentity, error) {
	return identity.SessionIdentity{TenantID: 7, UserID: 9}, nil
}
func (a testAuth) HasPermission(context.Context, identity.SessionIdentity, string) (bool, error) {
	return a.allowed, nil
}

type testReader struct {
	tenant, before uint64
	limit, calls   int
	filter         audit.Filter
}

func (s *testReader) ListFiltered(_ context.Context, tenant, before uint64, limit int, filter audit.Filter) (audit.Page, error) {
	s.tenant, s.before, s.limit, s.filter = tenant, before, limit, filter
	s.calls++
	return audit.Page{Events: []audit.Record{}}, nil
}
func auditRouter(reader *testReader, allowed bool) http.Handler {
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(reader), identity.NewMiddleware(testAuth{allowed: allowed}, config.SessionConfig{CookieName: "session"}))
	return router
}
func TestAuditFiltersUseSessionScope(t *testing.T) {
	reader := &testReader{}
	request := httptest.NewRequest("GET", "/api/v1/admin/audit/?tenant_id=999&from=2026-10-12T10:00:00Z&to=2026-10-12T12:00:00Z&actor_user_id=9007199254740993&action=rbac.role.create&object_type=role&object_id=123&request_id=req-1&before_id=456&limit=2", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	recorder := httptest.NewRecorder()
	auditRouter(reader, true).ServeHTTP(recorder, request)
	if recorder.Code != 200 || reader.tenant != 7 || reader.before != 456 || reader.limit != 2 || reader.filter.ActorUserID != 9007199254740993 || reader.filter.Action != "rbac.role.create" || reader.filter.ObjectType != "role" || reader.filter.ObjectID != "123" || reader.filter.RequestID != "req-1" || !reader.filter.From.Equal(time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("status=%d reader=%+v", recorder.Code, reader)
	}
}
func TestInvalidAuditFiltersDoNotReadDatabase(t *testing.T) {
	for _, query := range []string{"from=yesterday", "from=2026-10-13T00:00:00Z&to=2026-10-12T00:00:00Z", "actor_user_id=0", "actor_user_id=%2B12", "actor_user_id=18446744073709551616", "action=" + strings.Repeat("a", 161), "object_type=" + strings.Repeat("a", 101), "request_id=" + strings.Repeat("a", 129), "object_id=%00", "limit=201", "before_id=invalid"} {
		t.Run(query, func(t *testing.T) {
			reader := &testReader{}
			request := httptest.NewRequest("GET", "/api/v1/admin/audit/?"+query, nil)
			request.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
			recorder := httptest.NewRecorder()
			auditRouter(reader, true).ServeHTTP(recorder, request)
			if recorder.Code != 400 || reader.calls != 0 {
				t.Fatalf("status=%d calls=%d", recorder.Code, reader.calls)
			}
		})
	}
}
func TestAuditAuthenticationAndPermission(t *testing.T) {
	for _, tc := range []struct {
		cookie, allowed bool
		status          int
	}{{false, true, 401}, {true, false, 403}} {
		reader := &testReader{}
		request := httptest.NewRequest("GET", "/api/v1/admin/audit/?action=tenant.name.update", nil)
		if tc.cookie {
			request.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
		}
		recorder := httptest.NewRecorder()
		auditRouter(reader, tc.allowed).ServeHTTP(recorder, request)
		if recorder.Code != tc.status || reader.calls != 0 {
			t.Fatalf("status=%d calls=%d", recorder.Code, reader.calls)
		}
	}
}
