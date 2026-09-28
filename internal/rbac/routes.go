package rbac

import (
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, handler *Handler, auth *identity.Middleware) {
	r.Route("/api/v1/admin/rbac", func(r chi.Router) {
		r.Use(auth.Authenticate)
		r.Use(auth.RequirePermission("admin.rbac.manage"))

		r.Get("/permissions", handler.ListPermissions)
		r.Get("/roles", handler.ListRoles)
		r.Get("/groups", handler.ListGroups)
		r.With(auth.RequirePermission("admin.users.manage")).Get("/groups/{groupID}/members", handler.ListGroupMembers)
		r.With(auth.RequirePermission("admin.users.manage")).Get("/users/{userID}/roles", handler.ListUserRoles)
		r.Get("/groups/{groupID}/roles", handler.ListGroupRoles)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireCSRF)

			r.Post("/roles", handler.CreateRole)
			r.Put("/roles/{roleID}/permissions", handler.SetRolePermissions)

			r.Post("/groups", handler.CreateGroup)
			r.With(auth.RequirePermission("admin.users.manage")).Post("/groups/{groupID}/members/{userID}", handler.AddGroupMember)
			r.With(auth.RequirePermission("admin.users.manage")).Delete("/groups/{groupID}/members/{userID}", handler.RemoveGroupMember)

			r.With(auth.RequirePermission("admin.users.manage")).Post("/users/{userID}/roles/{roleID}", handler.AssignRoleToUser)
			r.With(auth.RequirePermission("admin.users.manage")).Delete("/users/{userID}/roles/{roleID}", handler.RemoveRoleFromUser)

			r.Post("/groups/{groupID}/roles/{roleID}", handler.AssignRoleToGroup)
			r.Delete("/groups/{groupID}/roles/{roleID}", handler.RemoveRoleFromGroup)
		})
	})
}
