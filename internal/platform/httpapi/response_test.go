package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsOversizedBodyAs413(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"123456789"}`))
	recorder := httptest.NewRecorder()
	request.Body = http.MaxBytesReader(recorder, request.Body, 5)

	var payload map[string]any
	err := DecodeJSON(request, &payload)
	if err == nil {
		t.Fatal("expected oversized body error")
	}

	response := httptest.NewRecorder()
	WriteDecodeError(response, request, err)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"REQUEST_TOO_LARGE"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok","extra":true}`))
	var payload struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(request, &payload); err == nil {
		t.Fatal("expected unknown field error")
	}
}
