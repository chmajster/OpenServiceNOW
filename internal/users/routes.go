package users

import (
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, handler *Handler, auth *identity.Middleware) {
	r.Route("/api/v1/admin/users", func(r chi.Router) {
		r.Use(auth.Authenticate)
		r.Use(auth.RequirePermission("admin.users.manage"))

		r.Get("/", handler.List)
		r.Get("/{userID}", handler.Profile)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireCSRF)
			r.Post("/", handler.Create)
			r.Post("/memberships", handler.AddExistingMembership)
			r.Put("/{userID}/membership", handler.SetMembership)
		})
	})
}
