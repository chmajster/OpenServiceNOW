package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func NewRouter(logger *slog.Logger, bodyLimit int64, healthTimeout time.Duration, checks ...Checker) http.Handler {
	router := chi.NewRouter()
	router.Use(RequestID)
	router.Use(SecurityHeaders)
	router.Use(BodyLimit(bodyLimit))
	router.Use(AccessLog(logger))
	router.Use(Recover(logger))

	health := NewHealthHandler(logger, healthTimeout, checks...)
	router.Get("/health/live", health.Live)
	router.Get("/health/ready", health.Ready)

	return router
}
