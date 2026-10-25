package incidents

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPriorityMatrix(t *testing.T) {
	expected := [3][3]int{{1, 2, 3}, {2, 3, 4}, {3, 4, 5}}
	for impact := 1; impact <= 3; impact++ {
		for urgency := 1; urgency <= 3; urgency++ {
			got, err := CalculatePriority(impact, urgency)
			if err != nil || got != expected[impact-1][urgency-1] {
				t.Fatalf("matrix[%d,%d]=%d err=%v", impact, urgency, got, err)
			}
		}
	}
	for _, input := range [][2]int{{0, 1}, {1, 4}, {-1, 2}} {
		if _, err := CalculatePriority(input[0], input[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid level accepted")
		}
	}
}
func TestLifecycleRules(t *testing.T) {
	user := uint64(42)
	tests := []struct {
		state    string
		input    TransitionInput
		assigned bool
		want     error
	}{
		{"NEW", TransitionInput{State: "CLOSED"}, true, ErrTransition},
		{"NEW", TransitionInput{State: "IN_PROGRESS"}, false, ErrInvalid},
		{"NEW", TransitionInput{State: "IN_PROGRESS"}, true, nil},
		{"IN_PROGRESS", TransitionInput{State: "ON_HOLD"}, true, ErrInvalid},
		{"IN_PROGRESS", TransitionInput{State: "ON_HOLD", HoldReason: "AWAITING_USER"}, true, nil},
		{"ON_HOLD", TransitionInput{State: "RESOLVED", ResolutionCode: "FIXED"}, true, ErrInvalid},
		{"ON_HOLD", TransitionInput{State: "RESOLVED", ResolutionCode: "BAD", ResolutionDescription: "Opis"}, true, ErrInvalid},
		{"ON_HOLD", TransitionInput{State: "RESOLVED", ResolutionCode: "FIXED", ResolutionDescription: "Naprawiono"}, true, nil},
		{"RESOLVED", TransitionInput{State: "CLOSED"}, true, nil},
		{"CLOSED", TransitionInput{State: "IN_PROGRESS"}, true, ErrInvalid},
		{"CLOSED", TransitionInput{State: "IN_PROGRESS", Reason: "Problem wystąpił ponownie"}, true, nil},
		{"CANCELLED", TransitionInput{State: "NEW"}, true, ErrTransition},
		{"NEW", TransitionInput{State: "CANCELLED"}, false, ErrInvalid},
		{"NEW", TransitionInput{State: "CANCELLED", Reason: "Błędne zgłoszenie"}, false, nil},
		{"NEW", TransitionInput{State: "IN_PROGRESS", ResolutionCode: "FIXED"}, true, ErrInvalid},
	}
	for _, tc := range tests {
		r := Record{State: tc.state}
		if tc.assigned {
			r.Assignment.AssignedTo = &user
		}
		err := validateTransition(r, tc.input)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%s→%s: %v want %v", tc.state, tc.input.State, err, tc.want)
		}
	}
}
func TestOwnReadAndActions(t *testing.T) {
	a := Actor{TenantID: 1, UserID: 2, Permissions: map[string]bool{"incident.read_own": true, "incident.update": true}}
	r := Record{TenantID: 1, ReporterID: 2, AffectedUserID: 3, State: "NEW"}
	if !readable(a, r) || !allowedActions(a, r).Edit {
		t.Fatal("own read/update denied")
	}
	if allowedActions(a, r).Assign || allowedActions(a, r).OverridePriority {
		t.Fatal("missing permissions were granted")
	}
	r.ReporterID = 4
	if readable(a, r) {
		t.Fatal("foreign report readable")
	}
	r.AffectedUserID = 2
	if !readable(a, r) {
		t.Fatal("affected person denied")
	}
	r.TenantID = 8
	if readable(a, r) {
		t.Fatal("tenant boundary bypassed")
	}
	a.Permissions["incident.read"] = true
	r.TenantID = 1
	r.State = "CLOSED"
	if allowedActions(a, r).Edit {
		t.Fatal("closed edit allowed")
	}
}
func TestFieldsAndCursorValidation(t *testing.T) {
	input := Fields{Title: "  Awaria  ", Description: "Opis", AffectedUserID: 1, Category: "network", Subcategory: "vpn", Impact: 2, Urgency: 3}
	normalized, err := normalizeFields(input)
	if err != nil || normalized.Title != "Awaria" {
		t.Fatal(err)
	}
	input.Subcategory = "computer"
	if _, err := normalizeFields(input); !errors.Is(err, ErrInvalid) {
		t.Fatal("mismatched category accepted")
	}
	input.Subcategory = "vpn"
	input.Description = strings.Repeat("😀", 20001)
	if _, err := normalizeFields(input); !errors.Is(err, ErrInvalid) {
		t.Fatal("description bound bypassed")
	}
	f := Filter{View: "all", Sort: "priority", Limit: 20, AfterID: 1}
	if validateFilter(f) == nil {
		t.Fatal("incomplete priority cursor accepted")
	}
	f.AfterPriority = 3
	if err := validateFilter(f); err != nil {
		t.Fatal(err)
	}
	f.From = time.Now()
	f.To = f.From.Add(-time.Hour)
	if validateFilter(f) == nil {
		t.Fatal("reverse dates accepted")
	}
}
func TestPublicBigIntsAndEventSnapshot(t *testing.T) {
	id := uint64(9007199254740993)
	r := Record{ID: id, TenantID: id, ReporterID: id, AffectedUserID: id, Version: id}
	r.Assignment.AssignedTo = &id
	r.Assignment.AssignmentGroupID = &id
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "tenant_id", "reporter_id", "affected_user_id", "version", "assigned_to", "assignment_group_id"} {
		if !strings.Contains(string(raw), `"`+field+`":"9007199254740993"`) {
			t.Fatalf("numeric %s: %s", field, raw)
		}
	}
	raw, err = json.Marshal(ChangeEvent{ActorUserID: id, IncidentVersion: id, After: snapshot(r)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"incident_version":"9007199254740993"`) || !strings.Contains(string(raw), `"started_at":null`) {
		t.Fatal(string(raw))
	}
}
