package problems

import (
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"testing"
)

func TestKnownErrorAndLifecycleRequirements(t *testing.T) {
	f := Fields{Title: "Błąd połączenia", Description: "Objawy pod obciążeniem", Impact: "HIGH", KnownError: true}
	if _, err := Normalize(f); !errors.Is(err, ErrInvalid) {
		t.Fatal("undocumented Known Error accepted", err)
	}
	f.RootCause = "Wyścig połączeń"
	f.Workaround = "Ograniczenie współbieżności"
	if _, err := Normalize(f); err != nil {
		t.Fatal(err)
	}
	group := uint64(9007199254741001)
	r := Record{Fields: f, State: "ROOT_CAUSE"}
	r.GroupID = &group
	if err := validateTransition(r, Change{State: "FIX_IN_PROGRESS"}); !errors.Is(err, ErrTransition) {
		t.Fatal("missing analysis/plan accepted", err)
	}
	r.Analysis = "Potwierdzono w logach"
	r.SolutionPlan = "Synchronizacja"
	if err := validateTransition(r, Change{State: "FIX_IN_PROGRESS"}); err != nil {
		t.Fatal(err)
	}
	if err := validateTransition(r, Change{State: "RESOLVED"}); !errors.Is(err, ErrTransition) {
		t.Fatal("missing verification accepted", err)
	}
	r.Verification = "Brak utraconych połączeń"
	if err := validateTransition(r, Change{State: "RESOLVED"}); err != nil {
		t.Fatal(err)
	}
	r.State = "CLOSED"
	if err := validateTransition(r, Change{State: "ASSESS"}); !errors.Is(err, ErrTransition) {
		t.Fatal("reopen requires reason", err)
	}
	if err := validateTransition(r, Change{State: "ASSESS", Reason: "Regresja"}); err != nil {
		t.Fatal(err)
	}
}
func TestProblemFilterCannotOverrideAccessOrExecuteSQL(t *testing.T) {
	a := Actor{Permissions: map[string]bool{"problem.read_own": true}}
	a.UserID = 9007199254741001
	a.TenantID = 1
	sql, args, err := listWhere(a, Filter{Query: "%' OR 1=1 --", Expression: &recordlists.Node{Field: "active", Operator: "eq", Value: "true"}})
	if err != nil || len(args) != 6 || sql == "" {
		t.Fatal(sql, args, err)
	}
	if _, _, err := listWhere(a, Filter{Expression: &recordlists.Node{Field: "private_note", Operator: "eq", Value: "secret"}}); err == nil {
		t.Fatal("private field accepted")
	}
	if _, _, err := listWhere(a, Filter{Sort: "p.title;DROP TABLE problems"}); err == nil {
		t.Fatal("SQL sort accepted")
	}
}
