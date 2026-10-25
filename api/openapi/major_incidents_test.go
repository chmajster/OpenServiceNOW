package openapi

import (
	"encoding/json"
	"testing"
)

func TestMajorIncidentContract(t *testing.T) {
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(loadDocument(t), &doc); err != nil {
		t.Fatal(err)
	}
	for path, methods := range map[string][]string{
		"/api/v1/incidents/major":                                   {"get"},
		"/api/v1/incidents/major/actions":                           {"get"},
		"/api/v1/incidents/major/actions/{actionID}":                {"get", "put"},
		"/api/v1/incidents/major/actions/{actionID}/messages":       {"get", "post"},
		"/api/v1/incidents/{incidentID}/major":                      {"get"},
		"/api/v1/incidents/{incidentID}/major/decisions/{decision}": {"post"},
		"/api/v1/incidents/{incidentID}/major/coordination":         {"put"},
		"/api/v1/incidents/{incidentID}/major/updates":              {"get", "post"},
		"/api/v1/incidents/{incidentID}/major/timeline":             {"get"},
		"/api/v1/incidents/{incidentID}/major/actions":              {"post"},
		"/api/v1/incidents/{incidentID}/major/children":             {"get", "post"},
		"/api/v1/incidents/{incidentID}/major/restoration":          {"post"},
		"/api/v1/incidents/{incidentID}/major/review":               {"put"},
		"/api/v1/incidents/assignment-preview":                      {"post"},
		"/api/v1/incidents/{incidentID}/assignment-rules":           {"post"},
	} {
		for _, method := range methods {
			var op struct {
				Security  []map[string]any `json:"security"`
				Responses map[string]any   `json:"responses"`
			}
			if err := json.Unmarshal(doc.Paths[path][method], &op); err != nil || len(op.Security) == 0 || len(op.Responses) == 0 {
				t.Errorf("missing secured operation %s %s", method, path)
			}
		}
	}
	var coordination struct {
		Properties map[string]struct {
			Type  string `json:"type"`
			Items struct {
				Type string `json:"type"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(doc.Components.Schemas["MajorCoordinationInput"], &coordination); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"team_group_ids", "service_ids", "ci_ids"} {
		if coordination.Properties[name].Items.Type != "string" {
			t.Errorf("%s must be an array of decimal strings", name)
		}
	}
}
