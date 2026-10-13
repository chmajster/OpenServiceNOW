package changes

import (
	"context"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"regexp"
	"strconv"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	service     *Service
	permissions PermissionProvider
}

func NewHandler(s *Service, p PermissionProvider) *Handler { return &Handler{s, p} }

type actorKey struct{}

var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/changes", func(r chi.Router) {
		r.Use(m.Authenticate, h.context)
		r.Get("/", h.list)
		r.With(m.RequireCSRF).Post("/", h.save)
		r.Get("/lookups/{kind:users|groups|problems}", h.lookup)
		r.Get("/source-problems/{problemID}", h.source)
		r.Get("/calendar", h.calendar)
		r.Get("/calendar-rules", h.rules)
		r.Get("/calendar-rules/{ruleID}", h.ruleGet)
		r.With(m.RequireCSRF).Post("/calendar-rules", h.ruleSave)
		r.With(m.RequireCSRF).Put("/calendar-rules/{ruleID}", h.ruleSave)
		r.Get("/templates", h.templates)
		r.Get("/templates/{templateID}", h.templateGet)
		r.Get("/templates/{templateID}/versions/{version}", h.templateVersion)
		r.With(m.RequireCSRF).Post("/templates", h.templateSave)
		r.With(m.RequireCSRF).Put("/templates/{templateID}", h.templateSave)
		r.With(m.RequireCSRF).Post("/templates/{templateID}/publish", h.templatePublish)
		r.Get("/approval-processes", h.approvalProcesses)
		r.Get("/approval-processes/{definitionID}", h.approvalProcess)
		r.Get("/{id}/collisions", h.collisions)
		r.Get("/{id}", h.get)
		r.With(m.RequireCSRF).Put("/{id}", h.save)
		r.With(m.RequireCSRF).Post("/{id}/transition", h.transition)
		r.Get("/{id}/history", h.history)
		r.Get("/{id}/problems", h.problems)
		r.With(m.RequireCSRF).Put("/{id}/problems/{problemID}", h.link)
		r.With(m.RequireCSRF).Delete("/{id}/problems/{problemID}", h.link)
		r.Get("/{id}/tasks", h.tasks)
		r.Get("/{id}/tasks/{taskID}", h.taskGet)
		r.With(m.RequireCSRF).Post("/{id}/tasks", h.taskSave)
		r.With(m.RequireCSRF).Put("/{id}/tasks/{taskID}", h.taskSave)
		r.Get("/{id}/communication-access", h.CommunicationAccess)
		r.Get("/{id}/messages", h.Messages)
		r.With(m.RequireCSRF).Post("/{id}/messages", h.AddMessage)
		r.Get("/{id}/files", h.FileList)
		r.With(m.RequireCSRF).Post("/{id}/files", h.UploadFile)
		r.Get("/{id}/files/{fileID}", h.DownloadFile)
		r.With(m.RequireCSRF).Delete("/{id}/files/{fileID}", h.DeleteFile)
	})
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	var out Record
	if err == nil {
		out, err = h.service.Get(r.Context(), actor(r), id)
	}
	respond(w, r, 200, out, err)
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	id, err := number(chi.URLParam(r, "id"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in Input
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.Save(r.Context(), actor(r), id, in, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}
func (h *Handler) transition(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in TransitionInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.Transition(r.Context(), actor(r), id, in, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	node, err := recordlists.Decode(q.Get("filter"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	problem, err := number(q.Get("problem_id"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	f := Filter{Before: before, Limit: limit, Query: q.Get("q"), State: q.Get("state"), Impact: q.Get("impact"), Active: q.Get("active"), Risk: q.Get("risk"), Type: q.Get("change_type"), Expression: node, Sort: q.Get("sort"), GroupBy: q.Get("group_by"), BeforeValue: q.Get("before_value"), ProblemID: problem}
	if q.Get("format") == "csv" {
		h.export(w, r, actor(r), f)
		return
	}
	out, err := h.service.List(r.Context(), actor(r), f)
	respond(w, r, 200, out, err)
}
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
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
	out, err := h.service.History(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, out, err)
}
func (h *Handler) source(w http.ResponseWriter, r *http.Request) {
	id, err := childID(r, "problemID")
	var out problems.ChangeSnapshot
	if err == nil {
		out, err = h.service.SourceProblem(r.Context(), actor(r), id)
	}
	respond(w, r, 200, out, err)
}
func (h *Handler) problems(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.ProblemChoices(r.Context(), actor(r), r.URL.Query().Get("q"), id, before, limit)
	respond(w, r, 200, out, err)
}
func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	problem, err := childID(r, "problemID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in LinkInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.LinkProblem(r.Context(), actor(r), id, problem, in, r.Method == http.MethodDelete, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}
func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.Tasks(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, out, err)
}
func (h *Handler) taskSave(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	task, err := number(chi.URLParam(r, "taskID"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in TaskInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.SaveTask(r.Context(), actor(r), id, task, in, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}

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
	status, code, message := 500, "PROBLEM_FAILED", "Nie udało się wykonać operacji."
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, workflow.ErrInvalid), errors.Is(err, cmdb.ErrInvalid), errors.Is(err, recordlists.ErrInvalid), errors.Is(err, workrecords.ErrInvalid), errors.Is(err, attachments.ErrInvalid):
		status, code, message = 400, "INVALID_REQUEST", err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, problems.ErrNotFound), errors.Is(err, workflow.ErrNotFound), errors.Is(err, cmdb.ErrNotFound), errors.Is(err, attachments.ErrNotFound), errors.Is(err, workrecords.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Nie znaleziono dostępnego rekordu."
	case errors.Is(err, ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Brak uprawnień do operacji."
	case errors.Is(err, ErrConflict), errors.Is(err, problems.ErrConflict), errors.Is(err, workflow.ErrConflict), errors.Is(err, cmdb.ErrConflict), errors.Is(err, workrecords.ErrConflict):
		status, code, message = 409, "VERSION_CONFLICT", err.Error()
	case errors.Is(err, workrecords.ErrOperationConflict):
		status, code, message = 409, "IDEMPOTENCY_CONFLICT", err.Error()
	case errors.Is(err, ErrTransition), errors.Is(err, ErrCollision):
		status, code, message = 409, "INVALID_TRANSITION", err.Error()
	case errors.Is(err, attachments.ErrTooLarge):
		status, code, message = 413, "FILE_TOO_LARGE", "Plik przekracza limit 10 MiB."
	case errors.Is(err, database.ErrCommitUncertain):
		status, code, message = 503, "COMMIT_UNCERTAIN", "Nie potwierdzono zapisu. Ponów identyczną operację z tym samym kluczem."
	}
	httpapi.WriteError(w, r, status, code, message)
}

func (h *Handler) taskGet(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	task, err := childID(r, "taskID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.Task(r.Context(), actor(r), id, task)
	respond(w, r, 200, out, err)
}
