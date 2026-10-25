package metadata

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestValidateConfigurationRejectsUnknownAndCyclicRules(t *testing.T) {
	configuration := DefaultConfiguration("Test")
	first := Field{Key: "first", Labels: map[string]string{"pl": "Pierwsze", "en": "First"}, Type: TypeText, MaxLength: 40, Active: true}
	second := Field{Key: "second", Labels: map[string]string{"pl": "Drugie", "en": "Second"}, Type: TypeText, MaxLength: 40, Active: true}
	first.VisibleWhen = &Expression{Op: "EQ", Field: "second", Value: json.RawMessage(`"yes"`)}
	second.VisibleWhen = &Expression{Op: "EQ", Field: "first", Value: json.RawMessage(`"yes"`)}
	incident := configuration.RecordTypes["incident"]
	incident.Fields = []Field{first, second}
	configuration.RecordTypes["incident"] = incident
	if err := ValidateConfiguration(configuration); err == nil {
		t.Fatal("cyclic field rules must not publish")
	}

	first.VisibleWhen = &Expression{Op: "EQ", Field: "does_not_exist", Value: json.RawMessage(`"yes"`)}
	incident.Fields = []Field{first}
	configuration.RecordTypes["incident"] = incident
	if err := ValidateConfiguration(configuration); err == nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown rule field error = %v", err)
	}
}

func TestNormalizeValuesPreservesHiddenValueAndValidatesSubmittedValue(t *testing.T) {
	field := Field{Key: "environment", Labels: map[string]string{"pl": "Środowisko", "en": "Environment"}, Type: TypeChoice, Options: []Option{{Value: "dev", Labels: map[string]string{"pl": "Deweloperskie", "en": "Development"}}}, Required: true, Active: true, VisibleWhen: &Expression{Op: "EQ", Field: "category", Value: json.RawMessage(`"hardware"`)}}
	base := map[string]json.RawMessage{"category": json.RawMessage(`"software"`)}
	stored := map[string]json.RawMessage{"environment": json.RawMessage(`"dev"`)}
	values, err := NormalizeValues([]Field{field}, nil, stored, base)
	if err != nil || string(values["environment"]) != `"dev"` {
		t.Fatalf("hidden field value was not preserved: values=%v err=%v", values, err)
	}
	if _, err := NormalizeValues([]Field{field}, map[string]json.RawMessage{"environment": json.RawMessage(`"prod"`)}, nil, base); err == nil {
		t.Fatal("invalid hidden-field input must still be rejected by the data validator")
	}
}

func TestParseFiltersRequiresConfiguredIndexableField(t *testing.T) {
	configuration := DefaultConfiguration("Test")
	field := Field{Key: "deployment_env", Labels: map[string]string{"pl": "Środowisko", "en": "Environment"}, Type: TypeText, MaxLength: 40, Active: true}
	ci := configuration.RecordTypes["ci"]
	ci.Fields = []Field{field}
	configuration.RecordTypes["ci"] = ci
	schema, err := BuildSchema("ci", 1, configuration)
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"cf.deployment_env": {"prod"}}
	if _, err := ParseFilters(schema, query); err == nil {
		t.Fatal("a field outside the configured filter list must be rejected")
	}
	schema.List.Filters = append(schema.List.Filters, "deployment_env")
	filters, err := ParseFilters(schema, query)
	if err != nil || len(filters) != 1 || filters[0].Key != "deployment_env" || filters[0].Value != "prod" {
		t.Fatalf("filters=%+v err=%v", filters, err)
	}
	if _, _, err := SearchClause("ci", "c", field, 12, "dev%"); err != nil {
		t.Fatalf("indexed text search clause: %v", err)
	}
	if _, _, err := SearchClause("ci", "c", Field{Key: "secret", Type: TypeText, Active: true, System: true}, 12, "x%"); err == nil {
		t.Fatal("system fields must not use the custom search adapter")
	}
}

func TestValidationBoundsAndLocalizedSections(t *testing.T) {
	configuration := DefaultConfiguration("Test")
	incident := configuration.RecordTypes["incident"]
	incident.Form.Sections = []FormSection{{Key: "general", Label: "Ogólne", Labels: map[string]string{"pl": "Ogólne", "en": "General"}, Fields: []string{}, Columns: 2}}
	incident.Form.Tabs = []string{"general"}
	incident.Form.RoleViews = map[string][]string{"incident.update": {"number"}}
	configuration.RecordTypes["incident"] = incident
	if err := ValidateConfiguration(configuration); err != nil {
		t.Fatalf("valid localized layout: %v", err)
	}
	incident.Form.Tabs = []string{"missing"}
	configuration.RecordTypes["incident"] = incident
	if err := ValidateConfiguration(configuration); err == nil || !strings.Contains(err.Error(), "tab") {
		t.Fatalf("invalid tab reference error = %v", err)
	}
}

func TestDecimalMaximumConfiguredPrecisionFitsTypedStorage(t *testing.T) {
	field := Field{Key: "amount", Type: TypeDecimal, Precision: 20, Scale: 0}
	value, err := NormalizeValue(field, json.RawMessage(`12345678901234567890`))
	if err != nil || string(value) != `12345678901234567890` {
		t.Fatalf("maximum precision decimal value=%s err=%v", value, err)
	}
}

func TestStoredDecimalAndDateTimeMatchMySQLPrecision(t *testing.T) {
	field := Field{Key: "amount", Type: TypeDecimal, Precision: 10, Scale: 2}
	value, err := NormalizeValue(field, json.RawMessage(canonicalStoredDecimal("1.230000")))
	if err != nil || string(value) != `1.23` {
		t.Fatalf("stored decimal value=%s err=%v", value, err)
	}
	value, err = NormalizeValue(field, json.RawMessage(`1.20`))
	if err != nil || string(value) != `1.2` {
		t.Fatalf("equivalent decimal value=%s err=%v", value, err)
	}
	smallest := Field{Key: "fraction", Type: TypeDecimal, Precision: 6, Scale: 6}
	if _, err = NormalizeValue(smallest, json.RawMessage(`0.000001`)); err != nil {
		t.Fatalf("DECIMAL(6,6) smallest non-zero value: %v", err)
	}

	dateTime := Field{Key: "recorded_at", Type: TypeDateTime}
	value, err = NormalizeValue(dateTime, json.RawMessage(`"2026-01-01T01:02:03.123456789+01:00"`))
	if err != nil || string(value) != `"2026-01-01T00:02:03.123456Z"` {
		t.Fatalf("stored datetime value=%s err=%v", value, err)
	}
}
