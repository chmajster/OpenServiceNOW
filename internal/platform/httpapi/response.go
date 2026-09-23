package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

var ErrUnsupportedMediaType = errors.New("unsupported media type")

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

func DecodeJSON(r *http.Request, target any) error {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return ErrUnsupportedMediaType
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func WriteDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrUnsupportedMediaType) {
		WriteError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		WriteError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Request body is too large")
		return
	}
	WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON request")
}

func WriteContextError(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		WriteError(w, r, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Request timed out")
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return false
}
