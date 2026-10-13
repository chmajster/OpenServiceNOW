package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"time"
)

func (s *Service) createTask(ctx context.Context, e database.Executor, a Actor, request, item, group uint64, step catalog.Step, at time.Time) (uint64, error) {
	if err := catalog.ActiveGroup(ctx, e, a.TenantID, group); err != nil {
		return 0, err
	}
	num, err := s.allocator.NextInTx(ctx, e, a.TenantID, "fulfillment_task", "SCTASK", 7)
	if err != nil {
		return 0, err
	}
	result, err := e.ExecContext(ctx, "INSERT INTO fulfillment_tasks(tenant_id,request_id,requested_item_id,number,title,instructions,required,result,updated_at) VALUES(?,?,?,?,?,?,?,'',?)", a.TenantID, request, item, num, step.Title, step.Instructions, step.Required, at)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = s.work.SetAssignmentInTx(ctx, e, a.Actor, "fulfillment_task", fmt.Sprint(id), 0, nil, &group)
	return uint64(id), err
}
func workflowGate(ctx context.Context, e database.Executor, tenant, request, skip uint64) error {
	var blocked bool
	err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workflow_executions WHERE tenant_id=? AND request_id=? AND id<>? AND (state<>'COMPLETED' OR approved=FALSE))", tenant, request, skip).Scan(&blocked)
	if err != nil {
		return err
	}
	if blocked {
		return ErrTransition
	}
	return nil
}
func cancelWorkflows(ctx context.Context, e database.Executor, tenant, request, skip uint64) error {
	if _, err := e.ExecContext(ctx, "UPDATE workflow_steps ws JOIN workflow_executions x ON x.tenant_id=ws.tenant_id AND x.id=ws.execution_id SET ws.state='CANCELLED',ws.version=ws.version+1,ws.finished_at=UTC_TIMESTAMP(6) WHERE x.tenant_id=? AND x.request_id=? AND x.id<>? AND ws.state='WAITING'", tenant, request, skip); err != nil {
		return err
	}
	_, err := e.ExecContext(ctx, "UPDATE workflow_executions SET state='CANCELLED',version=version+1,next_check_at=NULL,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND request_id=? AND id<>? AND state IN('READY','WAITING','ERROR')", tenant, request, skip)
	return err
}
func (s *Service) ContextInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution) (string, map[string]json.RawMessage, error) {
	// Verify the durable execution's complete scope, not arbitrary client object IDs.
	if !a.Has("workflow.execute") || !a.Has("request.fulfill") || a.UserID != x.ExecutorID {
		return "", nil, ErrForbidden
	}
	var state string
	var raw []byte
	var requester uint64
	err := e.QueryRowContext(ctx, "SELECT r.state,r.requester_id,i.form_values FROM service_requests r JOIN requested_items i ON i.tenant_id=r.tenant_id AND i.request_id=r.id JOIN workflow_executions x ON x.tenant_id=i.tenant_id AND x.requested_item_id=i.id WHERE r.tenant_id=? AND r.id=? AND i.id=? AND x.id=? AND x.group_id=? AND x.executor_id=?", a.TenantID, x.RequestID, x.RequestedItemID, x.ID, x.GroupID, a.UserID).Scan(&state, &requester, &raw)
	if err != nil {
		return "", nil, catalog.Missing(err)
	}
	if requester != x.RequesterID {
		return "", nil, ErrForbidden
	}
	var values map[string]json.RawMessage
	err = json.Unmarshal(raw, &values)
	return state, values, err
}
func (s *Service) CreateTaskInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution, n workflow.Node) (uint64, error) {
	state, _, err := s.ContextInTx(ctx, e, a, x)
	if err != nil {
		return 0, err
	}
	if !x.Approved || state != "SUBMITTED" && state != "IN_PROGRESS" {
		return 0, ErrTransition
	}
	at := time.Now().UTC()
	id, err := s.createTask(ctx, e, a, x.RequestID, x.RequestedItemID, x.GroupID, catalog.Step{Title: n.Title, Instructions: n.Instructions, Required: true}, at)
	if err != nil {
		return 0, err
	}
	if _, err = e.ExecContext(ctx, "UPDATE service_requests SET version=version+1,updated_at=? WHERE tenant_id=? AND id=?", at, a.TenantID, x.RequestID); err != nil {
		return 0, err
	}
	var version uint64
	if err = e.QueryRowContext(ctx, "SELECT version FROM service_requests WHERE tenant_id=? AND id=?", a.TenantID, x.RequestID).Scan(&version); err != nil {
		return 0, err
	}
	err = s.event(ctx, e, a, Request{ID: x.RequestID, Version: version, State: state}, "request.task.changed", "PUBLIC", fmt.Sprintf("request:%d:%d", x.RequestID, version), map[string]any{"resource_id": fmt.Sprint(id), "task_state_after": "OPEN"})
	return id, err
}
func (s *Service) TaskStateInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution, id uint64) (workflow.TaskStatus, error) {
	if _, _, err := s.ContextInTx(ctx, e, a, x); err != nil {
		return workflow.TaskStatus{}, err
	}
	var state workflow.TaskStatus
	err := e.QueryRowContext(ctx, "SELECT state,updated_at FROM fulfillment_tasks WHERE tenant_id=? AND request_id=? AND requested_item_id=? AND id=?", a.TenantID, x.RequestID, x.RequestedItemID, id).Scan(&state.State, &state.UpdatedAt)
	return state, catalog.Missing(err)
}
func (s *Service) UpdateInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution, n workflow.Node) error {
	state, _, err := s.ContextInTx(ctx, e, a, x)
	if err != nil {
		return err
	}
	if !catalog.Text(n.Reason, 1, 10000) {
		return ErrInvalid
	}
	if n.State != "CANCELLED" && !x.Approved {
		return ErrForbidden
	}
	var version uint64
	if err = e.QueryRowContext(ctx, "SELECT version FROM service_requests WHERE tenant_id=? AND id=?", a.TenantID, x.RequestID).Scan(&version); err != nil {
		return err
	}
	r := Request{ID: x.RequestID, Version: version, State: state}
	return s.applyStateInTx(ctx, e, a, &r, Change{Version: version, State: n.State, Reason: n.Reason}, x.ID)
}
