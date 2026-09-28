package identity

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, handler *Handler, middleware *Middleware) {
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/login", handler.Login)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate)
			r.Get("/me", handler.Me)
			r.With(middleware.RequireCSRF).Post("/logout", handler.Logout)
		})
	})
}
