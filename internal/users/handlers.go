package users

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

type createRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type membershipRequest struct {
	Active *bool `json:"active"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := identity.FromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	result, err := h.service.List(r.Context(), session.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if result == nil {
		result = []User{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"users": result})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request createRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	userID, err := h.service.Create(r.Context(), actor, CreateInput{
		Email: request.Email, DisplayName: request.DisplayName, Password: request.Password,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": userID})
}

func (h *Handler) SetMembership(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromPath(w, r)
	if !ok {
		return
	}
	var request membershipRequest
	if err := httpapi.DecodeJSON(r, &request); err != nil || request.Active == nil {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Field active is required")
		return
	}
	actor, ok := actorFromRequest(r)
	if !ok {
		httpapi.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
		return
	}
	if err := h.service.SetMembershipActive(r.Context(), actor, userID, *request.Active); err != nil {
		writeError(w, r, err)
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

func userIDFromPath(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "userID"), 10, 64)
	if err != nil || id == 0 {
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_USER_ID", "Invalid user identifier")
		return 0, false
	}
	return id, true
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		httpapi.WriteError(w, r, http.StatusBadRequest, "INVALID_USER_INPUT", "Invalid user input")
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "User membership not found")
	case errors.Is(err, ErrConflict):
		httpapi.WriteError(w, r, http.StatusConflict, "USER_CONFLICT", "User already exists")
	default:
		httpapi.WriteError(w, r, http.StatusInternalServerError, "USER_OPERATION_FAILED", "User operation failed")
	}
}
