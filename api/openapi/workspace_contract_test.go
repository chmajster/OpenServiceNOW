package openapi

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceContractDocumentsImplementedRoutes(t *testing.T) {
	raw := loadDocument(t)
	var doc struct {
		Paths      map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse OpenAPI: %v", err)
	}
	required := map[string][]string{
		"/api/v1/workspace/fields":            {"get"},
		"/api/v1/workspace/references/{kind}": {"get"},
		"/api/v1/workspace/queue":             {"post"},
		"/api/v1/workspace/items/{type}/{id}": {"get"},
		"/api/v1/workspace/views":             {"get", "post"},
		"/api/v1/workspace/views/{id}":        {"put", "delete"},
		"/api/v1/workspace/actions":           {"post"},
		"/api/v1/workspace/exports":           {"post"},
		"/api/v1/workspace/exports/{id}":      {"get"},
		"/api/v1/workspace/exports/{id}/file": {"get"},
	}
	for path, methods := range required {
		operations, ok := doc.Paths[path]
		if !ok {
			t.Errorf("missing workspace path %s", path)
			continue
		}
		for _, method := range methods {
			operation, ok := operations[method].(map[string]any)
			if !ok {
				t.Errorf("missing %s %s", method, path)
				continue
			}
			if len(operation["security"].([]any)) == 0 {
				t.Errorf("%s %s has no session security", method, path)
			}
			if len(operation["responses"].(map[string]any)) == 0 {
				t.Errorf("%s %s has no response contract", method, path)
			}
		}
	}
	for _, name := range []string{"WorkspaceQuery", "WorkspaceFilterNode", "WorkspaceWorkItem", "WorkspaceQueuePage", "WorkspaceView", "WorkspaceActionInput", "WorkspaceExportStatus"} {
		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("missing schema %s", name)
		}
	}
}
