package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkflowContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, item := range root["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/workflow/") {
			continue
		}
		count++
		for method, raw := range item.(map[string]any) {
			op := raw.(map[string]any)
			security := op["security"].([]any)[0].(map[string]any)
			if _, ok := security["sessionCookie"]; !ok {
				t.Fatal(path, "unauthenticated")
			}
			_, csrf := security["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "CSRF")
			}
		}
	}
	if count != 10 {
		t.Fatal("missing workflow paths", count)
	}
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	for name, keys := range map[string][]string{"WorkflowExecution": {"id", "version", "definition_id", "definition_version", "request_id", "requested_item_id", "executor_id", "group_id"}, "WorkflowDefinition": {"id", "version", "published_version"}, "WorkflowDecision": {"user_id"}, "WorkflowStep": {"version"}, "WorkflowAttempt": {"id", "execution_version"}, "WorkflowDecisionInput": {"version"}, "WorkflowRepair": {"version"}, "CatalogDefinition": {"workflow_id", "workflow_version"}, "Notification": {"execution_id"}} {
		props := schemas[name].(map[string]any)["properties"].(map[string]any)
		for _, key := range keys {
			if props[key].(map[string]any)["type"] != "string" {
				t.Fatal(name, key, "unsafe BIGINT")
			}
		}
	}
}
