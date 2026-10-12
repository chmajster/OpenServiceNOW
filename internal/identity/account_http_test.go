package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForcedPasswordChangeRejectsEveryOtherAuthenticatedRoute(t *testing.T) {
	m := NewMiddleware(fakeAuthService{authenticateFn: func(context.Context, string) (SessionIdentity, error) {
		return SessionIdentity{PasswordChangeRequired: true}, nil
	}}, testSessionConfig())
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/api/v1/auth/me", true}, {"POST", "/api/v1/auth/password", true}, {"POST", "/api/v1/auth/logout", true},
		{"GET", "/api/v1/admin/users/", false}, {"GET", "/api/v1/auth/sessions", false}, {"PUT", "/api/v1/auth/profile", false},
		{"DELETE", "/api/v1/auth/sessions", false}, {"GET", "/api/v1/auth/me/", false}, {"POST", "/api/v1/auth/me", false},
		{"GET", "/future-private-route", false},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		called := false
		m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
		if called != tc.allowed {
			t.Fatalf("%s %s allowed=%t", tc.method, tc.path, called)
		}
		if !tc.allowed && w.Code != http.StatusForbidden {
			t.Fatalf("status=%d", w.Code)
		}
	}
}
