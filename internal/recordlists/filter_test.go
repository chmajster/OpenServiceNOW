package recordlists

import (
	"encoding/json"
	"strings"
	"testing"
)

var testSchema = Schema{"title": {SQL: "i.title", Kind: "text"}, "id": {SQL: "i.id", Kind: "id"}, "state": {SQL: "i.state", Kind: "enum", Values: []string{"NEW", "CLOSED"}}, "active": {Predicate: BooleanPredicate("i.state<>'CLOSED'")}}

func TestCompileNestedParameterizedFilter(t *testing.T) {
	n, err := Decode(`{"logic":"AND","children":[{"field":"active","operator":"eq","value":"true"},{"logic":"OR","children":[{"field":"title","operator":"contains","value":"%' OR 1=1 --"},{"field":"id","operator":"eq","value":"9007199254740993"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	sql, args, err := Compile(n, testSchema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "OR 1=1") || !strings.Contains(sql, " OR ") || args[0] != "%!%' OR 1=1 --%" || args[1] != uint64(9007199254740993) {
		t.Fatalf("%s %#v", sql, args)
	}
}
func TestRejectUnsafeOrAmbiguousFilters(t *testing.T) {
	for _, raw := range []string{`null`, `{"field":"i.title; DROP TABLE users","operator":"eq","value":"a"}`, `{"field":"id","operator":"eq","value":"9007199254740993.0"}`, `{"logic":"OR","children":[]}`, `{"field":"title","operator":"sql","value":"a"}`, `{"logic":"AND","field":"title","children":[{}]}`, `{"field":"state","operator":"eq","value":"INVALID"}`, `{"field":"title","operator":"eq","value":"a","tenant_id":"1"}`, `{} {}`} {
		n, err := Decode(raw)
		if err == nil {
			_, _, err = Compile(n, testSchema)
		}
		if err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	n := Node{Field: "title", Operator: "eq", Value: "a"}
	for i := 0; i < 7; i++ {
		n = Node{Logic: "AND", Children: []Node{n}}
	}
	if _, _, err := Compile(&n, testSchema); err == nil {
		t.Fatal("unbounded depth")
	}
}
func TestCSVFormulaProtection(t *testing.T) {
	for _, s := range []string{"=cmd()", "\t+SUM(A1)", "\r@attack", " -1"} {
		if CSVCell(s) != "'"+s {
			t.Fatal(s)
		}
	}
	if CSVCell("INC0001") != "INC0001" {
		t.Fatal("normal text changed")
	}
}
func TestConfigCannotIntroduceFieldsOrRepeatedColumns(t *testing.T) {
	raw := json.RawMessage(`{"sort":"newest","columns":[{"key":"title","width":200},{"key":"title","width":200}]}`)
	if _, err := ValidateConfig(raw, testSchema, []string{"newest"}, nil, []string{"title"}); err == nil {
		t.Fatal("duplicate columns")
	}
}
