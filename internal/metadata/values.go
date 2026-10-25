package metadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type QueryFilter struct {
	Key    string
	Column string
	Value  any
}

func ParseFilters(schema Schema, query url.Values) ([]QueryFilter, error) {
	fields := map[string]Field{}
	for _, field := range schema.Fields { fields[field.Key] = field }
	allowed := map[string]bool{}
	for _, key := range schema.List.Filters { allowed[key] = true }
	filters := []QueryFilter{}
	for name, values := range query {
		if !strings.HasPrefix(name,"cf.") { continue }
		key:=strings.TrimPrefix(name,"cf.")
		field,ok:=fields[key]
		if !ok||!allowed[key]||!field.Active||field.System||len(values)!=1||len(values[0])>4096 { return nil,FieldError{Field:key,Code:"INVALID_FILTER",Cause:"unknown or invalid custom field filter"} }
		column,err:=filterColumn(field.Type)
		if err!=nil{return nil,FieldError{Field:key,Code:"NOT_FILTERABLE",Cause:"field type does not support indexed filtering"}}
		value,err:=filterValue(field,values[0]);if err!=nil{return nil,FieldError{Field:key,Code:"INVALID_FILTER",Cause:"filter does not match field type"}}
		filters=append(filters,QueryFilter{Key:key,Column:column,Value:value})
	}
	sort.Slice(filters,func(i,j int)bool{return filters[i].Key<filters[j].Key})
	return filters,nil
}

func SearchClause(typeKey, alias string, field Field, tenantID uint64, prefix string) (string, []any, error) {
	if (typeKey != "incident" && typeKey != "ci") || alias != "i" && alias != "c" || field.System || !field.Active {
		return "", nil, ErrInvalid
	}
	column := ""
	switch field.Type {
	case TypeText, TypeChoice:
		column = "value_text"
	case TypeLongText:
		column = "value_long"
	default:
		return "", nil, ErrInvalid
	}
	clause := fmt.Sprintf("EXISTS(SELECT 1 FROM metadata_record_values mv WHERE mv.tenant_id=? AND mv.record_type=? AND mv.record_id=%s.id AND mv.field_key=? AND mv.%s LIKE ? ESCAPE '!')", alias, column)
	return clause, []any{tenantID, typeKey, field.Key, prefix}, nil
}

func filterColumn(fieldType string) (string,error) {
	switch fieldType {
	case TypeText,TypeChoice:return "value_text",nil
	case TypeInteger:return "value_integer",nil
	case TypeDecimal:return "value_decimal",nil
	case TypeBoolean:return "value_boolean",nil
	case TypeDate:return "value_date",nil
	case TypeDateTime:return "value_datetime",nil
	case TypeReference:return "value_reference_id",nil
	default:return "",ErrInvalid
	}
}

func filterValue(field Field, raw string)(any,error){
	var encoded []byte
	switch field.Type {
	case TypeText,TypeLongText,TypeChoice,TypeMultiChoice:
		encoded,_=json.Marshal(raw)
	case TypeBoolean:
		value,err:=strconv.ParseBool(raw);if err!=nil{return nil,err};return value,nil
	case TypeInteger:
		value,err:=strconv.ParseInt(raw,10,64);if err!=nil{return nil,err};encoded=[]byte(strconv.FormatInt(value,10))
	case TypeDecimal:
		encoded=[]byte(raw)
	case TypeDate:
		if _,err:=time.Parse("2006-01-02",raw);err!=nil{return nil,err};return raw,nil
	case TypeDateTime:
		value,err:=time.Parse(time.RFC3339Nano,raw);if err!=nil{return nil,err};return value.UTC(),nil
	case TypeReference:
		value,err:=strconv.ParseUint(raw,10,64);if err!=nil||value==0||strconv.FormatUint(value,10)!=raw{return nil,ErrInvalid};return value,nil
	default:return nil,ErrInvalid
	}
	normalized,err:=NormalizeValue(field,encoded);if err!=nil{return nil,err}
	switch field.Type {
	case TypeText,TypeChoice:
		var value string;err=json.Unmarshal(normalized,&value);return value,err
	case TypeInteger:
		var value int64;err=json.Unmarshal(normalized,&value);return value,err
	case TypeDecimal:
		var value json.Number;err=json.Unmarshal(normalized,&value);return string(value),err
	default:return nil,ErrInvalid
	}
}

func (s *Service) PrepareValues(ctx context.Context, exec database.Executor, actor Actor, typeKey string, recordID uint64, input map[string]json.RawMessage, base map[string]json.RawMessage, expectedVersion ...uint64) (map[string]json.RawMessage, Schema, error) {
	version, configuration, err := s.Published(ctx,exec,actor.TenantID)
	if err!=nil{return nil,Schema{},err}
	if len(expectedVersion) > 0 && expectedVersion[0] > 0 && expectedVersion[0] != version {
		return nil, Schema{}, ErrConflict
	}
	return s.prepareValues(ctx,exec,actor,typeKey,recordID,input,base,version,configuration)
}

func (s *Service) PrepareValuesAtVersion(ctx context.Context,exec database.Executor,actor Actor,typeKey string,recordID uint64,input map[string]json.RawMessage,base map[string]json.RawMessage,version uint64)(map[string]json.RawMessage,Schema,error){
	if version==0{return nil,Schema{},ErrInvalid}
	item,err:=readVersion(ctx,exec,actor.TenantID,version);if err!=nil{return nil,Schema{},err}
	if item.State!="PUBLISHED"&&item.State!="RETIRED"{return nil,Schema{},ErrNotFound}
	return s.prepareValues(ctx,exec,actor,typeKey,recordID,input,base,version,item.Config)
}

func (s *Service) prepareValues(ctx context.Context,exec database.Executor,actor Actor,typeKey string,recordID uint64,input map[string]json.RawMessage,base map[string]json.RawMessage,version uint64,configuration Configuration)(map[string]json.RawMessage,Schema,error){
	schema,err:=configuration.Schema(typeKey,version)
	if err!=nil{return nil,Schema{},err}
	schema=enrichSchema(schema,configuration)
	var existing map[string]json.RawMessage
	if recordID>0 { all,readErr:=ReadValues(ctx,exec,actor.TenantID,typeKey,[]uint64{recordID});if readErr!=nil{return nil,Schema{},readErr};existing=all[recordID] }
	values,err:=NormalizeValues(schema.Fields,input,existing,base)
	if err!=nil{return nil,Schema{},err}
	for _,field:=range schema.Fields {
		if !field.Active||field.System||field.Type!=TypeReference {continue}
		raw,ok:=values[field.Key];if !ok{continue}
		var idText string;if json.Unmarshal(raw,&idText)!=nil{return nil,Schema{},FieldError{Field:field.Key,Code:"INVALID_REFERENCE",Cause:"reference value is invalid"}}
		id,parseErr:=strconv.ParseUint(idText,10,64);if parseErr!=nil{return nil,Schema{},FieldError{Field:field.Key,Code:"INVALID_REFERENCE",Cause:"reference value is invalid"}}
		if err:=s.validateReference(ctx,exec,actor,field.ReferenceType,id);err!=nil{return nil,Schema{},FieldError{Field:field.Key,Code:"REFERENCE_UNAVAILABLE",Cause:"referenced record is unavailable"}}
	}
	return values,schema,nil
}

func (s *Service) SaveValues(ctx context.Context, exec database.Executor, tenant uint64, typeKey string, recordID, schemaVersion uint64, schema Schema, values map[string]json.RawMessage) error {
	keys:=[]string{}
	for _,field:=range schema.Fields { if field.Active&&!field.System { keys=append(keys,field.Key) } }
	if len(keys)>0 {
		marks:=strings.TrimSuffix(strings.Repeat("?,",len(keys)),",")
		args:=[]any{tenant,typeKey,recordID};for _,key:=range keys{args=append(args,key)}
		if _,err:=exec.ExecContext(ctx,"DELETE FROM metadata_record_values WHERE tenant_id=? AND record_type=? AND record_id=? AND field_key IN ("+marks+")",args...);err!=nil{return err}
	}
	for _,field:=range schema.Fields {
		if !field.Active||field.System {continue}
		raw,ok:=values[field.Key];if !ok{continue}
		if err:=saveValue(ctx,exec,tenant,typeKey,recordID,field,schemaVersion,raw);err!=nil{return err}
	}
	return nil
}

func saveValue(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,recordID uint64,field Field,schemaVersion uint64,raw json.RawMessage)error{
	normalized, err := NormalizeValue(field, raw)
	if err != nil { return FieldError{Field:field.Key,Code:"INVALID_VALUE",Cause:"value does not match the published field definition"} }
	raw = normalized
	var textValue,longValue any;var intValue,refValue any;var decimalValue,boolValue,dateValue,dateTimeValue,jsonValue,hash any
	switch field.Type {
	case TypeText,TypeChoice:
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};textValue=value
	case TypeLongText:
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};longValue=value
	case TypeBoolean:
		var value bool;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};boolValue=value
	case TypeInteger:
		var value int64;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};intValue=value
	case TypeDecimal:
		var value json.Number;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};decimalValue=string(value)
	case TypeDate:
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};dateValue=value
	case TypeDateTime:
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};parsed,err:=time.Parse(time.RFC3339Nano,value);if err!=nil{return ErrInvalid};dateTimeValue=parsed.UTC()
	case TypeMultiChoice:
		jsonValue=[]byte(raw)
	case TypeReference:
		var value string;if err:=json.Unmarshal(raw,&value);err!=nil{return ErrInvalid};id,err:=strconv.ParseUint(value,10,64);if err!=nil{return ErrInvalid};refValue=id
	default:return ErrInvalid
	}
	if field.Unique { digest:=sha256.Sum256(raw);hash=digest[:] }
	_,err=exec.ExecContext(ctx,"INSERT INTO metadata_record_values(tenant_id,record_type,record_id,field_key,field_type,schema_version,value_text,value_long,value_integer,value_decimal,value_boolean,value_date,value_datetime,value_json,value_reference_id,value_hash,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))",tenant,typeKey,recordID,field.Key,field.Type,schemaVersion,textValue,longValue,intValue,decimalValue,boolValue,dateValue,dateTimeValue,jsonValue,refValue,hash)
	if err!=nil&&strings.Contains(strings.ToLower(err.Error()),"metadata_custom_unique"){return FieldError{Field:field.Key,Code:"DUPLICATE",Cause:"value must be unique"}}
	return err
}

func ReadValues(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,recordIDs []uint64)(map[uint64]map[string]json.RawMessage,error){
	out:=map[uint64]map[string]json.RawMessage{}
	if len(recordIDs)==0{return out,nil}
	args:=[]any{tenant,typeKey};for _,id:=range recordIDs{args=append(args,id)}
	marks:=strings.TrimSuffix(strings.Repeat("?,",len(recordIDs)),",")
	rows,err:=exec.QueryContext(ctx,"SELECT record_id,field_key,field_type,value_text,value_long,value_integer,value_decimal,value_boolean,value_date,value_datetime,value_json,value_reference_id FROM metadata_record_values WHERE tenant_id=? AND record_type=? AND record_id IN ("+marks+")",args...)
	if err!=nil{return out,err};defer rows.Close()
	for rows.Next(){
		var id uint64;var key,kind string;var textValue,longValue sql.NullString;var integerValue sql.NullInt64;var decimalValue sql.NullString;var boolValue sql.NullBool;var dateValue,timeValue sql.NullTime;var jsonValue []byte;var referenceValue sql.Null[uint64]
		if err=rows.Scan(&id,&key,&kind,&textValue,&longValue,&integerValue,&decimalValue,&boolValue,&dateValue,&timeValue,&jsonValue,&referenceValue);err!=nil{return out,err}
		var raw []byte
		switch kind {
		case TypeText,TypeChoice:if !textValue.Valid{continue};raw,_=json.Marshal(textValue.String)
		case TypeLongText:if !longValue.Valid{continue};raw,_=json.Marshal(longValue.String)
		case TypeBoolean:if !boolValue.Valid{continue};raw,_=json.Marshal(boolValue.Bool)
		case TypeInteger:if !integerValue.Valid{continue};raw,_=json.Marshal(integerValue.Int64)
		case TypeDecimal:if !decimalValue.Valid{continue};raw=[]byte(canonicalStoredDecimal(decimalValue.String))
		case TypeDate:if !dateValue.Valid{continue};raw,_=json.Marshal(dateValue.Time.Format("2006-01-02"))
		case TypeDateTime:if !timeValue.Valid{continue};raw,_=json.Marshal(timeValue.Time.UTC().Format(time.RFC3339Nano))
		case TypeMultiChoice:if len(jsonValue)==0{continue};raw=append([]byte{},jsonValue...)
		case TypeReference:if !referenceValue.Valid{continue};raw,_=json.Marshal(strconv.FormatUint(referenceValue.V,10))
		}
		if out[id]==nil{out[id]=map[string]json.RawMessage{}}
		out[id][key]=json.RawMessage(raw)
	}
	return out,rows.Err()
}

func canonicalStoredDecimal(value string) string {
	if strings.Contains(value, ".") {
		value = strings.TrimRight(value, "0")
		value = strings.TrimRight(value, ".")
	}
	if value == "" || value == "-0" {
		return "0"
	}
	return value
}

func (s *Service) validateReference(ctx context.Context,exec database.Executor,actor Actor,target string,id uint64)error{
	if id==0{return ErrInvalid}
	var exists bool
	switch target {
	case "user":
		if !canReferenceUsers(actor) || id != actor.UserID && !canListTenantUsers(actor) { return workrecords.ErrForbidden }
		err:=exec.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.user_id=? AND m.active=TRUE AND u.active=TRUE)",actor.TenantID,id).Scan(&exists);if err!=nil{return err}
	case "group":
		if !canReferenceGroups(actor){return workrecords.ErrForbidden}
		query:="SELECT EXISTS(SELECT 1 FROM `groups` g WHERE g.tenant_id=? AND g.id=? AND g.active=TRUE";args:=[]any{actor.TenantID,id};if !canListAllGroups(actor){query+=" AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=g.tenant_id AND gm.group_id=g.id AND gm.user_id=?)";args=append(args,actor.UserID)};query+=")";if err:=exec.QueryRowContext(ctx,query,args...).Scan(&exists);err!=nil{return err}
	case "ci":
		if !actor.Has("cmdb.read")&&!actor.Has("cmdb.read_own"){return workrecords.ErrForbidden}
		query:="SELECT EXISTS(SELECT 1 FROM cmdb_cis c WHERE c.tenant_id=? AND c.id=?";args:=[]any{actor.TenantID,id};if !actor.Has("cmdb.read"){query+=" AND c.owner_id=?";args=append(args,actor.UserID)};query+=")";if err:=exec.QueryRowContext(ctx,query,args...).Scan(&exists);err!=nil{return err}
	case "asset":
		if !actor.Has("assets.read")&&!actor.Has("assets.read_own"){return workrecords.ErrForbidden}
		query:="SELECT EXISTS(SELECT 1 FROM assets a WHERE a.tenant_id=? AND a.id=?";args:=[]any{actor.TenantID,id};if !actor.Has("assets.read"){query+=" AND a.owner_id=?";args=append(args,actor.UserID)};query+=")";if err:=exec.QueryRowContext(ctx,query,args...).Scan(&exists);err!=nil{return err}
	case "incident":
		if !actor.Has("incident.read")&&!actor.Has("incident.read_own"){return workrecords.ErrForbidden}
		query:="SELECT EXISTS(SELECT 1 FROM incidents i WHERE i.tenant_id=? AND i.id=?";args:=[]any{actor.TenantID,id};if !actor.Has("incident.read"){query+=" AND (i.reporter_id=? OR i.affected_user_id=?)";args=append(args,actor.UserID,actor.UserID)};query+=")";if err:=exec.QueryRowContext(ctx,query,args...).Scan(&exists);err!=nil{return err}
	case "request":
		if !actor.Has("request.read_own") && !actor.Has("request.fulfill") && !actor.Has("request.manage") {
			return workrecords.ErrForbidden
		}
		query := "SELECT EXISTS(SELECT 1 FROM service_requests r WHERE r.tenant_id=? AND r.id=?"
		args := []any{actor.TenantID, id}
		if !actor.Has("request.manage") {
			predicate, predicateArgs, ok := requestReferencePredicate(actor)
			if !ok {
				return workrecords.ErrForbidden
			}
			query += " AND " + predicate
			args = append(args, predicateArgs...)
		}
		query += ")"
		if err := exec.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
			return err
		}
	default:return ErrInvalid
	}
	if !exists{return ErrNotFound};return nil
}

func QueryClause(typeKey,alias string,filter QueryFilter,tenantID uint64)(string,[]any,error){
	if (typeKey!="incident"&&typeKey!="ci")||alias!="i"&&alias!="c"{return "",nil,ErrInvalid}
	column,err:=filterColumnForColumn(filter.Column);if err!=nil{return "",nil,err}
	return fmt.Sprintf("EXISTS(SELECT 1 FROM metadata_record_values mv WHERE mv.tenant_id=? AND mv.record_type=? AND mv.record_id=%s.id AND mv.field_key=? AND mv.%s=?)",alias,column),[]any{tenantID,typeKey,filter.Key,filter.Value},nil
}

func filterColumnForColumn(column string)(string,error){switch column{case "value_text","value_integer","value_decimal","value_boolean","value_date","value_datetime","value_reference_id":return column,nil;default:return "",ErrInvalid}}

