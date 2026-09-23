package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
)

func TestRouterSecurityHeaders(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	rr := httptest.NewRecorder()
	Router(logger, h, nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rr.Header().Get("Permissions-Policy"); got == "" {
		t.Fatal("Permissions-Policy is missing")
	}
	if got := rr.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("X-Request-ID is missing")
	}
}

func TestRouterUsesConsistentNotFoundError(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	rr := httptest.NewRecorder()
	Router(logger, h, nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}
