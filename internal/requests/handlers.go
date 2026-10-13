package requests

import (
	"context"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"strconv"
	"strings"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	service     *Service
	catalog     *catalog.Service
	permissions PermissionProvider
}

func NewHandler(s *Service, c *catalog.Service, p PermissionProvider) *Handler {
	return &Handler{s, c, p}
}

type actorKey struct{}

func actor(r *http.Request) Actor { a, _ := r.Context().Value(actorKey{}).(Actor); return a }
func (h *Handler) context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, ok := identity.FromContext(r.Context())
		if !ok {
			writeError(w, r, ErrForbidden)
			return
		}
		keys, err := h.permissions.Permissions(r.Context(), session)
		if err != nil {
			writeError(w, r, err)
			return
		}
		a := Actor{Actor: workrecords.Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context())}, IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
		for _, k := range keys {
			a.Permissions[k] = true
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}
func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/catalog", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(h.context)
		r.Get("/items", h.CatalogList)
		r.Get("/items/{id}", h.CatalogGet)
		r.Get("/{kind:catalogs|categories}", h.Nodes)
		r.Get("/lookups/groups", h.Groups)
		r.Route("/admin", func(r chi.Router) {
			r.Use(m.RequirePermission("admin.catalog.manage"))
			r.Get("/templates", h.Templates)
			r.Get("/items", h.CatalogList)
			r.Get("/items/{id}", h.CatalogGet)
			r.With(m.RequireCSRF).Post("/items", h.CatalogSave)
			r.With(m.RequireCSRF).Put("/items/{id}", h.CatalogSave)
			r.With(m.RequireCSRF).Post("/items/{id}/publish", h.Publish)
			r.With(m.RequireCSRF).Post("/items/{id}/withdraw", h.Publish)
			r.Get("/{kind:catalogs|categories}", h.Nodes)
			r.Get("/{kind:catalogs|categories}/{id}", h.NodeGet)
			r.With(m.RequireCSRF).Post("/{kind:catalogs|categories}", h.SaveNode)
			r.With(m.RequireCSRF).Put("/{kind:catalogs|categories}/{id}", h.SaveNode)
		})
	})
	r.Route("/api/v1/requests", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(h.context)
		r.Get("/", h.List)
		r.Get("/lookups/{kind}", h.listChoices)
		r.With(m.RequireCSRF).Post("/", h.Create)
		r.Get("/{id}", h.Get)
		r.With(m.RequireCSRF).Post("/{id}/transition", h.Transition)
		r.Get("/{id}/history", h.History)
		r.Get("/{id}/communication-access", h.CommunicationAccess)
		r.Get("/{id}/messages", h.Messages)
		r.With(m.RequireCSRF).Post("/{id}/messages", h.AddMessage)
		r.Get("/{id}/files", h.FileList)
		r.With(m.RequireCSRF).Post("/{id}/files", h.UploadFile)
		r.Get("/{id}/files/{fileID}", h.DownloadFile)
		r.With(m.RequireCSRF).Delete("/{id}/files/{fileID}", h.DeleteFile)
	})
	r.Route("/api/v1/fulfillment", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(h.context)
		r.Get("/", h.List)
		r.With(m.RequireCSRF).Post("/batch", h.batch)
		r.Get("/lookups/{kind}", h.listChoices)
		r.Get("/{id}", h.Get)
		r.Get("/{id}/users", h.TaskUsers)
		r.With(m.RequireCSRF).Put("/{id}/assignment", h.TaskChange)
		r.With(m.RequireCSRF).Post("/{id}/transition", h.TaskChange)
	})
}
func number(s string, optional bool) (uint64, error) {
	if optional && s == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 {
		return 0, ErrInvalid
	}
	return n, nil
}
func objectID(r *http.Request) (uint64, error) { return number(chi.URLParam(r, "id"), false) }
func pagination(r *http.Request, defaultLimit int, key string) (uint64, int, error) {
	n, err := number(r.URL.Query().Get(key), true)
	if err != nil {
		return 0, 0, err
	}
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, ErrInvalid
	}
	return n, limit, nil
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := httpapi.DecodeJSON(r, v); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return false
	}
	return true
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
	status, code, message := 500, "REQUEST_FAILED", "Nie udało się wykonać operacji."
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, workflow.ErrInvalid), errors.Is(err, workrecords.ErrInvalid), errors.Is(err, attachments.ErrInvalid):
		status, code, message = 400, "INVALID_REQUEST", err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, workflow.ErrNotFound), errors.Is(err, attachments.ErrNotFound), errors.Is(err, workrecords.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Nie znaleziono dostępnego rekordu."
	case errors.Is(err, ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Brak uprawnień do operacji."
	case errors.Is(err, ErrConflict), errors.Is(err, workrecords.ErrConflict):
		status, code, message = 409, "VERSION_CONFLICT", err.Error()
	case errors.Is(err, ErrIdempotency):
		status, code, message = 409, "IDEMPOTENCY_CONFLICT", err.Error()
	case errors.Is(err, workrecords.ErrOperationConflict):
		status, code, message = 409, "IDEMPOTENCY_CONFLICT", err.Error()
	case errors.Is(err, ErrTransition):
		status, code, message = 409, "INVALID_TRANSITION", err.Error()
	case errors.Is(err, attachments.ErrTooLarge):
		status, code, message = 413, "FILE_TOO_LARGE", "Plik przekracza limit 10 MiB."
	case errors.Is(err, database.ErrCommitUncertain):
		status, code, message = 503, "COMMIT_UNCERTAIN", "Nie potwierdzono zapisu. Ponów identyczną operację z tym samym kluczem."
	}
	httpapi.WriteError(w, r, status, code, message)
}
func isAdmin(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/v1/catalog/admin/") }
func isTask(r *http.Request) bool  { return strings.HasPrefix(r.URL.Path, "/api/v1/fulfillment/") }
func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r, 25, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	a := actor(r)
	if !isAdmin(r) {
		p := map[string]bool{}
		for k, v := range a.Permissions {
			p[k] = v
		}
		delete(p, "admin.catalog.manage")
		a.Permissions = p
	}
	p, err := h.catalog.Nodes(r.Context(), a, chi.URLParam(r, "kind"), after, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) SaveNode(w http.ResponseWriter, r *http.Request) {
	var n catalog.Node
	if !decode(w, r, &n) {
		return
	}
	id, err := number(chi.URLParam(r, "id"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if id == 0 && !requestKeyPattern.MatchString(r.Header.Get("Idempotency-Key")) {
		writeError(w, r, ErrInvalid)
		return
	}
	p, err := h.catalog.SaveNode(r.Context(), actor(r), chi.URLParam(r, "kind"), id, n, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, p, err)
}
func (h *Handler) CatalogList(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r, 25, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	category, err := number(r.URL.Query().Get("category_id"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	catalogID, err := number(r.URL.Query().Get("catalog_id"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := h.catalog.Items(r.Context(), actor(r), isAdmin(r), r.URL.Query().Get("q"), category, catalogID, after, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) CatalogGet(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a := actor(r)
	if isAdmin(r) {
		p, err := catalog.GetItem(r.Context(), h.service.db, a.TenantID, id, false)
		respond(w, r, 200, p, err)
		return
	}
	if !a.Has("catalog.read") {
		writeError(w, r, ErrForbidden)
		return
	}
	p, _, err := catalog.Published(r.Context(), h.service.db, a, id, 0)
	respond(w, r, 200, p, err)
}
func (h *Handler) CatalogSave(w http.ResponseWriter, r *http.Request) {
	var n catalog.Item
	if !decode(w, r, &n) {
		return
	}
	id, err := number(chi.URLParam(r, "id"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if id == 0 && !requestKeyPattern.MatchString(r.Header.Get("Idempotency-Key")) {
		writeError(w, r, ErrInvalid)
		return
	}
	p, err := h.catalog.SaveItem(r.Context(), actor(r), id, n, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, p, err)
}
func (h *Handler) NodeGet(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := catalog.GetNode(r.Context(), h.service.db, actor(r).TenantID, chi.URLParam(r, "kind"), id)
	respond(w, r, 200, p, err)
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in Change
	if !decode(w, r, &in) {
		return
	}
	withdraw := r.URL.Path[len(r.URL.Path)-8:] == "withdraw"
	p, err := h.catalog.Publish(r.Context(), actor(r), id, in.Version, withdraw)
	respond(w, r, 200, p, err)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "mine"
		if isTask(r) {
			view = "group"
		}
	}
	if v := r.URL.Query().Get("active"); v != "" && v != "true" && v != "false" {
		writeError(w, r, ErrInvalid)
		return
	}
	o, err := parseListOptions(r.URL.Query().Get("filter"), r.URL.Query().Get("group_by"), r.URL.Query().Get("sort"), r.URL.Query().Get("before_value"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		h.export(w, r, view, o)
		return
	}
	if r.URL.Query().Get("format") != "" {
		writeError(w, r, ErrInvalid)
		return
	}
	p, err := h.service.ListFiltered(r.Context(), actor(r), isTask(r), r.URL.Query().Get("q"), r.URL.Query().Get("state"), view, before, limit, r.URL.Query().Get("active") == "true", o)
	respond(w, r, 200, p, err)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in Submission
	if !decode(w, r, &in) {
		return
	}
	p, created, err := h.service.Create(r.Context(), actor(r), in, r.Header.Get("Idempotency-Key"))
	status := 200
	if created {
		status = 201
	}
	respond(w, r, status, p, err)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if isTask(r) {
		p, err := h.service.Task(r.Context(), actor(r), id)
		respond(w, r, 200, p, err)
	} else {
		p, err := h.service.Get(r.Context(), actor(r), id)
		respond(w, r, 200, p, err)
	}
}
func (h *Handler) Transition(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in Change
	if !decode(w, r, &in) {
		return
	}
	p, err := h.service.Transition(r.Context(), actor(r), id, in)
	respond(w, r, 200, p, err)
}
func (h *Handler) TaskChange(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in Change
	if !decode(w, r, &in) {
		return
	}
	assign := r.Method == http.MethodPut
	p, err := h.service.ChangeTask(r.Context(), actor(r), id, in, assign)
	respond(w, r, 200, p, err)
}
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	before, limit, err := pagination(r, 20, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := h.service.History(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, p, err)
}
