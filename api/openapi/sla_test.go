package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSLAAuthorizationAndDecimalContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, raw := range root["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/sla/") {
			continue
		}
		count++
		for method, raw := range raw.(map[string]any) {
			op := raw.(map[string]any)
			sec := op["security"].([]any)[0].(map[string]any)
			if _, ok := sec["sessionCookie"]; !ok {
				t.Fatal(path, "no session")
			}
			_, csrf := sec["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "CSRF mismatch")
			}
		}
	}
	if count != 4 {
		t.Fatal("missing SLA routes", count)
	}
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	for name, fields := range map[string][]string{"SLAInstance": {"id", "incident_id", "policy_id", "policy_version", "calendar_id", "calendar_version", "cycle", "incident_version", "version", "accrued_microseconds"}, "SLAHistoryEntry": {"id", "instance_id", "incident_version", "source_event_id"}} {
		props := schemas[name].(map[string]any)["properties"].(map[string]any)
		for _, field := range fields {
			if props[field].(map[string]any)["type"] != "string" {
				t.Fatal(name, field, "unsafe number")
			}
		}
	}
}
