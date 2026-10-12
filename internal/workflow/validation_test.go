package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func testConfig() Config {
	return Config{Name: "Access", ExecutorID: 1, GroupID: 2, SelfApproval: "DENY", Nodes: []Node{
		{ID: "start", Label: "Start", Type: "START", Next: "approval"},
		{ID: "approval", Label: "Approval", Type: "APPROVAL", Next: "task", Rejected: "cancel", Timeout: "cancel", TargetType: "USER", TargetID: 3, Policy: "ANY", Seconds: 60},
		{ID: "task", Label: "Task", Type: "CREATE_TASK", Next: "wait", Title: "Provision"},
		{ID: "wait", Label: "Wait", Type: "WAIT_TASK", Next: "complete", Timeout: "cancel", TaskRef: "task", Seconds: 120},
		{ID: "complete", Label: "Complete", Type: "UPDATE_RECORD", State: "FULFILLED", Reason: "Delivered", Next: "end"},
		{ID: "cancel", Label: "Cancel", Type: "UPDATE_RECORD", State: "CANCELLED", Reason: "Rejected or timeout", Next: "end"},
		{ID: "end", Label: "End", Type: "END"},
	}}
}
func TestPublishGraph(t *testing.T) {
	if err := Validate(testConfig(), true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"cycle", func(c *Config) { c.Nodes[4].Next = "task" }},
		{"unknown", func(c *Config) { c.Nodes[0].Next = "missing" }},
		{"task bypass", func(c *Config) { c.Nodes[1].Rejected = "task" }},
		{"timeout bypass", func(c *Config) { c.Nodes[1].Timeout = "task" }},
		{"missing wait", func(c *Config) { c.Nodes[2].Next = "complete" }},
		{"invalid task reference", func(c *Config) { c.Nodes[3].TaskRef = "start" }},
		{"self approval unspecified", func(c *Config) { c.SelfApproval = "" }},
		{"unsupported script", func(c *Config) { c.Nodes[4].Type = "SCRIPT" }},
		{"missing approver", func(c *Config) { c.Nodes[1].TargetID = 0 }},
		{"invalid policy", func(c *Config) { c.Nodes[1].Policy = "QUORUM" }},
		{"missing executor", func(c *Config) { c.ExecutorID = 0 }},
		{"negative timer", func(c *Config) { c.Nodes[1].Seconds = -1 }},
		{"empty reason", func(c *Config) { c.Nodes[4].Reason = "" }},
		{"arbitrary field update", func(c *Config) { c.Nodes[4].State = "ADMIN" }},
		{"close without fulfilled", func(c *Config) { c.Nodes[4].State = "CLOSED" }},
		{"approval after fulfillment", func(c *Config) {
			c.Nodes[4].Next = "lateapproval"
			late := c.Nodes[1]
			late.ID = "lateapproval"
			late.Next = "end"
			c.Nodes = append(c.Nodes, late)
		}},
		{"orphan node", func(c *Config) { c.Nodes = append(c.Nodes, Node{ID: "orphan", Label: "Orphan", Type: "END"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			tc.change(&c)
			if Validate(c, true) == nil {
				t.Fatal("invalid publication accepted")
			}
		})
	}
}
func TestBranchAndTimerGraph(t *testing.T) {
	c := testConfig()
	c.Nodes[0].Next = "condition"
	c.Nodes = append(c.Nodes, Node{ID: "condition", Label: "Conditional", Type: "CONDITION", Field: "needs_access", Equals: json.RawMessage(`true`), Next: "timer", Otherwise: "cancel"}, Node{ID: "timer", Label: "Timer", Type: "WAIT_TIMER", Seconds: 60, Next: "approval"})
	if err := Validate(c, true); err != nil {
		t.Fatal(err)
	}
}
func TestDecimalWorkflowDTO(t *testing.T) {
	v := uint64(9007199254740993)
	raw, err := json.Marshal(Execution{ID: v, DefinitionVersion: v, Steps: []Step{{Version: v, Decisions: []Decision{{UserID: v}}}}, Attempts: []Attempt{{ID: v, Version: v}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"9007199254740993"`) < 5 {
		t.Fatal(string(raw))
	}
}
func TestDraftMayBeIncomplete(t *testing.T) {
	c := testConfig()
	c.ExecutorID = 0
	c.Nodes[1].Next = ""
	if err := Validate(c, false); err != nil {
		t.Fatal(err)
	}
	if Validate(c, true) == nil {
		t.Fatal("incomplete draft published")
	}
}
