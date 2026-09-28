package auditapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
)

type ReadService interface {
	List(context.Context, uint64, uint64, int) (audit.Page, error)
}

type Handler struct {
	reader ReadService
}

func NewHandler(reader ReadService) *Handler {
	return &Handler{reader: reader}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}

	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	beforeID, ok := parseBeforeID(w, r)
	if !ok {
		return
	}

	page, err := h.reader.List(r.Context(), session.TenantID, beforeID, limit)
	if err != nil {
		if httpapi.WriteContextError(w, r, err) {
			return
		}
		httpapi.WriteError(w, r, http.StatusInternalServerError, "AUDIT_READ_FAILED", "Unable to read audit events")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 50, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 200 {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_AUDIT_LIMIT", "Audit limit must be between 1 and 200")
		return 0, false
	}
	return value, true
}

func parseBeforeID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	raw := r.URL.Query().Get("before_id")
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_AUDIT_CURSOR", "Invalid audit cursor")
		return 0, false
	}
	return value, true
}
