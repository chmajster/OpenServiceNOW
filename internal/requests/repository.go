package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func taskPredicate(a Actor, alias string) (string, []any) {
	if a.Has("request.manage") {
		return "1=1", nil
	}
	if !a.Has("request.fulfill") {
		return "1=0", nil
	}
	return "EXISTS(SELECT 1 FROM work_assignments wa JOIN group_memberships gm ON gm.tenant_id=wa.tenant_id AND gm.group_id=wa.assignment_group_id JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE wa.tenant_id=" + alias + ".tenant_id AND wa.object_type='fulfillment_task' AND wa.object_id=CAST(" + alias + ".id AS CHAR) AND gm.user_id=?)", []any{a.UserID}
}
func requestPredicate(a Actor, alias string) (string, []any) {
	if a.Has("request.manage") {
		return "1=1", nil
	}
	parts := "1=0"
	args := []any{}
	if a.Has("request.read_own") {
		parts += " OR " + alias + ".requester_id=?"
		args = append(args, a.UserID)
	}
	if a.Has("request.fulfill") {
		p, pa := taskPredicate(a, "t")
		parts += " OR EXISTS(SELECT 1 FROM fulfillment_tasks t WHERE t.tenant_id=" + alias + ".tenant_id AND t.request_id=" + alias + ".id AND " + p + ")"
		args = append(args, pa...)
	}
	return "(" + parts + ")", args
}
func (s *Service) canOperate(ctx context.Context, e database.Executor, a Actor, id uint64) (bool, error) {
	if a.Has("request.manage") {
		return true, nil
	}
	p, args := taskPredicate(a, "t")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	var ok bool
	err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM fulfillment_tasks t WHERE t.tenant_id=? AND t.request_id=? AND "+p+")", params...).Scan(&ok)
	return ok, err
}
func (s *Service) get(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (Request, error) {
	var r Request
	if lock {
		var v uint64
		if err := e.QueryRowContext(ctx, "SELECT id FROM service_requests WHERE tenant_id=? AND id=? FOR UPDATE", a.TenantID, id).Scan(&v); err != nil {
			return r, catalog.Missing(err)
		}
	}
	p, acl := requestPredicate(a, "r")
	args := []any{a.TenantID, id}
	args = append(args, acl...)
	err := e.QueryRowContext(ctx, "SELECT r.id,r.number,r.requester_id,u.display_name,r.state,r.reason,r.version,r.created_at,r.updated_at FROM service_requests r JOIN users u ON u.id=r.requester_id WHERE r.tenant_id=? AND r.id=? AND "+p, args...).Scan(&r.ID, &r.Number, &r.RequesterID, &r.RequesterName, &r.State, &r.Reason, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, catalog.Missing(err)
	}
	rows, err := e.QueryContext(ctx, `SELECT i.id,i.number,i.snapshot,i.form_values,COUNT(t.id),COALESCE(SUM(t.state='COMPLETED'),0),COALESCE(SUM(t.required),0),COALESCE(SUM(t.required AND t.state='COMPLETED'),0) FROM requested_items i LEFT JOIN fulfillment_tasks t ON t.tenant_id=i.tenant_id AND t.requested_item_id=i.id WHERE i.tenant_id=? AND i.request_id=? GROUP BY i.id ORDER BY i.id`, a.TenantID, id)
	if err != nil {
		return r, err
	}
	r.Items = []RequestedItem{}
	for rows.Next() {
		var i RequestedItem
		var snap, values []byte
		if err := rows.Scan(&i.ID, &i.Number, &snap, &values, &i.Total, &i.Completed, &i.Required, &i.RequiredCompleted); err != nil {
			rows.Close()
			return r, err
		}
		if err := json.Unmarshal(snap, &i.Snapshot); err != nil {
			rows.Close()
			return r, err
		}
		if err := json.Unmarshal(values, &i.Values); err != nil {
			rows.Close()
			return r, err
		}
		r.Items = append(r.Items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	r.Actions = []string{}
	var started bool
	if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM fulfillment_tasks WHERE tenant_id=? AND request_id=? AND state<>'OPEN')", a.TenantID, id).Scan(&started); err != nil {
		return r, err
	}
	if r.State == "SUBMITTED" && !started && a.UserID == r.RequesterID && a.Has("request.cancel_own") {
		r.Actions = append(r.Actions, "CANCELLED")
	}
	operator, err := s.canOperate(ctx, e, a, id)
	if err != nil {
		return r, err
	}
	if operator {
		if r.State == "SUBMITTED" || r.State == "IN_PROGRESS" {
			if a.Has("request.manage") {
				if !contains(r.Actions, "CANCELLED") {
					r.Actions = append(r.Actions, "CANCELLED")
				}
			}
			complete := true
			for _, i := range r.Items {
				complete = complete && i.Required == i.RequiredCompleted
			}
			if complete {
				r.Actions = append(r.Actions, "FULFILLED")
			}
		}
		if r.State == "FULFILLED" {
			r.Actions = append(r.Actions, "CLOSED")
		}
	}
	return r, nil
}

const taskColumns = `t.id,t.request_id,t.requested_item_id,t.number,r.number,t.title,t.instructions,t.required,t.state,t.result,t.version,t.updated_at,wa.assigned_to,wa.assignment_group_id,wa.version,wa.updated_at,COALESCE(u.display_name,''),g.name,r.state`
const taskJoins = " FROM fulfillment_tasks t JOIN service_requests r ON r.tenant_id=t.tenant_id AND r.id=t.request_id JOIN work_assignments wa ON wa.tenant_id=t.tenant_id AND wa.object_type='fulfillment_task' AND wa.object_id=CAST(t.id AS CHAR) LEFT JOIN users u ON u.id=wa.assigned_to JOIN `groups` g ON g.tenant_id=wa.tenant_id AND g.id=wa.assignment_group_id "

func scanTask(row interface{ Scan(...any) error }, tenant uint64) (Task, error) {
	var t Task
	var assigned, group sql.Null[uint64]
	var parentState string
	err := row.Scan(&t.ID, &t.RequestID, &t.RequestedItemID, &t.Number, &t.RequestNumber, &t.Title, &t.Instructions, &t.Required, &t.State, &t.Result, &t.Version, &t.UpdatedAt, &assigned, &group, &t.Assignment.Version, &t.Assignment.UpdatedAt, &t.AssignedName, &t.GroupName, &parentState)
	if assigned.Valid {
		t.Assignment.AssignedTo = &assigned.V
	}
	if group.Valid {
		t.Assignment.AssignmentGroupID = &group.V
	}
	t.Assignment.TenantID = tenant
	t.Assignment.ObjectType = "fulfillment_task"
	t.Assignment.ObjectID = fmt.Sprint(t.ID)
	t.Actions = []string{}
	if t.State == "OPEN" {
		t.Actions = []string{"IN_PROGRESS"}
	}
	if t.State == "IN_PROGRESS" {
		t.Actions = []string{"ON_HOLD", "COMPLETED"}
	}
	if t.State == "ON_HOLD" {
		t.Actions = []string{"IN_PROGRESS"}
	}
	if !assigned.Valid || parentState == "CLOSED" || parentState == "CANCELLED" || parentState == "FULFILLED" {
		t.Actions = []string{}
	}
	return t, catalog.Missing(err)
}
func (s *Service) getTask(ctx context.Context, e database.Executor, a Actor, id uint64) (Task, error) {
	p, args := taskPredicate(a, "t")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	t, err := scanTask(e.QueryRowContext(ctx, "SELECT "+taskColumns+taskJoins+" WHERE t.tenant_id=? AND t.id=? AND "+p, params...), a.TenantID)
	if !a.Has("request.manage") && t.Assignment.AssignedTo != nil && *t.Assignment.AssignedTo != a.UserID {
		t.Actions = []string{}
	}
	return t, err
}
func reserve(ctx context.Context, e database.Executor, a Actor, key, kind string, hash [32]byte) (uint64, error) {
	id, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, kind, hash)
	if errors.Is(err, workrecords.ErrOperationConflict) {
		return 0, ErrIdempotency
	}
	return id, err
}
func complete(ctx context.Context, e database.Executor, a Actor, key string, id uint64) error {
	return workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
}
