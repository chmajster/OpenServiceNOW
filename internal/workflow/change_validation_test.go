package workflow

import "testing"

func changeApproval() Config {
	return Config{Domain: "CHANGE", ChangeType: "NORMAL", Name: "CAB", ExecutorID: 1, GroupID: 1, SelfApproval: "DENY", Nodes: []Node{
		{ID: "start", Label: "Start", Type: "START", Next: "cab"},
		{ID: "cab", Label: "Ocena", Type: "APPROVAL", TargetType: "GROUP", TargetID: 2, Policy: "ALL", Seconds: 3600, Next: "authorize", Rejected: "reject", Timeout: "reject"},
		{ID: "authorize", Label: "Autoryzacja", Type: "UPDATE_RECORD", State: "AUTHORIZED", Reason: "CAB zatwierdził zakres", Next: "end"},
		{ID: "reject", Label: "Odrzucenie", Type: "UPDATE_RECORD", State: "REJECTED", Reason: "Wymaga uzupełnienia", Next: "end"},
		{ID: "end", Label: "Koniec", Type: "END"},
	}}
}
func TestChangeApprovalCannotBypassDomainOrDecision(t *testing.T) {
	c := changeApproval()
	if err := Validate(c, true); err != nil {
		t.Fatal(err)
	}
	for name, alter := range map[string]func(*Config){
		"self approval":          func(c *Config) { c.SelfApproval = "ALLOW" },
		"request domain":         func(c *Config) { c.Domain = "REQUEST" },
		"unapproved state":       func(c *Config) { c.Nodes[1].Rejected = "authorize" },
		"approval without stamp": func(c *Config) { c.Nodes[1].Next = "end" },
		"fabricated fulfillment": func(c *Config) { c.Nodes[2].Type = "CREATE_TASK" },
		"unknown condition": func(c *Config) {
			c.Nodes[0].Type = "CONDITION"
			c.Nodes[0].Field = "sql"
			c.Nodes[0].Equals = []byte(`"HIGH"`)
			c.Nodes[0].Otherwise = "cab"
		},
		"emergency deadline": func(c *Config) { c.ChangeType = "EMERGENCY"; c.Nodes[1].Seconds = 86401 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := changeApproval()
			alter(&bad)
			if Validate(bad, true) == nil {
				t.Fatal("unsafe Change definition accepted")
			}
		})
	}
	c.ChangeType = "EMERGENCY"
	if err := Validate(c, true); err != nil {
		t.Fatal(err)
	}
	c.ChangeType = "STANDARD"
	if err := Validate(c, true); err != nil {
		t.Fatal(err)
	}
}
