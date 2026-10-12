package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

type fakeAccountService struct {
	passwordCalls int
	identity      SessionIdentity
}

func (f *fakeAccountService) UpdateProfile(context.Context, SessionIdentity, ProfileInput, RequestMeta) error {
	return nil
}
func (f *fakeAccountService) ChangePassword(_ context.Context, i SessionIdentity, _ PasswordInput, _ RequestMeta) (LoginResult, error) {
	f.passwordCalls++
	f.identity = i
	return LoginResult{Identity: SessionIdentity{UserID: i.UserID, Version: 2, CredentialVersion: 2, ExpiresAt: time.Now().Add(time.Hour)}, SessionToken: "secret-session", CSRFToken: "secret-csrf"}, nil
}
func (f *fakeAccountService) ListOwnSessions(_ context.Context, i SessionIdentity, _ uint64, _ int) (AccountSessions, error) {
	f.identity = i
	return AccountSessions{Sessions: []AccountSession{{ID: 9007199254740993, Current: true, TenantID: 9007199254740995}}, NextAfterID: 9007199254740993}, nil
}
func (f *fakeAccountService) RevokeOwnSessions(context.Context, SessionIdentity, uint64, RequestMeta) error {
	return nil
}

func TestAccountHTTPUsesSessionActorAndPreventsSecretSerialization(t *testing.T) {
	f := &fakeAccountService{}
	auth := fakeAuthService{authenticateFn: func(context.Context, string) (SessionIdentity, error) {
		return SessionIdentity{UserID: 9007199254740993, TenantID: 17}, nil
	}, verifyCSRFFn: func(SessionIdentity, string) bool { return true }}
	limiter := &fakeLimiter{allowed: true}
	h := NewHandler(auth, limiter, testSessionConfig(), testAuthConfig())
	m := NewMiddleware(auth, testSessionConfig())
	router := chi.NewRouter()
	RegisterRoutes(router, h, m, NewAccountHandler(f, h))
	call := func(method, path, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "session", Value: "session"})
		if csrf {
			r.AddCookie(&http.Cookie{Name: "csrf", Value: "csrf"})
			r.Header.Set("X-CSRF-Token", "csrf")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	body := `{"current_password":"old-password","new_password":"new-password-long","credential_version":"1"}`
	if w := call("POST", "/api/v1/auth/password", body, false); w.Code != 403 || f.passwordCalls != 0 {
		t.Fatal("CSRF failure reached password service")
	}
	limiter.allowed = false
	if w := call("POST", "/api/v1/auth/password", body, true); w.Code != 429 || f.passwordCalls != 0 || w.Header().Get("Retry-After") == "" {
		t.Fatal("rate limit failed")
	}
	limiter.allowed = true
	if w := call("POST", "/api/v1/auth/password", strings.TrimSuffix(body, "}")+`,"user_id":"999"}`, true); w.Code != 400 || f.passwordCalls != 0 {
		t.Fatal("foreign identity input accepted")
	}
	w := call("POST", "/api/v1/auth/password", body, true)
	if w.Code != 200 || f.identity.UserID != 9007199254740993 || f.identity.TenantID != 17 || len(w.Result().Cookies()) != 2 {
		t.Fatalf("password transport: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret-") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("secret serialized")
	}
	w = call("GET", "/api/v1/auth/sessions", "", false)
	for _, fragment := range []string{`"id":"9007199254740993"`, `"tenant_id":"9007199254740995"`, `"next_after_id":"9007199254740993"`} {
		if !strings.Contains(w.Body.String(), fragment) {
			t.Fatal("BIGINT precision lost")
		}
	}
}
