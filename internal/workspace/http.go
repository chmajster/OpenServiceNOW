package workspace

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	Service     *Service
	Permissions PermissionProvider
}

func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/workspace", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Get("/summary", h.Summary)
		r.Get("/search", h.Search)
		r.Get("/metadata", h.Metadata)
		r.Get("/directory/{kind:users|groups}/{recordID}", h.DirectoryReference)
		r.Get("/reports", h.Reports)
		r.Get("/report-records", h.ReportSources)
		r.Get("/context/{root}/{recordID}", h.Context)
	})
}
func (h *Handler) actor(r *http.Request) (workrecords.Principal, error) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		return workrecords.Principal{}, workrecords.ErrForbidden
	}
	keys, err := h.Permissions.Permissions(r.Context(), session)
	if err != nil {
		return workrecords.Principal{}, err
	}
	a := workrecords.Principal{Actor: workrecords.Actor{TenantID: session.TenantID, UserID: session.UserID}, Permissions: map[string]bool{}}
	for _, k := range keys {
		a.Permissions[k] = true
	}
	return a, nil
}
func respond(w http.ResponseWriter, r *http.Request, value any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err == nil {
		httpapi.WriteJSON(w, 200, value)
		return
	}
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	if errors.Is(err, ErrInvalid) {
		httpapi.WriteError(w, r, 400, "WORKSPACE_INVALID", err.Error())
		return
	}
	if errors.Is(err, workrecords.ErrNotFound) {
		httpapi.WriteError(w, r, 404, "NOT_FOUND", "Nie znaleziono dostępnego rekordu.")
		return
	}
	if errors.Is(err, workrecords.ErrForbidden) {
		httpapi.WriteError(w, r, 403, "FORBIDDEN", "Brak dostępu do workspace.")
		return
	}
	httpapi.WriteError(w, r, 500, "WORKSPACE_FAILED", "Nie udało się odczytać danych workspace. Spróbuj ponownie.")
}
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	v, err := h.Service.Summary(ctx, a)
	respond(w, r, v, err)
}
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	v, err := h.Service.Search(ctx, a, r.URL.Query().Get("q"))
	respond(w, r, map[string]any{"items": v, "limit_per_type": 10}, err)
}

func (h *Handler) Metadata(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	v, err := h.Service.Metadata(ctx, a, r.URL.Query()["key"])
	respond(w, r, v, err)
}

func (h *Handler) Context(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "recordID"), 10, 64)
	if err != nil || id == 0 {
		respond(w, r, nil, ErrInvalid)
		return
	}
	v, err := h.Service.Context(ctx, a, chi.URLParam(r, "root"), id)
	respond(w, r, v, err)
}

func reportPeriod(r *http.Request) (ReportPeriod, error) {
	from, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("from"))
	if err != nil {
		return ReportPeriod{}, ErrInvalid
	}
	to, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("to"))
	if err != nil {
		return ReportPeriod{}, ErrInvalid
	}
	p := ReportPeriod{From: from.UTC(), To: to.UTC()}
	return p, ValidatePeriod(p)
}
func (h *Handler) Reports(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	p, err := reportPeriod(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	v, err := h.Service.Reports(ctx, a, p)
	respond(w, r, v, err)
}
func (h *Handler) ReportSources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	p, err := reportPeriod(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	q := r.URL.Query()
	f := ReportSourceFilter{Report: q.Get("report"), Domain: q.Get("domain"), Bucket: q.Get("bucket"), Period: p, Limit: 25}
	for key, target := range map[string]*uint64{"problem_id": &f.ProblemID, "before_id": &f.Before} {
		if value := q.Get(key); value != "" {
			*target, err = strconv.ParseUint(value, 10, 64)
			if err != nil || *target == 0 {
				respond(w, r, nil, ErrInvalid)
				return
			}
		}
	}
	if value := q.Get("as_of"); value != "" {
		f.AsOf, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			respond(w, r, nil, ErrInvalid)
			return
		}
	}
	if value := q.Get("limit"); value != "" {
		f.Limit, err = strconv.Atoi(value)
		if err != nil {
			respond(w, r, nil, ErrInvalid)
			return
		}
	}
	v, err := h.Service.ReportSources(ctx, a, f)
	respond(w, r, v, err)
}

func (h *Handler) DirectoryReference(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	id, err := workrecords.ChoiceID(chi.URLParam(r, "recordID"))
	if err != nil || id == 0 {
		respond(w, r, nil, ErrInvalid)
		return
	}
	v, err := h.Service.DirectoryReference(ctx, a, chi.URLParam(r, "kind"), id)
	respond(w, r, v, err)
}
