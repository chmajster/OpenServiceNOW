package requests

import (
	"encoding/json"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"testing"
)

func TestExactOptionalIDs(t *testing.T) {
	id := uint64(9007199254741999)
	for _, v := range []any{catalog.Item{ID: id, Version: id, PublishedVersion: &id}, Task{ID: id, Assignment: workrecords.Assignment{AssignedTo: &id, AssignmentGroupID: &id, Version: id}}, FileDTO{MessageID: &id, UploadedBy: &id}} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"9007199254741999"`) || strings.Contains(string(raw), `:9007199254741999`) {
			t.Fatal("unsafe decimal identifier", string(raw))
		}
	}
}
