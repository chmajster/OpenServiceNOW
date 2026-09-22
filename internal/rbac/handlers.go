package rbac

import (
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
	roles, err := h.service.ListRoles(r.Context(), session.TenantID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var request createRoleRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON request")
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
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": roleID})
}

func (h *Handler) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	roleID, ok := pathID(w, r, "roleID")
	if !ok {
		return
	}
	var request setRolePermissionsRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON request")
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if err := h.service.SetRolePermissions(r.Context(), actor, roleID, request.Permissions); err != nil {
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
	groups, err := h.service.ListGroups(r.Context(), session.TenantID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var request createGroupRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON request")
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
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": groupID})
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
		IPAddress: requestinfo.DirectPeerIP(r),
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
	switch {
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
