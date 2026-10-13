package catalog

import "github.com/chmajster/OpenServiceNOW/internal/workrecords"

func ReferenceScope(a Actor) (workrecords.ReferenceScope, error) {
	if !a.Has("catalog.read") {
		return workrecords.ReferenceScope{}, workrecords.ErrForbidden
	}
	return workrecords.ReferenceScope{Kind: "catalog_item", Root: "catalog", ID: "i.id", Number: "CAST(i.id AS CHAR)", Title: "JSON_UNQUOTE(JSON_EXTRACT(v.definition,'$.title'))", State: "'PUBLISHED'", Version: "v.version", CreatedAt: "v.published_at", UpdatedAt: "v.published_at", From: " FROM catalog_items i JOIN catalog_item_versions v ON v.tenant_id=i.tenant_id AND v.item_id=i.id AND v.version=i.published_version", Where: " WHERE i.tenant_id=? AND " + AudienceSQL("v"), Args: []any{a.TenantID, a.UserID}}, nil
}
