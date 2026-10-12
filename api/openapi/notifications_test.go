package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotificationSecurityAndSecretContract(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(loadDocument(t), &root); err != nil {
		t.Fatal(err)
	}
	for path, methods := range root["paths"].(map[string]any) {
		if !strings.HasPrefix(path, "/api/v1/notifications") && !strings.HasPrefix(path, "/api/v1/admin/notifications") {
			continue
		}
		for method, raw := range methods.(map[string]any) {
			op := raw.(map[string]any)
			sec := op["security"].([]any)[0].(map[string]any)
			if _, ok := sec["sessionCookie"]; !ok {
				t.Fatal(path, "missing session")
			}
			_, csrf := sec["csrfHeader"]
			if csrf != (method != "get") {
				t.Fatal(path, "wrong CSRF contract")
			}
		}
	}
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	input := schemas["NotificationSMTPConfigInput"].(map[string]any)["properties"].(map[string]any)
	if input["password"].(map[string]any)["writeOnly"] != true {
		t.Fatal("SMTP password not write-only")
	}
	output := schemas["NotificationSMTPConfig"].(map[string]any)["properties"].(map[string]any)
	for _, secret := range []string{"password", "password_cipher", "key"} {
		if _, ok := output[secret]; ok {
			t.Fatal("SMTP response exposes secret", secret)
		}
	}
	for schema, fields := range map[string][]string{"Notification": {"id", "incident_id", "source_event_id"}, "NotificationMailDelivery": {"id", "user_id", "notification_id", "version"}, "EventDelivery": {"id", "event_id"}} {
		properties := schemas[schema].(map[string]any)["properties"].(map[string]any)
		for _, field := range fields {
			if properties[field].(map[string]any)["type"] != "string" {
				t.Fatal(schema, field, "is not a decimal string")
			}
		}
	}
}
