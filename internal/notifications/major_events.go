package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (s *Service) handleMajorEvent(ctx context.Context,event events.Event)error{
	if event.Version!=1||event.TenantID==0||event.AggregateType!="incident"{return events.ErrInvalid}
	id,err:=strconv.ParseUint(event.AggregateID,10,64);if err!=nil||id==0{return events.ErrInvalid}
	var payload incidents.MajorDomainEvent;if err=json.Unmarshal(event.Payload,&payload);err!=nil{return events.ErrInvalid};if payload.IncidentID!=id||payload.CaseVersion==0||payload.OccurredAt.IsZero(){return events.ErrInvalid}
	if event.Type=="incident.major.update.published"&&(payload.ResourceID==0||payload.Audience!="REQUESTERS"||payload.Visibility!="PUBLIC"||payload.ActorUserID==0){return events.ErrInvalid}
	if event.Type=="incident.major.reminder.due"&&(payload.ActorUserID!=0||payload.Visibility!="INTERNAL"){return events.ErrInvalid}
	return s.tx.WithTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted},func(e database.Executor)error{
		var state string;if err:=e.QueryRowContext(ctx,`SELECT status FROM tenants WHERE id=? FOR UPDATE`,event.TenantID).Scan(&state);err!=nil{return err};if state!="ACTIVE"{return nil}
		var caseStatus,audience string;var commander,deputy,comms sql.Null[uint64];if err:=e.QueryRowContext(ctx,`SELECT status,communication_audience,commander_user_id,deputy_user_id,communications_owner_id FROM incident_major_cases WHERE tenant_id=? AND incident_id=? FOR UPDATE`,event.TenantID,id).Scan(&caseStatus,&audience,&commander,&deputy,&comms);err!=nil{if errors.Is(err,sql.ErrNoRows){return nil};return err}
		if event.Type=="incident.major.reminder.due"{if caseStatus!="ACTIVE"{return nil};return s.deliverMajorReminder(ctx,e,event,payload,id,commander,deputy,comms)}
		var updateAudience,delivery string;var updateIncident uint64
		if err:=e.QueryRowContext(ctx,`SELECT incident_id,audience,delivery_state FROM incident_major_updates WHERE tenant_id=? AND id=? AND source_event_id=?`,event.TenantID,payload.ResourceID,event.ID).Scan(&updateIncident,&updateAudience,&delivery);err!=nil{if errors.Is(err,sql.ErrNoRows){return nil};return err}
		if updateAudience!="REQUESTERS"||caseStatus!="ACTIVE"&&caseStatus!="RESTORED"{_,err=e.ExecContext(ctx,`UPDATE incident_major_updates SET delivery_state='NO_RECIPIENTS' WHERE tenant_id=? AND id=? AND delivery_state='PENDING'`,event.TenantID,payload.ResourceID);return err}
		candidates,err:=s.majorRecipients(ctx,e,event.TenantID,updateIncident,audience);if err!=nil{return err}
		inApp,mail,eligible:=false,false,false
		for user,incidentID:=range candidates{if user==payload.ActorUserID{continue};actor,err:=s.currentActor(ctx,e,event.TenantID,user);if err!=nil{return err};record,err:=incidents.NewRepository(s.db).Get(ctx,e,event.TenantID,incidentID,false);if errors.Is(err,incidents.ErrNotFound)||err==nil&&!incidents.CanRead(actor,record){continue};if err!=nil{return err};eligible=true
			pref,err:=s.preference(ctx,e,event.TenantID,user);if err!=nil{return err};if !pref.Replies||!pref.InApp&&!pref.SMTP{continue}
			_,err=e.ExecContext(ctx,`INSERT INTO notifications(tenant_id,user_id,source_event_id,incident_id,kind,visibility,summary,in_app_enabled,occurred_at) VALUES(?,?,?,?,?,'PUBLIC',?,?,?) ON DUPLICATE KEY UPDATE id=id`,event.TenantID,user,event.ID,incidentID,"reply","Opublikowano aktualizację dużej awarii",pref.InApp,payload.OccurredAt.UTC());if err!=nil{return err}
			var notificationID uint64;if err=e.QueryRowContext(ctx,`SELECT id FROM notifications WHERE tenant_id=? AND user_id=? AND source_event_id=?`,event.TenantID,user,event.ID).Scan(&notificationID);err!=nil{return err};if pref.SMTP{if _,err=s.createMail(ctx,e,event.TenantID,user,notificationID);err!=nil{return err};mail=true};if pref.InApp{inApp=true}
		}
		result:="NO_RECIPIENTS";if eligible&&!inApp&&!mail{result="SKIPPED_BY_PREFERENCE"};if mail{result="SMTP_QUEUED"};if inApp{result="IN_APP_DELIVERED"}
		_,err=e.ExecContext(ctx,`UPDATE incident_major_updates SET delivery_state=? WHERE tenant_id=? AND id=? AND delivery_state='PENDING'`,result,event.TenantID,payload.ResourceID);return err
	})
}

// majorRecipients applies the tenant's communication policy before current
// record authorization is evaluated. It returns only one accessible record
// per user, which also keeps notification idempotency scoped to the event.
func(s *Service)majorRecipients(ctx context.Context,e database.Executor,tenant,root uint64,policy string)(map[uint64]uint64,error){
	ids:=[]uint64{root};if policy=="ALL_LINKED"||policy=="WATCHERS"{rows,err:=e.QueryContext(ctx,`SELECT CAST(source_id AS UNSIGNED) FROM work_relations WHERE tenant_id=? AND source_type='incident' AND target_type='incident' AND relation_type='PARENT' AND target_id=? ORDER BY id LIMIT 100`,tenant,strconv.FormatUint(root,10));if err!=nil{return nil,err};for rows.Next(){var id uint64;if err=rows.Scan(&id);err!=nil{rows.Close();return nil,err};ids=append(ids,id)};if err=rows.Err();err!=nil{rows.Close();return nil,err};rows.Close()}
	out:=map[uint64]uint64{};repo:=incidents.NewRepository(s.db)
	for _,incidentID:=range ids{record,err:=repo.Get(ctx,e,tenant,incidentID,false);if errors.Is(err,incidents.ErrNotFound){continue};if err!=nil{return nil,err};users:=map[uint64]bool{}
		if policy=="ALL_LINKED"||policy=="REPORTERS_AND_AFFECTED"{users[record.ReporterID]=true;users[record.AffectedUserID]=true;if record.Assignment.AssignedTo!=nil{users[*record.Assignment.AssignedTo]=true}}
		if policy=="ALL_LINKED"||policy=="WATCHERS"{rows,err:=e.QueryContext(ctx,`SELECT user_id FROM work_watchers WHERE tenant_id=? AND object_type='incident' AND object_id=? ORDER BY user_id LIMIT 100`,tenant,strconv.FormatUint(incidentID,10));if err!=nil{return nil,err};for rows.Next(){var user uint64;if err=rows.Scan(&user);err!=nil{rows.Close();return nil,err};users[user]=true};if err=rows.Err();err!=nil{rows.Close();return nil,err};rows.Close()}
		for user:=range users{if user>0{if _,exists:=out[user];!exists{out[user]=incidentID}}}
	}
	return out,nil
}

func(s *Service)deliverMajorReminder(ctx context.Context,e database.Executor,event events.Event,payload incidents.MajorDomainEvent,id uint64,commander,deputy,comms sql.Null[uint64])error{
	record,err:=incidents.NewRepository(s.db).Get(ctx,e,event.TenantID,id,false);if err!=nil{return err};users:=map[uint64]bool{};for _,candidate:=range []sql.Null[uint64]{commander,deputy,comms}{if candidate.Valid{users[candidate.V]=true}};if record.Assignment.AssignedTo!=nil{users[*record.Assignment.AssignedTo]=true}
	rows,err:=e.QueryContext(ctx,`SELECT gm.user_id FROM incident_major_teams mt JOIN group_memberships gm ON gm.tenant_id=mt.tenant_id AND gm.group_id=mt.group_id JOIN tenant_memberships tm ON tm.tenant_id=gm.tenant_id AND tm.user_id=gm.user_id AND tm.active=TRUE WHERE mt.tenant_id=? AND mt.incident_id=? ORDER BY gm.user_id LIMIT 500`,event.TenantID,id);if err!=nil{return err};for rows.Next(){var user uint64;if err=rows.Scan(&user);err!=nil{rows.Close();return err};users[user]=true};if err=rows.Err();err!=nil{rows.Close();return err};rows.Close()
	for user:=range users{if user==0{continue};actor,err:=s.currentActor(ctx,e,event.TenantID,user);if err!=nil{return err};if !incidents.CanRead(actor,record)||!actor.Has("incident.major.manage")&&!actor.Has("incident.major.read")&&commanderOrParticipant(user,commander,deputy,comms)==false{continue};pref,err:=s.preference(ctx,e,event.TenantID,user);if err!=nil{return err};if !pref.StateChanges||!pref.InApp&&!pref.SMTP{continue};_,err=e.ExecContext(ctx,`INSERT INTO notifications(tenant_id,user_id,source_event_id,incident_id,kind,visibility,summary,in_app_enabled,occurred_at) VALUES(?,?,?,?,?,'PUBLIC',?,?,?) ON DUPLICATE KEY UPDATE id=id`,event.TenantID,user,event.ID,id,"major_reminder","Zbliża się termin aktualizacji dużej awarii",pref.InApp,payload.OccurredAt.UTC());if err!=nil{return err};if pref.SMTP{var notificationID uint64;if err=e.QueryRowContext(ctx,`SELECT id FROM notifications WHERE tenant_id=? AND user_id=? AND source_event_id=?`,event.TenantID,user,event.ID).Scan(&notificationID);err!=nil{return err};if _,err=s.createMail(ctx,e,event.TenantID,user,notificationID);err!=nil{return err}}}
	return nil
}

func commanderOrParticipant(user uint64,values ...sql.Null[uint64])bool{for _,value:=range values{if value.Valid&&value.V==user{return true}};return false}

var _ = time.Second

