package incidents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) majorRoot(ctx context.Context, exec database.Executor, tenant, incidentID uint64) (uint64, MajorCase, error) {
	item, err := s.majorCase(ctx, exec, tenant, incidentID, false)
	if err == nil { return incidentID, item, nil }
	if !errors.Is(err, ErrNotFound) { return 0, MajorCase{}, err }
	var parent uint64
	err = exec.QueryRowContext(ctx, `SELECT CAST(target_id AS UNSIGNED) FROM work_relations WHERE tenant_id=? AND source_type='incident' AND target_type='incident' AND relation_type='PARENT' AND source_id=? ORDER BY id LIMIT 1`, tenant, strconv.FormatUint(incidentID,10)).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) { return 0, MajorCase{}, ErrNotFound }
	if err != nil { return 0, MajorCase{}, err }
	item, err = s.majorCase(ctx,exec,tenant,parent,false)
	return parent,item,err
}

func (s *Service) canReadMajorPublic(ctx context.Context,e database.Executor,a Actor,rootID,currentID uint64,policy string)bool{
	if a.Has("incident.major.read")||a.Has("incident.major.manage")||a.Has("read"){return true}
	var allowed bool
	switch policy{
	case "REPORTERS_AND_AFFECTED":
		_ = e.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM incidents WHERE tenant_id=? AND id IN(?,?) AND (reporter_id=? OR affected_user_id=?))`,a.TenantID,rootID,currentID,a.UserID,a.UserID).Scan(&allowed)
	case "WATCHERS":
		_ = e.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM work_watchers WHERE tenant_id=? AND object_type='incident' AND object_id IN(?,?) AND user_id=?)`,a.TenantID,strconv.FormatUint(rootID,10),strconv.FormatUint(currentID,10),a.UserID).Scan(&allowed)
	case "ALL_LINKED":
		_ = e.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM incidents i WHERE i.tenant_id=? AND i.id IN(?,?) AND (i.reporter_id=? OR i.affected_user_id=?)) OR EXISTS(SELECT 1 FROM work_watchers w WHERE w.tenant_id=? AND w.object_type='incident' AND w.object_id IN(?,?) AND w.user_id=?)`,a.TenantID,rootID,currentID,a.UserID,a.UserID,a.TenantID,strconv.FormatUint(rootID,10),strconv.FormatUint(currentID,10),a.UserID).Scan(&allowed)
	}
	return allowed
}

func (s *Service) majorUpdates(ctx context.Context,e database.Executor,a Actor,rootID,currentID uint64,private bool)([]MajorUpdate,error){
	query:=`SELECT u.id,u.audience,u.body,u.actor_user_id,COALESCE(p.display_name,''),u.delivery_state,u.occurred_at FROM incident_major_updates u JOIN users p ON p.id=u.actor_user_id WHERE u.tenant_id=? AND u.incident_id=?`
	if private{query+=` AND u.audience='OPERATORS'`}else{query+=` AND u.audience='REQUESTERS'`}
	query+=` ORDER BY u.id DESC LIMIT 100`
	rows,err:=e.QueryContext(ctx,query,a.TenantID,rootID);if err!=nil{return nil,err};defer rows.Close();out:=[]MajorUpdate{}
	for rows.Next(){var item MajorUpdate;if err:=rows.Scan(&item.ID,&item.Audience,&item.Body,&item.ActorUserID,&item.ActorName,&item.Delivery,&item.OccurredAt);err!=nil{return nil,err};if private||s.canReadMajorPublic(ctx,e,a,rootID,currentID,"ALL_LINKED"){out=append(out,item)}}
	return out,rows.Err()
}

func (s *Service) MajorView(ctx context.Context,a Actor,id uint64)(MajorView,error){
	if _,err:=s.Get(ctx,a,id);err!=nil{return MajorView{},err}
	root,item,err:=s.majorRoot(ctx,s.repo.db,a.TenantID,id)
	if errors.Is(err,ErrNotFound){return MajorView{PublicUpdates:[]MajorUpdate{}},nil};if err!=nil{return MajorView{},err}
	if item.Status!="ACTIVE"&&item.Status!="RESTORED"{
		if a.Has("incident.major.read")||a.Has("incident.major.manage") { if err:=loadMajorAssociations(ctx,s.repo.db,a.TenantID,&item);err!=nil{return MajorView{},err};return MajorView{Case:&item,PublicUpdates:[]MajorUpdate{},InternalUpdates:[]MajorUpdate{}},nil }
		return MajorView{PublicUpdates:[]MajorUpdate{}},nil
	}
	if !s.canReadMajorPublic(ctx,s.repo.db,a,root,id,item.CommunicationAudience){return MajorView{Restricted:true,PublicUpdates:[]MajorUpdate{}},nil}
	public,err:=s.majorUpdates(ctx,s.repo.db,a,root,id,false);if err!=nil{return MajorView{},err}
	view:=MajorView{PublicUpdates:public}
	if !a.Has("incident.major.read")&&!a.Has("incident.major.manage"){
		view.Restricted=true
		view.Case=&MajorCase{IncidentID:root,Number:item.Number,Title:item.Title,Status:item.Status,IncidentState:item.IncidentState,Priority:item.Priority,UpdatedAt:item.UpdatedAt}
		return view,nil
	}
	if err:=loadMajorAssociations(ctx,s.repo.db,a.TenantID,&item);err!=nil{return MajorView{},err}
	view.Case=&item
	view.InternalUpdates,err=s.majorUpdates(ctx,s.repo.db,a,root,id,true);if err!=nil{return MajorView{},err}
	view.Timeline,err=s.majorTimeline(ctx,a.TenantID,root);if err!=nil{return MajorView{},err}
	view.Review,err=s.majorReview(ctx,s.repo.db,a.TenantID,root);if err!=nil&&!errors.Is(err,ErrNotFound){return MajorView{},err}
	view.Actions,err=s.majorActions(ctx,s.repo.db,a.TenantID,root,100);if err!=nil{return MajorView{},err}
	return view,nil
}

func (s *Service) majorTimeline(ctx context.Context,tenant,incident uint64)([]MajorDecision,error){
	rows,err:=s.repo.db.QueryContext(ctx,`SELECT d.id,d.case_version,d.decision_type,d.reason,d.actor_user_id,u.display_name,d.occurred_at FROM incident_major_decisions d JOIN users u ON u.id=d.actor_user_id WHERE d.tenant_id=? AND d.incident_id=? ORDER BY d.id DESC LIMIT 100`,tenant,incident);if err!=nil{return nil,err};defer rows.Close();out:=[]MajorDecision{};for rows.Next(){var item MajorDecision;if err:=rows.Scan(&item.ID,&item.Version,&item.Type,&item.Reason,&item.ActorUserID,&item.ActorName,&item.OccurredAt);err!=nil{return nil,err};out=append(out,item)};return out,rows.Err()
}

func (s *Service) majorReview(ctx context.Context,e database.Executor,tenant,incident uint64)(*MajorReview,error){
	var item MajorReview;var complete sql.NullTime
	err:=e.QueryRowContext(ctx,`SELECT detected_at,restored_at,impact_summary,known_causes,what_worked,completed_at,version FROM incident_major_reviews WHERE tenant_id=? AND incident_id=?`,tenant,incident).Scan(&item.DetectedAt,&item.RestoredAt,&item.ImpactSummary,&item.KnownCauses,&item.WhatWorked,&complete,&item.Version)
	if errors.Is(err,sql.ErrNoRows){return nil,ErrNotFound};if err!=nil{return nil,err};if complete.Valid{item.CompletedAt=&complete.Time};return &item,nil
}

func (s *Service) majorActions(ctx context.Context,e database.Executor,tenant,incident uint64,limit int)([]MajorAction,error){
	rows,err:=e.QueryContext(ctx,`SELECT ac.id,ac.incident_id,i.number,ac.description,ac.owner_user_id,COALESCE(u.display_name,''),ac.due_at,ac.status,ac.version,ac.updated_at FROM incident_major_actions ac JOIN incidents i ON i.tenant_id=ac.tenant_id AND i.id=ac.incident_id LEFT JOIN users u ON u.id=ac.owner_user_id WHERE ac.tenant_id=? AND ac.incident_id=? ORDER BY ac.id DESC LIMIT ?`,tenant,incident,limit);if err!=nil{return nil,err};defer rows.Close();out:=[]MajorAction{};for rows.Next(){var item MajorAction;var owner sql.Null[uint64];if err:=rows.Scan(&item.ID,&item.IncidentID,&item.Number,&item.Description,&owner,&item.OwnerName,&item.DueAt,&item.Status,&item.Version,&item.UpdatedAt);err!=nil{return nil,err};if owner.Valid{item.OwnerUserID=&owner.V};out=append(out,item)};return out,rows.Err()
}

func (s *Service) PublishMajorUpdate(ctx context.Context,a Actor,id uint64,input MajorUpdateInput,key string)(MajorUpdate,error){
	input.Audience=strings.TrimSpace(input.Audience);input.Body=strings.TrimSpace(input.Body)
	if !a.Has("incident.major.manage")||!requestKeyPattern.MatchString(key)||input.Version==0||(input.Audience!="OPERATORS"&&input.Audience!="REQUESTERS")||!validText(input.Body,1,20000){return MajorUpdate{},ErrInvalid}
	digest,err:=majorRequestHash(id,"update",input);if err!=nil{return MajorUpdate{},err};var result MajorUpdate
	err=s.majorTx(ctx,a,id,"incident.major.manage",func(exec database.Executor,incident Record)error{
		replay,err:=s.majorReplay(ctx,exec,a,key,"update",digest);if err!=nil{return err};if replay{err=exec.QueryRowContext(ctx,`SELECT mu.id,mu.audience,mu.body,mu.actor_user_id,p.display_name,mu.delivery_state,mu.occurred_at FROM incident_major_updates mu JOIN users p ON p.id=mu.actor_user_id WHERE mu.tenant_id=? AND mu.id=(SELECT resource_id FROM incident_collaboration_requests WHERE tenant_id=? AND user_id=? AND request_key=?)`,a.TenantID,a.TenantID,a.UserID,key).Scan(&result.ID,&result.Audience,&result.Body,&result.ActorUserID,&result.ActorName,&result.Delivery,&result.OccurredAt);return err}
		caseItem,err:=s.majorCase(ctx,exec,a.TenantID,id,true);if err!=nil{return err};if caseItem.Version!=input.Version{return ErrConflict};if caseItem.Status!="ACTIVE"{return ErrTransition}
		now:=s.now().UTC().Truncate(time.Microsecond);state:="PENDING";if input.Audience=="OPERATORS"{state="IN_APP_DELIVERED"}
		ins,err:=exec.ExecContext(ctx,`INSERT INTO incident_major_updates(tenant_id,incident_id,audience,body,actor_user_id,delivery_state,occurred_at) VALUES(?,?,?,?,?,?,?)`,a.TenantID,id,input.Audience,input.Body,a.UserID,state,now);if err!=nil{return err};updateID,err:=ins.LastInsertId();if err!=nil{return err}
		kind,visibility:="incident.major.update.internal","INTERNAL";if input.Audience=="REQUESTERS"{kind,visibility="incident.major.update.published","PUBLIC"}
		eventID,err:=s.recordMajorEvent(ctx,exec,a,id,caseItem.Version,kind,visibility,"",uint64(updateID),input.Audience,now);if err!=nil{return err};if _,err=exec.ExecContext(ctx,`UPDATE incident_major_updates SET source_event_id=? WHERE tenant_id=? AND id=?`,eventID,a.TenantID,updateID);err!=nil{return err};if err=s.repo.completeCollaboration(ctx,exec,a,key,uint64(updateID));err!=nil{return err}
		result=MajorUpdate{ID:uint64(updateID),Audience:input.Audience,Body:input.Body,ActorUserID:a.UserID,ActorName:"",Delivery:state,OccurredAt:now};return nil
	})
	return result,err
}

func (s *Service) MarkMajorRestored(ctx context.Context,a Actor,id uint64,input MajorRestoreInput,key string)(MajorCase,error){
	input.Reason=strings.TrimSpace(input.Reason);if !a.Has("incident.major.manage")||!requestKeyPattern.MatchString(key)||input.Version==0||input.DetectedAt.IsZero()||input.RestoredAt.IsZero()||input.RestoredAt.Before(input.DetectedAt)||!validText(input.Reason,1,2000){return MajorCase{},ErrInvalid}
	digest,err:=majorRequestHash(id,"restore",input);if err!=nil{return MajorCase{},err};var result MajorCase
	err=s.majorTx(ctx,a,id,"incident.major.manage",func(exec database.Executor,incident Record)error{
		replay,err:=s.majorReplay(ctx,exec,a,key,"restore",digest);if err!=nil{return err};if replay{result,err=s.majorCase(ctx,exec,a.TenantID,id,false);return err};current,err:=s.majorCase(ctx,exec,a.TenantID,id,true);if err!=nil{return err};if current.Version!=input.Version{return ErrConflict};if current.Status!="ACTIVE"||incident.State!="RESOLVED"{return ErrTransition};if input.DetectedAt.After(incident.ResolvedAtOrCreated()){return ErrInvalid};now:=s.now().UTC().Truncate(time.Microsecond)
		if _,err=exec.ExecContext(ctx,`UPDATE incident_major_cases SET status='RESTORED',decision_reason=?,decided_by=?,decided_at=?,detected_at=?,restored_at=?,next_update_at=NULL,version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=? AND version=?`,input.Reason,a.UserID,now,input.DetectedAt.UTC(),input.RestoredAt.UTC(),now,a.TenantID,id,input.Version);err!=nil{return err};result,err=s.majorCase(ctx,exec,a.TenantID,id,false);if err!=nil{return err};if err=insertMajorDecision(ctx,exec,a,id,result.Version,"RESTORED",input.Reason,now);err!=nil{return err};if err=s.repo.completeCollaboration(ctx,exec,a,key,id);err!=nil{return err};_,err=s.recordMajorEvent(ctx,exec,a,id,result.Version,"incident.major.restored","INTERNAL",input.Reason,0,"",now);return err
	});return result,err
}

// ResolvedAtOrCreated returns a stable incident time bound for major restoration validation.
func (r Record) ResolvedAtOrCreated() time.Time { if r.ResolvedAt!=nil{return *r.ResolvedAt};return r.CreatedAt }

func (s *Service) SaveMajorReview(ctx context.Context,a Actor,id uint64,input MajorReviewInput,key string)(MajorReview,error){
	input.ImpactSummary=strings.TrimSpace(input.ImpactSummary);input.KnownCauses=strings.TrimSpace(input.KnownCauses);input.WhatWorked=strings.TrimSpace(input.WhatWorked)
	if !a.Has("incident.major.manage")||!requestKeyPattern.MatchString(key)||input.DetectedAt.IsZero()||input.RestoredAt.IsZero()||input.RestoredAt.Before(input.DetectedAt)||len(input.Actions)>50||len([]rune(input.ImpactSummary))>8000||len([]rune(input.KnownCauses))>8000||len([]rune(input.WhatWorked))>8000{return MajorReview{},ErrInvalid}
	if input.Complete&&(!validText(input.ImpactSummary,1,8000)||!validText(input.KnownCauses,1,8000)||!validText(input.WhatWorked,1,8000)){return MajorReview{},ErrInvalid}
	for _,action:=range input.Actions{action.Description=strings.TrimSpace(action.Description);if !validText(action.Description,1,4000)||action.DueAt.IsZero()||action.OwnerUserID==nil&&input.Complete{return MajorReview{},ErrInvalid}}
	digest,err:=majorRequestHash(id,"review",input);if err!=nil{return MajorReview{},err};var result MajorReview
	err=s.majorTx(ctx,a,id,"incident.major.manage",func(exec database.Executor,incident Record)error{
		replay,err:=s.majorReplay(ctx,exec,a,key,"review",digest);if err!=nil{return err};if replay{r,err:=s.majorReview(ctx,exec,a.TenantID,id);if err==nil&&r!=nil{result=*r};return err}
		major,err:=s.majorCase(ctx,exec,a.TenantID,id,true);if err!=nil{return err};if major.Status!="RESTORED"{return ErrTransition};if major.DetectedAt==nil||major.RestoredAt==nil||!input.DetectedAt.Equal(*major.DetectedAt)||!input.RestoredAt.Equal(*major.RestoredAt){return ErrInvalid}
		previous,err:=s.majorReview(ctx,exec,a.TenantID,id);if err!=nil&&!errors.Is(err,ErrNotFound){return err};now:=s.now().UTC().Truncate(time.Microsecond);version:=uint64(1);var completeAt any;if input.Complete{completeAt=now}
		if previous==nil{if input.Version!=0{return ErrConflict};_,err=exec.ExecContext(ctx,`INSERT INTO incident_major_reviews(tenant_id,incident_id,detected_at,restored_at,impact_summary,known_causes,what_worked,completed_at,completed_by,version,updated_by,updated_at) VALUES(?,?,?,?,?,?,?,?,?,1,?,?)`,a.TenantID,id,input.DetectedAt.UTC(),input.RestoredAt.UTC(),input.ImpactSummary,input.KnownCauses,input.WhatWorked,completeAt,nullableUser(input.Complete,a.UserID),a.UserID,now);if err!=nil{return err}}else{if previous.Version!=input.Version{return ErrConflict};if previous.CompletedAt!=nil{return ErrTransition};version=previous.Version+1;_,err=exec.ExecContext(ctx,`UPDATE incident_major_reviews SET impact_summary=?,known_causes=?,what_worked=?,completed_at=?,completed_by=?,version=?,updated_by=?,updated_at=? WHERE tenant_id=? AND incident_id=? AND version=?`,input.ImpactSummary,input.KnownCauses,input.WhatWorked,completeAt,nullableUser(input.Complete,a.UserID),version,a.UserID,now,a.TenantID,id,input.Version);if err!=nil{return err}}
		for _,action:=range input.Actions{if err:=validateMajorPeople(ctx,exec,a.TenantID,action.OwnerUserID);err!=nil{return err};status:=action.Status;if status==""{status="OPEN"};if status!="OPEN"&&status!="IN_PROGRESS"{return ErrInvalid};if action.ID==0{_,err=exec.ExecContext(ctx,`INSERT INTO incident_major_actions(tenant_id,incident_id,description,owner_user_id,due_at,status,version,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,?,?)`,a.TenantID,id,action.Description,action.OwnerUserID,action.DueAt.UTC(),status,a.UserID,now,now)}else{if action.Version==0{return ErrInvalid};res,e:=exec.ExecContext(ctx,`UPDATE incident_major_actions SET description=?,owner_user_id=?,due_at=?,status=?,version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=? AND id=? AND version=? AND status IN('OPEN','IN_PROGRESS')`,action.Description,action.OwnerUserID,action.DueAt.UTC(),status,now,a.TenantID,id,action.ID,action.Version);if e!=nil{return e};n,e:=res.RowsAffected();if e!=nil{return e};if n!=1{return ErrConflict}};if err!=nil{return err}}
		result=MajorReview{DetectedAt:input.DetectedAt.UTC(),RestoredAt:input.RestoredAt.UTC(),ImpactSummary:input.ImpactSummary,KnownCauses:input.KnownCauses,WhatWorked:input.WhatWorked,Version:version};if input.Complete{result.CompletedAt=&now};if err=s.repo.completeCollaboration(ctx,exec,a,key,version);err!=nil{return err};_,err=s.recordMajorEvent(ctx,exec,a,id,major.Version,"incident.major.review.saved","INTERNAL","",version,"",now);return err
	});return result,err
}

func nullableUser(use bool,user uint64)any{if use{return user};return nil}

func (s *Service) MajorChildren(ctx context.Context,a Actor,parent uint64,input MajorChildrenInput,key string)([]MajorChildResult,error){
	input.Message=strings.TrimSpace(input.Message);if !a.Has("incident.major.manage")||!requestKeyPattern.MatchString(key)||input.Version==0||input.Action!="message"&&input.Action!="solution"||!validText(input.Message,1,20000)||len(input.Items)<1||len(input.Items)>50{return nil,ErrInvalid};seen:=map[uint64]bool{};for _,target:=range input.Items{if target.ID==0||target.Version==0||seen[target.ID]{return nil,ErrInvalid};seen[target.ID]=true}
	caseItem,err:=s.majorCase(ctx,s.repo.db,a.TenantID,parent,false);if err!=nil{return nil,err};if caseItem.Version!=input.Version||caseItem.Status!="ACTIVE"{return nil,ErrConflict}
	results:=make([]MajorChildResult,0,len(input.Items));for _,target:=range input.Items{message:=input.Message;if input.Action=="solution"{message="Propozycja rozwiązania awarii: "+message};sum:=sha256.Sum256([]byte(fmt.Sprintf("%s:%d",key,target.ID)));childKey:="major-child-"+hex.EncodeToString(sum[:24]);err:=s.majorChildMessage(ctx,a,parent,target,caseItem.Version,message,childKey);status:=200;text:="Zaktualizowano";if err!=nil{status=errorStatus(err);text=err.Error()};results=append(results,MajorChildResult{ID:target.ID,Status:status,Message:text})};return results,nil
}

func(s *Service)majorChildMessage(ctx context.Context,a Actor,parent uint64,target MajorChildTarget,parentVersion uint64,body,key string)error{
	if !a.Has("communicate"){return ErrForbidden};input:=CommunicationInput{Type:workrecords.MessageComment,Body:body};raw,_:=json.Marshal(struct{Parent uint64;Target MajorChildTarget;Input CommunicationInput}{parent,target,input});hash:=sha256.Sum256(raw)
	return s.communicationTx(ctx,a,target.ID,func(exec database.Executor,child Record)error{
		if child.Version!=target.Version||child.State=="CLOSED"||child.State=="CANCELLED"{return ErrConflict};var related bool;if err:=exec.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM work_relations WHERE tenant_id=? AND source_type='incident' AND target_type='incident' AND relation_type='PARENT' AND source_id=? AND target_id=?)`,a.TenantID,strconv.FormatUint(target.ID,10),strconv.FormatUint(parent,10)).Scan(&related);err!=nil{return err};if !related{return ErrNotFound};major,err:=s.majorCase(ctx,exec,a.TenantID,parent,true);if err!=nil{return err};if major.Version!=parentVersion||major.Status!="ACTIVE"{return ErrConflict};existing,err:=s.repo.reserveCollaboration(ctx,exec,a,key,"major-child-message",hash);if err!=nil{return err};if existing>0{return nil};message,err:=s.work.AppendMessageInTx(ctx,exec,workActor(a),"incident",strconv.FormatUint(target.ID,10),workrecords.MessageComment,body);if err!=nil{return err};if err=s.repo.completeCollaboration(ctx,exec,a,key,message.ID);err!=nil{return err};return s.collaborationEvent(ctx,exec,a,child,"work.comment.added","PUBLIC",fmt.Sprintf("incident-message:%d",message.ID),map[string]any{"resource_id":strconv.FormatUint(message.ID,10),"message_id":strconv.FormatUint(message.ID,10),"message_type":workrecords.MessageComment,"body":body})
	})
}

func(s *Service)ScheduleMajorReminders(ctx context.Context,now time.Time)(int,error){
	if now.IsZero(){now=time.Now().UTC()};now=now.UTC().Truncate(time.Microsecond);rows,err:=s.repo.db.QueryContext(ctx,`SELECT tenant_id,incident_id FROM incident_major_cases WHERE status='ACTIVE' AND next_update_at<=? ORDER BY next_update_at,tenant_id,incident_id LIMIT 100`,now);if err!=nil{return 0,err};type target struct{tenant,id uint64};targets:=[]target{};for rows.Next(){var item target;if err=rows.Scan(&item.tenant,&item.id);err!=nil{rows.Close();return 0,err};targets=append(targets,item)};if err=rows.Err();err!=nil{rows.Close();return 0,err};rows.Close();scheduled:=0
	for _,item:=range targets{err=s.tx.WithTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted},func(exec database.Executor)error{current,e:=s.majorCase(ctx,exec,item.tenant,item.id,true);if errors.Is(e,ErrNotFound){return nil};if e!=nil{return e};if current.Status!="ACTIVE"||current.NextUpdateAt==nil||current.NextUpdateAt.After(now){return nil};next:=now.Add(time.Duration(current.UpdateIntervalMinutes)*time.Minute);if _,e=exec.ExecContext(ctx,`UPDATE incident_major_cases SET next_update_at=?,version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=? AND version=?`,next,now,item.tenant,item.id,current.Version);e!=nil{return e};systemActor:=Actor{TenantID:item.tenant};_,e=s.recordMajorEvent(ctx,exec,systemActor,item.id,current.Version+1,"incident.major.reminder.due","INTERNAL","",0,"",now);if e==nil{scheduled++};return e});if err!=nil{return scheduled,err}}
	return scheduled,nil
}

func validateExternalBridge(value string) bool { parsed,err:=url.Parse(value);return err==nil&&parsed.Scheme=="https"&&parsed.Hostname()!=""&&parsed.User==nil }

func idempotencyHashForKey(key string)string{sum:=sha256.Sum256([]byte(key));return hex.EncodeToString(sum[:])}

func parseMajorID(raw string)(uint64,error){id,err:=strconv.ParseUint(raw,10,64);if err!=nil||id==0||strconv.FormatUint(id,10)!=raw{return 0,ErrInvalid};return id,nil}

