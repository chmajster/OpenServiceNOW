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
		"/api/v1/setup",
		"/api/v1/auth/login",
		"/api/v1/auth/me",
		"/api/v1/auth/logout",
		"/api/v1/auth/profile",
		"/api/v1/auth/password",
		"/api/v1/auth/sessions",
		"/api/v1/auth/sessions/{sessionID}",
		"/api/v1/admin/tenant/",
		"/api/v1/admin/users/",
		"/api/v1/admin/users/memberships",
		"/api/v1/admin/users/{userID}/membership",
		"/api/v1/admin/rbac/permissions",
		"/api/v1/admin/rbac/roles",
		"/api/v1/admin/rbac/roles/{roleID}",
		"/api/v1/admin/rbac/roles/{roleID}/permissions",
		"/api/v1/admin/rbac/groups",
		"/api/v1/admin/rbac/groups/{groupID}",
		"/api/v1/admin/rbac/groups/{groupID}/members",
		"/api/v1/admin/rbac/groups/{groupID}/members/{userID}",
		"/api/v1/admin/rbac/users/{userID}/roles",
		"/api/v1/admin/rbac/users/{userID}/roles/{roleID}",
		"/api/v1/admin/rbac/groups/{groupID}/roles",
		"/api/v1/admin/rbac/groups/{groupID}/roles/{roleID}",
		"/api/v1/admin/audit/",
		"/api/v1/incidents/",
		"/api/v1/incidents/metadata",
		"/api/v1/incidents/lookups/{kind}",
		"/api/v1/incidents/{incidentID}",
		"/api/v1/incidents/{incidentID}/assignment",
		"/api/v1/incidents/{incidentID}/transition",
		"/api/v1/incidents/{incidentID}/priority",
		"/api/v1/incidents/{incidentID}/history",
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

func TestOwnAccountSecurityContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path, method string
		csrf         bool
	}{
		{"/api/v1/auth/profile", "put", true}, {"/api/v1/auth/password", "post", true},
		{"/api/v1/auth/sessions", "get", false}, {"/api/v1/auth/sessions", "delete", true}, {"/api/v1/auth/sessions/{sessionID}", "delete", true},
	} {
		operation := root["paths"].(map[string]any)[item.path].(map[string]any)[item.method].(map[string]any)
		security := operation["security"].([]any)[0].(map[string]any)
		if _, ok := security["sessionCookie"]; !ok {
			t.Fatalf("%s missing session authentication", item.path)
		}
		_, hasCSRF := security["csrfHeader"]
		if hasCSRF != item.csrf {
			t.Fatalf("%s %s CSRF=%t", item.method, item.path, hasCSRF)
		}
	}
	for _, field := range []string{"current_password", "new_password"} {
		value, err := resolveLocalRef(root, "#/components/schemas/AccountPasswordRequest/properties/"+field)
		if err != nil || value.(map[string]any)["writeOnly"] != true {
			t.Fatal("password contract is not write-only")
		}
	}
}

func TestIncidentSecurityAndConcurrencyContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path, method, request string
		write                 bool
	}{
		{"/api/v1/incidents/", "get", "", false},
		{"/api/v1/incidents/", "post", "IncidentFields", true},
		{"/api/v1/incidents/metadata", "get", "", false},
		{"/api/v1/incidents/lookups/{kind}", "get", "", false},
		{"/api/v1/incidents/{incidentID}", "get", "", false},
		{"/api/v1/incidents/{incidentID}", "put", "IncidentUpdate", true},
		{"/api/v1/incidents/{incidentID}/assignment", "put", "IncidentAssign", true},
		{"/api/v1/incidents/{incidentID}/transition", "post", "IncidentTransition", true},
		{"/api/v1/incidents/{incidentID}/priority", "put", "IncidentPriority", true},
		{"/api/v1/incidents/{incidentID}/history", "get", "", false},
	} {
		operation := root["paths"].(map[string]any)[item.path].(map[string]any)[item.method].(map[string]any)
		security := operation["security"].([]any)[0].(map[string]any)
		if _, ok := security["sessionCookie"]; !ok {
			t.Fatalf("%s lacks session security", item.path)
		}
		_, csrf := security["csrfHeader"]
		if csrf != item.write {
			t.Fatalf("%s CSRF=%t", item.path, csrf)
		}
		if reads, ok := operation["x-any-read-permissions"].([]any); !ok || len(reads) != 2 {
			t.Fatalf("%s lacks object read contract", item.path)
		}
		if item.write && item.request != "IncidentFields" {
			value, err := resolveLocalRef(root, "#/components/schemas/"+item.request+"/properties/version")
			if err != nil || value.(map[string]any)["type"] != "string" {
				t.Fatalf("%s missing incident version", item.path)
			}
		}
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

func TestOpenAPIBigIntValuesUseDecimalStrings(t *testing.T) {
	raw := loadDocument(t)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}

	for _, ref := range []string{
		"#/components/parameters/UserID/schema",
		"#/components/parameters/RoleID/schema",
		"#/components/parameters/GroupID/schema",
		"#/components/schemas/AuthUser/properties/id",
		"#/components/schemas/AuthUser/properties/version",
		"#/components/schemas/AuthUser/properties/credential_version",
		"#/components/schemas/AccountSession/properties/id",
		"#/components/schemas/AccountSession/properties/tenant_id",
		"#/components/schemas/AccountSessions/properties/next_after_id",
		"#/components/schemas/AuthTenant/properties/id",
		"#/components/schemas/Tenant/properties/id",
		"#/components/schemas/Tenant/properties/version",
		"#/components/schemas/User/properties/id",
		"#/components/schemas/UserListResponse/properties/next_after_id",
		"#/components/schemas/Role/properties/id",
		"#/components/schemas/Role/properties/version",
		"#/components/schemas/RoleListResponse/properties/next_after_id",
		"#/components/schemas/Group/properties/id",
		"#/components/schemas/Group/properties/version",
		"#/components/schemas/GroupListResponse/properties/next_after_id",
		"#/components/schemas/GroupMember/properties/user_id",
		"#/components/schemas/GroupMemberPage/properties/next_after_id",
		"#/components/schemas/AssignedRole/properties/id",
		"#/components/schemas/AssignedRolePage/properties/next_after_id",
		"#/components/schemas/AuditRecord/properties/id",
		"#/components/schemas/AuditPage/properties/next_before_id",
		"#/components/schemas/Incident/properties/id",
		"#/components/schemas/Incident/properties/tenant_id",
		"#/components/schemas/Incident/properties/reporter_id",
		"#/components/schemas/Incident/properties/affected_user_id",
		"#/components/schemas/Incident/properties/version",
		"#/components/schemas/IncidentAssignment/properties/assigned_to",
		"#/components/schemas/IncidentAssignment/properties/assignment_group_id",
		"#/components/schemas/IncidentAssignment/properties/version",
		"#/components/schemas/IncidentPage/properties/next_after_id",
		"#/components/schemas/IncidentPage/properties/next_after_priority",
		"#/components/schemas/IncidentHistory/properties/next_before_event_id",
		"#/components/schemas/IncidentChange/properties/actor_user_id",
		"#/components/schemas/IncidentChange/properties/incident_version",
	} {
		value, err := resolveLocalRef(root, ref)
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		schema, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object", ref)
		}
		if schema["type"] != "string" {
			t.Errorf("%s type = %#v, want string", ref, schema["type"])
		}
		pattern, ok := schema["pattern"].(string)
		if !ok || pattern == "" {
			t.Errorf("%s is missing decimal-string pattern", ref)
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

func TestOpenAPIAdminOperationsDeclarePermissions(t *testing.T) {
	raw := loadDocument(t)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}

	paths, ok := root["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths object is missing")
	}
	for path, rawItem := range paths {
		if !strings.HasPrefix(path, "/api/v1/admin/") {
			continue
		}
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
			required, ok := operation["x-required-permissions"].([]any)
			if !ok || len(required) == 0 {
				t.Errorf("%s %s is missing x-required-permissions", method, path)
			}
		}
	}
}

func TestOpenAPIUserBearingRBACWritesRequireUserAdministration(t *testing.T) {
	raw := loadDocument(t)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}

	paths, ok := root["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths object is missing")
	}
	tests := []struct {
		path   string
		method string
	}{
		{path: "/api/v1/admin/rbac/groups/{groupID}/members/{userID}", method: "post"},
		{path: "/api/v1/admin/rbac/groups/{groupID}/members/{userID}", method: "delete"},
		{path: "/api/v1/admin/rbac/users/{userID}/roles/{roleID}", method: "post"},
		{path: "/api/v1/admin/rbac/users/{userID}/roles/{roleID}", method: "delete"},
	}

	for _, test := range tests {
		rawItem, ok := paths[test.path].(map[string]any)
		if !ok {
			t.Fatalf("path %s is missing", test.path)
		}
		operation, ok := rawItem[test.method].(map[string]any)
		if !ok {
			t.Fatalf("%s %s operation is missing", test.method, test.path)
		}
		required, ok := operation["x-required-permissions"].([]any)
		if !ok {
			t.Fatalf("%s %s permission metadata is missing", test.method, test.path)
		}
		seen := make(map[string]bool, len(required))
		for _, rawPermission := range required {
			permission, ok := rawPermission.(string)
			if ok {
				seen[permission] = true
			}
		}
		for _, permission := range []string{"admin.rbac.manage", "admin.users.manage"} {
			if !seen[permission] {
				t.Errorf("%s %s is missing required permission %s", test.method, test.path, permission)
			}
		}
	}
}
