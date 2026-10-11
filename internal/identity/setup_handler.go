package identity

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const setupCookieName = "opensnow_setup"

type SetupHandler struct {
	service SetupService
	limiter LoginLimiter
	session config.SessionConfig
}

func NewSetupHandler(service SetupService, limiter LoginLimiter, session config.SessionConfig) *SetupHandler {
	return &SetupHandler{service: service, limiter: limiter, session: session}
}

func RegisterSetupRoutes(r chi.Router, handler *SetupHandler) {
	r.Get("/api/v1/setup", handler.Status)
	r.Post("/api/v1/setup", handler.Create)
}

func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	required, err := h.service.Required(r.Context())
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}
	response := map[string]any{"required": required}
	if required {
		token := ""
		if cookie, err := r.Cookie(setupCookieName); err == nil && len(cookie.Value) == tokenEncodedLength {
			token = cookie.Value
		}
		if token == "" {
			var err error
			token, _, err = newToken()
			if err != nil {
				h.writeUnavailable(w, r, err)
				return
			}
		}
		http.SetCookie(w, &http.Cookie{
			Name: setupCookieName, Value: token, Path: "/api/v1/setup",
			HttpOnly: true, Secure: h.session.CookieSecure, SameSite: http.SameSiteStrictMode,
			MaxAge: 1200, Expires: time.Now().Add(20 * time.Minute),
		})
		response["token"] = token
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *SetupHandler) Create(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !setupSameOrigin(r) {
		httpapi.WriteError(w, r, http.StatusForbidden, "SETUP_ORIGIN_REJECTED", "Open the installer on this application's own address")
		return
	}
	required, err := h.service.Required(r.Context())
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}
	if !required {
		httpapi.WriteError(w, r, http.StatusConflict, "SETUP_COMPLETED", "Installation is already complete; sign in with your existing account")
		return
	}
	token := r.Header.Get("X-Setup-Token")
	cookie, err := r.Cookie(setupCookieName)
	if err != nil || len(token) != tokenEncodedLength ||
		len(cookie.Value) != tokenEncodedLength || subtle.ConstantTimeCompare([]byte(token), []byte(cookie.Value)) != 1 {
		httpapi.WriteError(w, r, http.StatusForbidden, "SETUP_TOKEN_INVALID", "Installer session expired; refresh the page and try again")
		return
	}
	allowed, err := h.limiter.Allow(r.Context(), "setup:ip:"+requestinfo.ClientIP(r), 10, time.Minute)
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		httpapi.WriteError(w, r, http.StatusTooManyRequests, "SETUP_RATE_LIMITED", "Too many installation attempts; try again in a minute")
		return
	}
	var input struct {
		TenantSlug string `json:"tenant"`
		TenantName string `json:"tenant_name"`
		AdminEmail string `json:"email"`
		AdminName  string `json:"display_name"`
		Password   string `json:"password"`
	}
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.Create(r.Context(), BootstrapInput{
		TenantSlug: input.TenantSlug, TenantName: input.TenantName,
		AdminEmail: input.AdminEmail, AdminName: input.AdminName, AdminPassword: input.Password,
	}, RequestMeta{RequestID: middleware.GetReqID(r.Context()), IPAddress: requestinfo.ClientIP(r), UserAgent: r.UserAgent()})
	if errors.Is(err, ErrSetupCompleted) {
		httpapi.WriteError(w, r, http.StatusConflict, "SETUP_COMPLETED", "Installation is already complete; sign in with your existing account")
		return
	}
	if errors.Is(err, ErrSetupInvalid) {
		httpapi.WriteError(w, r, http.StatusUnprocessableEntity, "SETUP_INVALID", strings.TrimPrefix(err.Error(), ErrSetupInvalid.Error()+": "))
		return
	}
	if errors.Is(err, ErrAuthBusy) {
		w.Header().Set("Retry-After", "1")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "AUTH_BUSY", "Installation is temporarily busy; try again shortly")
		return
	}
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: setupCookieName, Value: "", Path: "/api/v1/setup", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: h.session.CookieSecure, SameSite: http.SameSiteStrictMode,
	})
	httpapi.WriteJSON(w, http.StatusCreated, result)
}

func (h *SetupHandler) writeUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	if !httpapi.WriteContextError(w, r, err) {
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "SETUP_UNAVAILABLE", "Installation is temporarily unavailable; check the connection and try again")
	}
}

func setupSameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		return err == nil && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" &&
			(parsed.Scheme == "http" || parsed.Scheme == "https") && strings.EqualFold(parsed.Host, r.Host)
	}
	return true
}
