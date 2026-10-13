package requests

import (
	"context"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"net/http"
)

type BatchRecord struct {
	ID      uint64 `json:"id,string"`
	Version uint64 `json:"version,string"`
}
type BatchInput struct {
	Operation string        `json:"operation"`
	Records   []BatchRecord `json:"records"`
}
type BatchResult struct {
	ID      uint64 `json:"id,string"`
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Service) Batch(ctx context.Context, a Actor, in BatchInput) ([]BatchResult, error) {
	if !a.Has("request.manage") && !a.Has("request.fulfill") {
		return nil, ErrForbidden
	}
	if len(in.Records) < 1 || len(in.Records) > 50 || (in.Operation != "ASSIGN_SELF" && in.Operation != "START") {
		return nil, ErrInvalid
	}
	seen := map[uint64]bool{}
	for _, r := range in.Records {
		if r.ID == 0 || r.Version == 0 || seen[r.ID] {
			return nil, ErrInvalid
		}
		seen[r.ID] = true
	}
	out := []BatchResult{}
	for _, r := range in.Records {
		change := Change{Version: r.Version, State: "IN_PROGRESS"}
		assign := in.Operation == "ASSIGN_SELF"
		if assign {
			change.AssignedTo = &a.UserID
		}
		_, err := s.ChangeTask(ctx, a, r.ID, change, assign)
		result := BatchResult{ID: r.ID, Status: 200, Code: "SAVED", Message: "Zmiana zapisana"}
		switch {
		case err == nil:
		case errors.Is(err, ErrNotFound):
			result.Status, result.Code, result.Message = 404, "NOT_FOUND", "Rekord niedostępny"
		case errors.Is(err, ErrForbidden):
			result.Status, result.Code, result.Message = 403, "FORBIDDEN", "Brak uprawnień do działania"
		case errors.Is(err, ErrConflict), errors.Is(err, workrecords.ErrConflict):
			result.Status, result.Code, result.Message = 409, "VERSION_CONFLICT", "Rekord zmienił się; odczytaj aktualną wersję"
		case errors.Is(err, ErrTransition), errors.Is(err, ErrInvalid):
			result.Status, result.Code, result.Message = 422, "ACTION_INVALID", err.Error()
		case errors.Is(err, database.ErrCommitUncertain):
			result.Status, result.Code, result.Message = 503, "COMMIT_UNCERTAIN", "Nie potwierdzono zapisu; odczytaj aktualną wersję przed ponowieniem"
		default:
			result.Status, result.Code, result.Message = 503, "FAILED", "Nie wykonano operacji; spróbuj ponownie po odczytaniu stanu"
		}
		out = append(out, result)
	}
	return out, nil
}
func (h *Handler) batch(w http.ResponseWriter, r *http.Request) {
	var in BatchInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.Batch(r.Context(), actor(r), in)
	respond(w, r, 200, map[string]any{"results": out}, err)
}
