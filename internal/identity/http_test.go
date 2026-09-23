package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
)

type fakeAuthService struct {
	authenticateFn   func(context.Context, string) (SessionIdentity, error)
	verifyCSRFFn     func(SessionIdentity, string) bool
	loginFn          func(context.Context, LoginInput, RequestMeta) (LoginResult, error)
	logoutFn         func(context.Context, SessionIdentity, RequestMeta) error
	permissionsFn    func(context.Context, SessionIdentity) ([]string, error)
	hasPermissionFn  func(context.Context, SessionIdentity, string) (bool, error)
}

func (f fakeAuthService) Login(ctx context.Context, input LoginInput, meta RequestMeta) (LoginResult, error) {
	if f.loginFn != nil {
		return f.loginFn(ctx, input, meta)
	}
	return LoginResult{}, errors.New("unexpected Login call")
}

func (f fakeAuthService) Authenticate(ctx context.Context, token string) (SessionIdentity, error) {
	if f.authenticateFn != nil {
		return f.authenticateFn(ctx, token)
	}
	return SessionIdentity{}, ErrUnauthenticated
}

func (f fakeAuthService) VerifyCSRF(identity SessionIdentity, token string) bool {
	return f.verifyCSRFFn != nil && f.verifyCSRFFn(identity, token)
}

func (f fakeAuthService) Logout(ctx context.Context, identity SessionIdentity, meta RequestMeta) error {
	if f.logoutFn != nil {
		return f.logoutFn(ctx, identity, meta)
	}
	return nil
}

func (f fakeAuthService) Permissions(ctx context.Context, identity SessionIdentity) ([]string, error) {
	if f.permissionsFn != nil {
		return f.permissionsFn(ctx, identity)
	}
	return nil, nil
}

func (f fakeAuthService) HasPermission(ctx context.Context, identity SessionIdentity, permission string) (bool, error) {
	if f.hasPermissionFn != nil {
		return f.hasPermissionFn(ctx, identity, permission)
	}
	return false, nil
}

type fakeLimiter struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeLimiter) Allow(context.Context, string, int64, time.Duration) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

func testSessionConfig() config.SessionConfig {
	return config.SessionConfig{
		TTL:            time.Hour,
		CookieName:     "session",
		CSRFCookieName: "csrf",
	}
}

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		LoginIPLimit:       10,
		LoginIPWindow:      time.Minute,
		LoginAccountLimit:  20,
		LoginAccountWindow: 15 * time.Minute,
	}
}

func TestAuthenticateRejectsMissingCookie(t *testing.T) {
	middleware := NewMiddleware(fakeAuthService{}, testSessionConfig())
	nextCalled := false
	handler := middleware.Authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rr.Code)
	}
	if nextCalled {
		t.Fatal("next handler must not run")
	}
	if !strings.Contains(rr.Body.String(), `"code":"UNAUTHENTICATED"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestRequireCSRFRejectsMismatch(t *testing.T) {
	service := fakeAuthService{
		verifyCSRFFn: func(SessionIdentity, string) bool { return true },
	}
	middleware := NewMiddleware(service, testSessionConfig())
	handler := middleware.RequireCSRF(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run")
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request = request.WithContext(context.WithValue(request.Context(), identityContextKey, SessionIdentity{UserID: 1}))
	request.AddCookie(&http.Cookie{Name: "csrf", Value: "cookie-token"})
	request.Header.Set("X-CSRF-Token", "different-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestRequirePermissionEnforcesBackendDecision(t *testing.T) {
	service := fakeAuthService{
		hasPermissionFn: func(context.Context, SessionIdentity, string) (bool, error) {
			return false, nil
		},
	}
	middleware := NewMiddleware(service, testSessionConfig())
	handler := middleware.RequirePermission("admin.rbac.manage")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run")
	}))

	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request = request.WithContext(context.WithValue(request.Context(), identityContextKey, SessionIdentity{UserID: 1, TenantID: 2}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestLoginRateLimitStopsBeforeAuthentication(t *testing.T) {
	limiter := &fakeLimiter{allowed: false}
	service := fakeAuthService{
		loginFn: func(context.Context, LoginInput, RequestMeta) (LoginResult, error) {
			t.Fatal("Login must not run when rate limited")
			return LoginResult{}, nil
		},
	}
	handler := NewHandler(service, limiter, testSessionConfig(), testAuthConfig())

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"tenant":"default","email":"admin@example.test","password":"not-a-real-password"}`),
	)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Login(rr, request)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rr.Code)
	}
	if limiter.calls != 1 {
		t.Fatalf("rate limiter calls = %d", limiter.calls)
	}
}

func TestMeReturnsEffectivePermissions(t *testing.T) {
	service := fakeAuthService{
		permissionsFn: func(context.Context, SessionIdentity) ([]string, error) {
			return []string{"admin.access", "tenant.read"}, nil
		},
	}
	handler := NewHandler(service, &fakeLimiter{allowed: true}, testSessionConfig(), testAuthConfig())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request = request.WithContext(context.WithValue(request.Context(), identityContextKey, SessionIdentity{
		UserID: 1, TenantID: 2, Email: "admin@example.test", TenantSlug: "default",
	}))
	rr := httptest.NewRecorder()
	handler.Me(rr, request)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"admin.access"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestLoginRejectsOversizedIdentifiersBeforeRateLimiter(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	service := fakeAuthService{
		loginFn: func(context.Context, LoginInput, RequestMeta) (LoginResult, error) {
			t.Fatal("Login must not run for oversized identifiers")
			return LoginResult{}, nil
		},
	}
	handler := NewHandler(service, limiter, testSessionConfig(), testAuthConfig())

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"tenant":"`+strings.Repeat("a", 101)+`","email":"admin@example.test","password":"not-a-real-password"}`),
	)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Login(rr, request)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if limiter.calls != 0 {
		t.Fatalf("rate limiter calls = %d", limiter.calls)
	}
}

func TestLoginReturns503WhenPasswordVerifierIsBusy(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	service := fakeAuthService{
		loginFn: func(context.Context, LoginInput, RequestMeta) (LoginResult, error) {
			return LoginResult{}, ErrAuthBusy
		},
	}
	handler := NewHandler(service, limiter, testSessionConfig(), testAuthConfig())

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"tenant":"default","email":"admin@example.test","password":"not-a-real-password"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:12345"
	rr := httptest.NewRecorder()
	handler.Login(rr, request)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q", got)
	}
	if !strings.Contains(rr.Body.String(), `"code":"AUTH_BUSY"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestLoginRequiresJSONContentType(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	service := fakeAuthService{
		loginFn: func(context.Context, LoginInput, RequestMeta) (LoginResult, error) {
			t.Fatal("Login must not run for unsupported media type")
			return LoginResult{}, nil
		},
	}
	handler := NewHandler(service, limiter, testSessionConfig(), testAuthConfig())

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"tenant":"default","email":"admin@example.test","password":"not-a-real-password"}`),
	)
	request.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	handler.Login(rr, request)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}
