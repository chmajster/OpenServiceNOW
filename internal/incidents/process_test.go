package incidents

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"strings"
	"testing"
	"time"
)

func TestAssignmentRulesOrderAndAllConditions(t *testing.T) {
	p := metadata.DefaultIncidentProcess()
	p.DefaultGroupID = "99"
	p.AssignmentRules = []metadata.IncidentAssignmentRule{{Key: "first", Active: true, GroupID: "17", ServiceID: "9007199254740993", Category: "network", Location: "Warsaw", Priority: 1, Contains: "VPN", CustomField: "site", CustomValue: json.RawMessage(`"north"`)}, {Key: "second", Active: true, GroupID: "18"}}
	service := uint64(9007199254740993)
	f := Fields{ServiceID: &service, Category: "network", Location: "warsaw", Title: "VPN outage", CustomFields: map[string]json.RawMessage{"site": json.RawMessage(`"north"`)}}
	if result := matchAssignment(p, f, 1); result.RuleKey != "first" || result.GroupID == nil || *result.GroupID != 17 {
		t.Fatalf("first rule: %+v", result)
	}
	if result := matchAssignment(p, f, 2); result.RuleKey != "second" {
		t.Fatalf("priority not enforced: %+v", result)
	}
	p.AssignmentRules = nil
	if result := matchAssignment(p, f, 1); result.RuleKey != "default" || *result.GroupID != 99 {
		t.Fatalf("default: %+v", result)
	}
}

func TestRetiredCategoryOnlyRemainsValidOnItsExistingRecord(t *testing.T) {
	c := metadata.DefaultConfiguration("test")
	c.Dictionaries["incident.category"][0].Retired = true
	f := Fields{Title: "Laptop failed", Description: "Cannot boot", Category: "hardware", Subcategory: "computer", Impact: 2, Urgency: 2}
	if _, err := normalizeFieldsWith(f, incidentCategories(c), c.PriorityMatrix); !errors.Is(err, ErrInvalid) {
		t.Fatalf("new retired category accepted: %v", err)
	}
	if _, err := normalizeFieldsWith(f, historicalCategories(c, Record{Category: "hardware", Subcategory: "computer"}), c.PriorityMatrix); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeFieldsWith(f, historicalCategories(c, Record{Category: "other", Subcategory: "other"}), c.PriorityMatrix); !errors.Is(err, ErrInvalid) {
		t.Fatal("another historical record adopted retired value")
	}
}

func TestReopeningAndClosureFollowPinnedPolicy(t *testing.T) {
	now := time.Now().UTC()
	resolved := now.Add(-2 * time.Hour)
	r := Record{TenantID: 1, ReporterID: 2, State: "RESOLVED", ResolvedAt: &resolved, ProcessPolicy: metadata.DefaultIncidentProcess()}
	r.ProcessPolicy.RequesterCanReopen = true
	r.ProcessPolicy.ReopenHours = 1
	a := Actor{TenantID: 1, UserID: 2, Permissions: map[string]bool{"incident.read_own": true}}
	if canRequesterReopen(a, r, now) {
		t.Fatal("expired reopen window accepted")
	}
	r.ProcessPolicy.ReopenHours = 3
	if !canRequesterReopen(a, r, now) {
		t.Fatal("valid own reopen denied")
	}
	if err := validateProcessTransition(a, r, TransitionInput{State: "CLOSED"}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing resolution accepted: %v", err)
	}
	r.ResolutionCode = "FIXED"
	r.ResolutionDescription = "Replaced faulty part"
	if err := validateProcessTransition(a, r, TransitionInput{State: "CLOSED"}, now); err != nil {
		t.Fatal(err)
	}
	r.ProcessPolicy.Transitions["RESOLVED"] = []string{"IN_PROGRESS"}
	if err := validateProcessTransition(a, r, TransitionInput{State: "CLOSED"}, now); !errors.Is(err, ErrTransition) {
		t.Fatal("disabled transition accepted")
	}
}

func TestMajorIDsAndBridgeContract(t *testing.T) {
	var list IDList
	if err := json.Unmarshal([]byte(`[9007199254740993]`), &list); err == nil {
		t.Fatal("numeric BIGINT array accepted")
	}
	if err := json.Unmarshal([]byte(`["9007199254740993"]`), &list); err != nil || list[0] != 9007199254740993 {
		t.Fatalf("ID lost: %v", err)
	}
	for _, value := range []string{"javascript:alert(1)", "http://example.test", "https://user:secret@example.test", "https://example.test/#secret"} {
		if _, err := validateBridgeURL(value); err == nil {
			t.Fatalf("unsafe bridge %s", value)
		}
	}
	if _, err := validateBridgeURL("https://conference.example.test/meeting/42"); err != nil {
		t.Fatal(err)
	}
	a := Actor{Permissions: map[string]bool{"incident.major.read": true}}
	if !a.Has("major.read") || a.Has("incident.major.read") {
		t.Fatal("incident-relative permission convention changed")
	}
}

func TestMajorDeliveryUsesActualChannelResult(t *testing.T) {
	cases := []struct {
		inApp, queued, delivered, missing, failed, unknown bool
		want                                               string
	}{
		{true, false, false, true, false, false, "IN_APP_DELIVERED"},
		{false, false, false, true, false, false, "SMTP_NOT_CONFIGURED"},
		{false, true, false, false, false, false, "SMTP_QUEUED"},
		{false, true, false, false, false, true, "SMTP_UNKNOWN"},
		{false, false, true, false, false, false, "SMTP_DELIVERED"},
		{false, false, true, false, true, false, "SMTP_PARTIAL"},
		{false, false, false, false, true, false, "FAILED"},
	}
	for _, c := range cases {
		if got := majorDeliveryState("PENDING", c.inApp, c.queued, c.delivered, c.missing, c.failed, c.unknown); got != c.want {
			t.Fatalf("delivery %s want %s", got, c.want)
		}
	}
}

func TestServiceTemplatesRequirePortfolioAccess(t *testing.T) {
	c := metadata.DefaultConfiguration("Test")
	c.IncidentProcess.Templates = []metadata.IncidentTemplate{{Key: "public"}, {Key: "service", ServiceID: "7"}, {Key: "retired", Retired: true}}
	if templates := activeTemplates(c, Actor{}); len(templates) != 1 || templates[0].Key != "public" {
		t.Fatal("offered an unusable service template", templates)
	}
	if templates := activeTemplates(c, Actor{Permissions: map[string]bool{"service_portfolio.read": true}}); len(templates) != 2 {
		t.Fatal("authorized template missing", templates)
	}
}

func TestMajorChildProposalReservesSpaceForPrefix(t *testing.T) {
	_, err := (&Service{}).MajorChildren(context.Background(), Actor{Permissions: map[string]bool{"incident.major.manage": true}}, 1, MajorChildrenInput{Version: 1, Action: "solution", Message: strings.Repeat("x", 20000), Items: []MajorChildTarget{{ID: 2, Version: 1}}}, "oversize-solution-key")
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized prefixed solution reached the repository", err)
	}
}
