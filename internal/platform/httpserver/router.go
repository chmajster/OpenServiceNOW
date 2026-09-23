package httpserver

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type RouteRegistrar func(chi.Router)

func Router(
	logger *slog.Logger,
	healthHandler *health.Handler,
	globalMiddleware []func(http.Handler) http.Handler,
	registrars ...RouteRegistrar,
) http.Handler {
	r := chi.NewRouter()
	r.Use(sanitizeIncomingRequestID)
	r.Use(middleware.RequestID)
	r.Use(requestIDHeader)
	for _, item := range globalMiddleware {
		if item != nil {
			r.Use(item)
		}
	}
	r.Use(accessLog(logger))
	r.Use(panicRecovery(logger))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(securityHeaders)
	r.Use(limitBody(2 << 20))

	r.Get("/health/live", healthHandler.Live)
	r.Get("/health/ready", healthHandler.Ready)

	for _, register := range registrars {
		if register != nil {
			register(r)
		}
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteError(w, r, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	})
	return r
}

func panicRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recover() != nil {
					logger.Error(
						"http panic recovered",
						"request_id", middleware.GetReqID(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
					)
					httpapi.WriteError(
						w,
						r,
						http.StatusInternalServerError,
						"INTERNAL_SERVER_ERROR",
						"Internal server error",
					)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"request_id", middleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func sanitizeIncomingRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const header = "X-Request-ID"
		value := r.Header.Get(header)
		if value != "" && !validRequestID(value) {
			r.Header.Del(header)
		}
		next.ServeHTTP(w, r)
	})
}

func validRequestID(value string) bool {
	if len(value) < 1 || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case strings.ContainsRune("._:-", char):
		default:
			return false
		}
	}
	return true
}

func requestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID := middleware.GetReqID(r.Context()); requestID != "" {
			w.Header().Set("X-Request-ID", requestID)
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func limitBody(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}
