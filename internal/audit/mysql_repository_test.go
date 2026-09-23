package audit

import "testing"

func TestMarshalNullableUsesTextJSON(t *testing.T) {
	value, err := marshalNullable(map[string]any{"enabled": true, "count": 2})
	if err != nil {
		t.Fatalf("marshalNullable() error = %v", err)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("marshalNullable() type = %T, want string", value)
	}
	if text != `{"count":2,"enabled":true}` {
		t.Fatalf("marshalNullable() = %q", text)
	}
}

func TestMarshalNullablePreservesNull(t *testing.T) {
	value, err := marshalNullable(nil)
	if err != nil {
		t.Fatalf("marshalNullable(nil) error = %v", err)
	}
	if value != nil {
		t.Fatalf("marshalNullable(nil) = %#v", value)
	}
}
