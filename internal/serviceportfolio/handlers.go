package serviceportfolio

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
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
	return &Handler{service: service, permissions: permissions}
}

type actorKey struct{}

func RegisterRoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/api/v1/service-portfolio", func(r chi.Router) {
		r.Use(m.Authenticate, h.context)
		r.Get("/items", h.listItems)
		r.With(m.RequireCSRF).Post("/items", h.saveItem)
		r.Get("/items/{id}", h.getItem)
		r.With(m.RequireCSRF).Put("/items/{id}", h.saveItem)
		r.Get("/items/{id}/history", h.itemHistory)
		r.Get("/items/{id}/relations", h.relations)
		r.With(m.RequireCSRF).Post("/items/{id}/relations", h.addRelation)
		r.With(m.RequireCSRF).Delete("/items/{id}/relations/{relationID}", h.removeRelation)
		r.With(m.RequireCSRF).Post("/items/{id}/lifecycle/{action:publish|activate|retire}", h.transitionItem)
		r.Get("/items/{id}/retirement-impact", h.retirementImpact)
		r.Get("/items/{id}/impact", h.itemImpact)
		r.Get("/items/{id}/records/{kind:incidents|requests|problems|changes}", h.relatedRecords)
		r.Get("/cis/{ciID}/impact", h.ciImpact)
		r.Get("/availability/{kind:ITEM|CI}/{id}", h.getAvailability)
		r.Get("/quality", h.quality)
		r.Get("/lookups/{kind:users|groups|locations|org_units|departments|cost_centers|cis|items|catalog_items|sla_policies}", h.lookup)
		r.Get("/classification/preview", h.classificationPreview)
		r.With(m.RequireCSRF).Post("/classification", h.classify)
		r.Get("/offers", h.offers)
		r.Get("/offers/{id}", h.getOffer)
		r.With(m.RequireCSRF).Post("/offers", h.saveOffer)
		r.With(m.RequireCSRF).Put("/offers/{id}", h.saveOffer)
		r.With(m.RequireCSRF).Post("/offers/{id}/lifecycle/{action:publish|withdraw}", h.transitionOffer)
		r.With(m.RequireCSRF).Post("/availability", h.setAvailability)
		r.With(m.RequireCSRF).Post("/org-refs", h.saveOrgRef)
		r.With(m.RequireCSRF).Put("/org-refs/{id}", h.saveOrgRef)
	})
}

func (h *Handler) context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		session, ok := identity.FromContext(r.Context())
		if !ok {
			responseError(w, r, workrecords.ErrForbidden)
			return
		}
		keys, err := h.permissions.Permissions(r.Context(), session)
		if err != nil {
			responseError(w, r, err)
			return
		}
		a := Actor{Actor: workrecords.Actor{TenantID: session.TenantID, UserID: session.UserID, RequestID: middleware.GetReqID(r.Context())}, IPAddress: requestinfo.ClientIP(r), Permissions: map[string]bool{}}
		for _, key := range keys {
			a.Permissions[key] = true
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}

func portfolioActor(r *http.Request) Actor { a, _ := r.Context().Value(actorKey{}).(Actor); return a }

func routeID(r *http.Request, key string, optional bool) (uint64, error) {
	value := chi.URLParam(r, key)
	if optional && value == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != value {
		return 0, ErrInvalid
	}
	return id, nil
}
func queryPage(r *http.Request) (uint64, int, error) {
	before := uint64(0)
	var err error
	if value := r.URL.Query().Get("before_id"); value != "" {
		before, err = routeValue(value)
		if err != nil {
			return 0, 0, err
		}
	}
	limit := 25
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, ErrInvalid
	}
	return before, limit, nil
}
func routeValue(value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != value {
		return 0, ErrInvalid
	}
	return id, nil
}
func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := httpapi.DecodeJSON(r, target); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return false
	}
	return true
}
func write(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err == nil {
		httpapi.WriteJSON(w, http.StatusOK, value)
		return
	}
	responseError(w, r, err)
}
func responseError(w http.ResponseWriter, r *http.Request, err error) {
	if httpapi.WriteContextError(w, r, err) {
		return
	}
	status, code, message := 500, "SERVICE_PORTFOLIO_FAILED", "Nie udało się wykonać operacji portfela usług"
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, workrecords.ErrInvalid):
		status, code, message = 400, "INVALID_REQUEST", err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, cmdb.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "Nie znaleziono dostępnego rekordu"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrDuplicate), errors.Is(err, cmdb.ErrConflict), errors.Is(err, workrecords.ErrOperationConflict):
		status, code, message = 409, "VERSION_CONFLICT", err.Error()
	case errors.Is(err, workrecords.ErrForbidden):
		status, code, message = 403, "FORBIDDEN", "Brak uprawnień do tej operacji"
	}
	httpapi.WriteError(w, r, status, code, message)
}

func (h *Handler) listItems(w http.ResponseWriter, r *http.Request) {
	a := portfolioActor(r)
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	owner, err := optionalQueryID(r, "owner_id")
	if err != nil {
		responseError(w, r, err)
		return
	}
	page, err := h.service.List(r.Context(), a, ItemFilter{Query: r.URL.Query().Get("q"), Type: strings.ToUpper(r.URL.Query().Get("type")), Lifecycle: strings.ToUpper(r.URL.Query().Get("lifecycle")), Environment: strings.ToUpper(r.URL.Query().Get("environment")), OwnerID: owner, Before: before, Limit: limit})
	write(w, r, page, err)
}
func (h *Handler) getItem(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	item, err := h.service.Get(r.Context(), portfolioActor(r), id)
	write(w, r, item, err)
}
func (h *Handler) saveItem(w http.ResponseWriter, r *http.Request) {
	a := portfolioActor(r)
	id, err := routeID(r, "id", true)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var input ItemInput
	if !decodeBody(w, r, &input) {
		return
	}
	item, err := h.service.Save(r.Context(), a, id, input, r.Header.Get("Idempotency-Key"))
	write(w, r, item, err)
}
func (h *Handler) itemHistory(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	page, err := h.service.History(r.Context(), portfolioActor(r), id, before, limit)
	write(w, r, page, err)
}

func (h *Handler) relations(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	items, next, err := h.service.Relations(r.Context(), portfolioActor(r), id, before, limit)
	write(w, r, map[string]any{"items": items, "next_before_id": idText(next)}, err)
}
func (h *Handler) addRelation(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var input RelationInput
	if !decodeBody(w, r, &input) {
		return
	}
	item, err := h.service.AddRelation(r.Context(), portfolioActor(r), id, input)
	write(w, r, item, err)
}
func (h *Handler) removeRelation(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	relationID, err := routeID(r, "relationID", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var input RemoveRelationInput
	if !decodeBody(w, r, &input) {
		return
	}
	item, err := h.service.RemoveRelation(r.Context(), portfolioActor(r), id, relationID, input.SourceVersion, input.RelationVersion, input.TargetVersion)
	write(w, r, item, err)
}

func (h *Handler) transitionItem(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var body struct {
		Version uint64 `json:"version,string"`
		Reason  string `json:"reason"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	item, err := h.service.TransitionItem(r.Context(), portfolioActor(r), id, body.Version, chi.URLParam(r, "action"), body.Reason)
	write(w, r, item, err)
}
func (h *Handler) retirementImpact(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	impact, err := h.service.RetirementImpact(r.Context(), portfolioActor(r), id)
	write(w, r, impact, err)
}
func (h *Handler) itemImpact(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	impact, err := h.service.ImpactFromItem(r.Context(), portfolioActor(r), id)
	write(w, r, impact, err)
}
func (h *Handler) ciImpact(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "ciID", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	impact, err := h.service.ImpactFromCI(r.Context(), portfolioActor(r), id)
	write(w, r, impact, err)
}
func (h *Handler) relatedRecords(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	result, err := h.service.RelatedRecords(r.Context(), portfolioActor(r), id, chi.URLParam(r, "kind"), before, limit)
	write(w, r, result, err)
}

func (h *Handler) quality(w http.ResponseWriter, r *http.Request) {
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	if r.URL.Query().Get("limit") == "" {
		limit = 100
	}
	if limit < 5 {
		responseError(w, r, ErrInvalid)
		return
	}
	page, err := h.service.Quality(r.Context(), portfolioActor(r), before, limit)
	write(w, r, page, err)
}
func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	lookup, err := h.service.ListLookup(r.Context(), portfolioActor(r), chi.URLParam(r, "kind"), r.URL.Query().Get("q"), before, limit)
	write(w, r, lookup, err)
}
func (h *Handler) classificationPreview(w http.ResponseWriter, r *http.Request) {
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	items, next, err := h.service.ClassificationPreview(r.Context(), portfolioActor(r), before, limit)
	write(w, r, map[string]any{"items": items, "next_before_id": idText(next)}, err)
}
func (h *Handler) classify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []ClassificationInput `json:"items"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	result, err := h.service.Classify(r.Context(), portfolioActor(r), body.Items)
	write(w, r, result, err)
}

func (h *Handler) offers(w http.ResponseWriter, r *http.Request) {
	before, limit, err := queryPage(r)
	if err != nil {
		responseError(w, r, err)
		return
	}
	page, _, err := h.service.Offers(r.Context(), portfolioActor(r), before, limit, r.URL.Query().Get("q"))
	write(w, r, page, err)
}
func (h *Handler) getOffer(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	offer, err := h.service.GetOffer(r.Context(), portfolioActor(r), id)
	write(w, r, offer, err)
}
func (h *Handler) saveOffer(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", true)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var input OfferInput
	if !decodeBody(w, r, &input) {
		return
	}
	offer, err := h.service.SaveOffer(r.Context(), portfolioActor(r), id, input, r.Header.Get("Idempotency-Key"))
	write(w, r, offer, err)
}
func (h *Handler) transitionOffer(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var body struct {
		Version uint64 `json:"version,string"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	offer, err := h.service.TransitionOffer(r.Context(), portfolioActor(r), id, body.Version, chi.URLParam(r, "action"))
	write(w, r, offer, err)
}
func (h *Handler) setAvailability(w http.ResponseWriter, r *http.Request) {
	var body AvailabilityInput
	if !decodeBody(w, r, &body) {
		return
	}
	result, err := h.service.SetAvailability(r.Context(), portfolioActor(r), body)
	write(w, r, result, err)
}
func (h *Handler) getAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", false)
	if err != nil {
		responseError(w, r, err)
		return
	}
	result, err := h.service.GetAvailability(r.Context(), portfolioActor(r), chi.URLParam(r, "kind"), id)
	write(w, r, result, err)
}
func (h *Handler) saveOrgRef(w http.ResponseWriter, r *http.Request) {
	id, err := routeID(r, "id", true)
	if err != nil {
		responseError(w, r, err)
		return
	}
	var body OrgRef
	if !decodeBody(w, r, &body) {
		return
	}
	result, err := h.service.SaveOrgRef(r.Context(), portfolioActor(r), id, body)
	write(w, r, result, err)
}

func optionalQueryID(r *http.Request, key string) (uint64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return 0, nil
	}
	return routeValue(value)
}
func idText(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}
