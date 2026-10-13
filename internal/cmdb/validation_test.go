package cmdb

import (
	"encoding/json"
	"strings"
	"testing"
)

func validCI() Fields {
	return Fields{Identifier: "Server-01", Name: "Serwer Łódź", Class: "PHYSICAL_SERVER", Status: "ACTIVE", OwnerID: 9007199254740993, Environment: "PROD", Attributes: map[string]json.RawMessage{"cpu_cores": json.RawMessage(`16`)}}
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
func TestTypedDependencies(t *testing.T) {
	for _, c := range []struct {
		kind, source, target string
		valid                bool
	}{{"RUNS_ON", "APPLICATION", "VM", true}, {"HOSTED_ON", "VM", "PHYSICAL_SERVER", true}, {"DEPENDS_ON", "SERVICE", "APPLICATION", true}, {"DEPENDS_ON", "SERVICE", "SERVICE", true}, {"RUNS_ON", "VM", "APPLICATION", false}, {"HOSTED_ON", "VM", "VM", false}, {"UNKNOWN", "SERVICE", "APPLICATION", false}} {
		if RelationType(c.kind, c.source, c.target) != c.valid {
			t.Fatal(c)
		}
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

func TestDatabaseHostnameAndOperationalTypes(t *testing.T) {
	f := validCI()
	f.Class = "DATABASE"
	f.Attributes = map[string]json.RawMessage{"port": json.RawMessage(`5432`), "hostname": json.RawMessage(`"db.example.test"`)}
	if _, err := Normalize(f); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"-db.example"`, `"db..example"`, `"db/example"`, `"db.example."`, `null`, `42`} {
		f.Attributes["hostname"] = json.RawMessage(value)
		if _, err := Normalize(f); err == nil {
			t.Fatal(value)
		}
	}
	f.Attributes = map[string]json.RawMessage{}
	f.Criticality = "unknown"
	if _, err := Normalize(f); err == nil {
		t.Fatal("arbitrary criticality")
	}
}
