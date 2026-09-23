package rbac

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRBACIDsSerializeAsStrings(t *testing.T) {
	const largeID uint64 = 9007199254740993
	data, err := json.Marshal(RolePage{
		Roles: []Role{{
			ID: largeID,
			Name: "operator",
			Permissions: []Permission{{ID: largeID, Key: "tenant.read"}},
		}},
		NextAfterID: largeID,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(data)
	if strings.Count(text, `"9007199254740993"`) < 3 {
		t.Fatalf("RBAC BIGINT IDs are not consistently string encoded: %s", text)
	}
}
