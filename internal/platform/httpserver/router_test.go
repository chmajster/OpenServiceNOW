package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
	"github.com/go-chi/chi/v5"
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

func TestRouterPreservesSafeIncomingRequestID(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	request.Header.Set("X-Request-ID", "client-req_123")

	rr := httptest.NewRecorder()
	Router(logger, h, nil).ServeHTTP(rr, request)

	if got := rr.Header().Get("X-Request-ID"); got != "client-req_123" {
		t.Fatalf("X-Request-ID = %q", got)
	}
}

func TestRouterReplacesUnsafeIncomingRequestID(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	request.Header.Set("X-Request-ID", strings.Repeat("x", 200))

	rr := httptest.NewRecorder()
	Router(logger, h, nil).ServeHTTP(rr, request)

	got := rr.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("X-Request-ID is missing")
	}
	if got == strings.Repeat("x", 200) {
		t.Fatal("unsafe client request ID was preserved")
	}
	if len(got) > 128 {
		t.Fatalf("generated X-Request-ID is unexpectedly long: %d", len(got))
	}
}

func TestRouterRecoversPanicWithJSONError(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	handler := Router(logger, h, nil, func(r chi.Router) {
		r.Get("/panic", func(http.ResponseWriter, *http.Request) {
			panic("sensitive panic detail")
		})
	})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rr.Body.String(), `"code":"INTERNAL_SERVER_ERROR"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sensitive panic detail") {
		t.Fatalf("panic detail leaked in body: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"request_id"`) {
		t.Fatalf("request ID missing from body: %s", rr.Body.String())
	}
}

func TestRouterIgnoresClientSuppliedRequestID(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	request.Header.Set("X-Request-Id", strings.Repeat("client-controlled-", 20))

	rr := httptest.NewRecorder()
	Router(logger, h, nil).ServeHTTP(rr, request)

	got := rr.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("X-Request-ID is missing")
	}
	if got == request.Header.Get("X-Request-Id") {
		t.Fatal("server must not trust client-supplied request IDs")
	}
	if len(got) > 128 {
		t.Fatalf("server request ID length = %d", len(got))
	}
}
