package identity

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

func TestNormalizeLoginEmail(t *testing.T) {
	for input, want := range map[string]string{
		"admin":                DefaultAdminEmail,
		" ADMIN ":              DefaultAdminEmail,
		" Admin@Example.Test ": DefaultAdminEmail,
		" User@Example.Test ":  "user@example.test",
		"administrator":        "administrator",
		"":                     "",
	} {
		if got := normalizeLoginEmail(input); got != want {
			t.Errorf("normalizeLoginEmail(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBootstrapPasswordExceptionIsRestrictedToDefaultAccount(t *testing.T) {
	hash, err := hashBootstrapPassword(BootstrapInput{AdminEmail: DefaultAdminEmail, AdminPassword: DefaultAdminPassword})
	if err != nil {
		t.Fatalf("hash default administrator password: %v", err)
	}
	valid, err := password.Verify(hash, DefaultAdminPassword)
	if err != nil || !valid {
		t.Fatalf("verify default password: valid=%t err=%v", valid, err)
	}
	for _, input := range []BootstrapInput{
		{AdminEmail: "other@example.test", AdminPassword: DefaultAdminPassword},
		{AdminEmail: DefaultAdminEmail, AdminPassword: "short"},
		{AdminEmail: DefaultAdminEmail, AdminPassword: ""},
	} {
		if _, err := hashBootstrapPassword(input); err == nil {
			t.Errorf("short password unexpectedly accepted for %q", input.AdminEmail)
		}
	}
}

type recordingLoginLimiter struct {
	keys []string
}

func (l *recordingLoginLimiter) Allow(_ context.Context, key string, _ int64, _ time.Duration) (bool, error) {
	l.keys = append(l.keys, key)
	return true, nil
}

func TestAdministratorAliasSharesEmailRateLimit(t *testing.T) {
	limiter := &recordingLoginLimiter{}
	handler := NewHandler(fakeAuthService{}, limiter, testSessionConfig(), testAuthConfig())
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	for _, login := range []string{DefaultAdminLogin, DefaultAdminEmail, " ADMIN "} {
		allowed, _, err := handler.allowLogin(request, "default", login, "192.0.2.1")
		if err != nil || !allowed {
			t.Fatalf("allowLogin(%q): allowed=%t err=%v", login, allowed, err)
		}
	}
	if len(limiter.keys) != 6 || limiter.keys[1] != limiter.keys[3] || limiter.keys[1] != limiter.keys[5] {
		t.Fatalf("administrator aliases use different account limits: %v", limiter.keys)
	}
}
