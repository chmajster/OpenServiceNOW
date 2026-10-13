package incidents

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type PermissionProvider interface {
	Permissions(context.Context, identity.SessionIdentity) ([]string, error)
}
type Handler struct {
	service     *Service
	permissions PermissionProvider
}

func NewHandler(service *Service, permissions PermissionProvider) *Handler {
	return &Handler{service, permissions}
}
func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/incidents", func(r chi.Router) {
		r.Use(m.Authenticate)
		r.Use(h.context)
		r.Get("/", h.List)
		r.Get("/metadata", h.Metadata)
		r.Get("/lookups/{kind}", h.Choices)
		r.With(m.RequireCSRF).Post("/batch", h.Batch)
		r.With(m.RequireCSRF).Post("/", h.Create)
		r.Get("/{incidentID}", h.Get)
		r.Get("/{incidentID}/history", h.History)
		r.Get("/{incidentID}/communication-access", h.CommunicationAccess)
		r.Get("/{incidentID}/messages", h.Messages)
		r.With(m.RequireCSRF).Post("/{incidentID}/messages", h.AddMessage)
		r.Get("/{incidentID}/files", h.FileList)
		r.With(m.RequireCSRF).Post("/{incidentID}/files", h.UploadFile)
		r.Get("/{incidentID}/files/{fileID}", h.DownloadFile)
		r.With(m.RequireCSRF).Delete("/{incidentID}/files/{fileID}", h.DeleteFile)
		r.Get("/{incidentID}/watchers", h.Watchers)
		r.With(m.RequireCSRF).Put("/{incidentID}/watchers/{userID}", h.Watch)
		r.With(m.RequireCSRF).Delete("/{incidentID}/watchers/{userID}", h.Watch)
		r.Get("/{incidentID}/relations", h.Relations)
		r.With(m.RequireCSRF).Post("/{incidentID}/relations", h.AddRelation)
		r.With(m.RequireCSRF).Delete("/{incidentID}/relations/{relationID}", h.RemoveRelation)
		r.With(m.RequireCSRF).Put("/{incidentID}", h.Update)
		r.With(m.RequireCSRF).Put("/{incidentID}/assignment", h.Assign)
		r.With(m.RequireCSRF).Post("/{incidentID}/transition", h.Transition)
		r.With(m.RequireCSRF).Put("/{incidentID}/priority", h.Priority)
	})
}

type actorKey struct{}

func (h *Handler) context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, ok := identity.FromContext(r.Context())
		if !ok {
			writeError(w, r, ErrForbidden)
			return
		}
		permissions, err := h.permissions.Permissions(r.Context(), session)
		if err != nil {
			writeError(w, r, err)
			return
		}
		a := Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context()), IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
		for _, p := range permissions {
			a.Permissions[p] = true
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}
func actor(r *http.Request) Actor { a, _ := r.Context().Value(actorKey{}).(Actor); return a }
func objectID(r *http.Request) (uint64, error) {
	id, err := strconv.ParseUint(chi.URLParam(r, "incidentID"), 10, 64)
	if err != nil || id == 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
func respond(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, status, value)
}
func (h *Handler) Metadata(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Metadata(actor(r))
	respond(w, r, 200, result, err)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var input Fields
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, created, err := h.service.Create(r.Context(), actor(r), input, r.Header.Get("Idempotency-Key"))
	status := 200
	if created {
		status = 201
	}
	respond(w, r, status, result, err)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.service.Get(r.Context(), actor(r), id)
	respond(w, r, 200, result, err)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input UpdateInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.Update(r.Context(), actor(r), id, input)
	respond(w, r, 200, result, err)
}
func (h *Handler) Assign(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input AssignmentInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.Assign(r.Context(), actor(r), id, input)
	respond(w, r, 200, result, err)
}
func (h *Handler) Transition(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input TransitionInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.Transition(r.Context(), actor(r), id, input)
	respond(w, r, 200, result, err)
}
func (h *Handler) Priority(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input PriorityInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	result, err := h.service.OverridePriority(r.Context(), actor(r), id, input)
	respond(w, r, 200, result, err)
}
func pagination(r *http.Request, defaultLimit int, cursor string) (uint64, int, error) {
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, 0, ErrInvalid
		}
		limit = value
	}
	var after uint64
	if raw := r.URL.Query().Get(cursor); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			return 0, 0, ErrInvalid
		}
		after = value
	}
	return after, limit, nil
}
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	before, limit, err := pagination(r, 20, "before_event_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.service.History(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, result, err)
}
func (h *Handler) Choices(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r, 50, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var group uint64
	if raw := r.URL.Query().Get("group_id"); raw != "" {
		group, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || group == 0 {
			writeError(w, r, ErrInvalid)
			return
		}
	}
	result, err := h.service.Choices(r.Context(), actor(r), chi.URLParam(r, "kind"), r.URL.Query().Get("purpose"), r.URL.Query().Get("q"), after, group, limit)
	respond(w, r, 200, result, err)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r, 25, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	for _, k := range []string{"active", "sla_risk"} {
		if v := q.Get(k); v != "" && v != "true" && v != "false" {
			writeError(w, r, ErrInvalid)
			return
		}
	}
	expression, err := recordlists.Decode(q.Get("filter"))
	if err != nil {
		writeError(w, r, ErrInvalid)
		return
	}
	f := Filter{Expression: expression, GroupBy: q.Get("group_by"), AfterValue: q.Get("after_value"), Query: q.Get("q"), State: q.Get("state"), Category: q.Get("category"), View: q.Get("view"), Sort: q.Get("sort"), AfterID: after, Limit: limit, Active: q.Get("active") == "true", SLARisk: q.Get("sla_risk") == "true"}
	if f.View == "" {
		f.View = "all"
	}
	if f.Sort == "" {
		f.Sort = "newest"
	}
	for _, item := range []struct {
		key    string
		target *uint64
	}{{"assigned_to", &f.AssignedTo}, {"group_id", &f.GroupID}, {"reporter_id", &f.ReporterID}} {
		if raw := q.Get(item.key); raw != "" {
			*item.target, err = strconv.ParseUint(raw, 10, 64)
			if err != nil || *item.target == 0 {
				writeError(w, r, ErrInvalid)
				return
			}
		}
	}
	for _, item := range []struct {
		key    string
		target *int
	}{{"priority", &f.Priority}, {"after_priority", &f.AfterPriority}} {
		if raw := q.Get(item.key); raw != "" {
			*item.target, err = strconv.Atoi(raw)
			if err != nil || *item.target < 1 || *item.target > 5 {
				writeError(w, r, ErrInvalid)
				return
			}
		}
	}
	for _, item := range []struct {
		key    string
		target *time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if raw := q.Get(item.key); raw != "" {
			*item.target, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				writeError(w, r, ErrInvalid)
				return
			}
		}
	}
	if q.Get("format") == "csv" {
		f.AfterID = 0
		f.AfterPriority = 0
		f.AfterValue = ""
		f.GroupBy = ""
		items, err := h.service.Export(r.Context(), actor(r), f)
		writeCSV(w, r, items, err)
		return
	}
	if q.Get("format") != "" {
		writeError(w, r, ErrInvalid)
		return
	}
	result, err := h.service.List(r.Context(), actor(r), f)
	respond(w, r, 200, result, err)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, attachments.ErrTooLarge):
		httpapi.WriteError(w, r, 413, "FILE_TOO_LARGE", "Plik może mieć najwyżej 10 MiB; całe żądanie 11 MiB.")
	case errors.Is(err, attachments.ErrInvalid):
		httpapi.WriteError(w, r, 400, "FILE_INVALID", "Wybierz niepusty plik z poprawną nazwą i kategorią dostępu.")
	case errors.Is(err, attachments.ErrNotFound):
		httpapi.WriteError(w, r, 404, "FILE_NOT_FOUND", "Plik nie istnieje lub nie masz do niego dostępu.")
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, 404, "INCIDENT_NOT_FOUND", "Incydent nie istnieje lub nie masz do niego dostępu.")
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(w, r, 403, "FORBIDDEN", "Nie masz uprawnienia do tej operacji incydentu.")
	case errors.Is(err, ErrConflict), errors.Is(err, workrecords.ErrConflict):
		httpapi.WriteError(w, r, 409, "INCIDENT_CONFLICT", "Incydent zmienił się. Odczytaj aktualną wersję i rozstrzygnij konflikt.")
	case errors.Is(err, ErrIdempotency):
		httpapi.WriteError(w, r, 409, "INCIDENT_IDEMPOTENCY_CONFLICT", "Ten klucz tworzenia był użyty z innymi danymi.")
	case errors.Is(err, ErrTransition):
		httpapi.WriteError(w, r, 422, "INCIDENT_TRANSITION_INVALID", "To przejście stanu lub edycja nie jest dozwolona.")
	case errors.Is(err, ErrInvalid):
		httpapi.WriteError(w, r, 400, "INCIDENT_INVALID", err.Error())
	case errors.Is(err, database.ErrCommitUncertain):
		httpapi.WriteError(w, r, 500, "INCIDENT_RESULT_UNKNOWN", "Nie potwierdzono wyniku. Tworzenie sprawdź ponawiając te same dane z tym samym kluczem; aktualizację sprawdź przez odczyt rekordu.")
	default:
		httpapi.WriteError(w, r, 500, "INCIDENT_FAILED", "Nie udało się wykonać operacji incydentu.")
	}
}
