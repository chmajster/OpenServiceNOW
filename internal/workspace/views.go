package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (s *Service) ListViews(ctx context.Context,actor Actor)([]View,error) {
	if !actor.CanUse(){return nil,ErrForbidden}
	custom,err:=s.customFilterFields(ctx,actor);if err!=nil{return nil,err}
	rows,err:=s.db.QueryContext(ctx,`SELECT id,tenant_id,owner_user_id,name,shared,version,definition,created_at,updated_at FROM workspace_views WHERE tenant_id=? AND (owner_user_id=? OR shared=TRUE) ORDER BY shared DESC,updated_at DESC,id DESC LIMIT 100`,actor.TenantID,actor.UserID)
	if err!=nil{return nil,fmt.Errorf("list workspace views: %w",err)};defer rows.Close()
	items:=make([]View,0)
	for rows.Next(){item,err:=scanView(rows);if err!=nil{return nil,err};if err=validateQuery(&item.Definition,true,custom.definitions);err!=nil{continue};items=append(items,item)}
	return items,rows.Err()
}

func (s *Service) SaveView(ctx context.Context,actor Actor,id uint64,input SaveViewInput)(View,error) {
	if !actor.CanUse(){return View{},ErrForbidden}
	input.Name=strings.TrimSpace(input.Name)
	if !workspaceViewName.MatchString(input.Name)||strings.TrimSpace(input.Name)=="" {return View{},ErrInvalid}
	definition:=cloneQuery(input.Definition);if _,err:=s.validateActorQuery(ctx,actor,&definition);err!=nil{return View{},err}
	definition.Cursor=""
	raw,err:=json.Marshal(definition);if err!=nil{return View{},err}
	if input.Shared&&!actor.Has("admin.rbac.manage"){return View{},ErrForbidden}
	if id==0 {
		result,err:=s.db.ExecContext(ctx,`INSERT INTO workspace_views(tenant_id,owner_user_id,name,shared,definition,version) VALUES(?,?,?,?,?,1)`,actor.TenantID,actor.UserID,input.Name,input.Shared,raw)
		if err!=nil {if isDuplicate(err){return View{},ErrConflict};return View{},fmt.Errorf("save workspace view: %w",err)}
		created,err:=result.LastInsertId();if err!=nil{return View{},err}
		return s.getView(ctx,actor,uint64(created))
	}
	if input.Version==0{return View{},ErrInvalid}
	var owner uint64;var shared bool
	if err:=s.db.QueryRowContext(ctx,`SELECT owner_user_id,shared FROM workspace_views WHERE tenant_id=? AND id=?`,actor.TenantID,id).Scan(&owner,&shared);err!=nil{if errors.Is(err,sql.ErrNoRows){return View{},ErrNotFound};return View{},err}
	if owner!=actor.UserID&&!actor.Has("admin.rbac.manage"){return View{},ErrNotFound}
	if (shared||input.Shared)&&!actor.Has("admin.rbac.manage"){return View{},ErrForbidden}
	result,err:=s.db.ExecContext(ctx,`UPDATE workspace_views SET name=?,shared=?,definition=?,version=version+1,updated_at=? WHERE tenant_id=? AND id=? AND owner_user_id=? AND version=?`,input.Name,input.Shared,raw,time.Now().UTC(),actor.TenantID,id,owner,input.Version)
	if err!=nil {if isDuplicate(err){return View{},ErrConflict};return View{},fmt.Errorf("update workspace view: %w",err)}
	updated,err:=result.RowsAffected();if err!=nil{return View{},err};if updated!=1{return View{},ErrConflict}
	return s.getView(ctx,actor,id)
}

func (s *Service) DeleteView(ctx context.Context,actor Actor,id,version uint64)error {
	if !actor.CanUse(){return ErrForbidden};if id==0||version==0{return ErrInvalid}
	return s.tx.WithTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted},func(exec database.Executor)error {
		var owner,currentVersion uint64
		err:=exec.QueryRowContext(ctx,`SELECT owner_user_id,version FROM workspace_views WHERE tenant_id=? AND id=? FOR UPDATE`,actor.TenantID,id).Scan(&owner,&currentVersion)
		if errors.Is(err,sql.ErrNoRows){return ErrNotFound};if err!=nil{return err}
		if owner!=actor.UserID&&!actor.Has("admin.rbac.manage"){return ErrNotFound}
		if currentVersion!=version{return ErrConflict}
		result,err:=exec.ExecContext(ctx,`DELETE FROM workspace_views WHERE tenant_id=? AND id=? AND owner_user_id=? AND version=?`,actor.TenantID,id,owner,version)
		if err!=nil{return fmt.Errorf("delete workspace view: %w",err)}
		count,err:=result.RowsAffected();if err!=nil{return err};if count!=1{return ErrConflict};return nil
	})
}

func (s *Service) getView(ctx context.Context,actor Actor,id uint64)(View,error) {
	item,err:=scanView(s.db.QueryRowContext(ctx,`SELECT id,tenant_id,owner_user_id,name,shared,version,definition,created_at,updated_at FROM workspace_views WHERE tenant_id=? AND id=? AND (owner_user_id=? OR shared=TRUE OR ?)`,actor.TenantID,id,actor.UserID,actor.Has("admin.rbac.manage")))
	if errors.Is(err,sql.ErrNoRows){return View{},ErrNotFound};return item,err
}

type viewScanner interface{Scan(...any)error}
func scanView(row viewScanner)(View,error) {
	var item View;var raw []byte
	if err:=row.Scan(&item.ID,&item.TenantID,&item.OwnerUserID,&item.Name,&item.Shared,&item.Version,&raw,&item.CreatedAt,&item.UpdatedAt);err!=nil{return item,err}
	if err:=json.Unmarshal(raw,&item.Definition);err!=nil{return item,fmt.Errorf("decode workspace view: %w",err)}
	item.Definition.Cursor=""
	return item,nil
}

func isDuplicate(err error)bool {
	return err!=nil&&strings.Contains(strings.ToLower(err.Error()),"duplicate entry")
}
