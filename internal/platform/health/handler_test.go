package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLive(t *testing.T) {
	h := New(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	rr := httptest.NewRecorder()
	h.Live(rr, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestReadyFailsWhenDependencyUnavailable(t *testing.T) {
	h := New(func(context.Context) error { return errors.New("down") }, func(context.Context) error { return nil })
	rr := httptest.NewRecorder()
	h.Ready(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"mysql":"unavailable"`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
}
