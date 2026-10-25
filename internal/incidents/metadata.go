package incidents

import (
	"context"
	"encoding/json"

	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func metadataActor(a Actor) metadata.Actor {
	return metadata.Actor{TenantID:a.TenantID,UserID:a.UserID,Permissions:a.Permissions,RequestID:a.RequestID,IPAddress:a.IPAddress}
}

func (s *Service) tenantMetadata(ctx context.Context,exec database.Executor,a Actor)(uint64,metadata.Configuration,error){
	if s.metadata==nil{return 0,metadata.DefaultConfiguration(""),nil}
	return s.metadata.Published(ctx,exec,a.TenantID)
}

func incidentCategories(config metadata.Configuration) []Category {
	options:=config.Dictionaries["incident.category"]
	out:=make([]Category,0,len(options))
	for _,category:=range options{
		subOptions:=config.Dictionaries["incident.subcategory."+category.Value]
		subs:=make([]Option,0,len(subOptions))
		for _,option:=range subOptions{subs=append(subs,Option{Value:option.Value,Label:option.Labels["pl"],Labels:option.Labels})}
		out=append(out,Category{Value:category.Value,Label:category.Labels["pl"],Labels:category.Labels,Subcategories:subs})
	}
	return out
}

func incidentOptions(config metadata.Configuration,key string)[]Option{
	items:=config.Dictionaries[key];out:=make([]Option,0,len(items));for _,option:=range items{out=append(out,Option{Value:option.Value,Label:option.Labels["pl"],Labels:option.Labels})};return out
}

func baseValueMap(fields Fields) map[string]json.RawMessage {
	out:=map[string]json.RawMessage{}
	for key,value:=range map[string]any{"title":fields.Title,"description":fields.Description,"affected_user_id":fields.AffectedUserID,"category":fields.Category,"subcategory":fields.Subcategory,"impact":fields.Impact,"urgency":fields.Urgency}{raw,err:=json.Marshal(value);if err==nil{out[key]=raw}}
	return out
}

func (s *Service) loadCustomFields(ctx context.Context,exec database.Executor,a Actor,record *Record) error {
	if s.metadata==nil{return nil}
	version,config,err:=s.metadata.Published(ctx,exec,a.TenantID);if err!=nil{return err}
	record.CustomFieldVersion=version
	record.CalculatedPriority,_=CalculatePriorityWith(config.PriorityMatrix,record.Impact,record.Urgency)
	values,err:=metadata.ReadValues(ctx,exec,a.TenantID,"incident",[]uint64{record.ID});if err!=nil{return err}
	record.CustomFields=values[record.ID]
	if record.CustomFields==nil{record.CustomFields=map[string]json.RawMessage{}}
	return nil
}

func (s *Service) prepareCustomFields(ctx context.Context,exec database.Executor,a Actor,id uint64,input map[string]json.RawMessage,base map[string]json.RawMessage,expectedVersion uint64)(map[string]json.RawMessage,metadata.Schema,error){
	if s.metadata==nil{return map[string]json.RawMessage{},metadata.Schema{},nil}
	return s.metadata.PrepareValues(ctx,exec,metadataActor(a),"incident",id,input,base,expectedVersion)
}

func (s *Service) saveCustomFields(ctx context.Context,exec database.Executor,a Actor,id,version uint64,values map[string]json.RawMessage,schema metadata.Schema)error{
	if s.metadata==nil{return nil}
	return s.metadata.SaveValues(ctx,exec,a.TenantID,"incident",id,version,schema,values)
}

