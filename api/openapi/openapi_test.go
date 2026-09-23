package openapi

import (
	"encoding/json"
	"os"
	"testing"
)

type document struct {
	OpenAPI string                 `json:"openapi"`
	Paths   map[string]any         `json:"paths"`
	Components map[string]any      `json:"components"`
}

func TestOpenAPIContractParsesAndContainsImplementedRoutes(t *testing.T) {
	data, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
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
