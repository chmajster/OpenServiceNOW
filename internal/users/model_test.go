package users

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserAndCursorIDsSerializeAsStrings(t *testing.T) {
	const largeID uint64 = 9007199254740993
	data, err := json.Marshal(Page{
		Users: []User{{ID: largeID, Email: "user@example.test", DisplayName: "User"}},
		NextAfterID: largeID,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"id":"9007199254740993"`) {
		t.Fatalf("user ID is not a JSON string: %s", text)
	}
	if !strings.Contains(text, `"next_after_id":"9007199254740993"`) {
		t.Fatalf("cursor ID is not a JSON string: %s", text)
	}
}
