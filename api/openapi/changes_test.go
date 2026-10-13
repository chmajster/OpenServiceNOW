package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChangeContractPinsEvaluationAndSecuresCommands(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(loadDocument(t), &doc); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, raw := range doc["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/changes/") {
			continue
		}
		count++
		for method, v := range raw.(map[string]any) {
			op := v.(map[string]any)
			sec := op["security"].([]any)[0].(map[string]any)
			if _, ok := sec["sessionCookie"]; !ok {
				t.Fatal(path, "session")
			}
			_, csrf := sec["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "CSRF")
			}
		}
	}
	if count < 25 {
		t.Fatal("incomplete Change API", count)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"Change", "ChangeInput", "ChangeTransition", "ChangeTaskInput", "ChangeCalendarRule", "ChangeTemplate", "ChangeProblemLink"} {
		schema := schemas[name].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatal(name, "uncontrolled fields")
		}
		if schema["properties"].(map[string]any)["version"].(map[string]any)["type"] != "string" {
			t.Fatal(name, "BIGINT version")
		}
	}
	snapshot := schemas["ChangeEvaluationSnapshot"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"version", "assessment_revision", "fields", "cis"} {
		if snapshot[key] == nil {
			t.Fatal("missing evaluated field", key)
		}
	}
	for _, name := range []string{"RecordView", "RecordViewInput"} {
		values := schemas[name].(map[string]any)["properties"].(map[string]any)["domain"].(map[string]any)["enum"].([]any)
		for _, domain := range []string{"cmdb", "problems", "changes"} {
			found := false
			for _, v := range values {
				found = found || v == domain
			}
			if !found {
				t.Fatal(name, domain)
			}
		}
	}
}
