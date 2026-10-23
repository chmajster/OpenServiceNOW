package openapi

import (
	"encoding/json"
	"testing"
)

func TestCMDBOpenAPIContainsEveryRegisteredRouteAndWriteGuard(t *testing.T) {
	var doc document
	if err := json.Unmarshal(loadDocument(t), &doc); err != nil {
		t.Fatal(err)
	}
	routes := map[string][]string{
		"/api/v1/cmdb/classes":                                  {"get"},
		"/api/v1/cmdb/lookups/users":                            {"get"},
		"/api/v1/cmdb/lookups/assets/{kind}":                    {"get", "post"},
		"/api/v1/cmdb/cis":                                      {"get", "post"},
		"/api/v1/cmdb/cis/{id}":                                 {"get", "put"},
		"/api/v1/cmdb/cis/{id}/archive":                         {"post"},
		"/api/v1/cmdb/cis/{id}/history":                         {"get"},
		"/api/v1/cmdb/cis/{id}/relations":                       {"get", "post"},
		"/api/v1/cmdb/cis/{id}/relations/{relationID}":           {"delete"},
		"/api/v1/cmdb/cis/{id}/graph":                           {"get"},
		"/api/v1/cmdb/cis/{id}/asset":                            {"get", "put"},
		"/api/v1/cmdb/cis/{id}/records/{kind}":                   {"get"},
		"/api/v1/cmdb/assets":                                   {"get", "post"},
		"/api/v1/cmdb/assets/{id}":                              {"get", "put"},
		"/api/v1/cmdb/assets/{id}/archive":                      {"post"},
		"/api/v1/cmdb/assets/{id}/history":                      {"get"},
		"/api/v1/cmdb/assets/{id}/ci":                           {"get"},
		"/api/v1/cmdb/records/{kind}/{id}/cis":                  {"get"},
		"/api/v1/cmdb/records/{kind}/{id}/cis/{ciID}":           {"put", "delete"},
		"/api/v1/cmdb/imports":                                  {"get", "post"},
		"/api/v1/cmdb/imports/{id}":                             {"get"},
		"/api/v1/cmdb/imports/{id}/rows":                        {"get"},
		"/api/v1/cmdb/imports/{id}/{action}":                    {"post"},
	}
	writes := map[string]bool{
		"post /api/v1/cmdb/lookups/assets/{kind}":               true,
		"post /api/v1/cmdb/cis":                                 true,
		"put /api/v1/cmdb/cis/{id}":                             true,
		"post /api/v1/cmdb/cis/{id}/archive":                    true,
		"post /api/v1/cmdb/cis/{id}/relations":                  true,
		"delete /api/v1/cmdb/cis/{id}/relations/{relationID}":    true,
		"put /api/v1/cmdb/cis/{id}/asset":                       true,
		"post /api/v1/cmdb/assets":                              true,
		"put /api/v1/cmdb/assets/{id}":                          true,
		"post /api/v1/cmdb/assets/{id}/archive":                 true,
		"put /api/v1/cmdb/records/{kind}/{id}/cis/{ciID}":       true,
		"delete /api/v1/cmdb/records/{kind}/{id}/cis/{ciID}":    true,
		"post /api/v1/cmdb/imports":                             true,
		"post /api/v1/cmdb/imports/{id}/{action}":               true,
	}
	for path, methods := range routes {
		for _, method := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				pathItem, ok := doc.Paths[path].(map[string]any)
				if !ok {
					t.Fatalf("OpenAPI route %s %s is missing", method, path)
				}
				operation, ok := pathItem[method].(map[string]any)
				if !ok {
					t.Fatalf("OpenAPI operation %s %s is missing", method, path)
				}
				if operation["operationId"] == "" {
					t.Fatal("operationId is missing")
				}
				if _, ok := operation["responses"].(map[string]any)["200"]; !ok {
					t.Fatal("200 response is missing")
				}
				if writes[method+" "+path] {
					if _, ok := operation["requestBody"].(map[string]any); !ok {
						t.Fatal("write request body is missing")
					}
					if !containsSecurityScheme(operation, "csrfCookie") || !containsSecurityScheme(operation, "csrfHeader") {
						t.Fatal("write route must document double-submit CSRF")
					}
				}
				if !containsSecurityScheme(operation, "sessionCookie") {
					t.Fatal("authenticated session cookie is missing")
				}
			})
		}
	}
}

func TestCMDBOpenAPIDocumentsRBACVersioningAndBigIntStrings(t *testing.T) {
	var doc document
	if err := json.Unmarshal(loadDocument(t), &doc); err != nil {
		t.Fatal(err)
	}
	create := cmdbOperation(t, doc, "/api/v1/cmdb/cis", "post")
	if !containsString(create["x-required-permissions"], "cmdb.manage") || !containsString(create["x-required-permissions"], "cmdb.read") {
		t.Fatal("CI create must require cmdb.manage and cmdb.read")
	}
	list := cmdbOperation(t, doc, "/api/v1/cmdb/cis", "get")
	if !containsString(list["x-any-read-permissions"], "cmdb.read") || !containsString(list["x-any-read-permissions"], "cmdb.read_own") {
		t.Fatal("CI list read-own policy is missing")
	}
	imports := cmdbOperation(t, doc, "/api/v1/cmdb/imports", "post")
	if imports["x-conditional-authorization"] == "" {
		t.Fatal("import target authorization requirements are missing")
	}
	if _, ok := imports["requestBody"].(map[string]any)["content"].(map[string]any)["multipart/form-data"]; !ok {
		t.Fatal("CSV upload is not documented as multipart form data")
	}

	schemas := doc.Components["schemas"].(map[string]any)
	ci := schemas["CMDBCI"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"id", "version", "owner_id"} {
		if ci[field].(map[string]any)["type"] != "string" {
			t.Errorf("CI %s must be serialized as a decimal string", field)
		}
	}
	graph := schemas["CMDBGraph"].(map[string]any)["properties"].(map[string]any)
	if graph["max_nodes"].(map[string]any)["maximum"] != float64(100) || graph["max_edges"].(map[string]any)["maximum"] != float64(200) {
		t.Fatal("graph limits do not match the bounded implementation")
	}
}

func cmdbOperation(t *testing.T, doc document, path, method string) map[string]any {
	t.Helper()
	pathItem, ok := doc.Paths[path].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI path %q is missing", path)
	}
	operation, ok := pathItem[method].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI operation %s %s is missing", method, path)
	}
	return operation
}

func containsSecurityScheme(operation map[string]any, name string) bool {
	security, ok := operation["security"].([]any)
	if !ok {
		return false
	}
	for _, entry := range security {
		if _, exists := entry.(map[string]any)[name]; exists {
			return true
		}
	}
	return false
}

func containsString(value any, expected string) bool {
	values, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range values {
		if item == expected {
			return true
		}
	}
	return false
}
