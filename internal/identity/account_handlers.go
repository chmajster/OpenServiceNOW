package identity

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type AccountService interface {
	UpdateProfile(context.Context, SessionIdentity, ProfileInput, RequestMeta) error
	ChangePassword(context.Context, SessionIdentity, PasswordInput, RequestMeta) (LoginResult, error)
	ListOwnSessions(context.Context, SessionIdentity, uint64, int) (AccountSessions, error)
	RevokeOwnSessions(context.Context, SessionIdentity, uint64, RequestMeta) error
}

type AccountHandler struct {
	service AccountService
	auth    *Handler
}

func NewAccountHandler(service AccountService, auth *Handler) *AccountHandler {
	return &AccountHandler{service: service, auth: auth}
}

func registerAccountRoutes(r chi.Router, h *AccountHandler, m *Middleware) {
	r.Get("/sessions", h.Sessions)
	r.With(m.RequireCSRF).Put("/profile", h.Profile)
	r.With(m.RequireCSRF).Post("/password", h.Password)
	r.With(m.RequireCSRF).Delete("/sessions", h.RevokeOthers)
	r.With(m.RequireCSRF).Delete("/sessions/{sessionID}", h.RevokeSession)
}

func accountMeta(r *http.Request) RequestMeta {
	return RequestMeta{RequestID: middleware.GetReqID(r.Context()), IPAddress: requestinfo.ClientIP(r), UserAgent: r.UserAgent()}
}

func (h *AccountHandler) Profile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	i, _ := FromContext(r.Context())
	var input ProfileInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	if err := h.service.UpdateProfile(r.Context(), i, input, accountMeta(r)); err != nil {
		writeAccountError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AccountHandler) Password(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	i, _ := FromContext(r.Context())
	var input PasswordInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	// The account limit is global rather than scoped to an organization login.
	for _, limit := range []struct {
		key    string
		count  int
		window time.Duration
	}{
		{"password:ip:" + requestinfo.ClientIP(r), h.auth.auth.LoginIPLimit, h.auth.auth.LoginIPWindow},
		{"password:account:" + strconv.FormatUint(i.UserID, 10), h.auth.auth.LoginAccountLimit, h.auth.auth.LoginAccountWindow},
	} {
		allowed, err := h.auth.limiter.Allow(r.Context(), limit.key, int64(limit.count), limit.window)
		if err != nil {
			httpapi.WriteError(w, r, http.StatusServiceUnavailable, "AUTH_RATE_LIMIT_UNAVAILABLE", "Authentication temporarily unavailable")
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(limit.window.Seconds())))))
			httpapi.WriteError(w, r, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "Too many authentication attempts")
			return
		}
	}
	result, err := h.service.ChangePassword(r.Context(), i, input, accountMeta(r))
	if err != nil {
		writeAccountError(w, r, err)
		return
	}
	h.auth.setSessionCookies(w, result)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"user": accountUser(result.Identity), "password_change_required": false, "expires_at": result.Identity.ExpiresAt})
}

func (h *AccountHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	i, _ := FromContext(r.Context())
	limit := 50
	var after uint64
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			writeAccountError(w, r, ErrAccountInvalid)
			return
		}
	}
	if value := r.URL.Query().Get("after_id"); value != "" {
		after, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeAccountError(w, r, ErrAccountInvalid)
			return
		}
	}
	page, err := h.service.ListOwnSessions(r.Context(), i, after, limit)
	if err != nil {
		writeAccountError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (h *AccountHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "sessionID"), 10, 64)
	if err != nil || id == 0 {
		writeAccountError(w, r, ErrAccountInvalid)
		return
	}
	h.revoke(w, r, id)
}

func (h *AccountHandler) RevokeOthers(w http.ResponseWriter, r *http.Request) { h.revoke(w, r, 0) }

func (h *AccountHandler) revoke(w http.ResponseWriter, r *http.Request, id uint64) {
	i, _ := FromContext(r.Context())
	if err := h.service.RevokeOwnSessions(r.Context(), i, id, accountMeta(r)); err != nil {
		writeAccountError(w, r, err)
		return
	}
	if id == i.SessionID {
		h.auth.clearSessionCookies(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func accountUser(i SessionIdentity) map[string]any {
	return map[string]any{"id": strconv.FormatUint(i.UserID, 10), "email": i.Email, "display_name": i.DisplayName, "version": strconv.FormatUint(i.Version, 10), "credential_version": strconv.FormatUint(i.CredentialVersion, 10)}
}

func writeAccountError(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, ErrAccountResultUnknown):
		httpapi.WriteError(w, r, http.StatusInternalServerError, "ACCOUNT_RESULT_UNKNOWN", "Password change outcome is unconfirmed. Reload /me or sign in with the new password; do not retry automatically")
	case errors.Is(err, ErrAccountInvalid):
		httpapi.WriteError(w, r, http.StatusBadRequest, "ACCOUNT_INVALID", err.Error())
	case errors.Is(err, ErrAccountConflict):
		httpapi.WriteError(w, r, http.StatusConflict, "ACCOUNT_CONFLICT", "Account changed; reload its current state before deciding whether to retry")
	case errors.Is(err, ErrInvalidCredentials):
		httpapi.WriteError(w, r, http.StatusBadRequest, "CURRENT_PASSWORD_INVALID", "Current password is incorrect")
	case errors.Is(err, ErrUnauthenticated):
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Session not found")
	case errors.Is(err, ErrAuthBusy):
		w.Header().Set("Retry-After", "1")
		httpapi.WriteError(w, r, http.StatusServiceUnavailable, "AUTH_BUSY", "Authentication is temporarily busy")
	default:
		httpapi.WriteError(w, r, http.StatusInternalServerError, "ACCOUNT_FAILED", "Account operation failed")
	}
}
