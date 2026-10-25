package metadata

import "testing"

func TestIncidentProcessConfigurationLimits(t *testing.T) {
	c := DefaultConfiguration("Acme")
	if err := ValidateConfiguration(c); err != nil {
		t.Fatal(err)
	}
	c.IncidentProcess.Transitions["NEW"] = []string{"CLOSED"}
	if err := ValidateConfiguration(c); err == nil {
		t.Fatal("domain bypass published")
	}
	c = DefaultConfiguration("Acme")
	c.IncidentProcess.AutoCloseHours = 8761
	if err := ValidateConfiguration(c); err == nil {
		t.Fatal("unbounded automatic closure accepted")
	}
	c = DefaultConfiguration("Acme")
	c.IncidentProcess.DefaultGroupID = "090"
	if err := ValidateConfiguration(c); err == nil {
		t.Fatal("noncanonical group ID accepted")
	}
}

func TestIncidentTemplatePublishedVersionIsImmutable(t *testing.T) {
	old := DefaultConfiguration("Acme")
	old.IncidentProcess.Templates = []IncidentTemplate{{Key: "desktop", Version: 1, Title: "Desktop failed"}}
	next := DefaultConfiguration("Acme")
	next.IncidentProcess.Templates = []IncidentTemplate{{Key: "desktop", Version: 1, Title: "Different title"}}
	if err := validateTemplateVersions(old, next); err == nil {
		t.Fatal("changed immutable template accepted")
	}
	next.IncidentProcess.Templates[0].Version = 2
	if err := validateTemplateVersions(old, next); err != nil {
		t.Fatal(err)
	}
	next.IncidentProcess.Templates[0] = old.IncidentProcess.Templates[0]
	next.IncidentProcess.Templates[0].Retired = true
	if err := validateTemplateVersions(old, next); err != nil {
		t.Fatal("retirement should preserve version", err)
	}
}

func TestIncidentDictionariesRequireActiveChoices(t *testing.T) {
	for _, key := range []string{"incident.category", "incident.resolution_code", "incident.hold_reason", "incident.subcategory.network", "incident.state"} {
		t.Run(key, func(t *testing.T) {
			c := DefaultConfiguration("Test")
			for i := range c.Dictionaries[key] {
				c.Dictionaries[key][i].Retired = true
			}
			if ValidateConfiguration(c) == nil {
				t.Fatal("unusable process accepted")
			}
		})
	}
}
