package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"time"
)

// SubjectValidator checks the owning domain's immutable evaluation revision.
// It does not grant the approver permission to edit that domain's record.
type SubjectValidator interface {
	ValidateSubjectInTx(context.Context, database.Executor, Actor, Execution) error
}

// StartChangeInTx is called only after the Change command validates its type,
// complete plans, CI access and pinned definition in the same transaction.
func StartChangeInTx(ctx context.Context, e database.Executor, a Actor, definition, version, change, subjectVersion, revision, group uint64, snapshot json.RawMessage, at time.Time) (uint64, error) {
	c, err := Published(ctx, e, a.TenantID, definition, version)
	if err != nil {
		return 0, err
	}
	if c.Domain != "CHANGE" || c.GroupID != group || change == 0 || subjectVersion == 0 || revision == 0 || !json.Valid(snapshot) {
		return 0, ErrInvalid
	}
	if _, err = ActiveExecutor(ctx, e, a.TenantID, c); err != nil {
		return 0, err
	}
	result, err := e.ExecContext(ctx, "INSERT INTO workflow_executions(tenant_id,definition_id,definition_version,domain_type,change_id,subject_version,subject_revision,subject_snapshot,requester_id,executor_id,group_id,current_node,next_check_at,created_at,updated_at) VALUES(?,?,?,'CHANGE',?,?,?,?,?,?,?,?,?,?,?)", a.TenantID, definition, version, change, subjectVersion, revision, []byte(snapshot), a.UserID, c.ExecutorID, group, start(c), at, at, at)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	x := Execution{ID: uint64(id), Domain: "CHANGE", ChangeID: change, SubjectVersion: subjectVersion, SubjectRevision: revision, Version: 1, State: "READY"}
	if err = enqueue(ctx, e, a.TenantID, x.ID, 1, fmt.Sprintf("workflow:%d:1", x.ID), at); err != nil {
		return 0, err
	}
	return x.ID, emit(ctx, e, a, x, "workflow.changed", start(c), at)
}

// InvalidateChangeInTx keeps decisions and steps as historical evidence. Jobs
// already leased become harmless because the execution version changes.
func InvalidateChangeInTx(ctx context.Context, e database.Executor, a Actor, change uint64, at time.Time) error {
	rows, err := e.QueryContext(ctx, "SELECT id FROM workflow_executions WHERE tenant_id=? AND domain_type='CHANGE' AND change_id=? AND state IN('READY','WAITING','ERROR') ORDER BY id LIMIT 100", a.TenantID, change)
	if err != nil {
		return err
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		x, err := load(ctx, e, a.TenantID, id, true)
		if err != nil {
			return err
		}
		x.State = "CANCELLED"
		x.Approved = false
		x.NextCheckAt = nil
		if _, err = e.ExecContext(ctx, "UPDATE workflow_steps SET state='CANCELLED',version=version+1,finished_at=? WHERE tenant_id=? AND execution_id=? AND state='WAITING'", at, a.TenantID, id); err != nil {
			return err
		}
		if err = saveExecution(ctx, e, a, &x, at); err != nil {
			return err
		}
	}
	return nil
}
