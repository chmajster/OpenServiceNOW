package serviceportfolio

import "testing"

func TestNormalizeItemRequiresExplicitRedundancyThreshold(t *testing.T) {
	base := ItemInput{ServiceKey: "  crm.prod ", Name: " CRM ", Type: "business_service", Environment: "prod"}
	got, err := normalizeItem(base)
	if err != nil {
		t.Fatalf("normalize item: %v", err)
	}
	if got.ServiceKey != "CRM.PROD" || got.Name != "CRM" || got.DependencyMode != "NOT_MODELED" {
		t.Fatalf("normalized item = %#v", got)
	}

	base.DependencyMode = "QUORUM"
	if _, err = normalizeItem(base); err == nil {
		t.Fatal("QUORUM without a threshold should be rejected")
	}
	base.DependencyQuorum = 2
	if _, err = normalizeItem(base); err != nil {
		t.Fatalf("QUORUM with a positive threshold should be accepted: %v", err)
	}
	base.DependencyMode = "ALL"
	if _, err = normalizeItem(base); err == nil {
		t.Fatal("non-QUORUM mode with a threshold should be rejected")
	}
}

func TestNormalizeOfferAudienceIDsAreCanonicalAndUnique(t *testing.T) {
	base := OfferInput{OfferKey: "vpn.standard", ParentServiceID: 1, Name: "VPN", SupportModel: "STANDARD"}
	base.AudienceGroupIDs = []string{"3", "4"}
	if _, err := normalizeOffer(base); err != nil {
		t.Fatalf("valid audience IDs rejected: %v", err)
	}
	for _, ids := range [][]string{{"03"}, {"0"}, {"1", "1"}, {"+1"}, {"18446744073709551616"}} {
		base.AudienceGroupIDs = ids
		if _, err := normalizeOffer(base); err == nil {
			t.Errorf("audience IDs %v should be rejected", ids)
		}
	}
}

func TestRelationAllowedUsesTypedCSDMRelationships(t *testing.T) {
	cases := []struct {
		source, relation, target, ci string
		want                         bool
	}{
		{"SERVICE_INSTANCE", "REALIZES", "BUSINESS_APPLICATION", "", true},
		{"BUSINESS_SERVICE", "REALIZES", "BUSINESS_APPLICATION", "", false},
		{"BUSINESS_SERVICE", "DEPENDS_ON", "TECHNICAL_SERVICE", "", true},
		{"BUSINESS_APPLICATION", "DEPENDS_ON", "TECHNICAL_SERVICE", "", false},
		{"TECHNICAL_SERVICE", "MAPS_TO_CI", "", "PHYSICAL_SERVER", true},
		{"BUSINESS_SERVICE", "MAPS_TO_CI", "", "", false},
	}
	for _, test := range cases {
		if got := relationAllowed(test.source, test.relation, test.target, test.ci); got != test.want {
			t.Errorf("relationAllowed(%q,%q,%q,%q) = %v, want %v", test.source, test.relation, test.target, test.ci, got, test.want)
		}
	}
}

func TestDependencyAvailabilityRedundancyRules(t *testing.T) {
	cases := []struct {
		mode   string
		quorum uint16
		states []string
		state  string
		rule   string
	}{
		{"NOT_MODELED", 0, []string{"AVAILABLE"}, "NOT_MODELED", "no_redundancy_model"},
		{"ALL", 0, []string{"AVAILABLE", "DEGRADED"}, "DEGRADED", "all_dependencies_required"},
		{"ALL", 0, []string{"AVAILABLE", "UNKNOWN"}, "UNKNOWN", "all_dependencies_required"},
		{"ALL", 0, []string{"AVAILABLE", "UNAVAILABLE"}, "UNAVAILABLE", "all_dependencies_required"},
		{"ANY", 0, []string{"AVAILABLE", "UNAVAILABLE"}, "DEGRADED", "any_dependency_sufficient"},
		{"ANY", 0, []string{"UNAVAILABLE", "UNAVAILABLE"}, "UNAVAILABLE", "any_dependency_sufficient"},
		{"QUORUM", 2, []string{"AVAILABLE", "DEGRADED", "UNAVAILABLE"}, "DEGRADED", "quorum_dependency_count"},
		{"QUORUM", 2, []string{"AVAILABLE", "UNKNOWN"}, "UNKNOWN", "quorum_dependency_count"},
		{"QUORUM", 2, []string{"UNAVAILABLE", "UNAVAILABLE"}, "UNAVAILABLE", "quorum_dependency_count"},
	}
	for _, test := range cases {
		state, rule := dependencyAvailability(test.mode, test.quorum, test.states)
		if state != test.state || rule != test.rule {
			t.Errorf("dependencyAvailability(%s,%v) = %s/%s, want %s/%s", test.mode, test.states, state, rule, test.state, test.rule)
		}
	}
}

func TestLookupNextIDReadsSerializedID(t *testing.T) {
	for _, test := range []struct {
		item map[string]any
		id   uint64
		ok   bool
	}{
		{map[string]any{"id": "42"}, 42, true},
		{map[string]any{"id": uint64(7)}, 7, true},
		{map[string]any{"id": "042"}, 0, false},
		{map[string]any{"id": "0"}, 0, false},
		{map[string]any{"name": "missing"}, 0, false},
	} {
		got, ok := lookupNextID(test.item)
		if got != test.id || ok != test.ok {
			t.Errorf("lookupNextID(%v) = %d, %v; want %d, %v", test.item, got, ok, test.id, test.ok)
		}
	}
}
