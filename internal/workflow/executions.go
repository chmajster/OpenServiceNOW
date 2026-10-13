package workflow

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"time"
)

func participantSQL(alias string) string {
	return "EXISTS(SELECT 1 FROM workflow_decisions wd WHERE wd.tenant_id=" + alias + ".tenant_id AND wd.execution_id=" + alias + ".id AND wd.user_id=? AND (wd.group_id IS NULL OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=wd.tenant_id AND gm.group_id=wd.group_id AND gm.user_id=wd.user_id)))"
}

// AccessSQL filters lists/counts before pagination, including late notification reads.
func requestAccessSQL(a Actor, alias string) (string, []any) {
	if a.Has("workflow.manage") || a.Has("request.manage") {
		return "1=1", nil
	}
	q := "1=0"
	args := []any{}
	if a.Has("request.read_own") {
		q += " OR " + alias + ".requester_id=?"
		args = append(args, a.UserID)
	}
	if a.Has("request.fulfill") {
		q += " OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=" + alias + ".tenant_id AND gm.group_id=" + alias + ".group_id AND gm.user_id=?)"
		args = append(args, a.UserID)
	}
	if a.Has("workflow.approve") {
		q += " OR " + participantSQL(alias)
		args = append(args, a.UserID)
	}
	return "(" + q + ")", args
}

func AccessSQL(a Actor, alias string) (string, []any) {
	request, args := requestAccessSQL(a, alias)
	change := "1=0"
	changeArgs := []any{}
	if a.Has("change.read") {
		change = "1=1"
	} else if a.Has("change.read_own") {
		change = "EXISTS(SELECT 1 FROM changes ch JOIN work_assignments cwa ON cwa.tenant_id=ch.tenant_id AND cwa.object_type='change' AND cwa.object_id=CAST(ch.id AS CHAR) WHERE ch.tenant_id=" + alias + ".tenant_id AND ch.id=" + alias + ".change_id AND (ch.reporter_id=? OR cwa.assigned_to=? OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=ch.tenant_id AND gm.group_id=cwa.assignment_group_id AND gm.user_id=?)))"
		changeArgs = append(changeArgs, a.UserID, a.UserID, a.UserID)
	}
	if a.Has("workflow.approve") {
		change = "(" + change + " OR " + participantSQL(alias) + ")"
		changeArgs = append(changeArgs, a.UserID)
	}
	return "((" + alias + ".domain_type='REQUEST' AND " + request + ") OR (" + alias + ".domain_type='CHANGE' AND " + change + "))", append(args, changeArgs...)
}

type ExecutionPage struct {
	Items []Execution `json:"items"`
	Next  uint64      `json:"next_before_id,string,omitempty"`
}

func (s *Service) List(ctx context.Context, a Actor, mine bool, request, before uint64, state string, limit int) (ExecutionPage, error) {
	if limit < 1 || limit > 100 || state != "" && state != "READY" && state != "WAITING" && state != "ERROR" && state != "COMPLETED" && state != "CANCELLED" {
		return ExecutionPage{}, ErrInvalid
	}
	if mine && !a.Has("workflow.approve") {
		return ExecutionPage{}, workrecords.ErrForbidden
	}
	p, args := AccessSQL(a, "x")
	params := []any{a.TenantID}
	params = append(params, args...)
	q := "SELECT x.id,x.definition_id,x.definition_version,COALESCE(x.request_id,0),COALESCE(x.requested_item_id,0),x.executor_id,x.group_id,x.current_node,x.state,x.version,x.approved,x.error_code,x.created_at,x.updated_at,COALESCE(r.number,''),JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.name')),x.domain_type,COALESCE(x.change_id,0),COALESCE(x.subject_version,0),COALESCE(x.subject_revision,0),COALESCE(ch.number,'') FROM workflow_executions x LEFT JOIN service_requests r ON r.tenant_id=x.tenant_id AND r.id=x.request_id LEFT JOIN changes ch ON ch.tenant_id=x.tenant_id AND ch.id=x.change_id JOIN workflow_versions v ON v.tenant_id=x.tenant_id AND v.definition_id=x.definition_id AND v.version=x.definition_version WHERE x.tenant_id=? AND " + p
	if mine {
		q += pendingSQL()
		params = append(params, s.now(), a.UserID)
	}
	if request > 0 {
		q += " AND x.request_id=?"
		params = append(params, request)
	}
	if before > 0 {
		q += " AND x.id<?"
		params = append(params, before)
	}
	if state != "" {
		q += " AND x.state=?"
		params = append(params, state)
	}
	q += " ORDER BY x.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.DB.QueryContext(ctx, q, params...)
	if err != nil {
		return ExecutionPage{}, err
	}
	defer rows.Close()
	page := ExecutionPage{Items: []Execution{}}
	for rows.Next() {
		var x Execution
		if err = rows.Scan(&x.ID, &x.DefinitionID, &x.DefinitionVersion, &x.RequestID, &x.RequestedItemID, &x.ExecutorID, &x.GroupID, &x.CurrentNode, &x.State, &x.Version, &x.Approved, &x.ErrorCode, &x.CreatedAt, &x.UpdatedAt, &x.RequestNumber, &x.Name, &x.Domain, &x.ChangeID, &x.SubjectVersion, &x.SubjectRevision, &x.ChangeNumber); err != nil {
			return page, err
		}
		x.Steps = []Step{}
		x.Actions = []string{}
		if !a.Has("workflow.manage") {
			x.ErrorCode = ""
		}
		page.Items = append(page.Items, x)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Next = page.Items[limit-1].ID
	}
	return page, rows.Err()
}
func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Execution, error) {
	return s.get(ctx, s.DB, a, id)
}
func (s *Service) get(ctx context.Context, e database.Executor, a Actor, id uint64) (Execution, error) {
	p, args := AccessSQL(a, "x")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	var allowed bool
	if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workflow_executions x WHERE x.tenant_id=? AND x.id=? AND "+p+")", params...).Scan(&allowed); err != nil {
		return Execution{}, err
	}
	if !allowed {
		return Execution{}, ErrNotFound
	}
	x, err := load(ctx, e, a.TenantID, id, false)
	if err != nil {
		return x, err
	}
	c, err := Published(ctx, e, a.TenantID, x.DefinitionID, x.DefinitionVersion)
	if err != nil {
		return x, err
	}
	x.Name = c.Name
	x.Steps = []Step{}
	x.Actions = []string{}
	if x.Domain == "CHANGE" {
		x.Snapshot = x.SubjectSnapshot
	} else if err = e.QueryRowContext(ctx, "SELECT snapshot,form_values FROM requested_items WHERE tenant_id=? AND id=? AND request_id=?", a.TenantID, x.RequestedItemID, x.RequestID).Scan(&x.Snapshot, &x.Values); err != nil {
		return x, err
	}
	rows, err := e.QueryContext(ctx, "SELECT node_id,state,version,result,started_at,deadline,finished_at FROM workflow_steps WHERE tenant_id=? AND execution_id=? ORDER BY started_at,node_id LIMIT 50", a.TenantID, id)
	if err != nil {
		return x, err
	}
	stored := map[string]Step{}
	for rows.Next() {
		var st Step
		var started time.Time
		var deadline, finished sql.NullTime
		if err = rows.Scan(&st.NodeID, &st.State, &st.Version, &st.Result, &started, &deadline, &finished); err != nil {
			rows.Close()
			return x, err
		}
		st.StartedAt = &started
		if deadline.Valid {
			st.Deadline = &deadline.Time
		}
		if finished.Valid {
			st.FinishedAt = &finished.Time
		}
		stored[st.NodeID] = st
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return x, err
	}
	// One bounded query rather than one query per step/participant.
	rows, err = e.QueryContext(ctx, "SELECT wd.node_id,wd.user_id,u.display_name,wd.decision,wd.reason,wd.decided_at FROM workflow_decisions wd JOIN users u ON u.id=wd.user_id WHERE wd.tenant_id=? AND wd.execution_id=? ORDER BY wd.node_id,wd.user_id LIMIT 5000", a.TenantID, id)
	if err != nil {
		return x, err
	}
	decisions := map[string][]Decision{}
	for rows.Next() {
		var node string
		var d Decision
		var at sql.NullTime
		if err = rows.Scan(&node, &d.UserID, &d.Name, &d.Decision, &d.Reason, &at); err != nil {
			rows.Close()
			return x, err
		}
		if at.Valid {
			d.DecidedAt = &at.Time
		}
		if a.Has("workflow.manage") || d.UserID == a.UserID {
			decisions[node] = append(decisions[node], d)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return x, err
	}
	for _, n := range c.Nodes {
		st, ok := stored[n.ID]
		if !ok {
			st = Step{NodeID: n.ID, State: "NOT_STARTED"}
		}
		st.Label = n.Label
		st.Type = n.Type
		st.Decisions = decisions[n.ID]
		if n.Type == "APPROVAL" && n.ID == x.CurrentNode && x.State == "WAITING" && st.Deadline != nil && s.now().Before(*st.Deadline) {
			for _, d := range st.Decisions {
				if d.UserID == a.UserID && d.Decision == "PENDING" {
					st.CanDecide, err = userAllowed(ctx, e, a.TenantID, a.UserID, n, x.RequesterID, c.SelfApproval)
					if err != nil {
						return x, err
					}
				}
			}
		}
		x.Steps = append(x.Steps, st)
	}
	if a.Has("workflow.manage") {
		if x.State == "ERROR" {
			x.Actions = append(x.Actions, "RESUME")
		}
		if x.State == "ERROR" || x.State == "READY" || x.State == "WAITING" {
			x.Actions = append(x.Actions, "CANCEL")
		}
		rows, err := e.QueryContext(ctx, "SELECT id,node_id,execution_version,outcome,error_code,occurred_at FROM workflow_attempts WHERE tenant_id=? AND execution_id=? ORDER BY id DESC LIMIT 100", a.TenantID, id)
		if err != nil {
			return x, err
		}
		x.Attempts = []Attempt{}
		for rows.Next() {
			var at Attempt
			if err = rows.Scan(&at.ID, &at.NodeID, &at.Version, &at.Outcome, &at.ErrorCode, &at.At); err != nil {
				rows.Close()
				return x, err
			}
			x.Attempts = append(x.Attempts, at)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return x, err
		}
	} else {
		x.ErrorCode = ""
	}
	return x, nil
}
func (s *Service) Decide(ctx context.Context, a Actor, id uint64, node string, in DecisionInput) (Execution, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version == 0 || in.Decision != "APPROVED" && in.Decision != "REJECTED" || !validText(in.Reason, 1, 2000) {
		return Execution{}, ErrInvalid
	}
	var out Execution
	expired := false
	err := s.actorTx(ctx, a, "workflow.approve", func(e database.Executor, a Actor) error {
		x, err := lockExecution(ctx, e, a.TenantID, id)
		if err != nil {
			return err
		}
		if x.Domain == "CHANGE" {
			commands, err := s.domainCommands(x)
			if err != nil {
				return err
			}
			validator, ok := commands.(SubjectValidator)
			if !ok {
				return ErrInvalid
			}
			if err = validator.ValidateSubjectInTx(ctx, e, a, x); err != nil {
				return err
			}
		}
		c, err := Published(ctx, e, a.TenantID, x.DefinitionID, x.DefinitionVersion)
		if err != nil {
			return err
		}
		n := find(c, node)
		if n.Type != "APPROVAL" {
			return ErrNotFound
		}
		allowed, err := userAllowed(ctx, e, a.TenantID, a.UserID, n, x.RequesterID, c.SelfApproval)
		if err != nil {
			return err
		}
		if !allowed {
			return workrecords.ErrForbidden
		}
		var previous, reason string
		err = e.QueryRowContext(ctx, "SELECT decision,reason FROM workflow_decisions WHERE tenant_id=? AND execution_id=? AND node_id=? AND user_id=?", a.TenantID, id, node, a.UserID).Scan(&previous, &reason)
		if err != nil {
			return missing(err)
		}
		if previous != "PENDING" {
			if previous != in.Decision || reason != in.Reason {
				return ErrConflict
			}
			out, err = s.get(ctx, e, a, id)
			return err
		}
		st, err := readStep(ctx, e, a.TenantID, id, node)
		if err != nil {
			return err
		}
		if x.State != "WAITING" || x.CurrentNode != node || st.State != "WAITING" || in.Version != st.Version {
			return ErrConflict
		}
		at := s.now().UTC()
		if !at.Before(*st.Deadline) {
			x.Approved = false
			if err = finish(ctx, e, a, &x, n, n.Timeout, "TIMED_OUT", at); err != nil {
				return err
			}
			if err = saveExecution(ctx, e, a, &x, at); err != nil {
				return err
			}
			expired = true
			return nil
		}
		if _, err = e.ExecContext(ctx, "UPDATE workflow_decisions SET decision=?,reason=?,decided_at=? WHERE tenant_id=? AND execution_id=? AND node_id=? AND user_id=? AND decision='PENDING'", in.Decision, in.Reason, at, a.TenantID, id, node, a.UserID); err != nil {
			return err
		}
		if err = Audit(ctx, e, a, "workflow.decision", id, map[string]any{"node_id": node, "decision": in.Decision, "reason": in.Reason, "step_version": fmt.Sprint(st.Version)}); err != nil {
			return err
		}
		to, outcome, done := n.Next, "APPROVED", in.Decision == "REJECTED" || n.Policy == "ANY"
		if in.Decision == "REJECTED" {
			to = n.Rejected
			outcome = "REJECTED"
			x.Approved = false
		} else {
			var pending int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_decisions WHERE tenant_id=? AND execution_id=? AND node_id=? AND decision<>'APPROVED'", a.TenantID, id, node).Scan(&pending); err != nil {
				return err
			}
			done = done || pending == 0
			if done {
				// ALL also rechecks prior approvers: revocation while waiting cannot silently
				// authorize continuation. Keep the step waiting until its immutable deadline.
				rows, err := e.QueryContext(ctx, "SELECT user_id FROM workflow_decisions WHERE tenant_id=? AND execution_id=? AND node_id=? AND decision='APPROVED'", a.TenantID, id, node)
				if err != nil {
					return err
				}
				users := []uint64{}
				for rows.Next() {
					var user uint64
					if err = rows.Scan(&user); err != nil {
						rows.Close()
						return err
					}
					users = append(users, user)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return err
				}
				for _, user := range users {
					ok, err := userAllowed(ctx, e, a.TenantID, user, n, x.RequesterID, c.SelfApproval)
					if err != nil {
						return err
					}
					if !ok {
						done = false
					}
				}
			}
			x.Approved = done
		}
		if done {
			if err = finish(ctx, e, a, &x, n, to, outcome, at); err != nil {
				return err
			}
		} else {
			if _, err = e.ExecContext(ctx, "UPDATE workflow_steps SET version=version+1 WHERE tenant_id=? AND execution_id=? AND node_id=?", a.TenantID, id, node); err != nil {
				return err
			}
		}
		if err = saveExecution(ctx, e, a, &x, at); err != nil {
			return err
		}
		out, err = s.get(ctx, e, a, id)
		return err
	})
	if err == nil && expired {
		err = ErrConflict
	}
	return out, err
}
func (s *Service) Repair(ctx context.Context, a Actor, id, version uint64, action, reason string) (Execution, error) {
	if version == 0 || action != "RESUME" && action != "CANCEL" || !validText(reason, 1, 2000) {
		return Execution{}, ErrInvalid
	}
	var out Execution
	err := s.actorTx(ctx, a, "workflow.manage", func(e database.Executor, a Actor) error {
		x, err := lockExecution(ctx, e, a.TenantID, id)
		if err != nil {
			return err
		}
		if x.Domain == "CHANGE" {
			p, args := AccessSQL(a, "x")
			var ok bool
			params := append([]any{a.TenantID, id}, args...)
			if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workflow_executions x WHERE x.tenant_id=? AND x.id=? AND "+p+")", params...).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return ErrNotFound
			}
		}
		if x.Version != version {
			return ErrConflict
		}
		at := s.now()
		if action == "RESUME" {
			if x.State != "ERROR" {
				return ErrConflict
			}
			x.State = "READY"
			x.ErrorCode = ""
			x.NextCheckAt = &at
		} else {
			if x.State != "ERROR" && x.State != "READY" && x.State != "WAITING" {
				return ErrConflict
			}
			x.State = "CANCELLED"
			x.NextCheckAt = nil
			if _, err = e.ExecContext(ctx, "UPDATE workflow_steps SET state='CANCELLED',version=version+1,finished_at=? WHERE tenant_id=? AND execution_id=? AND state='WAITING'", at, a.TenantID, id); err != nil {
				return err
			}
		}
		if err = Audit(ctx, e, a, "workflow.repair", id, map[string]string{"action": action, "reason": reason}); err != nil {
			return err
		}
		if err = saveExecution(ctx, e, a, &x, at); err != nil {
			return err
		}
		out, err = s.get(ctx, e, a, id)
		return err
	})
	return out, err
}

func pendingSQL() string {
	return " AND x.state='WAITING' AND EXISTS(SELECT 1 FROM workflow_steps ws JOIN workflow_decisions wd ON wd.tenant_id=ws.tenant_id AND wd.execution_id=ws.execution_id AND wd.node_id=ws.node_id WHERE ws.tenant_id=x.tenant_id AND ws.execution_id=x.id AND ws.node_id=x.current_node AND ws.state='WAITING' AND ws.deadline>? AND wd.user_id=? AND wd.decision='PENDING' AND (wd.group_id IS NULL OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=wd.tenant_id AND gm.group_id=wd.group_id AND gm.user_id=wd.user_id)))"
}
func (s *Service) PendingCount(ctx context.Context, a Actor) (uint64, error) {
	if !a.Has("workflow.approve") {
		return 0, workrecords.ErrForbidden
	}
	p, args := AccessSQL(a, "x")
	params := []any{a.TenantID}
	params = append(params, args...)
	params = append(params, s.now(), a.UserID)
	var n uint64
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_executions x WHERE x.tenant_id=? AND "+p+pendingSQL(), params...).Scan(&n)
	return n, err
}
