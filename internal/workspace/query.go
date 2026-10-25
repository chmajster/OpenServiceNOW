package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/metadata"
)

func (s *Service) Query(ctx context.Context, actor Actor, input Query) (QueryPage,error) {
	if !actor.CanUse() { return QueryPage{},ErrForbidden }
	custom,err:=s.validateActorQuery(ctx,actor,&input);if err!=nil{return QueryPage{},err}
	decoded,err:=decodeCursor(input.Cursor,input.Sort);if err!=nil{return QueryPage{},err}
	items:=make([]WorkItem,0,input.Limit+1)
	for _,kind:=range normalizedTypes(input) {
		if kind==IncidentType&&!actor.CanReadIncidents()||kind==TaskType&&!actor.CanReadTasks(){continue}
		rows,err:=s.listType(ctx,actor,input,kind,decoded,input.Limit+1,custom.fields);if err!=nil{return QueryPage{},err};items=append(items,rows...)
	}
	if err:=ctx.Err();err!=nil{return QueryPage{},err}
	sort.Slice(items,func(i,j int)bool{return less(items[i],items[j],input.Sort)})
	page:=QueryPage{Items:items,Limit:input.Limit}
	if len(page.Items)>input.Limit { page.Items=page.Items[:input.Limit];last:=page.Items[len(page.Items)-1];page.NextCursor=encodeCursor(cursorFor(last,input.Sort)) }
	if page.Items==nil { page.Items=[]WorkItem{} }
	if err:=s.attachCustomFields(ctx,actor,page.Items,selectedCustomColumns(input,custom.columns));err!=nil{return QueryPage{},err}
	return page,nil
}

func selectedCustomColumns(query Query, available map[string]metadata.Field) map[string]metadata.Field {
	selected := map[string]bool{}
	columns := query.Columns
	if columns == nil {
		columns = []string{"number", "object_type", "title", "state", "priority", "assigned_to", "assignment_group_id", "updated_at"}
	}
	for _, column := range columns {
		selected[column] = true
	}
	out := map[string]metadata.Field{}
	for key, field := range available {
		if selected[key] {
			out[key] = field
		}
	}
	return out
}

func (s *Service) listType(ctx context.Context, actor Actor, q Query, kind string, after cursor, limit int, customFields map[string]metadata.Field)([]WorkItem,error) {
	alias:="i";assignment:="w";id:="i.id";number:="i.number";title:="i.title";state:="i.state";priority:="i.priority";version:="i.version";created:="i.created_at";updated:="i.updated_at";category:="i.category";reporterACL:="(i.reporter_id=? OR i.affected_user_id=?)";watchedType:=IncidentType
	from:=` FROM incidents i JOIN work_assignments w ON w.tenant_id=i.tenant_id AND w.object_type='incident' AND w.object_id=CAST(i.id AS CHAR) LEFT JOIN users assigned ON assigned.id=w.assigned_to LEFT JOIN ` + "`groups`" + ` g ON g.tenant_id=w.tenant_id AND g.id=w.assignment_group_id `
	if kind==TaskType {
		alias="t";assignment="wa";id="t.id";number="t.number";title="t.title";state="t.state";priority="NULL";version="t.version";created="r.created_at";updated="t.updated_at";category="'FULFILLMENT'";reporterACL="1=0";watchedType=TaskType
		from=` FROM fulfillment_tasks t JOIN service_requests r ON r.tenant_id=t.tenant_id AND r.id=t.request_id JOIN work_assignments wa ON wa.tenant_id=t.tenant_id AND wa.object_type='fulfillment_task' AND wa.object_id=CAST(t.id AS CHAR) LEFT JOIN users assigned ON assigned.id=wa.assigned_to LEFT JOIN ` + "`groups`" + ` g ON g.tenant_id=wa.tenant_id AND g.id=wa.assignment_group_id `
	}
	selectRank:="0"
	args:=[]any{}
	pattern:=likePattern(q.Query)
	if q.Sort=="rank" {
		selectRank=rankExpression(number,title)
		args=append(args,q.Query,likePattern(q.Query),likePattern(q.Query))
	}
	query:=`SELECT `+id+`, `+number+`, `+title+`, `+state+`, `+priority+`, `+category+`, `+version+`, `+assignment+`.assigned_to, `+assignment+`.assignment_group_id, `+assignment+`.version, COALESCE(assigned.display_name,''), COALESCE(g.name,''), `+created+`, `+updated+`, EXISTS(SELECT 1 FROM work_watchers ww WHERE ww.tenant_id=`+alias+`.tenant_id AND ww.object_type=? AND ww.object_id=CAST(`+id+` AS CHAR) AND ww.user_id=?), `+selectRank+from+` WHERE `+alias+`.tenant_id=?`
	args=append(args,watchedType,actor.UserID,actor.TenantID)
	if kind==IncidentType {
		if !actor.Has("incident.read") { query+=` AND `+reporterACL;args=append(args,actor.UserID,actor.UserID) }
	} else if actor.Has("request.manage") {
		query+=` AND 1=1`
	} else {
		query+=` AND EXISTS(SELECT 1 FROM group_memberships gm JOIN `+"`groups`"+` ag ON ag.tenant_id=gm.tenant_id AND ag.id=gm.group_id AND ag.active=TRUE WHERE gm.tenant_id=t.tenant_id AND gm.group_id=wa.assignment_group_id AND gm.user_id=?)`
		args=append(args,actor.UserID)
	}
	switch q.View {
	case "mine": query+=` AND `+assignment+`.assigned_to=?`;args=append(args,actor.UserID)
	case "group":
		query+=` AND `+assignment+`.assignment_group_id IN (SELECT gm.group_id FROM group_memberships gm JOIN `+"`groups`"+` ag ON ag.tenant_id=gm.tenant_id AND ag.id=gm.group_id AND ag.active=TRUE WHERE gm.tenant_id=`+alias+`.tenant_id AND gm.user_id=?)`;args=append(args,actor.UserID)
	case "unassigned": query+=` AND `+assignment+`.assigned_to IS NULL`
	case "watched": query+=` AND EXISTS(SELECT 1 FROM work_watchers ww2 WHERE ww2.tenant_id=`+alias+`.tenant_id AND ww2.object_type=? AND ww2.object_id=CAST(`+id+` AS CHAR) AND ww2.user_id=?)`;args=append(args,watchedType,actor.UserID)
	}
	if q.Query!="" {
		query+=` AND (`+number+` LIKE ? ESCAPE '!' OR `+title+` LIKE ? ESCAPE '!'`
		args=append(args,pattern,pattern)
		if kind==TaskType { query+=` OR r.number LIKE ? ESCAPE '!'`;args=append(args,pattern) }
		query+=`)`
	}
	if q.Filters!=nil {
		compiler:=filterCompiler{objectType:kind,tenantID:actor.TenantID,userID:actor.UserID,now:time.Now().UTC(),customFields:customFields}
		filter,argsFilter,err:=compiler.compile(q.Filters,1);if err!=nil{return nil,err};query+=` AND (`+filter+`)`
		args=append(args,argsFilter...)
	}
	cursorSQL,cursorArgs,err:=cursorPredicate(q.Sort,after,kind,number,title,priority,created,updated,id,q.Query)
	if err!=nil{return nil,err};if cursorSQL!="" { query+=` AND (`+cursorSQL+`)`;args=append(args,cursorArgs...) }
	query+=` ORDER BY `+orderExpression(q.Sort,number,title,priority,created,updated,id)+` LIMIT ?`
	if q.Sort=="rank" { args=append(args,q.Query,likePattern(q.Query),likePattern(q.Query)) }
	args=append(args,limit)
	rows,err:=s.db.QueryContext(ctx,query,args...);if err!=nil{return nil,fmt.Errorf("query workspace %s: %w",kind,err)};defer rows.Close()
	items:=make([]WorkItem,0,limit)
	for rows.Next() {
		item,err:=scanWorkItem(rows,actor.TenantID,kind);if err!=nil{return nil,err};item.Capabilities=capabilities(actor,item);items=append(items,item)
	}
	if err:=rows.Err();err!=nil{return nil,err}
	return items,nil
}

type rowScanner interface { Scan(...any) error }

func scanWorkItem(row rowScanner,tenant uint64,kind string)(WorkItem,error) {
	var item WorkItem;var id string;var priority sql.NullInt64;var assigned,group sql.Null[uint64];var watched bool
	if err:=row.Scan(&id,&item.Number,&item.Title,&item.State,&priority,&item.Category,&item.Version,&assigned,&group,&item.AssignmentVersion,&item.AssignedName,&item.GroupName,&item.CreatedAt,&item.UpdatedAt,&watched,&item.SearchRank);err!=nil{return item,err}
	item.WorkReference=WorkReference{TenantID:tenant,ObjectType:kind,ObjectID:id};item.Watched=watched
	if priority.Valid { p:=int(priority.Int64);item.Priority=&p }
	if assigned.Valid { v:=assigned.V;item.AssignedTo=&v }
	if group.Valid { v:=group.V;item.AssignmentGroupID=&v }
	item.StateCategory=stateCategory(item.State,kind)
	if kind==IncidentType { item.RecordPath="/incidents/"+id } else { item.RecordPath="/fulfillment/"+id }
	return item,nil
}

func stateCategory(state,kind string)string {
	if kind==IncidentType {switch state{case "RESOLVED","CLOSED","CANCELLED":return "complete";case "ON_HOLD":return "waiting";default:return "active"}}
	switch state {case "COMPLETED","CANCELLED":return "complete";case "ON_HOLD":return "waiting";default:return "active"}
}

func capabilities(actor Actor,item WorkItem)[]string {
	out:=[]string{"open"}
	if item.ObjectType==IncidentType {
		editable:=item.State!="CLOSED"&&item.State!="CANCELLED"
		if editable&&actor.Has("incident.update"){out=append(out,"edit")}
		if editable&&actor.Has("incident.assign"){out=append(out,"assign")}
		if editable&&actor.Has("incident.priority.override"){out=append(out,"override_priority")}
		transitions:=map[string][]string{"NEW":{"IN_PROGRESS","ON_HOLD","CANCELLED"},"IN_PROGRESS":{"ON_HOLD","RESOLVED","CANCELLED"},"ON_HOLD":{"IN_PROGRESS","RESOLVED","CANCELLED"},"RESOLVED":{"CLOSED","IN_PROGRESS"},"CLOSED":{"IN_PROGRESS"}}
		for _,target:=range transitions[item.State] { perm:="incident.update";if target=="RESOLVED"||target=="CLOSED"{perm="incident.resolve"};if !actor.Has(perm){continue};if (target=="IN_PROGRESS"||target=="RESOLVED")&&item.AssignedTo==nil{continue};out=append(out,"transition:"+target) }
	} else {
		final:=item.State=="COMPLETED"||item.State=="CANCELLED"
		if !final&&(actor.Has("request.fulfill")||actor.Has("request.manage")){out=append(out,"assign")}
		canChange:=item.AssignedTo!=nil&&(*item.AssignedTo==actor.UserID||actor.Has("request.manage"))
		if !final&&canChange { switch item.State {case "OPEN":out=append(out,"transition:IN_PROGRESS");case "IN_PROGRESS":out=append(out,"transition:ON_HOLD","transition:COMPLETED");case "ON_HOLD":out=append(out,"transition:IN_PROGRESS")} }
	}
	return out
}

func less(a,b WorkItem,sortBy string)bool {
	switch sortBy {
	case "created_desc": if !a.CreatedAt.Equal(b.CreatedAt){return a.CreatedAt.After(b.CreatedAt)}
	case "priority_asc": ap,bp:=priorityKey(a),priorityKey(b);if ap!=bp{return ap<bp};if !a.UpdatedAt.Equal(b.UpdatedAt){return a.UpdatedAt.After(b.UpdatedAt)}
	case "number_asc": if a.Number!=b.Number{return a.Number<b.Number}
	case "rank": if a.SearchRank!=b.SearchRank{return a.SearchRank<b.SearchRank};if !a.UpdatedAt.Equal(b.UpdatedAt){return a.UpdatedAt.After(b.UpdatedAt)}
	default: if !a.UpdatedAt.Equal(b.UpdatedAt){return a.UpdatedAt.After(b.UpdatedAt)}
	}
	if a.ObjectType!=b.ObjectType{return a.ObjectType<b.ObjectType}
	ai,_:=strconv.ParseUint(a.ObjectID,10,64);bi,_:=strconv.ParseUint(b.ObjectID,10,64);return ai>bi
}

func priorityKey(item WorkItem)int { if item.Priority==nil{return 9999};return *item.Priority }

func cursorFor(item WorkItem,sortBy string)cursor {
	c:=cursor{Sort:sortBy,ObjectType:item.ObjectType,ObjectID:item.ObjectID}
	switch sortBy {case "created_desc":c.Value=item.CreatedAt.UTC().Format(time.RFC3339Nano);case "priority_asc":c.Number=priorityKey(item);c.UpdatedAt=item.UpdatedAt.UTC().Format(time.RFC3339Nano);case "rank":c.Number=item.SearchRank;c.UpdatedAt=item.UpdatedAt.UTC().Format(time.RFC3339Nano);case "number_asc":c.Value=item.Number;default:c.UpdatedAt=item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	return c
}

func cursorPredicate(sortBy string,c cursor,kind,number,title,priority,created,updated,id,query string)(string,[]any,error) {
	if c.ObjectID=="" { return "",nil,nil }
	valueID,err:=strconv.ParseUint(c.ObjectID,10,64);if err!=nil{return "",nil,ErrInvalid}
	tie,argsTie:=typeTie(kind,c.ObjectType,id,valueID)
	switch sortBy {
	case "created_desc":
		value,err:=time.Parse(time.RFC3339Nano,c.Value);if err!=nil{return "",nil,ErrInvalid}
		return `(`+created+` < ? OR (`+created+` = ? AND `+tie+`))`,append([]any{value,value},argsTie...),nil
	case "priority_asc":
		value,err:=time.Parse(time.RFC3339Nano,c.UpdatedAt);if err!=nil{return "",nil,ErrInvalid}
		return `(COALESCE(`+priority+`,9999) > ? OR (COALESCE(`+priority+`,9999) = ? AND (`+updated+` < ? OR (`+updated+` = ? AND `+tie+`))))`,append([]any{c.Number,c.Number,value,value},argsTie...),nil
	case "number_asc":
		return `(`+number+` > ? OR (`+number+` = ? AND `+tie+`))`,append([]any{c.Value,c.Value},argsTie...),nil
	case "rank":
		value,err:=time.Parse(time.RFC3339Nano,c.UpdatedAt);if err!=nil{return "",nil,ErrInvalid}
		rank:=rankExpression(number,title);rankArgs:=[]any{query,likePattern(query),likePattern(query)}
		args:=append([]any{},rankArgs...);args=append(args,c.Number);args=append(args,rankArgs...);args=append(args,value,value);args=append(args,argsTie...)
		return `(`+rank+` > ? OR (`+rank+` = ? AND (`+updated+` < ? OR (`+updated+` = ? AND `+tie+`))))`,args,nil
	default:
		value,err:=time.Parse(time.RFC3339Nano,c.UpdatedAt);if err!=nil{return "",nil,ErrInvalid}
		return `(`+updated+` < ? OR (`+updated+` = ? AND `+tie+`))`,append([]any{value,value},argsTie...),nil
	}
}

func typeTie(kind,cursorType,id string,cursorID uint64)(string,[]any) {
	if kind>cursorType{return "1=1",nil};if kind<cursorType{return "1=0",nil};return id+" < ?",[]any{cursorID}
}

func orderExpression(sortBy,number,title,priority,created,updated,id string)string {
	switch sortBy {case "created_desc":return created+` DESC, `+id+` DESC`;case "priority_asc":return `COALESCE(`+priority+`,9999) ASC, `+updated+` DESC, `+id+` DESC`;case "number_asc":return number+` ASC, `+id+` DESC`;case "rank":return rankExpression(number,title)+` ASC, `+updated+` DESC, `+id+` DESC`;default:return updated+` DESC, `+id+` DESC`}
}

func rankExpression(number,title string)string { return `CASE WHEN `+number+`=? THEN 0 WHEN `+number+` LIKE ? ESCAPE '!' THEN 1 WHEN `+title+` LIKE ? ESCAPE '!' THEN 2 ELSE 3 END` }
func likePattern(value string)string { escaped:=strings.NewReplacer("!","!!","%","!%","_","!_").Replace(value);return "%"+escaped+"%" }

