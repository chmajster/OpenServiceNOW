package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogRequestContracts(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, raw := range root["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/catalog/") && !strings.HasPrefix(path, "/api/v1/requests/") && !strings.HasPrefix(path, "/api/v1/fulfillment/") {
			continue
		}
		count++
		for method, raw := range raw.(map[string]any) {
			op := raw.(map[string]any)
			sec := op["security"].([]any)[0].(map[string]any)
			if _, ok := sec["sessionCookie"]; !ok {
				t.Fatal(path, "unauthenticated")
			}
			_, csrf := sec["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "missing CSRF")
			}
		}
	}
	if count != 30 {
		t.Fatal("missing request routes", count)
	}
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	for name, fields := range map[string][]string{"CatalogItem": {"id", "category_id", "version", "published_version"}, "CatalogPublication": {"id", "category_id", "version"}, "RequestedItem": {"id"}, "ServiceRequest": {"id", "requester_id", "version"}, "FulfillmentTask": {"id", "request_id", "requested_item_id", "version"}, "FulfillmentAssignment": {"assigned_to", "assignment_group_id", "version"}, "RequestHistoryEntry": {"actor_user_id", "source_event_id"}} {
		p := schemas[name].(map[string]any)["properties"].(map[string]any)
		for _, field := range fields {
			if p[field].(map[string]any)["type"] != "string" {
				t.Fatal(name, field, "unsafe BIGINT")
			}
		}
	}
	p := schemas["CatalogPublication"].(map[string]any)["properties"].(map[string]any)
	for _, private := range []string{"draft", "group_id", "audience_group_id", "steps", "method"} {
		if _, ok := p[private]; ok {
			t.Fatal("private publication data", private)
		}
	}
}
