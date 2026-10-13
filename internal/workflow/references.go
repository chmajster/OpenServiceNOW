package workflow

import "github.com/chmajster/OpenServiceNOW/internal/workrecords"

func ReferenceScope(a Actor) (workrecords.ReferenceScope, error) {
	p, args := AccessSQL(a, "x")
	return workrecords.ReferenceScope{Kind: "workflow_execution", Root: "approvals", ID: "x.id", Number: "CAST(x.id AS CHAR)", Title: "JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.name'))", State: "x.state", Version: "x.version", CreatedAt: "x.created_at", UpdatedAt: "x.updated_at", From: " FROM workflow_executions x JOIN workflow_versions v ON v.tenant_id=x.tenant_id AND v.definition_id=x.definition_id AND v.version=x.definition_version", Where: " WHERE x.tenant_id=? AND " + p, Args: append([]any{a.TenantID}, args...)}, nil
}
