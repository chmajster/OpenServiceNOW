package incidents

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func childID(r *http.Request, key string) (uint64, error) {
	id, err := strconv.ParseUint(chi.URLParam(r, key), 10, 64)
	if err != nil || id == 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
func (h *Handler) CommunicationAccess(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err == nil {
		_, err = h.service.Get(r.Context(), actor(r), id)
	}
	respond(w, r, 200, collaborationAccess(actor(r)), err)
}
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.service.Messages(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) AddMessage(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input CommunicationInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	p, err := h.service.AddMessage(r.Context(), actor(r), id, input, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, p, err)
}
func (h *Handler) FileList(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.service.FileList(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if _, err = h.service.Get(r.Context(), actor(r), id); err != nil {
		writeError(w, r, err)
		return
	}
	// ParseMultipartForm uses bounded temporary disk storage; RemoveAll covers all
	// exits. The router bounds the entire request, storage independently bounds file.
	r.Body = http.MaxBytesReader(w, r.Body, attachments.MaxMultipartBytes)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			writeError(w, r, attachments.ErrTooLarge)
		} else {
			writeError(w, r, ErrInvalid)
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	for key := range r.MultipartForm.Value {
		if key != "visibility" && key != "message_id" {
			writeError(w, r, ErrInvalid)
			return
		}
		if len(r.MultipartForm.Value[key]) != 1 {
			writeError(w, r, ErrInvalid)
			return
		}
	}
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		writeError(w, r, ErrInvalid)
		return
	}
	var messageID *uint64
	if raw := r.FormValue("message_id"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v == 0 {
			writeError(w, r, ErrInvalid)
			return
		}
		messageID = &v
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, ErrInvalid)
		return
	}
	defer file.Close()
	result, err := h.service.UploadFile(r.Context(), actor(r), id, header.Filename, r.FormValue("visibility"), messageID, file, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, result, err)
}
func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	fileID, err := childID(r, "fileID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	file, reader, err := h.service.OpenFile(r.Context(), actor(r), id, fileID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.FormatUint(file.Size, 10))
	w.WriteHeader(200)
	_, _ = io.Copy(w, reader)
}
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	fid, err := childID(r, "fileID")
	if err == nil {
		err = h.service.DeleteFile(r.Context(), actor(r), id, fid)
	}
	respond(w, r, 204, nil, err)
}
func (h *Handler) Watchers(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	after, limit, err := pagination(r, 20, "after_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := h.service.Watchers(r.Context(), actor(r), id, after, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) Watch(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	uid, err := childID(r, "userID")
	if err == nil {
		err = h.service.Watch(r.Context(), actor(r), id, uid, r.Method == "PUT")
	}
	respond(w, r, 204, nil, err)
}
func (h *Handler) Relations(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.service.Relations(r.Context(), actor(r), id, before, limit)
	respond(w, r, 200, p, err)
}
func (h *Handler) AddRelation(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input RelationInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	p, err := h.service.AddRelation(r.Context(), actor(r), id, input)
	respond(w, r, 200, p, err)
}
func (h *Handler) RemoveRelation(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rel, err := childID(r, "relationID")
	if err == nil {
		err = h.service.RemoveRelation(r.Context(), actor(r), id, rel)
	}
	respond(w, r, 204, nil, err)
}
func errorStatus(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return 404
	case errors.Is(err, ErrForbidden), errors.Is(err, workrecords.ErrForbidden):
		return 403
	case errors.Is(err, ErrConflict):
		return 409
	case errors.Is(err, ErrTransition):
		return 422
	case errors.Is(err, ErrInvalid):
		return 400
	}
	return 500
}
func (h *Handler) Batch(w http.ResponseWriter, r *http.Request) {
	var input BatchInput
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		httpapi.WriteDecodeError(w, r, err)
		return
	}
	if len(input.Items) < 1 || len(input.Items) > 50 || input.Action != "assign_self" && input.Action != "start" {
		writeError(w, r, ErrInvalid)
		return
	}
	seen := map[uint64]bool{}
	for _, i := range input.Items {
		if i.ID == 0 || i.Version == 0 || seen[i.ID] {
			writeError(w, r, ErrInvalid)
			return
		}
		seen[i.ID] = true
	}
	results := []BatchResult{}
	a := actor(r)
	for _, i := range input.Items {
		var updated Record
		var err error
		if input.Action == "assign_self" {
			var current Record
			current, err = h.service.Get(r.Context(), a, i.ID)
			if err == nil {
				updated, err = h.service.Assign(r.Context(), a, i.ID, AssignmentInput{Version: i.Version, AssignedTo: &a.UserID, AssignmentGroupID: current.Assignment.AssignmentGroupID, Reason: input.Reason})
			}
		} else {
			updated, err = h.service.Transition(r.Context(), a, i.ID, TransitionInput{Version: i.Version, State: "IN_PROGRESS"})
		}
		result := BatchResult{ID: i.ID, Status: 200, Message: "Zapisano"}
		if err == nil {
			result.Record = &updated
		} else {
			result.Status = errorStatus(err)
			switch result.Status {
			case 500:
				result.Message = "Nie potwierdzono zapisu. Odczytaj rekord przed ponowieniem."
			case 404:
				result.Message = "Brak rekordu lub dostępu."
			case 409:
				result.Message = "Konflikt wersji. Odczytaj rekord i ponownie wybierz akcję."
			case 403:
				result.Message = "Brak uprawnienia."
			default:
				result.Message = strings.TrimSpace(err.Error())
			}
		}
		results = append(results, result)
	}
	respond(w, r, 200, struct {
		Results []BatchResult `json:"results"`
	}{results}, nil)
}
