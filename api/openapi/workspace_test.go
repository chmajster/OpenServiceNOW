package openapi

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceMetadataAndReportsAreReadOnlyBoundedContracts(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(loadDocument(t), &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	for _, path := range []string{"/api/v1/workspace/search", "/api/v1/workspace/metadata", "/api/v1/workspace/context/{root}/{recordID}", "/api/v1/workspace/reports", "/api/v1/workspace/report-records", "/api/v1/workspace/directory/{kind}/{recordID}"} {
		item, ok := paths[path].(map[string]any)
		if !ok || len(item) != 1 {
			t.Fatal(path)
		}
		op := item["get"].(map[string]any)
		security := op["security"].([]any)[0].(map[string]any)
		if security["sessionCookie"] == nil {
			t.Fatal(path, "authentication")
		}
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	r := schemas["WorkspaceReference"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"id", "version"} {
		if r[key].(map[string]any)["type"] != "string" {
			t.Fatal(key, "precision")
		}
	}
	if len(r["type"].(map[string]any)["enum"].([]any)) != 11 {
		t.Fatal("missing domains")
	}
	if schemas["WorkspaceSearch"].(map[string]any)["properties"].(map[string]any)["items"].(map[string]any)["maxItems"] != float64(110) {
		t.Fatal("search bound")
	}
}
