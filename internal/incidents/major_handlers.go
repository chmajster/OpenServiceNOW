package incidents

import (
	"net/http"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) MajorList(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r, 25, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, next, err := h.service.ListMajor(r.Context(), actor(r), r.URL.Query().Get("view"), after, limit)
	respond(w, r, 200, map[string]any{"items": items, "next_after_id": strconv.FormatUint(next, 10)}, err)
}
func (h *Handler) MyMajorActions(w http.ResponseWriter, r *http.Request) {
	after, limit, err := pagination(r, 25, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, next, err := h.service.ListMyMajorActions(r.Context(), actor(r), after, limit)
	respond(w, r, 200, map[string]any{"items": items, "next_after_id": strconv.FormatUint(next, 10)}, err)
}
func (h *Handler) MajorGet(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	view, err := h.service.MajorView(r.Context(), actor(r), id)
	respond(w, r, 200, view, err)
}
func decodeMajor(w http.ResponseWriter, r *http.Request, input any) (uint64, bool) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return 0, false
	}
	if !decodeBody(w, r, input) {
		return 0, false
	}
	return id, true
}
func (h *Handler) MajorDecision(w http.ResponseWriter, r *http.Request) {
	var input MajorDecisionInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	action := chi.URLParam(r, "decision")
	var result MajorCase
	var err error
	if action == "nominate" {
		result, err = h.service.NominateMajor(r.Context(), actor(r), id, input, key)
	} else {
		result, err = h.service.DecideMajor(r.Context(), actor(r), id, action, input, key)
	}
	respond(w, r, 200, result, err)
}
func (h *Handler) MajorCoordinate(w http.ResponseWriter, r *http.Request) {
	var input MajorCoordinationInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.UpdateMajorCoordination(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, result, err)
}
func (h *Handler) MajorPublish(w http.ResponseWriter, r *http.Request) {
	var input MajorUpdateInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.PublishMajorUpdate(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 201, result, err)
}
func (h *Handler) MajorRestore(w http.ResponseWriter, r *http.Request) {
	var input MajorRestoreInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.MarkMajorRestored(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, result, err)
}
func (h *Handler) MajorReview(w http.ResponseWriter, r *http.Request) {
	var input MajorReviewInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.SaveMajorReview(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, result, err)
}
func (h *Handler) MajorChildren(w http.ResponseWriter, r *http.Request) {
	var input MajorChildrenInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.MajorChildren(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, map[string]any{"results": result}, err)
}
func (h *Handler) MajorActionCreate(w http.ResponseWriter, r *http.Request) {
	var input MajorActionCreateInput
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.CreateMajorAction(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 201, result, err)
}
func (h *Handler) MajorActionChange(w http.ResponseWriter, r *http.Request) {
	var input MajorActionChangeInput
	id, err := parseMajorID(chi.URLParam(r, "actionID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !decodeBody(w, r, &input) {
		return
	}
	result, err := h.service.ChangeMajorAction(r.Context(), actor(r), id, input)
	respond(w, r, 200, result, err)
}
func (h *Handler) AssignmentPreview(w http.ResponseWriter, r *http.Request) {
	var input Fields
	if !decodeBody(w, r, &input) {
		return
	}
	id := uint64(0)
	if raw := r.URL.Query().Get("incident_id"); raw != "" {
		var err error
		id, err = parseMajorID(raw)
		if err != nil {
			writeError(w, r, err)
			return
		}
	}
	result, err := h.service.PreviewAssignment(r.Context(), actor(r), input, id)
	respond(w, r, 200, result, err)
}
func (h *Handler) AssignmentApply(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version uint64 `json:"version,string"`
	}
	id, ok := decodeMajor(w, r, &input)
	if !ok {
		return
	}
	result, err := h.service.ReapplyAssignment(r.Context(), actor(r), id, input.Version)
	respond(w, r, 200, result, err)
}

func decodeBody(w http.ResponseWriter, r *http.Request, input any) bool {
	if err := httpapi.DecodeJSON(r, input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return false
	}
	return true
}

func (h *Handler) MajorChildrenList(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	after, limit, err := pagination(r, 100, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, next, err := h.service.MajorLinkedChildren(r.Context(), actor(r), id, after, limit)
	respond(w, r, 200, map[string]any{"items": items, "next_after_id": strconv.FormatUint(next, 10)}, err)
}
func (h *Handler) MajorUpdatesList(w http.ResponseWriter, r *http.Request) {
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
	items, next, err := h.service.MajorUpdatePage(r.Context(), actor(r), id, before, limit, r.URL.Query().Get("audience") == "OPERATORS")
	respond(w, r, 200, map[string]any{"items": items, "next_before_id": strconv.FormatUint(next, 10)}, err)
}
func (h *Handler) MajorActionGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseMajorID(chi.URLParam(r, "actionID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.service.GetMajorAction(r.Context(), actor(r), id)
	respond(w, r, 200, item, err)
}

func (h *Handler) MajorActionMessages(w http.ResponseWriter, r *http.Request) {
	id, err := parseMajorID(chi.URLParam(r, "actionID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	page, err := h.service.MajorActionNotes(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, page, err)
}
func (h *Handler) MajorActionAddMessage(w http.ResponseWriter, r *http.Request) {
	id, err := parseMajorID(chi.URLParam(r, "actionID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input MajorActionNoteInput
	if !decodeBody(w, r, &input) {
		return
	}
	message, err := h.service.AddMajorActionNote(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 201, message, err)
}

func (h *Handler) MajorTimelineList(w http.ResponseWriter, r *http.Request) {
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
	items, next, err := h.service.MajorDecisionPage(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, map[string]any{"items": items, "next_before_id": strconv.FormatUint(next, 10)}, err)
}
