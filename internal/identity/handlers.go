package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/go-chi/chi/v5/middleware"
)

type Handler struct {
	service AuthService
	limiter LoginLimiter
	session config.SessionConfig
	auth    config.AuthConfig
}

func NewHandler(service AuthService, limiter LoginLimiter, session config.SessionConfig, auth config.AuthConfig) *Handler {
	return &Handler{service: service, limiter: limiter, session: session, auth: auth}
}

type loginRequest struct {
	Tenant   string `json:"tenant"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request loginRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	if len(strings.TrimSpace(request.Tenant)) > 100 ||
		len(strings.TrimSpace(request.Email)) > 254 ||
		len(request.Password) > password.MaxPasswordBytes {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid credentials")
		return
	}

	ip := requestinfo.ClientIP(r)
	allowed, retryAfter, err := h.allowLogin(r, request.Tenant, request.Email, ip)
	if err != nil {
		if httpapi.WriteContextError(w, r, err) {
			return
		}
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "AUTH_RATE_LIMIT_UNAVAILABLE", "Authentication temporarily unavailable")
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
		httpapi.WriteError(w, r, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "Too many authentication attempts")
		return
	}

	result, err := h.service.Login(r.Context(), LoginInput{
		Tenant: request.Tenant, Email: request.Email, Password: request.Password,
	}, RequestMeta{
		RequestID: middleware.GetReqID(r.Context()),
		IPAddress: ip,
		UserAgent: r.UserAgent(),
	})
	if errors.Is(err, ErrInvalidCredentials) {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid credentials")
		return
	}
	if errors.Is(err, ErrAuthBusy) {
		w.Header().Set("Retry-After", "1")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "AUTH_BUSY", "Authentication is temporarily busy")
		return
	}
	if err != nil {
		if httpapi.WriteContextError(w, r, err) {
			return
		}
		httpapi.WriteError(w, r, http.StatusInternalServerError, "LOGIN_FAILED", "Authentication failed")
		return
	}

	h.setSessionCookies(w, result)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"user":                     accountUser(result.Identity),
		"password_change_required": result.Identity.PasswordChangeRequired,
		"tenant": map[string]any{
			"id":   strconv.FormatUint(result.Identity.TenantID, 10),
			"slug": result.Identity.TenantSlug,
			"name": result.Identity.TenantName,
		},
		"expires_at": result.Identity.ExpiresAt,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	permissions, err := h.service.Permissions(r.Context(), identity)
	if err != nil {
		if httpapi.WriteContextError(w, r, err) {
			return
		}
		httpapi.WriteError(w, r, http.StatusInternalServerError, "PERMISSIONS_FAILED", "Unable to load permissions")
		return
	}
	if permissions == nil {
		permissions = []string{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"user":                     accountUser(identity),
		"password_change_required": identity.PasswordChangeRequired,
		"tenant": map[string]any{
			"id":   strconv.FormatUint(identity.TenantID, 10),
			"slug": identity.TenantSlug,
			"name": identity.TenantName,
		},
		"permissions": permissions,
		"expires_at":  identity.ExpiresAt,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	identity, ok := FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if err := h.service.Logout(r.Context(), identity, RequestMeta{
		RequestID: middleware.GetReqID(r.Context()),
		IPAddress: requestinfo.ClientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		if httpapi.WriteContextError(w, r, err) {
			return
		}
		httpapi.WriteError(w, r, http.StatusInternalServerError, "LOGOUT_FAILED", "Logout failed")
		return
	}
	h.clearSessionCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) allowLogin(r *http.Request, tenant, email, ip string) (bool, time.Duration, error) {
	if ip == "" {
		ip = "unknown"
	}
	allowed, err := h.limiter.Allow(
		r.Context(),
		"login:ip:"+ip,
		int64(h.auth.LoginIPLimit),
		h.auth.LoginIPWindow,
	)
	if err != nil {
		return false, 0, err
	}
	if !allowed {
		return false, h.auth.LoginIPWindow, nil
	}

	account := sha256.Sum256([]byte(
		strings.ToLower(strings.TrimSpace(tenant)) + "\x00" +
			normalizeLoginEmail(email),
	))
	allowed, err = h.limiter.Allow(
		r.Context(),
		"login:account:"+hex.EncodeToString(account[:]),
		int64(h.auth.LoginAccountLimit),
		h.auth.LoginAccountWindow,
	)
	if err != nil {
		return false, 0, err
	}
	if !allowed {
		return false, h.auth.LoginAccountWindow, nil
	}
	return true, 0, nil
}

func (h *Handler) setSessionCookies(w http.ResponseWriter, result LoginResult) {
	maxAge := int(time.Until(result.Identity.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.session.CookieName,
		Value:    result.SessionToken,
		Path:     "/",
		Expires:  result.Identity.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.session.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     h.session.CSRFCookieName,
		Value:    result.CSRFToken,
		Path:     "/",
		Expires:  result.Identity.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   h.session.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearSessionCookies(w http.ResponseWriter) {
	expired := time.Unix(1, 0).UTC()
	for _, cookie := range []http.Cookie{
		{Name: h.session.CookieName, HttpOnly: true},
		{Name: h.session.CSRFCookieName, HttpOnly: false},
	} {
		cookie.Value = ""
		cookie.Path = "/"
		cookie.Expires = expired
		cookie.MaxAge = -1
		cookie.Secure = h.session.CookieSecure
		cookie.SameSite = http.SameSiteStrictMode
		http.SetCookie(w, &cookie)
	}
}
