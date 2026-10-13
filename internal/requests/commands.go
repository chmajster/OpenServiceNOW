package requests

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"strings"
	"time"
)

// Both HTTP and workflow use this domain command in their existing transaction.
// Authorization and the execution exception are verified by the callers; the
// command still checks lifecycle, required work, version, audit and outbox.
func (s *Service) applyStateInTx(ctx context.Context, e database.Executor, a Actor, r *Request, in Change, execution uint64) error {
	id := r.ID
	if in.Version == 0 || in.Version != r.Version {
		return ErrConflict
	}
	allowed := r.State == "SUBMITTED" && (in.State == "IN_PROGRESS" || in.State == "FULFILLED" || in.State == "CANCELLED") || r.State == "IN_PROGRESS" && (in.State == "FULFILLED" || in.State == "CANCELLED") || r.State == "FULFILLED" && in.State == "CLOSED"
	if !allowed {
		return ErrTransition
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !catalog.Text(in.Reason, 0, 10000) || in.State == "CANCELLED" && !catalog.Text(in.Reason, 1, 10000) {
		return ErrInvalid
	}
	if in.State == "CLOSED" || in.State == "FULFILLED" {
		if err := workflowGate(ctx, e, a.TenantID, id, execution); err != nil {
			return err
		}
		var incomplete bool
		if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM fulfillment_tasks WHERE tenant_id=? AND request_id=? AND required=TRUE AND state<>'COMPLETED')", a.TenantID, id).Scan(&incomplete); err != nil {
			return err
		}
		if incomplete {
			return ErrTransition
		}
	}
	if in.State == "CANCELLED" {
		if err := cancelWorkflows(ctx, e, a.TenantID, id, execution); err != nil {
			return err
		}
		if _, err := e.ExecContext(ctx, "UPDATE fulfillment_tasks SET state='CANCELLED',version=version+1,updated_at=? WHERE tenant_id=? AND request_id=? AND state NOT IN('COMPLETED','CANCELLED')", time.Now().UTC(), a.TenantID, id); err != nil {
			return err
		}
	}
	if in.State == "FULFILLED" {
		if _, err := e.ExecContext(ctx, "UPDATE fulfillment_tasks SET state='CANCELLED',version=version+1,updated_at=? WHERE tenant_id=? AND request_id=? AND required=FALSE AND state NOT IN('COMPLETED','CANCELLED')", time.Now().UTC(), a.TenantID, id); err != nil {
			return err
		}
	}
	if _, err := e.ExecContext(ctx, "UPDATE service_requests SET state=?,reason=?,version=version+1,updated_at=? WHERE tenant_id=? AND id=?", in.State, in.Reason, time.Now().UTC(), a.TenantID, id); err != nil {
		return err
	}
	r.Version++
	r.State = in.State
	if err := s.event(ctx, e, a, *r, "request.state.changed", "PUBLIC", fmt.Sprintf("request:%d:%d", id, r.Version), map[string]any{"state": r.State, "reason": in.Reason}); err != nil {
		return err
	}
	return nil
}
