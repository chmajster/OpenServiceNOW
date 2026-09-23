package auditapi

import (
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, handler *Handler, auth *identity.Middleware) {
	r.Route("/api/v1/admin/audit", func(r chi.Router) {
		r.Use(auth.Authenticate)
		r.Use(auth.RequirePermission("admin.audit.read"))
		r.Get("/", handler.List)
	})
}
