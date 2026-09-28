package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditIDsSerializeAsStrings(t *testing.T) {
	const largeID uint64 = 9007199254740993
	actorID := largeID
	data, err := json.Marshal(Page{
		Events:       []Record{{ID: largeID, ActorUserID: &actorID, Action: "test", ObjectType: "test", Source: "test"}},
		NextBeforeID: largeID,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(data)
	for _, expected := range []string{
		`"id":"9007199254740993"`,
		`"actor_user_id":"9007199254740993"`,
		`"next_before_id":"9007199254740993"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
}
