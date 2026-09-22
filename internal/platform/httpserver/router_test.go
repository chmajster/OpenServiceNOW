package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
)

func TestRouterSecurityHeaders(t *testing.T) {
	h := health.New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	rr := httptest.NewRecorder()
	Router(h).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
}
