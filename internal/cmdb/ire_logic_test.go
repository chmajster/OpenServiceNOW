package cmdb

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeIdentity(t *testing.T) {
	tests := []struct {
		key, value, mode, want string
	}{
		{"identifier", "  Host-A  ", "LOWER_TRIM", "host-a"},
		{"attributes.serial_number", " ab-123 ", "UPPER_TRIM", "AB-123"},
		{"attributes.mac_address", "aa:bb:cc:dd:ee:ff", "UPPER_MAC", "AABBCCDDEEFF"},
	}
	for _, test := range tests {
		got, err := normalizeIdentity(test.key, test.value, test.mode)
		if err != nil || got != test.want {
			t.Errorf("normalizeIdentity(%q, %q, %q) = %q, %v; want %q", test.key, test.value, test.mode, got, err, test.want)
		}
	}
	if _, err := normalizeIdentity("attributes.mac_address", "not-a-mac", "UPPER_MAC"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid MAC error = %v; want ErrInvalid", err)
	}
}

func TestValidateIdentityRuleRejectsUnstableAndInvalidKeys(t *testing.T) {
	valid := IdentityRuleInput{Class: "APPLICATION", MatchKeys: [][]string{{"identifier"}}, RequiredAttributes: []string{}, Normalization: map[string]string{"identifier": "LOWER_TRIM"}}
	if err := validateRuleInput(valid); err != nil {
		t.Fatalf("valid identity rule rejected: %v", err)
	}
	invalid := valid
	invalid.MatchKeys = [][]string{{"name"}}
	if err := validateRuleInput(invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mutable name identity accepted: %v", err)
	}
	invalid = valid
	invalid.MatchKeys = [][]string{{"attributes.ip_address"}}
	if err := validateRuleInput(invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("IP address identity accepted: %v", err)
	}
}

func TestValidateFieldPolicyRequiresConsistentAuthority(t *testing.T) {
	policy := FieldPolicy{Class: "*", FieldKey: "name", Strategy: "AUTHORITATIVE"}
	if err := validatePolicy(policy); !errors.Is(err, ErrInvalid) {
		t.Fatalf("authoritative policy without a source accepted: %v", err)
	}
	policy.AuthoritativeSourceID = 42
	if err := validatePolicy(policy); err != nil {
		t.Fatalf("valid authoritative policy rejected: %v", err)
	}
	policy.Strategy = "PRIORITY"
	if err := validatePolicy(policy); !errors.Is(err, ErrInvalid) {
		t.Fatalf("priority policy with authoritative source accepted: %v", err)
	}
}

func TestValidateRunInputAndEmptyValues(t *testing.T) {
	input := ReconciliationRunInput{SourceID: 1, IdempotencyKey: "batch-123", Mode: "INCREMENTAL", Observations: []Observation{{NativeKey: "row-1", Values: map[string]json.RawMessage{"identifier": json.RawMessage(`"ci-1"`)}}}}
	if err := validateRunInput(input); err != nil {
		t.Fatalf("valid run input rejected: %v", err)
	}
	input.Mode = "FULL"
	if err := validateRunInput(input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("full snapshot without scope accepted: %v", err)
	}
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`""`), json.RawMessage(`"  "`)} {
		if !isEmptyJSON(raw) {
			t.Errorf("isEmptyJSON(%s) = false", raw)
		}
	}
	if isEmptyJSON(json.RawMessage(`0`)) || isEmptyJSON(json.RawMessage(`false`)) {
		t.Fatal("valid non-string JSON scalar treated as empty")
	}
}

func TestManualProvenanceClearMarkerReleasesImplicitHold(t *testing.T) {
	if !manualProvenanceHolds(currentProvenance{sourceType: "MANUAL", runType: "MANUAL"}) {
		t.Fatal("manual CI edit should hold its value until an override is cleared")
	}
	if manualProvenanceHolds(currentProvenance{sourceType: "MANUAL", runType: "MANUAL_OVERRIDE_CLEARED"}) {
		t.Fatal("explicitly cleared override must allow the configured source policy to decide")
	}
}

func TestSystemCSVSourceKeepsItsLegacyIdentityScope(t *testing.T) {
	source := csvSourceScope(DataSource{Key: "csv-import", ScopeKey: ""})
	if source.ScopeKey != "default" {
		t.Fatalf("system CSV scope = %q, want legacy default scope", source.ScopeKey)
	}
	custom := csvSourceScope(DataSource{Key: "inventory_csv", ScopeKey: ""})
	if custom.ScopeKey != "" {
		t.Fatalf("configured source scope = %q, want configured empty scope preserved", custom.ScopeKey)
	}
}

func TestCustomFieldPolicyKeyIsTenantSchemaScoped(t *testing.T) {
	if !validPolicyFieldKey("custom_fields.cost_center") {
		t.Fatal("valid custom field key rejected")
	}
	if validPolicyFieldKey("custom_fields.CostCenter") || validPolicyFieldKey("custom_fields.cost-center") || validPolicyFieldKey("custom_fields."+strings.Repeat("a", 49)) {
		t.Fatal("invalid custom field key accepted")
	}
}

func TestQualityReferenceTargetIsAnAllowlistedTenantTable(t *testing.T) {
	for _, target := range []string{"user", "group", "incident", "request", "ci", "asset"} {
		if _, ok := qualityReferenceExistsSQL(target); !ok {
			t.Errorf("supported reference target %q rejected", target)
		}
	}
	if _, ok := qualityReferenceExistsSQL("external_table"); ok {
		t.Fatal("untrusted reference target accepted")
	}
}
