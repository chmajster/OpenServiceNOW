package incidents

import (
	"encoding/json"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"testing"
)

func TestCollaborationDecimalIDs(t *testing.T) {
	id := uint64(9007199254741999)
	cases := []struct {
		value  any
		fields []string
	}{
		{workrecords.Assignment{AssignedTo: &id, AssignmentGroupID: &id}, []string{"assigned_to", "assignment_group_id"}},
		{workrecords.AssignmentEventPayload{AssignedTo: &id, AssignmentGroupID: &id}, []string{"assigned_to", "assignment_group_id"}},
		{activity.Entry{ActorUserID: &id, SourceEventID: &id}, []string{"actor_user_id", "source_event_id"}},
		{HistoryEntry{ActorUserID: &id, SourceEventID: &id}, []string{"actor_user_id", "source_event_id"}},
		{FileDTO{ID: id, MessageID: &id, UploadedBy: &id}, []string{"id", "message_id", "uploaded_by"}},
		{BatchItem{ID: id, Version: id}, []string{"id", "version"}},
	}
	for _, c := range cases {
		raw, err := json.Marshal(c.value)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range c.fields {
			if !strings.Contains(string(raw), `"`+field+`":"9007199254741999"`) {
				t.Fatalf("%T lost decimal %s: %s", c.value, field, raw)
			}
		}
	}
}
func TestNotesRequireOperatorObjectRead(t *testing.T) {
	for _, tc := range []struct {
		permissions map[string]bool
		allowed     bool
	}{
		{map[string]bool{"incident.read_own": true, "incident.notes.read": true}, false},
		{map[string]bool{"incident.read": true}, false},
		{map[string]bool{"incident.read": true, "incident.notes.read": true}, true},
	} {
		if notesAllowed(Actor{Permissions: tc.permissions}) != tc.allowed {
			t.Fatal(tc)
		}
	}
}
