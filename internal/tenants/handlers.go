package tenants

import (
	"errors"
	"net/http"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/go-chi/chi/v5/middleware"
)

type Handler struct {
	service APIService
}

func NewHandler(service APIService) *Handler {
	return &Handler{service: service}
}

type updateRequest struct {
	Name    string `json:"name"`
	Version uint64 `json:"version"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	tenant, err := h.service.Get(r.Context(), session.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"tenant": tenant})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var request updateRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON request")
		return
	}
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	updated, err := h.service.UpdateName(r.Context(), Actor{
		TenantID:  session.TenantID,
		UserID:    session.UserID,
		RequestID: middleware.GetReqID(r.Context()),
		IPAddress: requestinfo.DirectPeerIP(r),
	}, request.Version, request.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"tenant": updated})
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_TENANT_INPUT", "Invalid tenant input")
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
	case errors.Is(err, ErrConflict):
		httpapi.WriteError(w, r, http.StatusConflict, "TENANT_VERSION_CONFLICT", "Tenant was modified by another operation")
	default:
		httpapi.WriteError(w, r, http.StatusInternalServerError, "TENANT_OPERATION_FAILED", "Tenant operation failed")
	}
}
