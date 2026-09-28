package tenants

import (
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, handler *Handler, auth *identity.Middleware) {
	r.Route("/api/v1/admin/tenant", func(r chi.Router) {
		r.Use(auth.Authenticate)

		r.With(auth.RequirePermission("tenant.read")).Get("/", handler.Get)
		r.With(
			auth.RequirePermission("tenant.manage"),
			auth.RequireCSRF,
		).Put("/", handler.Update)
	})
}
