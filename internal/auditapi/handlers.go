package auditapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
)

type ReadService interface {
	ListFiltered(context.Context, uint64, uint64, int, audit.Filter) (audit.Page, error)
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

	filter, ok := parseFilter(w, r)
	if !ok {
		return
	}
	page, err := h.reader.ListFiltered(r.Context(), session.TenantID, beforeID, limit, filter)
	if err != nil {
		if httpapi.WriteContextError(w, r, err) {
			return
		}
		httpapi.WriteError(w, r, http.StatusInternalServerError, "AUDIT_READ_FAILED", "Unable to read audit events")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func parseFilter(w http.ResponseWriter, r *http.Request) (audit.Filter, bool) {
	var filter audit.Filter
	query := r.URL.Query()
	invalid := func() (audit.Filter, bool) {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_AUDIT_FILTER", "Invalid audit filter: use RFC3339 dates, a positive decimal actor ID and bounded text")
		return audit.Filter{}, false
	}
	for key, target := range map[string]*time.Time{"from": &filter.From, "to": &filter.To} {
		if raw := query.Get(key); raw != "" {
			value, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return invalid()
			}
			*target = value.UTC()
		}
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		return invalid()
	}
	if raw := query.Get("actor_user_id"); raw != "" {
		if strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return invalid()
		}
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			return invalid()
		}
		filter.ActorUserID = value
	}
	for _, field := range []struct {
		key    string
		max    int
		target *string
	}{
		{"action", 160, &filter.Action}, {"object_type", 100, &filter.ObjectType},
		{"object_id", 128, &filter.ObjectID}, {"request_id", 128, &filter.RequestID},
	} {
		value := strings.TrimSpace(query.Get(field.key))
		if utf8.RuneCountInString(value) > field.max || strings.ContainsAny(value, "\x00\r\n") {
			return invalid()
		}
		*field.target = value
	}
	return filter, true
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
