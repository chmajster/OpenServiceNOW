package metadata

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type ReferenceItem struct {
	ID    uint64 `json:"id,string"`
	Label string `json:"label"`
}

type ReferencePage struct {
	Items []ReferenceItem `json:"items"`
	Next  uint64          `json:"next_before_id,string,omitempty"`
}

func (s *Service) References(ctx context.Context, actor Actor, target, query string, before uint64, limit int) (ReferencePage,error) {
	out:=ReferencePage{Items:[]ReferenceItem{}}
	if !referenceTypes[target]||limit<1||limit>100||!text(query,0,160){return out,ErrInvalid}
	access:="";selectExpr:="";from:="";args:=[]any{actor.TenantID}
	switch target {
	case "user":
		if !canReferenceUsers(actor) { return out, workrecords.ErrForbidden }
		selectExpr="u.id,u.display_name";from=" FROM users u JOIN tenant_memberships m ON m.user_id=u.id WHERE m.tenant_id=? AND m.active=TRUE AND u.active=TRUE"
		if !canListTenantUsers(actor) { from += " AND u.id=?"; args = append(args, actor.UserID) }
	case "group":
		if !canReferenceGroups(actor){return out,workrecords.ErrForbidden}
		selectExpr="g.id,g.name";from=" FROM `groups` g WHERE g.tenant_id=? AND g.active=TRUE"
		if !canListAllGroups(actor) { from += " AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=g.tenant_id AND gm.group_id=g.id AND gm.user_id=?)"; args = append(args, actor.UserID) }
	case "incident":
		if !actor.Has("incident.read")&&!actor.Has("incident.read_own"){return out,workrecords.ErrForbidden}
		selectExpr="i.id,CONCAT(i.number,' · ',i.title)";from=" FROM incidents i WHERE i.tenant_id=?";if !actor.Has("incident.read"){access=" AND (i.reporter_id=? OR i.affected_user_id=?)";args=append(args,actor.UserID,actor.UserID)}
	case "request":
		if !actor.Has("request.read_own") && !actor.Has("request.fulfill") && !actor.Has("request.manage") {
			return out, workrecords.ErrForbidden
		}
		selectExpr = "r.id,CONCAT(r.number,' / ',r.state)"
		from = " FROM service_requests r WHERE r.tenant_id=?"
		if !actor.Has("request.manage") {
			predicate, predicateArgs, ok := requestReferencePredicate(actor)
			if !ok {
				return out, workrecords.ErrForbidden
			}
			access = " AND " + predicate
			args = append(args, predicateArgs...)
		}
	case "ci":
		if !actor.Has("cmdb.read")&&!actor.Has("cmdb.read_own"){return out,workrecords.ErrForbidden}
		selectExpr="c.id,CONCAT(c.identifier,' · ',c.name)";from=" FROM cmdb_cis c WHERE c.tenant_id=?";if !actor.Has("cmdb.read"){access=" AND c.owner_id=?";args=append(args,actor.UserID)}
	case "asset":
		if !actor.Has("assets.read")&&!actor.Has("assets.read_own"){return out,workrecords.ErrForbidden}
		selectExpr="a.id,CONCAT(a.asset_tag,' · ',a.name)";from=" FROM assets a WHERE a.tenant_id=?";if !actor.Has("assets.read"){access=" AND a.owner_id=?";args=append(args,actor.UserID)}
	}
	querySQL:="SELECT "+selectExpr+from+access
	if query!="" {querySQL+=" AND LOCATE(?, "+referenceSearchExpression(target)+")>0";args=append(args,query)}
	if before>0 {querySQL+=" AND "+referenceIDExpression(target)+"<?";args=append(args,before)}
	querySQL+=" ORDER BY "+referenceIDExpression(target)+" DESC LIMIT ?";args=append(args,limit+1)
	rows,err:=s.DB.QueryContext(ctx,querySQL,args...);if err!=nil{return out,err};defer rows.Close()
	for rows.Next(){var item ReferenceItem;if err=rows.Scan(&item.ID,&item.Label);err!=nil{return out,err};out.Items=append(out.Items,item)}
	if err=rows.Err();err!=nil{return out,err}
	if len(out.Items)>limit {out.Items=out.Items[:limit];out.Next=out.Items[limit-1].ID}
	return out,nil
}

func canReferenceUsers(actor Actor) bool {
	return canListTenantUsers(actor) || actor.Has("incident.read_own") || actor.Has("incident.create") || actor.Has("incident.update") || actor.Has("cmdb.read_own") || actor.Has("cmdb.manage") || actor.Has("assets.read_own") || actor.Has("assets.manage") || actor.Has("request.read_own") || actor.Has("request.manage") || actor.Has("request.fulfill")
}

func canListTenantUsers(actor Actor) bool {
	return actor.Has("admin.users.manage") || actor.Has("incident.read") || actor.Has("cmdb.read") || actor.Has("assets.read") || actor.Has("request.manage")
}

func canReferenceGroups(actor Actor) bool {
	return canListAllGroups(actor) || actor.Has("incident.read_own") || actor.Has("request.read_own") || actor.Has("request.fulfill")
}

func canListAllGroups(actor Actor) bool {
	return actor.Has("admin.users.manage") || actor.Has("incident.assign") || actor.Has("incident.read") || actor.Has("request.manage")
}

func requestReferencePredicate(actor Actor) (string, []any, bool) {
	parts := []string{}
	args := []any{}
	if actor.Has("request.read_own") {
		parts = append(parts, "r.requester_id=?")
		args = append(args, actor.UserID)
	}
	if actor.Has("request.fulfill") {
		parts = append(parts, "EXISTS(SELECT 1 FROM fulfillment_tasks t WHERE t.tenant_id=r.tenant_id AND t.request_id=r.id AND EXISTS(SELECT 1 FROM work_assignments wa JOIN group_memberships gm ON gm.tenant_id=wa.tenant_id AND gm.group_id=wa.assignment_group_id JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE wa.tenant_id=t.tenant_id AND wa.object_type='fulfillment_task' AND wa.object_id=CAST(t.id AS CHAR) AND gm.user_id=?))")
		args = append(args, actor.UserID)
	}
	if len(parts) == 0 {
		return "", nil, false
	}
	return "(" + strings.Join(parts, " OR ") + ")", args, true
}

func referenceSearchExpression(target string)string{switch target{case"user":return"CONCAT(u.display_name,' ',u.email)";case"group":return"g.name";case"incident":return"CONCAT(i.number,' ',i.title)";case"request":return"CONCAT(r.number,' ',r.state)";case"ci":return"CONCAT(c.identifier,' ',c.name)";case"asset":return"CONCAT(a.asset_tag,' ',a.name)"};return"''"}
func referenceIDExpression(target string)string{switch target{case"user":return"u.id";case"group":return"g.id";case"incident":return"i.id";case"request":return"r.id";case"ci":return"c.id";case"asset":return"a.id"};return"0"}

type Preferences struct {
	Locale  string `json:"locale,omitempty"`
	TimeZone string `json:"time_zone,omitempty"`
	Version uint64 `json:"version,string"`
	OrganizationName string `json:"organization_name"`
	BrandColor string `json:"brand_color"`
	LogoURL string `json:"logo_url,omitempty"`
	DefaultLocale string `json:"default_locale"`
	DefaultTimeZone string `json:"default_time_zone"`
	DefaultDateFormat string `json:"default_date_format"`
	DefaultNumberFormat string `json:"default_number_format"`
	DefaultCurrency string `json:"default_currency"`
}

func (s *Service) GetPreferences(ctx context.Context,actor Actor)(Preferences,error){
	_,config,err:=s.Published(ctx,s.DB,actor.TenantID);if err!=nil{return Preferences{},err};out:=Preferences{OrganizationName:config.Organization.Name,BrandColor:config.Organization.BrandColor,LogoURL:config.Organization.LogoURL,DefaultLocale:config.Organization.DefaultLocale,DefaultTimeZone:config.Organization.TimeZone,DefaultDateFormat:config.Organization.DateFormat,DefaultNumberFormat:config.Organization.NumberFormat,DefaultCurrency:config.Organization.Currency}
	err=s.DB.QueryRowContext(ctx,"SELECT COALESCE(locale,''),COALESCE(time_zone,''),version FROM metadata_user_preferences WHERE tenant_id=? AND user_id=?",actor.TenantID,actor.UserID).Scan(&out.Locale,&out.TimeZone,&out.Version)
	if errors.Is(err,sql.ErrNoRows){return out,nil};return out,err
}

func (s *Service) SetPreferences(ctx context.Context,actor Actor,expected uint64,locale,timeZone string)(Preferences,error){
	if locale!=""&&!emailLocale[locale]||timeZone!=""&&!validTimeZone(timeZone){return Preferences{},ErrInvalid}
	err:=s.tx.WithTx(ctx,nil,func(exec database.Executor)error{
		principal:=workrecords.Principal{Actor:workrecords.Actor{TenantID:actor.TenantID,UserID:actor.UserID,RequestID:actor.RequestID},IPAddress:actor.IPAddress,Permissions:actor.Permissions}
		current,err:=workrecords.LockPrincipal(ctx,exec,principal);if err!=nil{return err}
		var version uint64
		err=exec.QueryRowContext(ctx,"SELECT version FROM metadata_user_preferences WHERE tenant_id=? AND user_id=? FOR UPDATE",actor.TenantID,actor.UserID).Scan(&version)
		if err!=nil&&!errors.Is(err,sql.ErrNoRows){return err}
		if version!=expected{return ErrConflict}
		if version==0 {_,err=exec.ExecContext(ctx,"INSERT INTO metadata_user_preferences(tenant_id,user_id,locale,time_zone,version) VALUES(?,?,?,?,1)",actor.TenantID,actor.UserID,nullString(locale),nullString(timeZone))}else{_,err=exec.ExecContext(ctx,"UPDATE metadata_user_preferences SET locale=?,time_zone=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND user_id=? AND version=?",nullString(locale),nullString(timeZone),actor.TenantID,actor.UserID,version)}
		_ = current
		return err
	})
	if err!=nil{return Preferences{},err};return s.GetPreferences(ctx,actor)
}

func nullString(value string)any{if strings.TrimSpace(value)==""{return nil};return value}

