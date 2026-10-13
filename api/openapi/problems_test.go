package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProblemContractRequiresDomainVersionsAndSecurity(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(loadDocument(t), &doc); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, raw := range doc["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/problems/") {
			continue
		}
		count++
		for method, raw := range raw.(map[string]any) {
			op := raw.(map[string]any)
			security := op["security"].([]any)[0].(map[string]any)
			if _, ok := security["sessionCookie"]; !ok {
				t.Fatal(path, "session")
			}
			_, csrf := security["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "CSRF")
			}
		}
	}
	if count < 15 {
		t.Fatal("incomplete Problem API", count)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"Problem", "ProblemInput", "ProblemTask", "ProblemTaskInput", "ProblemTransition", "ProblemIncidentLink"} {
		schema := schemas[name].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatal(name, "uncontrolled fields")
		}
		if schema["properties"].(map[string]any)["version"].(map[string]any)["type"] != "string" {
			t.Fatal(name, "BIGINT version")
		}
	}
}
