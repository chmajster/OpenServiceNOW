package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, status, ErrorEnvelope{Error: ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: middleware.GetReqID(r.Context()),
	}})
}
