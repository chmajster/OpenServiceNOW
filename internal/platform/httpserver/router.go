package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
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
	r.Use(serverRequestID)
	r.Use(requestIDHeader)
	for _, item := range globalMiddleware {
		if item != nil {
			r.Use(item)
		}
	}
	r.Use(accessLog(logger))
	r.Use(panicRecovery(logger))
	r.Use(timeoutJSON(30 * time.Second))
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
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error(
						"http panic recovered",
						"request_id", middleware.GetReqID(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"response_started", ww.Status() != 0,
					)
					if ww.Status() == 0 {
						httpapi.WriteError(
							ww,
							r,
							http.StatusInternalServerError,
							"INTERNAL_SERVER_ERROR",
							"Internal server error",
						)
					}
				}
			}()
			next.ServeHTTP(ww, r)
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

var requestIDFallback atomic.Uint64

func serverRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [16]byte
		var requestID string
		if _, err := rand.Read(raw[:]); err == nil {
			requestID = hex.EncodeToString(raw[:])
		} else {
			requestID = fmt.Sprintf("fallback-%d-%d", time.Now().UnixNano(), requestIDFallback.Add(1))
		}
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID := middleware.GetReqID(r.Context()); requestID != "" {
			w.Header().Set("X-Request-ID", requestID)
		}
		next.ServeHTTP(w, r)
	})
}

func timeoutJSON(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			request := r.WithContext(ctx)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, request)

			if errors.Is(ctx.Err(), context.DeadlineExceeded) && ww.Status() == 0 {
				httpapi.WriteError(
					ww,
					request,
					http.StatusGatewayTimeout,
					"REQUEST_TIMEOUT",
					"Request timed out",
				)
			}
		})
	}
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

var incidentUploadPath = regexp.MustCompile(`^/api/v1/(incidents|requests)/[0-9]+/files$`)

func limitBody(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				limit := max
				if r.Method == "POST" && incidentUploadPath.MatchString(strings.TrimSuffix(r.URL.Path, "/")) {
					limit = attachments.MaxMultipartBytes
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
