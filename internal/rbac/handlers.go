package rbac

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Handler struct {
	service APIService
}

func NewHandler(service APIService) *Handler {
	return &Handler{service: service}
}

type createRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type setRolePermissionsRequest struct {
	Version     uint64   `json:"version,string"`
	Permissions []string `json:"permissions"`
}

type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	permissions, err := h.service.ListPermissions(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"permissions": permissions})
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	afterID, limit, query, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListRoles(r.Context(), session.TenantID, afterID, limit, query)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var request createRoleRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	roleID, err := h.service.CreateRole(r.Context(), actor, CreateRoleInput{
		Name: request.Name, Description: request.Description, Permissions: request.Permissions,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": strconv.FormatUint(roleID, 10)})
}

func (h *Handler) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	roleID, ok := pathID(w, r, "roleID")
	if !ok {
		return
	}
	var request setRolePermissionsRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if request.Version == 0 {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_RBAC_INPUT", "Role version is required")
		return
	}
	if err := h.service.SetRolePermissions(
		r.Context(),
		actor,
		roleID,
		request.Version,
		request.Permissions,
	); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	afterID, limit, query, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListGroups(r.Context(), session.TenantID, afterID, limit, query)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(w, r, "groupID")
	if !ok {
		return
	}
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	afterID, limit, ok := parseAssignmentListQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListGroupMembers(r.Context(), session.TenantID, groupID, afterID, limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "userID")
	if !ok {
		return
	}
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	afterID, limit, ok := parseAssignmentListQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListUserRoles(r.Context(), session.TenantID, userID, afterID, limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) ListGroupRoles(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(w, r, "groupID")
	if !ok {
		return
	}
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	afterID, limit, ok := parseAssignmentListQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListGroupRoles(r.Context(), session.TenantID, groupID, afterID, limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var request createGroupRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	groupID, err := h.service.CreateGroup(r.Context(), actor, CreateGroupInput{
		Name: request.Name, Description: request.Description,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": strconv.FormatUint(groupID, 10)})
}

func (h *Handler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	h.groupMemberAction(w, r, h.service.AddGroupMember)
}

func (h *Handler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	h.groupMemberAction(w, r, h.service.RemoveGroupMember)
}

func (h *Handler) AssignRoleToUser(w http.ResponseWriter, r *http.Request) {
	h.userRoleAction(w, r, h.service.AssignRoleToUser)
}

func (h *Handler) RemoveRoleFromUser(w http.ResponseWriter, r *http.Request) {
	h.userRoleAction(w, r, h.service.RemoveRoleFromUser)
}

func (h *Handler) AssignRoleToGroup(w http.ResponseWriter, r *http.Request) {
	h.groupRoleAction(w, r, h.service.AssignRoleToGroup)
}

func (h *Handler) RemoveRoleFromGroup(w http.ResponseWriter, r *http.Request) {
	h.groupRoleAction(w, r, h.service.RemoveRoleFromGroup)
}

func (h *Handler) groupMemberAction(
	w http.ResponseWriter,
	r *http.Request,
	action func(context.Context, Actor, uint64, uint64) error,
) {
	groupID, ok := pathID(w, r, "groupID")
	if !ok {
		return
	}
	userID, ok := pathID(w, r, "userID")
	if !ok {
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if err := action(r.Context(), actor, groupID, userID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) userRoleAction(
	w http.ResponseWriter,
	r *http.Request,
	action func(context.Context, Actor, uint64, uint64) error,
) {
	userID, ok := pathID(w, r, "userID")
	if !ok {
		return
	}
	roleID, ok := pathID(w, r, "roleID")
	if !ok {
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if err := action(r.Context(), actor, roleID, userID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) groupRoleAction(
	w http.ResponseWriter,
	r *http.Request,
	action func(context.Context, Actor, uint64, uint64) error,
) {
	groupID, ok := pathID(w, r, "groupID")
	if !ok {
		return
	}
	roleID, ok := pathID(w, r, "roleID")
	if !ok {
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if err := action(r.Context(), actor, groupID, roleID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func actorFromRequest(r *http.Request) (Actor, bool) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		return Actor{}, false
	}
	return Actor{
		TenantID:  session.TenantID,
		UserID:    session.UserID,
		RequestID: middleware.GetReqID(r.Context()),
		IPAddress: requestinfo.ClientIP(r),
	}, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	raw := chi.URLParam(r, name)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_RESOURCE_ID", "Invalid resource identifier")
		return 0, false
	}
	return id, true
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, identity.ErrLastPlatformAdmin):
		httpapi.WriteError(
			w,
			r,
			http.StatusConflict,
			"LAST_PLATFORM_ADMIN",
			"Tenant must retain at least one active platform administrator",
		)
	case errors.Is(err, identity.ErrPlatformAdminRequired):
		httpapi.WriteError(
			w,
			r,
			http.StatusForbidden,
			"PLATFORM_ADMIN_REQUIRED",
			"Platform administrator authority is required",
		)
	case errors.Is(err, ErrInvalid):
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_RBAC_INPUT", "Invalid RBAC input")
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "RBAC_RESOURCE_NOT_FOUND", "RBAC resource not found")
	case errors.Is(err, ErrConflict):
		httpapi.WriteError(w, r, http.StatusConflict, "RBAC_CONFLICT", "RBAC resource already exists")
	default:
		httpapi.WriteError(w, r, http.StatusInternalServerError, "RBAC_OPERATION_FAILED", "RBAC operation failed")
	}
}

func parseListQuery(w http.ResponseWriter, r *http.Request) (uint64, int, string, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGINATION", "limit must be between 1 and 200")
			return 0, 0, "", false
		}
		limit = value
	}

	var afterID uint64
	if raw := r.URL.Query().Get("after_id"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGINATION", "after_id must be a positive integer")
			return 0, 0, "", false
		}
		afterID = value
	}

	query := r.URL.Query().Get("q")
	if len([]rune(query)) > 200 {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGINATION", "q must not exceed 200 characters")
		return 0, 0, "", false
	}
	return afterID, limit, query, true
}

func parseAssignmentListQuery(w http.ResponseWriter, r *http.Request) (uint64, int, bool) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGINATION", "limit must be between 1 and 200")
			return 0, 0, false
		}
		limit = value
	}

	var afterID uint64
	if raw := r.URL.Query().Get("after_id"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGINATION", "after_id must be a positive integer")
			return 0, 0, false
		}
		afterID = value
	}
	return afterID, limit, true
}
