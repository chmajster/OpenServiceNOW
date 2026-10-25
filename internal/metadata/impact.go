package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

const migrationBatch = 500
const maxSynchronousMigrationRows = 10000

type fieldValue struct {
	RecordID uint64
	Value    json.RawMessage
}

func (s *Service) Impact(ctx context.Context, actor Actor) (Impact,error) {
	head,err:=s.Head(ctx,actor);if err!=nil{return Impact{},err}
	prior:=head.Published
	if head.PublishedVersion==0 { prior=DefaultConfiguration(head.Published.Organization.Name) }
	return s.analyzeAndMigrate(ctx,s.DB,actor.TenantID,prior,head.Draft,head.DraftVersion,false)
}

func (s *Service) analyzeAndMigrate(ctx context.Context,exec database.Executor,tenant uint64,prior,next Configuration,version uint64,apply bool)(Impact,error){
	impact:=Impact{CanPublish:true,Changes:[]Change{}}
	dictionaryImpact,err:=analyzeDictionaryChanges(ctx,exec,tenant,prior,next)
	if err!=nil{return impact,err}
	impact.Changes=append(impact.Changes,dictionaryImpact.Changes...)
	if !dictionaryImpact.CanPublish { impact.CanPublish=false }
	for typeKey,nextType:=range next.RecordTypes {
		oldFields:=map[string]Field{};newFields:=map[string]Field{}
		if old,ok:=prior.RecordTypes[typeKey];ok{for _,f:=range old.Fields{oldFields[f.Key]=f}}
		for _,f:=range nextType.Fields{newFields[f.Key]=f}
		for key,newField:=range newFields {
			oldField,exists:=oldFields[key]
			count,err:=fieldValueCount(ctx,exec,tenant,typeKey,key);if err!=nil{return impact,err}
			if exists && oldField.Type!=newField.Type && count>0 {
				values,collisions,tooLarge,err:=s.convertFieldValues(ctx,exec,tenant,typeKey,oldField,newField)
				if err!=nil{return impact,err}
				change:=Change{Key:key,Kind:"TYPE_MIGRATION",Count:count,Examples:collisions,Message:"Stored values must be converted to the new field type."}
				if tooLarge {change.Kind="MIGRATION_LIMIT";change.Message="More than 10,000 stored values require an offline migration."}
				if len(collisions)>0 {change.Kind="TYPE_COLLISIONS";change.Message="Some stored values cannot be converted to the new type."}
				if tooLarge||len(collisions)>0 {impact.CanPublish=false}
				impact.Changes=append(impact.Changes,change)
				if apply&&impact.CanPublish {if err:=s.applyFieldValues(ctx,exec,tenant,typeKey,newField,version,values);err!=nil{return impact,err}}
			} else if (!exists||!oldField.Required)&&newField.Required {
				missing,err:=missingFieldCount(ctx,exec,tenant,typeKey,key);if err!=nil{return impact,err}
				if missing>0 {
					change:=Change{Key:key,Kind:"REQUIRED_FIELD",Count:missing,Message:"Existing records do not have a value for this required field."}
					if newField.Default==nil {impact.CanPublish=false;change.Kind="REQUIRED_FIELD_MISSING";change.Message="Add a validated default or populate these records before publishing."}else if apply {if err:=s.backfillDefault(ctx,exec,tenant,typeKey,newField,version);err!=nil{return impact,err}}
					impact.Changes=append(impact.Changes,change)
				}
			}
			if newField.Unique && (!exists||!oldField.Unique) {
				collisions,tooLarge,err:=s.uniqueCollisions(ctx,exec,tenant,typeKey,newField);if err!=nil{return impact,err}
				if len(collisions)>0||tooLarge {impact.CanPublish=false;kind:="UNIQUE_COLLISIONS";message:="Existing values are not unique.";if tooLarge{kind="MIGRATION_LIMIT";message="More than 10,000 values require an offline uniqueness migration."};impact.Changes=append(impact.Changes,Change{Key:key,Kind:kind,Count:uint64(len(collisions)),Examples:collisions,Message:message})}else if apply {if err:=s.setUnique(ctx,exec,tenant,typeKey,newField,version,true);err!=nil{return impact,err}}
			} else if exists&&oldField.Unique&&!newField.Unique&&apply {
				if _,err:=exec.ExecContext(ctx,"UPDATE metadata_record_values SET value_hash=NULL,schema_version=? WHERE tenant_id=? AND record_type=? AND field_key=?",version,tenant,typeKey,key);err!=nil{return impact,err}
			}
			if exists&&oldField.Type==newField.Type&&apply {if _,err:=exec.ExecContext(ctx,"UPDATE metadata_record_values SET schema_version=? WHERE tenant_id=? AND record_type=? AND field_key=?",version,tenant,typeKey,key);err!=nil{return impact,err}}
		}
	}
	for typeKey,oldType:=range prior.RecordTypes {
		nextType,ok:=next.RecordTypes[typeKey];if !ok{continue}
		active := make(map[string]struct{}, len(nextType.Fields))
		for _, field := range nextType.Fields {
			if field.Active {
				active[field.Key] = struct{}{}
			}
		}
		for _, oldField := range oldType.Fields {
			if !oldField.Active {
				continue
			}
			if _, exists := active[oldField.Key]; exists {
				continue
			}
			count, err := fieldValueCount(ctx, exec, tenant, typeKey, oldField.Key)
			if err != nil {
				return impact, err
			}
			impact.Changes = append(impact.Changes, Change{Key: oldField.Key, Kind: "RETIRED_FIELD", Count: count, Message: "Field is removed from new forms; stored values are retained."})
		}
	}
	sort.Slice(impact.Changes,func(i,j int)bool{if impact.Changes[i].Key==impact.Changes[j].Key{return impact.Changes[i].Kind<impact.Changes[j].Kind};return impact.Changes[i].Key<impact.Changes[j].Key})
	return impact,nil
}

func analyzeDictionaryChanges(ctx context.Context,exec database.Executor,tenant uint64,prior,next Configuration)(Impact,error){
	impact:=Impact{CanPublish:true,Changes:[]Change{}}
	for key,oldOptions:=range prior.Dictionaries {
		newSet:=map[string]bool{};for _,option:=range next.Dictionaries[key]{newSet[option.Value]=true}
		removed:=[]string{};for _,option:=range oldOptions{if !newSet[option.Value]{removed=append(removed,option.Value)}}
		if len(removed)==0{continue}
		table,column,where,whereValue,ok:=dictionaryColumn(key);if !ok{continue}
		marks:=strings.TrimSuffix(strings.Repeat("?,",len(removed)),",")
		query:="SELECT COUNT(*) FROM "+table+" WHERE tenant_id=? AND "+column+" IN ("+marks+")";args:=[]any{tenant};for _,value:=range removed{args=append(args,value)};if where!=""{query+=" AND "+where;args=append(args,whereValue)}
		var count uint64;if err:=exec.QueryRowContext(ctx,query,args...).Scan(&count);err!=nil{return impact,err};if count==0{continue}
		query="SELECT id FROM "+table+" WHERE tenant_id=? AND "+column+" IN ("+marks+")";if where!=""{query+=" AND "+where};query+=" ORDER BY id LIMIT 5"
		rows,err:=exec.QueryContext(ctx,query,args...);if err!=nil{return impact,err};examples:=[]string{};for rows.Next(){var id uint64;if err=rows.Scan(&id);err!=nil{rows.Close();return impact,err};examples=append(examples,fmt.Sprintf("record %d",id))};if err=rows.Err();err!=nil{rows.Close();return impact,err};rows.Close()
		impact.CanPublish=false
		impact.Changes=append(impact.Changes,Change{Key:key,Kind:"DICTIONARY_VALUE_IN_USE",Count:count,Examples:examples,Message:"Existing records use values removed from this dictionary."})
	}
	sort.Slice(impact.Changes,func(i,j int)bool{return impact.Changes[i].Key<impact.Changes[j].Key})
	return impact,nil
}

func dictionaryColumn(key string)(table,column,where string,whereValue string,ok bool){
	switch {
	case key=="incident.category":return "incidents","category","","",true
	case key=="incident.state":return "incidents","state","","",true
	case key=="incident.resolution_code":return "incidents","resolution_code","","",true
	case strings.HasPrefix(key,"incident.subcategory."):
		category:=strings.TrimPrefix(key,"incident.subcategory.");if category==""{return "","","","",false};return "incidents","subcategory","category=?",category,true
	case key=="ci.environment":return "cmdb_cis","environment","","",true
	case key=="ci.status":return "cmdb_cis","status","","",true
	default:return "","","","",false
	}
}

func fieldValueCount(ctx context.Context,exec database.Executor,tenant uint64,typeKey,key string)(uint64,error){var n uint64;err:=exec.QueryRowContext(ctx,"SELECT COUNT(*) FROM metadata_record_values WHERE tenant_id=? AND record_type=? AND field_key=?",tenant,typeKey,key).Scan(&n);return n,err}

func recordIDs(ctx context.Context,exec database.Executor,tenant uint64,typeKey string)([]uint64,error){
	table:="";switch typeKey{case "incident":table="incidents";case "ci":table="cmdb_cis";default:return nil,ErrInvalid}
	rows,err:=exec.QueryContext(ctx,"SELECT id FROM "+table+" WHERE tenant_id=? ORDER BY id",tenant);if err!=nil{return nil,err};defer rows.Close();out:=[]uint64{};for rows.Next(){var id uint64;if err=rows.Scan(&id);err!=nil{return nil,err};out=append(out,id)};return out,rows.Err()
}

func missingFieldCount(ctx context.Context,exec database.Executor,tenant uint64,typeKey,key string)(uint64,error){
	table:="";switch typeKey{case "incident":table="incidents";case "ci":table="cmdb_cis";default:return 0,ErrInvalid}
	var count uint64;err:=exec.QueryRowContext(ctx,"SELECT COUNT(*) FROM "+table+" r WHERE r.tenant_id=? AND NOT EXISTS(SELECT 1 FROM metadata_record_values v WHERE v.tenant_id=r.tenant_id AND v.record_type=? AND v.record_id=r.id AND v.field_key=?)",tenant,typeKey,key).Scan(&count);return count,err
}

func (s *Service) convertFieldValues(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,oldField,newField Field)([]fieldValue,[]string,bool,error){
	ids,err:=recordIDs(ctx,exec,tenant,typeKey);if err!=nil{return nil,nil,false,err};if len(ids)>maxSynchronousMigrationRows{return nil,nil,true,nil}
	stored,err:=ReadValues(ctx,exec,tenant,typeKey,ids);if err!=nil{return nil,nil,false,err}
	values:=[]fieldValue{};collisions:=[]string{}
	for _,id:=range ids {value,ok:=stored[id][oldField.Key];if !ok{continue};converted,err:=convertValue(oldField,newField,value);if err!=nil{if len(collisions)<5{collisions=append(collisions,fmt.Sprintf("record %d",id))};continue};values=append(values,fieldValue{RecordID:id,Value:converted})}
	return values,collisions,false,nil
}

func convertValue(oldField,newField Field,value json.RawMessage)(json.RawMessage,error){
	if oldField.Type==newField.Type{return NormalizeValue(newField,value)}
	var out any
	switch newField.Type {
	case TypeText,TypeLongText:
		if oldField.Type==TypeText||oldField.Type==TypeLongText||oldField.Type==TypeChoice {var text string;if json.Unmarshal(value,&text)!=nil{return nil,ErrInvalid};out=text
		}else if oldField.Type==TypeInteger||oldField.Type==TypeDecimal{out=json.Number(string(value))
		}else if oldField.Type==TypeBoolean{out=string(value)
		}else{return nil,ErrInvalid}
	case TypeInteger,TypeDecimal:
		if oldField.Type==TypeText||oldField.Type==TypeLongText||oldField.Type==TypeChoice {var text string;if json.Unmarshal(value,&text)!=nil{return nil,ErrInvalid};out=json.Number(text)
		}else if oldField.Type==TypeBoolean{if strings.TrimSpace(string(value))=="true"{out=1}else if strings.TrimSpace(string(value))=="false"{out=0}else{return nil,ErrInvalid}
		}else {out=json.Number(string(value))}
	case TypeBoolean:
		if oldField.Type==TypeText||oldField.Type==TypeChoice {var text string;if json.Unmarshal(value,&text)!=nil{return nil,ErrInvalid};parsed,err:=json.Marshal(strings.EqualFold(text,"true")||text=="1");if err!=nil{return nil,err};return NormalizeValue(newField,parsed)}
		return nil,ErrInvalid
	case TypeChoice:
		var text string
		if oldField.Type==TypeMultiChoice {var values []string;if json.Unmarshal(value,&values)!=nil||len(values)!=1{return nil,ErrInvalid};text=values[0]}else if oldField.Type==TypeText||oldField.Type==TypeLongText||oldField.Type==TypeChoice {if json.Unmarshal(value,&text)!=nil{return nil,ErrInvalid}}else{return nil,ErrInvalid}
		out=text
	case TypeMultiChoice:
		if oldField.Type==TypeChoice {var text string;if json.Unmarshal(value,&text)!=nil{return nil,ErrInvalid};out=[]string{text}} else if oldField.Type==TypeMultiChoice {return NormalizeValue(newField,value)} else{return nil,ErrInvalid}
	default:return nil,ErrInvalid
	}
	raw,err:=json.Marshal(out);if err!=nil{return nil,err};return NormalizeValue(newField,raw)
}

func (s *Service) applyFieldValues(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,field Field,version uint64,values []fieldValue)error{
	if len(values)==0{return nil}
	if _,err:=exec.ExecContext(ctx,"DELETE FROM metadata_record_values WHERE tenant_id=? AND record_type=? AND field_key=?",tenant,typeKey,field.Key);err!=nil{return err}
	for start:=0;start<len(values);start+=migrationBatch{end:=start+migrationBatch;if end>len(values){end=len(values)};for _,item:=range values[start:end]{if err:=saveValue(ctx,exec,tenant,typeKey,item.RecordID,field,version,item.Value);err!=nil{return err}}}
	return nil
}

func (s *Service) backfillDefault(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,field Field,version uint64)error{
	if field.Default==nil{return ErrInvalid};ids,err:=recordIDs(ctx,exec,tenant,typeKey);if err!=nil{return err};if len(ids)>maxSynchronousMigrationRows{return ImpactError{Impact:Impact{CanPublish:false,Changes:[]Change{{Key:field.Key,Kind:"MIGRATION_LIMIT",Count:uint64(len(ids)),Message:"Default backfill requires an offline migration."}}}}}
	if _,err=NormalizeValue(field,field.Default);err!=nil{return err}
	for start:=0;start<len(ids);start+=migrationBatch{end:=start+migrationBatch;if end>len(ids){end=len(ids)};subset:=ids[start:end];read,err:=ReadValues(ctx,exec,tenant,typeKey,subset);if err!=nil{return err};for _,id:=range subset{if _,exists:=read[id][field.Key];exists{continue};if err=saveValue(ctx,exec,tenant,typeKey,id,field,version,field.Default);err!=nil{return err}}}
	return nil
}

func (s *Service) uniqueCollisions(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,field Field)([]string,bool,error){
	ids,err:=recordIDs(ctx,exec,tenant,typeKey);if err!=nil{return nil,false,err};if len(ids)>maxSynchronousMigrationRows{return nil,true,nil}
	stored,err:=ReadValues(ctx,exec,tenant,typeKey,ids);if err!=nil{return nil,false,err};seen:=map[[32]byte]uint64{};duplicates:=[]string{}
	for _,id:=range ids{value,ok:=stored[id][field.Key];if !ok{continue};normalized,err:=NormalizeValue(field,value);if err!=nil{if len(duplicates)<5{duplicates=append(duplicates,fmt.Sprintf("record %d: invalid value",id))};continue};hash:=sha256.Sum256(normalized);if previous,ok:=seen[hash];ok{if len(duplicates)<5{duplicates=append(duplicates,fmt.Sprintf("records %d and %d",previous,id))}}else{seen[hash]=id}}
	return duplicates,false,nil
}

func (s *Service) setUnique(ctx context.Context,exec database.Executor,tenant uint64,typeKey string,field Field,version uint64,unique bool)error{
	ids,err:=recordIDs(ctx,exec,tenant,typeKey);if err!=nil{return err};if len(ids)>maxSynchronousMigrationRows{return ErrConflict}
	stored,err:=ReadValues(ctx,exec,tenant,typeKey,ids);if err!=nil{return err}
	for _,id:=range ids{value,ok:=stored[id][field.Key];if !ok{continue};if unique{normalized,err:=NormalizeValue(field,value);if err!=nil{return err};value=normalized;hash:=sha256.Sum256(value);if _,err=exec.ExecContext(ctx,"UPDATE metadata_record_values SET value_hash=?,schema_version=? WHERE tenant_id=? AND record_type=? AND record_id=? AND field_key=?",hash[:],version,tenant,typeKey,id,field.Key);err!=nil{return err}}else if _,err=exec.ExecContext(ctx,"UPDATE metadata_record_values SET value_hash=NULL,schema_version=? WHERE tenant_id=? AND record_type=? AND record_id=? AND field_key=?",version,tenant,typeKey,id,field.Key);err!=nil{return err}}
	return nil
}

