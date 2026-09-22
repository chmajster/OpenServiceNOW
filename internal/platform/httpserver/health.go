package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Checker interface {
	Name() string
	Check(context.Context) error
}

type HealthHandler struct {
	logger  *slog.Logger
	timeout time.Duration
	checks  []Checker
}

func NewHealthHandler(logger *slog.Logger, timeout time.Duration, checks ...Checker) *HealthHandler {
	return &HealthHandler{logger: logger, timeout: timeout, checks: checks}
}

func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *HealthHandler) Ready(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), h.timeout)
	defer cancel()

	components := make(map[string]string, len(h.checks))
	ready := true
	for _, check := range h.checks {
		if err := check.Check(ctx); err != nil {
			ready = false
			components[check.Name()] = "unavailable"
			h.logger.ErrorContext(req.Context(), "readiness check failed", "component", check.Name(), "error", err)
			continue
		}
		components[check.Name()] = "ok"
	}

	status := http.StatusOK
	state := "ok"
	if !ready {
		status = http.StatusServiceUnavailable
		state = "unavailable"
	}
	writeJSON(w, status, map[string]any{"status": state, "components": components})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
