package workspace

import (
	"context"
	"errors"
	"net/http"
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
