package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"strconv"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	service     *Service
	permissions PermissionProvider
}

func NewHandler(s *Service, p PermissionProvider) *Handler { return &Handler{s, p} }
func (h *Handler) actor(r *http.Request) (Actor, error) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		return Actor{}, workrecords.ErrForbidden
	}
	keys, err := h.permissions.Permissions(r.Context(), session)
	a := Actor{Actor: workrecords.Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context())}, IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
	for _, k := range keys {
		a.Permissions[k] = true
	}
	return a, err
}
func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/workflow", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/definitions", h.Definitions)
		r.Get("/definitions/{id}", h.Definition)
		r.With(m.RequireCSRF).Post("/definitions", h.Save)
		r.With(m.RequireCSRF).Put("/definitions/{id}", h.Save)
		r.With(m.RequireCSRF).Post("/definitions/{id}/publish", h.Publish)
		r.Get("/versions/{id}/{version}", h.Version)
		r.Get("/lookups/{kind:users|groups}", h.Lookups)
		r.Get("/executions", h.Executions)
		r.Get("/approvals", h.Executions)
		r.Get("/executions/{id}", h.Execution)
		r.With(m.RequireCSRF).Post("/executions/{id}/decisions/{node}", h.Decide)
		r.With(m.RequireCSRF).Post("/executions/{id}/repair", h.Repair)
	})
}
func fail(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	status, code, message := 500, "WORKFLOW_FAILED", "Nie udało się wykonać operacji procesu."
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, workrecords.ErrInvalid):
		status, code, message = 400, "INVALID_WORKFLOW", err.Error()
	case errors.Is(err, ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Nie znaleziono dostępnego procesu."
	case errors.Is(err, workrecords.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Brak uprawnień do operacji procesu."
	case errors.Is(err, ErrConflict), errors.Is(err, workrecords.ErrOperationConflict):
		status, code, message = 409, "VERSION_CONFLICT", err.Error()
	case errors.Is(err, database.ErrCommitUncertain):
		status, code, message = 503, "COMMIT_UNCERTAIN", "Nie potwierdzono zapisu. Ponów tę samą operację z tym samym kluczem."
	}
	httpapi.WriteError(w, r, status, code, message)
}
func respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		fail(w, r, err)
	} else {
		httpapi.WriteJSON(w, 200, v)
	}
}
func number(raw string, optional bool) (uint64, error) {
	if optional && raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		return 0, ErrInvalid
	}
	return v, nil
}
func object(r *http.Request) (uint64, error) { return number(chi.URLParam(r, "id"), false) }
func paging(r *http.Request, key string) (uint64, int, error) {
	cursor, err := number(r.URL.Query().Get(key), true)
	if err != nil {
		return 0, 0, err
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, ErrInvalid
	}
	return cursor, limit, nil
}
func (h *Handler) Definition(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	id, err := object(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.service.Definition(r.Context(), a, id)
	respond(w, r, v, err)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	id, err := number(chi.URLParam(r, "id"), true)
	if err != nil {
		fail(w, r, err)
		return
	}
	var d Definition
	if err = httpapi.DecodeJSON(r, &d); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	v, err := h.service.Save(r.Context(), a, id, d, r.Header.Get("Idempotency-Key"))
	respond(w, r, v, err)
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	id, err := object(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in struct {
		Version uint64 `json:"version,string"`
	}
	if err = httpapi.DecodeJSON(r, &in); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	v, err := h.service.Publish(r.Context(), a, id, in.Version)
	respond(w, r, v, err)
}
func canConfigure(a Actor) bool {
	return a.Has("workflow.author") || a.Has("workflow.publish") || a.Has("admin.catalog.manage")
}
func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !canConfigure(a) {
		fail(w, r, workrecords.ErrForbidden)
		return
	}
	id, err := object(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	version, err := number(chi.URLParam(r, "version"), false)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := Published(r.Context(), h.service.DB, a.TenantID, id, version)
	respond(w, r, v, err)
}
func (h *Handler) Definitions(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !canConfigure(a) {
		fail(w, r, workrecords.ErrForbidden)
		return
	}
	after, limit, err := paging(r, "after_id")
	if err != nil {
		fail(w, r, err)
		return
	}
	q := r.URL.Query().Get("q")
	if !validText(q, 0, 200) {
		fail(w, r, ErrInvalid)
		return
	}
	domain := r.URL.Query().Get("domain")
	if domain != "" && domain != "REQUEST" && domain != "CHANGE" {
		fail(w, r, ErrInvalid)
		return
	}
	query := "SELECT d.id,d.version,d.published_version,d.name FROM workflow_definitions d WHERE d.tenant_id=? AND d.id>? AND (?='' OR LOCATE(?,d.name)>0)"
	args := []any{a.TenantID, after, q, q}
	if domain != "" {
		query += " AND EXISTS(SELECT 1 FROM workflow_versions v WHERE v.tenant_id=d.tenant_id AND v.definition_id=d.id AND v.version=d.published_version AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.domain')),'REQUEST')=?)"
		args = append(args, domain)
	}
	query += " ORDER BY d.id LIMIT ?"
	args = append(args, limit+1)
	rows, err := h.service.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []Definition{}
	for rows.Next() {
		var d Definition
		var pub sql.Null[uint64]
		if err = rows.Scan(&d.ID, &d.Version, &pub, &d.Draft.Name); err != nil {
			fail(w, r, err)
			return
		}
		if pub.Valid {
			d.PublishedVersion = fmt.Sprint(pub.V)
		}
		items = append(items, d)
	}
	if err = rows.Err(); err != nil {
		fail(w, r, err)
		return
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID
	}
	respond(w, r, struct {
		Items []Definition `json:"items"`
		Next  uint64       `json:"next_after_id,string,omitempty"`
	}{items, next}, nil)
}
func (h *Handler) Executions(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	before, limit, err := paging(r, "before_id")
	if err != nil {
		fail(w, r, err)
		return
	}
	request, err := number(r.URL.Query().Get("request_id"), true)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.service.List(r.Context(), a, r.URL.Path == "/api/v1/workflow/approvals", request, before, r.URL.Query().Get("state"), limit)
	respond(w, r, v, err)
}
func (h *Handler) Execution(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	id, err := object(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.service.Get(r.Context(), a, id)
	respond(w, r, v, err)
}
func (h *Handler) Decide(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	id, err := object(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in DecisionInput
	if err = httpapi.DecodeJSON(r, &in); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	v, err := h.service.Decide(r.Context(), a, id, chi.URLParam(r, "node"), in)
	respond(w, r, v, err)
}
func (h *Handler) Repair(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	id, err := object(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in struct {
		Version uint64 `json:"version,string"`
		Action  string `json:"action"`
		Reason  string `json:"reason"`
	}
	if err = httpapi.DecodeJSON(r, &in); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	v, err := h.service.Repair(r.Context(), a, id, in.Version, in.Action, in.Reason)
	respond(w, r, v, err)
}
func (h *Handler) Lookups(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !canConfigure(a) {
		fail(w, r, workrecords.ErrForbidden)
		return
	}
	after, limit, err := paging(r, "after_id")
	if err != nil {
		fail(w, r, err)
		return
	}
	q := r.URL.Query().Get("q")
	if !validText(q, 0, 200) {
		fail(w, r, ErrInvalid)
		return
	}
	query := "SELECT u.id,u.display_name FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id WHERE tm.tenant_id=? AND tm.active=TRUE AND u.active=TRUE AND u.id>? AND (?='' OR LOCATE(?,u.display_name)>0) ORDER BY u.id LIMIT ?"
	if chi.URLParam(r, "kind") == "groups" {
		query = "SELECT id,name FROM `groups` WHERE tenant_id=? AND active=TRUE AND id>? AND (?='' OR LOCATE(?,name)>0) ORDER BY id LIMIT ?"
	}
	rows, err := h.service.DB.QueryContext(r.Context(), query, a.TenantID, after, q, q, limit+1)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer rows.Close()
	type choice struct {
		ID    uint64 `json:"id,string"`
		Label string `json:"name"`
	}
	items := []choice{}
	for rows.Next() {
		var c choice
		if err = rows.Scan(&c.ID, &c.Label); err != nil {
			fail(w, r, err)
			return
		}
		items = append(items, c)
	}
	if err = rows.Err(); err != nil {
		fail(w, r, err)
		return
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID
	}
	page := map[string]any{chi.URLParam(r, "kind"): items}
	if next > 0 {
		page["next_after_id"] = fmt.Sprint(next)
	}
	respond(w, r, page, nil)
}
