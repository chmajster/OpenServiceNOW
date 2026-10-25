package cmdb

import (
	"encoding/json"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"strings"
	"testing"
)

func validCI() Fields {
	return Fields{Identifier: "Server-01", Name: "Serwer Łódź", Class: "PHYSICAL_SERVER", Status: "ACTIVE", OwnerID: 9007199254740993, Environment: "PROD", Attributes: map[string]json.RawMessage{"cpu_cores": json.RawMessage(`16`)}}
}

func TestCSVCustomFieldAdapterUsesPinnedSchemaAndRejectsUndeclaredFields(t *testing.T) {
	c := csvConfig()
	c.CustomFieldsVersion = 7
	c.CustomFields = []string{"managed"}
	c.Mapping["custom_fields.managed"] = "Managed"
	if err := ValidateImportConfig(c); err != nil {
		t.Fatal(err)
	}
	c.CustomFields = nil
	if ValidateImportConfig(c) == nil {
		t.Fatal("custom field outside the pinned allowlist was accepted")
	}
	c.CustomFields = []string{"managed"}
	configuration := metadata.DefaultConfiguration("Test")
	typeDef := configuration.RecordTypes["ci"]
	typeDef.Fields = append(typeDef.Fields, metadata.Field{Key: "managed", Labels: map[string]string{"pl": "Zarządzany", "en": "Managed"}, Type: metadata.TypeBoolean, Required: true, Active: true})
	configuration.RecordTypes["ci"] = typeDef
	schema, err := metadata.BuildSchema("ci", 7, configuration)
	if err != nil {
		t.Fatal(err)
	}
	row := CSVRow{Values: map[string]string{"identifier": "ci-1", "name": "CI", "class": "PHYSICAL_SERVER", "status": "ACTIVE", "owner_id": "1", "environment": "DEV", "attributes.cpu_cores": "16", "custom_fields.managed": "true"}}
	raw, err := csvFields(c, row, nil, schema)
	if err != nil {
		t.Fatal(err)
	}
	var fields Fields
	if json.Unmarshal(raw, &fields) != nil || string(fields.CustomFields["managed"]) != "true" || fields.CustomFieldsVersion != 7 {
		t.Fatalf("custom field import was not typed and versioned: %+v", fields)
	}
	row.Values["custom_fields.managed"] = "sometimes"
	if _, err = csvFields(c, row, nil, schema); err == nil {
		t.Fatal("invalid custom boolean was accepted")
	}
}
func TestClassAttributesAreStrict(t *testing.T) {
	f := validCI()
	out, err := Normalize(f)
	if err != nil || out.Identifier != "server-01" {
		t.Fatal(out, err)
	}
	for _, raw := range []string{`null`, `"16"`, `[]`, `{}`, `1.5`, `0`, `4097`, `true`} {
		f := validCI()
		f.Attributes["cpu_cores"] = json.RawMessage(raw)
		if _, err := Normalize(f); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	f = validCI()
	f.Attributes["discovery"] = json.RawMessage(`{}`)
	if _, err := Normalize(f); err == nil {
		t.Fatal("unvalidated attribute")
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"owner_id":"9007199254740993"`) {
		t.Fatal(string(raw))
	}
}
func TestPhysicalHardwareClassesArePublishedAndTyped(t *testing.T) {
	wanted := []string{"WORKSTATION", "PRINTER", "STORAGE_DEVICE", "MOBILE_DEVICE", "PERIPHERAL_DEVICE", "UPS"}
	schema := Schema()
	classOptions := map[string]bool{}
	for _, field := range schema.CIFields {
		if field.Key == "class" {
			for _, option := range field.Options {
				classOptions[option] = true
			}
		}
	}
	for _, key := range wanted {
		definition, ok := ClassDefinition(key)
		if !ok || !classOptions[key] {
			t.Fatalf("hardware class %q is not published", key)
		}
		attributeKeys := map[string]bool{}
		for _, attribute := range definition.Attributes {
			attributeKeys[attribute.Key] = true
		}
		for _, common := range []string{"manufacturer", "model", "serial_number", "asset_tag"} {
			if !attributeKeys[common] {
				t.Fatalf("hardware class %q lacks %q", key, common)
			}
		}
	}

	workstation, ok := ClassDefinition("WORKSTATION")
	if !ok {
		t.Fatal("workstation class missing")
	}
	var macField Attribute
	for _, field := range workstation.Attributes {
		if field.Key == "mac_address" {
			macField = field
		}
	}
	if macField.Type != "MAC" {
		t.Fatal("workstation MAC address must use the MAC validator")
	}
	for _, raw := range []string{`"AA:BB:CC:DD:EE:FF"`, `"aabb.ccdd.eeff"`} {
		f := validCI()
		f.Class = "WORKSTATION"
		f.Attributes = map[string]json.RawMessage{"mac_address": json.RawMessage(raw), "form_factor": json.RawMessage(`"LAPTOP"`)}
		if _, err := Normalize(f); err != nil {
			t.Fatalf("valid workstation attributes rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`"not-a-mac"`, `"01:02:03:04:05:06:07:08"`} {
		f := validCI()
		f.Class = "WORKSTATION"
		f.Attributes = map[string]json.RawMessage{"mac_address": json.RawMessage(raw)}
		if _, err := Normalize(f); err == nil {
			t.Fatalf("invalid MAC address accepted: %s", raw)
		}
	}
	f := validCI()
	f.Class = "WORKSTATION"
	f.Attributes = map[string]json.RawMessage{"form_factor": json.RawMessage(`"SERVER_RACK"`)}
	if _, err := Normalize(f); err == nil {
		t.Fatal("unknown workstation form factor accepted")
	}
}

func TestCSVImportAcceptsWorkstationAttributes(t *testing.T) {
	config := csvConfig()
	config.Mapping["attributes.form_factor"] = "Type"
	config.Mapping["attributes.mac_address"] = "MAC"
	rows, err := ReadCSV([]byte("ID,Name,Class,Status,Owner,Env,CPU,Type,MAC\nws-1,Workstation,WORKSTATION,ACTIVE,1,PROD,8,LAPTOP,00:11:22:33:44:55\n"), config)
	if err != nil || len(rows) != 1 || rows[0].Error != "" {
		t.Fatalf("workstation CSV row rejected: %+v, %v", rows, err)
	}
	raw, err := csvFields(config, rows[0], nil)
	if err != nil {
		t.Fatalf("workstation CSV fields rejected: %v", err)
	}
	var fields Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields.Attributes["form_factor"]) != `"LAPTOP"` || string(fields.Attributes["mac_address"]) != `"00:11:22:33:44:55"` {
		t.Fatalf("workstation attributes were not imported: %s", raw)
	}
}

func TestTypedDependencies(t *testing.T) {
	for _, c := range []struct {
		kind, source, target string
		valid                bool
	}{{"RUNS_ON", "APPLICATION", "VM", true}, {"RUNS_ON", "APPLICATION", "PHYSICAL_SERVER", true}, {"RUNS_ON", "APPLICATION", "WORKSTATION", true}, {"HOSTED_ON", "VM", "PHYSICAL_SERVER", true}, {"DEPENDS_ON", "SERVICE", "APPLICATION", true}, {"DEPENDS_ON", "SERVICE", "SERVICE", true}, {"CONNECTS_TO", "WORKSTATION", "NETWORK_DEVICE", true}, {"CONNECTS_TO", "NETWORK_DEVICE", "PRINTER", true}, {"CONNECTS_TO", "PHYSICAL_SERVER", "STORAGE_DEVICE", true}, {"RUNS_ON", "VM", "APPLICATION", false}, {"HOSTED_ON", "VM", "VM", false}, {"UNKNOWN", "SERVICE", "APPLICATION", false}} {
		if RelationType(c.kind, c.source, c.target) != c.valid {
			t.Fatal(c)
		}
	}
}

func TestSchemaPublishesValidationAndImportRules(t *testing.T) {
	schema := Schema()
	required := map[string]bool{}
	for _, field := range schema.CIFields {
		if field.Required {
			required[field.Key] = true
		}
	}
	for _, field := range []string{"identifier", "name", "class", "status", "owner_id", "environment"} {
		if !required[field] {
			t.Fatalf("required CI field %q is missing from the schema", field)
		}
	}
	for _, value := range schema.Statuses {
		if !Status(value) {
			t.Fatalf("schema status %q is not accepted by the validator", value)
		}
	}
	for _, value := range schema.Environments {
		if !Environment(value) {
			t.Fatalf("schema environment %q is not accepted by the validator", value)
		}
	}
	if Status("UNKNOWN") || Environment("UNKNOWN") {
		t.Fatal("validator accepted a value missing from the schema")
	}
	for _, definition := range schema.RelationTypes {
		for _, pair := range definition.Pairs {
			if !RelationType(definition.Key, pair.Source, pair.Target) {
				t.Fatalf("relation contract is not accepted by the validator: %s %s -> %s", definition.Key, pair.Source, pair.Target)
			}
		}
	}
	for _, field := range schema.CIImportRequiredFields {
		found := false
		for _, allowed := range schema.CIImportFields {
			found = found || field == allowed
		}
		if !found {
			t.Fatalf("required CI import field %q is not mapped", field)
		}
	}
	if schema.MaxCSVBytes != MaxCSVBytes || schema.MaxCSVRows != MaxCSVRows || schema.MaxCSVColumns != MaxCSVColumns {
		t.Fatalf("published import limits differ from the importer: %+v", schema)
	}
}
func csvConfig() ImportConfig {
	return ImportConfig{Target: "CI", IdentityKey: "identifier", Mapping: map[string]string{"identifier": "ID", "name": "Name", "class": "Class", "status": "Status", "owner_id": "Owner", "environment": "Env", "attributes.cpu_cores": "CPU"}}
}
func TestCSVIdentityLimitsAndMapping(t *testing.T) {
	c := csvConfig()
	data := "ID,Name,Class,Status,Owner,Env,CPU\nsrv1,Server,PHYSICAL_SERVER,ACTIVE,9007199254740993,PROD,16\nSRV1,Other,PHYSICAL_SERVER,ACTIVE,1,PROD,32\n"
	rows, err := ReadCSV([]byte(data), c)
	if err != nil || len(rows) != 2 || rows[0].Error == "" || rows[1].Error == "" {
		t.Fatal(rows, err)
	}
	if _, err := ReadCSV([]byte(strings.Repeat("a", MaxCSVBytes+1)), c); err == nil {
		t.Fatal("size limit")
	}
	many := "ID,Name,Class,Status,Owner,Env,CPU\n" + strings.Repeat("x,Server,PHYSICAL_SERVER,ACTIVE,1,PROD,16\n", 501)
	if _, err := ReadCSV([]byte(many), c); err == nil {
		t.Fatal("row limit")
	}
	c.Mapping["attributes.script"] = "Script"
	if ValidateImportConfig(c) == nil {
		t.Fatal("script mapping")
	}
	c = csvConfig()
	c.Mapping["name"] = "ID"
	if ValidateImportConfig(c) == nil {
		t.Fatal("reused column")
	}
}

func TestCSVSourceSelectionIsCIOnly(t *testing.T) {
	config := csvConfig()
	config.SourceID = 42
	if err := ValidateImportConfig(config); err != nil {
		t.Fatalf("configured CI source should be accepted: %v", err)
	}
	config.Target = "ASSET"
	if err := ValidateImportConfig(config); err == nil {
		t.Fatal("CSV IRE source must not be accepted for an asset import")
	}
}

func TestCSVPreservesUnmappedAndRejectsMalformedNumbers(t *testing.T) {
	c := csvConfig()
	row := CSVRow{Values: map[string]string{"identifier": "srv1", "name": "Changed", "class": "PHYSICAL_SERVER", "status": "ACTIVE", "owner_id": "9007199254740993", "environment": "PROD", "attributes.cpu_cores": "32"}}
	old := validCI()
	old.Location = "Łódź"
	old.Attributes["os"] = json.RawMessage(`"Linux"`)
	raw, err := csvFields(c, row, old)
	if err != nil {
		t.Fatal(err)
	}
	var f Fields
	json.Unmarshal(raw, &f)
	if f.Location != "Łódź" || f.OwnerID != old.OwnerID || string(f.Attributes["os"]) != `"Linux"` {
		t.Fatal(f)
	}
	for _, n := range []string{"broken", "null", "16.5", "\"32\"", "[32]"} {
		row.Values["attributes.cpu_cores"] = n
		if _, err := csvFields(c, row, old); err == nil {
			t.Fatal("invalid number", n)
		}
	}
}
