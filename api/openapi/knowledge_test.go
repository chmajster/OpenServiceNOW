package openapi

import (
	"encoding/json"
	"testing"
)

func TestKnowledgeSecurityAndConcurrencyContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}

	paths := root["paths"].(map[string]any)
	operations := []struct {
		path, method string
		write        bool
		idempotent   bool
	}{
		{"/api/v1/knowledge/search", "get", false, false},
		{"/api/v1/knowledge/spaces", "get", false, false},
		{"/api/v1/knowledge/spaces", "post", true, true},
		{"/api/v1/knowledge/spaces/{spaceID}", "get", false, false},
		{"/api/v1/knowledge/spaces/{spaceID}", "put", true, false},
		{"/api/v1/knowledge/spaces/{spaceID}/pages", "get", false, false},
		{"/api/v1/knowledge/spaces/{spaceID}/pages", "post", true, true},
		{"/api/v1/knowledge/spaces/{spaceID}/search", "get", false, false},
		{"/api/v1/knowledge/spaces/{spaceID}/attention", "get", false, false},
		{"/api/v1/knowledge/spaces/{spaceID}/subscription", "put", true, false},
		{"/api/v1/knowledge/spaces/{spaceID}/subscription", "delete", true, false},
		{"/api/v1/knowledge/pages/{pageID}", "get", false, false},
		{"/api/v1/knowledge/pages/{pageID}", "put", true, false},
		{"/api/v1/knowledge/pages/{pageID}/versions", "get", false, false},
	}
	for _, item := range operations {
		rawPath, ok := paths[item.path].(map[string]any)
		if !ok {
			t.Fatalf("knowledge path %s is missing", item.path)
		}
		operation, ok := rawPath[item.method].(map[string]any)
		if !ok {
			t.Fatalf("%s %s operation is missing", item.method, item.path)
		}
		security := operation["security"].([]any)[0].(map[string]any)
		if _, ok := security["sessionCookie"]; !ok {
			t.Errorf("%s %s lacks session authentication", item.method, item.path)
		}
		_, csrf := security["csrfHeader"]
		if csrf != item.write {
			t.Errorf("%s %s CSRF=%t", item.method, item.path, csrf)
		}
		if item.idempotent && !hasHeaderParameter(operation, "Idempotency-Key") {
			t.Errorf("%s %s lacks Idempotency-Key", item.method, item.path)
		}
		if item.idempotent {
			responses, _ := operation["responses"].(map[string]any)
			for _, status := range []string{"200", "201"} {
				if _, ok := responses[status]; !ok {
					t.Errorf("%s %s lacks documented %s idempotent-create response", item.method, item.path, status)
				}
			}
			schemaRef := func(status string) string {
				response, _ := responses[status].(map[string]any)
				content, _ := response["content"].(map[string]any)
				jsonContent, _ := content["application/json"].(map[string]any)
				schema, _ := jsonContent["schema"].(map[string]any)
				ref, _ := schema["$ref"].(string)
				return ref
			}
			if replay, created := schemaRef("200"), schemaRef("201"); replay == "" || replay != created {
				t.Errorf("%s %s replay and creation schemas differ: 200=%q 201=%q", item.method, item.path, replay, created)
			}
		}
	}
}

func TestKnowledgeIdentifiersUseDecimalStrings(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{
		"#/components/schemas/KnowledgeSpace/properties/id",
		"#/components/schemas/KnowledgeSpace/properties/version",
		"#/components/schemas/KnowledgeSpace/properties/page_count",
		"#/components/schemas/KnowledgeSpaces/properties/next_after_id",
		"#/components/schemas/KnowledgePageSummary/properties/id",
		"#/components/schemas/KnowledgePageSummary/properties/space_id",
		"#/components/schemas/KnowledgePageSummary/properties/version",
		"#/components/schemas/KnowledgePage/properties/id",
		"#/components/schemas/KnowledgePage/properties/space_id",
		"#/components/schemas/KnowledgePage/properties/version",
		"#/components/schemas/KnowledgePageVersion/properties/page_id",
		"#/components/schemas/KnowledgePageVersion/properties/version",
	} {
		value, err := resolveLocalRef(root, ref)
		if err != nil {
			t.Errorf("%s: %v", ref, err)
			continue
		}
		if value.(map[string]any)["type"] != "string" {
			t.Errorf("%s is not a decimal string", ref)
		}
	}
}

func TestKnowledgeArticleLifecycleContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	for _, schemaName := range []string{"KnowledgePageSummary", "KnowledgePage", "KnowledgePageInput", "KnowledgePageVersion"} {
		value, err := resolveLocalRef(root, "#/components/schemas/"+schemaName+"/properties/status/enum")
		if err != nil {
			t.Fatal(err)
		}
		values := value.([]any)
		if len(values) != 3 || values[0] != "DRAFT" || values[1] != "PUBLISHED" || values[2] != "RETIRED" {
			t.Errorf("%s lifecycle = %#v", schemaName, values)
		}
	}
}

func hasHeaderParameter(operation map[string]any, name string) bool {
	parameters, _ := operation["parameters"].([]any)
	for _, raw := range parameters {
		parameter, _ := raw.(map[string]any)
		if parameter["in"] == "header" && parameter["name"] == name && parameter["required"] == true {
			return true
		}
	}
	return false
}
