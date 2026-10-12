package sla

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Service) authorizeAdmin(ctx context.Context, e database.Executor, a incidents.Actor) error {
	keys, err := identity.NewMySQLRepository(s.db).ListPermissionsInTx(ctx, e, a.TenantID, a.UserID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key == "admin.sla.manage" {
			return nil
		}
	}
	return incidents.ErrForbidden
}

type Handler struct {
	service     *Service
	permissions incidents.PermissionProvider
}

func NewHandler(s *Service, p incidents.PermissionProvider) *Handler { return &Handler{s, p} }
func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/sla", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/incidents", h.Instances)
		r.Get("/incidents/{id}/history", h.History)
		r.Group(func(r chi.Router) {
			r.Use(m.RequirePermission("admin.sla.manage"))
			r.Get("/definitions/{kind}", h.List)
			r.Get("/definitions/{kind}/{id}", h.Get)
			r.With(m.RequireCSRF).Post("/definitions/{kind}", h.Save)
			r.With(m.RequireCSRF).Put("/definitions/{kind}/{id}", h.Save)
		})
	})
}
func (h *Handler) actor(r *http.Request) (incidents.Actor, error) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		return incidents.Actor{}, incidents.ErrForbidden
	}
	keys, err := h.permissions.Permissions(r.Context(), session)
	a := incidents.Actor{TenantID: session.TenantID, UserID: session.UserID, Permissions: map[string]bool{}, RequestID: middleware.GetReqID(r.Context()), IPAddress: requestinfo.ClientIP(r)}
	for _, k := range keys {
		a.Permissions[k] = true
	}
	return a, err
}
func number(s string, zero bool) (uint64, error) {
	if zero && s == "" {
		return 0, nil
	}
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil || !zero && n == 0 {
		return 0, ErrInvalid
	}
	return n, nil
}
func page(r *http.Request) (uint64, int, error) {
	n, e := number(r.URL.Query().Get("before_id"), true)
	if e != nil {
		return 0, 0, ErrInvalid
	}
	limit := 25
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
	}
	if e != nil || limit < 1 || limit > 100 {
		return 0, 0, ErrInvalid
	}
	return n, limit, nil
}
func response(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err == nil {
		httpapi.WriteJSON(w, 200, value)
		return
	}
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	code, status, message := "SLA_FAILED", 500, "Nie udało się wykonać operacji SLA"
	switch {
	case errors.Is(err, ErrInvalid):
		code, status, message = "INVALID_REQUEST", 400, err.Error()
	case errors.Is(err, ErrConflict):
		code, status, message = "VERSION_CONFLICT", 409, err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, incidents.ErrNotFound):
		code, status, message = "NOT_FOUND", 404, "Nie znaleziono SLA lub incydentu"
	case errors.Is(err, incidents.ErrForbidden):
		code, status, message = "FORBIDDEN", 403, "Brak uprawnień do tej operacji"
	}
	httpapi.WriteError(w, r, status, code, message)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	a, e := h.actor(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	before, limit, e := page(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	v, e := h.service.ListDefinitions(r.Context(), a.TenantID, chi.URLParam(r, "kind"), before, limit)
	response(w, r, v, e)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	a, e := h.actor(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	id, e := number(chi.URLParam(r, "id"), false)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	version, e := number(r.URL.Query().Get("version"), true)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	d, e := h.service.Definition(r.Context(), h.service.db, a.TenantID, id, version)
	if e == nil && d.Kind != chi.URLParam(r, "kind") {
		e = ErrNotFound
	}
	response(w, r, d, e)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	a, e := h.actor(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	var body struct {
		Version uint64          `json:"version,string"`
		Config  json.RawMessage `json:"config"`
	}
	if e = httpapi.DecodeJSON(r, &body); e != nil {
		httpapi.WriteDecodeError(w, r, e)
		return
	}
	id, e := number(chi.URLParam(r, "id"), true)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	v, e := h.service.SaveDefinition(r.Context(), a, chi.URLParam(r, "kind"), id, body.Version, r.Header.Get("Idempotency-Key"), body.Config)
	response(w, r, v, e)
}

type InstancePage struct {
	Items   []Instance `json:"items"`
	Pending bool       `json:"pending"`
	Rules   string     `json:"rules"`
}

// Batch authorization filters unreadable incidents before fetching SLA state.
// A maximum of 100 IDs supports an entire list page with two SQL queries.
func (s *Service) Instances(ctx context.Context, a incidents.Actor, ids []uint64) (InstancePage, error) {
	result := InstancePage{Items: []Instance{}, Rules: Rules}
	if len(ids) < 1 || len(ids) > 100 {
		return result, ErrInvalid
	}
	if !a.Has("read") && !a.Has("read_own") {
		return result, incidents.ErrForbidden
	}
	args := []any{a.TenantID}
	marks := []string{}
	for _, id := range ids {
		if id == 0 {
			return result, ErrInvalid
		}
		marks = append(marks, "?")
		args = append(args, id)
	}
	scope := `tenant_id=? AND id IN (` + strings.Join(marks, ",") + `)`
	if !a.Has("read") {
		scope += ` AND (reporter_id=? OR affected_user_id=?)`
		args = append(args, a.UserID, a.UserID)
	}
	queryArgs := append([]any{s.now().UTC().Add(-30 * time.Second)}, args...)
	rows, err := s.db.QueryContext(ctx, `SELECT i.id,i.version,COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(w.checkpoint,'$.incident_version')) AS UNSIGNED),0),COALESCE(w.next_check_at<?,FALSE) FROM (SELECT id,tenant_id,version FROM incidents WHERE `+scope+`) i LEFT JOIN sla_work w ON w.tenant_id=i.tenant_id AND w.incident_id=i.id`, queryArgs...)
	if err != nil {
		return result, err
	}
	versions := map[uint64]uint64{}
	for rows.Next() {
		var id, v, projected uint64
		var overdue bool
		if err = rows.Scan(&id, &v, &projected, &overdue); err != nil {
			rows.Close()
			return result, err
		}
		versions[id] = v
		if projected < v || overdue {
			result.Pending = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(versions) == 0 {
		return result, ErrNotFound
	}
	marks = nil
	args = []any{a.TenantID}
	for id := range versions {
		marks = append(marks, "?")
		args = append(args, id)
	}
	rows, err = s.db.QueryContext(ctx, `SELECT i.state FROM sla_instances i WHERE i.tenant_id=? AND i.incident_id IN (`+strings.Join(marks, ",")+`) AND NOT EXISTS(SELECT 1 FROM sla_instances newer WHERE newer.tenant_id=i.tenant_id AND newer.incident_id=i.incident_id AND newer.kind=i.kind AND newer.cycle>i.cycle) ORDER BY i.incident_id,i.kind LIMIT 200`, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	// Only the current cycle of each metric is included in list/detail summaries.
	seen := map[string]bool{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return result, err
		}
		i := Instance{}
		if err = json.Unmarshal(raw, &i); err != nil {
			return result, err
		}
		key := fmt.Sprintf("%d:%s", i.IncidentID, i.Kind)
		if seen[key] {
			continue
		}
		seen[key] = true
		if i.IncidentVersion < versions[i.IncidentID] {
			result.Pending = true
		}
		result.Items = append(result.Items, i)
	}
	return result, rows.Err()
}
func (h *Handler) Instances(w http.ResponseWriter, r *http.Request) {
	a, e := h.actor(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	values := strings.Split(r.URL.Query().Get("ids"), ",")
	ids := []uint64{}
	for _, v := range values {
		id, e := number(v, false)
		if e != nil {
			response(w, r, nil, e)
			return
		}
		ids = append(ids, id)
	}
	p, e := h.service.Instances(r.Context(), a, ids)
	response(w, r, p, e)
}
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	a, e := h.actor(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	id, e := number(chi.URLParam(r, "id"), false)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	record, e := incidents.NewRepository(h.service.db).Get(r.Context(), h.service.db, a.TenantID, id, false)
	if e != nil || !incidents.CanRead(a, record) {
		response(w, r, nil, ErrNotFound)
		return
	}
	before, limit, e := page(r)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	q := `SELECT h.id,h.entry FROM sla_history h JOIN sla_instances i ON i.id=h.instance_id WHERE h.tenant_id=? AND i.incident_id=?`
	args := []any{a.TenantID, id}
	if before > 0 {
		q += ` AND h.id<?`
		args = append(args, before)
	}
	q += ` ORDER BY h.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, e := h.service.db.QueryContext(r.Context(), q, args...)
	if e != nil {
		response(w, r, nil, e)
		return
	}
	entries := []History{}
	for rows.Next() {
		var raw []byte
		var n uint64
		if e = rows.Scan(&n, &raw); e != nil {
			break
		}
		item := History{}
		if e = json.Unmarshal(raw, &item); e != nil {
			break
		}
		item.ID = n
		entries = append(entries, item)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	next := uint64(0)
	if len(entries) > limit {
		entries = entries[:limit]
		next = entries[limit-1].ID
	}
	response(w, r, struct {
		Items        []History `json:"items"`
		NextBeforeID uint64    `json:"next_before_id,string,omitempty"`
	}{entries, next}, e)
}
