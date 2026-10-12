package catalog

import (
	"encoding/json"
	"testing"
)

func TestDeclarativeForms(t *testing.T) {
	fields := []Field{{Key: "hardware", Label: "Sprzęt", Type: "checkbox"}, {Key: "details", Label: "Opis", Type: "longtext", Required: true, VisibleWhen: &Condition{Field: "hardware", Equals: json.RawMessage("true")}}, {Key: "qty", Label: "Liczba", Type: "number", RequiredWhen: &Condition{Field: "hardware", Equals: json.RawMessage("true")}}, {Key: "date", Label: "Data", Type: "date"}, {Key: "choice", Label: "Wybór", Type: "select", Options: []string{"a", "b"}}}
	d := Definition{Title: "Usługa", Fields: fields, GroupID: 1, Method: "MANUAL_TASKS", Steps: []Step{{Title: "Zadanie", Required: true}}}
	if err := ValidateDefinition(d, true); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"details":"ukryty"}`, `{"hardware":true}`, `{"hardware":true,"details":"opis"}`, `{"hardware":true,"details":"opis","qty":"1"}`, `{"date":"2026-02-30"}`, `{"choice":"c"}`, `{"unknown":"x"}`, `{"hardware":null}`, `{"qty":1000000000001}`} {
		var values map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &values)
		if _, err := ValidateValues(fields, values); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	var values map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"hardware":true,"details":"Zażółć 🛠️","qty":1.5,"date":"2028-02-29","choice":"b"}`), &values)
	if _, err := ValidateValues(fields, values); err != nil {
		t.Fatal(err)
	}
	d.Fields[0].VisibleWhen = &Condition{Field: "details", Equals: json.RawMessage(`"x"`)}
	if err := ValidateDefinition(d, true); err == nil {
		t.Fatal("forward/cyclic condition accepted")
	}
	d.Fields = fields
	d.Fields[0].VisibleWhen = nil
	d.Method = "eval(script)"
	if err := ValidateDefinition(d, true); err == nil {
		t.Fatal("script execution configuration accepted")
	}
}
