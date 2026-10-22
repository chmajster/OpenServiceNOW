package knowledge

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}

type APIService interface {
	CreateSpace(context.Context, Actor, SpaceInput, string) (Space, bool, error)
	UpdateSpace(context.Context, Actor, uint64, SpaceInput) (Space, error)
	Space(context.Context, Actor, uint64) (Space, error)
	Spaces(context.Context, Actor, uint64, int) (SpacePage, error)
	CreatePage(context.Context, Actor, uint64, PageInput, string) (Page, bool, error)
	UpdatePage(context.Context, Actor, uint64, PageInput) (Page, error)
	Page(context.Context, Actor, uint64) (Page, error)
	Pages(context.Context, Actor, uint64, uint64, int) (PagePage, error)
	Search(context.Context, Actor, uint64, string, uint64, int) (PagePage, error)
	Attention(context.Context, Actor, uint64, uint64, int, string) (PagePage, error)
	Versions(context.Context, Actor, uint64, uint64, int) (VersionPage, error)
	Subscribe(context.Context, Actor, uint64, bool) (Space, error)
}

type Handler struct {
	service     APIService
	permissions PermissionProvider
}

func NewHandler(service APIService, permissions PermissionProvider) *Handler {
	return &Handler{service: service, permissions: permissions}
}

func RegisterRoutes(router chi.Router, handler *Handler, auth *identity.Middleware) {
	router.Route("/api/v1/knowledge", func(router chi.Router) {
		router.Use(auth.Authenticate)
		router.Use(handler.context)
		router.Get("/spaces", handler.ListSpaces)
		router.With(auth.RequireCSRF).Post("/spaces", handler.CreateSpace)
		router.Get("/spaces/{spaceID}", handler.GetSpace)
		router.With(auth.RequireCSRF).Put("/spaces/{spaceID}", handler.UpdateSpace)
		router.Get("/spaces/{spaceID}/pages", handler.ListPages)
		router.With(auth.RequireCSRF).Post("/spaces/{spaceID}/pages", handler.CreatePage)
		router.Get("/search", handler.SearchAll)
		router.Get("/spaces/{spaceID}/search", handler.Search)
		router.Get("/spaces/{spaceID}/attention", handler.Attention)
		router.With(auth.RequireCSRF).Put("/spaces/{spaceID}/subscription", handler.Subscribe)
		router.With(auth.RequireCSRF).Delete("/spaces/{spaceID}/subscription", handler.Subscribe)
		router.Get("/pages/{pageID}", handler.GetPage)
		router.With(auth.RequireCSRF).Put("/pages/{pageID}", handler.UpdatePage)
		router.Get("/pages/{pageID}/versions", handler.Versions)
	})
}

type actorContextKey struct{}

func (handler *Handler) context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		session, ok := identity.FromContext(request.Context())
		if !ok {
			writeError(writer, request, ErrForbidden)
			return
		}
		permissions, err := handler.permissions.Permissions(request.Context(), session)
		if err != nil {
			writeError(writer, request, err)
			return
		}
		actor := Actor{
			TenantID:    session.TenantID,
			UserID:      session.UserID,
			RequestID:   middleware.GetReqID(request.Context()),
			IPAddress:   requestinfo.ClientIP(request),
			Permissions: map[string]bool{},
		}
		for _, permission := range permissions {
			actor.Permissions[permission] = true
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), actorContextKey{}, actor)))
	})
}

func requestActor(request *http.Request) Actor {
	actor, _ := request.Context().Value(actorContextKey{}).(Actor)
	return actor
}

func routeID(request *http.Request, name string) (uint64, error) {
	id, err := strconv.ParseUint(chi.URLParam(request, name), 10, 64)
	if err != nil || id == 0 {
		return 0, ErrInvalid
	}
	return id, nil
}

func pagination(request *http.Request, cursorName string) (uint64, int, error) {
	after := uint64(0)
	var err error
	if raw := request.URL.Query().Get(cursorName); raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || after == 0 {
			return 0, 0, ErrInvalid
		}
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, ErrInvalid
		}
	}
	return after, limit, nil
}

func respond(writer http.ResponseWriter, request *http.Request, status int, value any, err error) {
	if err != nil {
		writeError(writer, request, err)
		return
	}
	httpapi.WriteJSON(writer, status, value)
}

func (handler *Handler) ListSpaces(writer http.ResponseWriter, request *http.Request) {
	after, limit, err := pagination(request, "after_id")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	result, err := handler.service.Spaces(request.Context(), requestActor(request), after, limit)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) CreateSpace(writer http.ResponseWriter, request *http.Request) {
	var input SpaceInput
	if err := httpapi.DecodeJSON(request, &input); err != nil {
		httpapi.WriteDecodeError(writer, request, err)
		return
	}
	result, created, err := handler.service.CreateSpace(request.Context(), requestActor(request), input, request.Header.Get("Idempotency-Key"))
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(writer, request, status, result, err)
}

func (handler *Handler) GetSpace(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request, "spaceID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	result, err := handler.service.Space(request.Context(), requestActor(request), id)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) UpdateSpace(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request, "spaceID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	var input SpaceInput
	if err := httpapi.DecodeJSON(request, &input); err != nil {
		httpapi.WriteDecodeError(writer, request, err)
		return
	}
	result, err := handler.service.UpdateSpace(request.Context(), requestActor(request), id, input)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) ListPages(writer http.ResponseWriter, request *http.Request) {
	handler.pageList(writer, request, "list")
}

func (handler *Handler) Search(writer http.ResponseWriter, request *http.Request) {
	handler.pageList(writer, request, "search")
}

func (handler *Handler) SearchAll(writer http.ResponseWriter, request *http.Request) {
	after, limit, err := pagination(request, "after_id")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	result, err := handler.service.Search(request.Context(), requestActor(request), 0, request.URL.Query().Get("q"), after, limit)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) Attention(writer http.ResponseWriter, request *http.Request) {
	handler.pageList(writer, request, "attention")
}

func (handler *Handler) pageList(writer http.ResponseWriter, request *http.Request, mode string) {
	spaceID, err := routeID(request, "spaceID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	after, limit, err := pagination(request, "after_id")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	var result PagePage
	switch mode {
	case "search":
		result, err = handler.service.Search(request.Context(), requestActor(request), spaceID, request.URL.Query().Get("q"), after, limit)
	case "attention":
		result, err = handler.service.Attention(request.Context(), requestActor(request), spaceID, after, limit, request.URL.Query().Get("reason"))
	default:
		result, err = handler.service.Pages(request.Context(), requestActor(request), spaceID, after, limit)
	}
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) CreatePage(writer http.ResponseWriter, request *http.Request) {
	spaceID, err := routeID(request, "spaceID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	var input PageInput
	if err := httpapi.DecodeJSON(request, &input); err != nil {
		httpapi.WriteDecodeError(writer, request, err)
		return
	}
	result, created, err := handler.service.CreatePage(request.Context(), requestActor(request), spaceID, input, request.Header.Get("Idempotency-Key"))
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(writer, request, status, result, err)
}

func (handler *Handler) GetPage(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request, "pageID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	result, err := handler.service.Page(request.Context(), requestActor(request), id)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) UpdatePage(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request, "pageID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	var input PageInput
	if err := httpapi.DecodeJSON(request, &input); err != nil {
		httpapi.WriteDecodeError(writer, request, err)
		return
	}
	result, err := handler.service.UpdatePage(request.Context(), requestActor(request), id, input)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) Versions(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request, "pageID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	before, limit, err := pagination(request, "before_version")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	result, err := handler.service.Versions(request.Context(), requestActor(request), id, before, limit)
	respond(writer, request, http.StatusOK, result, err)
}

func (handler *Handler) Subscribe(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request, "spaceID")
	if err != nil {
		writeError(writer, request, err)
		return
	}
	result, err := handler.service.Subscribe(request.Context(), requestActor(request), id, request.Method == http.MethodPut)
	respond(writer, request, http.StatusOK, result, err)
}

func writeError(writer http.ResponseWriter, request *http.Request, err error) {
	if httpapi.WriteContextError(writer, request, err) {
		return
	}
	switch {
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(writer, request, http.StatusNotFound, "KNOWLEDGE_NOT_FOUND", "Baza wiedzy lub artykuł nie istnieje albo nie masz do nich dostępu.")
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(writer, request, http.StatusForbidden, "FORBIDDEN", "Nie masz uprawnienia do tej operacji w bazie wiedzy.")
	case errors.Is(err, ErrConflict):
		httpapi.WriteError(writer, request, http.StatusConflict, "KNOWLEDGE_CONFLICT", "Baza wiedzy zmieniła się. Odczytaj aktualną wersję i rozstrzygnij konflikt.")
	case errors.Is(err, workrecords.ErrOperationConflict):
		httpapi.WriteError(writer, request, http.StatusConflict, "KNOWLEDGE_IDEMPOTENCY_CONFLICT", "Ten klucz operacji bazy wiedzy był już użyty z innymi danymi.")
	case errors.Is(err, ErrInvalid):
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = "Dane bazy wiedzy są nieprawidłowe."
		}
		httpapi.WriteError(writer, request, http.StatusBadRequest, "KNOWLEDGE_INVALID", message)
	case errors.Is(err, database.ErrCommitUncertain):
		httpapi.WriteError(writer, request, http.StatusInternalServerError, "KNOWLEDGE_RESULT_UNKNOWN", "Nie potwierdzono wyniku operacji. Odśwież dane przed ponowieniem.")
	default:
		httpapi.WriteError(writer, request, http.StatusInternalServerError, "KNOWLEDGE_FAILED", "Nie udało się wykonać operacji w bazie wiedzy.")
	}
}
