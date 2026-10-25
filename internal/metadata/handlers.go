package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	service     *Service
	permissions PermissionProvider
}

func NewHandler(service *Service, permissions PermissionProvider) *Handler {
	return &Handler{service: service, permissions: permissions}
}

type actorKey struct{}

func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/metadata", func(r chi.Router) {
		r.Use(m.Authenticate, h.context)
		r.Get("/schema/{type}", h.schema)
		r.Get("/references/{type}", h.references)
		r.Get("/configuration", h.configuration)
		r.Get("/configuration/impact", h.impact)
		r.With(m.RequireCSRF).Put("/configuration/draft", h.saveDraft)
		r.With(m.RequireCSRF).Post("/configuration/publish", h.publish)
		r.With(m.RequireCSRF).Post("/configuration/test", h.testRule)
		r.Get("/configuration/versions", h.versions)
		r.Get("/configuration/versions/{version}", h.version)
		r.Get("/configuration/versions/{from}/compare/{to}", h.compare)
		r.With(m.RequireCSRF).Post("/configuration/versions/{version}/restore", h.restore)
		r.Get("/preferences", h.preferences)
		r.With(m.RequireCSRF).Put("/preferences", h.setPreferences)
	})
}

func (h *Handler) context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, ok := identity.FromContext(r.Context())
		if !ok {
			writeError(w, r, workrecords.ErrForbidden)
			return
		}
		keys, err := h.permissions.Permissions(r.Context(), session)
		if err != nil {
			writeError(w, r, err)
			return
		}
		a := Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context()), IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
		for _, key := range keys {
			a.Permissions[key] = true
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}
func actor(r *http.Request) Actor { a, _ := r.Context().Value(actorKey{}).(Actor); return a }

func (h *Handler) schema(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	typeKey := chi.URLParam(r, "type")
	switch typeKey {
	case "incident":
		if !a.Has("incident.read") && !a.Has("incident.read_own") && !a.Has("incident.create") {
			writeError(w, r, workrecords.ErrForbidden)
			return
		}
	case "ci":
		if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
			writeError(w, r, workrecords.ErrForbidden)
			return
		}
	default:
		writeError(w, r, ErrNotFound)
		return
	}
	result, err := h.service.Schema(r.Context(), a.TenantID, typeKey)
	writeResult(w, r, 200, result, err)
}

func (h *Handler) references(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pageArgs(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.service.References(r.Context(), actor(r), chi.URLParam(r, "type"), r.URL.Query().Get("q"), after, limit)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) configuration(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has(ManagePermission) {
		writeError(w, r, workrecords.ErrForbidden)
		return
	}
	result, err := h.service.Head(r.Context(), a)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) impact(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has(ManagePermission) {
		writeError(w, r, workrecords.ErrForbidden)
		return
	}
	result, err := h.service.Impact(r.Context(), a)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) saveDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version       uint64        `json:"version,string"`
		Configuration Configuration `json:"configuration"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.SaveDraft(r.Context(), actor(r), body.Version, body.Configuration)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version uint64 `json:"version,string"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, impact, err := h.service.Publish(r.Context(), actor(r), body.Version)
	if err != nil {
		if errors.As(err, new(ImpactError)) {
			var impactErr ImpactError
			errors.As(err, &impactErr)
			httpapi.WriteJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"code": "CONFIGURATION_IMPACT", "message": err.Error(), "details": impactErr.Impact}})
			return
		}
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, 200, map[string]any{"head": result, "impact": impact})
}
func (h *Handler) testRule(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has(ManagePermission) {
		writeError(w, r, workrecords.ErrForbidden)
		return
	}
	var body struct {
		Type          string                     `json:"type"`
		Configuration Configuration              `json:"configuration"`
		Expression    Expression                 `json:"expression"`
		Values        map[string]json.RawMessage `json:"values"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := TestRule(body.Configuration, body.Type, body.Expression, body.Values)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) versions(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has(ManagePermission) {
		writeError(w, r, workrecords.ErrForbidden)
		return
	}
	before, limit, err := pageArgs(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, next, err := h.service.Versions(r.Context(), a, before, limit)
	writeResult(w, r, 200, map[string]any{"items": items, "next_before_id": next}, err)
}
func (h *Handler) version(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has(ManagePermission) {
		writeError(w, r, workrecords.ErrForbidden)
		return
	}
	version, err := parseID(chi.URLParam(r, "version"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.service.Version(r.Context(), a, version)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) compare(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has(ManagePermission) {
		writeError(w, r, workrecords.ErrForbidden)
		return
	}
	from, err := parseID(chi.URLParam(r, "from"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	to, err := parseID(chi.URLParam(r, "to"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	left, err := h.service.Version(r.Context(), a, from)
	if err != nil {
		writeError(w, r, err)
		return
	}
	right, err := h.service.Version(r.Context(), a, to)
	if err != nil {
		writeError(w, r, err)
		return
	}
	paths := changedPaths(left.Config, right.Config)
	writeResult(w, r, 200, map[string]any{"from": left, "to": right, "changed_paths": paths}, nil)
}
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version uint64 `json:"version,string"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	source, err := parseID(chi.URLParam(r, "version"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.service.Restore(r.Context(), actor(r), body.Version, source)
	writeResult(w, r, 200, result, err)
}
func (h *Handler) preferences(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetPreferences(r.Context(), actor(r))
	writeResult(w, r, 200, result, err)
}
func (h *Handler) setPreferences(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version  uint64 `json:"version,string"`
		Locale   string `json:"locale"`
		TimeZone string `json:"time_zone"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.SetPreferences(r.Context(), actor(r), body.Version, body.Locale, body.TimeZone)
	writeResult(w, r, 200, result, err)
}

func changedPaths(a, b Configuration) []string {
	paths := []string{}
	leftProcess, _ := json.Marshal(a.IncidentProcess)
	rightProcess, _ := json.Marshal(b.IncidentProcess)
	if string(leftProcess) != string(rightProcess) {
		paths = append(paths, "incident_process")
	}
	if a.Organization != b.Organization {
		paths = append(paths, "organization")
	}
	if a.PriorityMatrix != b.PriorityMatrix {
		paths = append(paths, "priority_matrix")
	}
	keys := map[string]bool{}
	for key := range a.Dictionaries {
		keys[key] = true
	}
	for key := range b.Dictionaries {
		keys[key] = true
	}
	for key := range keys {
		left, _ := json.Marshal(a.Dictionaries[key])
		right, _ := json.Marshal(b.Dictionaries[key])
		if string(left) != string(right) {
			paths = append(paths, "dictionaries."+key)
		}
	}
	keys = map[string]bool{}
	for key := range a.RecordTypes {
		keys[key] = true
	}
	for key := range b.RecordTypes {
		keys[key] = true
	}
	for key := range keys {
		left, _ := json.Marshal(a.RecordTypes[key])
		right, _ := json.Marshal(b.RecordTypes[key])
		if string(left) != string(right) {
			paths = append(paths, "record_types."+key)
		}
	}
	sort.Strings(paths)
	return paths
}
func pageArgs(r *http.Request) (uint64, int, error) {
	before := uint64(0)
	limit := 25
	raw := r.URL.Query().Get("before_id")
	if raw == "" {
		raw = r.URL.Query().Get("before_version_id")
	}
	if raw != "" {
		parsed, err := parseID(raw)
		if err != nil {
			return 0, 0, err
		}
		before = parsed
	}
	if raw = r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, 0, ErrInvalid
		}
		limit = parsed
	}
	return before, limit, nil
}
func parseID(raw string) (uint64, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
		return 0, ErrInvalid
	}
	return id, nil
}
func writeResult(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, status, value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	status, code, message := 500, "METADATA_FAILED", "Nie udało się wykonać operacji konfiguracji"
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, workrecords.ErrInvalid):
		status, code, message = 400, "INVALID_REQUEST", err.Error()
	case errors.Is(err, ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Nie znaleziono konfiguracji lub rekordu"
	case errors.Is(err, workrecords.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Brak uprawnień do tej operacji"
	case errors.Is(err, ErrConflict):
		status, code, message = 409, "VERSION_CONFLICT", err.Error()
	}
	var fieldError FieldError
	if errors.As(err, &fieldError) {
		httpapi.WriteJSON(w, status, map[string]any{"error": map[string]any{"code": "FIELD_VALIDATION", "message": fieldError.Error(), "field": fieldError.Field, "reason_code": fieldError.Code, "request_id": middleware.GetReqID(r.Context())}})
		return
	}
	httpapi.WriteError(w, r, status, code, message)
}
