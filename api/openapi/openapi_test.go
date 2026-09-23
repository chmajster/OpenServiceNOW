package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type document struct {
	OpenAPI    string         `json:"openapi"`
	Paths      map[string]any `json:"paths"`
	Components map[string]any `json:"components"`
}

func TestOpenAPIContractParsesAndContainsImplementedRoutes(t *testing.T) {
	raw := loadDocument(t)

	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi = %q", doc.OpenAPI)
	}

	requiredPaths := []string{
		"/health/live",
		"/health/ready",
		"/api/v1/auth/login",
		"/api/v1/auth/me",
		"/api/v1/auth/logout",
		"/api/v1/admin/tenant/",
		"/api/v1/admin/users/",
		"/api/v1/admin/users/{userID}/membership",
		"/api/v1/admin/rbac/permissions",
		"/api/v1/admin/rbac/roles",
		"/api/v1/admin/rbac/roles/{roleID}/permissions",
		"/api/v1/admin/rbac/groups",
		"/api/v1/admin/rbac/groups/{groupID}/members/{userID}",
		"/api/v1/admin/rbac/users/{userID}/roles/{roleID}",
		"/api/v1/admin/rbac/groups/{groupID}/roles/{roleID}",
		"/api/v1/admin/audit/",
	}
	for _, path := range requiredPaths {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("OpenAPI path %q is missing", path)
		}
	}
	if len(doc.Components) == 0 {
		t.Fatal("OpenAPI components are missing")
	}
}

func TestOpenAPILocalReferencesResolve(t *testing.T) {
	raw := loadDocument(t)
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}

	var references []string
	walkJSON(root, func(value map[string]any) {
		ref, ok := value["$ref"].(string)
		if ok && strings.HasPrefix(ref, "#/") {
			references = append(references, ref)
		}
	})

	for _, ref := range references {
		if _, err := resolveLocalRef(root, ref); err != nil {
			t.Errorf("%s: %v", ref, err)
		}
	}
	if len(references) == 0 {
		t.Fatal("expected local OpenAPI references")
	}
}

func TestOpenAPIOperationIDsAreUnique(t *testing.T) {
	raw := loadDocument(t)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}

	paths, ok := root["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths object is missing")
	}

	seen := make(map[string]string)
	for path, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOperation := range item {
			if !isHTTPMethod(method) {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Errorf("%s %s operation is not an object", method, path)
				continue
			}
			operationID, _ := operation["operationId"].(string)
			if operationID == "" {
				t.Errorf("%s %s is missing operationId", method, path)
				continue
			}
			if previous, exists := seen[operationID]; exists {
				t.Errorf("operationId %q is duplicated by %s and %s %s", operationID, previous, method, path)
				continue
			}
			seen[operationID] = method + " " + path
		}
	}
}

func loadDocument(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}
	return data
}

func walkJSON(value any, visit func(map[string]any)) {
	switch current := value.(type) {
	case map[string]any:
		visit(current)
		for _, child := range current {
			walkJSON(child, visit)
		}
	case []any:
		for _, child := range current {
			walkJSON(child, visit)
		}
	}
}

func resolveLocalRef(root any, ref string) (any, error) {
	current := root
	for _, escapedPart := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(escapedPart, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("parent of %q is not an object", part)
		}
		next, exists := object[part]
		if !exists {
			return nil, fmt.Errorf("component %q does not exist", part)
		}
		current = next
	}
	return current, nil
}

func isHTTPMethod(value string) bool {
	switch strings.ToLower(value) {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	default:
		return false
	}
}
