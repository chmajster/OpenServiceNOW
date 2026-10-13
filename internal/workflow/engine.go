package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"time"
)

// Commands is implemented by the owning domain. The engine never writes domain
// business tables or grants request.manage to its execution principal.
type Commands interface {
	ContextInTx(context.Context, database.Executor, Actor, Execution) (string, map[string]json.RawMessage, error)
	CreateTaskInTx(context.Context, database.Executor, Actor, Execution, Node) (uint64, error)
	TaskStateInTx(context.Context, database.Executor, Actor, Execution, uint64) (TaskStatus, error)
	UpdateInTx(context.Context, database.Executor, Actor, Execution, Node) error
}
type TaskStatus struct {
	State     string
	UpdatedAt time.Time
}

func enqueue(ctx context.Context, e database.Executor, tenant, id, version uint64, key string, at time.Time) error {
	raw, _ := json.Marshal(struct {
		ID      uint64 `json:"execution_id,string"`
		Version uint64 `json:"version,string"`
	}{id, version})
	_, _, err := jobs.NewRepository(nil).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: tenant, Type: JobType, Payload: raw, MaxAttempts: 5, AvailableAt: at, IdempotencyKey: key})
	return err
}
func emit(ctx context.Context, e database.Executor, a Actor, x Execution, kind, node string, at time.Time) error {
	data := map[string]any{"actor_user_id": fmt.Sprint(a.UserID), "occurred_at": at, "visibility": "PUBLIC", "execution_id": fmt.Sprint(x.ID), "node_id": node, "state": x.State, "execution_version": fmt.Sprint(x.Version)}
	if err := Audit(ctx, e, a, kind, x.ID, data); err != nil {
		return err
	}
	raw, _ := json.Marshal(data)
	_, _, err := events.NewRepository(nil).Append(ctx, e, events.Input{TenantID: a.TenantID, Type: kind, AggregateType: "request", AggregateID: fmt.Sprint(x.RequestID), Payload: raw, IdempotencyKey: fmt.Sprintf("workflow:%d:%d:%s", x.ID, x.Version, kind)})
	return err
}

// StartInTx is called inside catalog submission's transaction. No tasks are
// created here; even the first worker job commits with the request and receipt.
func StartInTx(ctx context.Context, e database.Executor, a Actor, definition, version, request, item, group uint64, at time.Time) (uint64, error) {
	c, err := Published(ctx, e, a.TenantID, definition, version)
	if err != nil {
		return 0, err
	}
	if c.GroupID != group {
		return 0, invalid("grupa pozycji katalogu nie odpowiada wersji procesu")
	}
	if _, err = ActiveExecutor(ctx, e, a.TenantID, c); err != nil {
		return 0, err
	}
	result, err := e.ExecContext(ctx, "INSERT INTO workflow_executions(tenant_id,definition_id,definition_version,request_id,requested_item_id,requester_id,executor_id,group_id,current_node,next_check_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", a.TenantID, definition, version, request, item, a.UserID, c.ExecutorID, group, start(c), at, at, at)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err = enqueue(ctx, e, a.TenantID, uint64(id), 1, fmt.Sprintf("workflow:%d:1", id), at); err != nil {
		return 0, err
	}
	x := Execution{ID: uint64(id), RequestID: request, Version: 1, State: "READY"}
	return x.ID, emit(ctx, e, a, x, "workflow.changed", start(c), at)
}
func load(ctx context.Context, e database.Executor, tenant, id uint64, lock bool) (Execution, error) {
	var x Execution
	var due sql.NullTime
	q := "SELECT x.id,x.definition_id,x.definition_version,x.request_id,x.requested_item_id,x.requester_id,x.executor_id,x.group_id,x.current_node,x.state,x.version,x.approved,x.error_code,x.next_check_at,x.created_at,x.updated_at,r.number FROM workflow_executions x JOIN service_requests r ON r.tenant_id=x.tenant_id AND r.id=x.request_id WHERE x.tenant_id=? AND x.id=?"
	if lock {
		q += " FOR UPDATE"
	}
	err := e.QueryRowContext(ctx, q, tenant, id).Scan(&x.ID, &x.DefinitionID, &x.DefinitionVersion, &x.RequestID, &x.RequestedItemID, &x.RequesterID, &x.ExecutorID, &x.GroupID, &x.CurrentNode, &x.State, &x.Version, &x.Approved, &x.ErrorCode, &due, &x.CreatedAt, &x.UpdatedAt, &x.RequestNumber)
	if due.Valid {
		x.NextCheckAt = &due.Time
	}
	return x, missing(err)
}
func lockExecution(ctx context.Context, e database.Executor, tenant, id uint64) (Execution, error) {
	var state string
	if err := e.QueryRowContext(ctx, "SELECT status FROM tenants WHERE id=? FOR UPDATE", tenant).Scan(&state); err != nil {
		return Execution{}, missing(err)
	}
	if state != "ACTIVE" {
		return Execution{}, workrecords.ErrForbidden
	}
	var request uint64
	if err := e.QueryRowContext(ctx, "SELECT request_id FROM workflow_executions WHERE tenant_id=? AND id=?", tenant, id).Scan(&request); err != nil {
		return Execution{}, missing(err)
	}
	if err := e.QueryRowContext(ctx, "SELECT state FROM service_requests WHERE tenant_id=? AND id=? FOR UPDATE", tenant, request).Scan(&state); err != nil {
		return Execution{}, missing(err)
	}
	return load(ctx, e, tenant, id, true)
}
func readStep(ctx context.Context, e database.Executor, tenant, id uint64, node string) (Step, error) {
	st := Step{NodeID: node}
	var started time.Time
	var deadline, finished sql.NullTime
	err := e.QueryRowContext(ctx, "SELECT state,version,result,started_at,deadline,finished_at FROM workflow_steps WHERE tenant_id=? AND execution_id=? AND node_id=?", tenant, id, node).Scan(&st.State, &st.Version, &st.Result, &started, &deadline, &finished)
	if err != nil {
		return st, missing(err)
	}
	st.StartedAt = &started
	if deadline.Valid {
		st.Deadline = &deadline.Time
	}
	if finished.Valid {
		st.FinishedAt = &finished.Time
	}
	return st, nil
}
func saveExecution(ctx context.Context, e database.Executor, a Actor, x *Execution, at time.Time) error {
	x.Version++
	x.UpdatedAt = at
	if _, err := e.ExecContext(ctx, "UPDATE workflow_executions SET current_node=?,state=?,version=?,approved=?,error_code=?,next_check_at=?,updated_at=? WHERE tenant_id=? AND id=?", x.CurrentNode, x.State, x.Version, x.Approved, x.ErrorCode, x.NextCheckAt, at, a.TenantID, x.ID); err != nil {
		return err
	}
	if x.State == "READY" {
		if err := enqueue(ctx, e, a.TenantID, x.ID, x.Version, fmt.Sprintf("workflow:%d:%d", x.ID, x.Version), at); err != nil {
			return err
		}
	}
	return emit(ctx, e, a, *x, "workflow.changed", x.CurrentNode, at)
}
func finish(ctx context.Context, e database.Executor, a Actor, x *Execution, n Node, to, outcome string, at time.Time) error {
	raw, _ := json.Marshal(map[string]string{"outcome": outcome})
	if _, err := e.ExecContext(ctx, "UPDATE workflow_steps SET state='COMPLETED',version=version+1,result=JSON_MERGE_PATCH(result,?),finished_at=? WHERE tenant_id=? AND execution_id=? AND node_id=?", raw, at, a.TenantID, x.ID, n.ID); err != nil {
		return err
	}
	x.State = "READY"
	x.CurrentNode = to
	x.NextCheckAt = &at
	x.ErrorCode = ""
	if n.Type == "END" {
		x.State = "COMPLETED"
		x.CurrentNode = n.ID
		x.NextCheckAt = nil
	}
	return nil
}
func (s *Service) advance(ctx context.Context, e database.Executor, tenant, id, expected uint64) (Execution, error) {
	x, err := lockExecution(ctx, e, tenant, id)
	if err != nil {
		return x, err
	}
	if x.Version != expected || x.State != "READY" && x.State != "WAITING" {
		return x, nil
	}
	c, err := Published(ctx, e, tenant, x.DefinitionID, x.DefinitionVersion)
	if err != nil {
		return x, err
	}
	a, err := ActiveExecutor(ctx, e, tenant, c)
	if err != nil {
		return x, fmt.Errorf("%w: wykonawca procesu", workrecords.ErrForbidden)
	}
	n := find(c, x.CurrentNode)
	if n.ID == "" {
		return x, ErrInvalid
	}
	at := s.now().UTC()
	parent, values, err := s.commands.ContextInTx(ctx, e, a, x)
	if err != nil {
		return x, err
	}
	if parent == "CANCELLED" && n.Type != "NOTIFY" && n.Type != "END" {
		x.State = "CANCELLED"
		x.NextCheckAt = nil
		return x, saveExecution(ctx, e, a, &x, at)
	}
	st, err := readStep(ctx, e, tenant, id, n.ID)
	if errors.Is(err, ErrNotFound) {
		st = Step{NodeID: n.ID, State: "WAITING", Version: 1, StartedAt: &at, Result: json.RawMessage(`{}`)}
		if n.Type == "APPROVAL" || n.Type == "WAIT_TASK" || n.Type == "WAIT_TIMER" {
			deadline := at.Add(time.Duration(n.Seconds) * time.Second)
			st.Deadline = &deadline
		}
		_, err = e.ExecContext(ctx, "INSERT INTO workflow_steps(tenant_id,execution_id,node_id,state,result,started_at,deadline) VALUES(?,?,?,'WAITING',?,?,?)", tenant, id, n.ID, []byte(st.Result), at, st.Deadline)
		if err != nil {
			return x, err
		}
		if n.Type == "APPROVAL" {
			ids, err := eligible(ctx, e, tenant, n, x.RequesterID, c.SelfApproval)
			if err != nil {
				return x, err
			}
			var group any
			if n.TargetType == "GROUP" {
				group = n.TargetID
			}
			for _, user := range ids {
				if _, err = e.ExecContext(ctx, "INSERT INTO workflow_decisions(tenant_id,execution_id,node_id,user_id,group_id,reason) VALUES(?,?,?,?,?,'')", tenant, id, n.ID, user, group); err != nil {
					return x, err
				}
			}
			if err = emit(ctx, e, a, x, "workflow.approval.requested", n.ID, at); err != nil {
				return x, err
			}
		}
	} else if err != nil {
		return x, err
	}
	to, outcome := n.Next, "COMPLETED"
	wasWaiting := x.State == "WAITING"
	wait := false
	switch n.Type {
	case "START", "END":
	case "CONDITION":
		if !equal(values[n.Field], n.Equals) {
			to = n.Otherwise
			outcome = "FALSE"
		} else {
			outcome = "TRUE"
		}
	case "APPROVAL":
		if !at.Before(*st.Deadline) {
			to = n.Timeout
			outcome = "TIMED_OUT"
			x.Approved = false
		} else {
			wait = true
		}
	case "CREATE_TASK":
		if !x.Approved {
			return x, workrecords.ErrForbidden
		}
		task, err := s.commands.CreateTaskInTx(ctx, e, a, x, n)
		if err != nil {
			return x, err
		}
		raw, _ := json.Marshal(map[string]string{"task_id": fmt.Sprint(task)})
		if _, err = e.ExecContext(ctx, "UPDATE workflow_steps SET result=? WHERE tenant_id=? AND execution_id=? AND node_id=?", raw, tenant, id, n.ID); err != nil {
			return x, err
		}
	case "WAIT_TASK":
		prior, err := readStep(ctx, e, tenant, id, n.TaskRef)
		if err != nil {
			return x, err
		}
		var result struct {
			TaskID uint64 `json:"task_id,string"`
		}
		if json.Unmarshal(prior.Result, &result) != nil || result.TaskID == 0 {
			return x, ErrInvalid
		}
		state, err := s.commands.TaskStateInTx(ctx, e, a, x, result.TaskID)
		if err != nil {
			return x, err
		}
		if state.State == "COMPLETED" && !state.UpdatedAt.After(*st.Deadline) {
			outcome = "TASK_COMPLETED"
		} else if state.State == "CANCELLED" || !at.Before(*st.Deadline) {
			to = n.Timeout
			outcome = "TIMED_OUT"
			x.Approved = false
		} else {
			wait = true
		}
	case "WAIT_TIMER":
		if at.Before(*st.Deadline) {
			wait = true
		} else {
			outcome = "TIMER_ELAPSED"
		}
	case "UPDATE_RECORD":
		if n.State != "CANCELLED" && !x.Approved {
			return x, workrecords.ErrForbidden
		}
		if err = s.commands.UpdateInTx(ctx, e, a, x, n); err != nil {
			return x, err
		}
	case "NOTIFY":
		if err = emit(ctx, e, a, x, "workflow.notification", n.ID, at); err != nil {
			return x, err
		}
	default:
		return x, ErrInvalid
	}
	if wait {
		x.State = "WAITING"
		due := *st.Deadline
		if n.Type == "WAIT_TASK" && due.After(at.Add(30*time.Second)) {
			due = at.Add(30 * time.Second)
		}
		x.NextCheckAt = &due
		// Durable polling advances only the next check. A long wait does not create
		// a new business version, audit event or attempt on every scheduler tick.
		if wasWaiting {
			_, err := e.ExecContext(ctx, "UPDATE workflow_executions SET next_check_at=? WHERE tenant_id=? AND id=?", due, tenant, id)
			return x, err
		}
	} else {
		if err = finish(ctx, e, a, &x, n, to, outcome, at); err != nil {
			return x, err
		}
	}
	if _, err = e.ExecContext(ctx, "INSERT INTO workflow_attempts(tenant_id,execution_id,node_id,execution_version,outcome,occurred_at) VALUES(?,?,?,?,?,?)", tenant, id, n.ID, expected, map[bool]string{true: "WAITING", false: "COMPLETED"}[wait], at); err != nil {
		return x, err
	}
	return x, saveExecution(ctx, e, a, &x, at)
}
func equal(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	p, _ := json.Marshal(x)
	q, _ := json.Marshal(y)
	return string(p) == string(q)
}
func (s *Service) Advance(ctx context.Context, tenant, id, version uint64) error {
	if s.commands == nil {
		return jobs.ErrInvalid
	}
	var loaded Execution
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		var err error
		loaded, err = s.advance(ctx, e, tenant, id, version)
		return err
	})
	if err == nil || errors.Is(err, database.ErrCommitUncertain) || ctx.Err() != nil {
		return err
	}
	code := "DOMAIN_COMMAND_REJECTED"
	if errors.Is(err, workrecords.ErrForbidden) {
		code = "EXECUTOR_ACCESS_REVOKED"
	}
	if errors.Is(err, ErrInvalid) {
		code = "INVALID_STEP_CONTEXT"
	}
	if loaded.ID == 0 {
		return err
	}
	// Effects of the failing step rolled back. Persist a safe diagnostic without
	// SQL/provider secrets. An administrator resumes precisely this same node.
	return s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		x, lockErr := lockExecution(ctx, e, tenant, id)
		if lockErr != nil {
			return lockErr
		}
		if x.Version != version || x.State != "READY" && x.State != "WAITING" {
			return nil
		}
		a := Actor{Actor: workrecords.Actor{TenantID: tenant, UserID: x.ExecutorID}}
		x.State = "ERROR"
		x.ErrorCode = code
		x.NextCheckAt = nil
		at := s.now()
		if _, err := e.ExecContext(ctx, "INSERT INTO workflow_attempts(tenant_id,execution_id,node_id,execution_version,outcome,error_code,occurred_at) VALUES(?,?,?,?,'ERROR',?,?)", tenant, id, x.CurrentNode, x.Version, code, at); err != nil {
			return err
		}
		return saveExecution(ctx, e, a, &x, at)
	})
}
func (s *Service) JobHandler(ctx context.Context, j jobs.Job) (json.RawMessage, error) {
	var p struct {
		ID      uint64 `json:"execution_id,string"`
		Version uint64 `json:"version,string"`
	}
	if j.TenantID == 0 || json.Unmarshal(j.Payload, &p) != nil || p.ID == 0 || p.Version == 0 {
		return nil, jobs.ErrInvalid
	}
	err := s.Advance(ctx, j.TenantID, p.ID, p.Version)
	return json.RawMessage(`{"processed":true}`), err
}
func (s *Service) Schedule(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT x.tenant_id,x.id,x.version FROM workflow_executions x JOIN tenants t ON t.id=x.tenant_id AND t.status='ACTIVE' WHERE x.state IN('READY','WAITING') AND x.next_check_at<=? ORDER BY x.next_check_at,x.id LIMIT 100", now.UTC())
	if err != nil {
		return 0, err
	}
	type ref struct{ tenant, id, version uint64 }
	refs := []ref{}
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.tenant, &r.id, &r.version); err != nil {
			rows.Close()
			return 0, err
		}
		refs = append(refs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, r := range refs {
		if err = enqueue(ctx, s.DB, r.tenant, r.id, r.version, fmt.Sprintf("workflow:tick:%d:%d:%d", r.id, r.version, now.Unix()/30), now.UTC()); err != nil {
			return 0, err
		}
	}
	return len(refs), nil
}
