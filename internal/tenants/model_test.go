package tenants

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTenantIDSerializesAsStringAndVersionAsNumber(t *testing.T) {
	const largeID uint64 = 9007199254740993
	data, err := json.Marshal(Tenant{ID: largeID, Slug: "default", Name: "Default", Version: 7})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"id":"9007199254740993"`) {
		t.Fatalf("tenant ID is not a JSON string: %s", text)
	}
	if !strings.Contains(text, `"version":7`) {
		t.Fatalf("tenant version must remain numeric: %s", text)
	}
}
