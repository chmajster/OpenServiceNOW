package recordviews

import (
	"context"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
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
	Service     *Service
	Permissions PermissionProvider
}

func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/record-views", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Get("/", h.list)
		r.Get("/groups", h.groups)
		r.Get("/{id}", h.get)
		r.With(m.RequireCSRF).Post("/", h.save)
		r.With(m.RequireCSRF).Put("/{id}", h.save)
		r.With(m.RequireCSRF).Delete("/{id}", h.remove)
	})
}
func (h *Handler) actor(r *http.Request) (workrecords.Principal, error) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		return workrecords.Principal{}, workrecords.ErrForbidden
	}
	keys, err := h.Permissions.Permissions(r.Context(), session)
	a := workrecords.Principal{Actor: workrecords.Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context())}, IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
	for _, k := range keys {
		a.Permissions[k] = true
	}
	return a, err
}
func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err == nil {
		httpapi.WriteJSON(w, status, v)
		return
	}
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, 404, "VIEW_NOT_FOUND", err.Error())
	case errors.Is(err, workrecords.ErrForbidden):
		httpapi.WriteError(w, r, 403, "FORBIDDEN", "Brak uprawnień do tego widoku lub grupy.")
	case errors.Is(err, ErrConflict), errors.Is(err, ErrDuplicate), errors.Is(err, workrecords.ErrOperationConflict):
		httpapi.WriteError(w, r, 409, "VIEW_CONFLICT", err.Error())
	case errors.Is(err, recordlists.ErrInvalid), errors.Is(err, workrecords.ErrInvalid):
		httpapi.WriteError(w, r, 422, "VIEW_INVALID", "Sprawdź nazwę, filtry, kolumny i zakres widoku.")
	case errors.Is(err, database.ErrCommitUncertain):
		httpapi.WriteError(w, r, 503, "COMMIT_UNCERTAIN", "Nie potwierdzono zapisu. Ponów tworzenie z tym samym kluczem lub odczytaj aktualną wersję.")
	default:
		httpapi.WriteError(w, r, 500, "VIEW_FAILED", "Nie udało się wykonać operacji widoku.")
	}
}
func id(r *http.Request) (uint64, error) {
	v, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || v == 0 {
		return 0, recordlists.ErrInvalid
	}
	return v, nil
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	var before uint64
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		before, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || before == 0 {
			respond(w, r, 0, nil, recordlists.ErrInvalid)
			return
		}
	}
	items, next, err := h.Service.List(r.Context(), a, r.URL.Query().Get("domain"), before, 50)
	respond(w, r, 200, struct {
		Items []View `json:"items"`
		Next  uint64 `json:"next_before_id,string,omitempty"`
	}{items, next}, err)
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	key, err := id(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	v, err := h.Service.Get(r.Context(), a, key)
	respond(w, r, 200, v, err)
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	var key uint64
	if chi.URLParam(r, "id") != "" {
		key, err = id(r)
		if err != nil {
			respond(w, r, 0, nil, err)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var in Input
	if err = httpapi.DecodeJSON(r, &in); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	v, err := h.Service.Save(r.Context(), a, key, in, r.Header.Get("Idempotency-Key"))
	status := 200
	if key == 0 {
		status = 201
	}
	respond(w, r, status, v, err)
}
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	key, err := id(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	version, err := strconv.ParseUint(r.URL.Query().Get("version"), 10, 64)
	if err != nil || version == 0 {
		respond(w, r, 0, nil, recordlists.ErrInvalid)
		return
	}
	err = h.Service.Delete(r.Context(), a, key, version)
	respond(w, r, 200, map[string]bool{"deleted": err == nil}, err)
}
