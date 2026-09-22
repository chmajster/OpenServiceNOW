package health

import (
	"context"
	"encoding/json"
	"net/http"
)

type CheckFunc func(context.Context) error

type Handler struct {
	mysql CheckFunc
	redis CheckFunc
}

func New(mysqlCheck, redisCheck CheckFunc) *Handler {
	return &Handler{mysql: mysqlCheck, redis: redisCheck}
}

func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{"mysql": "ok", "redis": "ok"}
	status := http.StatusOK
	if err := h.mysql(r.Context()); err != nil {
		checks["mysql"] = "unavailable"
		status = http.StatusServiceUnavailable
	}
	if err := h.redis(r.Context()); err != nil {
		checks["redis"] = "unavailable"
		status = http.StatusServiceUnavailable
	}
	state := "ok"
	if status != http.StatusOK {
		state = "unavailable"
	}
	writeJSON(w, status, map[string]any{"status": state, "checks": checks})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
