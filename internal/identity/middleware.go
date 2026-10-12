package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
)

type contextKey string

const identityContextKey contextKey = "identity"

type Middleware struct {
	service AuthService
	session config.SessionConfig
}

func NewMiddleware(service AuthService, session config.SessionConfig) *Middleware {
	return &Middleware{service: service, session: session}
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(m.session.CookieName)
		if err != nil || cookie.Value == "" {
			httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
			return
		}

		identity, err := m.service.Authenticate(r.Context(), cookie.Value)
		if errors.Is(err, ErrUnauthenticated) {
			httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
			return
		}
		if err != nil {
			if httpapi.WriteContextError(w, r, err) {
				return
			}
			httpapi.WriteError(w, r, http.StatusInternalServerError, "AUTHENTICATION_FAILED", "Authentication failed")
			return
		}

		if identity.PasswordChangeRequired && !allowedDuringPasswordChange(r) {
			httpapi.WriteError(w, r, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "Change your temporary password before continuing")
			return
		}
		ctx := context.WithValue(r.Context(), identityContextKey, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func allowedDuringPasswordChange(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me") ||
		(r.Method == http.MethodPost && (r.URL.Path == "/api/v1/auth/password" || r.URL.Path == "/api/v1/auth/logout"))
}

func (m *Middleware) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := FromContext(r.Context())
		if !ok {
			httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
			return
		}

		header := r.Header.Get("X-CSRF-Token")
		cookie, err := r.Cookie(m.session.CSRFCookieName)
		if err != nil || header == "" || cookie.Value == "" ||
			subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 ||
			!m.service.VerifyCSRF(identity, header) {
			httpapi.WriteError(w, r, http.StatusForbidden, "CSRF_VALIDATION_FAILED", "CSRF validation failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := FromContext(r.Context())
			if !ok {
				httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
				return
			}
			allowed, err := m.service.HasPermission(r.Context(), identity, permission)
			if err != nil {
				if httpapi.WriteContextError(w, r, err) {
					return
				}
				httpapi.WriteError(w, r, http.StatusInternalServerError, "AUTHORIZATION_FAILED", "Authorization check failed")
				return
			}
			if !allowed {
				httpapi.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func FromContext(ctx context.Context) (SessionIdentity, bool) {
	identity, ok := ctx.Value(identityContextKey).(SessionIdentity)
	return identity, ok
}
