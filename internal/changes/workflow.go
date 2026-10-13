package changes

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
)

func (s *Service) ValidateSubjectInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution) error {
	if x.Domain != "CHANGE" || x.ChangeID == 0 {
		return workflow.ErrInvalid
	}
	var revision uint64
	var execution *uint64
	if err := e.QueryRowContext(ctx, "SELECT assessment_revision,execution_id FROM changes WHERE tenant_id=? AND id=?", a.TenantID, x.ChangeID).Scan(&revision, &execution); err != nil {
		return missing(err)
	}
	if revision != x.SubjectRevision || execution == nil || *execution != x.ID || x.State == "CANCELLED" {
		return workflow.ErrConflict
	}
	return nil
}
func (s *Service) ContextInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution) (string, map[string]json.RawMessage, error) {
	if !a.Has("workflow.execute") || !a.Has("change.execute") || x.ExecutorID != a.UserID {
		return "", nil, ErrForbidden
	}
	if err := s.ValidateSubjectInTx(ctx, e, a, x); err != nil {
		return "", nil, err
	}
	r, err := s.get(ctx, e, a, x.ChangeID, true)
	if err != nil {
		return "", nil, err
	}
	if r.GroupID == nil || *r.GroupID != x.GroupID {
		return "", nil, ErrForbidden
	}
	values := map[string]json.RawMessage{}
	for k, v := range map[string]string{"risk": r.Risk, "impact": r.Impact, "change_type": r.Type} {
		raw, _ := json.Marshal(v)
		values[k] = raw
	}
	return r.State, values, nil
}
func (s *Service) CreateTaskInTx(context.Context, database.Executor, Actor, workflow.Execution, workflow.Node) (uint64, error) {
	return 0, workflow.ErrInvalid
}
func (s *Service) TaskStateInTx(context.Context, database.Executor, Actor, workflow.Execution, uint64) (workflow.TaskStatus, error) {
	return workflow.TaskStatus{}, workflow.ErrInvalid
}
func (s *Service) UpdateInTx(ctx context.Context, e database.Executor, a Actor, x workflow.Execution, n workflow.Node) error {
	if _, _, err := s.ContextInTx(ctx, e, a, x); err != nil {
		return err
	}
	r, err := s.get(ctx, e, a, x.ChangeID, true)
	if err != nil {
		return err
	}
	if r.State != "AUTHORIZE" || r.ApprovalState != "PENDING" {
		return ErrTransition
	}
	state, approval := r.State, "APPROVED"
	var revision any = r.AssessmentRevision
	if n.State == "REJECTED" && !x.Approved {
		approval = "REJECTED"
		revision = nil
		state = "NEW"
		if r.Type == "NORMAL" {
			state = "ASSESS"
		}
	} else if n.State != "AUTHORIZED" || !x.Approved {
		return ErrForbidden
	}
	if _, err = e.ExecContext(ctx, "UPDATE changes SET state=?,approval_state=?,approved_revision=?,reason=? WHERE tenant_id=? AND id=?", state, approval, revision, n.Reason, a.TenantID, r.ID); err != nil {
		return err
	}
	_, err = s.touch(ctx, e, a, r, "approval")
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) ApprovalProcesses(ctx context.Context, a Actor, kind string, before uint64, limit int) (ChoicePage, error) {
	out := ChoicePage{Items: []Choice{}}
	if !a.Has("change.read") && !a.Has("change.read_own") {
		return out, ErrForbidden
	}
	if !contains([]string{"NORMAL", "STANDARD", "EMERGENCY"}, kind) || limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, "SELECT d.id,d.name FROM workflow_definitions d JOIN workflow_versions v ON v.tenant_id=d.tenant_id AND v.definition_id=d.id AND v.version=d.published_version WHERE d.tenant_id=? AND JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.domain'))='CHANGE' AND JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.change_type'))=? AND (?=0 OR d.id<?) ORDER BY d.id DESC LIMIT ?", a.TenantID, kind, before, before, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Choice
		if err = rows.Scan(&c.ID, &c.Name); err != nil {
			return out, err
		}
		out.Items = append(out.Items, c)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}

// The choice endpoint exposes the pinned public configuration needed to select
// a compatible group. It does not expose unpublished definition drafts.
func (s *Service) ApprovalProcess(ctx context.Context, a Actor, id uint64) (workflow.Config, string, error) {
	if !a.Has("change.read") && !a.Has("change.read_own") {
		return workflow.Config{}, "", ErrForbidden
	}
	var version uint64
	if err := s.db.QueryRowContext(ctx, "SELECT published_version FROM workflow_definitions WHERE tenant_id=? AND id=? AND published_version IS NOT NULL", a.TenantID, id).Scan(&version); err != nil {
		return workflow.Config{}, "", missing(err)
	}
	c, err := workflow.Published(ctx, s.db, a.TenantID, id, version)
	if err != nil {
		return c, "", err
	}
	if c.Domain != "CHANGE" {
		return c, "", ErrNotFound
	}
	return c, fmt.Sprint(version), nil
}
