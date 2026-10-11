package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
)

type setupServiceStub struct {
	required  bool
	statusErr error
	createErr error
	calls     int
	input     BootstrapInput
}

func (s *setupServiceStub) Required(context.Context) (bool, error) { return s.required, s.statusErr }
func (s *setupServiceStub) Create(_ context.Context, input BootstrapInput, _ RequestMeta) (SetupResult, error) {
	s.calls++
	s.input = input
	return SetupResult{TenantSlug: input.TenantSlug, AdminEmail: input.AdminEmail}, s.createErr
}

type setupLimiterStub struct {
	allowed bool
	err     error
}

func (s setupLimiterStub) Allow(context.Context, string, int64, time.Duration) (bool, error) {
	return s.allowed, s.err
}

func TestSetupStatusCookieAndPermanentClosure(t *testing.T) {
	service := &setupServiceStub{required: true}
	h := NewSetupHandler(service, setupLimiterStub{allowed: true}, config.SessionConfig{CookieSecure: true})
	rr := httptest.NewRecorder()
	h.Status(rr, httptest.NewRequest("GET", "https://app.test/api/v1/setup", nil))
	var body struct {
		Required bool   `json:"required"`
		Token    string `json:"token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	cookies := rr.Result().Cookies()
	if rr.Code != 200 || !body.Required || len(body.Token) != tokenEncodedLength || len(cookies) != 1 {
		t.Fatalf("response: %d %s", rr.Code, rr.Body.String())
	}
	cookie := cookies[0]
	if cookie.Value != body.Token || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/v1/setup" || cookie.MaxAge != 1200 {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("setup response must not be cached")
	}
	r := httptest.NewRequest("GET", "https://app.test/api/v1/setup", nil)
	r.AddCookie(cookie)
	rr = httptest.NewRecorder()
	h.Status(rr, r)
	if !strings.Contains(rr.Body.String(), body.Token) {
		t.Fatal("refresh should retain the installer token")
	}
	service.required = false
	rr = httptest.NewRecorder()
	h.Status(rr, r)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), "token") || strings.Contains(rr.Body.String(), "true") || len(rr.Result().Cookies()) != 0 {
		t.Fatalf("closed setup: %s", rr.Body.String())
	}
	service.statusErr = errors.New("sensitive database detail")
	rr = httptest.NewRecorder()
	h.Status(rr, r)
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "sensitive") || strings.Contains(rr.Body.String(), "required") {
		t.Fatalf("fail closed: %d %s", rr.Code, rr.Body.String())
	}
}

func TestSetupCreateSecurityValidationAndErrors(t *testing.T) {
	valid := `{"tenant":"default","tenant_name":"Example","email":"admin","display_name":"Admin","password":"admin"}`
	for _, tc := range []struct {
		name      string
		mutate    func(*http.Request)
		required  bool
		limiter   setupLimiterStub
		createErr error
		body      string
		status    int
		calls     int
	}{
		{name: "success", required: true, status: 201, calls: 1},
		{name: "missing cookie", required: true, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, status: 403},
		{name: "missing header", required: true, mutate: func(r *http.Request) { r.Header.Del("X-Setup-Token") }, status: 403},
		{name: "mismatched token", required: true, mutate: func(r *http.Request) { r.Header.Set("X-Setup-Token", strings.Repeat("b", 43)) }, status: 403},
		{name: "cross origin", required: true, mutate: func(r *http.Request) { r.Header.Set("Origin", "https://evil.test") }, status: 403},
		{name: "cross site", required: true, mutate: func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, status: 403},
		{name: "invalid origin", required: true, mutate: func(r *http.Request) { r.Header.Set("Origin", "null") }, status: 403},
		{name: "installed", status: 409},
		{name: "race loser", required: true, createErr: ErrSetupCompleted, status: 409, calls: 1},
		{name: "validation", required: true, createErr: fmt.Errorf("%w: invalid email", ErrSetupInvalid), status: 422, calls: 1},
		{name: "busy", required: true, createErr: ErrAuthBusy, status: 503, calls: 1},
		{name: "database failure", required: true, createErr: errors.New("sensitive SQL password"), status: 503, calls: 1},
		{name: "rate limit", required: true, limiter: setupLimiterStub{allowed: false}, status: 429},
		{name: "limiter unavailable", required: true, limiter: setupLimiterStub{err: errors.New("redis password")}, status: 503},
		{name: "malformed body", required: true, body: `{`, status: 400},
		{name: "unknown field", required: true, body: `{"admin":true}`, status: 400},
		{name: "wrong media", required: true, mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, status: 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &setupServiceStub{required: tc.required, createErr: tc.createErr}
			limiter := setupLimiterStub{allowed: true}
			if tc.name == "rate limit" || tc.name == "limiter unavailable" {
				limiter = tc.limiter
			}
			h := NewSetupHandler(service, limiter, config.SessionConfig{})
			body := valid
			if tc.body != "" {
				body = tc.body
			}
			r := httptest.NewRequest("POST", "https://app.test/api/v1/setup", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "https://app.test")
			r.Header.Set("X-Setup-Token", strings.Repeat("a", 43))
			r.AddCookie(&http.Cookie{Name: setupCookieName, Value: strings.Repeat("a", 43)})
			if tc.mutate != nil {
				tc.mutate(r)
			}
			rr := httptest.NewRecorder()
			h.Create(rr, r)
			if rr.Code != tc.status || service.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", rr.Code, service.calls, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "sensitive") || strings.Contains(rr.Body.String(), "password") || strings.Contains(rr.Body.String(), "redis password") {
				t.Fatalf("details leaked: %s", rr.Body.String())
			}
			if tc.status == 429 && rr.Header().Get("Retry-After") != "60" {
				t.Fatal("missing retry window")
			}
			if tc.name == "busy" && rr.Header().Get("Retry-After") != "1" {
				t.Fatal("missing busy retry")
			}
			if tc.status == 201 {
				if service.input.AdminPassword != "admin" || service.input.TenantName != "Example" {
					t.Fatalf("input %+v", service.input)
				}
				cookies := rr.Result().Cookies()
				if len(cookies) != 1 || cookies[0].MaxAge != -1 {
					t.Fatal("setup cookie was not cleared")
				}
			}
		})
	}
}

func TestInstallerUsesSharedPasswordCapacity(t *testing.T) {
	auth := &Service{passwordChecks: make(chan struct{}, 1)}
	auth.passwordChecks <- struct{}{}
	installer := NewInstaller(&Bootstrapper{}, auth)
	if _, err := installer.Create(context.Background(), BootstrapInput{}, RequestMeta{}); !errors.Is(err, ErrAuthBusy) {
		t.Fatalf("capacity error=%v", err)
	}
}

func TestSetupInvalidInputNeverTouchesDatabase(t *testing.T) {
	base := BootstrapInput{TenantSlug: "default", TenantName: "Example", AdminEmail: "admin", AdminName: "Administrator", AdminPassword: "admin"}
	for _, tc := range []struct {
		name   string
		mutate func(*BootstrapInput)
	}{
		{"empty tenant", func(i *BootstrapInput) { i.TenantSlug = "" }},
		{"invalid tenant", func(i *BootstrapInput) { i.TenantSlug = "-invalid" }},
		{"long tenant", func(i *BootstrapInput) { i.TenantSlug = strings.Repeat("a", 101) }},
		{"empty name", func(i *BootstrapInput) { i.TenantName = " " }},
		{"long admin name", func(i *BootstrapInput) { i.AdminName = strings.Repeat("界", 201) }},
		{"invalid email", func(i *BootstrapInput) { i.AdminEmail = "other" }},
		{"short custom password", func(i *BootstrapInput) { i.AdminPassword = "wrong" }},
		{"custom email default password", func(i *BootstrapInput) { i.AdminEmail = "owner@example.test" }},
		{"long password", func(i *BootstrapInput) { i.AdminPassword = strings.Repeat("x", 1025) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.mutate(&input)
			_, err := (&Bootstrapper{}).run(context.Background(), input, true, RequestMeta{})
			if !errors.Is(err, ErrSetupInvalid) {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}
