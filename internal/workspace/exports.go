package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const ExportJobType = "workspace.export.v1"
const maxExportItems = 10000

type exportPayload struct {
	Version int `json:"version"`
	RequesterID uint64 `json:"requester_id,string"`
	Query Query `json:"query"`
}

func (s *Service) CreateExport(ctx context.Context,actor Actor,q Query,key string)(ExportStatus,error) {
	if !actor.CanUse(){return ExportStatus{},ErrForbidden}
	if len(key)<8||len(key)>128{return ExportStatus{},ErrInvalid}
	q=cloneQuery(q);q.Cursor="";q.Limit=100
	if _,err:=s.validateActorQuery(ctx,actor,&q);err!=nil{return ExportStatus{},err}
	payload,err:=json.Marshal(exportPayload{Version:1,RequesterID:actor.UserID,Query:q});if err!=nil{return ExportStatus{},err}
	definition,err:=json.Marshal(q);if err!=nil{return ExportStatus{},err}
	createdBy:=actor.UserID
	var job jobs.Job
	err=s.tx.WithTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted},func(exec database.Executor)error {
		var created bool
		job,created,err=s.jobs.EnqueueInTx(ctx,exec,jobs.EnqueueInput{TenantID:actor.TenantID,Type:ExportJobType,Payload:payload,MaxAttempts:5,IdempotencyKey:key,CreatedBy:&createdBy})
		if err!=nil{return err}
		if created {
			_,err=exec.ExecContext(ctx,`INSERT INTO workspace_exports(tenant_id,job_id,requester_id,definition) VALUES(?,?,?,?)`,actor.TenantID,job.ID,actor.UserID,definition)
			return err
		}
		var requester uint64;var existing []byte
		err=exec.QueryRowContext(ctx,`SELECT requester_id,definition FROM workspace_exports WHERE tenant_id=? AND job_id=? FOR UPDATE`,actor.TenantID,job.ID).Scan(&requester,&existing)
		if errors.Is(err,sql.ErrNoRows){return ErrConflict}
		if err!=nil{return err}
		if requester!=actor.UserID||!bytes.Equal(existing,definition){return ErrConflict}
		return nil
	})
	if err!=nil{return ExportStatus{},err}
	return s.exportStatus(ctx,actor,job)
}

func (s *Service) ExportStatus(ctx context.Context,actor Actor,jobID uint64)(ExportStatus,error) {
	if !actor.CanUse()||jobID==0{return ExportStatus{},ErrNotFound}
	job,err:=s.jobs.Get(ctx,actor.TenantID,jobID);if err!=nil{return ExportStatus{},ErrNotFound}
	var requester uint64
	err=s.db.QueryRowContext(ctx,`SELECT requester_id FROM workspace_exports WHERE tenant_id=? AND job_id=?`,actor.TenantID,jobID).Scan(&requester)
	if errors.Is(err,sql.ErrNoRows){return ExportStatus{},ErrNotFound};if err!=nil{return ExportStatus{},err};if requester!=actor.UserID{return ExportStatus{},ErrNotFound}
	return s.exportStatus(ctx,actor,job)
}

func (s *Service) exportStatus(ctx context.Context,actor Actor,job jobs.Job)(ExportStatus,error) {
	status:=ExportStatus{JobID:job.ID,Status:string(job.Status),Progress:job.Progress,CreatedAt:job.CreatedAt,UpdatedAt:job.UpdatedAt}
	if job.Status==jobs.StatusFailed {status.Error="Eksport nie powiódł się. Spróbuj ponownie."}
	if job.Status==jobs.StatusCompleted {
		var attachmentID sql.Null[uint64]
		if err:=s.db.QueryRowContext(ctx,`SELECT attachment_id FROM workspace_exports WHERE tenant_id=? AND job_id=? AND requester_id=?`,actor.TenantID,job.ID,actor.UserID).Scan(&attachmentID);err!=nil{return ExportStatus{},err}
		if attachmentID.Valid {status.DownloadPath=fmt.Sprintf("/api/v1/workspace/exports/%d/file",job.ID)}
	}
	return status,nil
}

func (s *Service) ExportJobHandler(ctx context.Context,job jobs.Job)(json.RawMessage,error) {
	if job.Type!=ExportJobType||s.files==nil||s.permissionLoader==nil{return nil,ErrInvalid}
	var payload exportPayload
	if err:=json.Unmarshal(job.Payload,&payload);err!=nil||payload.Version!=1||payload.RequesterID==0{return nil,ErrInvalid}
	actor,err:=s.currentActor(ctx,job.TenantID,payload.RequesterID);if err!=nil{return nil,err}
	if _,err:=s.validateActorQuery(ctx,actor,&payload.Query);err!=nil{return nil,err}
	var existing sql.Null[uint64]
	if err:=s.db.QueryRowContext(ctx,`SELECT attachment_id FROM workspace_exports WHERE tenant_id=? AND job_id=? AND requester_id=?`,job.TenantID,job.ID,payload.RequesterID).Scan(&existing);err!=nil{return nil,err}
	if existing.Valid {
		if err:=s.ensureExportRowsReadable(ctx,actor,job.ID);err!=nil{return nil,err}
		return exportResult(existing.V,0)
	}
	payload.Query.Cursor="";payload.Query.Limit=100
	items:=make([]WorkItem,0)
	for {
		if len(items)>=maxExportItems{return nil,fmt.Errorf("workspace export exceeds %d rows",maxExportItems)}
		actor,err=s.currentActor(ctx,job.TenantID,payload.RequesterID);if err!=nil{return nil,err}
		page,err:=s.Query(ctx,actor,payload.Query);if err!=nil{return nil,err}
		items=append(items,page.Items...)
		if page.NextCursor==""{break};payload.Query.Cursor=page.NextCursor
	}
	payload.Query.Cursor=""
	var stale sql.Null[uint64]
	err=s.db.QueryRowContext(ctx,`SELECT id FROM attachments WHERE tenant_id=? AND object_type='workspace_export' AND object_id=? AND status='ACTIVE' ORDER BY id DESC LIMIT 1`,job.TenantID,strconv.FormatUint(job.ID,10)).Scan(&stale)
	if err==nil&&stale.Valid { if err=s.files.DeleteAs(ctx,job.TenantID,stale.V,payload.RequesterID);err!=nil{return nil,err} } else if err!=nil&&!errors.Is(err,sql.ErrNoRows){return nil,err}
	content,rowCount,err:=renderCSV(items,payload.Query);if err!=nil{return nil,err}
	if int64(content.Len())>attachments.MaxFileBytes{return nil,attachments.ErrTooLarge}
	createdBy:=payload.RequesterID
	file,err:=s.files.Upload(ctx,attachments.CreateInput{Visibility:"INTERNAL",TenantID:job.TenantID,ObjectType:"workspace_export",ObjectID:strconv.FormatUint(job.ID,10),FileName:"work-items.csv",UploadedBy:&createdBy},bytes.NewReader(content.Bytes()))
	if err!=nil{return nil,err}
	err=s.tx.WithTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted},func(exec database.Executor)error {
		for start:=0;start<len(items);start+=250 {
			end:=start+250;if end>len(items){end=len(items)}
			values:=make([]string,0,end-start);args:=make([]any,0,(end-start)*4)
			for _,item:=range items[start:end] {values=append(values,"(?,?,?,?)");args=append(args,job.TenantID,job.ID,item.ObjectType,item.ObjectID)}
			if len(values)>0 {if _,err:=exec.ExecContext(ctx,`INSERT IGNORE INTO workspace_export_items(tenant_id,job_id,object_type,object_id) VALUES `+strings.Join(values,","),args...);err!=nil{return err}}
		}
		result,err:=exec.ExecContext(ctx,`UPDATE workspace_exports SET attachment_id=? WHERE tenant_id=? AND job_id=? AND requester_id=? AND attachment_id IS NULL`,file.ID,job.TenantID,job.ID,payload.RequesterID)
		if err!=nil{return err};changed,err:=result.RowsAffected();if err!=nil{return err};if changed!=1 {
			var stored sql.Null[uint64];if err:=exec.QueryRowContext(ctx,`SELECT attachment_id FROM workspace_exports WHERE tenant_id=? AND job_id=? AND requester_id=? FOR UPDATE`,job.TenantID,job.ID,payload.RequesterID).Scan(&stored);err!=nil{return err};if !stored.Valid||stored.V!=file.ID{return ErrConflict}
		}
		return nil
	})
	if err!=nil { if !errors.Is(err,database.ErrCommitUncertain){_ = s.files.DeleteAs(context.WithoutCancel(ctx),job.TenantID,file.ID,payload.RequesterID)};return nil,err }
	return exportResult(file.ID,rowCount)
}

func (s *Service) currentActor(ctx context.Context,tenant,user uint64)(Actor,error) {
	if err:=workrecords.ValidateActiveUser(ctx,s.db,tenant,user);err!=nil{return Actor{},err}
	var status string;if err:=s.db.QueryRowContext(ctx,`SELECT status FROM tenants WHERE id=?`,tenant).Scan(&status);err!=nil||status!="ACTIVE"{return Actor{},ErrForbidden}
	permissions,err:=s.permissionLoader.ListPermissions(ctx,tenant,user);if err!=nil{return Actor{},err}
	actor:=Actor{TenantID:tenant,UserID:user,Permissions:map[string]bool{}}
	for _,permission:=range permissions{actor.Permissions[permission]=true}
	if !actor.CanUse(){return Actor{},ErrForbidden};return actor,nil
}

func renderCSV(items []WorkItem,query Query)(*bytes.Buffer,int,error) {
	buffer:=&bytes.Buffer{};writer:=csv.NewWriter(buffer)
	columns:=exportColumns(query)
	header:=[]string{"tenant_id","object_type","object_id"};for _,column:=range columns{if column!="object_type"{header=append(header,column)}}
	if err:=writer.Write(header);err!=nil{return nil,0,err}
	for _,item:=range items {
		row:=[]string{strconv.FormatUint(item.TenantID,10),item.ObjectType,item.ObjectID}
		for _,column:=range columns { if column=="object_type"{continue};value:=exportValue(item,column);if column=="number"||column=="title"||column=="category"||column=="assigned_to"||column=="assignment_group_id"||strings.HasPrefix(column,"cf."){value=safeCSVText(value)};row=append(row,value) }
		if err:=writer.Write(row);err!=nil{return nil,0,err};if buffer.Len()>int(attachments.MaxFileBytes){return nil,0,attachments.ErrTooLarge}
	}
	writer.Flush();if err:=writer.Error();err!=nil{return nil,0,err};return buffer,len(items),nil
}

func exportColumns(query Query)[]string {
	selected:=query.Columns
	if selected==nil { selected=[]string{"number","object_type","title","state","priority","assigned_to","assignment_group_id","updated_at"} }
	order:=query.ColumnOrder
	if len(order)==0 { return append([]string(nil),selected...) }
	result:=make([]string,0,len(selected));seen:=map[string]bool{}
	for _,column:=range order { for _,allowed:=range selected {if column==allowed&&!seen[column]{result=append(result,column);seen[column]=true;break}} }
	for _,column:=range selected {if !seen[column]{result=append(result,column)}}
	return result
}

func exportValue(item WorkItem,column string)string {
	if strings.HasPrefix(column,"cf.") {
		raw:=item.CustomFields[strings.TrimPrefix(column,"cf.")]
		if len(raw)==0{return ""}
		var value any;if json.Unmarshal(raw,&value)!=nil{return string(raw)}
		if list,ok:=value.([]any);ok {items:=make([]string,0,len(list));for _,entry:=range list{items=append(items,fmt.Sprint(entry))};return strings.Join(items,", ")}
		return fmt.Sprint(value)
	}
	switch column {
	case "number":return item.Number
	case "object_type":return item.ObjectType
	case "title":return item.Title
	case "state":return item.State
	case "state_category":return item.StateCategory
	case "category":return item.Category
	case "priority":if item.Priority!=nil{return strconv.Itoa(*item.Priority)}
	case "assigned_to":return item.AssignedName
	case "assignment_group_id":return item.GroupName
	case "created_at":return item.CreatedAt.UTC().Format(time.RFC3339)
	case "updated_at":return item.UpdatedAt.UTC().Format(time.RFC3339)
	case "watched":return strconv.FormatBool(item.Watched)
	}
	return ""
}

func safeCSVText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}

func exportResult(attachmentID uint64,rows int)(json.RawMessage,error) {
	result:=struct{Version int `json:"version"`;AttachmentID uint64 `json:"attachment_id,string"`;Rows int `json:"rows"`}{Version:1,AttachmentID:attachmentID,Rows:rows}
	data,err:=json.Marshal(result);return data,err
}
