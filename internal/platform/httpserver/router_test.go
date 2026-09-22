package httpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeChecker struct {
	name string
	err  error
}

func (f fakeChecker) Name() string                { return f.name }
func (f fakeChecker) Check(context.Context) error { return f.err }

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestLiveness(t *testing.T) {
	handler := NewRouter(testLogger(), 1024, time.Second)
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if res.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request ID")
	}
	if res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
}

func TestReadinessReportsDependencyFailureWithoutLeakingError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := NewRouter(logger, 1024, time.Second,
		fakeChecker{name: "mysql"},
		fakeChecker{name: "redis", err: errors.New("secret connection details")},
	)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "secret connection details") {
		t.Fatalf("readiness response leaked internal error: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"redis":"unavailable"`) {
		t.Fatalf("missing dependency status: %s", res.Body.String())
	}
}
