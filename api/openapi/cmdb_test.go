package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCMDBSecurityAndTypedFields(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, raw := range root["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/cmdb/") {
			continue
		}
		count++
		for method, raw := range raw.(map[string]any) {
			op := raw.(map[string]any)
			security := op["security"].([]any)[0].(map[string]any)
			if _, ok := security["sessionCookie"]; !ok {
				t.Fatal(path, "session missing")
			}
			_, csrf := security["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "CSRF missing")
			}
		}
	}
	if count < 25 {
		t.Fatal("incomplete CMDB contracts", count)
	}
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	for name, fields := range map[string][]string{"CI": {"id", "version", "owner_id", "support_group_id"}, "CIRelation": {"id", "version", "source_id", "target_id"}, "CIRecordReference": {"id", "version"}, "InventoryImport": {"id", "version", "creator_id", "executor_id"}} {
		p := schemas[name].(map[string]any)["properties"].(map[string]any)
		for _, field := range fields {
			if p[field].(map[string]any)["type"] != "string" {
				t.Fatal(name, field, "BIGINT precision")
			}
		}
	}
	input := schemas["CIInput"].(map[string]any)
	if input["additionalProperties"] != false || len(input["oneOf"].([]any)) != 6 {
		t.Fatal("CI classes not typed")
	}
	for _, raw := range input["oneOf"].([]any) {
		p := raw.(map[string]any)["properties"].(map[string]any)
		if p["attributes"].(map[string]any)["additionalProperties"] != false {
			t.Fatal("arbitrary attribute JSON")
		}
	}
}
