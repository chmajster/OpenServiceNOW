package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCMDBIREOpenAPIUsesTypedContracts(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	paths := root["paths"].(map[string]any)
	for path, rawItem := range paths {
		if !strings.HasPrefix(path, "/api/v1/cmdb/ire/") {
			continue
		}
		item := rawItem.(map[string]any)
		for method, rawOperation := range item {
			if !isHTTPMethod(method) {
				continue
			}
			operation := rawOperation.(map[string]any)
			tags := operation["tags"].([]any)
			if len(tags) != 1 || tags[0] != "CMDB IRE" {
				t.Errorf("%s %s is not grouped under CMDB IRE", method, path)
			}
			responses := operation["responses"].(map[string]any)
			success := responses["200"].(map[string]any)
			content := success["content"].(map[string]any)
			media := content["application/json"].(map[string]any)
			responseSchema := media["schema"].(map[string]any)
			if _, err := resolveLocalRef(root, responseSchema["$ref"].(string)); err != nil {
				t.Errorf("%s %s response schema reference: %v", method, path, err)
			}
			if method == "post" || method == "put" {
				requestBody := operation["requestBody"].(map[string]any)
				bodyContent := requestBody["content"].(map[string]any)["application/json"].(map[string]any)
				bodySchema := bodyContent["schema"].(map[string]any)
				if _, err := resolveLocalRef(root, bodySchema["$ref"].(string)); err != nil {
					t.Errorf("%s %s request schema reference: %v", method, path, err)
				}
			}
		}
	}

	findings := paths["/api/v1/cmdb/ire/quality/findings"].(map[string]any)["get"].(map[string]any)
	parameters := findings["parameters"].([]any)
	seen := map[string]bool{}
	for _, raw := range parameters {
		parameter := raw.(map[string]any)
		if parameter["in"] == "query" {
			seen[parameter["name"].(string)] = true
		}
	}
	for _, name := range []string{"class", "owner_id", "rule", "state", "before_id", "limit"} {
		if !seen[name] {
			t.Errorf("quality findings are missing the %q query filter", name)
		}
	}
	responses := findings["responses"].(map[string]any)
	media := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
	page, err := resolveLocalRef(root, media["schema"].(map[string]any)["$ref"].(string))
	if err != nil {
		t.Fatal(err)
	}
	pageSchema := page.(map[string]any)
	items := pageSchema["properties"].(map[string]any)["items"].(map[string]any)
	finding, err := resolveLocalRef(root, items["items"].(map[string]any)["$ref"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := finding.(map[string]any)["properties"].(map[string]any)["class"]; !ok {
		t.Fatal("quality finding schema is missing the CI class")
	}
}

func TestCMDBImportOpenAPIExposesOptionalCSVSource(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"CMDBImportConfig", "CMDBImport"} {
		schema := schemas[name].(map[string]any)
		properties := schema["properties"].(map[string]any)
		source, ok := properties["source_id"].(map[string]any)
		if !ok || source["type"] != "string" {
			t.Errorf("%s must expose the decimal-string source_id", name)
		}
		for _, required := range schema["required"].([]any) {
			if required == "source_id" {
				t.Errorf("%s source_id must remain optional for system CSV imports", name)
			}
		}
	}
}
