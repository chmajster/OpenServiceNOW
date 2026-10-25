package workspace

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/metadata"
)

func TestValidateQueryBoundsAndDefaults(t *testing.T) {
	query := Query{}
	if err := validateQuery(&query, true); err != nil {
		t.Fatalf("valid default query rejected: %v", err)
	}
	if query.View != "all" || query.Sort != "updated_desc" || query.Limit != 25 {
		t.Fatalf("defaults = %#v", query)
	}

	bad := Query{View: "all", Sort: "updated_desc", Limit: 101}
	if err := validateQuery(&bad, false); err == nil {
		t.Fatal("oversized page limit accepted")
	}
	bad = Query{View: "all", Sort: "updated_desc", Limit: 25, Types: []string{IncidentType, IncidentType}}
	if err := validateQuery(&bad, false); err == nil {
		t.Fatal("duplicate adapter accepted")
	}
}

func TestValidateFilterRejectsHiddenFieldsAndDeepTrees(t *testing.T) {
	field := FilterNode{Field: "description", Compare: "contains", Value: json.RawMessage(`"secret"`)}
	query := Query{View: "all", Sort: "updated_desc", Limit: 25, Filters: &field}
	if err := validateQuery(&query, false); err == nil {
		t.Fatal("unregistered field was filterable")
	}
	deep := FilterNode{Field: "state", Compare: "eq", Value: json.RawMessage(`"OPEN"`)}
	for depth := 0; depth < 5; depth++ {
		deep = FilterNode{Operator: "AND", Children: []FilterNode{deep}}
	}
	query.Filters = &deep
	if err := validateQuery(&query, false); err == nil {
		t.Fatal("overly deep filter tree was accepted")
	}
}

func TestFilterCompilerUsesTypedValuesAndTenantScopedReferenceFields(t *testing.T) {
	node := FilterNode{Field: "title", Compare: "contains", Value: json.RawMessage(`"%' OR 1=1 --"`)}
	compiler := filterCompiler{objectType: IncidentType, userID: 7, now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	clause, args, err := compiler.compile(&node, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clause, "1=1 --") || len(args) != 1 || args[0] != "%!%' OR 1=1 --%" {
		t.Fatalf("unsafe contains compilation: %s %#v", clause, args)
	}

	ref := FilterNode{Field: "assigned_to", Compare: "eq", Value: json.RawMessage(`"42"`)}
	clause, args, err = compiler.compile(&ref, 1)
	if err != nil || clause != "w.assigned_to = ?" || len(args) != 1 || args[0] != uint64(42) {
		t.Fatalf("reference compilation = %q %#v, %v", clause, args, err)
	}
	taskCompiler := filterCompiler{objectType: TaskType, userID: 7, now: time.Now()}
	clause, _, err = taskCompiler.compile(&ref, 1)
	if err != nil || clause != "wa.assigned_to = ?" {
		t.Fatalf("task reference compilation = %q, %v", clause, err)
	}
}

func TestDynamicMetadataFiltersAreTypedAndIncidentScoped(t *testing.T) {
	field := metadata.Field{Key: "serial", Type: metadata.TypeText, Active: true, MaxLength: 100}
	node := FilterNode{Field: "cf.serial", Compare: "contains", Value: json.RawMessage(`"abc%"`)}
	compiler := filterCompiler{objectType: IncidentType, tenantID: 19, userID: 7, now: time.Now(), customFields: map[string]metadata.Field{"cf.serial": field}}
	clause, args, err := compiler.compile(&node, 1)
	if err != nil || !strings.Contains(clause, "mv.record_id=i.id") || !strings.Contains(clause, "mv.tenant_id=?") || len(args) != 3 || args[0] != uint64(19) || args[1] != "serial" || args[2] != "%abc!%%" {
		t.Fatalf("metadata filter compilation = %q %#v, %v", clause, args, err)
	}
	taskCompiler := filterCompiler{objectType: TaskType, tenantID: 19, userID: 7, now: time.Now(), customFields: map[string]metadata.Field{"cf.serial": field}}
	clause, _, err = taskCompiler.compile(&node, 1)
	if err != nil || clause != "1=0" {
		t.Fatalf("metadata task filter = %q, %v", clause, err)
	}
	query := Query{View: "all", Sort: "updated_desc", Limit: 25, Filters: &node}
	if err := validateQuery(&query, false); err == nil {
		t.Fatal("custom field accepted without visible metadata definition")
	}
	if err := validateQuery(&query, false, []FieldDefinition{{Key: "cf.serial", Type: "text", Operators: []string{"contains"}, FilterOnly: true}}); err != nil {
		t.Fatalf("configured metadata field rejected: %v", err)
	}
}

func TestSelectedCustomColumnsDoNotAttachUnselectedMetadataValues(t *testing.T) {
	available := map[string]metadata.Field{
		"cf.serial": {Key: "serial", Type: metadata.TypeText, Active: true},
		"cf.owner":  {Key: "owner", Type: metadata.TypeReference, Active: true},
	}
	selected := selectedCustomColumns(Query{Columns: []string{"number", "cf.serial"}}, available)
	if len(selected) != 1 || selected["cf.serial"].Key != "serial" {
		t.Fatalf("selected custom columns = %#v", selected)
	}
	if got := selectedCustomColumns(Query{}, available); len(got) != 0 {
		t.Fatalf("default columns unexpectedly include metadata values: %#v", got)
	}
}

func TestRelativeDateAndNestedFilterCompilation(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	node := FilterNode{Operator: "OR", Children: []FilterNode{
		{Field: "state_category", Compare: "eq", Value: json.RawMessage(`"waiting"`)},
		{Field: "updated_at", Compare: "within_last_days", Value: json.RawMessage(`7`)},
	}}
	compiler := filterCompiler{objectType: TaskType, userID: 9, now: now}
	clause, args, err := compiler.compile(&node, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clause, " OR ") || !strings.Contains(clause, "t.updated_at>=?") || len(args) != 2 {
		t.Fatalf("unexpected compiled expression: %s %#v", clause, args)
	}
	if got := args[1].(time.Time); !got.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("relative date = %s", got)
	}
}

func TestWorkIdentityAndCursorAreTypeScoped(t *testing.T) {
	incident := WorkReference{TenantID: 5, ObjectType: IncidentType, ObjectID: "31"}
	task := WorkReference{TenantID: 5, ObjectType: TaskType, ObjectID: "31"}
	otherTenant := WorkReference{TenantID: 6, ObjectType: IncidentType, ObjectID: "31"}
	if incident.Key() == task.Key() || incident.Key() == otherTenant.Key() {
		t.Fatal("composite work identity collided across types or tenants")
	}
	value := cursor{Sort: "updated_desc", ObjectType: TaskType, ObjectID: "31", UpdatedAt: "2026-10-23T12:00:00Z"}
	decoded, err := decodeCursor(encodeCursor(value), "updated_desc")
	if err != nil || decoded.ObjectType != TaskType || decoded.ObjectID != "31" {
		t.Fatalf("cursor round trip = %#v, %v", decoded, err)
	}
	if _, err := decodeCursor(encodeCursor(value), "created_desc"); err == nil {
		t.Fatal("cursor was accepted for a different sort")
	}
}

func TestCSVNeutralizesSpreadsheetFormulas(t *testing.T) {
	for _, value := range []string{"=1+1", "  @SUM(A1:A2)", "-2+4", "+cmd", "plain text"} {
		got := safeCSVText(value)
		trimmed := strings.TrimSpace(got)
		if strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "@") || strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") {
			t.Errorf("formula-like CSV cell was not neutralized: %q -> %q", value, got)
		}
	}
}

func TestExportUsesSelectedColumnOrderAndOnlyViewFields(t *testing.T) {
	priority:=2
	item:=WorkItem{WorkReference:WorkReference{TenantID:3,ObjectType:IncidentType,ObjectID:"12"},Number:"INC0012",Title:"=1+1",State:"NEW",Priority:&priority,UpdatedAt:time.Date(2026,10,23,0,0,0,0,time.UTC)}
	content,rows,err:=renderCSV([]WorkItem{item},Query{Columns:[]string{"title","number","state"},ColumnOrder:[]string{"state","title","number"}})
	if err!=nil||rows!=1{t.Fatalf("render export rows=%d err=%v",rows,err)}
	records,err:=csv.NewReader(content).ReadAll();if err!=nil{t.Fatal(err)}
	want:=[]string{"tenant_id","object_type","object_id","state","title","number"}
	if len(records)!=2||len(records[0])!=len(want){t.Fatalf("CSV records=%#v",records)}
	for index,value:=range want{if records[0][index]!=value{t.Fatalf("header[%d]=%q want %q",index,records[0][index],value)}}
	if records[1][4]!="'=1+1"||records[1][5]!="INC0012"{t.Fatalf("export data row=%#v",records[1])}
}

func TestEmptyColumnSelectionIsPreserved(t *testing.T) {
	query:=Query{Columns:[]string{},ColumnOrder:[]string{}}
	cloned:=cloneQuery(query)
	if cloned.Columns==nil||cloned.ColumnOrder==nil{t.Fatal("explicit empty column selection collapsed into default columns")}
	content,_,err:=renderCSV(nil,cloned);if err!=nil{t.Fatal(err)}
	record,err:=csv.NewReader(content).Read();if err!=nil{t.Fatal(err)}
	want:=[]string{"tenant_id","object_type","object_id"}
	if strings.Join(record,",")!=strings.Join(want,","){t.Fatalf("empty-view CSV header = %#v",record)}
}
