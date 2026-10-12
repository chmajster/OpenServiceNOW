package notifications

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
	events  *events.Repository
}

func NewHandler(service *Service, eventRepo *events.Repository) *Handler {
	return &Handler{service, eventRepo}
}
func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/notifications", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(noStore)
		r.Get("/", h.List)
		r.Get("/preferences", h.GetPreference)
		r.With(m.RequireCSRF).Put("/preferences", h.SavePreference)
		r.With(m.RequireCSRF).Post("/read-all", h.Read)
		r.With(m.RequireCSRF).Post("/{id}/read", h.Read)
		r.Get("/mail-history", h.OwnMail)
	})
	r.Route("/api/v1/admin/notifications", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(m.RequirePermission("admin.notifications.manage"))
		r.Use(noStore)
		r.Get("/smtp", h.GetConfig)
		r.With(m.RequireCSRF).Put("/smtp", h.SaveConfig)
		r.With(m.RequireCSRF).Post("/smtp/test", h.Test)
		r.Get("/deliveries", h.Deliveries)
		r.With(m.RequireCSRF).Post("/deliveries/{id}/retry", h.RetryEvent)
		r.Get("/mail", h.Mail)
		r.Get("/mail/{id}/attempts", h.Attempts)
		r.With(m.RequireCSRF).Post("/mail/{id}/retry", h.RetryMail)
	})
}
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func session(r *http.Request) identity.SessionIdentity {
	a, _ := identity.FromContext(r.Context())
	return a
}
func pagination(r *http.Request) (uint64, int, error) {
	before := uint64(0)
	limit := 25
	var err error
	if v := r.URL.Query().Get("before_id"); v != "" {
		before, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, 0, ErrInvalid
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, ErrInvalid
		}
	}
	return before, limit, nil
}
func objectID(r *http.Request) (uint64, error) {
	v, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || v == 0 {
		return 0, ErrInvalid
	}
	return v, nil
}
func respond(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	if status == 204 {
		w.WriteHeader(status)
		return
	}
	httpapi.WriteJSON(w, status, value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	status := 500
	code := "NOTIFICATION_FAILED"
	message := "Nie udało się wykonać operacji powiadomień"
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, events.ErrInvalid):
		status = 400
		code = "INVALID_REQUEST"
		message = ErrInvalid.Error()
	case errors.Is(err, ErrConflict), errors.Is(err, events.ErrConflict):
		status = 409
		code = "VERSION_CONFLICT"
		message = ErrConflict.Error()
	case errors.Is(err, ErrNotFound):
		status = 404
		code = "NOT_FOUND"
		message = ErrNotFound.Error()
	case errors.Is(err, ErrEncryption):
		status = 503
		code = "SMTP_KEY_UNAVAILABLE"
		message = ErrEncryption.Error()
	}
	httpapi.WriteError(w, r, status, code, message)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a := session(r)
	p, err := h.service.List(r.Context(), a.TenantID, a.UserID, before, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) GetPreference(w http.ResponseWriter, r *http.Request) {
	a := session(r)
	p, err := h.service.Preference(r.Context(), a.TenantID, a.UserID)
	respond(w, r, 200, p, err)
}
func (h *Handler) SavePreference(w http.ResponseWriter, r *http.Request) {
	var p Preference
	if err := httpapi.DecodeJSON(r, &p); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	a := session(r)
	respond(w, r, 204, nil, h.service.SavePreference(r.Context(), a.TenantID, a.UserID, p))
}
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	id := uint64(0)
	var err error
	if chi.URLParam(r, "id") != "" {
		id, err = objectID(r)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	a := session(r)
	respond(w, r, 204, nil, h.service.MarkRead(r.Context(), a.TenantID, a.UserID, id))
}
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Config(r.Context(), session(r).TenantID)
	respond(w, r, 200, c, err)
}
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var input ConfigInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	a := session(r)
	respond(w, r, 204, nil, h.service.SaveConfig(r.Context(), a.TenantID, a.UserID, input))
}
func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	a := session(r)
	id, err := h.service.TestSMTP(r.Context(), a.TenantID, a.UserID, r.Header.Get("Idempotency-Key"))
	respond(w, r, 202, map[string]string{"delivery_id": strconv.FormatUint(id, 10)}, err)
}
func (h *Handler) Deliveries(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := h.events.ListDeliveries(r.Context(), session(r).TenantID, before, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) RetryEvent(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		Attempts uint32 `json:"attempt_count"`
	}
	if err = httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	respond(w, r, 204, nil, h.events.RetryDelivery(r.Context(), session(r).TenantID, id, input.Attempts))
}
func (h *Handler) Mail(w http.ResponseWriter, r *http.Request)    { h.mail(w, r, 0) }
func (h *Handler) OwnMail(w http.ResponseWriter, r *http.Request) { h.mail(w, r, session(r).UserID) }
func (h *Handler) mail(w http.ResponseWriter, r *http.Request, user uint64) {
	before, limit, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := h.service.MailList(r.Context(), session(r).TenantID, user, before, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) Attempts(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a := session(r)
	p, err := h.service.Attempts(r.Context(), a.TenantID, a.UserID, id)
	respond(w, r, 200, map[string]any{"attempts": p}, err)
}
func (h *Handler) RetryMail(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		Version uint64 `json:"version,string"`
	}
	if err = httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	a := session(r)
	respond(w, r, 204, nil, h.service.RetryMail(r.Context(), a.TenantID, a.UserID, id, input.Version))
}
