package workspace

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/metadata"
)

var workspaceViewName = regexp.MustCompile(`^[^\x00-\x1f]{1,80}$`)

var fieldDefinitions = []FieldDefinition{
	{Key:"number", Label:"Numer", Type:"text", Operators:[]string{"eq","neq","contains","starts_with"}},
	{Key:"title", Label:"Tytuł", Type:"text", Operators:[]string{"eq","neq","contains","starts_with"}},
	{Key:"object_type", Label:"Typ pracy", Type:"enum", Operators:[]string{"eq","neq","in"}},
	{Key:"state", Label:"Stan", Type:"enum", Operators:[]string{"eq","neq","in"}},
	{Key:"state_category", Label:"Kategoria stanu", Type:"enum", Operators:[]string{"eq","neq","in"}},
	{Key:"category", Label:"Kategoria", Type:"enum", Operators:[]string{"eq","neq","in"}},
	{Key:"priority", Label:"Priorytet", Type:"number", Operators:[]string{"eq","neq","gt","gte","lt","lte","is_empty"}},
	{Key:"assigned_to", Label:"Przypisany użytkownik", Type:"reference", ReferenceType:"user", Operators:[]string{"eq","neq","is_empty"}},
	{Key:"assignment_group_id", Label:"Grupa przypisania", Type:"reference", ReferenceType:"group", Operators:[]string{"eq","neq","is_empty"}},
	{Key:"created_at", Label:"Utworzono", Type:"date", Operators:[]string{"before","after","within_last_days"}},
	{Key:"updated_at", Label:"Zmieniono", Type:"date", Operators:[]string{"before","after","within_last_days"}},
	{Key:"watched", Label:"Obserwowane przeze mnie", Type:"boolean", Operators:[]string{"eq"}},
}

func availableFields() []FieldDefinition {
	out:=make([]FieldDefinition,len(fieldDefinitions));copy(out,fieldDefinitions)
	for i:=range out { out[i].Operators=append([]string(nil),out[i].Operators...) }
	return out
}

func validateQuery(q *Query, applyDefaults bool, extraFields ...[]FieldDefinition) error {
	if q == nil { return ErrInvalid }
	if q.View=="" { q.View="all" }
	if q.Sort=="" { q.Sort="updated_desc" }
	if q.Limit==0 && applyDefaults { q.Limit=25 }
	if q.Limit<1 || q.Limit>100 || len([]rune(q.Query))>200 || !utf8.ValidString(q.Query) { return ErrInvalid }
	if q.View!="mine" && q.View!="group" && q.View!="unassigned" && q.View!="watched" && q.View!="all" { return ErrInvalid }
	if q.Sort!="updated_desc" && q.Sort!="created_desc" && q.Sort!="priority_asc" && q.Sort!="number_asc" && q.Sort!="rank" { return ErrInvalid }
	if q.Sort=="rank" && strings.TrimSpace(q.Query)=="" { return ErrInvalid }
	if q.GroupBy!="" && q.GroupBy!="object_type" && q.GroupBy!="state" && q.GroupBy!="state_category" && q.GroupBy!="priority" && q.GroupBy!="assignment_group_id" { return ErrInvalid }
	known:=map[string]bool{IncidentType:true,TaskType:true}
	seen:=map[string]bool{}
	for _,kind:=range q.Types { if !known[kind] || seen[kind] { return ErrInvalid };seen[kind]=true }
	if len(q.Types)>2 { return ErrInvalid }
	filterFields:=make(map[string]FieldDefinition,len(fieldDefinitions))
	for _,field:=range fieldDefinitions { filterFields[field.Key]=field }
	for _,fields:=range extraFields { for _,field:=range fields { filterFields[field.Key]=field } }
	if q.Filters!=nil { count:=0;if err:=validateFilter(q.Filters,1,&count,filterFields);err!=nil{return err} }
	validCols:=map[string]bool{}
	for _,field:=range fieldDefinitions { validCols[field.Key]=true }
	for _,fields:=range extraFields { for _,field:=range fields { if !field.FilterOnly { validCols[field.Key]=true } } }
	if len(q.Columns)>0 {
		if len(q.Columns)>12 { return ErrInvalid }
		seen=map[string]bool{}
		for _,column:=range q.Columns { if !validCols[column]||seen[column] { return ErrInvalid };seen[column]=true }
	}
	if len(q.ColumnOrder)>0 {
		if len(q.ColumnOrder)>12 { return ErrInvalid }
		seen=map[string]bool{}
		for _,column:=range q.ColumnOrder { if !validCols[column]||seen[column] { return ErrInvalid };seen[column]=true }
	}
	if len(q.ColumnWidths)>12 { return ErrInvalid }
	for key,width:=range q.ColumnWidths { if !validCols[key]||width<80||width>800 { return ErrInvalid } }
	if len(q.Cursor)>512 { return ErrInvalid }
	return nil
}

func validateFilter(node *FilterNode, depth int, count *int, fields map[string]FieldDefinition) error {
	if node==nil || depth>5 { return ErrInvalid }
	*count = *count + 1
	if *count>40 { return ErrInvalid }
	if len(node.Children)>0 {
		if (node.Operator!="AND"&&node.Operator!="OR")||node.Field!=""||len(node.Value)>0||len(node.Children)<1||len(node.Children)>20 { return ErrInvalid }
		for i:=range node.Children { if err:=validateFilter(&node.Children[i],depth+1,count,fields);err!=nil{return err} }
		return nil
	}
	if node.Operator!=""||node.Field=="" { return ErrInvalid }
	var def *FieldDefinition
	if item,ok:=fields[node.Field];ok { def=&item }
	if def==nil { return ErrInvalid }
	allowed:=false;for _,operator:=range def.Operators { if operator==node.Compare { allowed=true } }
	if !allowed { return ErrInvalid }
	if node.Compare=="is_empty" { if len(node.Value)>0 { return ErrInvalid };return nil }
	if len(node.Value)==0||!json.Valid(node.Value) { return ErrInvalid }
	switch node.Compare {
	case "in":
		var values []json.RawMessage
		if err:=json.Unmarshal(node.Value,&values);err!=nil||len(values)<1||len(values)>20{return ErrInvalid}
		for _,value:=range values { if _,err:=validateValue(def.Type,value);err!=nil{return ErrInvalid} }
	case "within_last_days":
		var days int
		if err:=json.Unmarshal(node.Value,&days);err!=nil||days<1||days>3650{return ErrInvalid}
	default:
		if _,err:=validateValue(def.Type,node.Value);err!=nil{return ErrInvalid}
	}
	return nil
}

func validateValue(kind string, raw json.RawMessage)(any,error) {
	switch kind {
	case "text","enum","reference":
		var value string
		if err:=json.Unmarshal(raw,&value);err!=nil||len([]rune(value))>200||!utf8.ValidString(value){return nil,ErrInvalid}
		if kind=="reference" { id,err:=strconv.ParseUint(value,10,64);if err!=nil||id==0{return nil,ErrInvalid};return id,nil }
		if value=="" { return nil,ErrInvalid }
		return value,nil
	case "number":
		var value json.Number
		dec:=json.NewDecoder(bytes.NewReader(raw));dec.UseNumber();if err:=dec.Decode(&value);err!=nil{return nil,ErrInvalid}
		n,err:=strconv.Atoi(value.String());if err!=nil||n<0||n>1000000{return nil,ErrInvalid};return n,nil
	case "integer":
		var value json.Number
		dec:=json.NewDecoder(bytes.NewReader(raw));dec.UseNumber();if err:=dec.Decode(&value);err!=nil{return nil,ErrInvalid}
		n,err:=strconv.ParseInt(value.String(),10,64);if err!=nil{return nil,ErrInvalid};return n,nil
	case "decimal":
		var value json.Number
		dec:=json.NewDecoder(bytes.NewReader(raw));dec.UseNumber();if err:=dec.Decode(&value);err!=nil||len(value.String())>40{return nil,ErrInvalid}
		if _,err:=strconv.ParseFloat(value.String(),64);err!=nil{return nil,ErrInvalid};return value.String(),nil
	case "date":
		var value string
		if err:=json.Unmarshal(raw,&value);err!=nil{return nil,ErrInvalid}
		t,err:=time.Parse(time.RFC3339,value);if err!=nil{return nil,ErrInvalid};return t.UTC(),nil
	case "date_only":
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return nil,ErrInvalid};parsed,err:=time.Parse("2006-01-02",value);if err!=nil{return nil,ErrInvalid};return parsed.Format("2006-01-02"),nil
	case "datetime":
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return nil,ErrInvalid};parsed,err:=time.Parse(time.RFC3339Nano,value);if err!=nil{return nil,ErrInvalid};return parsed.UTC(),nil
	case "boolean":
		var value bool;if err:=json.Unmarshal(raw,&value);err!=nil{return nil,ErrInvalid};return value,nil
	default: return nil,ErrInvalid
	}
}

type filterCompiler struct { objectType string; tenantID,userID uint64; now time.Time; customFields map[string]metadata.Field }

func (c *filterCompiler) compile(node *FilterNode, depth int)(string,[]any,error) {
	if node==nil{return "1=1",nil,nil}
	if depth>5{return "",nil,ErrInvalid}
	if len(node.Children)>0 {
		parts:=make([]string,0,len(node.Children));args:=[]any{}
		for i:=range node.Children { sqlPart,values,err:=c.compile(&node.Children[i],depth+1);if err!=nil{return "",nil,err};parts=append(parts,"("+sqlPart+")");args=append(args,values...) }
		return strings.Join(parts," "+node.Operator+" "),args,nil
	}
	if custom,ok:=c.customFields[node.Field];ok { return c.compileCustomField(node,custom) }
	field,kind,err:=filterField(node.Field,c.objectType);if err!=nil{return "",nil,err}
	if node.Compare=="is_empty" { if kind=="text"||kind=="enum" { return "("+field+" IS NULL OR "+field+"='')",nil,nil };return field+" IS NULL",nil,nil }
	if node.Compare=="within_last_days" { days,err:=validateValue("number",node.Value);if err!=nil{return "",nil,err};return field+">=?",[]any{c.now.Add(-time.Duration(days.(int))*24*time.Hour)},nil }
	if node.Compare=="in" {
		var rawValues []json.RawMessage;if err:=json.Unmarshal(node.Value,&rawValues);err!=nil{return "",nil,ErrInvalid}
		marks:=make([]string,0,len(rawValues));args:=make([]any,0,len(rawValues));for _,raw:=range rawValues { value,err:=validateValue(kind,raw);if err!=nil{return "",nil,err};marks=append(marks,"?");args=append(args,value) }
		return field+" IN ("+strings.Join(marks,",")+")",args,nil
	}
	value,err:=validateValue(kind,node.Value);if err!=nil{return "",nil,err}
	args:=[]any{}
	if node.Field=="watched" { args=append(args,c.userID) }
	switch node.Compare {
	case "eq": return field+" = ?",append(args,value),nil
	case "neq": return field+" <> ?",append(args,value),nil
	case "before": return field+" < ?",append(args,value),nil
	case "after": return field+" > ?",append(args,value),nil
	case "gt": return field+" > ?",append(args,value),nil
	case "gte": return field+" >= ?",append(args,value),nil
	case "lt": return field+" < ?",append(args,value),nil
	case "lte": return field+" <= ?",append(args,value),nil
	case "contains","starts_with":
		text:=value.(string);escaped:=strings.NewReplacer("!","!!","%","!%","_","!_").Replace(text);if node.Compare=="contains" { escaped="%"+escaped+"%" } else { escaped += "%" }
		return field+" LIKE ? ESCAPE '!'",append(args,escaped),nil
	default: return "",nil,ErrInvalid
	}
}

func filterField(key, objectType string)(string,string,error) {
	common:=map[string]struct{kind,inc,task string}{
		"number":{"text","i.number","t.number"},
		"title":{"text","i.title","t.title"},
		"object_type":{"enum","'incident'","'fulfillment_task'"},
		"state":{"enum","i.state","t.state"},
		"state_category":{"enum","CASE WHEN i.state IN ('RESOLVED','CLOSED','CANCELLED') THEN 'complete' WHEN i.state='ON_HOLD' THEN 'waiting' ELSE 'active' END","CASE WHEN t.state IN ('COMPLETED','CANCELLED') THEN 'complete' WHEN t.state='ON_HOLD' THEN 'waiting' ELSE 'active' END"},
		"category":{"enum","i.category","'FULFILLMENT'"},
		"priority":{"number","i.priority","NULL"},
		"assigned_to":{"reference","w.assigned_to","wa.assigned_to"},
		"assignment_group_id":{"reference","w.assignment_group_id","wa.assignment_group_id"},
		"created_at":{"date","i.created_at","r.created_at"},
		"updated_at":{"date","i.updated_at","t.updated_at"},
		"watched":{"boolean","EXISTS(SELECT 1 FROM work_watchers ww WHERE ww.tenant_id=i.tenant_id AND ww.object_type='incident' AND ww.object_id=CAST(i.id AS CHAR) AND ww.user_id=?)","EXISTS(SELECT 1 FROM work_watchers ww WHERE ww.tenant_id=t.tenant_id AND ww.object_type='fulfillment_task' AND ww.object_id=CAST(t.id AS CHAR) AND ww.user_id=?)"},
	}
	def,ok:=common[key];if !ok{return "","",ErrInvalid}
	if objectType==IncidentType{return def.inc,def.kind,nil};if objectType==TaskType{return def.task,def.kind,nil};return "","",ErrInvalid
}

func (c *filterCompiler) compileCustomField(node *FilterNode, field metadata.Field) (string, []any, error) {
	if c.objectType != IncidentType { return "1=0", nil, nil }
	column := ""
	kind := ""
	switch field.Type {
	case metadata.TypeText, metadata.TypeChoice:
		column,kind="value_text","text"
	case metadata.TypeLongText:
		column,kind="value_long","text"
	case metadata.TypeInteger:
		column,kind="value_integer","number"
	case metadata.TypeDecimal:
		column,kind="value_decimal","decimal"
	case metadata.TypeBoolean:
		column,kind="value_boolean","boolean"
	case metadata.TypeDate:
		column,kind="value_date","date"
	case metadata.TypeDateTime:
		column,kind="value_datetime","datetime"
	case metadata.TypeReference:
		column,kind="value_reference_id","reference"
	default:
		return "",nil,ErrInvalid
	}
	base := "EXISTS(SELECT 1 FROM metadata_record_values mv WHERE mv.tenant_id=? AND mv.record_type='incident' AND mv.record_id=i.id AND mv.field_key=? AND mv."+column
	args := []any{c.tenantID,field.Key}
	if node.Compare=="is_empty" {
		empty:="mv."+column+" IS NOT NULL"
		if kind=="text" { empty+=" AND mv."+column+"<>''" }
		return "NOT EXISTS(SELECT 1 FROM metadata_record_values mv WHERE mv.tenant_id=? AND mv.record_type='incident' AND mv.record_id=i.id AND mv.field_key=? AND "+empty+")",args,nil
	}
	if node.Compare=="within_last_days" { days,err:=validateValue("number",node.Value);if err!=nil{return "",nil,err};return base+">?)",append(args,c.now.Add(-time.Duration(days.(int))*24*time.Hour)),nil }
	if node.Compare=="in" {
		var rawValues []json.RawMessage
		if err:=json.Unmarshal(node.Value,&rawValues);err!=nil||len(rawValues)==0{return "",nil,ErrInvalid}
		marks:=make([]string,0,len(rawValues))
		for _,raw:=range rawValues { value,err:=metadataFilterValue(field,kind,raw);if err!=nil{return "",nil,ErrInvalid};marks=append(marks,"?");args=append(args,value) }
		return base+" IN ("+strings.Join(marks,",")+"))",args,nil
	}
	value,err:=metadataFilterValue(field,kind,node.Value);if err!=nil{return "",nil,ErrInvalid}
	operator:="="
	switch node.Compare {
	case "eq":
	case "neq":operator="<>"
	case "gt":operator=">"
	case "gte":operator=">="
	case "lt":operator="<"
	case "lte":operator="<="
	case "before":operator="<"
	case "after":operator=">"
	case "contains","starts_with":
		text:=value.(string);escaped:=strings.NewReplacer("!","!!","%","!%","_","!_").Replace(text)
		if node.Compare=="contains"{escaped="%"+escaped+"%"}else{escaped+="%"}
		return base+" LIKE ? ESCAPE '!')",append(args,escaped),nil
	default:return "",nil,ErrInvalid
	}
	return base+operator+"?)",append(args,value),nil
}

func metadataFilterValue(field metadata.Field, kind string, raw json.RawMessage) (any,error) {
	normalized,err:=metadata.NormalizeValue(field,raw);if err!=nil{return nil,err}
	switch kind {
	case "text":
		var value string;err=json.Unmarshal(normalized,&value);return value,err
	case "number":
		var value int64;err=json.Unmarshal(normalized,&value);return value,err
	case "decimal":
		var value json.Number;err=json.Unmarshal(normalized,&value);return value.String(),err
	case "boolean":
		var value bool;err=json.Unmarshal(normalized,&value);return value,err
	case "date":
		var value string;err=json.Unmarshal(normalized,&value);return value,err
	case "datetime":
		var value string;if err=json.Unmarshal(normalized,&value);err!=nil{return nil,err};return time.Parse(time.RFC3339Nano,value)
	case "reference":
		var value string;if err=json.Unmarshal(normalized,&value);err!=nil{return nil,err};return strconv.ParseUint(value,10,64)
	default:return nil,ErrInvalid
	}
}

func encodeCursor(value cursor) string {
	raw,_:=json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(raw, sortBy string)(cursor,error) {
	if raw=="" { return cursor{},nil }
	data,err:=base64.RawURLEncoding.DecodeString(raw);if err!=nil||len(data)>256{return cursor{},ErrInvalid}
	var c cursor;if err=json.Unmarshal(data,&c);err!=nil||c.Sort!=sortBy||c.ObjectID==""||(c.ObjectType!=IncidentType&&c.ObjectType!=TaskType){return cursor{},ErrInvalid}
	if _,err=strconv.ParseUint(c.ObjectID,10,64);err!=nil{return cursor{},ErrInvalid}
	if (sortBy=="updated_desc"||sortBy=="created_desc"||sortBy=="rank"||sortBy=="priority_asc") { if _,err=time.Parse(time.RFC3339Nano,c.UpdatedAt);sortBy!="created_desc"&&err!=nil{return cursor{},ErrInvalid};if sortBy=="created_desc" {if _,err=time.Parse(time.RFC3339Nano,c.Value);err!=nil{return cursor{},ErrInvalid}} }
	if sortBy=="priority_asc"&&c.Number<0{return cursor{},ErrInvalid}
	if sortBy=="rank"&&(c.Number<0||c.Number>3){return cursor{},ErrInvalid}
	return c,nil
}

func normalizedTypes(q Query) []string {
	if q.Types!=nil { return append(make([]string,0,len(q.Types)),q.Types...) }
	return []string{IncidentType,TaskType}
}

func cloneQuery(q Query) Query {
	q.Cursor=""
	if q.Types!=nil { q.Types=append(make([]string,0,len(q.Types)),q.Types...) }
	if q.Columns!=nil { q.Columns=append(make([]string,0,len(q.Columns)),q.Columns...) }
	if q.ColumnOrder!=nil { q.ColumnOrder=append(make([]string,0,len(q.ColumnOrder)),q.ColumnOrder...) }
	if q.ColumnWidths!=nil { widths:=map[string]int{};for k,v:=range q.ColumnWidths {widths[k]=v};q.ColumnWidths=widths }
	return q
}

func validateID(value string) (uint64,error) { id,err:=strconv.ParseUint(value,10,64);if err!=nil||id==0{return 0,fmt.Errorf("%w: invalid record id",ErrInvalid)};return id,nil }
